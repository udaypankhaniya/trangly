// Package db provides the SQLite data access layer for Trangly.
// All database reads and writes go through this package — no raw SQL elsewhere.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // pure-Go SQLite driver; no CGO required
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB wraps *sql.DB with all typed query methods.
type DB struct {
	*sql.DB
}

// New opens the SQLite database at path, enables WAL mode for concurrency,
// and runs all pending migrations. The database file and its parent directories
// are created if they do not exist.
func New(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", path, err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("db: ping %s: %w", path, err)
	}

	// SQLite performs best with a single writer connection.
	sqlDB.SetMaxOpenConns(1)

	db := &DB{sqlDB}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("db: migrate: %w", err)
	}
	return db, nil
}

// migrate runs all *.sql files in the embedded migrations directory in lexicographic order.
// Applied migrations are recorded in the schema_migrations table so each file runs exactly once.
func (db *DB) migrate() error {
	// Ensure the tracking table exists before anything else.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}

	// Sort for deterministic ordering (001_, 002_, etc.)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		// Skip already-applied migrations.
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, entry.Name()).Scan(&count); err != nil {
			return fmt.Errorf("checking migration %s: %w", entry.Name(), err)
		}
		if count > 0 {
			continue
		}

		data, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", entry.Name(), err)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.Exec(string(data)); err != nil {
			_ = tx.Rollback()
			// Databases created before the schema_migrations table was introduced
			// will have already applied earlier migrations. "duplicate column name"
			// means the ALTER TABLE was already applied — record it and continue.
			if strings.Contains(err.Error(), "duplicate column name") {
				if _, err2 := db.Exec(`INSERT OR IGNORE INTO schema_migrations (name) VALUES (?)`, entry.Name()); err2 != nil {
					return fmt.Errorf("recording pre-applied migration %s: %w", entry.Name(), err2)
				}
				continue
			}
			return fmt.Errorf("running migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, entry.Name()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("recording migration %s: %w", entry.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}
