package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ExportFormat identifies Vrac exports.
const (
	ExportFormat  = "vrac"
	ExportVersion = 1
)

// Export is the versioned, complete JSON representation of a workspace,
// excluding authentication state.
type Export struct {
	Format     string           `json:"format"`
	Version    int              `json:"version"`
	ExportedAt string           `json:"exported_at"`
	Types      []ExportType     `json:"types"`
	Nodes      []ExportNode     `json:"nodes"`
	Revisions  []ExportRevision `json:"revisions"`
}

// ExportType is a type in an export.
type ExportType struct {
	Name      string `json:"name"`
	Builtin   bool   `json:"builtin"`
	Color     string `json:"color"`
	CreatedAt string `json:"created_at"`
}

// ExportNode is a node in an export. Parents precede their children.
type ExportNode struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	ParentID      string   `json:"parent_id,omitempty"`
	Position      int64    `json:"position"`
	Text          string   `json:"text"`
	Types         []string `json:"types"`
	Status        string   `json:"status,omitempty"`
	Due           string   `json:"due,omitempty"`
	Start         string   `json:"start,omitempty"`
	End           string   `json:"end,omitempty"`
	Day           string   `json:"day"`
	Aliases       []string `json:"aliases"`
	Revision      int64    `json:"revision"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
	CreatedBy     string   `json:"created_by"`
	CreatedSource string   `json:"created_source"`
	UpdatedBy     string   `json:"updated_by"`
	UpdatedSource string   `json:"updated_source"`
	TrashedAt     string   `json:"trashed_at,omitempty"`
	TrashRoot     string   `json:"trash_root,omitempty"`
}

// ExportRevision is a revision in an export.
type ExportRevision struct {
	NodeID   string          `json:"node_id"`
	Revision int64           `json:"revision"`
	Action   string          `json:"action"`
	At       string          `json:"at"`
	Actor    string          `json:"actor"`
	Source   string          `json:"source"`
	Snapshot json.RawMessage `json:"snapshot"`
}

// Export writes the workspace as JSON from one consistent read.
func (e *Engine) Export(ctx context.Context, w io.Writer) error {
	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	x := Export{Format: ExportFormat, Version: ExportVersion, ExportedAt: e.stamp(), Types: []ExportType{}, Nodes: []ExportNode{}, Revisions: []ExportRevision{}}
	rows, err := tx.QueryContext(ctx, `SELECT name, builtin, color, created_at FROM types ORDER BY id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var t ExportType
		if err := rows.Scan(&t.Name, &t.Builtin, &t.Color, &t.CreatedAt); err != nil {
			rows.Close()
			return err
		}
		x.Types = append(x.Types, t)
	}
	rows.Close()
	// Depth-first from the roots so parents always precede children.
	rows, err = tx.QueryContext(ctx, `WITH RECURSIVE tree(seq, path) AS (
			SELECT seq, printf('%020d', seq) FROM nodes WHERE parent_seq IS NULL
			UNION ALL SELECT n.seq, tree.path || '/' || printf('%020d', n.seq) FROM nodes n JOIN tree ON n.parent_seq = tree.seq)
		SELECT n.id, n.kind, coalesce(p.id, ''), n.position, n.text, coalesce(n.status, ''), coalesce(n.due, ''), coalesce(n.start_at, ''), coalesce(n.end_at, ''),
			n.day, n.revision, n.created_at, n.updated_at, n.created_by, n.created_source, n.updated_by, n.updated_source,
			coalesce(n.trashed_at, ''), coalesce(r.id, ''),
			coalesce((SELECT group_concat(t.name, ' ') FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE nt.node_seq = n.seq), ''),
			coalesce((SELECT json_group_array(alias) FROM aliases WHERE node_seq = n.seq), '[]')
		FROM tree JOIN nodes n ON n.seq = tree.seq LEFT JOIN nodes p ON p.seq = n.parent_seq LEFT JOIN nodes r ON r.seq = n.trash_root
		ORDER BY tree.path`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var n ExportNode
		var types, aliases string
		if err := rows.Scan(&n.ID, &n.Kind, &n.ParentID, &n.Position, &n.Text, &n.Status, &n.Due, &n.Start, &n.End, &n.Day, &n.Revision,
			&n.CreatedAt, &n.UpdatedAt, &n.CreatedBy, &n.CreatedSource, &n.UpdatedBy, &n.UpdatedSource, &n.TrashedAt, &n.TrashRoot, &types, &aliases); err != nil {
			rows.Close()
			return err
		}
		n.Types = strings.Fields(types)
		if n.Types == nil {
			n.Types = []string{}
		}
		if err := json.Unmarshal([]byte(aliases), &n.Aliases); err != nil {
			rows.Close()
			return err
		}
		x.Nodes = append(x.Nodes, n)
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT n.id, r.revision, r.action, r.at, r.actor, r.source, r.snapshot FROM revisions r JOIN nodes n ON n.seq = r.node_seq ORDER BY r.node_seq, r.revision`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r ExportRevision
		var snap string
		if err := rows.Scan(&r.NodeID, &r.Revision, &r.Action, &r.At, &r.Actor, &r.Source, &snap); err != nil {
			rows.Close()
			return err
		}
		r.Snapshot = json.RawMessage(snap)
		x.Revisions = append(x.Revisions, r)
	}
	rows.Close()
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	return enc.Encode(x)
}

// Import loads an export into an empty workspace, atomically. It validates
// structure, references and types, then rebuilds derived indexes.
func (e *Engine) Import(ctx context.Context, r io.Reader) error {
	var x Export
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&x); err != nil {
		return invalid("malformed export: %v", err)
	}
	if x.Format != ExportFormat || x.Version != ExportVersion {
		return invalid("unsupported export %q version %d", x.Format, x.Version)
	}
	err := e.write(ctx, func(tx *sql.Tx) error {
		var nodes, days int
		if err := tx.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE kind = 'day') FROM nodes`).Scan(&nodes, &days); err != nil {
			return err
		}
		if nodes != 1 || days != 0 {
			return fmt.Errorf("%w: import requires an empty workspace", ErrConflict)
		}
		for _, stmt := range []string{`DELETE FROM concerns`, `DELETE FROM revisions`, `DELETE FROM requests`, `DELETE FROM search`, `DELETE FROM nodes`, `DELETE FROM types`} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		s := &txState{e: e, ctx: ctx, tx: tx, typeIDs: map[string]int64{}}
		for _, t := range x.Types {
			if err := validTypeName(t.Name); err != nil {
				return err
			}
			r, err := tx.ExecContext(ctx, `INSERT INTO types(name, builtin, color, created_at) VALUES (?, ?, ?, ?)`, t.Name, t.Builtin, t.Color, t.CreatedAt)
			if err != nil {
				return invalid("type %s: %v", t.Name, err)
			}
			s.typeIDs[t.Name], _ = r.LastInsertId()
		}
		for _, b := range builtinTypes {
			if _, ok := s.typeIDs[b]; !ok {
				return invalid("built-in type %s is missing", b)
			}
		}
		seqs := map[string]int64{}
		journals := 0
		for _, n := range x.Nodes {
			if !isID(n.ID) {
				return invalid("node id %q is malformed", n.ID)
			}
			if _, dup := seqs[n.ID]; dup {
				return invalid("node %s appears twice", n.ID)
			}
			parent := sql.NullInt64{}
			if n.ParentID != "" {
				p, ok := seqs[n.ParentID]
				if !ok {
					return invalid("node %s precedes or lacks its parent %s", n.ID, n.ParentID)
				}
				parent = sql.NullInt64{Int64: p, Valid: true}
			}
			switch n.Kind {
			case KindJournal:
				journals++
				if parent.Valid {
					return invalid("the journal root has no parent")
				}
			case KindDay:
				if parent.Int64 != seqs[firstJournal(x.Nodes)] || validDate(n.Day) != nil || n.Text != n.Day {
					return invalid("day %s is malformed", n.ID)
				}
			case KindNode:
				if validDate(n.Day) != nil {
					return invalid("node %s has a malformed day", n.ID)
				}
			default:
				return invalid("node %s has unknown kind %q", n.ID, n.Kind)
			}
			r, err := tx.ExecContext(ctx, `INSERT INTO nodes(id, kind, parent_seq, position, text, name_key, status, due, start_at, end_at, day, revision,
				created_at, updated_at, created_by, created_source, updated_by, updated_source, trashed_at)
				VALUES (?, ?, ?, ?, ?, ?, nullif(?, ''), nullif(?, ''), nullif(?, ''), nullif(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, nullif(?, ''))`,
				n.ID, n.Kind, parent, n.Position, n.Text, nameKeyOfKind(n.Kind, n.Text), n.Status, n.Due, n.Start, n.End, n.Day, n.Revision,
				n.CreatedAt, n.UpdatedAt, n.CreatedBy, n.CreatedSource, n.UpdatedBy, n.UpdatedSource, n.TrashedAt)
			if err != nil {
				return invalid("node %s: %v", n.ID, err)
			}
			seq, _ := r.LastInsertId()
			seqs[n.ID] = seq
			var ids []int64
			for _, t := range n.Types {
				id, ok := s.typeIDs[t]
				if !ok {
					return invalid("node %s uses unknown type %s", n.ID, t)
				}
				ids = append(ids, id)
			}
			if err := s.setTypes(seq, ids); err != nil {
				return err
			}
			if err := s.setAliases(seq, n.Aliases); err != nil {
				return err
			}
		}
		if journals != 1 {
			return invalid("an export has exactly one journal root")
		}
		for _, n := range x.Nodes {
			seq := seqs[n.ID]
			if n.TrashRoot != "" {
				root, ok := seqs[n.TrashRoot]
				if !ok {
					return invalid("node %s has an unknown trash root", n.ID)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE nodes SET trash_root = ? WHERE seq = ?`, root, seq); err != nil {
					return err
				}
			}
			if err := s.setRefs(seq, n.Text); err != nil {
				return fmt.Errorf("node %s: %w", n.ID, err)
			}
		}
		if err := rebuildContext(ctx, tx); err != nil {
			return err
		}
		for _, n := range x.Nodes {
			if err := s.syncTypeTokens(seqs[n.ID]); err != nil {
				return err
			}
			if err := s.index(seqs[n.ID]); err != nil {
				return err
			}
		}
		for _, r := range x.Revisions {
			seq, ok := seqs[r.NodeID]
			if !ok {
				return invalid("revision of unknown node %s", r.NodeID)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO revisions(node_seq, revision, action, at, actor, source, snapshot) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				seq, r.Revision, r.Action, r.At, r.Actor, r.Source, string(r.Snapshot)); err != nil {
				return invalid("revision %s/%d: %v", r.NodeID, r.Revision, err)
			}
		}
		var fk int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&fk); err != nil {
			return err
		}
		if fk != 0 {
			return invalid("export violates foreign keys")
		}
		return tx.QueryRowContext(ctx, `SELECT seq FROM nodes WHERE kind = 'journal'`).Scan(&e.journal)
	})
	if err != nil {
		e.db.QueryRowContext(ctx, `SELECT seq FROM nodes WHERE kind = 'journal'`).Scan(&e.journal)
		return err
	}
	return e.Check()
}

func firstJournal(nodes []ExportNode) string {
	for _, n := range nodes {
		if n.Kind == KindJournal {
			return n.ID
		}
	}
	return ""
}

func nameKeyOfKind(kind, text string) any {
	if kind != KindNode {
		return nil
	}
	return nameKeyOf(text)
}
