package engine

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
)

// refMap describes the given node IDs for display.
func (e *Engine) refMap(ctx context.Context, q queryer, ids []string) (map[string]Ref, error) {
	refs := map[string]Ref{}
	unique := map[string]bool{}
	var args []any
	for _, id := range ids {
		if !unique[id] {
			unique[id] = true
			args = append(args, id)
		}
	}
	if len(args) == 0 {
		return refs, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT n.id, n.text, n.trashed_at IS NOT NULL,
		coalesce((SELECT group_concat(t.name, ' ') FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE nt.node_seq = n.seq), '')
		FROM nodes n WHERE n.id IN (`+placeholders(len(args))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Ref
		var types string
		if err := rows.Scan(&r.ID, &r.Name, &r.Trashed, &types); err != nil {
			return nil, err
		}
		r.Types = strings.Fields(types)
		if r.Types == nil {
			r.Types = []string{}
		}
		refs[r.ID] = r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	var names []*string
	list := make([]Ref, 0, len(refs))
	for _, r := range refs {
		list = append(list, r)
	}
	var labels []*string
	for i := range list {
		names = append(names, &list[i].Name)
		labels = append(labels, &list[i].Label)
	}
	if err := e.resolveNames(ctx, q, names, labels...); err != nil {
		return nil, err
	}
	for _, r := range list {
		refs[r.ID] = r
	}
	return refs, nil
}

// resolveNames turns canonical texts into names in place: references become
// the names of their targets, one level deep, and types are removed. When
// labels[i] is not nil it receives the label: the same text keeping the
// #type tokens written inside the sentence, for display.
func (e *Engine) resolveNames(ctx context.Context, q queryer, names []*string, labels ...*string) error {
	var ids []any
	seen := map[string]bool{}
	for _, n := range names {
		for _, id := range refIDs(*n) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	targets := map[string]string{}
	if len(ids) > 0 {
		rows, err := q.QueryContext(ctx, `SELECT id, text FROM nodes WHERE id IN (`+placeholders(len(ids))+`)`, ids...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, text string
			if err := rows.Scan(&id, &text); err != nil {
				rows.Close()
				return err
			}
			targets[id] = plainName(text)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	for i, n := range names {
		label := labelOf(refTokenRe.ReplaceAllStringFunc(*n, func(tok string) string {
			if t, ok := targets[tok[2:len(tok)-2]]; ok {
				return t
			}
			return "…"
		}))
		if i < len(labels) && labels[i] != nil {
			*labels[i] = label
		}
		*n = untyped(label)
	}
	return nil
}

var refTokenRe = regexp.MustCompile(`\[\[([a-z2-7]{26})\]\]`)

func refIDs(text string) []string {
	ids := []string{}
	seen := map[string]bool{}
	for _, m := range refTokenRe.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			ids = append(ids, m[1])
		}
	}
	return ids
}

// Render writes canonical text in the readable and writable form
// [[Name|ID]], using the references of a response.
func Render(text string, refs map[string]Ref) string {
	return refTokenRe.ReplaceAllStringFunc(text, func(tok string) string {
		id := tok[2 : len(tok)-2]
		if r, ok := refs[id]; ok {
			return "[[" + r.Name + "|" + id + "]]"
		}
		return tok
	})
}

// plainName renders canonical text without resolving nested references.
func plainName(text string) string {
	return untyped(refTokenRe.ReplaceAllString(text, "…"))
}

// typeTokenRe matches #type tokens of canonical text.
var typeTokenRe = regexp.MustCompile(`(^|\s)#([\p{L}\p{N}_-]+)`)

var trailingTypesRe = regexp.MustCompile(`(\s*(^|\s)#[\p{L}\p{N}_-]+)+\s*$`)

// labelOf drops the trailing type tokens of a text, which label the node,
// and keeps those written inside the sentence.
func labelOf(text string) string {
	// A node of several lines is named by its first one.
	text, _, _ = strings.Cut(text, "\n")
	return strings.TrimSpace(trailingTypesRe.ReplaceAllString(text, ""))
}

// untyped is text as a name: trailing type tokens are dropped and inner
// ones become plain words, so "Northwind #project" is named "Northwind" and "#meeting
// with Alice" is named "meeting with Alice".
func untyped(text string) string {
	text = labelOf(text)
	return strings.TrimSpace(typeTokenRe.ReplaceAllString(text, "$1$2"))
}

// Resolve returns nodes whose name or alias matches name, exact matches
// first, for autocomplete and for AI clients before writing.
func (e *Engine) Resolve(ctx context.Context, name string, limit int) ([]Brief, error) {
	limit = clampLimit(limit, 10, 50)
	return e.candidates(ctx, e.db, name, limit)
}

func (e *Engine) candidates(ctx context.Context, q queryer, name string, limit int) ([]Brief, error) {
	key := nameKey(name)
	var rows *sql.Rows
	var err error
	if key == "" {
		// Nothing typed yet: the subjects in use, the ones referenced most
		// recently first, then the typed nodes, so a menu opens full.
		rows, err = q.QueryContext(ctx, `SELECT n.id, n.text, coalesce((SELECT group_concat(t.name, ' ') FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE nt.node_seq = n.seq), ''), n.seq
			FROM nodes n
			WHERE n.kind = 'node' AND n.trashed_at IS NULL AND n.text != ''
			  AND (EXISTS (SELECT 1 FROM refs WHERE target_seq = n.seq) OR EXISTS (SELECT 1 FROM node_types WHERE node_seq = n.seq))
			ORDER BY EXISTS (SELECT 1 FROM refs WHERE target_seq = n.seq) DESC,
				coalesce((SELECT max(s.updated_at) FROM refs r JOIN nodes s ON s.seq = r.source_seq WHERE r.target_seq = n.seq), n.updated_at) DESC,
				coalesce((SELECT max(s.seq) FROM refs r JOIN nodes s ON s.seq = r.source_seq WHERE r.target_seq = n.seq), n.seq) DESC
			LIMIT ?`, limit)
	} else {
		// Names and aliases starting with the key, through their indexes;
		// words anywhere come next from full text.
		upper := key + "\U0010FFFF"
		rows, err = q.QueryContext(ctx, `WITH matches(seq, rank) AS (
			SELECT seq, CASE WHEN name_key = ?1 THEN 0 ELSE 2 END
			FROM nodes WHERE name_key >= ?1 AND name_key < ?2 AND kind = 'node' AND trashed_at IS NULL
			UNION ALL
			SELECT a.node_seq, CASE WHEN a.alias_key = ?1 THEN 1 ELSE 3 END FROM aliases a JOIN nodes n ON n.seq = a.node_seq
			WHERE a.alias_key >= ?1 AND a.alias_key < ?2 AND n.trashed_at IS NULL)
		SELECT n.id, n.text, coalesce((SELECT group_concat(t.name, ' ') FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE nt.node_seq = n.seq), ''),
			n.seq
		FROM (SELECT seq, min(rank) AS rank FROM matches GROUP BY seq) m JOIN nodes n ON n.seq = m.seq
		ORDER BY m.rank,
			(EXISTS (SELECT 1 FROM node_types WHERE node_seq = n.seq) OR n.parent_seq IS NULL OR EXISTS (SELECT 1 FROM refs WHERE target_seq = n.seq)) DESC,
			length(n.text), n.seq
		LIMIT ?3`, key, upper, limit)
	}
	if err != nil {
		return nil, err
	}
	type found struct {
		b   Brief
		seq int64
	}
	var list []found
	seen := map[int64]bool{}
	scan := func(rows *sql.Rows) error {
		defer rows.Close()
		for rows.Next() {
			var f found
			var types string
			if err := rows.Scan(&f.b.ID, &f.b.Name, &types, &f.seq); err != nil {
				return err
			}
			if seen[f.seq] {
				continue
			}
			seen[f.seq] = true
			f.b.Types = strings.Fields(types)
			if f.b.Types == nil {
				f.b.Types = []string{}
			}
			list = append(list, f)
		}
		return rows.Err()
	}
	if err := scan(rows); err != nil {
		return nil, err
	}
	// Then any node whose words match, such as a meeting or a task, typed
	// and recent ones first.
	if match := ftsQuery(name); key != "" && match != "" && len(list) < limit {
		rows, err := q.QueryContext(ctx, `SELECT n.id, n.text, coalesce((SELECT group_concat(t.name, ' ') FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE nt.node_seq = n.seq), ''), n.seq
			FROM search JOIN nodes n ON n.seq = search.rowid
			WHERE search MATCH ? AND n.kind = 'node' AND n.trashed_at IS NULL AND n.text != ''
			ORDER BY EXISTS (SELECT 1 FROM node_types WHERE node_seq = n.seq) DESC, n.updated_at DESC
			LIMIT ?`, match, limit)
		if err != nil {
			return nil, err
		}
		if err := scan(rows); err != nil {
			return nil, err
		}
		if len(list) > limit {
			list = list[:limit]
		}
	}
	var names, labels []*string
	for i := range list {
		names = append(names, &list[i].b.Name)
		labels = append(labels, &list[i].b.Label)
	}
	if err := e.resolveNames(ctx, q, names, labels...); err != nil {
		return nil, err
	}
	out := make([]Brief, 0, len(list))
	for _, f := range list {
		path, err := e.path(ctx, q, f.seq)
		if err != nil {
			return nil, err
		}
		for _, p := range path {
			f.b.Path = append(f.b.Path, p.Name)
		}
		out = append(out, f.b)
	}
	return out, nil
}
