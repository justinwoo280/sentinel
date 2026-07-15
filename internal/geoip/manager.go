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

// Manager handles the mmdb lifecycle: ensure the database exists,
// open it for lookups, and refresh it daily.
type Manager struct {
	cfg    Config
	log    *slog.Logger
	mu     sync.RWMutex
	reader *geoip2.Reader
	dbPath string
}

// NewManager creates a Manager. If the database file exists, it's
// opened immediately. If not, an initial download is attempted.
func NewManager(cfg Config, log *slog.Logger) (*Manager, error) {
	if log == nil {
		log = slog.Default()
	}
	if cfg.DBPath == "" {
		cfg.DBPath = DefaultConfig().DBPath
	}
	if cfg.UpdateInterval == 0 {
		cfg.UpdateInterval = 24 * time.Hour
	}

	m := &Manager{
		cfg:    cfg,
		log:    log,
		dbPath: cfg.DBPath,
	}

	// Try to open existing database.
	if err := m.open(); err != nil {
		if !cfg.Enabled {
			log.Info("geoip disabled, skipping database download")
			return m, nil
		}
		// Database doesn't exist — try initial download.
		log.Info("geoip database not found, attempting initial download")
		if err := m.downloadAndUpdate(context.Background()); err != nil {
			log.Warn("initial geoip download failed", "err", err)
			return m, nil // return manager anyway; lookups will return nil
		}
	}

	return m, nil
}

// open opens the mmdb file for lookups.
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

// downloadAndUpdate downloads a fresh copy and atomically replaces
// the database, then reopens it.
func (m *Manager) downloadAndUpdate(ctx context.Context) error {
	result, err := Download(ctx, m.cfg)
	if err != nil {
		return err
	}
	m.log.Info("geoip database updated",
		"sha256", result.SHA256[:16], "size", result.Size)

	// Reopen the database.
	return m.open()
}

// StartRefreshLoop launches a background goroutine that checks for
// database updates at the configured interval. Blocks until ctx is
// cancelled (should be called in a goroutine).
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
			m.log.Debug("geoip: checking for database update")
			if err := m.downloadAndUpdate(ctx); err != nil {
				m.log.Warn("geoip: daily update failed", "err", err)
			}
		}
	}
}

// LookupResult holds the city/region/country/continent/coordinates/
// timezone info returned by a mmdb lookup.
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

// Lookup performs a GeoIP city lookup. Returns nil if the database
// is not loaded or the IP is not found.
func (m *Manager) Lookup(ipStr string) *LookupResult {
	m.mu.RLock()
	reader := m.reader
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

	return result
}

// LookupASN performs a GeoIP ASN lookup (requires GeoLite2-ASN database;
// not supported with City-only database — returns nil gracefully).
func (m *Manager) LookupASN(ipStr string) *LookupResult {
	return m.Lookup(ipStr) // City db has no ASN; use separate ASN db if needed
}

// IsAvailable reports whether the database is loaded and ready.
func (m *Manager) IsAvailable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reader != nil
}

// Close closes the database reader.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.reader != nil {
		m.reader.Close()
		m.reader = nil
	}
	m.mu.Unlock()
}

// DBPath returns the configured database path.
func (m *Manager) DBPath() string {
	return m.dbPath
}

// DBInfo returns database file info (size, modtime).
func (m *Manager) DBInfo() (size int64, modTime time.Time, exists bool) {
	info, err := os.Stat(m.dbPath)
	if err != nil {
		return 0, time.Time{}, false
	}
	return info.Size(), info.ModTime(), true
}
