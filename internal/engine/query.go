package engine

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// Query is the one search used by the interface and by MCP.
type Query struct {
	// Text is full-text, matched as word prefixes over text and aliases.
	Text string `json:"text,omitempty"`
	// Types keeps nodes carrying any of these types.
	Types []string `json:"types,omitempty"`
	// Concerns keeps nodes concerning every one of these node IDs: the node
	// or one of its descendants is the result itself, an ancestor of it, or
	// referenced by it or one of its ancestors.
	Concerns []string `json:"concerns,omitempty"`
	// Mentions keeps nodes that directly reference this node ID.
	Mentions string `json:"mentions,omitempty"`
	// Status keeps tasks with this status.
	Status string `json:"status,omitempty"`
	// From and To bound the node date, inclusive: due date of tasks, start
	// of meetings and events, journal date otherwise.
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	// Scheduled keeps nodes with an explicit due date or start.
	Scheduled bool `json:"scheduled,omitempty"`
	// Sort is "date_desc" (default), "date_asc" or "updated".
	Sort   string `json:"sort,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type searchCursor struct {
	K string `json:"k"`
	S int64  `json:"s"`
}

// Search runs a query. Results are deterministic and paginated.
func (e *Engine) Search(ctx context.Context, q Query) (Page, error) {
	limit := clampLimit(q.Limit, 50, 200)
	var with []string
	var where []string
	var args []any
	for i, id := range q.Concerns {
		n, err := e.nodeByID(ctx, e.db, id)
		if err != nil {
			return Page{}, err
		}
		if n.Kind != KindNode {
			return Page{}, invalid("journal and day nodes are time, not subjects")
		}
		with = append(with, fmt.Sprintf(`subject%[1]d(seq) AS (SELECT ? UNION SELECT n.seq FROM nodes n JOIN subject%[1]d s ON n.parent_seq = s.seq),
			anchor%[1]d(seq) AS (SELECT seq FROM subject%[1]d UNION SELECT r.source_seq FROM refs r JOIN subject%[1]d s ON r.target_seq = s.seq),
			concern%[1]d(seq) AS (SELECT seq FROM anchor%[1]d UNION SELECT n.seq FROM nodes n JOIN concern%[1]d c ON n.parent_seq = c.seq)`, i))
		args = append(args, n.seq)
		where = append(where, fmt.Sprintf(`n.seq IN (SELECT seq FROM concern%d)`, i))
	}
	if q.Mentions != "" {
		n, err := e.nodeByID(ctx, e.db, q.Mentions)
		if err != nil {
			return Page{}, err
		}
		where = append(where, `n.seq IN (SELECT source_seq FROM refs WHERE target_seq = ?)`)
		args = append(args, n.seq)
	}
	if len(q.Types) > 0 {
		var names []any
		for _, t := range q.Types {
			names = append(names, strings.ToLower(t))
		}
		where = append(where, `n.seq IN (SELECT nt.node_seq FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE t.name IN (`+placeholders(len(names))+`))`)
		args = append(args, names...)
	}
	if match := ftsQuery(q.Text); match != "" {
		where = append(where, `n.seq IN (SELECT rowid FROM search WHERE search MATCH ?)`)
		args = append(args, match)
	}
	if q.Status != "" {
		if q.Status != StatusOpen && q.Status != StatusDone {
			return Page{}, invalid("status is open or done")
		}
		where = append(where, `n.status = ?`)
		args = append(args, q.Status)
	}
	for _, b := range []struct {
		v, op string
	}{{q.From, ">="}, {q.To, "<="}} {
		if b.v != "" {
			if err := validDate(b.v); err != nil {
				return Page{}, err
			}
			where = append(where, `n.date_key `+b.op+` ?`)
			args = append(args, b.v)
		}
	}
	if q.Scheduled {
		where = append(where, `(n.due IS NOT NULL OR n.start_at IS NOT NULL)`)
	}
	key, dir := "n.date_key", "DESC"
	switch q.Sort {
	case "", "date_desc":
	case "date_asc":
		dir = "ASC"
	case "updated":
		key = "n.updated_at"
	default:
		return Page{}, invalid("sort is date_desc, date_asc or updated")
	}
	if q.Cursor != "" {
		var c searchCursor
		if err := decodeCursor(q.Cursor, &c); err != nil {
			return Page{}, err
		}
		op := "<"
		if dir == "ASC" {
			op = ">"
		}
		where = append(where, fmt.Sprintf(`(%s, n.seq) %s (?, ?)`, key, op))
		args = append(args, c.K, c.S)
	}
	where = append(where, `n.kind = 'node' AND n.trashed_at IS NULL`)
	sql := `SELECT ` + nodeColumns + `, ` + key + nodeFrom + ` WHERE ` + strings.Join(where, " AND ") +
		fmt.Sprintf(` ORDER BY %s %s, n.seq %s LIMIT ?`, key, dir, dir)
	if len(with) > 0 {
		sql = `WITH RECURSIVE ` + strings.Join(with, ", ") + " " + sql
	}
	args = append(args, limit+1)
	rows, err := e.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return Page{}, err
	}
	var nodes []Node
	var keys []string
	for rows.Next() {
		var n Node
		var k string
		if err := rows.Scan(&n.seq, &n.ID, &n.Kind, &n.ParentID, &n.parentSeq, &n.Text, &n.Status, &n.Due, &n.Start, &n.End,
			&n.Day, &n.Revision, &n.CreatedAt, &n.UpdatedAt, &n.CreatedBy, &n.UpdatedBy, &n.Source, &n.Trashed, &n.ChildCount, &k); err != nil {
			rows.Close()
			return Page{}, err
		}
		nodes = append(nodes, n)
		keys = append(keys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	page := Page{Nodes: nodes}
	if len(nodes) > limit {
		page.Nodes = nodes[:limit]
		page.NextCursor = encodeCursor(searchCursor{keys[limit-1], nodes[limit-1].seq})
	}
	if page.Nodes == nil {
		page.Nodes = []Node{}
	}
	page.Refs, err = e.hydrate(ctx, e.db, page.Nodes)
	if err != nil {
		return Page{}, err
	}
	// Results also carry their location, so readers know where they live.
	ids := []string{}
	for _, n := range page.Nodes {
		if n.ParentID != "" {
			ids = append(ids, n.ParentID)
		}
	}
	parents, err := e.refMap(ctx, e.db, ids)
	for id, r := range parents {
		if _, ok := page.Refs[id]; !ok {
			page.Refs[id] = r
		}
	}
	return page, err
}

// ftsQuery turns free text into an FTS5 query of quoted word prefixes.
func ftsQuery(text string) string {
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var parts []string
	for _, w := range words {
		parts = append(parts, `"`+w+`"*`)
	}
	return strings.Join(parts, " ")
}

// Sections are what Vrac gathers on a zoomed node.
type Sections struct {
	OpenTasks []Node         `json:"open_tasks"`
	Upcoming  []Node         `json:"upcoming"`
	Meetings  []Node         `json:"meetings"`
	Decisions []Node         `json:"decisions"`
	Mentions  Page           `json:"mentions"`
	Refs      map[string]Ref `json:"refs"`
}

// NodeSections gathers open tasks, upcoming meetings and events, recent
// meetings, recent decisions and direct mentions of a node.
func (e *Engine) NodeSections(ctx context.Context, id string) (Sections, error) {
	today := e.today()
	s := Sections{Refs: map[string]Ref{}}
	merge := func(p Page, err error) ([]Node, error) {
		for k, v := range p.Refs {
			s.Refs[k] = v
		}
		return p.Nodes, err
	}
	var err error
	if s.OpenTasks, err = merge(e.Search(ctx, Query{Concerns: []string{id}, Types: []string{TypeTask}, Status: StatusOpen, Sort: "date_asc", Limit: 50})); err != nil {
		return s, err
	}
	if s.Upcoming, err = merge(e.Search(ctx, Query{Concerns: []string{id}, Types: []string{TypeMeeting, TypeEvent}, From: today, Sort: "date_asc", Limit: 10})); err != nil {
		return s, err
	}
	if s.Meetings, err = merge(e.Search(ctx, Query{Concerns: []string{id}, Types: []string{TypeMeeting}, To: today, Limit: 10})); err != nil {
		return s, err
	}
	if s.Decisions, err = merge(e.Search(ctx, Query{Concerns: []string{id}, Types: []string{TypeDecision}, Limit: 20})); err != nil {
		return s, err
	}
	s.Mentions, err = e.Search(ctx, Query{Mentions: id, Limit: 30})
	for k, v := range s.Mentions.Refs {
		s.Refs[k] = v
	}
	// The node itself is not one of its own sections.
	for _, list := range []*[]Node{&s.OpenTasks, &s.Upcoming, &s.Meetings, &s.Decisions} {
		*list = slices.DeleteFunc(*list, func(n Node) bool { return n.ID == id })
	}
	return s, err
}

// TypeRow is one line of a type view.
type TypeRow struct {
	Node         Node     `json:"node"`
	Path         []string `json:"path"`
	OpenTasks    int      `json:"open_tasks"`
	LastActivity string   `json:"last_activity"`
}

// TypeView lists the nodes of a type with their open tasks and last
// activity, most recently active first.
type TypeView struct {
	Type Type           `json:"type"`
	Rows []TypeRow      `json:"rows"`
	Refs map[string]Ref `json:"refs"`
}

const maxTypeRows = 500

// TypeTable returns the view of a type.
func (e *Engine) TypeTable(ctx context.Context, name string) (TypeView, error) {
	name = strings.ToLower(name)
	types, err := e.Types(ctx)
	if err != nil {
		return TypeView{}, err
	}
	v := TypeView{Rows: []TypeRow{}, Refs: map[string]Ref{}}
	found := false
	for _, t := range types {
		if t.Name == name {
			v.Type, found = t, true
		}
	}
	if !found {
		return v, fmt.Errorf("%w: type %s", ErrNotFound, name)
	}
	nodes, err := e.nodesWhere(ctx, e.db, `WHERE n.trashed_at IS NULL AND n.seq IN (SELECT nt.node_seq FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE t.name = ?) ORDER BY n.seq LIMIT ?`, name, maxTypeRows)
	if err != nil {
		return v, err
	}
	if v.Refs, err = e.hydrate(ctx, e.db, nodes); err != nil {
		return v, err
	}
	for _, n := range nodes {
		row := TypeRow{Node: n, Path: []string{}}
		path, err := e.path(ctx, e.db, n.seq)
		if err != nil {
			return v, err
		}
		for _, p := range path {
			row.Path = append(row.Path, p.Name)
		}
		err = e.db.QueryRowContext(ctx, `WITH RECURSIVE
			subject(seq) AS (SELECT ? UNION SELECT n.seq FROM nodes n JOIN subject s ON n.parent_seq = s.seq),
			anchor(seq) AS (SELECT seq FROM subject UNION SELECT r.source_seq FROM refs r JOIN subject s ON r.target_seq = s.seq),
			concern(seq) AS (SELECT seq FROM anchor UNION SELECT n.seq FROM nodes n JOIN concern c ON n.parent_seq = c.seq)
			SELECT count(*) FILTER (WHERE n.status = 'open'), coalesce(max(n.updated_at), '')
			FROM concern c JOIN nodes n ON n.seq = c.seq WHERE n.trashed_at IS NULL AND n.kind = 'node'`, n.seq).Scan(&row.OpenTasks, &row.LastActivity)
		if err != nil {
			return v, err
		}
		v.Rows = append(v.Rows, row)
	}
	sort.SliceStable(v.Rows, func(i, j int) bool { return v.Rows[i].LastActivity > v.Rows[j].LastActivity })
	return v, nil
}

// Activity is one revision in the activity feed.
type Activity struct {
	Node     Brief  `json:"node"`
	Revision int64  `json:"revision"`
	Action   string `json:"action"`
	At       string `json:"at"`
	Actor    string `json:"actor"`
	Source   string `json:"source"`
	// Latest is true when this revision is the node's current one, so it
	// can be undone.
	Latest bool `json:"latest"`
}

// ActivityPage is a page of the activity feed.
type ActivityPage struct {
	Items      []Activity `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

// Activity lists revisions, most recent first, optionally for one actor.
func (e *Engine) Activity(ctx context.Context, actor, cursor string, limit int) (ActivityPage, error) {
	limit = clampLimit(limit, 50, 200)
	where := `WHERE 1`
	var args []any
	if actor != "" {
		where += ` AND r.actor = ?`
		args = append(args, actor)
	}
	if cursor != "" {
		var c struct {
			A string
			S int64
			R int64
		}
		if err := decodeCursor(cursor, &c); err != nil {
			return ActivityPage{}, err
		}
		where += ` AND (r.at, r.node_seq, r.revision) < (?, ?, ?)`
		args = append(args, c.A, c.S, c.R)
	}
	args = append(args, limit+1)
	rows, err := e.db.QueryContext(ctx, `SELECT n.id, n.text, r.node_seq, r.revision, r.action, r.at, r.actor, r.source, r.revision = n.revision,
		coalesce((SELECT group_concat(t.name, ' ') FROM node_types nt JOIN types t ON t.id = nt.type_id WHERE nt.node_seq = n.seq), '')
		FROM revisions r JOIN nodes n ON n.seq = r.node_seq `+where+` ORDER BY r.at DESC, r.node_seq DESC, r.revision DESC LIMIT ?`, args...)
	if err != nil {
		return ActivityPage{}, err
	}
	defer rows.Close()
	page := ActivityPage{Items: []Activity{}}
	var last struct {
		A string
		S int64
		R int64
	}
	for rows.Next() {
		var a Activity
		var seq int64
		var types string
		if err := rows.Scan(&a.Node.ID, &a.Node.Name, &seq, &a.Revision, &a.Action, &a.At, &a.Actor, &a.Source, &a.Latest, &types); err != nil {
			return ActivityPage{}, err
		}
		if len(page.Items) == limit {
			page.NextCursor = encodeCursor(last)
			break
		}
		a.Node.Types = strings.Fields(types)
		if a.Node.Types == nil {
			a.Node.Types = []string{}
		}
		page.Items = append(page.Items, a)
		last.A, last.S, last.R = a.At, seq, a.Revision
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	rows.Close()
	var names []*string
	for i := range page.Items {
		names = append(names, &page.Items[i].Node.Name)
	}
	return page, e.resolveNames(ctx, e.db, names)
}
