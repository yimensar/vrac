package engine

import (
	"database/sql"
	"encoding/json"
	"strings"
)

func (s *txState) insert(parent sql.NullInt64, pos int64, text string, typeIDs []int64, f fields, aliases []string, day string) (int64, string, error) {
	id := newID()
	r, err := s.tx.ExecContext(s.ctx, `INSERT INTO nodes(id, kind, parent_seq, position, text, name_key, status, due, start_at, end_at, day, revision,
		created_at, updated_at, created_by, created_source, updated_by, updated_source)
		VALUES (?, 'node', ?, ?, ?, ?, nullif(?, ''), nullif(?, ''), nullif(?, ''), nullif(?, ''), ?, 1, ?, ?, ?, ?, ?, ?)`,
		id, parent, pos, text, nameKeyOf(text), f.Status, f.Due, f.Start, f.End, day, s.now, s.now, s.actor.Name, s.actor.Source, s.actor.Name, s.actor.Source)
	if err != nil {
		return 0, "", err
	}
	seq, _ := r.LastInsertId()
	if err := s.setTypes(seq, typeIDs); err != nil {
		return 0, "", err
	}
	if err := s.setAliases(seq, aliases); err != nil {
		return 0, "", err
	}
	if err := s.setRefs(seq, text); err != nil {
		return 0, "", err
	}
	if err := s.index(seq); err != nil {
		return 0, "", err
	}
	return seq, id, s.record(seq, "create")
}

func nameKeyOf(text string) any {
	name := untyped(text)
	if len(refIDs(text)) > 0 || len(name) > maxNameKeyLen || name == "" {
		return nil
	}
	return nameKey(name)
}

func (s *txState) setTypes(seq int64, typeIDs []int64) error {
	if _, err := s.tx.ExecContext(s.ctx, `DELETE FROM node_types WHERE node_seq = ?`, seq); err != nil {
		return err
	}
	for _, id := range typeIDs {
		if _, err := s.tx.ExecContext(s.ctx, `INSERT OR IGNORE INTO node_types(node_seq, type_id) VALUES (?, ?)`, seq, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *txState) setAliases(seq int64, aliases []string) error {
	if _, err := s.tx.ExecContext(s.ctx, `DELETE FROM aliases WHERE node_seq = ?`, seq); err != nil {
		return err
	}
	for _, a := range aliases {
		a = strings.Join(strings.Fields(a), " ")
		if a == "" || len(a) > maxAliasLen || strings.Contains(a, "[[") {
			return invalid("aliases have 1 to %d characters and no [[", maxAliasLen)
		}
		if _, err := s.tx.ExecContext(s.ctx, `INSERT OR IGNORE INTO aliases(node_seq, alias, alias_key) VALUES (?, ?, ?)`, seq, a, nameKey(a)); err != nil {
			return err
		}
	}
	return nil
}

func (s *txState) setRefs(seq int64, text string) error {
	if _, err := s.tx.ExecContext(s.ctx, `DELETE FROM refs WHERE source_seq = ?`, seq); err != nil {
		return err
	}
	for _, id := range refIDs(text) {
		r, err := s.tx.ExecContext(s.ctx, `INSERT OR IGNORE INTO refs(source_seq, target_seq) SELECT ?, seq FROM nodes WHERE id = ?`, seq, id)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return invalid("reference to unknown node %s", id)
		}
	}
	return nil
}

// index refreshes the full-text entry of a node: its text with referenced
// names, and its aliases.
func (s *txState) index(seq int64) error {
	var text string
	if err := s.tx.QueryRowContext(s.ctx, `SELECT text FROM nodes WHERE seq = ?`, seq).Scan(&text); err != nil {
		return err
	}
	refs, err := s.e.refMap(s.ctx, s.tx, refIDs(text))
	if err != nil {
		return err
	}
	body := typeTokenRe.ReplaceAllString(refTokenRe.ReplaceAllStringFunc(text, func(tok string) string {
		return refs[tok[2:len(tok)-2]].Name
	}), "$1$2")
	rows, err := s.tx.QueryContext(s.ctx, `SELECT alias FROM aliases WHERE node_seq = ?`, seq)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			rows.Close()
			return err
		}
		body += " " + a
	}
	rows.Close()
	if _, err := s.tx.ExecContext(s.ctx, `DELETE FROM search WHERE rowid = ?`, seq); err != nil {
		return err
	}
	_, err = s.tx.ExecContext(s.ctx, `INSERT INTO search(rowid, body) VALUES (?, ?)`, seq, body)
	return err
}

