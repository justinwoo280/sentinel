package ctrl

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeExecutor records all calls and returns canned OK results.
type fakeExecutor struct {
	calls []string
}

func (f *fakeExecutor) Run(context.Context) Result {
	f.calls = append(f.calls, "run")
	return Result{OK: true}
}
func (f *fakeExecutor) ModGoogle(context.Context) Result {
	f.calls = append(f.calls, "google")
	return Result{OK: true}
}
func (f *fakeExecutor) ModTrust(context.Context) Result {
	f.calls = append(f.calls, "trust")
	return Result{OK: true}
}
func (f *fakeExecutor) ModQuality(context.Context) Result {
	f.calls = append(f.calls, "quality")
	return Result{OK: true}
}
func (f *fakeExecutor) Report(context.Context) Result {
	f.calls = append(f.calls, "report")
	return Result{OK: true}
}
func (f *fakeExecutor) Log(context.Context) Result {
	f.calls = append(f.calls, "log")
	return Result{OK: true}
}
func (f *fakeExecutor) ConfigRename(_ context.Context, a string) Result {
	f.calls = append(f.calls, "rename:"+a)
	return Result{OK: true}
}
func (f *fakeExecutor) ConfigToggle(_ context.Context, m string, _ bool) Result {
	f.calls = append(f.calls, "toggle:"+m)
	return Result{OK: true}
}
func (f *fakeExecutor) OTA(context.Context) Result {
	f.calls = append(f.calls, "ota")
	return Result{OK: true}
}

func TestDispatch_ValidCommands(t *testing.T) {
	tests := []struct {
		cmd     Command
		want    string
		wantEvt Event
	}{
		{CmdRun, "run", EvtResult},
		{CmdModGoogle, "google", EvtResult},
		{CmdModTrust, "trust", EvtResult},
		{CmdModQuality, "quality", EvtQuality},
		{CmdReport, "report", EvtReport},
		{CmdLog, "log", EvtLog},
		{CmdOTA, "ota", EvtResult},
	}
	for _, tt := range tests {
		fe := &fakeExecutor{}
		ev := Dispatch(context.Background(), CommandMessage{ID: "1", Cmd: tt.cmd}, fe)
		if ev.Evt != tt.wantEvt {
			t.Errorf("%s: expected %s, got %s", tt.cmd, tt.wantEvt, ev.Evt)
		}
		if len(fe.calls) != 1 || fe.calls[0] != tt.want {
			t.Errorf("%s: executor got %v, want [%s]", tt.cmd, fe.calls, tt.want)
		}
		var res Result
		if err := json.Unmarshal(ev.Data, &res); err != nil {
			t.Errorf("%s: result not valid JSON: %v", tt.cmd, err)
		}
		if !res.OK {
			t.Errorf("%s: expected OK result", tt.cmd)
		}
	}
}

func TestDispatch_ConfigRename_Validation(t *testing.T) {
	// Valid.
	fe := &fakeExecutor{}
	ev := Dispatch(context.Background(), CommandMessage{
		ID: "1", Cmd: CmdConfigRename, Params: Params{Alias: "tokyo-1"},
	}, fe)
	if len(fe.calls) != 1 {
		t.Fatalf("valid alias should reach executor, got calls=%v", fe.calls)
	}
	var res Result
	json.Unmarshal(ev.Data, &res)
	if !res.OK {
		t.Fatal("valid alias rejected")
	}

	// Too long.
	fe2 := &fakeExecutor{}
	ev2 := Dispatch(context.Background(), CommandMessage{
		Cmd: CmdConfigRename, Params: Params{Alias: strings.Repeat("a", 30)},
	}, fe2)
	if len(fe2.calls) != 0 {
		t.Fatal("too-long alias should not reach executor")
	}
	json.Unmarshal(ev2.Data, &res)
	if res.OK {
		t.Fatal("too-long alias should return error")
	}

	// Shell metacharacters.
	fe3 := &fakeExecutor{}
	ev3 := Dispatch(context.Background(), CommandMessage{
		Cmd: CmdConfigRename, Params: Params{Alias: "'; rm -rf /"},
	}, fe3)
	if len(fe3.calls) != 0 {
		t.Fatal("bad-char alias should not reach executor")
	}
	json.Unmarshal(ev3.Data, &res)
	if res.OK {
		t.Fatal("bad-char alias should return error")
	}

	// CJK allowed.
	fe4 := &fakeExecutor{}
	ev4 := Dispatch(context.Background(), CommandMessage{
		Cmd: CmdConfigRename, Params: Params{Alias: "東京-1"},
	}, fe4)
	if len(fe4.calls) != 1 {
		t.Fatal("CJK alias should reach executor")
	}
	json.Unmarshal(ev4.Data, &res)
	if !res.OK {
		t.Fatal("CJK alias rejected")
	}
}

