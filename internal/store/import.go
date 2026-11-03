package store

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yimensar/vrac/internal/model"
)

type ImportReport struct {
	Total         int    `json:"total"`
	Imported      int    `json:"imported"`
	Skipped       int    `json:"skipped"`
	Active        int    `json:"active"`
	Archived      int    `json:"archived"`
	Attachments   int    `json:"attachments"`
	ContentSHA256 string `json:"contentSha256"`
}
type ExportNote struct {
	model.Note
	RawSource json.RawMessage `json:"rawSource,omitempty"`
}
type ExportData struct {
	Format     string       `json:"format"`
	Version    int          `json:"version"`
	ExportedAt string       `json:"exportedAt"`
	Notes      []ExportNote `json:"notes"`
}

type memosNote struct {
	Name        string   `json:"name"`
	UID         string   `json:"uid"`
	Content     string   `json:"content"`
	CreateTime  string   `json:"createTime"`
	UpdateTime  string   `json:"updateTime"`
	State       string   `json:"state"`
	Pinned      bool     `json:"pinned"`
	Tags        []string `json:"tags"`
	Attachments []struct {
		Name         string          `json:"name"`
		UID          string          `json:"uid"`
		Filename     string          `json:"filename"`
		Type         string          `json:"type"`
		Size         json.RawMessage `json:"size"`
		ExternalLink string          `json:"externalLink"`
	} `json:"attachments"`
	Relations []struct {
		Memo struct {
			Name string `json:"name"`
		} `json:"memo"`
		RelatedMemo struct {
			Name string `json:"name"`
		} `json:"relatedMemo"`
		Type string `json:"type"`
	} `json:"relations"`
}

