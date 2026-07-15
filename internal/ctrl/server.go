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

// ServerConfig holds the Master-side control channel configuration.
type ServerConfig struct {
	ListenAddr    string // e.g. ":8443"
	StaticPrivB64 string // Master static X25519 private key (base64)
}

// ServerHandler is called by the Server when agent events arrive. The
// Master implements this to update its store and forward results to
// Telegram. All callbacks are non-blocking (called in the read loop
// goroutine); the handler should do async work if needed.
type ServerHandler interface {
	// OnHello is called when an agent sends its hello frame.
	OnHello(uuid [ewp.UUIDLen]byte, hd HelloData)
	// OnHeartbeat is called on each heartbeat.
	OnHeartbeat(uuid [ewp.UUIDLen]byte)
	// OnResult is called when a command result arrives.
	OnResult(uuid [ewp.UUIDLen]byte, ev EventMessage)
	// OnQuality is called when a quality check result arrives.
	OnQuality(uuid [ewp.UUIDLen]byte, ev EventMessage)
	// OnReport is called when a report event arrives.
	OnReport(uuid [ewp.UUIDLen]byte, ev EventMessage)
	// OnLog is called when a log event arrives.
	OnLog(uuid [ewp.UUIDLen]byte, ev EventMessage)
	// OnDisconnect is called when an agent disconnects.
	OnDisconnect(uuid [ewp.UUIDLen]byte)
}

// Server is the Master-side control channel. It listens for Agent
// connections, authenticates them via EWP, and maintains an online
// table of connected Agents. Commands can be sent to any connected
// Agent via SendCommand.
type Server struct {
	cfg     ServerConfig
	ewp     *ewp.ServiceV21
	log     *slog.Logger
	handler ServerHandler

	mu     sync.RWMutex
	agents map[[ewp.UUIDLen]byte]*agentConn

	pendingMu sync.Mutex
	pending   map[string]map[string]*pendingCmd

	ln     net.Listener
	closed bool
}

// agentConn tracks a single connected Agent.
type agentConn struct {
	conn     net.Conn
	uuid     [ewp.UUIDLen]byte
	mu       sync.RWMutex // protects info and lastSeen
	info     *HelloData
	lastSeen time.Time
	writeMu  sync.Mutex // serialises frame writes to conn
}

// NewServer creates a Server. The EWP service is configured with the
// given static private key; agents are added via AddAgent before they
// can connect.
func NewServer(cfg ServerConfig, log *slog.Logger) (*Server, error) {
	if cfg.ListenAddr == "" {
		return nil, errors.New("ctrl: ListenAddr is required")
	}
	if cfg.StaticPrivB64 == "" {
		return nil, errors.New("ctrl: StaticPrivB64 is required")
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		cfg:    cfg,
		log:    log,
		agents: make(map[[ewp.UUIDLen]byte]*agentConn),
	}
	// Create EWP service with our handler.
	h := &ewpHandler{server: s}
	svc, err := ewp.NewServiceV21(h, cfg.StaticPrivB64)
	if err != nil {
		return nil, fmt.Errorf("ctrl: create EWP service: %w", err)
	}
	s.ewp = svc
	return s, nil
}

// SetHandler registers an event handler for agent events.
func (s *Server) SetHandler(h ServerHandler) {
	s.handler = h
}

// AddAgent registers a UUID in the EWP whitelist so the agent can
// authenticate. Must be called before the agent connects.
func (s *Server) AddAgent(uuid string) error {
	return s.ewp.AddUser(uuid)
}

// RemoveAgent removes a UUID from the whitelist.
func (s *Server) RemoveAgent(uuid string) bool {
	return s.ewp.RemoveUser(uuid)
}

// Listen starts accepting connections. Blocks until ctx is cancelled
// or the listener is closed.
func (s *Server) Listen(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("ctrl: listen: %w", err)
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	s.log.Info("control server listening", "addr", s.cfg.ListenAddr)

	go func() {
		<-ctx.Done()
		s.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			return fmt.Errorf("ctrl: accept: %w", err)
		}
		go s.handleRawConn(ctx, conn)
	}
}

