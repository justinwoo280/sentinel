-- Master SQLite schema (executed on first open via embedded SQL)

PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS nodes (
    uuid         TEXT NOT NULL UNIQUE,   -- EWP PSK identity (v4 UUID)
    chat_id      TEXT NOT NULL,          -- Telegram chat that owns this node
    node_name    TEXT NOT NULL,          -- immutable primary identifier
    node_alias   TEXT DEFAULT '',        -- display name (remotely changeable)
    region       TEXT DEFAULT 'UNKNOWN',
    ip           TEXT DEFAULT '',        -- last reported public IP
    last_seen    DATETIME,
    enable_google INTEGER DEFAULT 1,
    enable_trust  INTEGER DEFAULT 1,
    enable_ota    INTEGER DEFAULT 0,
    version       TEXT DEFAULT '',
    PRIMARY KEY (chat_id, node_name)
);

CREATE TABLE IF NOT EXISTS ip_trend_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    node_name   TEXT NOT NULL,
    check_time  DATETIME DEFAULT CURRENT_TIMESTAMP,
    scam_score  INTEGER,
    goog_status TEXT,
    nf_status   TEXT,
    gpt_status  TEXT
);

CREATE INDEX IF NOT EXISTS idx_trend_node ON ip_trend_log(node_name, check_time);

CREATE TABLE IF NOT EXISTS tg_offset (
    key   TEXT PRIMARY KEY DEFAULT 'offset',
    value INTEGER DEFAULT 0
);
