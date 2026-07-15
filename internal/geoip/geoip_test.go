package geoip

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// createFakeMMDBTarGz creates a tar.gz containing a fake .mmdb file
// to test the extraction logic.
func createFakeMMDBTarGz(t *testing.T, path string, mmdbContent []byte) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	hdr := &tar.Header{
		Name: "GeoLite2-City_20240101/GeoLite2-City.mmdb",
		Mode: 0644,
		Size: int64(len(mmdbContent)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(mmdbContent); err != nil {
		t.Fatal(err)
	}
}

func TestExtractMMDB(t *testing.T) {
	dir := t.TempDir()

	// Create a tar.gz with a fake mmdb.
	mmdbContent := make([]byte, 2*1024*1024) // 2MB fake db
	tarGzPath := filepath.Join(dir, "test.tar.gz")
	createFakeMMDBTarGz(t, tarGzPath, mmdbContent)

	// Extract.
	extractedPath, err := extractMMDB(tarGzPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(extractedPath)

	// Verify extracted file.
	data, err := os.ReadFile(extractedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(mmdbContent) {
		t.Fatalf("extracted size: got %d, want %d", len(data), len(mmdbContent))
	}
}

func TestDownloadSuccess(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "GeoLite2-City.mmdb")

	// Create a fake tar.gz with mmdb payload.
	mmdbContent := make([]byte, 2*1024*1024)
	tarGzBuf := newTarGz(t, mmdbContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarGzBuf)
	}))
	defer srv.Close()

	result, err := Download(context.Background(), Config{
		DBPath:      dbPath,
		DownloadURL: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatal("expected OK")
	}
	if !result.Updated {
		t.Fatal("expected updated=true")
	}
	if result.Size < 1024*1024 {
		t.Fatalf("size too small: %d", result.Size)
	}

	// Verify file exists.
	if !fileExists(dbPath) {
		t.Fatal("database file should exist")
	}
}

func TestDownloadASNSuccess(t *testing.T) {
	dir := t.TempDir()
	asnDBPath := filepath.Join(dir, "GeoLite2-ASN.mmdb")

	mmdbContent := make([]byte, 2*1024*1024)
	tarGzBuf := newTarGz(t, mmdbContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarGzBuf)
	}))
	defer srv.Close()

	result, err := DownloadASN(context.Background(), Config{
		ASNDBPath:   asnDBPath,
		DownloadURL: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatal("expected OK")
	}
	if result.Path != asnDBPath {
		t.Fatalf("path: got %q, want %q", result.Path, asnDBPath)
	}
	if !fileExists(asnDBPath) {
		t.Fatal("asn database file should exist")
	}
}

func TestDownloadASNMissingCredentials(t *testing.T) {
	_, err := DownloadASN(context.Background(), Config{
		ASNDBPath: filepath.Join(t.TempDir(), "GeoLite2-ASN.mmdb"),
		// No account_id/license_key, no download URL.
	})
	if err == nil {
		t.Fatal("expected error for missing account_id/license_key")
	}
}

func TestDownloadHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := Download(context.Background(), Config{
		DBPath:      filepath.Join(t.TempDir(), "test.mmdb"),
		DownloadURL: srv.URL,
	})
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestDownloadMissingLicenseKey(t *testing.T) {
	_, err := Download(context.Background(), Config{
		DBPath: filepath.Join(t.TempDir(), "test.mmdb"),
		// No license key, no download URL.
	})
	if err == nil {
		t.Fatal("expected error for missing license key")
	}
}

func TestDownloadEmptyDBPath(t *testing.T) {
	_, err := Download(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected error for empty db_path")
	}
}

func TestManagerNewNoDB(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Enabled: false, // don't attempt download
		DBPath:  filepath.Join(dir, "nonexistent.mmdb"),
	}
	m, err := NewManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.IsAvailable() {
		t.Fatal("should not be available without db")
	}
	m.Close()
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test")
	os.WriteFile(path, []byte("test"), 0644)

	if !fileExists(path) {
		t.Fatal("fileExists should return true")
	}
	if fileExists(filepath.Join(dir, "nonexistent")) {
		t.Fatal("fileExists should return false")
	}
}

// newTarGz creates a tar.gz containing a fake .mmdb file in memory.
func newTarGz(t *testing.T, mmdbContent []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: "GeoLite2-City_20240101/GeoLite2-City.mmdb",
		Mode: 0644,
		Size: int64(len(mmdbContent)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	tw.Write(mmdbContent)
	tw.Close()
	gw.Close()
	return buf.Bytes()
}