// snapshot is the recorded state of a node after a revision.
type snapshot struct {
	ParentID string   `json:"parent_id,omitempty"`
	Text     string   `json:"text"`
	Types    []string `json:"types"`
	Status   string   `json:"status,omitempty"`
	Due      string   `json:"due,omitempty"`
	Start    string   `json:"start,omitempty"`
	End      string   `json:"end,omitempty"`
	Aliases  []string `json:"aliases"`
	Trashed  bool     `json:"trashed,omitempty"`
}

func (s *txState) record(seq int64, action string) error {
	n, err := s.e.nodeBySeq(s.ctx, s.tx, seq)
	if err != nil {
		return err
	}
	nodes := []Node{n}
	if _, err := s.e.hydrate(s.ctx, s.tx, nodes); err != nil {
		return err
	}
	n = nodes[0]
	snap, _ := json.Marshal(snapshot{ParentID: n.ParentID, Text: n.Text, Types: n.Types, Status: n.Status, Due: n.Due, Start: n.Start, End: n.End, Aliases: n.Aliases, Trashed: n.Trashed})
	_, err = s.tx.ExecContext(s.ctx, `INSERT INTO revisions(node_seq, revision, action, at, actor, source, snapshot) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		seq, n.Revision, action, s.now, s.actor.Name, s.actor.Source, string(snap))
	return err
}

func (s *txState) bump(seq int64) error {
	_, err := s.tx.ExecContext(s.ctx, `UPDATE nodes SET revision = revision + 1, updated_at = ?, updated_by = ?, updated_source = ? WHERE seq = ?`,
		s.now, s.actor.Name, s.actor.Source, seq)
	return err
}

// reindex refreshes a node's search entry and, after a rename, the entries
// of the nodes that reference it.
func (s *txState) reindex(seq int64, renamed bool) error {
	if err := s.index(seq); err != nil {
		return err
	}
	if !renamed {
		return nil
	}
	rows, err := s.tx.QueryContext(s.ctx, `SELECT source_seq FROM refs WHERE target_seq = ?`, seq)
	if err != nil {
		return err
	}
	var sources []int64
	for rows.Next() {
		var src int64
		if err := rows.Scan(&src); err != nil {
			rows.Close()
			return err
		}
		sources = append(sources, src)
	}
	rows.Close()
	for _, src := range sources {
		if err := s.index(src); err != nil {
			return err
		}
	}
	return nil
}

// syncTypeTokens keeps the rule that a node's types are the #type tokens of
// its text: it appends the tokens of types the text does not show.
func (s *txState) syncTypeTokens(seq int64) error {
	var text string
	if err := s.tx.QueryRowContext(s.ctx, `SELECT text FROM nodes WHERE seq = ?`, seq).Scan(&text); err != nil {
		return err
	}
	rows, err := s.tx.QueryContext(s.ctx, `SELECT t.name FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE nt.node_seq = ? ORDER BY t.builtin DESC, t.id`, seq)
	if err != nil {
		return err
	}
	var types []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		types = append(types, t)
	}
	rows.Close()
	present := map[string]bool{}
	for _, m := range typeTokenRe.FindAllStringSubmatch(text, -1) {
		present[strings.ToLower(m[2])] = true
	}
	next := text
	for _, t := range types {
		if !present[t] {
			next = strings.TrimSpace(next + " #" + t)
		}
	}
	if next == text {
		return nil
	}
	_, err = s.tx.ExecContext(s.ctx, `UPDATE nodes SET text = ?, name_key = ? WHERE seq = ?`, next, nameKeyOf(next), seq)
	return err
}
