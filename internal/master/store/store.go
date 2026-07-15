// Package store provides the Master-side SQLite persistence layer for
// registered agents, IP trend logs, and the Telegram getUpdates offset.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Store wraps a SQLite database for the Master.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and runs migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite single-writer

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// ---------------------------------------------------------------------------
// Node CRUD
// ---------------------------------------------------------------------------

// Node represents a registered agent in the database.
type Node struct {
	UUID         string
	ChatID       string
	NodeName     string
	NodeAlias    string
	Region       string
	IP           string
	LastSeen     time.Time
	EnableGoogle bool
	EnableTrust  bool
	EnableOTA    bool
	Version      string
}

// RegisterNode inserts a new agent into the database. Returns an error if
// the (chat_id, node_name) pair or UUID already exists.
func (s *Store) RegisterNode(ctx context.Context, n Node) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO nodes (uuid, chat_id, node_name, node_alias, region,
            ip, last_seen, enable_google, enable_trust, enable_ota, version)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.UUID, n.ChatID, n.NodeName, n.NodeAlias, n.Region,
		n.IP, sql.NullTime{Time: n.LastSeen, Valid: !n.LastSeen.IsZero()},
		boolToInt(n.EnableGoogle), boolToInt(n.EnableTrust),
		boolToInt(n.EnableOTA), n.Version)
	if err != nil {
		return fmt.Errorf("store: register node: %w", err)
	}
	return nil
}

// UpdateNode updates mutable fields of a node (alias, region, IP, module
// states, version, last_seen) keyed by UUID.
func (s *Store) UpdateNode(ctx context.Context, uuid string, n Node) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET
            node_alias = ?,
            region = ?,
            ip = ?,
            last_seen = ?,
            enable_google = ?,
            enable_trust = ?,
            enable_ota = ?,
            version = ?
         WHERE uuid = ?`,
		n.NodeAlias, n.Region, n.IP,
		sql.NullTime{Time: n.LastSeen, Valid: !n.LastSeen.IsZero()},
		boolToInt(n.EnableGoogle), boolToInt(n.EnableTrust),
		boolToInt(n.EnableOTA), n.Version, uuid)
	if err != nil {
		return fmt.Errorf("store: update node: %w", err)
	}
	return nil
}

// UpdateAlias changes the alias of a node.
func (s *Store) UpdateAlias(ctx context.Context, uuid, alias string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET node_alias = ? WHERE uuid = ?`, alias, uuid)
	if err != nil {
		return fmt.Errorf("store: update alias: %w", err)
	}
	return nil
}

// ToggleModule enables or disables a module for a node.
func (s *Store) ToggleModule(ctx context.Context, uuid, mod string, enable bool) error {
	col := ""
	switch mod {
	case "google":
		col = "enable_google"
	case "trust":
		col = "enable_trust"
	default:
		return fmt.Errorf("store: unknown module %q", mod)
	}
	val := boolToInt(enable)
	_, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET `+col+` = ? WHERE uuid = ?`, val, uuid)
	if err != nil {
		return fmt.Errorf("store: toggle module: %w", err)
	}
	return nil
}

// DeleteNode removes a node by UUID.
func (s *Store) DeleteNode(ctx context.Context, uuid string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM nodes WHERE uuid = ?`, uuid)
	if err != nil {
		return fmt.Errorf("store: delete node: %w", err)
	}
	return nil
}

// GetNodeByUUID returns a node by its EWP UUID.
func (s *Store) GetNodeByUUID(ctx context.Context, uuid string) (*Node, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT uuid, chat_id, node_name, node_alias, region,
                ip, last_seen, enable_google, enable_trust, enable_ota, version
         FROM nodes WHERE uuid = ?`, uuid)
	return scanNode(row)
}

// ListNodesByChat returns all nodes owned by a Telegram chat.
func (s *Store) ListNodesByChat(ctx context.Context, chatID string) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT uuid, chat_id, node_name, node_alias, region,
                ip, last_seen, enable_google, enable_trust, enable_ota, version
         FROM nodes WHERE chat_id = ? ORDER BY node_name`, chatID)
	if err != nil {
		return nil, fmt.Errorf("store: list nodes: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// ListAllNodes returns all registered nodes.
func (s *Store) ListAllNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT uuid, chat_id, node_name, node_alias, region,
                ip, last_seen, enable_google, enable_trust, enable_ota, version
         FROM nodes ORDER BY region, node_name`)
	if err != nil {
		return nil, fmt.Errorf("store: list all nodes: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// ListAllUUIDs returns all registered agent UUIDs (for EWP whitelist
// loading at startup).
func (s *Store) ListAllUUIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT uuid FROM nodes`)
	if err != nil {
		return nil, fmt.Errorf("store: list uuids: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// IP trend log
// ---------------------------------------------------------------------------

// TrendEntry is a single IP quality trend data point.
type TrendEntry struct {
	NodeName   string
	ScamScore  int
	GoogStatus string
	NfStatus   string
	GptStatus  string
}

// InsertTrend records a new IP quality trend entry.
func (s *Store) InsertTrend(ctx context.Context, e TrendEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ip_trend_log (node_name, scam_score, goog_status, nf_status, gpt_status)
         VALUES (?, ?, ?, ?, ?)`,
		e.NodeName, e.ScamScore, e.GoogStatus, e.NfStatus, e.GptStatus)
	if err != nil {
		return fmt.Errorf("store: insert trend: %w", err)
	}
	return nil
}

// GetTrends returns the most recent N trend entries for a node.
func (s *Store) GetTrends(ctx context.Context, nodeName string, limit int) ([]TrendEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT node_name, scam_score, goog_status, nf_status, gpt_status
         FROM ip_trend_log WHERE node_name = ?
         ORDER BY check_time DESC LIMIT ?`, nodeName, limit)
	if err != nil {
		return nil, fmt.Errorf("store: get trends: %w", err)
	}
	defer rows.Close()
	var out []TrendEntry
	for rows.Next() {
		var e TrendEntry
		if err := rows.Scan(&e.NodeName, &e.ScamScore, &e.GoogStatus,
			&e.NfStatus, &e.GptStatus); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Telegram offset
// ---------------------------------------------------------------------------

// GetTGOffset returns the last Telegram getUpdates offset.
func (s *Store) GetTGOffset(ctx context.Context) (int64, error) {
	var off int64
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM tg_offset WHERE key = 'offset'`).Scan(&off)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return off, err
}

// SetTGOffset persists the Telegram getUpdates offset.
func (s *Store) SetTGOffset(ctx context.Context, offset int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tg_offset (key, value) VALUES ('offset', ?)
         ON CONFLICT(key) DO UPDATE SET value = excluded.value`, offset)
	return err
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intToBool(i int) bool { return i != 0 }

type scanner interface {
	Scan(dest ...any) error
}

func scanNode(row scanner) (*Node, error) {
	var n Node
	var lastSeen sql.NullTime
	var gGoogle, gTrust, gOTA int
	err := row.Scan(
		&n.UUID, &n.ChatID, &n.NodeName, &n.NodeAlias, &n.Region,
		&n.IP, &lastSeen, &gGoogle, &gTrust, &gOTA, &n.Version)
	if err != nil {
		return nil, err
	}
	n.LastSeen = lastSeen.Time
	n.EnableGoogle = intToBool(gGoogle)
	n.EnableTrust = intToBool(gTrust)
	n.EnableOTA = intToBool(gOTA)
	return &n, nil
}

func scanNodes(rows *sql.Rows) ([]Node, error) {
	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}
