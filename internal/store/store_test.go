package store

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/yimensar/vrac/internal/model"
)

var testCtx = context.Background()

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func createTest(t *testing.T, s *Store, c string) model.Note {
	t.Helper()
	n, err := s.Create(testCtx, CreateInput{Content: c})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func TestContentTagsAndPersistence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	content := "# État du projet\n\nDécision exacte. #projet/alpha #Go\n\n```go\n#secret\n```\n`#inline`"
	n := createTest(t, s, content)
	if !reflect.DeepEqual(n.Tags, []string{"go", "projet", "projet/alpha"}) {
		t.Fatalf("tags: %v", n.Tags)
	}
	if n.Title != "État du projet" {
		t.Fatal(n.Title)
	}
	s.Close()
	s, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Get(testCtx, n.ID)
	if err != nil || got.Content != content || got.CreatedAt != n.CreatedAt {
		t.Fatalf("reopen: %v %#v", err, got)
	}
	var version int
	s.DB().QueryRow("PRAGMA user_version").Scan(&version)
	if version != 2 {
		t.Fatal(version)
	}
}
func TestVersionConflictAndHistory(t *testing.T) {
	s := openTest(t)
	n := createTest(t, s, "Before #first")
	content := "After #second"
	updated, err := s.Update(testCtx, n.ID, UpdateInput{Content: &content, Version: n.Version})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.CreatedAt != n.CreatedAt {
		t.Fatal(updated)
	}
	bad := "Lost write"
	if _, err = s.Update(testCtx, n.ID, UpdateInput{Content: &bad, Version: n.Version}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	got, _ := s.Get(testCtx, n.ID)
	if got.Content != content {
		t.Fatal(got.Content)
	}
	history, err := s.History(testCtx, n.ID)
	if err != nil || len(history) != 2 || history[1].Content != "Before #first" {
		t.Fatalf("history %v %+v", err, history)
	}
	for _, query := range []struct {
		q    string
		want int
	}{{"first", 0}, {"second", 1}} {
		p, err := s.List(testCtx, model.Query{Q: query.q})
		if err != nil || p.Total != query.want {
			t.Fatalf("search %s: %v %d", query.q, err, p.Total)
		}
	}
	if err = s.RebuildSearch(testCtx); err != nil {
		t.Fatal(err)
	}
}
func TestIdempotentCreate(t *testing.T) {
	s := openTest(t)
	in := CreateInput{Content: "Same capture", IdempotencyKey: "request-1"}
	a, err := s.Create(testCtx, in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(testCtx, in)
	if err != nil || a.ID != b.ID {
		t.Fatal(err)
	}
	in.Content = "Different capture"
	if _, err = s.Create(testCtx, in); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	stats, _ := s.Stats(testCtx)
	if stats.Active != 1 {
		t.Fatal(stats)
	}
}
func rawMemo(id, content, date, state string) json.RawMessage {
	return json.RawMessage(jsonString(map[string]any{"uid": id, "content": content, "createTime": date, "updateTime": date, "state": state, "tags": []string{"explicit/retained"}}))
}
func TestImportAtomicRepeatableAndRawMetadata(t *testing.T) {
	s := openTest(t)
	raw := rawMemo("stable", "Original Markdown\n#tag", "2026-11-03T12:30:00Z", "ARCHIVED")
	if _, err := s.ImportMemos(testCtx, []json.RawMessage{raw, json.RawMessage(`{"uid":"bad"}`)}); err == nil {
		t.Fatal("invalid import accepted")
	}
	stats, _ := s.Stats(testCtx)
	if stats.Active+stats.Archived != 0 {
		t.Fatal("partial import", stats)
	}
	report, err := s.ImportMemos(testCtx, []json.RawMessage{raw})
	if err != nil || report.Imported != 1 {
		t.Fatal(err, report)
	}
	repeat, err := s.ImportMemos(testCtx, []json.RawMessage{raw})
	if err != nil || repeat.Skipped != 1 || repeat.ContentSHA256 != report.ContentSHA256 {
		t.Fatal(err, repeat)
	}
	n, _ := s.Get(testCtx, "stable")
	if n.State != "archived" || n.SourceID != "memos/stable" || !bytes.Equal(n.RawSource, raw) {
		t.Fatal(n)
	}
	if !contains(n.Tags, "explicit/retained") {
		t.Fatal(n.Tags)
	}
	export, err := s.Export(testCtx)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(export)
	if !bytes.Contains(b, []byte(`"rawSource"`)) {
		t.Fatal("raw source lost in export")
	}
}
func contains(s []string, v string) bool {
	for _, a := range s {
		if a == v {
			return true
		}
	}
	return false
}
func TestCursorPaginationTimelineAndFTS(t *testing.T) {
	s := openTest(t)
	raws := []json.RawMessage{}
	for i, id := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		date := "2026-11-03T12:00:00Z"
		if i == 6 {
			date = "2026-11-03T12:00:00.5Z"
		}
		raws = append(raws, rawMemo(id, "Shared searchable token #common", date, "NORMAL"))
	}
	if _, err := s.ImportMemos(testCtx, raws); err != nil {
		t.Fatal(err)
	}
	pinned := true
	if _, err := s.Update(testCtx, "one", UpdateInput{Pinned: &pinned, Version: 1}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []model.Query{{Limit: 2}, {Limit: 2, Sort: "oldest"}, {Limit: 2, Q: "searchable"}, {Limit: 2, Tag: "common"}} {
		seen := map[string]bool{}
		ids := []string{}
		for {
			p, err := s.List(testCtx, q)
			if err != nil {
				t.Fatal(err)
			}
			if p.Total != 7 {
				t.Fatal(p.Total)
			}
			for _, n := range p.Notes {
				if seen[n.ID] {
					t.Fatal("duplicate", n.ID)
				}
				seen[n.ID] = true
				ids = append(ids, n.ID)
			}
			if p.NextCursor == "" {
				break
			}
			q.Cursor = p.NextCursor
		}
		if len(ids) != 7 {
			t.Fatal("missing notes", ids)
		}
		if q.Q == "" && ids[0] != "one" {
			t.Fatal("pinned order", ids)
		}
	}
	p, _ := s.List(testCtx, model.Query{Limit: 2})
	if _, err := s.List(testCtx, model.Query{Cursor: p.NextCursor, Tag: "other"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	p, err := s.List(testCtx, model.Query{Pinned: true, Limit: 1})
	if err != nil || p.Total != 1 || p.Notes[0].ID != "one" || p.NextCursor != "" {
		t.Fatal(p, err)
	}
	// Same second, distinct fractional encodings must still be chronological.
	p, err = s.List(testCtx, model.Query{Limit: 50})
	if err != nil || p.Notes[1].ID != "seven" {
		t.Fatal("timestamp order", err, p.Notes)
	}
}
func TestSearchAccentsExpansionAndUnsafeSyntax(t *testing.T) {
	s := openTest(t)
	createTest(t, s, "# Deployment\nBitbucket pipeline authentication JWT. #salesforce")
	createTest(t, s, "# Sauvegarde\nRétention des snapshots et restauration.")
	for _, q := range []string{"deploiement", "authentification Salesforce", "retention", "\" OR *) --"} {
		p, err := s.List(testCtx, model.Query{Q: q})
		if err != nil {
			t.Fatal(q, err)
		}
		if q != "\" OR *) --" && p.Total != 1 {
			t.Fatal(q, p.Total)
		}
	}
}
func makeZIP(t *testing.T, filename string, wrongHash bool, extraPath string) {
	t.Helper()
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	write := func(path string, b []byte) {
		entry, err := w.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	attachment := []byte("original attachment bytes")
	digest := sha256.Sum256(attachment)
	sum := hex.EncodeToString(digest[:])
	if wrongHash {
		sum = strings.Repeat("0", 64)
	}
	write("manifest.json", []byte(`{"format":"memos-export","formatVersion":"1.0","counts":{"memos":1,"attachments":1}}`))
	metadata := map[string]any{"uid": "note-1", "contentPath": "memos/note-1.md", "createTime": "2026-11-03T12:00:00Z", "updateTime": "2026-11-03T12:00:00Z", "state": "ARCHIVED", "tags": []string{"project", "project/one"}, "attachments": []map[string]any{{"uid": "attachment-1", "filename": "original.html", "type": "text/html", "size": len(attachment), "sha256": sum, "path": "attachments/attachment-1/original.html"}}}
	write("memos/note-1.json", []byte(jsonString(metadata)))
	write("memos/note-1.md", []byte("# Original\n\nByte for byte.\n"))
	write("attachments/attachment-1/original.html", attachment)
	if extraPath != "" {
		write(extraPath, []byte("bad"))
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}
func TestNativeZIPIntegrityAndRelations(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "export.zip")
	makeZIP(t, file, false, "")
	metadata := []json.RawMessage{json.RawMessage(`{"name":"memos/note-1","relations":[{"memo":{"name":"memos/note-1"},"relatedMemo":{"name":"memos/other"},"type":"REFERENCE"}]}`)}
	raws, err := ReadMemosZIP(file, filepath.Join(dir, "attachments"), metadata)
	if err != nil {
		t.Fatal(err)
	}
	s := openTest(t)
	if _, err = s.ImportMemos(testCtx, raws); err != nil {
		t.Fatal(err)
	}
	n, _ := s.Get(testCtx, "note-1")
	if n.Content != "# Original\n\nByte for byte.\n" || n.Relations[0].TargetID != "other" || n.State != "archived" {
		t.Fatal(n)
	}
	a, err := s.Attachment(testCtx, "attachment-1")
	if err != nil || a.Filename != "original.html" {
		t.Fatal(a, err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "attachments/attachment-1/original.html"))
	if err != nil || string(content) != "original attachment bytes" {
		t.Fatal(err)
	}
}
func TestZIPRejectsCorruptionAndTraversal(t *testing.T) {
	for _, tc := range []struct {
		wrong bool
		extra string
	}{{true, ""}, {false, "../outside"}} {
		dir := t.TempDir()
		file := filepath.Join(dir, "export.zip")
		makeZIP(t, file, tc.wrong, tc.extra)
		if _, err := ReadMemosZIP(file, filepath.Join(dir, "attachments"), nil); err == nil {
			t.Fatal("unsafe ZIP accepted")
		}
		entries, _ := os.ReadDir(filepath.Join(dir, "attachments"))
		if len(entries) > 0 {
			t.Fatal("partial files before validation")
		}
	}
}
func TestTagUpdatesAndStateVisibility(t *testing.T) {
	s := openTest(t)
	n := createTest(t, s, "#tag/child hello")
	p, err := s.List(testCtx, model.Query{Tag: "tag"})
	if err != nil || p.Total != 1 {
		t.Fatal(p, err)
	}
	archived := "archived"
	if _, err = s.Update(testCtx, n.ID, UpdateInput{State: &archived, Version: n.Version}); err != nil {
		t.Fatal(err)
	}
	p, _ = s.List(testCtx, model.Query{Q: "hello"})
	if p.Total != 0 {
		t.Fatal("archive leaked")
	}
	tags, _ := s.Tags(testCtx)
	if len(tags) != 0 {
		t.Fatal(tags)
	}
}
func TestSortingHelper(t *testing.T) {
	dates := []string{"2026-11-03T13:00:00.1+01:00", "2026-11-03T12:00:00Z"}
	sort.Slice(dates, func(i, j int) bool { return sortTime(dates[i]) < sortTime(dates[j]) })
	if dates[0] != "2026-11-03T12:00:00Z" {
		t.Fatal(dates)
	}
}

func TestMarkdownTitles(t *testing.T) {
	for input, want := range map[string]string{"**Migration Trilium → Memos**": "Migration Trilium → Memos", "# Heading": "Heading", "#tag #project/one": "Untitled", "Normal title": "Normal title"} {
		if got := Title(input); got != want {
			t.Fatalf("Title(%q)=%q; want %q", input, got, want)
		}
	}
}
func TestImportedMetadataTagsSurviveContentEdit(t *testing.T) {
	s := openTest(t)
	raw := rawMemo("metadata", "Original #body", "2026-11-03T12:00:00Z", "NORMAL")
	if _, err := s.ImportMemos(testCtx, []json.RawMessage{raw}); err != nil {
		t.Fatal(err)
	}
	content := "Changed #new"
	n, err := s.Update(testCtx, "metadata", UpdateInput{Version: 1, Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(n.Tags, "explicit/retained") || contains(n.Tags, "body") || !contains(n.Tags, "new") {
		t.Fatal(n.Tags)
	}
}
func TestImportedMixedCaseTagFilter(t *testing.T) {
	s := openTest(t)
	raw := json.RawMessage(`{"uid":"mixed-case","content":"A note","createTime":"2026-11-03T12:00:00Z","tags":["Équipe/Alpha"]}`)
	if _, err := s.ImportMemos(testCtx, []json.RawMessage{raw}); err != nil {
		t.Fatal(err)
	}
	page, err := s.List(testCtx, model.Query{Tag: "ÉQUIPE/ALPHA"})
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
}

func TestFailedMutationRollsBackContentTagsAndSearch(t *testing.T) {
	s := openTest(t)
	n := createTest(t, s, "Before failure #original")
	_, err := s.DB().Exec(`CREATE TRIGGER fail_revision BEFORE INSERT ON revisions WHEN new.version>1 BEGIN SELECT RAISE(ABORT,'simulated storage failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	content := "After failure #replacement"
	if _, err = s.Update(testCtx, n.ID, UpdateInput{Version: 1, Content: &content}); err == nil {
		t.Fatal("failed write accepted")
	}
	got, _ := s.Get(testCtx, n.ID)
	if got.Content != n.Content || got.Version != 1 {
		t.Fatal("partial note update", got)
	}
	p, err := s.List(testCtx, model.Query{Tag: "original", Q: "Before"})
	if err != nil || p.Total != 1 {
		t.Fatal("partial derived update", p, err)
	}
	history, _ := s.History(testCtx, n.ID)
	if len(history) != 1 {
		t.Fatal(history)
	}
}
func TestMigrationBackfillsSortableDates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	n := createTest(t, s, "A durable original #migration")
	_, err = s.DB().Exec(`DROP INDEX notes_timeline; ALTER TABLE notes DROP COLUMN created_sort; CREATE INDEX notes_timeline ON notes(state,pinned DESC,created_at DESC,id DESC); PRAGMA user_version=1`)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Get(testCtx, n.ID)
	if err != nil || got.Content != n.Content || got.CreatedAt != n.CreatedAt {
		t.Fatal(got, err)
	}
	var sorted string
	if err = s.DB().QueryRow("SELECT created_sort FROM notes WHERE id=?", n.ID).Scan(&sorted); err != nil || sorted != sortTime(n.CreatedAt) {
		t.Fatal(sorted, err)
	}
}

func TestSearchCountAppliesStateTagAndPinnedFilters(t *testing.T) {
	s := openTest(t)
	a := createTest(t, s, "Matched token #alpha")
	b := createTest(t, s, "Matched token #beta")
	pin := true
	archived := "archived"
	if _, err := s.Update(testCtx, a.ID, UpdateInput{Pinned: &pin, Version: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(testCtx, b.ID, UpdateInput{Pinned: &pin, State: &archived, Version: 1}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []model.Query{{Q: "token", Tag: "alpha", Pinned: true}, {Q: "token", Tag: "beta", Pinned: true, State: "archived"}} {
		page, err := s.List(testCtx, q)
		if err != nil || page.Total != 1 || len(page.Notes) != 1 {
			t.Fatalf("filtered count/page mismatch: %+v %v", page, err)
		}
	}
	page, err := s.List(testCtx, model.Query{Q: "token", Tag: "beta", Pinned: true})
	if err != nil || page.Total != 0 || len(page.Notes) != 0 {
		t.Fatalf("archived result leaked: %+v %v", page, err)
	}
}