func TestDispatch_ConfigToggle_Validation(t *testing.T) {
	state := true
	// Valid.
	fe := &fakeExecutor{}
	ev := Dispatch(context.Background(), CommandMessage{
		Cmd: CmdConfigToggle, Params: Params{Mod: "google", State: &state},
	}, fe)
	if len(fe.calls) != 1 {
		t.Fatalf("valid toggle should reach executor, got calls=%v", fe.calls)
	}
	var res Result
	json.Unmarshal(ev.Data, &res)
	if !res.OK {
		t.Fatal("valid toggle rejected")
	}

	// Invalid mod.
	fe2 := &fakeExecutor{}
	ev2 := Dispatch(context.Background(), CommandMessage{
		Cmd: CmdConfigToggle, Params: Params{Mod: "evil", State: &state},
	}, fe2)
	if len(fe2.calls) != 0 {
		t.Fatal("invalid mod should not reach executor")
	}
	json.Unmarshal(ev2.Data, &res)
	if res.OK {
		t.Fatal("invalid mod should return error")
	}

	// Missing state.
	fe3 := &fakeExecutor{}
	ev3 := Dispatch(context.Background(), CommandMessage{
		Cmd: CmdConfigToggle, Params: Params{Mod: "google"},
	}, fe3)
	if len(fe3.calls) != 0 {
		t.Fatal("missing state should not reach executor")
	}
	json.Unmarshal(ev3.Data, &res)
	if res.OK {
		t.Fatal("missing state should return error")
	}
}

func TestDispatch_UnknownCommand(t *testing.T) {
	fe := &fakeExecutor{}
	ev := Dispatch(context.Background(), CommandMessage{Cmd: "rm-rf"}, fe)
	if len(fe.calls) != 0 {
		t.Fatal("unknown command should not reach executor")
	}
	var res Result
	json.Unmarshal(ev.Data, &res)
	if res.OK {
		t.Fatal("unknown command should return error")
	}
}

func TestDecodeEvent(t *testing.T) {
	// Valid.
	_, err := DecodeEvent([]byte(`{"evt":"heartbeat"}`))
	if err != nil {
		t.Fatalf("valid heartbeat rejected: %v", err)
	}
	// Unknown event.
	_, err = DecodeEvent([]byte(`{"evt":"evil"}`))
	if err == nil {
		t.Fatal("unknown event accepted")
	}
	// Unknown field.
	_, err = DecodeEvent([]byte(`{"evt":"heartbeat","evil":1}`))
	if err == nil {
		t.Fatal("unknown field accepted")
	}
}

// FuzzDispatchCommand asserts (DESIGN.md §12): on ANY input, the
// Decode→Dispatch path never panics, and when DecodeCommand succeeds,
// Dispatch always produces a valid EvtResult with parseable JSON.
func FuzzDispatchCommand(f *testing.F) {
	f.Add([]byte(`{"id":"1","cmd":"run"}`))
	f.Add([]byte(`{"id":"2","cmd":"config.toggle","params":{"mod":"google","state":true}}`))
	f.Add([]byte(`{"id":"3","cmd":"config.rename","params":{"alias":"東京"}}`))
	f.Add([]byte(`{"cmd":"evil"}`))
	f.Add([]byte(`{"cmd":"config.toggle","params":{"mod":"evil","state":true}}`))
	f.Add([]byte(`{"cmd":"config.rename","params":{"alias":"` + strings.Repeat("a", 50) + `"}}`))
	f.Add([]byte(`{"cmd":123}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte(``))
	f.Add([]byte(`{"cmd":"config.toggle","params":{"mod":"google"}}`))
	f.Add([]byte(`{"cmd":"config.rename","params":{"alias":"'; rm -rf /"}}`))

	f.Fuzz(func(t *testing.T, raw []byte) {
		msg, err := DecodeCommand(raw)
		if err != nil {
			return // clean rejection is the acceptable outcome
		}
		fe := &fakeExecutor{}
		ev := Dispatch(context.Background(), msg, fe)
		if !IsValidEvent(ev.Evt) {
			t.Fatalf("Dispatch returned invalid event type %q", ev.Evt)
		}
		var res Result
		if err := json.Unmarshal(ev.Data, &res); err != nil {
			t.Fatalf("result data not valid JSON: %v (data=%s)", err, ev.Data)
		}
	})
}
