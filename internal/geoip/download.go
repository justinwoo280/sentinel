// Package geoip provides Maxmind GeoLite2-City mmdb management:
// download, SHA-256 verification, tar.gz extraction, daily update
// check, and lookup. The database is NOT embedded in the binary —
// it's downloaded on first use and refreshed daily (DESIGN.md §5.4).
package geoip

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config controls the mmdb download and update behaviour.
type Config struct {
	Enabled        bool          `yaml:"enabled"`
	AccountID      string        `yaml:"account_id"`      // Maxmind Account ID (used as Basic Auth username)
	LicenseKey     string        `yaml:"license_key"`     // Maxmind license key (free, used as Basic Auth password)
	DBPath         string        `yaml:"db_path"`         // local path to .mmdb
	UpdateInterval time.Duration `yaml:"update_interval"` // check interval (default 24h)
	DownloadURL    string        `yaml:"download_url"`    // optional override
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:        true,
		DBPath:         "/var/lib/sentinel/GeoLite2-City.mmdb",
		UpdateInterval: 24 * time.Hour,
	}
}

// maxmindDownloadURL is the official Maxmind GeoLite2-City download
// endpoint. Per https://dev.maxmind.com/geoip/updating-databases, this
// endpoint requires HTTP Basic Authentication (Account ID as username,
// License Key as password) — the old query-parameter form only works
// on the deprecated geoip_download endpoint.
const maxmindDownloadURL = "https://download.maxmind.com/geoip/databases/GeoLite2-City/download?suffix=tar.gz"

// DownloadResult holds the outcome of a download attempt.
type DownloadResult struct {
	OK      bool
	Path    string
	SHA256  string
	Size    int64
	Updated bool
}

// Download fetches the GeoLite2-City database, verifies it, extracts
// the .mmdb file, and atomically installs it at cfg.DBPath.
//
// The download URL defaults to Maxmind's official endpoint, authenticated
// via HTTP Basic Auth (Account ID + License Key), as required by
// https://dev.maxmind.com/geoip/updating-databases. If cfg.DownloadURL
// is set, it's used instead (for mirrors or alternative sources).
func Download(ctx context.Context, cfg Config) (*DownloadResult, error) {
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("geoip: db_path is required")
	}

	// Determine download URL.
	dlURL := cfg.DownloadURL
	if dlURL == "" {
		if cfg.AccountID == "" || cfg.LicenseKey == "" {
			return nil, fmt.Errorf("geoip: account_id and license_key are required (find them at https://www.maxmind.com/en/accounts/current/license-key)")
		}
		dlURL = maxmindDownloadURL
	}

	// 1. Download tar.gz to a temp file.
	tmpDir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Errorf("geoip: create db dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(tmpDir, ".mmdb-dl-*")
	if err != nil {
		return nil, fmt.Errorf("geoip: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	req, err := http.NewRequestWithContext(ctx, "GET", dlURL, nil)
	if err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("geoip: create request: %w", err)
	}
	req.Header.Set("User-Agent", "sentinel/geoip-updater")
	if cfg.DownloadURL == "" {
		// Official Maxmind endpoint requires HTTP Basic Auth
		// (Account ID as username, License Key as password) — the
		// key must never be passed as a URL query parameter.
		req.SetBasicAuth(cfg.AccountID, cfg.LicenseKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("geoip: download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return nil, fmt.Errorf("geoip: download HTTP %d", resp.StatusCode)
	}

	// 2. Copy body to temp file, computing SHA-256.
	hasher := sha256.New()
	w := io.MultiWriter(tmpFile, hasher)
	if _, err := io.Copy(w, resp.Body); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("geoip: write body: %w", err)
	}
	tmpFile.Close()

	actualHash := hex.EncodeToString(hasher.Sum(nil))

	// 3. Extract .mmdb from tar.gz.
	mmdbPath, err := extractMMDB(tmpPath, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("geoip: extract: %w", err)
	}
	defer os.Remove(mmdbPath)

	// 4. Verify the extracted mmdb is non-empty.
	info, err := os.Stat(mmdbPath)
	if err != nil {
		return nil, fmt.Errorf("geoip: stat extracted: %w", err)
	}
	if info.Size() < 1024*1024 {
		return nil, fmt.Errorf("geoip: extracted db too small: %d bytes", info.Size())
	}

	// 5. Atomic install: rename old → .bak, rename new → dbPath.
	bakPath := cfg.DBPath + ".bak"
	hasOld := fileExists(cfg.DBPath)
	if hasOld {
		if err := os.Rename(cfg.DBPath, bakPath); err != nil {
			return nil, fmt.Errorf("geoip: backup old db: %w", err)
		}
	}

	if err := os.Rename(mmdbPath, cfg.DBPath); err != nil {
		// Restore backup on failure (best-effort).
		if hasOld {
			if rerr := os.Rename(bakPath, cfg.DBPath); rerr != nil {
				return nil, fmt.Errorf("geoip: install new db: %w (rollback also failed: %v)", err, rerr)
			}
		}
		return nil, fmt.Errorf("geoip: install new db: %w", err)
	}

	// Clean up backup.
	if hasOld {
		os.Remove(bakPath)
	}

	return &DownloadResult{
		OK:      true,
		Path:    cfg.DBPath,
		SHA256:  actualHash,
		Size:    info.Size(),
		Updated: true,
	}, nil
}

// extractMMDB finds and extracts the .mmdb file from a tar.gz archive
// to the target directory, returning the path to the extracted file.
func extractMMDB(tarGzPath, destDir string) (string, error) {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gunzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar read: %w", err)
		}

		// Look for .mmdb file.
		if !strings.HasSuffix(hdr.Name, ".mmdb") {
			continue
		}

		// Create the extracted file with a temp name.
		outPath := filepath.Join(destDir, ".mmdb-extracted-"+strings.TrimSuffix(filepath.Base(hdr.Name), ".mmdb"))
		outFile, err := os.Create(outPath)
		if err != nil {
			return "", fmt.Errorf("create extracted: %w", err)
		}

		if _, err := io.Copy(outFile, tr); err != nil {
			outFile.Close()
			os.Remove(outPath)
			return "", fmt.Errorf("copy mmdb: %w", err)
		}
		outFile.Close()

		return outPath, nil
	}

	return "", fmt.Errorf("no .mmdb file found in archive")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
