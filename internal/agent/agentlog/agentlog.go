// Package agentlog provides a bounded ring buffer for capturing recent
// log lines, queryable by the Master via the `log` command.
package agentlog

import (
	"strings"
	"sync"
	"time"
)

// Entry is a single log line with timestamp.
type Entry struct {
	Time   time.Time
	Level  string
	Module string
	Msg    string
}

// Buffer is a thread-safe ring buffer of log entries.
type Buffer struct {
	mu      sync.Mutex
	entries []Entry
	next    int // write cursor
	size    int // capacity
	count   int // actual count (up to size)
}

// New creates a ring buffer with the given capacity.
func New(size int) *Buffer {
	if size <= 0 {
		size = 500
	}
	return &Buffer{
		entries: make([]Entry, size),
		size:    size,
	}
}

// Push appends a log entry, evicting the oldest if full.
func (b *Buffer) Push(level, module, msg string) {
	b.mu.Lock()
	b.entries[b.next] = Entry{
		Time:   time.Now().UTC(),
		Level:  level,
		Module: module,
		Msg:    msg,
	}
	b.next = (b.next + 1) % b.size
	if b.count < b.size {
		b.count++
	}
	b.mu.Unlock()
}

// Recent returns the most recent n entries (oldest first).
func (b *Buffer) Recent(n int) []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()

	if n <= 0 || n > b.count {
		n = b.count
	}
	out := make([]Entry, n)
	start := (b.next - n + b.size) % b.size
	for i := 0; i < n; i++ {
		out[i] = b.entries[(start+i)%b.size]
	}
	return out
}

// All returns all stored entries (oldest first).
func (b *Buffer) All() []Entry {
	return b.Recent(0)
}

// Format renders the last n entries as a plain-text string.
func (b *Buffer) Format(n int) string {
	entries := b.Recent(n)
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(e.Time.Format("2006-01-02 15:04:05"))
		sb.WriteString(" [")
		sb.WriteString(e.Level)
		sb.WriteString("]")
		if e.Module != "" {
			sb.WriteString(" [")
			sb.WriteString(e.Module)
			sb.WriteString("]")
		}
		sb.WriteString(" ")
		sb.WriteString(e.Msg)
		sb.WriteString("\n")
	}
	return sb.String()
}
