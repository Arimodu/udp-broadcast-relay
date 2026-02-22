package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

func Open(dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "ubr.db")
	sqlDB, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_busy_timeout=5000&_foreign_keys=1")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	sqlDB.SetMaxOpenConns(1) // SQLite concurrency best practice

	d := &DB{db: sqlDB}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return d, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			username      TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			is_admin      INTEGER NOT NULL DEFAULT 0,
			is_active     INTEGER NOT NULL DEFAULT 1,
			created_at    TEXT NOT NULL DEFAULT (datetime('now')),
			last_login_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key          TEXT NOT NULL UNIQUE,
			name         TEXT NOT NULL,
			created_at   TEXT NOT NULL DEFAULT (datetime('now')),
			last_used_at TEXT,
			expires_at   TEXT,
			is_revoked   INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token      TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			expires_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS forward_rules (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			name           TEXT NOT NULL,
			listen_port    INTEGER NOT NULL,
			listen_ip      TEXT NOT NULL DEFAULT '0.0.0.0',
			dest_broadcast TEXT NOT NULL DEFAULT '255.255.255.255',
			direction      TEXT NOT NULL DEFAULT 'server_to_client',
			is_enabled     INTEGER NOT NULL DEFAULT 1,
			created_at     TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS client_rules (
			client_key_id INTEGER NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
			rule_id       INTEGER NOT NULL REFERENCES forward_rules(id) ON DELETE CASCADE,
			PRIMARY KEY (client_key_id, rule_id)
		)`,
		`CREATE TABLE IF NOT EXISTS packet_log (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			rule_id     INTEGER REFERENCES forward_rules(id),
			src_ip      TEXT NOT NULL,
			dst_ip      TEXT NOT NULL,
			src_port    INTEGER NOT NULL,
			dst_port    INTEGER NOT NULL,
			size        INTEGER NOT NULL,
			direction   TEXT NOT NULL,
			client_name TEXT,
			timestamp   TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_packet_log_timestamp ON packet_log(timestamp DESC)`,
		`CREATE TABLE IF NOT EXISTS broadcast_observations (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			src_ip        TEXT NOT NULL,
			dst_ip        TEXT NOT NULL,
			src_port      INTEGER NOT NULL,
			dst_port      INTEGER NOT NULL,
			protocol_type TEXT NOT NULL,
			first_seen    TEXT NOT NULL DEFAULT (datetime('now')),
			last_seen     TEXT NOT NULL DEFAULT (datetime('now')),
			count         INTEGER NOT NULL DEFAULT 1,
			has_rule      INTEGER NOT NULL DEFAULT 0,
			UNIQUE(src_ip, dst_ip, src_port, dst_port)
		)`,
	}

	for _, m := range migrations {
		if _, err := d.db.Exec(m); err != nil {
			return fmt.Errorf("executing migration: %w", err)
		}
	}

	return nil
}
