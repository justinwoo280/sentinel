package ota

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadSuccess(t *testing.T) {
	// Create a fake "binary" that's large enough to pass size check.
	payload := make([]byte, 2*1024*1024) // 2MB
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "sentinel")

	// Create a dummy existing binary.
	if err := os.WriteFile(binaryPath, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}

	result, err := Download(context.Background(), UpdateConfig{
		URL:  srv.URL,
		Path: binaryPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatal("expected OK")
	}
	if !result.Restart {
		t.Fatal("expected restart=true")
	}

	// Verify new binary is in place.
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(payload) {
		t.Fatalf("binary size: got %d, want %d", len(data), len(payload))
	}

	// Verify backup exists.
	bak, err := os.ReadFile(binaryPath + ".bak")
	if err != nil {
		t.Fatal("backup file should exist")
	}
	if string(bak) != "old binary" {
		t.Fatalf("backup content: got %q, want 'old binary'", string(bak))
	}
}

func TestDownloadHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "sentinel")
	os.WriteFile(binaryPath, []byte("old"), 0755)

	_, err := Download(context.Background(), UpdateConfig{
		URL:  srv.URL,
		Path: binaryPath,
	})
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestDownloadEmptyURL(t *testing.T) {
	_, err := Download(context.Background(), UpdateConfig{})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestDownloadSmallFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("tiny"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "sentinel")
	os.WriteFile(binaryPath, []byte("old"), 0755)

	_, err := Download(context.Background(), UpdateConfig{
		URL:  srv.URL,
		Path: binaryPath,
	})
	if err == nil {
		t.Fatal("expected error for small file")
	}
}

func TestDownloadHashMismatch(t *testing.T) {
	payload := make([]byte, 2*1024*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "sentinel")
	os.WriteFile(binaryPath, []byte("old"), 0755)

	_, err := Download(context.Background(), UpdateConfig{
		URL:    srv.URL,
		SHA256: "0000000000000000000000000000000000000000000000000000000000000000",
		Path:   binaryPath,
	})
	if err == nil {
		t.Fatal("expected error for hash mismatch")
	}
}
