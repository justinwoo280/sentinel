// Package ctrl implements the control channel between Agent and Master
// (DESIGN.md §4 + §11): an EWP/v2.1 encrypted long-lived connection
// carrying length-prefixed JSON messages.
//
// Direction of connection is REVERSED vs the original project: the
// Agent dials out to the Master, so no Agent needs a public inbound
// port. The Master is the EWP service (holds the static private key);
// each Agent is an EWP client (pins the Master static public key and
// authenticates with its own UUID as PSK).
package ctrl

import (
	"encoding/json"
	"errors"
	"fmt"
)

// MaxMessageBytes bounds a single decoded control message. Anything
// larger is rejected before parsing (DESIGN.md SR-5).
const MaxMessageBytes = 64 * 1024

// ---------------------------------------------------------------------------
// Commands (Master → Agent)
// ---------------------------------------------------------------------------

// Command is the closed enumeration of Master→Agent instructions.
// Only these exact values are dispatchable (DESIGN.md SR-1); anything
// else is dropped by the dispatcher's default branch.
type Command string

const (
	CmdRun          Command = "run"
	CmdModGoogle    Command = "mod.google"
	CmdModTrust     Command = "mod.trust"
	CmdModQuality   Command = "mod.quality"
	CmdReport       Command = "report"
	CmdLog          Command = "log"
	CmdConfigRename Command = "config.rename"
	CmdConfigToggle Command = "config.toggle"
	CmdOTA          Command = "ota"
)

var validCommands = map[Command]struct{}{
	CmdRun: {}, CmdModGoogle: {}, CmdModTrust: {}, CmdModQuality: {},
	CmdReport: {}, CmdLog: {}, CmdConfigRename: {}, CmdConfigToggle: {},
	CmdOTA: {},
}

func IsValidCommand(c Command) bool {
	_, ok := validCommands[c]
	return ok
}

// CommandMessage is a Master→Agent instruction. Params are intentionally
// a small, strongly-typed set — never a free-form map that could smuggle
// URLs or shell fragments (DESIGN.md SR-3/SR-4).
type CommandMessage struct {
	ID     string  `json:"id"`
	Cmd    Command `json:"cmd"`
	Params Params  `json:"params,omitempty"`
}

// Params holds the only parameters any command may carry. Each is
// validated against a strict allow-list before use.
type Params struct {
	Alias string `json:"alias,omitempty"` // config.rename
	Mod   string `json:"mod,omitempty"`   // config.toggle: "google"|"trust"
	State *bool  `json:"state,omitempty"` // config.toggle
}

// ---------------------------------------------------------------------------
// Events (Agent → Master)
// ---------------------------------------------------------------------------

// Event is the closed enumeration of Agent→Master reports.
type Event string

const (
	EvtHello     Event = "hello"
	EvtHeartbeat Event = "heartbeat"
	EvtResult    Event = "result"
	EvtReport    Event = "report"
	EvtQuality   Event = "quality"
	EvtLog       Event = "log"
)

var validEvents = map[Event]struct{}{
	EvtHello: {}, EvtHeartbeat: {}, EvtResult: {}, EvtReport: {},
	EvtQuality: {}, EvtLog: {},
}

func IsValidEvent(e Event) bool {
	_, ok := validEvents[e]
	return ok
}

// EventMessage is an Agent→Master report.
type EventMessage struct {
	ID   string          `json:"id,omitempty"`
	Evt  Event           `json:"evt"`
	Data json.RawMessage `json:"data,omitempty"`
}

// HelloData is sent by the Agent as the first EventMessage after the
// EWP handshake completes. It carries the node's display info so the
// Master can register it in the online table without a separate
// out-of-band registration round-trip.
type HelloData struct {
	Node    string `json:"node"`
	Alias   string `json:"alias"`
	Region  string `json:"region"`
	IP      string `json:"ip"`
	Version string `json:"version"`
	Google  bool   `json:"google"`
	Trust   bool   `json:"trust"`
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	ErrMessageTooLarge = errors.New("ctrl: message exceeds max size")
	ErrUnknownCommand  = errors.New("ctrl: unknown command")
	ErrUnknownEvent    = errors.New("ctrl: unknown event")
)

// ---------------------------------------------------------------------------
// Decoders (hardened: bounded size, DisallowUnknownFields, never panic)
// ---------------------------------------------------------------------------

func DecodeCommand(raw []byte) (CommandMessage, error) {
	var msg CommandMessage
	if len(raw) > MaxMessageBytes {
		return msg, ErrMessageTooLarge
	}
	dec := json.NewDecoder(newBoundedReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&msg); err != nil {
		return CommandMessage{}, fmt.Errorf("ctrl: decode command: %w", err)
	}
	if !IsValidCommand(msg.Cmd) {
		return CommandMessage{}, ErrUnknownCommand
	}
	return msg, nil
}

func DecodeEvent(raw []byte) (EventMessage, error) {
	var msg EventMessage
	if len(raw) > MaxMessageBytes {
		return msg, ErrMessageTooLarge
	}
	dec := json.NewDecoder(newBoundedReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&msg); err != nil {
		return EventMessage{}, fmt.Errorf("ctrl: decode event: %w", err)
	}
	if !IsValidEvent(msg.Evt) {
		return EventMessage{}, ErrUnknownEvent
	}
	return msg, nil
}
