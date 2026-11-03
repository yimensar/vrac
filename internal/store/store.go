// Package store owns Vrac's durable notes and rebuildable search index.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yimensar/vrac/internal/model"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("note not found")
	ErrConflict = errors.New("note changed; reload before saving")
	ErrInvalid  = errors.New("invalid note")
)

type Store struct{ db *sql.DB }
type CreateInput struct {
	Content        string `json:"content"`
	Pinned         bool   `json:"pinned"`
	Source         string `json:"source"`
	IdempotencyKey string `json:"idempotencyKey"`
}
type UpdateInput struct {
	Content *string `json:"content,omitempty"`
	Pinned  *bool   `json:"pinned,omitempty"`
	State   *string `json:"state,omitempty"`
	Version int64   `json:"version"`
}

func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	for _, p := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL"} {
		if _, err = db.Exec(p); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if path != ":memory:" {
		for _, file := range []string{path, path + "-wal", path + "-shm"} {
			if err = os.Chmod(file, 0600); err != nil && !os.IsNotExist(err) {
				db.Close()
				return nil, err
			}
		}
	}
	return s, nil
}
func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v > 2 {
		return fmt.Errorf("database schema %d is newer than this app", v)
	}
	if v >= 1 {
		return s.migrate2(v)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
 CREATE TABLE notes (
 id TEXT PRIMARY KEY, content TEXT NOT NULL, title TEXT NOT NULL, tags TEXT NOT NULL DEFAULT '[]',
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('active','archived','deleted')),
 pinned INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
 source TEXT NOT NULL DEFAULT 'web', source_id TEXT NOT NULL DEFAULT '',
 attachments TEXT NOT NULL DEFAULT '[]', relations TEXT NOT NULL DEFAULT '[]', raw_source BLOB);
 CREATE UNIQUE INDEX notes_source ON notes(source,source_id) WHERE source_id<>'';
 CREATE INDEX notes_timeline ON notes(state,pinned DESC,created_at DESC,id DESC);
 CREATE TABLE note_tags(note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,tag TEXT NOT NULL,PRIMARY KEY(note_id,tag));
 CREATE INDEX tags_notes ON note_tags(tag,note_id);
 CREATE TABLE revisions(note_id TEXT NOT NULL REFERENCES notes(id),version INTEGER NOT NULL,snapshot TEXT NOT NULL,PRIMARY KEY(note_id,version));
 CREATE TABLE idempotency(key TEXT PRIMARY KEY, note_id TEXT NOT NULL REFERENCES notes(id), content_hash TEXT NOT NULL);
 CREATE VIRTUAL TABLE notes_fts USING fts5(title,content,tags,content='notes',content_rowid='rowid',tokenize='unicode61 remove_diacritics 2');
 CREATE TRIGGER notes_ai AFTER INSERT ON notes BEGIN INSERT INTO notes_fts(rowid,title,content,tags) VALUES(new.rowid,new.title,new.content,new.tags); END;
 CREATE TRIGGER notes_ad AFTER DELETE ON notes BEGIN INSERT INTO notes_fts(notes_fts,rowid,title,content,tags) VALUES('delete',old.rowid,old.title,old.content,old.tags); END;
 CREATE TRIGGER notes_au AFTER UPDATE OF title,content,tags ON notes BEGIN
 INSERT INTO notes_fts(notes_fts,rowid,title,content,tags) VALUES('delete',old.rowid,old.title,old.content,old.tags);
 INSERT INTO notes_fts(rowid,title,content,tags) VALUES(new.rowid,new.title,new.content,new.tags); END;
 PRAGMA user_version=1;`)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.migrate2(1)
}

const columns = "id,content,title,tags,created_at,updated_at,state,pinned,version,source,source_id,attachments,relations,raw_source"

func prefixedColumns(p string) string { return p + strings.ReplaceAll(columns, ",", ","+p) }

type scanner interface{ Scan(...any) error }

func scanNote(row scanner) (model.Note, error) {
	var n model.Note
	var tags, attachments, relations string
	var raw []byte
	err := row.Scan(&n.ID, &n.Content, &n.Title, &tags, &n.CreatedAt, &n.UpdatedAt, &n.State, &n.Pinned, &n.Version, &n.Source, &n.SourceID, &attachments, &relations, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	if err != nil {
		return n, err
	}
	if err = json.Unmarshal([]byte(tags), &n.Tags); err != nil {
		return n, err
	}
	if err = json.Unmarshal([]byte(attachments), &n.Attachments); err != nil {
		return n, err
	}
	if err = json.Unmarshal([]byte(relations), &n.Relations); err != nil {
		return n, err
	}
	n.RawSource = raw
	return n, nil
}
func sortTime(t string) string {
	v, err := time.Parse(time.RFC3339Nano, t)
	if err != nil {
		return t
	}
	return v.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
func (s *Store) migrate2(version int) error {
	if version >= 2 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("ALTER TABLE notes ADD COLUMN created_sort TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	rows, err := tx.Query("SELECT id,created_at FROM notes")
	if err != nil {
		return err
	}
	type entry struct{ id, t string }
	values := []entry{}
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.t); err != nil {
			rows.Close()
			return err
		}
		values = append(values, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range values {
		if _, err = tx.Exec("UPDATE notes SET created_sort=? WHERE id=?", sortTime(v.t), v.id); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DROP INDEX notes_timeline; CREATE INDEX notes_timeline ON notes(state,pinned DESC,created_sort DESC,id DESC); PRAGMA user_version=2"); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Get(ctx context.Context, id string) (model.Note, error) {
	return scanNote(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM notes WHERE id=?", id))
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func hash(s string) string    { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func jsonString(v any) string { b, _ := json.Marshal(v); return string(b) }
func validateContent(c string) error {
	if strings.TrimSpace(c) == "" {
		return fmt.Errorf("%w: content is empty", ErrInvalid)
	}
	if len(c) > 2*1024*1024 {
		return fmt.Errorf("%w: content exceeds 2 MiB", ErrInvalid)
	}
	return nil
}
func (s *Store) Create(ctx context.Context, in CreateInput) (model.Note, error) {
	if err := validateContent(in.Content); err != nil {
		return model.Note{}, err
	}
	if len(in.IdempotencyKey) > 200 {
		return model.Note{}, fmt.Errorf("%w: idempotency key too long", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Note{}, err
	}
	defer tx.Rollback()
	if in.IdempotencyKey != "" {
		var id, h string
		err = tx.QueryRowContext(ctx, "SELECT note_id,content_hash FROM idempotency WHERE key=?", in.IdempotencyKey).Scan(&id, &h)
		if err == nil {
			if h != hash(in.Content) {
				return model.Note{}, fmt.Errorf("%w: idempotency key reused with different content", ErrConflict)
			}
			return scanNote(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM notes WHERE id=?", id))
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return model.Note{}, err
		}
	}
	t := now()
	if in.Source == "" {
		in.Source = "web"
	}
	n := model.Note{ID: newID(), Content: in.Content, Title: Title(in.Content), Tags: ExtractTags(in.Content), CreatedAt: t, UpdatedAt: t, State: "active", Pinned: in.Pinned, Version: 1, Source: in.Source, Attachments: []model.Attachment{}, Relations: []model.Relation{}}
	if err = insertNote(ctx, tx, n); err != nil {
		return n, err
	}
	if in.IdempotencyKey != "" {
		if _, err = tx.ExecContext(ctx, "INSERT INTO idempotency VALUES(?,?,?)", in.IdempotencyKey, n.ID, hash(n.Content)); err != nil {
			return n, err
		}
	}
	if err = tx.Commit(); err != nil {
		return model.Note{}, err
	}
	return n, nil
}
func insertNote(ctx context.Context, tx *sql.Tx, n model.Note) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO notes("+columns+",created_sort) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", n.ID, n.Content, n.Title, jsonString(n.Tags), n.CreatedAt, n.UpdatedAt, n.State, n.Pinned, n.Version, n.Source, n.SourceID, jsonString(n.Attachments), jsonString(n.Relations), []byte(n.RawSource), sortTime(n.CreatedAt))
	if err != nil {
		return err
	}
	return recordDerived(ctx, tx, n)
}
func recordDerived(ctx context.Context, tx *sql.Tx, n model.Note) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM note_tags WHERE note_id=?", n.ID); err != nil {
		return err
	}
	for _, tag := range n.Tags {
		if _, err := tx.ExecContext(ctx, "INSERT INTO note_tags VALUES(?,?)", n.ID, strings.ToLower(tag)); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO revisions(note_id,version,snapshot) VALUES(?,?,?)", n.ID, n.Version, jsonString(n))
	return err
}
func (s *Store) Update(ctx context.Context, id string, in UpdateInput) (model.Note, error) {
	if in.Version < 1 {
		return model.Note{}, fmt.Errorf("%w: version is required", ErrInvalid)
	}
	if in.Content != nil {
		if err := validateContent(*in.Content); err != nil {
			return model.Note{}, err
		}
	}
	if in.State != nil && *in.State != "active" && *in.State != "archived" && *in.State != "deleted" {
		return model.Note{}, fmt.Errorf("%w: unknown state", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Note{}, err
	}
	defer tx.Rollback()
	n, err := scanNote(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM notes WHERE id=?", id))
	if err != nil {
		return n, err
	}
	if n.Version != in.Version {
		return model.Note{}, ErrConflict
	}
	if in.Content != nil {
		oldDerived := map[string]bool{}
		for _, tag := range ExtractTags(n.Content) {
			oldDerived[strings.ToLower(tag)] = true
		}
		explicit := []string{}
		for _, tag := range n.Tags {
			if !oldDerived[strings.ToLower(tag)] {
				explicit = append(explicit, tag)
			}
		}
		n.Content = *in.Content
		n.Title = Title(n.Content)
		n.Tags = mergeTags(explicit, ExtractTags(n.Content))
	}
	if in.Pinned != nil {
		n.Pinned = *in.Pinned
	}
	if in.State != nil {
		n.State = *in.State
	}
	n.Version++
	n.UpdatedAt = now()
	_, err = tx.ExecContext(ctx, "UPDATE notes SET content=?,title=?,tags=?,updated_at=?,state=?,pinned=?,version=? WHERE id=? AND version=?", n.Content, n.Title, jsonString(n.Tags), n.UpdatedAt, n.State, n.Pinned, n.Version, id, in.Version)
	if err != nil {
		return n, err
	}
	if err = recordDerived(ctx, tx, n); err != nil {
		return n, err
	}
	if err = tx.Commit(); err != nil {
		return model.Note{}, err
	}
	return n, nil
}
func (s *Store) Tags(ctx context.Context) ([]model.Tag, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT t.tag,count(*) FROM note_tags t JOIN notes n ON n.id=t.note_id WHERE n.state='active' GROUP BY t.tag ORDER BY count(*) DESC,t.tag")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Tag{}
	for rows.Next() {
		var t model.Tag
		if err = rows.Scan(&t.Name, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) Stats(ctx context.Context) (model.Stats, error) {
	var st model.Stats
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(sum(state='active'),0),coalesce(sum(state='archived'),0),(SELECT count(DISTINCT tag) FROM note_tags t JOIN notes n ON n.id=t.note_id WHERE n.state='active'),coalesce(sum(json_array_length(attachments)),0) FROM notes WHERE state<>'deleted'`).Scan(&st.Active, &st.Archived, &st.Tags, &st.Attachments)
	return st, err
}
func (s *Store) History(ctx context.Context, id string) ([]model.Note, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT snapshot FROM revisions WHERE note_id=? ORDER BY version DESC LIMIT 100", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Note{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var n model.Note
		if err = json.Unmarshal([]byte(raw), &n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, rows.Err()
}

// RebuildSearch regenerates every FTS token from canonical note content.
func (s *Store) RebuildSearch(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO notes_fts(notes_fts) VALUES('rebuild')")
	return err
}
