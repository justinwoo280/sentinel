// Package geoip provides Maxmind GeoLite2 mmdb management: download,
// SHA-256 verification, tar.gz extraction, daily update check, and
// lookup, for both the City and ASN editions. Databases are NOT
// embedded in the binary — they're downloaded on first use and
// refreshed daily (DESIGN.md §5.4).
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
	DBPath         string        `yaml:"db_path"`         // local path to the GeoLite2-City .mmdb
	ASNDBPath      string        `yaml:"asn_db_path"`     // local path to the GeoLite2-ASN .mmdb
	UpdateInterval time.Duration `yaml:"update_interval"` // check interval (default 24h)
	DownloadURL    string        `yaml:"download_url"`    // optional override (edition-agnostic, for mirrors/tests)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:        true,
		DBPath:         "/var/lib/sentinel/GeoLite2-City.mmdb",
		ASNDBPath:      "/var/lib/sentinel/GeoLite2-ASN.mmdb",
		UpdateInterval: 24 * time.Hour,
	}
}

// maxmindDownloadURLTemplate is the official Maxmind GeoLite2 download
// endpoint, parameterized by edition ID (GeoLite2-City, GeoLite2-ASN).
// Per https://dev.maxmind.com/geoip/updating-databases, this endpoint
// requires HTTP Basic Authentication (Account ID as username, License
// Key as password) — the old query-parameter form only works on the
// deprecated geoip_download endpoint.
const maxmindDownloadURLTemplate = "https://download.maxmind.com/geoip/databases/%s/download?suffix=tar.gz"

// editionCity and editionASN are the Maxmind database edition IDs we
// download. GeoLite2-City already includes country/region/city/coords/
// timezone; GeoLite2-ASN adds the autonomous system number and
// organization that City lacks (DESIGN.md §5.4 — avoids depending on
// remote IPinfo/ipapi.is calls for ASN/Org whenever possible).
const (
	editionCity = "GeoLite2-City"
	editionASN  = "GeoLite2-ASN"
)

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
	return downloadEdition(ctx, cfg, editionCity, cfg.DBPath)
}

// DownloadASN fetches the GeoLite2-ASN database (autonomous system
// number + organization — not included in GeoLite2-City) and
// atomically installs it at cfg.ASNDBPath. Same auth/verification
// process as Download.
func DownloadASN(ctx context.Context, cfg Config) (*DownloadResult, error) {
	return downloadEdition(ctx, cfg, editionASN, cfg.ASNDBPath)
}

// downloadEdition fetches the given Maxmind edition, verifies it,
// extracts the .mmdb file, and atomically installs it at destPath.
func downloadEdition(ctx context.Context, cfg Config, editionID, destPath string) (*DownloadResult, error) {
	if destPath == "" {
		return nil, fmt.Errorf("geoip: db_path is required")
	}

	// Determine download URL.
	dlURL := cfg.DownloadURL
	if dlURL == "" {
		if cfg.AccountID == "" || cfg.LicenseKey == "" {
			return nil, fmt.Errorf("geoip: account_id and license_key are required (find them at https://www.maxmind.com/en/accounts/current/license-key)")
		}
		dlURL = fmt.Sprintf(maxmindDownloadURLTemplate, editionID)
	}

	// 1. Download tar.gz to a temp file.
	tmpDir := filepath.Dir(destPath)
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

	// 5. Atomic install: rename old → .bak, rename new → destPath.
	bakPath := destPath + ".bak"
	hasOld := fileExists(destPath)
	if hasOld {
		if err := os.Rename(destPath, bakPath); err != nil {
			return nil, fmt.Errorf("geoip: backup old db: %w", err)
		}
	}

	if err := os.Rename(mmdbPath, destPath); err != nil {
		// Restore backup on failure (best-effort).
		if hasOld {
			if rerr := os.Rename(bakPath, destPath); rerr != nil {
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
		Path:    destPath,
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