// Addr returns the listener address once Listen has bound it, or empty
// string if not yet listening. Useful for tests that need the actual port.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Close stops the listener. Already-connected agents remain until their
// connections drop.
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.ln != nil {
		s.ln.Close()
	}
	s.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Online table
// ---------------------------------------------------------------------------

// IsOnline reports whether the given agent UUID currently has an active
// control connection.
func (s *Server) IsOnline(uuid [ewp.UUIDLen]byte) bool {
	s.mu.RLock()
	_, ok := s.agents[uuid]
	s.mu.RUnlock()
	return ok
}

// AgentInfo is a snapshot of a connected agent's state, for the UI layer.
type AgentInfo struct {
	UUID     [ewp.UUIDLen]byte
	Info     *HelloData
	LastSeen time.Time
}

// OnlineAgents returns a snapshot of all currently connected agents.
func (s *Server) OnlineAgents() []AgentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AgentInfo, 0, len(s.agents))
	for uuid, ac := range s.agents {
		ai := AgentInfo{UUID: uuid}
		ac.mu.RLock()
		ai.LastSeen = ac.lastSeen
		if ac.info != nil {
			hd := *ac.info // copy to avoid external race
			ai.Info = &hd
		}
		ac.mu.RUnlock()
		out = append(out, ai)
	}
	return out
}

