package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestRegisterAndGetNode(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	n := Node{
		UUID:         "11111111-2222-3333-4444-555555555555",
		ChatID:       "123456",
		NodeName:     "tokyo-a1",
		NodeAlias:    "Tokyo-1",
		Region:       "JP",
		IP:           "1.2.3.4",
		LastSeen:     time.Now(),
		EnableGoogle: true,
		EnableTrust:  true,
		EnableOTA:    false,
		Version:      "dev",
	}

	if err := st.RegisterNode(ctx, n); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetNodeByUUID(ctx, n.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeName != n.NodeName {
		t.Fatalf("name: got %q, want %q", got.NodeName, n.NodeName)
	}
	if got.Region != "JP" {
		t.Fatalf("region: got %q, want JP", got.Region)
	}
	if !got.EnableGoogle {
		t.Fatal("google should be enabled")
	}
	if got.EnableOTA {
		t.Fatal("OTA should be disabled")
	}
	if got.Version != "dev" {
		t.Fatalf("version: got %q, want dev", got.Version)
	}
}

func TestDuplicateRegister(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	n := Node{
		UUID:     "11111111-2222-3333-4444-555555555555",
		ChatID:   "123",
		NodeName: "node-a",
	}

	if err := st.RegisterNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	// Same UUID should fail.
	err := st.RegisterNode(ctx, n)
	if err == nil {
		t.Fatal("expected error for duplicate UUID")
	}
}

func TestUpdateAlias(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	n := Node{
		UUID:      "11111111-2222-3333-4444-555555555555",
		ChatID:    "123",
		NodeName:  "node-a",
		NodeAlias: "Old",
	}
	st.RegisterNode(ctx, n)

	if err := st.UpdateAlias(ctx, n.UUID, "New Name"); err != nil {
		t.Fatal(err)
	}

	got, _ := st.GetNodeByUUID(ctx, n.UUID)
	if got.NodeAlias != "New Name" {
		t.Fatalf("alias: got %q, want 'New Name'", got.NodeAlias)
	}
}

func TestToggleModule(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	n := Node{
		UUID:     "11111111-2222-3333-4444-555555555555",
		ChatID:   "123",
		NodeName: "node-a",
	}
	st.RegisterNode(ctx, n)

	// Disable google.
	if err := st.ToggleModule(ctx, n.UUID, "google", false); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetNodeByUUID(ctx, n.UUID)
	if got.EnableGoogle {
		t.Fatal("google should be disabled")
	}

	// Re-enable.
	if err := st.ToggleModule(ctx, n.UUID, "google", true); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetNodeByUUID(ctx, n.UUID)
	if !got.EnableGoogle {
		t.Fatal("google should be enabled")
	}

	// Unknown module.
	err := st.ToggleModule(ctx, n.UUID, "unknown", true)
	if err == nil {
		t.Fatal("expected error for unknown module")
	}
}

func TestDeleteNode(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	n := Node{
		UUID:     "11111111-2222-3333-4444-555555555555",
		ChatID:   "123",
		NodeName: "node-a",
	}
	st.RegisterNode(ctx, n)

	if err := st.DeleteNode(ctx, n.UUID); err != nil {
		t.Fatal(err)
	}

	_, err := st.GetNodeByUUID(ctx, n.UUID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestListNodesByChat(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	for _, n := range []Node{
		{UUID: "11111111-2222-3333-4444-555555555555", ChatID: "111", NodeName: "a"},
		{UUID: "22222222-3333-4444-5555-666666666666", ChatID: "111", NodeName: "b"},
		{UUID: "33333333-4444-5555-6666-777777777777", ChatID: "222", NodeName: "c"},
	} {
		st.RegisterNode(ctx, n)
	}

	nodes, err := st.ListNodesByChat(ctx, "111")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d, want 2", len(nodes))
	}

	nodes, err = st.ListAllNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("got %d, want 3", len(nodes))
	}
}

func TestListAllUUIDs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	uuids := []string{
		"11111111-2222-3333-4444-555555555555",
		"22222222-3333-4444-5555-666666666666",
	}
	for i, u := range uuids {
		st.RegisterNode(ctx, Node{
			UUID: u, ChatID: "1", NodeName: string(rune('a' + i)),
		})
	}

	got, err := st.ListAllUUIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}

func TestTrendLog(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Register a node first (for FK if needed — but our schema has no FK).
	st.RegisterNode(ctx, Node{
		UUID:   "11111111-2222-3333-4444-555555555555",
		ChatID: "1", NodeName: "node-a",
	})

	for i := 0; i < 5; i++ {
		if err := st.InsertTrend(ctx, TrendEntry{
			NodeName:   "node-a",
			ScamScore:  i * 10,
			GoogStatus: "OK",
			NfStatus:   "Yes",
			GptStatus:  "Yes",
		}); err != nil {
			t.Fatal(err)
		}
	}

	trends, err := st.GetTrends(ctx, "node-a", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(trends) != 3 {
		t.Fatalf("got %d, want 3", len(trends))
	}
	// Most recent first (highest score since all inserted in sequence).
	if trends[0].ScamScore != 40 {
		t.Fatalf("latest score: got %d, want 40", trends[0].ScamScore)
	}
}

func TestTGOffset(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	off, err := st.GetTGOffset(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if off != 0 {
		t.Fatalf("initial offset: got %d, want 0", off)
	}

	if err := st.SetTGOffset(ctx, 12345); err != nil {
		t.Fatal(err)
	}

	off, _ = st.GetTGOffset(ctx)
	if off != 12345 {
		t.Fatalf("offset: got %d, want 12345", off)
	}

	// Update again.
	st.SetTGOffset(ctx, 99999)
	off, _ = st.GetTGOffset(ctx)
	if off != 99999 {
		t.Fatalf("offset: got %d, want 99999", off)
	}
}
