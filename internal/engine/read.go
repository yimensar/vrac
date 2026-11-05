package engine

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const nodeColumns = `n.seq, n.id, n.kind, coalesce(p.id, ''), coalesce(n.parent_seq, 0), n.text,
	coalesce(n.status, ''), coalesce(n.due, ''), coalesce(n.start_at, ''), coalesce(n.end_at, ''),
	n.day, n.revision, n.created_at, n.updated_at, n.created_by, n.updated_by, n.updated_source,
	n.trashed_at IS NOT NULL,
	(SELECT count(*) FROM nodes c WHERE c.parent_seq = n.seq AND c.trashed_at IS NULL)`

const nodeFrom = ` FROM nodes n LEFT JOIN nodes p ON p.seq = n.parent_seq `

func scanNodes(rows *sql.Rows) ([]Node, error) {
	defer rows.Close()
	var out []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.seq, &n.ID, &n.Kind, &n.ParentID, &n.parentSeq, &n.Text, &n.Status, &n.Due, &n.Start, &n.End,
			&n.Day, &n.Revision, &n.CreatedAt, &n.UpdatedAt, &n.CreatedBy, &n.UpdatedBy, &n.Source, &n.Trashed, &n.ChildCount); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (e *Engine) nodesWhere(ctx context.Context, q queryer, where string, args ...any) ([]Node, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+nodeColumns+nodeFrom+where, args...)
	if err != nil {
		return nil, err
	}
	return scanNodes(rows)
}

func (e *Engine) nodeBySeq(ctx context.Context, q queryer, seq int64) (Node, error) {
	nodes, err := e.nodesWhere(ctx, q, `WHERE n.seq = ?`, seq)
	if err != nil {
		return Node{}, err
	}
	if len(nodes) == 0 {
		return Node{}, ErrNotFound
	}
	return nodes[0], nil
}

func (e *Engine) nodeByID(ctx context.Context, q queryer, id string) (Node, error) {
	nodes, err := e.nodesWhere(ctx, q, `WHERE n.id = ?`, id)
	if err != nil {
		return Node{}, err
	}
	if len(nodes) == 0 {
		return Node{}, fmt.Errorf("%w: node %s", ErrNotFound, id)
	}
	return nodes[0], nil
}

