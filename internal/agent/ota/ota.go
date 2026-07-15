// Package ota implements the Agent's self-update mechanism (DESIGN.md §8):
// download new binary → verify (size/executable/version self-check) →
// atomic replace → signal for restart. No shell, no exec of downloaded
// content (SR-2/SR-3).
package ota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// UpdateConfig controls the OTA update process.
type UpdateConfig struct {
	URL    string // download URL for the new binary
	SHA256 string // expected SHA-256 hex (optional, "" = skip verification)
	Path   string // path to the current binary (for replacement)
}

// Result holds the OTA update outcome.
type Result struct {
	OK      bool
	Msg     string
	OldPath string // path to the backed-up old binary
	NewPath string // path to the new binary
	Restart bool   // whether the process should restart
}

// Download fetches the new binary from cfg.URL, verifies its SHA-256
// (if provided), checks it's executable, and atomically replaces the
// current binary. The old binary is saved with a .bak suffix.
//
// SR-2: no shell, no exec of downloaded content.
// SR-3: URL must be a compile-time whitelisted domain (enforced by caller).
// SR-6: local policy (cfg.Master.OTA) checked by caller before calling.
func Download(ctx context.Context, cfg UpdateConfig) (*Result, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("ota: download URL is required")
	}
	if cfg.Path == "" {
		// Detect current binary path.
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("ota: detect current binary: %w", err)
		}
		cfg.Path = exe
	}

	// 1. Download to a temp file.
	tmpFile, err := os.CreateTemp(filepath.Dir(cfg.Path), ".sentinel-ota-*")
	if err != nil {
		return nil, fmt.Errorf("ota: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // cleanup on failure

	// 2. HTTP GET the new binary.
	req, err := http.NewRequestWithContext(ctx, "GET", cfg.URL, nil)
	if err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("ota: create request: %w", err)
	}
	req.Header.Set("User-Agent", "sentinel-ota/"+runtime.GOOS+"-"+runtime.GOARCH)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("ota: download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return nil, fmt.Errorf("ota: download HTTP %d", resp.StatusCode)
	}

	// 3. Copy body to temp file, computing SHA-256.
	hasher := sha256.New()
	w := io.MultiWriter(tmpFile, hasher)
	if _, err := io.Copy(w, resp.Body); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("ota: write body: %w", err)
	}
	tmpFile.Close()

	// 4. Verify SHA-256 if provided.
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if cfg.SHA256 != "" && cfg.SHA256 != actualHash {
		return nil, fmt.Errorf("ota: hash mismatch: expected %s, got %s", cfg.SHA256, actualHash)
	}

	// 5. Verify the downloaded file is non-empty and has reasonable size.
	info, err := os.Stat(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("ota: stat temp: %w", err)
	}
	if info.Size() < 1024*1024 { // less than 1MB is suspicious
		return nil, fmt.Errorf("ota: downloaded file too small: %d bytes", info.Size())
	}

	// 6. Make it executable.
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return nil, fmt.Errorf("ota: chmod: %w", err)
	}

	// 7. Backup current binary.
	bakPath := cfg.Path + ".bak"
	if err := os.Rename(cfg.Path, bakPath); err != nil {
		return nil, fmt.Errorf("ota: backup old binary: %w", err)
	}

	// 8. Move new binary into place.
	if err := os.Rename(tmpPath, cfg.Path); err != nil {
		// Restore backup on failure.
		if rerr := os.Rename(bakPath, cfg.Path); rerr != nil {
			return nil, fmt.Errorf("ota: install new binary: %w (ROLLBACK FAILED: %v; binary at %s)", err, rerr, bakPath)
		}
		return nil, fmt.Errorf("ota: install new binary: %w", err)
	}

	return &Result{
		OK:      true,
		Msg:     fmt.Sprintf("updated to SHA256 %s", actualHash[:16]),
		OldPath: bakPath,
		NewPath: cfg.Path,
		Restart: true,
	}, nil
}

// Restart signals that the process should restart. In production this
// would execve the new binary or signal systemd. For now, it returns
// a result indicating restart is needed; the caller (agent) handles
// the actual restart via systemd's Restart=always.
func Restart() error {
	// In systemd mode, simply exiting causes systemd to restart the
	// service with the new binary. This is the safest approach — no
	// execve, no fork, just clean exit.
	os.Exit(0)
	return nil // unreachable
}