func decodeMemos(raw json.RawMessage) (model.Note, error) {
	var m memosNote
	if err := json.Unmarshal(raw, &m); err != nil {
		return model.Note{}, err
	}
	if m.UID == "" {
		m.UID = strings.TrimPrefix(m.Name, "memos/")
	}
	if m.UID == "" || strings.ContainsAny(m.UID, "/\\\x00") {
		return model.Note{}, fmt.Errorf("invalid Memos UID")
	}
	if m.Name == "" {
		m.Name = "memos/" + m.UID
	}
	if _, err := time.Parse(time.RFC3339Nano, m.CreateTime); err != nil {
		return model.Note{}, fmt.Errorf("invalid Memos creation time for %s", m.UID)
	}
	if m.UpdateTime == "" {
		m.UpdateTime = m.CreateTime
	}
	if _, err := time.Parse(time.RFC3339Nano, m.UpdateTime); err != nil {
		return model.Note{}, fmt.Errorf("invalid Memos update time for %s", m.UID)
	}
	state := "active"
	if m.State == "ARCHIVED" || m.State == "archived" {
		state = "archived"
	}
	if m.State == "DELETED" || m.State == "deleted" {
		state = "deleted"
	}
	n := model.Note{ID: m.UID, Content: m.Content, Title: Title(m.Content), Tags: mergeTags(m.Tags, ExtractTags(m.Content)), CreatedAt: m.CreateTime, UpdatedAt: m.UpdateTime, State: state, Pinned: m.Pinned, Version: 1, Source: "memos", SourceID: m.Name, Attachments: []model.Attachment{}, Relations: []model.Relation{}, RawSource: append(json.RawMessage{}, raw...)}
	for _, a := range m.Attachments {
		if a.UID == "" {
			a.UID = strings.TrimPrefix(a.Name, "attachments/")
		}
		if !safeComponent(a.UID) || !safeComponent(a.Filename) {
			return model.Note{}, fmt.Errorf("unsafe attachment filename for %s", m.UID)
		}
		var size int64
		sizeText := strings.Trim(string(a.Size), `"`)
		if sizeText != "" {
			var err error
			size, err = strconv.ParseInt(sizeText, 10, 64)
			if err != nil || size < 0 {
				return model.Note{}, fmt.Errorf("invalid attachment size")
			}
		}
		n.Attachments = append(n.Attachments, model.Attachment{ID: a.UID, Filename: a.Filename, Type: a.Type, Size: size, URL: "/api/attachments/" + a.UID})
	}
	for _, r := range m.Relations {
		target := r.RelatedMemo.Name
		kind := r.Type
		if r.Memo.Name != m.Name {
			target = r.Memo.Name
			kind = "BACKLINK"
		}
		target = strings.TrimPrefix(target, "memos/")
		if target != "" {
			n.Relations = append(n.Relations, model.Relation{TargetID: target, Type: kind})
		}
	}
	return n, nil
}
func safeComponent(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00")
}
func mergeTags(a, b []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, tags := range [][]string{a, b} {
		for _, t := range tags {
			t = strings.TrimSpace(t)
			key := strings.ToLower(t)
			if t != "" && !seen[key] {
				out = append(out, t)
				seen[key] = true
			}
		}
	}
	sort.Strings(out)
	return out
}
func (s *Store) ImportMemos(ctx context.Context, raws []json.RawMessage) (ImportReport, error) {
	report := ImportReport{Total: len(raws)}
	notes := make([]model.Note, 0, len(raws))
	seen := map[string]bool{}
	for _, raw := range raws {
		n, err := decodeMemos(raw)
		if err != nil {
			return report, err
		}
		if seen[n.ID] {
			return report, fmt.Errorf("duplicate note ID in import: %s", n.ID)
		}
		seen[n.ID] = true
		notes = append(notes, n)
		if n.State == "active" {
			report.Active++
		}
		if n.State == "archived" {
			report.Archived++
		}
		report.Attachments += len(n.Attachments)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	for _, n := range notes {
		var existingSource string
		err = tx.QueryRowContext(ctx, "SELECT source_id FROM notes WHERE id=?", n.ID).Scan(&existingSource)
		if err == nil {
			if existingSource != n.SourceID {
				return report, fmt.Errorf("%w: import ID collision", ErrConflict)
			}
			// Titles are derived presentation data. Refresh them when the title
			// parser improves, without overwriting edited content or dates.
			var currentContent string
			if err = tx.QueryRowContext(ctx, "SELECT content FROM notes WHERE id=?", n.ID).Scan(&currentContent); err != nil {
				return report, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE notes SET title=? WHERE id=? AND title<>?", Title(currentContent), n.ID, Title(currentContent)); err != nil {
				return report, err
			}
			report.Skipped++
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return report, err
		}
		if err = insertNote(ctx, tx, n); err != nil {
			return report, err
		}
		report.Imported++
	}
	if err = tx.Commit(); err != nil {
		return ImportReport{}, err
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].ID < notes[j].ID })
	digest := sha256.New()
	for _, n := range notes {
		digest.Write([]byte(n.ID))
		digest.Write([]byte{0})
		digest.Write([]byte(n.Content))
		digest.Write([]byte{0})
	}
	report.ContentSHA256 = hex.EncodeToString(digest.Sum(nil))
	return report, nil
}
func (s *Store) Export(ctx context.Context) (ExportData, error) {
	out := ExportData{Format: "vrac-export", Version: 1, ExportedAt: now(), Notes: []ExportNote{}}
	rows, err := s.db.QueryContext(ctx, "SELECT "+columns+" FROM notes ORDER BY id")
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return out, err
		}
		out.Notes = append(out.Notes, ExportNote{Note: n, RawSource: n.RawSource})
	}
	return out, rows.Err()
}
func (s *Store) Attachment(ctx context.Context, id string) (model.Attachment, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT j.value FROM notes n,json_each(n.attachments) j WHERE json_extract(j.value,'$.id')=? AND n.state<>'deleted' LIMIT 1", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Attachment{}, ErrNotFound
	}
	if err != nil {
		return model.Attachment{}, err
	}
	var a model.Attachment
	err = json.Unmarshal([]byte(raw), &a)
	return a, err
}

