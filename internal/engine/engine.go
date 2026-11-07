// Package engine owns Vrac's model, rules, SQLite storage, transactions,
// migrations and search. It has no HTTP, UI or network dependency.
package engine

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/base32"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema_v1.sql
var schemaV1 string

//go:embed schema_v3.sql
var schemaV3 string

//go:embed schema_v4.sql
var schemaV4 string

//go:embed schema_v5.sql
var schemaV5 string

//go:embed schema_v6.sql
var schemaV6 string

//go:embed schema_v7.sql
var schemaV7 string

// applicationID marks a SQLite file as a Vrac database ("VRAC").
const applicationID = 0x56524143

// migrations[i] upgrades user_version i to i+1. Published migrations are
// never modified.
var migrations = []func(*Engine, *sql.Tx) error{
	func(e *Engine, tx *sql.Tx) error {
		if _, err := tx.Exec(schemaV1); err != nil {
			return err
		}
		return e.seed(tx)
	},
	migrateTypeTokens,
	func(e *Engine, tx *sql.Tx) error {
		_, err := tx.Exec(schemaV3)
		return err
	},
	func(e *Engine, tx *sql.Tx) error {
		if _, err := tx.Exec(schemaV4); err != nil {
			return err
		}
		return rebuildContext(context.Background(), tx)
	},
	func(e *Engine, tx *sql.Tx) error {
		_, err := tx.Exec(schemaV5)
		return err
	},
	migrateOwner,
	migrateTags,
}

// Errors returned by the engine. Callers compare with errors.Is.
var (
	ErrNotFound  = errors.New("not found")
	ErrInvalid   = errors.New("invalid request")
	ErrConflict  = errors.New("conflict")
	ErrProtected = errors.New("protected node")
)

// Options configures an engine.
type Options struct {
	// Location is the user's time zone. Journal days and "today" use it.
	Location *time.Location
	// Now overrides the clock, for tests.
	Now func() time.Time
}

// Engine is the single entry point to a Vrac database.
type Engine struct {
	db      *sql.DB
	loc     *time.Location
	now     func() time.Time
	journal int64
}

// Open opens or creates the database at path, refusing files that are not
// Vrac databases, and applies pending migrations after a snapshot backup.
func Open(path string, opts Options) (*Engine, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	q := url.Values{}
	q.Set("_foreign_keys", "on")
	q.Set("_busy_timeout", "5000")
	q.Set("_journal_mode", "WAL")
	q.Set("_synchronous", "FULL")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: abs, RawQuery: q.Encode()}).String())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// One connection: writes are serialized and personal workloads are small.
	db.SetMaxOpenConns(1)
	e := &Engine{db: db, loc: opts.Location, now: opts.Now}
	if e.loc == nil {
		e.loc = time.Local
	}
	if e.now == nil {
		e.now = time.Now
	}
	if err := e.migrate(abs); err != nil {
		db.Close()
		return nil, err
	}
	if err := db.QueryRow(`SELECT seq FROM nodes WHERE kind = 'journal'`).Scan(&e.journal); err != nil {
		db.Close()
		return nil, fmt.Errorf("read journal root: %w", err)
	}
	return e, nil
}

// Close releases the database.
func (e *Engine) Close() error { return e.db.Close() }

func (e *Engine) migrate(path string) error {
	var version, app int
	if err := e.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read database version: %w", err)
	}
	if err := e.db.QueryRow(`PRAGMA application_id`).Scan(&app); err != nil {
		return err
	}
	if version == 0 {
		var objects int
		if err := e.db.QueryRow(`SELECT count(*) FROM sqlite_schema`).Scan(&objects); err != nil {
			return err
		}
		if objects != 0 || app != 0 {
			return fmt.Errorf("%w: %s is not a Vrac database", ErrInvalid, path)
		}
	} else if app != applicationID {
		return fmt.Errorf("%w: %s is not a Vrac database", ErrInvalid, path)
	}
	if version > len(migrations) {
		return fmt.Errorf("%w: database version %d is newer than this Vrac", ErrInvalid, version)
	}
	if version == len(migrations) {
		return nil
	}
	if version > 0 {
		// Named after the version it preserves, so successive upgrades never
		// collide.
		backup := fmt.Sprintf("%s.v%d-%s.backup", path, version, e.now().UTC().Format("20060102T150405Z"))
		if err := e.Backup(backup); err != nil {
			return fmt.Errorf("snapshot before migration: %w", err)
		}
	}
	return e.write(context.Background(), func(tx *sql.Tx) error {
		for v := version; v < len(migrations); v++ {
			if err := migrations[v](e, tx); err != nil {
				return fmt.Errorf("migration %d: %w", v+1, err)
			}
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA application_id = %d`, applicationID)); err != nil {
			return err
		}
		_, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations)))
		return err
	})
}

// seed is part of migration 1, which is published and stays as it was: it
// wrote the six types of version 1, which migration 7 removes when unused.
func (e *Engine) seed(tx *sql.Tx) error {
	now := e.stamp()
	for i, t := range []string{"task", "meeting", "event", "decision", "project", "person"} {
		if _, err := tx.Exec(`INSERT INTO types(name, builtin, color, created_at) VALUES (?, 1, ?, ?)`, t, palette[i%len(palette)], now); err != nil {
			return err
		}
	}
	_, err := tx.Exec(`INSERT INTO nodes(id, kind, parent_seq, position, text, day, revision, created_at, updated_at, created_by, created_source, updated_by, updated_source)
		VALUES (?, 'journal', NULL, 0, 'Journal', ?, 1, ?, ?, 'vrac', 'vrac', 'vrac', 'vrac')`, newID(), e.today(), now, now)
	return err
}

// Backup writes a consistent snapshot of the database to path using SQLite.
func (e *Engine) Backup(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%w: %s already exists", ErrConflict, path)
	}
	_, err := e.db.Exec(`VACUUM INTO ?`, path)
	return err
}

// Check runs SQLite integrity and foreign key checks.
func (e *Engine) Check() error {
	var result string
	if err := e.db.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("integrity check: %s", result)
	}
	rows, err := e.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("foreign key check failed")
	}
	return rows.Err()
}

// write runs fn in one immediate transaction.
func (e *Engine) write(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Today returns the current local date as YYYY-MM-DD.
func (e *Engine) Today() string { return e.today() }

func (e *Engine) today() string { return e.now().In(e.loc).Format(dateLayout) }

func (e *Engine) stamp() string { return e.now().UTC().Format(time.RFC3339Nano) }

const dateLayout = "2006-01-02"

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns an opaque random identifier of 26 characters.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return idEncoding.EncodeToString(b)
}

func isID(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '2' && r <= '7') {
			return false
		}
	}
	return true
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func nameKey(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
