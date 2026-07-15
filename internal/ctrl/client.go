package ctrl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	ewp "github.com/justinwoo280/sing-ewp"
)

// ClientConfig holds the Agent-side control channel configuration.
type ClientConfig struct {
	MasterAddr       string        // e.g. "master.example.com:8443"
	UUID             string        // Agent UUID (EWP PSK)
	ServerPubB64     string        // Master static X25519 public key (base64)
	Heartbeat        time.Duration // heartbeat interval (default 30s)
	MinBackoff       time.Duration // min reconnect backoff (default 1s)
	MaxBackoff       time.Duration // max reconnect backoff (default 60s)
	HandshakeTimeout time.Duration // EWP handshake timeout (default 15s)
	Hello            HelloData     // sent as first event on connect
}

func (c *ClientConfig) defaults() error {
	if c.MasterAddr == "" {
		return errors.New("ctrl: MasterAddr is required")
	}
	if c.UUID == "" {
		return errors.New("ctrl: UUID is required")
	}
	if c.ServerPubB64 == "" {
		return errors.New("ctrl: ServerPubB64 is required")
	}
	if c.Heartbeat == 0 {
		c.Heartbeat = 30 * time.Second
	}
	if c.MinBackoff == 0 {
		c.MinBackoff = 1 * time.Second
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = 60 * time.Second
	}
	if c.HandshakeTimeout == 0 {
		c.HandshakeTimeout = 15 * time.Second
	}
	return nil
}

// Client is the Agent-side control connection manager. It dials the
// Master, maintains a persistent EWP-encrypted connection, dispatches
// incoming commands to the Executor, sends heartbeats, and reconnects
// on failure with exponential backoff.
type Client struct {
	cfg  ClientConfig
	ewp  *ewp.ClientV21
	exec Executor
	log  *slog.Logger
}

// NewClient creates a Client that connects to Master and dispatches
// commands to exec.
func NewClient(cfg ClientConfig, exec Executor, log *slog.Logger) (*Client, error) {
	if exec == nil {
		return nil, errors.New("ctrl: Executor is required")
	}
	if err := cfg.defaults(); err != nil {
		return nil, err
	}
	c, err := ewp.NewClientV21(cfg.UUID, cfg.ServerPubB64)
	if err != nil {
		return nil, fmt.Errorf("ctrl: create EWP client: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	return &Client{cfg: cfg, ewp: c, exec: exec, log: log}, nil
}

// Run is the main loop: connect → serve → on error, backoff → reconnect.
// Blocks until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	backoff := c.cfg.MinBackoff
	for {
		err := c.connectAndServe(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.log.Warn("control connection lost, reconnecting",
			"err", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			backoff *= 2
			if backoff > c.cfg.MaxBackoff {
				backoff = c.cfg.MaxBackoff
			}
		}
	}
}

// connWriter serialises frame writes to a single connection. The EWP
// SecureStream is not safe for concurrent writes, and we now write from
// several goroutines (heartbeat, and one per in-flight command), so all
// writes must go through this mutex.
type connWriter struct {
	mu   sync.Mutex
	conn net.Conn
}

func (w *connWriter) write(ev EventMessage) error {
	out, _ := json.Marshal(ev)
	w.mu.Lock()
	defer w.mu.Unlock()
	return writeFrame(w.conn, out)
}

func (c *Client) connectAndServe(ctx context.Context) error {
	d := &net.Dialer{Timeout: c.cfg.HandshakeTimeout}
	rawConn, err := d.DialContext(ctx, "tcp", c.cfg.MasterAddr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	hctx, cancel := context.WithTimeout(ctx, c.cfg.HandshakeTimeout)
	defer cancel()
	// Dummy address — we use the EWP stream as a control channel, not
	// a proxy. The server handler ignores this destination.
	dst := ewp.Address{Domain: "control.sentinel.local", Port: 1}
	conn, err := c.ewp.DialConn(hctx, rawConn, dst)
	if err != nil {
		rawConn.Close()
		return fmt.Errorf("ewp handshake: %w", err)
	}
	defer conn.Close()
	c.log.Info("control connection established", "master", c.cfg.MasterAddr)

	w := &connWriter{conn: conn}

	// Close the connection when ctx is cancelled to unblock the blocking
	// readFrame in readLoop (otherwise cancellation would not be observed
	// until the next frame arrives).
	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()
	go func() {
		<-connCtx.Done()
		conn.Close()
	}()

	// Send hello event as first frame.
	if err := w.write(EventMessage{Evt: EvtHello, Data: mustMarshal(c.cfg.Hello)}); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	// Start heartbeat ticker.
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go c.heartbeatLoop(hbCtx, w)

	// Read-dispatch loop.
	return c.readLoop(ctx, conn, w)
}

func (c *Client) heartbeatLoop(ctx context.Context, w *connWriter) {
	ticker := time.NewTicker(c.cfg.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.write(EventMessage{Evt: EvtHeartbeat}); err != nil {
				return
			}
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn net.Conn, w *connWriter) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		raw, err := readFrame(conn)
		if err != nil {
			return fmt.Errorf("read frame: %w", err)
		}
		msg, err := DecodeCommand(raw)
		if err != nil {
			c.log.Warn("rejecting malformed command", "err", err)
			// Send error result without an ID (we don't have one).
			_ = w.write(EventMessage{
				Evt:  EvtResult,
				Data: mustMarshal(Result{OK: false, Msg: err.Error()}),
			})
			continue
		}
		c.log.Info("received command", "cmd", msg.Cmd, "id", msg.ID)
		// Execute each command in its own goroutine so a long-running
		// module (google/trust sessions can take minutes) does not block
		// reading subsequent commands or heartbeats. Writes are serialised
		// by connWriter.
		go func(msg CommandMessage) {
			ev := Dispatch(ctx, msg, c.exec)
			if err := w.write(ev); err != nil {
				c.log.Warn("failed to send command result", "id", msg.ID, "err", err)
			}
		}(msg)
	}
}
