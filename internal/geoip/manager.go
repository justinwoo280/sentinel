package geoip

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/oschwald/geoip2-golang"
)

// Manager handles the mmdb lifecycle for both the City and ASN
// databases: ensure they exist, open them for lookups, and refresh
// them daily. The ASN database is optional — if it fails to download
// or open, City lookups still work; only ASN/Organization are omitted
// (callers fall back to remote sources in that case).
type Manager struct {
	cfg       Config
	log       *slog.Logger
	mu        sync.RWMutex
	reader    *geoip2.Reader
	asnReader *geoip2.Reader
	dbPath    string
	asnDBPath string
}

// NewManager creates a Manager. If the database files exist, they're
// opened immediately. If not, an initial download is attempted for
// each (independently — a failure on one doesn't block the other).
func NewManager(cfg Config, log *slog.Logger) (*Manager, error) {
	if log == nil {
		log = slog.Default()
	}
	if cfg.DBPath == "" {
		cfg.DBPath = DefaultConfig().DBPath
	}
	if cfg.ASNDBPath == "" {
		cfg.ASNDBPath = DefaultConfig().ASNDBPath
	}
	if cfg.UpdateInterval == 0 {
		cfg.UpdateInterval = 24 * time.Hour
	}

	m := &Manager{
		cfg:       cfg,
		log:       log,
		dbPath:    cfg.DBPath,
		asnDBPath: cfg.ASNDBPath,
	}

	// City database (primary — required for location fields). Try to
	// open an existing file first regardless of cfg.Enabled (so a
	// previously-downloaded db still works if GeoIP is later disabled);
	// only attempt a fresh download when enabled.
	if err := m.open(); err != nil {
		if !cfg.Enabled {
			log.Info("geoip disabled, skipping database download")
			return m, nil
		}
		log.Info("geoip city database not found, attempting initial download")
		if err := m.downloadAndUpdate(context.Background()); err != nil {
			log.Warn("initial geoip city download failed", "err", err)
			// continue anyway; lookups will return nil
		}
	}

	// ASN database (optional — adds ASN/Organization, absent from City).
	if cfg.Enabled {
		if err := m.openASN(); err != nil {
			log.Info("geoip asn database not found, attempting initial download")
			if err := m.downloadAndUpdateASN(context.Background()); err != nil {
				log.Warn("initial geoip asn download failed", "err", err)
				// continue anyway; ASN/Org lookups will be empty and
				// callers fall back to remote sources (IPinfo/ipapi.is)
			}
		}
	} else {
		_ = m.openASN() // best-effort: use existing file if present
	}

	return m, nil
}

// open opens the City mmdb file for lookups.
func (m *Manager) open() error {
	if !fileExists(m.dbPath) {
		return fmt.Errorf("geoip: database not found at %s", m.dbPath)
	}

	reader, err := geoip2.Open(m.dbPath)
	if err != nil {
		return fmt.Errorf("geoip: open database: %w", err)
	}

	m.mu.Lock()
	oldReader := m.reader
	m.reader = reader
	m.mu.Unlock()

	if oldReader != nil {
		oldReader.Close()
	}

	m.log.Info("geoip database opened", "path", m.dbPath)
	return nil
}

// openASN opens the ASN mmdb file for lookups.
func (m *Manager) openASN() error {
	if !fileExists(m.asnDBPath) {
		return fmt.Errorf("geoip: asn database not found at %s", m.asnDBPath)
	}

	reader, err := geoip2.Open(m.asnDBPath)
	if err != nil {
		return fmt.Errorf("geoip: open asn database: %w", err)
	}

	m.mu.Lock()
	oldReader := m.asnReader
	m.asnReader = reader
	m.mu.Unlock()

	if oldReader != nil {
		oldReader.Close()
	}

	m.log.Info("geoip asn database opened", "path", m.asnDBPath)
	return nil
}

// downloadAndUpdate downloads a fresh City database and atomically
// replaces it, then reopens it.
func (m *Manager) downloadAndUpdate(ctx context.Context) error {
	result, err := Download(ctx, m.cfg)
	if err != nil {
		return err
	}
	m.log.Info("geoip database updated",
		"sha256", result.SHA256[:16], "size", result.Size)

	return m.open()
}

