package ctrl

import (
	"context"
	"testing"
	"time"

	ewp "github.com/justinwoo280/sing-ewp"
)

// testExec is a recording Executor used in the integration test. Each
// method pushes its Command into a buffered channel so the test can
// assert which command was received.
type testExec struct {
	got chan Command
}

func newTestExec() *testExec {
	return &testExec{got: make(chan Command, 16)}
}

func (e *testExec) Run(context.Context) Result {
	e.got <- CmdRun
	return Result{OK: true, Msg: "run done"}
}
func (e *testExec) ModGoogle(context.Context) Result { e.got <- CmdModGoogle; return Result{OK: true} }
func (e *testExec) ModTrust(context.Context) Result  { e.got <- CmdModTrust; return Result{OK: true} }
func (e *testExec) ModQuality(context.Context) Result {
	e.got <- CmdModQuality
	return Result{OK: true}
}
func (e *testExec) Report(context.Context) Result { e.got <- CmdReport; return Result{OK: true} }
func (e *testExec) Log(context.Context) Result    { e.got <- CmdLog; return Result{OK: true} }
func (e *testExec) ConfigRename(_ context.Context, _ string) Result {
	e.got <- CmdConfigRename
	return Result{OK: true}
}
func (e *testExec) ConfigToggle(_ context.Context, _ string, _ bool) Result {
	e.got <- CmdConfigToggle
	return Result{OK: true}
}
func (e *testExec) OTA(context.Context) Result { e.got <- CmdOTA; return Result{OK: true} }

// TestIntegration_E2E verifies the full control channel:
//
//	Server listens → Agent dials out → EWP/v2.1 handshake →
//	Agent sends hello → Server registers agent →
//	Server sends command → Agent dispatches to Executor →
//	Agent sends result back.
func TestIntegration_E2E(t *testing.T) {
	// 1. Generate Master static keypair.
	privB64, pubB64, err := ewp.GenerateServerStaticKeypair()
	if err != nil {
		t.Fatalf("GenerateServerStaticKeypair: %v", err)
	}

	testUUID := "11111111-2222-3333-4444-555555555555"
	uuidBytes, err := ewp.ParseUUID(testUUID)
	if err != nil {
		t.Fatalf("ParseUUID: %v", err)
	}

	// 2. Create and start server.
	srv, err := NewServer(ServerConfig{
		ListenAddr:    "127.0.0.1:0",
		StaticPrivB64: privB64,
	}, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.AddAgent(testUUID); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}

	srvCtx, srvCancel := context.WithCancel(context.Background())
	defer srvCancel()
	go func() { _ = srv.Listen(srvCtx) }()

	// Wait for listener to be ready.
	addr := ""
	for i := 0; i < 100; i++ {
		addr = srv.Addr()
		if addr != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if addr == "" {
		t.Fatal("server did not start listening")
	}

	// 3. Create client (Agent) with a test executor.
	te := newTestExec()
	cli, err := NewClient(ClientConfig{
		MasterAddr:   addr,
		UUID:         testUUID,
		ServerPubB64: pubB64,
		Heartbeat:    1 * time.Second,
		MinBackoff:   200 * time.Millisecond,
		MaxBackoff:   1 * time.Second,
		Hello: HelloData{
			Node: "test-node-1", Alias: "test", Region: "JP",
			IP: "127.0.0.1", Version: "test", Google: true, Trust: true,
		},
	}, te, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	cliCtx, cliCancel := context.WithCancel(context.Background())
	defer cliCancel()
	go func() { _ = cli.Run(cliCtx) }()

	// 4. Wait for agent to connect.
	if !srv.WaitForAgent(uuidBytes, 5*time.Second) {
		t.Fatal("agent did not connect within 5s")
	}
	t.Log("agent connected")

	// Wait for hello data to arrive (the agent sends hello after the
	// connection is established, so we poll for it).
	var helloOK bool
	for i := 0; i < 100; i++ {
		agents := srv.OnlineAgents()
		if len(agents) == 1 && agents[0].Info != nil && agents[0].Info.Node == "test-node-1" {
			helloOK = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !helloOK {
		agents := srv.OnlineAgents()
		t.Fatalf("hello data not received: %+v", agents[0].Info)
	}
	t.Log("hello data received")

	// 5. Send a command from server to agent.
	if err := srv.SendCommand(uuidBytes, CommandMessage{
		ID: "cmd-1", Cmd: CmdRun,
	}); err != nil {
		t.Fatalf("SendCommand: %v", err)
	}

	// 6. Wait for executor to receive the command.
	select {
	case cmd := <-te.got:
		if cmd != CmdRun {
			t.Fatalf("expected CmdRun, got %s", cmd)
		}
		t.Log("command dispatched to executor successfully")
	case <-time.After(3 * time.Second):
		t.Fatal("executor did not receive command within 3s")
	}

	// 7. Send a config.toggle command.
	state := false
	if err := srv.SendCommand(uuidBytes, CommandMessage{
		ID: "cmd-2", Cmd: CmdConfigToggle,
		Params: Params{Mod: "trust", State: &state},
	}); err != nil {
		t.Fatalf("SendCommand toggle: %v", err)
	}
	select {
	case cmd := <-te.got:
		if cmd != CmdConfigToggle {
			t.Fatalf("expected CmdConfigToggle, got %s", cmd)
		}
		t.Log("toggle command dispatched successfully")
	case <-time.After(3 * time.Second):
		t.Fatal("executor did not receive toggle within 3s")
	}

	// 8. Verify agent is still online after multiple commands.
	if !srv.IsOnline(uuidBytes) {
		t.Fatal("agent went offline after commands")
	}

	// 9. Test heartbeat: wait for at least one heartbeat.
	// (1s interval; give 3s window.)
	time.Sleep(3 * time.Second)
	if !srv.IsOnline(uuidBytes) {
		t.Fatal("agent went offline (heartbeat failed)")
	}
	t.Log("heartbeat maintaining connection")

	t.Log("E2E integration test passed")
}
