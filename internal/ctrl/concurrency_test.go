package ctrl

import (
	"context"
	"testing"
	"time"

	ewp "github.com/justinwoo280/sing-ewp"
)

// slowExec blocks Run (simulating a long google/trust session) but returns
// quickly for Log, so we can verify the readLoop keeps processing commands
// concurrently instead of head-of-line blocking.
type slowExec struct {
	started chan Command // pushed when a command begins executing
	release chan struct{}
}

func newSlowExec() *slowExec {
	return &slowExec{started: make(chan Command, 16), release: make(chan struct{})}
}

func (e *slowExec) Run(context.Context) Result {
	e.started <- CmdRun
	<-e.release // block until the test releases us
	return Result{OK: true}
}
func (e *slowExec) Log(context.Context) Result {
	e.started <- CmdLog
	return Result{OK: true}
}
func (e *slowExec) ModGoogle(context.Context) Result {
	e.started <- CmdModGoogle
	return Result{OK: true}
}
func (e *slowExec) ModTrust(context.Context) Result {
	e.started <- CmdModTrust
	return Result{OK: true}
}
func (e *slowExec) ModQuality(context.Context) Result {
	e.started <- CmdModQuality
	return Result{OK: true}
}
func (e *slowExec) Report(context.Context) Result { e.started <- CmdReport; return Result{OK: true} }
func (e *slowExec) ConfigRename(context.Context, string) Result {
	e.started <- CmdConfigRename
	return Result{OK: true}
}
func (e *slowExec) ConfigToggle(context.Context, string, bool) Result {
	e.started <- CmdConfigToggle
	return Result{OK: true}
}
func (e *slowExec) OTA(context.Context) Result { e.started <- CmdOTA; return Result{OK: true} }

// TestConcurrentCommandDispatch is a regression test for the head-of-line
// blocking bug: a long-running command must not prevent a later command
// from being received and executed.
func TestConcurrentCommandDispatch(t *testing.T) {
	privB64, pubB64, err := ewp.GenerateServerStaticKeypair()
	if err != nil {
		t.Fatal(err)
	}
	const testUUID = "11111111-2222-3333-4444-555555555555"
	uuidBytes, _ := ewp.ParseUUID(testUUID)

	srv, err := NewServer(ServerConfig{ListenAddr: "127.0.0.1:0", StaticPrivB64: privB64}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.AddAgent(testUUID); err != nil {
		t.Fatal(err)
	}
	srvCtx, srvCancel := context.WithCancel(context.Background())
	defer srvCancel()
	go func() { _ = srv.Listen(srvCtx) }()

	var addr string
	for i := 0; i < 200; i++ {
		if addr = srv.Addr(); addr != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}

	ex := newSlowExec()
	cli, err := NewClient(ClientConfig{
		MasterAddr:   addr,
		UUID:         testUUID,
		ServerPubB64: pubB64,
		Heartbeat:    1 * time.Second,
		MinBackoff:   100 * time.Millisecond,
		MaxBackoff:   1 * time.Second,
		Hello:        HelloData{Node: "t", Region: "JP"},
	}, ex, nil)
	if err != nil {
		t.Fatal(err)
	}
	cliCtx, cliCancel := context.WithCancel(context.Background())
	defer cliCancel()
	go func() { _ = cli.Run(cliCtx) }()

	if !srv.WaitForAgent(uuidBytes, 5*time.Second) {
		t.Fatal("agent did not connect")
	}

	// Send a slow command (Run blocks), then a fast one (Log).
	if err := srv.SendCommand(uuidBytes, CommandMessage{ID: "1", Cmd: CmdRun}); err != nil {
		t.Fatal(err)
	}
	if err := srv.SendCommand(uuidBytes, CommandMessage{ID: "2", Cmd: CmdLog}); err != nil {
		t.Fatal(err)
	}

	// Both commands must start executing even though Run is still blocked.
	got := map[Command]bool{}
	timeout := time.After(5 * time.Second)
	for len(got) < 2 {
		select {
		case cmd := <-ex.started:
			got[cmd] = true
		case <-timeout:
			t.Fatalf("head-of-line blocking: only saw %v (Log should run while Run blocks)", got)
		}
	}
	if !got[CmdRun] || !got[CmdLog] {
		t.Fatalf("expected both Run and Log to start, got %v", got)
	}
	close(ex.release) // let Run finish
}
