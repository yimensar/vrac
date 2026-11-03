package engine

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func checkDatabase(q queryer) (CheckReport, error) {
	report := CheckReport{Issues: []string{}}
	rows, err := q.Query("PRAGMA integrity_check")
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var message string
		if err = rows.Scan(&message); err != nil {
			rows.Close()
			return report, err
		}
		if message != "ok" {
			report.Issues = append(report.Issues, message)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return report, err
	}
	rows, err = q.Query("PRAGMA foreign_key_check")
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var table, parent string
		var row sql.NullInt64
		var fk int
		if err = rows.Scan(&table, &row, &parent, &fk); err != nil {
			rows.Close()
			return report, err
		}
		report.Issues = append(report.Issues, fmt.Sprintf("foreign key failure in %s to %s", table, parent))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return report, err
	}
	var reachable int64
	if err = q.QueryRow("SELECT count(*) FROM nodes").Scan(&report.NodeCount); err != nil {
		return report, err
	}
	if err = q.QueryRow(`WITH RECURSIVE reachable(id) AS (SELECT id FROM nodes WHERE parent_id IS NULL UNION SELECT n.id FROM nodes n JOIN reachable p ON n.parent_id=p.id) SELECT count(*) FROM reachable`).Scan(&reachable); err != nil {
		return report, err
	}
	if reachable != report.NodeCount {
		report.Issues = append(report.Issues, "nodes contain a cycle or are unreachable from a root")
	}
	var branchMismatch int
	if err = q.QueryRow(`SELECT count(*) FROM branches b LEFT JOIN nodes n ON n.id=b.node_id WHERE n.id IS NULL OR n.deleted=1 OR b.parent_id IS NOT n.parent_id OR NOT EXISTS(SELECT 1 FROM nodes child WHERE child.parent_id=b.node_id AND child.deleted=0)`).Scan(&branchMismatch); err != nil {
		return report, err
	}
	if branchMismatch > 0 {
		report.Issues = append(report.Issues, "derived branch index contains incorrect entries")
	}
	if err = q.QueryRow(`SELECT count(*) FROM nodes n WHERE n.deleted=0 AND EXISTS(SELECT 1 FROM nodes child WHERE child.parent_id=n.id AND child.deleted=0) AND NOT EXISTS(SELECT 1 FROM branches b WHERE b.node_id=n.id)`).Scan(&branchMismatch); err != nil {
		return report, err
	}
	if branchMismatch > 0 {
		report.Issues = append(report.Issues, "derived branch index is missing entries")
	}
	var invalidRevision int
	if err = q.QueryRow(`SELECT count(*) FROM nodes n LEFT JOIN revisions r ON r.node_id=n.id AND r.revision=n.revision WHERE n.system IS NULL AND (r.node_id IS NULL OR json_extract(r.snapshot,'$.id') IS NOT n.id OR json_extract(r.snapshot,'$.revision') IS NOT n.revision OR json_extract(r.snapshot,'$.text') IS NOT n.text OR json_extract(r.snapshot,'$.parent_id') IS NOT n.parent_id)`).Scan(&invalidRevision); err != nil {
		return report, err
	}
	if invalidRevision > 0 {
		report.Issues = append(report.Issues, "current nodes have missing or inconsistent revision snapshots")
	}
	if err = q.QueryRow(`SELECT count(*) FROM revisions WHERE json_extract(snapshot,'$.id') IS NOT node_id OR json_extract(snapshot,'$.revision') IS NOT revision`).Scan(&invalidRevision); err != nil {
		return report, err
	}
	if invalidRevision > 0 {
		report.Issues = append(report.Issues, "historical revision identities are inconsistent")
	}
	var count int
	if err = q.QueryRow("SELECT count(*) FROM nodes WHERE system='journal' AND parent_id IS NULL AND deleted=0").Scan(&count); err != nil {
		return report, err
	}
	if count != 1 {
		report.Issues = append(report.Issues, "expected exactly one active root Journal")
	}
	rows, err = q.Query(`SELECT n.id,n.system,COALESCE(p.system,'') FROM nodes n LEFT JOIN nodes p ON p.id=n.parent_id WHERE n.system IS NOT NULL`)
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var id, system, parent string
		if err = rows.Scan(&id, &system, &parent); err != nil {
			rows.Close()
			return report, err
		}
		if system == "journal" {
			continue
		}
		if !strings.HasPrefix(system, "day:") || validDate(strings.TrimPrefix(system, "day:")) != nil || parent != "journal" {
			report.Issues = append(report.Issues, "invalid protected system node "+id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return report, err
	}
	if err = q.QueryRow("SELECT count(*) FROM nodes n JOIN nodes p ON p.id=n.parent_id WHERE n.deleted=0 AND p.deleted=1").Scan(&count); err != nil {
		return report, err
	}
	if count > 0 {
		report.Issues = append(report.Issues, "active nodes have archived parents")
	}
	rows, err = q.Query("SELECT DISTINCT tag FROM node_tags")
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var tag string
		if err = rows.Scan(&tag); err != nil {
			rows.Close()
			return report, err
		}
		canonical, err := CanonicalTag(tag)
		if err != nil || canonical != tag {
			report.Issues = append(report.Issues, "noncanonical tag "+tag)
		}
	}
	err = rows.Err()
	rows.Close()
	return report, err
}
func (e *Engine) Check() (CheckReport, error) {
	var report CheckReport
	err := e.mutate("check", "", nil, nil, func(tx *sql.Tx) error {
		var err error
		report, err = checkDatabase(tx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO node_fts(node_fts,rank) VALUES('integrity-check',1)"); err != nil {
			report.Issues = append(report.Issues, "full-text index integrity: "+err.Error())
		}
		return nil
	})
	return report, err
}
func (e *Engine) RebuildSearch() error {
	return e.mutate("rebuild-search", "", nil, nil, func(tx *sql.Tx) error {
		if err := rebuildBranches(tx); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO node_fts(node_fts) VALUES('rebuild')")
		return err
	})
}
func (e *Engine) Backup(destination string) error {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(abs); err == nil {
		return fmt.Errorf("%w: backup destination already exists", ErrConflict)
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(abs), ".vrac-backup-*.sqlite")
	if err != nil {
		return err
	}
	temporary := f.Name()
	f.Close()
	os.Remove(temporary)
	defer os.Remove(temporary)
	if _, err = e.db.Exec("VACUUM INTO ?", temporary); err != nil {
		return fmt.Errorf("create SQLite snapshot: %w", err)
	}
	if err = os.Chmod(temporary, 0600); err != nil {
		return err
	}
	backup, err := Open(temporary)
	if err != nil {
		return err
	}
	report, checkErr := backup.Check()
	if checkErr == nil {
		_, checkErr = backup.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	}
	closeErr := backup.Close()
	if checkErr != nil {
		return checkErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(report.Issues) > 0 {
		return fmt.Errorf("backup integrity failed: %s", strings.Join(report.Issues, "; "))
	}
	snapshot, err := os.OpenFile(temporary, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err = snapshot.Sync(); err != nil {
		snapshot.Close()
		return err
	}
	if err = snapshot.Close(); err != nil {
		return err
	}
	// Link publishes without ever overwriting an existing recoverable copy.
	if err = os.Link(temporary, abs); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(abs))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

type exportTable struct{ name, query string }

var exportTables = []exportTable{
	{"nodes", `SELECT json_object('id',id,'parent_id',parent_id,'position',position,'text',text,'revision',revision,'created_at',created_at,'updated_at',updated_at,'system',system,'deleted',deleted) FROM nodes ORDER BY id`},
	{"tags", `SELECT json_object('node_id',node_id,'tag',tag) FROM node_tags ORDER BY node_id,tag`},
	{"references", `SELECT json_object('source_id',source_id,'token',token,'target_id',target_id) FROM node_references ORDER BY source_id,token`},
	{"revisions", `SELECT json_object('node_id',node_id,'revision',revision,'at',at,'summary',summary,'snapshot',json(snapshot)) FROM revisions ORDER BY node_id,revision`},
	{"activities", `SELECT json_object('node_id',node_id,'target_id',target_id,'target_revision',target_revision,'journal_date',journal_date) FROM activities ORDER BY node_id`},
	{"mutations", `SELECT json_object('request_id',request_id,'kind',kind,'payload_hash',payload_hash,'result',json(result)) FROM mutations ORDER BY request_id`},
}

// Export writes one consistent, versioned UTF-8 JSON document. Arrays are
// streamed so background recovery operations do not retain the whole tree.
func (e *Engine) Export(w io.Writer) error {
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	b := bufio.NewWriter(w)
	if _, err = io.WriteString(b, `{"format":"vrac-hosted","version":1`); err != nil {
		return err
	}
	for _, table := range exportTables {
		if _, err = fmt.Fprintf(b, ",\n%q:[\n", table.name); err != nil {
			return err
		}
		rows, err := tx.Query(table.query)
		if err != nil {
			return err
		}
		first := true
		for rows.Next() {
			var row string
			if err = rows.Scan(&row); err != nil {
				rows.Close()
				return err
			}
			if !first {
				if _, err = io.WriteString(b, ",\n"); err != nil {
					rows.Close()
					return err
				}
			}
			if _, err = io.WriteString(b, row); err != nil {
				rows.Close()
				return err
			}
			first = false
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if _, err = io.WriteString(b, "\n]"); err != nil {
			return err
		}
	}
	if _, err = io.WriteString(b, "\n}\n"); err != nil {
		return err
	}
	if err = b.Flush(); err != nil {
		return err
	}
	return tx.Commit()
}

// Import restores a complete export into a fresh Vrac database. It never
// overwrites populated data and validates integrity before atomic publication.
func (e *Engine) Import(r io.Reader) error {
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRow("SELECT count(*) FROM nodes WHERE system IS NULL OR system<>'journal'").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("%w: import requires a fresh database", ErrConflict)
	}
	if _, err = tx.Exec("PRAGMA defer_foreign_keys=ON"); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM nodes"); err != nil {
		return err
	}
	decoder := json.NewDecoder(r)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("%w: expected a Vrac JSON export", ErrInvalid)
	}
	seen := map[string]bool{}
	format := ""
	version := 0
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return fmt.Errorf("%w: duplicate or invalid export key", ErrInvalid)
		}
		seen[key] = true
		switch key {
		case "format":
			if err = decoder.Decode(&format); err != nil {
				return err
			}
		case "version":
			if err = decoder.Decode(&version); err != nil {
				return err
			}
		default:
			known := false
			for _, table := range exportTables {
				if key == table.name {
					known = true
					break
				}
			}
			if !known {
				return fmt.Errorf("%w: unknown export field %q", ErrInvalid, key)
			}
			token, err = decoder.Token()
			if err != nil || token != json.Delim('[') {
				return fmt.Errorf("%w: export table must be an array", ErrInvalid)
			}
			for decoder.More() {
				var raw json.RawMessage
				if err = decoder.Decode(&raw); err != nil {
					return err
				}
				if err = importRow(tx, key, raw); err != nil {
					return fmt.Errorf("import %s: %w", key, err)
				}
			}
			if _, err = decoder.Token(); err != nil {
				return err
			}
		}
	}
	if _, err = decoder.Token(); err != nil {
		return err
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("%w: trailing data after export", ErrInvalid)
	}
	if format != "vrac-hosted" || version != 1 {
		return fmt.Errorf("%w: unsupported export format", ErrInvalid)
	}
	for _, table := range exportTables {
		if !seen[table.name] {
			return fmt.Errorf("%w: missing export table %s", ErrInvalid, table.name)
		}
	}
	if err = rebuildBranches(tx); err != nil {
		return err
	}
	report, err := checkDatabase(tx)
	if err != nil {
		return err
	}
	if len(report.Issues) > 0 {
		return fmt.Errorf("%w: imported data failed integrity: %s", ErrInvalid, strings.Join(report.Issues, "; "))
	}
	if _, err = tx.Exec("INSERT INTO node_fts(node_fts,rank) VALUES('integrity-check',1)"); err != nil {
		return fmt.Errorf("imported search index: %w", err)
	}
	return tx.Commit()
}
func importRow(tx *sql.Tx, table string, raw json.RawMessage) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("%w: export rows must be UTF-8", ErrInvalid)
	}
	var row struct {
		ID             string          `json:"id"`
		ParentID       *string         `json:"parent_id"`
		Position       int64           `json:"position"`
		Text           string          `json:"text"`
		Revision       int64           `json:"revision"`
		CreatedAt      string          `json:"created_at"`
		UpdatedAt      string          `json:"updated_at"`
		System         *string         `json:"system"`
		Deleted        int             `json:"deleted"`
		NodeID         string          `json:"node_id"`
		Tag            string          `json:"tag"`
		SourceID       string          `json:"source_id"`
		Token          string          `json:"token"`
		TargetID       string          `json:"target_id"`
		At             string          `json:"at"`
		Summary        string          `json:"summary"`
		Snapshot       json.RawMessage `json:"snapshot"`
		TargetRevision int64           `json:"target_revision"`
		JournalDate    string          `json:"journal_date"`
		RequestID      string          `json:"request_id"`
		Kind           string          `json:"kind"`
		PayloadHash    string          `json:"payload_hash"`
		Result         json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &row); err != nil {
		return err
	}
	var err error
	switch table {
	case "nodes":
		_, err = tx.Exec("INSERT INTO nodes(id,parent_id,position,text,revision,created_at,updated_at,system,deleted) VALUES(?,?,?,?,?,?,?,?,?)", row.ID, row.ParentID, row.Position, row.Text, row.Revision, row.CreatedAt, row.UpdatedAt, row.System, row.Deleted)
	case "tags":
		_, err = tx.Exec("INSERT INTO node_tags(node_id,tag) VALUES(?,?)", row.NodeID, row.Tag)
	case "references":
		_, err = tx.Exec("INSERT INTO node_references(source_id,token,target_id) VALUES(?,?,?)", row.SourceID, row.Token, row.TargetID)
	case "revisions":
		_, err = tx.Exec("INSERT INTO revisions(node_id,revision,at,summary,snapshot) VALUES(?,?,?,?,?)", row.NodeID, row.Revision, row.At, row.Summary, string(row.Snapshot))
	case "activities":
		_, err = tx.Exec("INSERT INTO activities(node_id,target_id,target_revision,journal_date) VALUES(?,?,?,?)", row.NodeID, row.TargetID, row.TargetRevision, row.JournalDate)
	case "mutations":
		_, err = tx.Exec("INSERT INTO mutations(request_id,kind,payload_hash,result) VALUES(?,?,?,?)", row.RequestID, row.Kind, row.PayloadHash, string(row.Result))
	}
	return err
}

func rebuildBranches(tx *sql.Tx) error {
	if _, err := tx.Exec("DELETE FROM branches"); err != nil {
		return err
	}
	_, err := tx.Exec("INSERT INTO branches(node_id,parent_id) SELECT n.id,n.parent_id FROM nodes n WHERE n.deleted=0 AND EXISTS(SELECT 1 FROM nodes child WHERE child.parent_id=n.id AND child.deleted=0)")
	return err
}