// ReadMemosJSON accepts the connector response envelope or a plain memo array.
func ReadMemosJSON(data []byte) ([]json.RawMessage, error) {
	var envelope struct {
		Memos []json.RawMessage `json:"memos"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Memos != nil {
		return envelope.Memos, nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, fmt.Errorf("expected a Memos array or {memos:[...]}: %w", err)
	}
	return raws, nil
}

type zipMemo struct {
	ContentPath string `json:"contentPath"`
	Attachments []struct {
		UID      string `json:"uid"`
		Filename string `json:"filename"`
		Path     string `json:"path"`
		SHA256   string `json:"sha256"`
		Size     int64  `json:"size"`
	} `json:"attachments"`
}

// ReadMemosZIP verifies the complete export before writing attachments. Markdown
// stays byte-for-byte identical. Metadata may enrich relations omitted by ZIPs.
func ReadMemosZIP(filename, attachmentDir string, metadata []json.RawMessage) ([]json.RawMessage, error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	files := map[string]*zip.File{}
	for _, f := range z.File {
		if strings.HasPrefix(f.Name, "/") || strings.HasPrefix(f.Name, "../") || f.Name == ".." || strings.Contains(f.Name, "\\") || path.Clean(f.Name) != strings.TrimSuffix(f.Name, "/") {
			return nil, fmt.Errorf("unsafe ZIP path")
		}
		if _, exists := files[f.Name]; exists {
			return nil, fmt.Errorf("duplicate ZIP path")
		}
		if f.UncompressedSize64 > 64*1024*1024 {
			return nil, fmt.Errorf("ZIP member exceeds 64 MiB")
		}
		files[f.Name] = f
	}
	read := func(name string) ([]byte, error) {
		f, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("missing export member: %s", name)
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(io.LimitReader(r, 64*1024*1024+1))
	}
	manifestBytes, err := read("manifest.json")
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Format  string `json:"format"`
		Version string `json:"formatVersion"`
		Counts  struct {
			Memos       int `json:"memos"`
			Attachments int `json:"attachments"`
		} `json:"counts"`
	}
	if json.Unmarshal(manifestBytes, &manifest) != nil || manifest.Format != "memos-export" || manifest.Version != "1.0" {
		return nil, fmt.Errorf("unsupported Memos export format")
	}
	metaByID := map[string]map[string]json.RawMessage{}
	for _, raw := range metadata {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		var name string
		json.Unmarshal(obj["name"], &name)
		metaByID[strings.TrimPrefix(name, "memos/")] = obj
	}
	type attachmentFile struct {
		id, filename string
		bytes        []byte
	}
	attachments := []attachmentFile{}
	raws := []json.RawMessage{}
	paths := []string{}
	for name := range files {
		if strings.HasPrefix(name, "memos/") && strings.HasSuffix(name, ".json") {
			paths = append(paths, name)
		}
	}
	sort.Strings(paths)
	for _, name := range paths {
		b, err := read(name)
		if err != nil {
			return nil, err
		}
		var m zipMemo
		if err = json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(m.ContentPath, "memos/") || !strings.HasSuffix(m.ContentPath, ".md") {
			return nil, fmt.Errorf("invalid Markdown member")
		}
		content, err := read(m.ContentPath)
		if err != nil {
			return nil, err
		}
		var obj map[string]json.RawMessage
		json.Unmarshal(b, &obj)
		obj["content"], _ = json.Marshal(string(content))
		var uid string
		json.Unmarshal(obj["uid"], &uid)
		if meta := metaByID[uid]; meta != nil {
			if rel := meta["relations"]; rel != nil {
				obj["relations"] = rel
			}
		}
		raw, _ := json.Marshal(obj)
		if _, err = decodeMemos(raw); err != nil {
			return nil, err
		}
		raws = append(raws, raw)
		for _, a := range m.Attachments {
			if !safeComponent(a.UID) || !safeComponent(a.Filename) || a.Path != "attachments/"+a.UID+"/"+a.Filename {
				return nil, fmt.Errorf("invalid attachment member")
			}
			content, err := read(a.Path)
			if err != nil {
				return nil, err
			}
			sum := sha256.Sum256(content)
			if a.Size != int64(len(content)) || !strings.EqualFold(a.SHA256, hex.EncodeToString(sum[:])) {
				return nil, fmt.Errorf("attachment integrity check failed for %s", a.UID)
			}
			attachments = append(attachments, attachmentFile{a.UID, a.Filename, content})
		}
	}
	if len(raws) != manifest.Counts.Memos || len(attachments) != manifest.Counts.Attachments {
		return nil, fmt.Errorf("export count mismatch")
	}
	if len(attachments) > 0 && attachmentDir == "" {
		return nil, fmt.Errorf("attachment directory is required for a complete ZIP import")
	}
	for _, a := range attachments {
		dir := filepath.Join(attachmentDir, a.id)
		if err = os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
		dest := filepath.Join(dir, a.filename)
		if existing, err := os.ReadFile(dest); err == nil {
			if hash(string(existing)) != hash(string(a.bytes)) {
				return nil, fmt.Errorf("attachment already exists with different bytes: %s", a.id)
			}
			continue
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		_, writeErr := file.Write(a.bytes)
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil {
			os.Remove(dest)
			return nil, writeErr
		}
		if closeErr != nil {
			os.Remove(dest)
			return nil, closeErr
		}
	}
	return raws, nil
}