// WaitForAgent blocks until the given UUID connects or timeout elapses.
// Useful for tests.
func (s *Server) WaitForAgent(uuid [ewp.UUIDLen]byte, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.IsOnline(uuid) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// ---------------------------------------------------------------------------
// Command delivery
// ---------------------------------------------------------------------------

// pendingCmd tracks a command awaiting a result.
type pendingCmd struct {
	done   chan struct{}
	result EventMessage
}

// SendCommand sends a command to the agent identified by uuid. Returns
// an error if the agent is not currently connected.
func (s *Server) SendCommand(uuid [ewp.UUIDLen]byte, msg CommandMessage) error {
	s.mu.RLock()
	ac, ok := s.agents[uuid]
	s.mu.RUnlock()
	if !ok {
		return errors.New("ctrl: agent not connected")
	}
	data, _ := json.Marshal(msg)
	ac.writeMu.Lock()
	defer ac.writeMu.Unlock()
	return writeFrame(ac.conn, data)
}

// SendCommandAndWait sends a command and blocks until the agent replies
// with a result matching msg.ID, or the timeout elapses. Returns the
// result event, or an error if the agent disconnects / times out.
func (s *Server) SendCommandAndWait(uuid [ewp.UUIDLen]byte, msg CommandMessage, timeout time.Duration) (EventMessage, error) {
	// Register pending entry before sending.
	pc := &pendingCmd{done: make(chan struct{})}
	s.pendingMu.Lock()
	if s.pending == nil {
		s.pending = make(map[string]map[string]*pendingCmd)
	}
	if s.pending[string(uuid[:])] == nil {
		s.pending[string(uuid[:])] = make(map[string]*pendingCmd)
	}
	s.pending[string(uuid[:])][msg.ID] = pc
	s.pendingMu.Unlock()

	// Send the command.
	if err := s.SendCommand(uuid, msg); err != nil {
		s.pendingMu.Lock()
		delete(s.pending[string(uuid[:])], msg.ID)
		s.pendingMu.Unlock()
		return EventMessage{}, err
	}

	// Wait for result or timeout.
	select {
	case <-pc.done:
		return pc.result, nil
	case <-time.After(timeout):
		s.pendingMu.Lock()
		delete(s.pending[string(uuid[:])], msg.ID)
		s.pendingMu.Unlock()
		return EventMessage{}, errors.New("ctrl: command timed out")
	}
}

// resolveResult wakes any goroutine waiting for the given command ID.
func (s *Server) resolveResult(uuid [ewp.UUIDLen]byte, ev EventMessage) {
	s.pendingMu.Lock()
	uuidMap, ok := s.pending[string(uuid[:])]
	if !ok {
		s.pendingMu.Unlock()
		return
	}
	pc, ok := uuidMap[ev.ID]
	if !ok {
		s.pendingMu.Unlock()
		return
	}
	delete(uuidMap, ev.ID)
	s.pendingMu.Unlock()

	pc.result = ev
	close(pc.done)
}

// ---------------------------------------------------------------------------
// EWP handler + connection lifecycle
// ---------------------------------------------------------------------------

// ewpHandler implements ewp.Handler.
type ewpHandler struct {
	server *Server
}

func (h *ewpHandler) NewConnection(ctx context.Context, conn net.Conn, md ewp.Metadata) error {
	return h.server.serveAgent(ctx, conn, md)
}

func (h *ewpHandler) NewPacketConnection(_ context.Context, _ net.PacketConn, _ ewp.Metadata) error {
	return errors.New("ctrl: UDP not supported")
}

func (s *Server) handleRawConn(ctx context.Context, conn net.Conn) {
	// EWP handshake happens inside HandleConn; on success our handler
	// is called. On failure, HandleConn closes the conn and returns.
	if err := s.ewp.HandleConn(ctx, conn); err != nil {
		s.log.Debug("EWP handshake failed", "err", err, "remote", conn.RemoteAddr())
	}
}

// serveAgent is called by the EWP handler after a successful handshake.
// It registers the agent in the online table and runs the read loop
// until the connection closes.
func (s *Server) serveAgent(ctx context.Context, conn net.Conn, md ewp.Metadata) error {
	ac := &agentConn{
		conn:     conn,
		uuid:     md.UserUUID,
		lastSeen: time.Now(),
	}

	s.mu.Lock()
	s.agents[md.UserUUID] = ac
	s.mu.Unlock()
	s.log.Info("agent connected", "uuid", md.UserUUID,
		"remote", md.Source)

	defer func() {
		s.mu.Lock()
		delete(s.agents, md.UserUUID)
		s.mu.Unlock()
		// Wake any goroutines waiting for results from this agent.
		s.pendingMu.Lock()
		if uuidMap, ok := s.pending[string(md.UserUUID[:])]; ok {
			for id, pc := range uuidMap {
				close(pc.done)
				delete(uuidMap, id)
			}
		}
		s.pendingMu.Unlock()
		conn.Close()
		if s.handler != nil {
			s.handler.OnDisconnect(md.UserUUID)
		}
		s.log.Info("agent disconnected", "uuid", md.UserUUID)
	}()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		raw, err := readFrame(conn)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		ev, err := DecodeEvent(raw)
		if err != nil {
			s.log.Warn("rejecting malformed event", "err", err)
			continue
		}
		switch ev.Evt {
		case EvtHello:
			var hd HelloData
			if err := json.Unmarshal(ev.Data, &hd); err != nil {
				s.log.Warn("malformed hello data", "err", err)
				continue
			}
			ac.mu.Lock()
			ac.info = &hd
			ac.mu.Unlock()
			s.log.Info("agent hello",
				"node", hd.Node, "alias", hd.Alias,
				"region", hd.Region, "ip", hd.IP)
			if s.handler != nil {
				s.handler.OnHello(md.UserUUID, hd)
			}
		case EvtHeartbeat:
			ac.mu.Lock()
			ac.lastSeen = time.Now()
			ac.mu.Unlock()
			if s.handler != nil {
				s.handler.OnHeartbeat(md.UserUUID)
			}
		case EvtResult:
			s.log.Debug("agent result", "id", ev.ID,
				"data_len", len(ev.Data))
			s.resolveResult(md.UserUUID, ev)
			if s.handler != nil {
				s.handler.OnResult(md.UserUUID, ev)
			}
		case EvtQuality:
			s.log.Debug("agent quality", "id", ev.ID,
				"data_len", len(ev.Data))
			if s.handler != nil {
				s.handler.OnQuality(md.UserUUID, ev)
			}
		case EvtReport:
			s.log.Debug("agent report", "data_len", len(ev.Data))
			if s.handler != nil {
				s.handler.OnReport(md.UserUUID, ev)
			}
		case EvtLog:
			s.log.Debug("agent log", "data_len", len(ev.Data))
			if s.handler != nil {
				s.handler.OnLog(md.UserUUID, ev)
			}
		default:
			s.log.Warn("unhandled event", "evt", ev.Evt)
		}
	}
}
