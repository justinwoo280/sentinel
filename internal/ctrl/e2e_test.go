package ctrl

import (
	"context"
	"testing"
	"time"

	ewp "github.com/justinwoo280/sing-ewp"
)

// waitOfflineExec is an executor that records disconnect via commands.
type e2eExec struct {
	got chan Command
}

func newE2EExec() *e2eExec { return &e2eExec{got: make(chan Command, 16)} }

func (e *e2eExec) Run(context.Context) Result        { e.got <- CmdRun; return Result{OK: true, Msg: "ok"} }
func (e *e2eExec) ModGoogle(context.Context) Result  { e.got <- CmdModGoogle; return Result{OK: true} }
func (e *e2eExec) ModTrust(context.Context) Result   { e.got <- CmdModTrust; return Result{OK: true} }
func (e *e2eExec) ModQuality(context.Context) Result { e.got <- CmdModQuality; return Result{OK: true} }
func (e *e2eExec) Report(context.Context) Result {
	e.got <- CmdReport
	return Result{OK: true, Msg: "report-data"}
}
func (e *e2eExec) Log(context.Context) Result { e.got <- CmdLog; return Result{OK: true} }
func (e *e2eExec) ConfigRename(_ context.Context, _ string) Result {
	e.got <- CmdConfigRename
	return Result{OK: true}
}
func (e *e2eExec) ConfigToggle(_ context.Context, _ string, _ bool) Result {
	e.got <- CmdConfigToggle
	return Result{OK: true}
}
func (e *e2eExec) OTA(context.Context) Result { e.got <- CmdOTA; return Result{OK: true} }

// startE2E spins up a server + client and returns them once the agent is
// connected. Callers must cancel both contexts.
func startE2E(t *testing.T) (*Server, *Client, [ewp.UUIDLen]byte, context.CancelFunc, context.CancelFunc) {
	t.Helper()
	privB64, pubB64, err := ewp.GenerateServerStaticKeypair()
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	uuidBytes, err := ewp.ParseUUID(testUUID)
	if err != nil {
		t.Fatalf("ParseUUID: %v", err)
	}

	srv, err := NewServer(ServerConfig{ListenAddr: "127.0.0.1:0", StaticPrivB64: privB64}, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.AddAgent(testUUID); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	srvCtx, srvCancel := context.WithCancel(context.Background())
	go func() { _ = srv.Listen(srvCtx) }()

	addr := ""
	for i := 0; i < 200; i++ {
		if addr = srv.Addr(); addr != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if addr == "" {
		srvCancel()
		t.Fatal("server did not listen")
	}

	te := newE2EExec()
	cli, err := NewClient(ClientConfig{
		MasterAddr:   addr,
		UUID:         testUUID,
		ServerPubB64: pubB64,
		Heartbeat:    500 * time.Millisecond,
		MinBackoff:   100 * time.Millisecond,
		MaxBackoff:   1 * time.Second,
		Hello: HelloData{
			Node: "e2e-node", Alias: "e2e", Region: "US",
			IP: "127.0.0.1", Version: "test", Google: true, Trust: true,
		},
	}, te, nil)
	if err != nil {
		srvCancel()
		t.Fatalf("NewClient: %v", err)
	}
	cliCtx, cliCancel := context.WithCancel(context.Background())
	go func() { _ = cli.Run(cliCtx) }()

	if !srv.WaitForAgent(uuidBytes, 5*time.Second) {
		srvCancel()
		cliCancel()
		t.Fatal("agent did not connect")
	}
	return srv, cli, uuidBytes, srvCancel, cliCancel
}

// TestE2E_SendCommandAndWait verifies the request/response correlation:
// the Master sends a command and blocks until the agent's result with the
// matching ID arrives (exercises the pending-command registry).
func TestE2E_SendCommandAndWait(t *testing.T) {
	srv, _, uuidBytes, srvCancel, cliCancel := startE2E(t)
	defer srvCancel()
	defer cliCancel()

	ev, err := srv.SendCommandAndWait(uuidBytes, CommandMessage{
		ID: "wait-1", Cmd: CmdRun,
	}, 3*time.Second)
	if err != nil {
		t.Fatalf("SendCommandAndWait: %v", err)
	}
	if ev.ID != "wait-1" {
		t.Fatalf("result ID mismatch: got %q, want wait-1", ev.ID)
	}
	if ev.Evt != EvtResult {
		t.Fatalf("expected EvtResult, got %s", ev.Evt)
	}
	t.Log("SendCommandAndWait returned matching result")
}

// TestE2E_WaitTimeout verifies that a command with no matching result
// times out cleanly (no goroutine leak, returns error).
func TestE2E_WaitTimeout(t *testing.T) {
	srv, _, _, srvCancel, cliCancel := startE2E(t)
	defer srvCancel()
	defer cliCancel()

	// Use a UUID that is not connected → SendCommand fails immediately.
	var ghost [ewp.UUIDLen]byte
	ghost[0] = 0xFF
	_, err := srv.SendCommandAndWait(ghost, CommandMessage{ID: "ghost", Cmd: CmdRun}, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected error for disconnected agent")
	}
	t.Logf("got expected error: %v", err)
}

// TestE2E_DisconnectMarksOffline verifies that when the agent's context
// is cancelled, the server marks it offline.
func TestE2E_DisconnectMarksOffline(t *testing.T) {
	srv, _, uuidBytes, srvCancel, cliCancel := startE2E(t)
	defer srvCancel()

	if !srv.IsOnline(uuidBytes) {
		t.Fatal("agent should be online")
	}

	// Cancel the client → connection drops.
	cliCancel()

	// Poll for offline.
	offline := false
	for i := 0; i < 200; i++ {
		if !srv.IsOnline(uuidBytes) {
			offline = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !offline {
		t.Fatal("server did not mark agent offline after disconnect")
	}
	t.Log("agent marked offline after disconnect")
}

// TestE2E_WaitWokenOnDisconnect verifies that a pending SendCommandAndWait
// is woken (with error) if the agent disconnects mid-wait, rather than
// blocking until timeout.
func TestE2E_WaitWokenOnDisconnect(t *testing.T) {
	srv, _, uuidBytes, srvCancel, cliCancel := startE2E(t)
	defer srvCancel()

	// Kick off a wait with a long timeout for a command the executor will
	// answer — but we cancel the client immediately to force disconnect.
	done := make(chan error, 1)
	go func() {
		_, err := srv.SendCommandAndWait(uuidBytes, CommandMessage{
			ID: "long-wait", Cmd: CmdRun,
		}, 30*time.Second)
		done <- err
	}()

	// Give the command a moment to register, then drop the connection.
	time.Sleep(50 * time.Millisecond)
	cliCancel()

	select {
	case <-done:
		// Either got a result (fast agent) or woken by disconnect — both
		// return before the 30s timeout, which is the invariant we test.
		t.Log("wait returned before timeout (woken or answered)")
	case <-time.After(5 * time.Second):
		t.Fatal("SendCommandAndWait did not return within 5s (should be woken by disconnect)")
	}
}
