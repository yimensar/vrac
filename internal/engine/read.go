package engine

import (
	"context"
	"database/sql"
	"fmt"
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
	(SELECT count(*) FROM nodes c WHERE c.parent_seq = n.seq AND c.trashed_at IS NULL), n.position`

const nodeFrom = ` FROM nodes n LEFT JOIN nodes p ON p.seq = n.parent_seq `

func scanNodes(rows *sql.Rows) ([]Node, error) {
	defer rows.Close()
	var out []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.seq, &n.ID, &n.Kind, &n.ParentID, &n.parentSeq, &n.Text, &n.Status, &n.Due, &n.Start, &n.End,
			&n.Day, &n.Revision, &n.CreatedAt, &n.UpdatedAt, &n.CreatedBy, &n.UpdatedBy, &n.Source, &n.Trashed, &n.ChildCount, &n.position); err != nil {
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
	return e.siblings(ctx, sql.NullInt64{Int64: seq, Valid: true}, cursor, limit)
}

// siblings returns a page of the nodes under parent (the root when not
// valid), in order, with a cursor on their private position.
func (e *Engine) siblings(ctx context.Context, parent sql.NullInt64, cursor string, limit int) (Page, error) {
	limit = clampLimit(limit, 200, 500)
	var c struct{ P int64 }
	c.P = -1 << 62
	if cursor != "" {
		if err := decodeCursor(cursor, &c); err != nil {
			return Page{}, err
		}
	}
	nodes, err := e.nodesWhere(ctx, e.db, `WHERE n.parent_seq IS ? AND n.kind = 'node' AND n.position > ? AND n.trashed_at IS NULL ORDER BY n.position LIMIT ?`, parent, c.P, limit+1)
	if err != nil {
		return Page{}, err
	}
	page := Page{Nodes: nodes}
	if len(nodes) > limit {
		page.Nodes = nodes[:limit]
		page.NextCursor = encodeCursor(struct{ P int64 }{nodes[limit-1].position})
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
	return e.siblings(ctx, sql.NullInt64{}, cursor, limit)
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
