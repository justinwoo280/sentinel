package master

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/justinwoo280/sentinel/internal/master/store"
	ewp "github.com/justinwoo280/sing-ewp"
)

// TestStoreIntegration verifies the full store lifecycle: register,
// query, toggle, trend, delete.
func TestStoreIntegration(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// Register three agents.
	agents := []store.Node{
		{UUID: "11111111-2222-3333-4444-555555555555", ChatID: "100", NodeName: "tokyo-a1", NodeAlias: "Tokyo", Region: "JP", IP: "1.1.1.1"},
		{UUID: "22222222-3333-4444-5555-666666666666", ChatID: "100", NodeName: "us-b1", NodeAlias: "US-West", Region: "US", IP: "2.2.2.2"},
		{UUID: "33333333-4444-5555-6666-777777777777", ChatID: "200", NodeName: "hk-c1", NodeAlias: "HK", Region: "HK", IP: "3.3.3.3"},
	}
	for _, a := range agents {
		if err := st.RegisterNode(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	// Verify listAllNodes returns all 3.
	all, err := st.ListAllNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(all))
	}

	// Verify listByChat returns 2 for chat 100.
	byChat, err := st.ListNodesByChat(ctx, "100")
	if err != nil {
		t.Fatal(err)
	}
	if len(byChat) != 2 {
		t.Fatalf("expected 2 nodes for chat 100, got %d", len(byChat))
	}

	// Toggle google off on tokyo-a1.
	if err := st.ToggleModule(ctx, agents[0].UUID, "google", false); err != nil {
		t.Fatal(err)
	}
	node, err := st.GetNodeByUUID(ctx, agents[0].UUID)
	if err != nil {
		t.Fatal(err)
	}
	if node.EnableGoogle {
		t.Fatal("google should be disabled")
	}

	// Update alias.
	if err := st.UpdateAlias(ctx, agents[0].UUID, "Tokyo-Updated"); err != nil {
		t.Fatal(err)
	}
	node, _ = st.GetNodeByUUID(ctx, agents[0].UUID)
	if node.NodeAlias != "Tokyo-Updated" {
		t.Fatalf("alias: got %q, want Tokyo-Updated", node.NodeAlias)
	}

	// Insert trend data.
	for i := 0; i < 5; i++ {
		if err := st.InsertTrend(ctx, store.TrendEntry{
			NodeName:   "tokyo-a1",
			ScamScore:  i,
			GoogStatus: "OK",
			NfStatus:   "Yes",
			GptStatus:  "Yes",
		}); err != nil {
			t.Fatal(err)
		}
	}

	trends, err := st.GetTrends(ctx, "tokyo-a1", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(trends) != 3 {
		t.Fatalf("expected 3 trends, got %d", len(trends))
	}

	// Delete one agent.
	if err := st.DeleteNode(ctx, agents[2].UUID); err != nil {
		t.Fatal(err)
	}
	all, _ = st.ListAllNodes(ctx)
	if len(all) != 2 {
		t.Fatalf("expected 2 nodes after delete, got %d", len(all))
	}

	// Verify UUIDs.
	uuids, err := st.ListAllUUIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(uuids) != 2 {
		t.Fatalf("expected 2 UUIDs, got %d", len(uuids))
	}

	// TG offset persistence.
	if err := st.SetTGOffset(ctx, 42); err != nil {
		t.Fatal(err)
	}
	off, _ := st.GetTGOffset(ctx)
	if off != 42 {
		t.Fatalf("offset: got %d, want 42", off)
	}
}

// TestMasterNew verifies that master.New() initialises correctly with
// a store and ctrl server.
func TestMasterNew(t *testing.T) {
	dir := t.TempDir()

	// Generate static keypair.
	privB64, _, err := ewp.GenerateServerStaticKeypair()
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "master.key")
	if err := os.WriteFile(keyPath, []byte(privB64), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := config.MasterConfig{
		Master: config.MasterNodeConfig{Version: "test"},
		Store:  config.StoreConfig{Path: filepath.Join(dir, "test.db")},
		Control: config.ControlConfig{
			Listen:        "127.0.0.1:0",
			StaticKeyPath: keyPath,
		},
	}

	m, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.store.Close()

	// Verify store is working.
	uuids, err := m.store.ListAllUUIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(uuids) != 0 {
		t.Fatalf("expected 0 UUIDs, got %d", len(uuids))
	}

	// Register an agent.
	if err := m.store.RegisterNode(context.Background(), store.Node{
		UUID:     "11111111-2222-3333-4444-555555555555",
		ChatID:   "1",
		NodeName: "test-node",
		Region:   "JP",
	}); err != nil {
		t.Fatal(err)
	}

	// Verify it's loaded.
	uuids, _ = m.store.ListAllUUIDs(context.Background())
	if len(uuids) != 1 {
		t.Fatalf("expected 1 UUID, got %d", len(uuids))
	}
}
