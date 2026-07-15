package ctrl

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// Result is the outcome of executing a command on the Agent.
type Result struct {
	OK   bool            `json:"ok"`
	Msg  string          `json:"msg,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Executor is the interface the Agent implements to carry out commands
// dispatched by the Master. It is intentionally narrow and typed —
// there is no "execute arbitrary string" method (DESIGN.md SR-2).
//
// In production, the real executor starts keepalive modules, generates
// reports, etc. In fuzz tests, a recording fake implementation is
// injected to assert that no unexpected actions occur.
type Executor interface {
	Run(ctx context.Context) Result
	ModGoogle(ctx context.Context) Result
	ModTrust(ctx context.Context) Result
	ModQuality(ctx context.Context) Result
	Report(ctx context.Context) Result
	Log(ctx context.Context) Result
	ConfigRename(ctx context.Context, alias string) Result
	ConfigToggle(ctx context.Context, mod string, state bool) Result
	OTA(ctx context.Context) Result
}

// ---------------------------------------------------------------------------
// Parameter validation (DESIGN.md SR-4)
// ---------------------------------------------------------------------------

var (
	ErrAliasInvalid  = errors.New("ctrl: alias contains invalid characters or encoding")
	ErrAliasTooLong  = errors.New("ctrl: alias exceeds 20 runes")
	ErrInvalidMod    = errors.New("ctrl: mod must be 'google' or 'trust'")
	ErrStateRequired = errors.New("ctrl: state is required for config.toggle")
)

// validateAlias enforces SR-4: ≤20 runes, only [CJK / a-z / A-Z / 0-9 / -],
// valid UTF-8, no control characters.
func validateAlias(s string) error {
	if !utf8.ValidString(s) {
		return ErrAliasInvalid
	}
	count := 0
	for _, r := range s {
		count++
		if count > 20 {
			return ErrAliasTooLong
		}
		if !isAllowedAliasRune(r) {
			return ErrAliasInvalid
		}
	}
	return nil
}

func isAllowedAliasRune(r rune) bool {
	switch {
	case r == '-':
		return true
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r >= 0x4e00 && r <= 0x9fff: // CJK Unified Ideographs
		return true
	default:
		return false
	}
}

func validateToggle(p Params) error {
	if p.Mod != "google" && p.Mod != "trust" {
		return ErrInvalidMod
	}
	if p.State == nil {
		return ErrStateRequired
	}
	return nil
}

// ---------------------------------------------------------------------------
// Dispatch (parse-then-validate-then-act, DESIGN.md SR-5)
// ---------------------------------------------------------------------------

// Dispatch routes a validated CommandMessage to the appropriate Executor
// method. Parameters are validated BEFORE any Executor method is called.
// The default branch is defense-in-depth: DecodeCommand already rejects
// unknown commands, but if one somehow reaches here it is dropped.
func Dispatch(ctx context.Context, msg CommandMessage, ex Executor) EventMessage {
	switch msg.Cmd {
	case CmdRun:
		return resultEvent(msg.ID, ex.Run(ctx))
	case CmdModGoogle:
		return resultEvent(msg.ID, ex.ModGoogle(ctx))
	case CmdModTrust:
		return resultEvent(msg.ID, ex.ModTrust(ctx))
	case CmdModQuality:
		return qualityEvent(msg.ID, ex.ModQuality(ctx))
	case CmdReport:
		return reportEvent(msg.ID, ex.Report(ctx))
	case CmdLog:
		return logEvent(msg.ID, ex.Log(ctx))
	case CmdConfigRename:
		if err := validateAlias(msg.Params.Alias); err != nil {
			return errorEvent(msg.ID, err)
		}
		return resultEvent(msg.ID, ex.ConfigRename(ctx, msg.Params.Alias))
	case CmdConfigToggle:
		if err := validateToggle(msg.Params); err != nil {
			return errorEvent(msg.ID, err)
		}
		return resultEvent(msg.ID, ex.ConfigToggle(ctx, msg.Params.Mod, *msg.Params.State))
	case CmdOTA:
		return resultEvent(msg.ID, ex.OTA(ctx))
	default:
		return errorEvent(msg.ID, ErrUnknownCommand)
	}
}

func resultEvent(id string, r Result) EventMessage {
	return EventMessage{ID: id, Evt: EvtResult, Data: mustMarshal(r)}
}

func qualityEvent(id string, r Result) EventMessage {
	return EventMessage{ID: id, Evt: EvtQuality, Data: mustMarshal(r)}
}

func reportEvent(id string, r Result) EventMessage {
	return EventMessage{ID: id, Evt: EvtReport, Data: mustMarshal(r)}
}

func logEvent(id string, r Result) EventMessage {
	return EventMessage{ID: id, Evt: EvtLog, Data: mustMarshal(r)}
}

func errorEvent(id string, err error) EventMessage {
	return EventMessage{
		ID:   id,
		Evt:  EvtResult,
		Data: mustMarshal(Result{OK: false, Msg: err.Error()}),
	}
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
