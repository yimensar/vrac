package engine

import (
	"context"
	"fmt"
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
	// Untagged keeps nodes carrying no tag at all.
	Untagged bool `json:"untagged,omitempty"`
	// Outside drops the nodes inside the subtrees of Concerns: a node's
	// page lists what refers to it from elsewhere, not its own outline.
	Outside bool `json:"outside,omitempty"`
	// Status keeps nodes whose checkbox is open or done.
	Status string `json:"status,omitempty"`
	// From and To bound the node date, inclusive: its date when it has
	// one, its journal day otherwise.
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	// Dated keeps nodes with a date.
	Dated bool `json:"dated,omitempty"`
	// Subjects adds, for every result, the typed nodes it concerns (see
	// Concerns), most specific first, so a client can group results by
	// project or person without repeating the context on each node.
	Subjects bool `json:"with_subjects,omitempty"`
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
	var filters []string
	var fargs []any
	// Walking nodes in date order finds the latest of a huge set quickly,
	// but only when nothing else makes matches rare.
	sparse := q.From != "" || q.To != "" || q.Dated || q.Status != "" || ftsQuery(q.Text) != ""
	if q.Mentions != "" {
		n, err := e.nodeByID(ctx, e.db, q.Mentions)
		if err != nil {
			return Page{}, err
		}
		var many int
		if err := e.db.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT 1 FROM refs WHERE target_seq = ? LIMIT ?)`, n.seq, largeSubject+1).Scan(&many); err != nil {
			return Page{}, err
		}
		// Like context: walk dates for a much referenced node.
		if many > largeSubject && !sparse {
			filters = append(filters, `EXISTS (SELECT 1 FROM refs r WHERE r.source_seq = n.seq AND r.target_seq = ?)`)
		} else {
			filters = append(filters, `n.seq IN (SELECT source_seq FROM refs WHERE target_seq = ?)`)
		}
		fargs = append(fargs, n.seq)
	}
	if len(q.Types) > 0 {
		ids, err := e.typeIDs(ctx, q.Types)
		if err != nil {
			return Page{}, err
		}
		if len(ids) == 0 {
			return Page{Nodes: []Node{}, Refs: map[string]Ref{}}, nil
		}
		// Checked per row, so an index drives the search instead.
		filters = append(filters, `EXISTS (SELECT 1 FROM node_types nt WHERE nt.node_seq = n.seq AND nt.type_id IN (`+placeholders(len(ids))+`))`)
		fargs = append(fargs, ids...)
	}
	if q.Untagged {
		filters = append(filters, `NOT EXISTS (SELECT 1 FROM node_types nt WHERE nt.node_seq = n.seq)`)
	}
	if match := ftsQuery(q.Text); match != "" {
		filters = append(filters, `n.seq IN (SELECT rowid FROM search WHERE search MATCH ?)`)
		fargs = append(fargs, match)
	}
	if q.Status != "" {
		if q.Status != StatusOpen && q.Status != StatusDone {
			return Page{}, invalid("status is open or done")
		}
		filters = append(filters, `n.status = ?`)
		fargs = append(fargs, q.Status)
	}
	// Dates are bound once the date column is known (see below).
	var dates []struct{ v, op string }
	for _, b := range []struct {
		v, op string
	}{{q.From, ">="}, {q.To, "<="}} {
		if b.v != "" {
			if err := validDate(b.v); err != nil {
				return Page{}, err
			}
			dates = append(dates, b)
		}
	}
	if q.Dated {
		filters = append(filters, `n.start_at IS NOT NULL`)
	}
	// Context drives the search through its index when given: the
	// smallest subject is read in date order, the others checked per row.
	from := nodeFrom
	key, seq := "n.date_key", "n.seq"
	var args []any
	where := []string{`n.kind = 'node' AND n.trashed_at IS NULL`}
	subjects, err := e.subjects(ctx, q.Concerns)
	if err != nil {
		return Page{}, err
	}
	for i, n := range subjects {
		if i == 0 {
			from = ` FROM concerns c JOIN nodes n ON n.seq = c.node_seq LEFT JOIN nodes p ON p.seq = n.parent_seq `
			key, seq = "c.date_key", "c.node_seq"
			where = append(where, `c.subject_seq = ?`)
		} else {
			where = append(where, `EXISTS (SELECT 1 FROM concerns o WHERE o.subject_seq = ? AND o.node_seq = n.seq)`)
		}
		args = append(args, n.seq)
		if q.Outside {
			where = append(where, `n.seq NOT IN (WITH RECURSIVE sub(seq) AS (SELECT ? UNION ALL SELECT d.seq FROM nodes d JOIN sub ON d.parent_seq = sub.seq) SELECT seq FROM sub)`)
			args = append(args, n.seq)
		}
	}
	where = append(where, filters...)
	args = append(args, fargs...)
	// The index holds the same date, so ranges use it there.
	for _, d := range dates {
		where = append(where, key+" "+d.op+" ?")
		args = append(args, d.v)
	}
	dir := "DESC"
	switch q.Sort {
	case "", "date_desc":
	case "date_asc":
		dir = "ASC"
	case "updated":
		key, seq = "n.updated_at", "n.seq"
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
		where = append(where, fmt.Sprintf(`(%s, %s) %s (?, ?)`, key, seq, op))
		args = append(args, c.K, c.S)
	}
	sql := `SELECT ` + nodeColumns + `, ` + key + from + ` WHERE ` + strings.Join(where, " AND ") +
		fmt.Sprintf(` ORDER BY %s %s, %s %s LIMIT ?`, key, dir, seq, dir)
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
		if err := rows.Scan(&n.seq, &n.ID, &n.Kind, &n.ParentID, &n.parentSeq, &n.Text, &n.Status, &n.Date, &n.End,
			&n.Day, &n.Revision, &n.CreatedAt, &n.UpdatedAt, &n.CreatedBy, &n.UpdatedBy, &n.Source, &n.Trashed, &n.ChildCount, &n.position, &k); err != nil {
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
	if err != nil {
		return Page{}, err
	}
	for id, r := range parents {
		if _, ok := page.Refs[id]; !ok {
			page.Refs[id] = r
		}
	}
	if q.Subjects {
		if page.Subjects, err = e.subjectsOf(ctx, page.Nodes, page.Refs); err != nil {
			return Page{}, err
		}
	}
	return page, nil
}

// subjectsOf lists, for each node, the typed nodes it concerns other than
// itself, most specific first: a subject whose own context is larger sits
// deeper in the tree. Their display entries are added to refs.
func (e *Engine) subjectsOf(ctx context.Context, nodes []Node, refs map[string]Ref) (map[string][]string, error) {
	out := map[string][]string{}
	if len(nodes) == 0 {
		return out, nil
	}
	seqs := make([]any, len(nodes))
	byseq := map[int64]string{}
	for i, n := range nodes {
		seqs[i] = n.seq
		byseq[n.seq] = n.ID
	}
	rows, err := e.db.QueryContext(ctx, `SELECT c.node_seq, s.seq, s.id, (SELECT count(*) FROM concerns o WHERE o.node_seq = s.seq)
		FROM concerns c JOIN nodes s ON s.seq = c.subject_seq
		WHERE c.node_seq IN (`+placeholders(len(seqs))+`) AND c.subject_seq != c.node_seq AND s.trashed_at IS NULL
		AND EXISTS (SELECT 1 FROM node_types nt WHERE nt.node_seq = s.seq)
		ORDER BY c.node_seq, 4 DESC, s.seq`, seqs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var node, seq int64
		var id string
		var depth int
		if err := rows.Scan(&node, &seq, &id, &depth); err != nil {
			return nil, err
		}
		out[byseq[node]] = append(out[byseq[node]], id)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	more, err := e.refMap(ctx, e.db, ids)
	if err != nil {
		return nil, err
	}
	for id, r := range more {
		if _, ok := refs[id]; !ok {
			refs[id] = r
		}
	}
	return out, nil
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

// ListCount is one list gathered on a node's page: the nodes, outside its
// own outline, that concern it and carry Type; Type is empty for the
// untagged ones.
type ListCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// NodeLists counts, by tag, what concerns a node from outside its own
// subtree: what refers to it, with the inherited context. The lists
// themselves are read with Search (Concerns, Types or Untagged, Outside).
// Largest first, the untagged list last.
func (e *Engine) NodeLists(ctx context.Context, id string) ([]ListCount, error) {
	n, err := e.nodeByID(ctx, e.db, id)
	if err != nil {
		return nil, err
	}
	rows, err := e.db.QueryContext(ctx, `WITH RECURSIVE sub(seq) AS (SELECT ?1 UNION ALL SELECT d.seq FROM nodes d JOIN sub ON d.parent_seq = sub.seq),
		outside(seq) AS (SELECT c.node_seq FROM concerns c JOIN nodes n ON n.seq = c.node_seq
			WHERE c.subject_seq = ?1 AND n.trashed_at IS NULL AND n.seq NOT IN (SELECT seq FROM sub))
		SELECT coalesce(t.name, ''), count(*) FROM outside o LEFT JOIN node_types nt ON nt.node_seq = o.seq LEFT JOIN types t ON t.id = nt.type_id
		GROUP BY t.name ORDER BY t.name IS NULL, count(*) DESC, t.name`, n.seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lists := []ListCount{}
	for rows.Next() {
		var l ListCount
		if err := rows.Scan(&l.Type, &l.Count); err != nil {
			return nil, err
		}
		lists = append(lists, l)
	}
	return lists, rows.Err()
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

// typeIDs returns the IDs of the named types that exist.
func (e *Engine) typeIDs(ctx context.Context, names []string) ([]any, error) {
	var args []any
	for _, n := range names {
		args = append(args, strings.ToLower(n))
	}
	rows, err := e.db.QueryContext(ctx, `SELECT id FROM types WHERE name IN (`+placeholders(len(args))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []any
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// largeSubject bounds counts that only decide an order: above it, a set is
// large.
const largeSubject = 5000

// subjects resolves context subjects, the one concerning the fewest nodes
// first, so searches start from the smallest set.
func (e *Engine) subjects(ctx context.Context, ids []string) ([]Node, error) {
	type sized struct {
		Node
		size int
	}
	var list []sized
	for _, id := range ids {
		n, err := e.nodeByID(ctx, e.db, id)
		if err != nil {
			return nil, err
		}
		if n.Kind != KindNode {
			return nil, invalid("journal and day nodes are time, not subjects")
		}
		s := sized{Node: n}
		if len(ids) > 1 {
			// Counting stops at the bound: only the order matters above.
			if err := e.db.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT 1 FROM concerns WHERE subject_seq = ? LIMIT ?)`, n.seq, largeSubject+1).Scan(&s.size); err != nil {
				return nil, err
			}
		}
		list = append(list, s)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].size < list[j].size })
	out := make([]Node, len(list))
	for i, s := range list {
		out[i] = s.Node
	}
	return out, nil
}