// hydrate loads types, aliases and references of nodes and returns the
// display map of every referenced node.
func (e *Engine) hydrate(ctx context.Context, q queryer, nodes []Node) (map[string]Ref, error) {
	refs := map[string]Ref{}
	if len(nodes) == 0 {
		return refs, nil
	}
	index := map[int64]int{}
	seqs := make([]any, len(nodes))
	for i := range nodes {
		index[nodes[i].seq] = i
		seqs[i] = nodes[i].seq
		nodes[i].Types = []string{}
		nodes[i].Aliases = []string{}
		nodes[i].RefIDs = []string{}
	}
	in := placeholders(len(seqs))
	rows, err := q.QueryContext(ctx, `SELECT nt.node_seq, t.name FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE nt.node_seq IN (`+in+`) ORDER BY t.builtin DESC, t.id`, seqs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var seq int64
		var name string
		if err := rows.Scan(&seq, &name); err != nil {
			rows.Close()
			return nil, err
		}
		nodes[index[seq]].Types = append(nodes[index[seq]].Types, name)
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT node_seq, alias FROM aliases WHERE node_seq IN (`+in+`) ORDER BY alias_key`, seqs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var seq int64
		var alias string
		if err := rows.Scan(&seq, &alias); err != nil {
			rows.Close()
			return nil, err
		}
		nodes[index[seq]].Aliases = append(nodes[index[seq]].Aliases, alias)
	}
	rows.Close()
	var targets []string
	for i := range nodes {
		nodes[i].RefIDs = refIDs(nodes[i].Text)
		targets = append(targets, nodes[i].RefIDs...)
	}
	return e.refMap(ctx, q, targets)
}

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
	return strings.TrimSpace(trailingTypesRe.ReplaceAllString(text, ""))
}

// untyped is text as a name: trailing type tokens are dropped and inner
// ones become plain words, so "Northwind #project" is named "Northwind" and "#meeting
// with Alice" is named "meeting with Alice".
func untyped(text string) string {
	text = labelOf(text)
	return strings.TrimSpace(typeTokenRe.ReplaceAllString(text, "$1$2"))
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// GetNode returns a node with its ancestors and its first page of children.
func (e *Engine) GetNode(ctx context.Context, id string) (NodeView, error) {
	n, err := e.nodeByID(ctx, e.db, id)
	if err != nil {
		return NodeView{}, err
	}
	return e.nodeView(ctx, n)
}

// NodeView is a zoomed node.
type NodeView struct {
	Node     Node           `json:"node"`
	Path     []Brief        `json:"path"`
	Children Page           `json:"children"`
	Refs     map[string]Ref `json:"refs"`
}

func (e *Engine) nodeView(ctx context.Context, n Node) (NodeView, error) {
	nodes := []Node{n}
	refs, err := e.hydrate(ctx, e.db, nodes)
	if err != nil {
		return NodeView{}, err
	}
	v := NodeView{Node: nodes[0], Refs: refs}
	if v.Path, err = e.path(ctx, e.db, n.seq); err != nil {
		return NodeView{}, err
	}
	if v.Children, err = e.children(ctx, n.seq, "", 200); err != nil {
		return NodeView{}, err
	}
	return v, nil
}

// path returns the ancestors of seq, root first.
func (e *Engine) path(ctx context.Context, q queryer, seq int64) ([]Brief, error) {
	rows, err := q.QueryContext(ctx, `WITH RECURSIVE up(seq, depth) AS (
			SELECT parent_seq, 1 FROM nodes WHERE seq = ? AND parent_seq IS NOT NULL
			UNION ALL SELECT n.parent_seq, up.depth + 1 FROM nodes n JOIN up ON n.seq = up.seq WHERE n.parent_seq IS NOT NULL)
		SELECT n.id, n.text, coalesce((SELECT group_concat(t.name, ' ') FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE nt.node_seq = n.seq), '')
		FROM up JOIN nodes n ON n.seq = up.seq ORDER BY up.depth DESC`, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	path := []Brief{}
	for rows.Next() {
		var b Brief
		var types string
		if err := rows.Scan(&b.ID, &b.Name, &types); err != nil {
			return nil, err
		}
		b.Types = strings.Fields(types)
		if b.Types == nil {
			b.Types = []string{}
		}
		path = append(path, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	var names []*string
	for i := range path {
		names = append(names, &path[i].Name)
	}
	return path, e.resolveNames(ctx, q, names)
}

// Children returns a page of the children of a node, in order.
func (e *Engine) Children(ctx context.Context, id, cursor string, limit int) (Page, error) {
	n, err := e.nodeByID(ctx, e.db, id)
	if err != nil {
		return Page{}, err
	}
	return e.children(ctx, n.seq, cursor, limit)
}

func (e *Engine) children(ctx context.Context, seq int64, cursor string, limit int) (Page, error) {
	limit = clampLimit(limit, 200, 500)
	after := int64(-1 << 62)
	if cursor != "" {
		var c struct{ P int64 }
		if err := decodeCursor(cursor, &c); err != nil {
			return Page{}, err
		}
		after = c.P
	}
	nodes, err := e.nodesWhere(ctx, e.db, `WHERE n.parent_seq = ? AND n.position > ? AND n.trashed_at IS NULL ORDER BY n.position LIMIT ?`, seq, after, limit+1)
	if err != nil {
		return Page{}, err
	}
	page := Page{Nodes: nodes}
	if len(nodes) > limit {
		page.Nodes = nodes[:limit]
		var pos int64
		if err := e.db.QueryRowContext(ctx, `SELECT position FROM nodes WHERE seq = ?`, nodes[limit-1].seq).Scan(&pos); err != nil {
			return Page{}, err
		}
		page.NextCursor = encodeCursor(struct{ P int64 }{pos})
	}
	if page.Nodes == nil {
		page.Nodes = []Node{}
	}
	page.Refs, err = e.hydrate(ctx, e.db, page.Nodes)
	return page, err
}

// DayView is a journal day. Exists is false when nothing was written yet.
type DayView struct {
	Day      string `json:"day"`
	Exists   bool   `json:"exists"`
	Node     *Node  `json:"node,omitempty"`
	Children Page   `json:"children"`
}

// Day returns a journal day without creating it.
func (e *Engine) Day(ctx context.Context, day, cursor string, limit int) (DayView, error) {
	if day == "" {
		day = e.today()
	}
	if err := validDate(day); err != nil {
		return DayView{}, err
	}
	v := DayView{Day: day, Children: Page{Nodes: []Node{}, Refs: map[string]Ref{}}}
	nodes, err := e.nodesWhere(ctx, e.db, `WHERE n.kind = 'day' AND n.day = ?`, day)
	if err != nil || len(nodes) == 0 {
		return v, err
	}
	if _, err := e.hydrate(ctx, e.db, nodes); err != nil {
		return v, err
	}
	v.Exists, v.Node = true, &nodes[0]
	v.Children, err = e.children(ctx, nodes[0].seq, cursor, limit)
	return v, err
}

// Days lists the journal days that have content, most recent first.
func (e *Engine) Days(ctx context.Context, before string, limit int) ([]string, error) {
	limit = clampLimit(limit, 30, 365)
	if before == "" {
		before = "9999-12-31"
	}
	rows, err := e.db.QueryContext(ctx, `SELECT d.day FROM nodes d WHERE d.kind = 'day' AND d.day < ?
		AND EXISTS (SELECT 1 FROM nodes c WHERE c.parent_seq = d.seq AND c.trashed_at IS NULL)
		ORDER BY d.day DESC LIMIT ?`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	days := []string{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		days = append(days, d)
	}
	return days, rows.Err()
}

// Roots returns a page of nodes at the root, in order.
func (e *Engine) Roots(ctx context.Context, cursor string, limit int) (Page, error) {
	limit = clampLimit(limit, 200, 500)
	after := int64(-1 << 62)
	if cursor != "" {
		var c struct{ P int64 }
		if err := decodeCursor(cursor, &c); err != nil {
			return Page{}, err
		}
		after = c.P
	}
	nodes, err := e.nodesWhere(ctx, e.db, `WHERE n.parent_seq IS NULL AND n.kind = 'node' AND n.position > ? AND n.trashed_at IS NULL ORDER BY n.position LIMIT ?`, after, limit+1)
	if err != nil {
		return Page{}, err
	}
	page := Page{Nodes: nodes}
	if len(nodes) > limit {
		page.Nodes = nodes[:limit]
		var pos int64
		if err := e.db.QueryRowContext(ctx, `SELECT position FROM nodes WHERE seq = ?`, nodes[limit-1].seq).Scan(&pos); err != nil {
			return Page{}, err
		}
		page.NextCursor = encodeCursor(struct{ P int64 }{pos})
	}
	if page.Nodes == nil {
		page.Nodes = []Node{}
	}
	page.Refs, err = e.hydrate(ctx, e.db, page.Nodes)
	return page, err
}

// Types lists built-in and free types with the number of nodes carrying them.
func (e *Engine) Types(ctx context.Context) ([]Type, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT t.name, t.builtin, t.color,
		(SELECT count(*) FROM node_types nt JOIN nodes n ON n.seq = nt.node_seq WHERE nt.type_id = t.id AND n.trashed_at IS NULL)
		FROM types t ORDER BY t.builtin DESC, t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	types := []Type{}
	for rows.Next() {
		var t Type
		if err := rows.Scan(&t.Name, &t.Builtin, &t.Color, &t.Count); err != nil {
			return nil, err
		}
		types = append(types, t)
	}
	return types, rows.Err()
}

// Resolve returns nodes whose name or alias matches name, exact matches
// first, for autocomplete and for AI clients before writing.
func (e *Engine) Resolve(ctx context.Context, name string, limit int) ([]Brief, error) {
	limit = clampLimit(limit, 10, 50)
	return e.candidates(ctx, e.db, name, limit)
}

func (e *Engine) candidates(ctx context.Context, q queryer, name string, limit int) ([]Brief, error) {
	key := nameKey(name)
	if key == "" {
		return []Brief{}, nil
	}
	like := "%" + escapeLike(key) + "%"
	rows, err := q.QueryContext(ctx, `WITH matches(seq, rank) AS (
			SELECT seq, CASE WHEN name_key = ?1 THEN 0 WHEN name_key LIKE ?2 ESCAPE '\' THEN 2 ELSE 3 END
			FROM nodes WHERE name_key LIKE ?3 ESCAPE '\' AND kind = 'node' AND trashed_at IS NULL
			UNION ALL
			SELECT a.node_seq, CASE WHEN a.alias_key = ?1 THEN 1 ELSE 3 END FROM aliases a JOIN nodes n ON n.seq = a.node_seq
			WHERE a.alias_key LIKE ?3 ESCAPE '\' AND n.trashed_at IS NULL)
		SELECT n.id, n.text, coalesce((SELECT group_concat(t.name, ' ') FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE nt.node_seq = n.seq), ''),
			n.seq
		FROM (SELECT seq, min(rank) AS rank FROM matches GROUP BY seq) m JOIN nodes n ON n.seq = m.seq
		ORDER BY m.rank,
			(EXISTS (SELECT 1 FROM node_types WHERE node_seq = n.seq) OR n.parent_seq IS NULL OR EXISTS (SELECT 1 FROM refs WHERE target_seq = n.seq)) DESC,
			length(n.text), n.seq
		LIMIT ?4`, key, escapeLike(key)+"%", like, limit)
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
	if match := ftsQuery(name); match != "" && len(list) < limit {
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

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func clampLimit(limit, def, max int) int {
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}
