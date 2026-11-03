package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schema string

const applicationID = 1448235826
const nodeColumns = `n.id,COALESCE(n.parent_id,''),n.text,n.revision,n.created_at,n.updated_at,COALESCE(n.system,''),n.deleted,EXISTS(SELECT 1 FROM nodes child WHERE child.parent_id=n.id AND child.deleted=0)`

type Engine struct{ db *sql.DB }

type queryer interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func Open(path string) (*Engine, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open database file: %w", err)
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: abs}
	values := url.Values{}
	values.Set("_foreign_keys", "on")
	values.Set("_busy_timeout", "5000")
	u.RawQuery = values.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	e := &Engine{db: db}
	fail := func(err error) (*Engine, error) { db.Close(); return nil, err }
	var version, app int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fail(fmt.Errorf("read database version: %w", err))
	}
	if err = db.QueryRow("PRAGMA application_id").Scan(&app); err != nil {
		return fail(err)
	}
	if !((version == 0 && app == 0) || (version == 1 && app == applicationID)) {
		return fail(fmt.Errorf("%w: unsupported database format (application=%d, version=%d)", ErrInvalid, app, version))
	}
	if version == 0 {
		var count int
		if err = db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&count); err != nil {
			return fail(err)
		}
		if count != 0 {
			return fail(fmt.Errorf("%w: file is not an empty Vrac database", ErrInvalid))
		}
	}
	if _, err = db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fail(err)
	}
	if _, err = db.Exec("PRAGMA cache_size=-16384"); err != nil {
		return fail(err)
	}
	if _, err = db.Exec("PRAGMA synchronous=FULL"); err != nil {
		return fail(err)
	}
	if version == 0 && app == 0 {
		var count int
		if err = db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&count); err != nil {
			return fail(err)
		}
		if count != 0 {
			return fail(fmt.Errorf("%w: file is not an empty Vrac database", ErrInvalid))
		}
		tx, err := db.Begin()
		if err != nil {
			return fail(err)
		}
		if _, err = tx.Exec(schema); err == nil {
			_, err = e.insertSystem(tx, "Journal", "journal", "")
		}
		if err != nil {
			tx.Rollback()
			return fail(fmt.Errorf("initialize database: %w", err))
		}
		if err = tx.Commit(); err != nil {
			return fail(err)
		}
	} else if version != 1 || app != applicationID {
		return fail(fmt.Errorf("%w: unsupported database format (application=%d, version=%d)", ErrInvalid, app, version))
	}
	// Validate the guarantees on the actual connection, not just configuration.
	var foreignKeys, synchronous int
	if err = db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return fail(err)
	}
	if err = db.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
		return fail(err)
	}
	if foreignKeys != 1 || synchronous != 2 {
		return fail(fmt.Errorf("database durability configuration is invalid"))
	}
	if _, err = e.Journal(); err != nil {
		return fail(fmt.Errorf("invalid Journal identity: %w", err))
	}
	return e, nil
}
func (e *Engine) Close() error { return e.db.Close() }

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func validDate(s string) error {
	t, err := time.Parse("2006-01-02", s)
	if err != nil || t.Year() < 1 || t.Format("2006-01-02") != s {
		return fmt.Errorf("%w: date must be YYYY-MM-DD", ErrInvalid)
	}
	return nil
}
func defaultDate(s string, fallback ...string) (string, error) {
	if len(fallback) > 1 {
		return "", fmt.Errorf("%w: at most one fallback date is accepted", ErrInvalid)
	}
	if s == "" && len(fallback) == 1 {
		s = fallback[0]
	}
	if s == "" {
		s = time.Now().UTC().Format("2006-01-02")
	}
	return s, validDate(s)
}