// downloadAndUpdateASN downloads a fresh ASN database and atomically
// replaces it, then reopens it.
func (m *Manager) downloadAndUpdateASN(ctx context.Context) error {
	result, err := DownloadASN(ctx, m.cfg)
	if err != nil {
		return err
	}
	m.log.Info("geoip asn database updated",
		"sha256", result.SHA256[:16], "size", result.Size)

	return m.openASN()
}

// StartRefreshLoop launches a background goroutine that checks for
// database updates (City and ASN) at the configured interval. Blocks
// until ctx is cancelled (should be called in a goroutine).
func (m *Manager) StartRefreshLoop(ctx context.Context) {
	if !m.cfg.Enabled {
		return
	}
	if m.cfg.UpdateInterval <= 0 {
		return
	}

	ticker := time.NewTicker(m.cfg.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.log.Debug("geoip: checking for database updates")
			if err := m.downloadAndUpdate(ctx); err != nil {
				m.log.Warn("geoip: daily city update failed", "err", err)
			}
			if err := m.downloadAndUpdateASN(ctx); err != nil {
				m.log.Warn("geoip: daily asn update failed", "err", err)
			}
		}
	}
}

// LookupResult holds the city/region/country/continent/coordinates/
// timezone/ASN info returned by a mmdb lookup. ASN/Organization are
// only populated when the GeoLite2-ASN database is available; check
// ASN != 0 before using them.
type LookupResult struct {
	CountryCode   string
	CountryName   string
	RegionCode    string
	RegionName    string
	CityName      string
	Latitude      float64
	Longitude     float64
	TimeZone      string
	Continent     string
	ContinentCode string
	PostalCode    string
	ASN           uint
	Organization  string
}

// Lookup performs a GeoIP lookup, combining the City database (location)
// with the ASN database (autonomous system number + organization) when
// both are available. Returns nil if the City database is not loaded
// or the IP is not found there; ASN/Organization are left zero-valued
// if the ASN database is unavailable or has no entry for the IP — the
// caller should fall back to a remote source in that case.
func (m *Manager) Lookup(ipStr string) *LookupResult {
	m.mu.RLock()
	reader := m.reader
	asnReader := m.asnReader
	m.mu.RUnlock()

	if reader == nil {
		return nil
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil
	}

	record, err := reader.City(ip)
	if err != nil {
		return nil
	}

	result := &LookupResult{
		CountryCode:   record.Country.IsoCode,
		CountryName:   record.Country.Names["en"],
		CityName:      record.City.Names["en"],
		Latitude:      record.Location.Latitude,
		Longitude:     record.Location.Longitude,
		TimeZone:      record.Location.TimeZone,
		PostalCode:    record.Postal.Code,
		Continent:     record.Continent.Names["en"],
		ContinentCode: record.Continent.Code,
	}

	if len(record.Subdivisions) > 0 {
		result.RegionCode = record.Subdivisions[0].IsoCode
		result.RegionName = record.Subdivisions[0].Names["en"]
	}

	if asnReader != nil {
		if asn, err := asnReader.ASN(ip); err == nil && asn.AutonomousSystemNumber != 0 {
			result.ASN = asn.AutonomousSystemNumber
			result.Organization = asn.AutonomousSystemOrganization
		}
	}

	return result
}

// IsAvailable reports whether the City database is loaded and ready.
func (m *Manager) IsAvailable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reader != nil
}

// IsASNAvailable reports whether the ASN database is loaded and ready.
func (m *Manager) IsASNAvailable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.asnReader != nil
}

// Close closes both database readers.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.reader != nil {
		m.reader.Close()
		m.reader = nil
	}
	if m.asnReader != nil {
		m.asnReader.Close()
		m.asnReader = nil
	}
	m.mu.Unlock()
}

// DBPath returns the configured City database path.
func (m *Manager) DBPath() string {
	return m.dbPath
}

// ASNDBPath returns the configured ASN database path.
func (m *Manager) ASNDBPath() string {
	return m.asnDBPath
}

// DBInfo returns City database file info (size, modtime).
func (m *Manager) DBInfo() (size int64, modTime time.Time, exists bool) {
	info, err := os.Stat(m.dbPath)
	if err != nil {
		return 0, time.Time{}, false
	}
	return info.Size(), info.ModTime(), true
}
