package agentlog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// Handler is an slog.Handler that captures every log record into a
// Buffer — so the Master's `log` command can retrieve recent activity —
// while still forwarding every record to an underlying handler for
// normal output (stdout / journalctl). Without this, the ring buffer
// (Buffer) would only ever contain whatever a caller explicitly pushed
// into it, missing everything logged through the module loggers
// (google/trust/quality/scheduler/ctrl), which is why `Log` reported
// "no logs captured yet" even after those modules had run.
type Handler struct {
	next   slog.Handler
	buffer *Buffer
	module string // set via log.With("module", "..."); propagated through WithAttrs
}

// NewHandler wraps next, capturing every handled record into buffer in
// addition to forwarding it to next unchanged.
func NewHandler(next slog.Handler, buffer *Buffer) *Handler {
	return &Handler{next: next, buffer: buffer}
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	module := h.module
	var attrParts []string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "module" {
			module = a.Value.String()
			return true
		}
		attrParts = append(attrParts, fmt.Sprintf("%s=%v", a.Key, a.Value.Any()))
		return true
	})
	msg := r.Message
	if len(attrParts) > 0 {
		msg = msg + " " + strings.Join(attrParts, " ")
	}
	h.buffer.Push(r.Level.String(), module, msg)
	return h.next.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	module := h.module
	for _, a := range attrs {
		if a.Key == "module" {
			module = a.Value.String()
		}
	}
	return &Handler{next: h.next.WithAttrs(attrs), buffer: h.buffer, module: module}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name), buffer: h.buffer, module: h.module}
}