func (e *Engine) mutate(kind, requestID string, payload any, result any, fn func(*sql.Tx) error) error {
	if len(requestID) > 128 || strings.TrimSpace(requestID) != requestID {
		return fmt.Errorf("%w: request_id must be at most 128 characters with no outer whitespace", ErrInvalid)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(hash[:])
	tx, err := e.db.Begin()
	if err != nil {
		return fmt.Errorf("begin mutation: %w", err)
	}
	defer tx.Rollback()
	if requestID != "" {
		var oldKind, oldHash, oldResult string
		err = tx.QueryRow("SELECT kind,payload_hash,result FROM mutations WHERE request_id=?", requestID).Scan(&oldKind, &oldHash, &oldResult)
		if err == nil {
			if oldKind != kind || oldHash != fingerprint {
				return fmt.Errorf("%w: request_id already used with a different mutation", ErrConflict)
			}
			if result != nil {
				return json.Unmarshal([]byte(oldResult), result)
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}
	}
	if err = fn(tx); err != nil {
		return err
	}
	if requestID != "" {
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO mutations(request_id,kind,payload_hash,result) VALUES(?,?,?,?)", requestID, kind, fingerprint, string(b)); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit mutation: %w", err)
	}
	return nil
}

func scanNode(scan interface{ Scan(...any) error }) (Node, error) {
	n := Node{Tags: []string{}, References: []Reference{}}
	var deleted int
	err := scan.Scan(&n.ID, &n.ParentID, &n.Text, &n.Revision, &n.CreatedAt, &n.UpdatedAt, &n.System, &deleted, &n.HasChildren)
	n.Deleted = deleted != 0
	if strings.HasPrefix(n.System, "day:") {
		n.JournalDate = strings.TrimPrefix(n.System, "day:")
	}
	if err == sql.ErrNoRows {
		return n, ErrNotFound
	}
	return n, err
}
func getNode(q queryer, id string, includeDeleted bool) (Node, error) {
	suffix := " AND n.deleted=0"
	if includeDeleted {
		suffix = ""
	}
	n, err := scanNode(q.QueryRow("SELECT "+nodeColumns+" FROM nodes n WHERE n.id=?"+suffix, id))
	if err != nil {
		return n, err
	}
	nodes := []Node{n}
	if err = hydrate(q, nodes); err != nil {
		return n, err
	}
	return nodes[0], nil
}
func hydrate(q queryer, nodes []Node) error {
	if len(nodes) == 0 {
		return nil
	}
	args := make([]any, len(nodes))
	marks := make([]string, len(nodes))
	byID := map[string]int{}
	for i := range nodes {
		args[i] = nodes[i].ID
		marks[i] = "?"
		byID[nodes[i].ID] = i
		nodes[i].Tags = []string{}
		nodes[i].References = []Reference{}
	}
	in := strings.Join(marks, ",")
	rows, err := q.Query("SELECT node_id,tag FROM node_tags WHERE node_id IN ("+in+") ORDER BY node_id,tag", args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, tag string
		if err = rows.Scan(&id, &tag); err != nil {
			rows.Close()
			return err
		}
		i := byID[id]
		nodes[i].Tags = append(nodes[i].Tags, tag)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = q.Query("SELECT r.source_id,r.target_id,t.text,r.token FROM node_references r JOIN nodes t ON t.id=r.target_id WHERE r.source_id IN ("+in+") ORDER BY r.source_id,r.token", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var ref Reference
		if err = rows.Scan(&id, &ref.TargetID, &ref.Label, &ref.SourceLabel); err != nil {
			return err
		}
		i := byID[id]
		nodes[i].References = append(nodes[i].References, ref)
	}
	return rows.Err()
}

func (e *Engine) read(fn func(*sql.Tx) error) error {
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (e *Engine) Get(id string) (Node, error) {
	var n Node
	err := e.read(func(tx *sql.Tx) error {
		var err error
		n, err = getNode(tx, id, false)
		if err != nil {
			return err
		}
		n.JournalDate, err = journalDate(tx, id)
		return err
	})
	return n, err
}
func journalDate(q queryer, id string) (string, error) {
	var date string
	err := q.QueryRow(`WITH RECURSIVE ancestors(id,parent_id,system) AS (SELECT id,parent_id,system FROM nodes WHERE id=? UNION ALL SELECT n.id,n.parent_id,n.system FROM nodes n JOIN ancestors a ON n.id=a.parent_id) SELECT substr(system,5) FROM ancestors WHERE system LIKE 'day:%' LIMIT 1`, id).Scan(&date)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return date, err
}
func (e *Engine) Journal() (Node, error) {
	var n Node
	err := e.read(func(tx *sql.Tx) error {
		var id string
		if err := tx.QueryRow("SELECT id FROM nodes WHERE system='journal' AND parent_id IS NULL AND deleted=0").Scan(&id); err != nil {
			return err
		}
		var err error
		n, err = getNode(tx, id, false)
		return err
	})
	return n, err
}
func (e *Engine) insertSystem(tx *sql.Tx, text, system, parentID string) (Node, error) {
	id, err := newID()
	if err != nil {
		return Node{}, err
	}
	at := now()
	var parent any
	if parentID != "" {
		parent = parentID
	}
	position, err := positionFor(tx, parentID, Placement{First: strings.HasPrefix(system, "day:")}, "")
	if err != nil {
		return Node{}, err
	}
	if _, err = tx.Exec("INSERT INTO nodes(id,parent_id,position,text,revision,created_at,updated_at,system) VALUES(?,?,?,?,1,?,?,?)", id, parent, position, text, at, at, system); err != nil {
		return Node{}, err
	}
	return getNode(tx, id, false)
}
func (e *Engine) ensureDay(tx *sql.Tx, date string) (Node, error) {
	if err := validDate(date); err != nil {
		return Node{}, err
	}
	var id string
	err := tx.QueryRow("SELECT id FROM nodes WHERE system=? AND deleted=0", "day:"+date).Scan(&id)
	if err == nil {
		return getNode(tx, id, false)
	}
	if err != sql.ErrNoRows {
		return Node{}, err
	}
	var journal string
	if err = tx.QueryRow("SELECT id FROM nodes WHERE system='journal' AND deleted=0").Scan(&journal); err != nil {
		return Node{}, err
	}
	return e.insertSystem(tx, date, "day:"+date, journal)
}
func (e *Engine) EnsureDay(date string) (Node, error) {
	var n Node
	err := e.mutate("ensure-day", "", date, &n, func(tx *sql.Tx) error { var err error; n, err = e.ensureDay(tx, date); return err })
	return n, err
}
