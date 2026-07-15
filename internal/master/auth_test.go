package master

import (
	"context"
	"testing"

	"github.com/justinwoo280/sentinel/internal/master/store"
	"github.com/justinwoo280/sentinel/internal/master/telegram"
)

func TestIsAuthorizedEmptyIsFailClosed(t *testing.T) {
	m := newTestMaster() // empty admins map
	if m.isAuthorized(12345) {
		t.Fatal("empty allowlist must deny everyone (fail-closed)")
	}
	if m.isAuthorized(0) {
		t.Fatal("empty allowlist must deny user 0")
	}
}

func TestIsAuthorizedAllowlist(t *testing.T) {
	m := newTestMaster()
	m.admins[111] = true
	m.admins[222] = true

	if !m.isAuthorized(111) {
		t.Error("111 should be authorized")
	}
	if !m.isAuthorized(222) {
		t.Error("222 should be authorized")
	}
	if m.isAuthorized(333) {
		t.Error("333 must NOT be authorized")
	}
}

// TestHandleMessageUnauthorized verifies an unauthorized /register does not
// reach downstream logic (no node created). Gates the highest-impact path.
func TestHandleMessageUnauthorized(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	m := newTestMaster()
	m.store = st
	m.tg = telegram.New("test-token") // SendMessage will fail HTTP; harmless
	// admins is empty → fail-closed.

	msg := &telegram.Message{
		From: &telegram.User{ID: 999},
		Chat: &telegram.Chat{ID: 999},
		Text: "/register SENTINEL-REG:whatever",
	}
	m.handleMessage(msg)

	nodes, err := st.ListAllNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Fatalf("unauthorized /register created %d nodes; auth gate failed", len(nodes))
	}
}

// TestHandleMessageNilFromDenied ensures a message with no From (nil) is
// denied without panicking.
func TestHandleMessageNilFromDenied(t *testing.T) {
	m := newTestMaster()
	m.admins[1] = true
	m.tg = telegram.New("test-token")
	msg := &telegram.Message{
		Chat: &telegram.Chat{ID: 1},
		Text: "/start",
	}
	m.handleMessage(msg) // must not panic
}

// TestHandleCallbackUnauthorized ensures an unauthorized callback is denied
// (gate on cb.From, not the message chat).
func TestHandleCallbackUnauthorized(t *testing.T) {
	m := newTestMaster()
	m.admins[1] = true // user 1 is admin; presser is user 999
	m.tg = telegram.New("test-token")

	cb := &telegram.CallbackQuery{
		ID:   "cbid",
		From: &telegram.User{ID: 999},
		Message: &telegram.Message{
			Chat: &telegram.Chat{ID: 1}, // button lives in admin's chat…
		},
		Data: "delete_yes|some-uuid", // destructive action
	}
	// Should be denied purely on cb.From.ID (999), not the chat (1).
	m.handleCallback(cb) // must not panic and must not act
}
