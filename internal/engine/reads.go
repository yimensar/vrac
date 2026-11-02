package engine

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

type pageCursor struct {
	Scope    string `json:"scope"`
	Position int64  `json:"position,omitempty"`
	ID       string `json:"id"`
	At       string `json:"at,omitempty"`
	Revision int64  `json:"revision,omitempty"`
}

func encodeCursor(c pageCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeCursor(raw, scope string) (pageCursor, error) {
	var c pageCursor
	if raw == "" {
		return c, nil
	}
	if len(raw) > 4096 {
		return c, fmt.Errorf("%w: invalid cursor", ErrInvalid)
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return c, fmt.Errorf("%w: invalid cursor", ErrInvalid)
	}
	if err = json.Unmarshal(b, &c); err != nil || c.Scope != scope {
		return c, fmt.Errorf("%w: cursor does not belong to this query", ErrInvalid)
	}
	return c, nil
}
func pageLimit(limit int) (int, error) {
	if limit == 0 {
		return 100, nil
	}
	if limit < 1 || limit > 100 {
		return 0, fmt.Errorf("%w: limit must be between 1 and 100", ErrInvalid)
	}
	return limit, nil
}
func (e *Engine) children(q queryer, parentID, cursor string, limit int) (NodePage, error) {
	result := NodePage{Nodes: []Node{}}
	limit, err := pageLimit(limit)
	if err != nil {
		return result, err
	}
	scope := "children:" + parentID
	c, err := decodeCursor(cursor, scope)
	if err != nil {
		return result, err
	}
	if parentID != "" {
		var exists int
		err = q.QueryRow("SELECT 1 FROM nodes WHERE id=? AND deleted=0", parentID).Scan(&exists)
		if err == sql.ErrNoRows {
			return result, ErrNotFound
		}
		if err != nil {
			return result, err
		}
	}
	var parent any
	if parentID != "" {
		parent = parentID
	}
	query := "SELECT " + nodeColumns + ",n.position FROM nodes n WHERE n.parent_id IS ? AND n.deleted=0"
	args := []any{parent}
	// Journal days have ISO keys and a chronological product order independent
	// from the order in which an older day was first opened.
	var system string
	if parentID != "" {
		_ = q.QueryRow("SELECT COALESCE(system,'') FROM nodes WHERE id=?", parentID).Scan(&system)
	}
	if system == "journal" {
		if cursor != "" {
			query += " AND (n.system<? OR (n.system=? AND n.id>?))"
			args = append(args, c.At, c.At, c.ID)
		}
		query += " ORDER BY n.system DESC,n.id LIMIT ?"
	} else {
		if cursor != "" {
			query += " AND (n.position>? OR (n.position=? AND n.id>?))"
			args = append(args, c.Position, c.Position, c.ID)
		}
		query += " ORDER BY n.position,n.id LIMIT ?"
	}
	args = append(args, limit+1)
	rows, err := q.Query(query, args...)
	if err != nil {
		return result, err
	}
	positions := []int64{}
	for rows.Next() {
		n := Node{Tags: []string{}, References: []Reference{}}
		var deleted int
		var pos int64
		err = rows.Scan(&n.ID, &n.ParentID, &n.Text, &n.Revision, &n.CreatedAt, &n.UpdatedAt, &n.System, &deleted, &n.HasChildren, &pos)
		if err != nil {
			rows.Close()
			return result, err
		}
		if strings.HasPrefix(n.System, "day:") {
			n.JournalDate = strings.TrimPrefix(n.System, "day:")
		}
		result.Nodes = append(result.Nodes, n)
		positions = append(positions, pos)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if len(result.Nodes) > limit {
		last := result.Nodes[limit-1]
		result.NextCursor = encodeCursor(pageCursor{Scope: scope, Position: positions[limit-1], ID: last.ID, At: last.System})
		result.Nodes = result.Nodes[:limit]
	}
	if err = hydrate(q, result.Nodes); err != nil {
		return result, err
	}
	date := ""
	if parentID != "" {
		date, err = journalDate(q, parentID)
		if err != nil {
			return result, err
		}
	}
	if date != "" {
		for i := range result.Nodes {
			result.Nodes[i].JournalDate = date
		}
	}
	return result, nil
}
func pathsFor(q queryer, ids []string) (map[string][]Node, error) {
	result := map[string][]Node{}
	if len(ids) == 0 {
		return result, nil
	}
	marks := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		marks[i] = "?"
		args[i] = id
	}
	rows, err := q.Query(`WITH RECURSIVE ancestors(root_id,id,parent_id,depth) AS (
  SELECT id,id,parent_id,0 FROM nodes WHERE deleted=0 AND id IN (`+strings.Join(marks, ",")+`)
  UNION ALL SELECT a.root_id,n.id,n.parent_id,a.depth+1 FROM nodes n JOIN ancestors a ON n.id=a.parent_id WHERE n.deleted=0
 ) SELECT `+nodeColumns+`,a.root_id FROM ancestors a JOIN nodes n ON n.id=a.id ORDER BY a.root_id,a.depth DESC`, args...)
	if err != nil {
		return nil, err
	}
	pathIDs := map[string][]string{}
	unique := []Node{}
	indices := map[string]int{}
	for rows.Next() {
		n := Node{Tags: []string{}, References: []Reference{}}
		var deleted int
		var root string
		if err = rows.Scan(&n.ID, &n.ParentID, &n.Text, &n.Revision, &n.CreatedAt, &n.UpdatedAt, &n.System, &deleted, &n.HasChildren, &root); err != nil {
			rows.Close()
			return nil, err
		}
		pathIDs[root] = append(pathIDs[root], n.ID)
		if _, exists := indices[n.ID]; !exists {
			indices[n.ID] = len(unique)
			unique = append(unique, n)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Bound SQL parameter counts even for a representative unusually deep path.
	for offset := 0; offset < len(unique); offset += 500 {
		end := offset + 500
		if end > len(unique) {
			end = len(unique)
		}
		if err = hydrate(q, unique[offset:end]); err != nil {
			return nil, err
		}
	}
	for root, pathIDs := range pathIDs {
		date := ""
		p := make([]Node, 0, len(pathIDs))
		for _, id := range pathIDs {
			n := unique[indices[id]]
			if strings.HasPrefix(n.System, "day:") {
				date = strings.TrimPrefix(n.System, "day:")
			}
			n.JournalDate = date
			p = append(p, n)
		}
		result[root] = p
	}
	return result, nil
}
func path(q queryer, id string) ([]Node, error) {
	paths, err := pathsFor(q, []string{id})
	if err != nil {
		return nil, err
	}
	p, ok := paths[id]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}
func (e *Engine) Path(id string) ([]Node, error) {
	var result []Node
	err := e.read(func(tx *sql.Tx) error { var err error; result, err = path(tx, id); return err })
	return result, err
}
func (e *Engine) tags(q queryer, prefix string, limit int) ([]string, error) {
	limit, err := pageLimit(limit)
	if err != nil {
		return nil, err
	}
	prefix = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(prefix), "#"))
	rows, err := q.Query("SELECT DISTINCT t.tag FROM node_tags t WHERE t.tag>=? AND t.tag<? AND EXISTS(SELECT 1 FROM nodes n WHERE n.id=t.node_id AND n.deleted=0) ORDER BY t.tag LIMIT ?", prefix, prefix+"\U0010ffff", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var tag string
		if err = rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}
func (e *Engine) revisions(q queryer, id, cursor string, limit int) (RevisionPage, error) {
	result := RevisionPage{Revisions: []Revision{}}
	limit, err := pageLimit(limit)
	if err != nil {
		return result, err
	}
	if _, err = getNode(q, id, true); err != nil {
		return result, err
	}
	scope := "revisions:" + id
	c, err := decodeCursor(cursor, scope)
	if err != nil {
		return result, err
	}
	query := "SELECT revision,at,summary,snapshot FROM revisions WHERE node_id=?"
	args := []any{id}
	if cursor != "" {
		query += " AND revision<?"
		args = append(args, c.Revision)
	}
	query += " ORDER BY revision DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := q.Query(query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Revision
		var snapshot string
		if err = rows.Scan(&r.Revision, &r.At, &r.Summary, &snapshot); err != nil {
			return result, err
		}
		if err = json.Unmarshal([]byte(snapshot), &r.Node); err != nil {
			return result, err
		}
		result.Revisions = append(result.Revisions, r)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Revisions) > limit {
		result.NextCursor = encodeCursor(pageCursor{Scope: scope, Revision: result.Revisions[limit-1].Revision})
		result.Revisions = result.Revisions[:limit]
	}
	return result, nil
}
func ftsQuery(raw string) (string, error) {
	words := strings.FieldsFunc(raw, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	if len(words) == 0 {
		return "", fmt.Errorf("%w: search query needs a word or identifier", ErrInvalid)
	}
	if len(words) > 32 {
		return "", fmt.Errorf("%w: search query must contain at most 32 words", ErrInvalid)
	}
	for i := range words {
		words[i] = "\"" + strings.ReplaceAll(words[i], "\"", "\"\"") + "\"*"
	}
	return strings.Join(words, " AND "), nil
}

const subjectScopeSQL = `subject_scope(id) AS (
 SELECT node_id FROM branches WHERE node_id=?1
 UNION SELECT b.node_id FROM node_references r JOIN branches b ON b.node_id=r.source_id WHERE r.target_id=?1
 UNION SELECT b.node_id FROM branches b JOIN subject_scope scope ON b.parent_id=scope.id
)`

// A global first N can contain only the first N children of any one parent.
// Seek before each source limit, then merge/deduplicate those bounded streams.
// This preserves the exact ID order without scanning the whole workspace or
// sorting every leaf in a large project. Parameters: subject, after ID, limit.
const subjectBrowseSQL = `WITH RECURSIVE ` + subjectScopeSQL + `,
 direct(id) AS (
  SELECT DISTINCT r.source_id FROM node_references r JOIN nodes source ON source.id=r.source_id
  WHERE r.target_id=?1 AND source.deleted=0 AND r.source_id>?2 ORDER BY r.source_id LIMIT ?3
 ),
 children(id) AS (
  SELECT child.value FROM subject_scope scope JOIN json_each((
   SELECT json_group_array(id) FROM (
    SELECT n.id FROM nodes n INDEXED BY nodes_by_parent_identifier
    WHERE n.parent_id=scope.id AND n.deleted=0 AND n.id>?2 ORDER BY n.id LIMIT ?3
   )
  )) child
 )
 SELECT id FROM (
  SELECT ?1 AS id WHERE ?1>?2
  UNION SELECT id FROM direct
  UNION SELECT id FROM children
 ) ORDER BY id LIMIT ?3`

func (e *Engine) search(q queryer, f SearchFilters) (SearchPage, error) {
	result := SearchPage{Items: []SearchItem{}}
	limit, err := pageLimit(f.Limit)
	if err != nil {
		return result, err
	}
	for _, d := range []string{f.From, f.To, f.ActivityFrom, f.ActivityTo} {
		if d != "" {
			if err = validDate(d); err != nil {
				return result, err
			}
		}
	}
	if (f.From != "" && f.To != "" && f.From > f.To) || (f.ActivityFrom != "" && f.ActivityTo != "" && f.ActivityFrom > f.ActivityTo) {
		return result, fmt.Errorf("%w: date range is reversed", ErrInvalid)
	}
	fingerprint := f
	fingerprint.Cursor = ""
	fingerprint.Limit = 0
	b, _ := json.Marshal(fingerprint)
	sum := sha256.Sum256(b)
	scope := "search:" + hex.EncodeToString(sum[:])
	c, err := decodeCursor(f.Cursor, scope)
	if err != nil {
		return result, err
	}
	args := []any{}
	conditions := []string{"n.deleted=0"}
	tags := append([]string{}, f.Tags...)
	if f.Tag != "" {
		tags = append(tags, f.Tag)
	}
	ctes := []string{}
	if f.SubjectID != "" {
		if _, err = getNode(q, f.SubjectID, false); err != nil {
			return result, fmt.Errorf("subject: %w", err)
		}
		// Expand only internal branches. Leaf children match by their parent, so a
		// large meeting's unrelated leaf contents never need to be traversed.
		ctes = append(ctes, subjectScopeSQL)
		args = append(args, f.SubjectID)
		conditions = append(conditions, `(n.id=?1 OR EXISTS(SELECT 1 FROM node_references direct WHERE direct.source_id=n.id AND direct.target_id=?1) OR n.parent_id IN (SELECT id FROM subject_scope))`)

	}
	if f.From != "" || f.To != "" {
		from, to := f.From, f.To
		if from == "" {
			from = "0001-01-01"
		}
		if to == "" {
			to = "9999-12-31"
		}
		conditions = append(conditions, `EXISTS(WITH RECURSIVE ancestors(id,parent_id,system) AS (
   SELECT n.id,n.parent_id,n.system
   UNION ALL SELECT p.id,p.parent_id,p.system FROM nodes p INDEXED BY nodes_by_context JOIN ancestors a ON p.id=a.parent_id WHERE p.deleted=0
  ) SELECT 1 FROM ancestors WHERE system>=? AND system<=? LIMIT 1)`)
		args = append(args, "day:"+from, "day:"+to)
	}
	query := "SELECT n.id FROM nodes n"
	if strings.TrimSpace(f.Query) != "" {
		match, err := ftsQuery(f.Query)
		if err != nil {
			return result, err
		}
		conditions = append(conditions, "n.rowid IN (SELECT rowid FROM node_fts WHERE node_fts MATCH ?)")
		args = append(args, match)
	}
	for i, tag := range tags {
		tag, err = CanonicalTag(tag)
		if err != nil {
			return result, err
		}
		if i == 0 {
			query = "SELECT n.id FROM node_tags anchor INDEXED BY tags_by_tag JOIN nodes n INDEXED BY nodes_by_context ON n.id=anchor.node_id"
			conditions = append(conditions, "anchor.tag=?")
			args = append(args, tag)
		} else {
			conditions = append(conditions, "EXISTS(SELECT 1 FROM node_tags t WHERE t.node_id=n.id AND t.tag=?)")
			args = append(args, tag)
		}
	}
	if f.ActivityFrom != "" || f.ActivityTo != "" {
		from, to := f.ActivityFrom, f.ActivityTo
		if from == "" {
			from = "0001-01-01"
		}
		if to == "" {
			to = "9999-12-31"
		}
		conditions = append(conditions, "n.id IN (SELECT target_id FROM activities WHERE journal_date>=? AND journal_date<=?)")
		args = append(args, from, to)
	}
	if f.Cursor != "" {
		conditions = append(conditions, "n.id>?")
		args = append(args, c.ID)
	}
	order := "n.id"
	if len(tags) > 0 {
		order = "anchor.node_id"
	}
	query += " WHERE " + strings.Join(conditions, " AND ")
	query += " ORDER BY " + order + " LIMIT ?"
	args = append(args, limit+1)
	if len(ctes) > 0 {
		query = "WITH RECURSIVE " + strings.Join(ctes, ",") + " " + query
	}
	if f.SubjectID != "" && len(tags) == 0 && strings.TrimSpace(f.Query) == "" && f.From == "" && f.To == "" && f.ActivityFrom == "" && f.ActivityTo == "" {
		query = subjectBrowseSQL
		args = []any{f.SubjectID, c.ID, limit + 1}
	}
	rows, err := q.Query(query, args...)
	if err != nil {
		return result, fmt.Errorf("search: %w", err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if len(ids) > limit {
		result.NextCursor = encodeCursor(pageCursor{Scope: scope, ID: ids[limit-1]})
		ids = ids[:limit]
	}
	paths, err := pathsFor(q, ids)
	if err != nil {
		return result, err
	}
	for _, id := range ids {
		p := paths[id]
		n := p[len(p)-1]
		via := ""
		if f.SubjectID != "" {
			via = "branch context"
			for _, r := range n.References {
				if r.TargetID == f.SubjectID {
					via = "direct reference"
					break
				}
			}
		}
		if len(p) > 0 {
			n.JournalDate = p[len(p)-1].JournalDate
		}
		result.Items = append(result.Items, SearchItem{Node: n, Path: p, MatchedVia: via})
	}
	return result, nil
}

// Backlinks includes mentions and their branch context, matching subject search.
func (e *Engine) Backlinks(id, cursor string, limit int) (SearchPage, error) {
	return e.Search(SearchFilters{SubjectID: id, Cursor: cursor, Limit: limit})
}

func (e *Engine) Children(parentID, cursor string, limit int) (NodePage, error) {
	var result NodePage
	err := e.read(func(tx *sql.Tx) error {
		var err error
		result, err = e.children(tx, parentID, cursor, limit)
		return err
	})
	return result, err
}
func (e *Engine) Tags(prefix string, limit int) ([]string, error) {
	var result []string
	err := e.read(func(tx *sql.Tx) error { var err error; result, err = e.tags(tx, prefix, limit); return err })
	return result, err
}
func (e *Engine) Revisions(id, cursor string, limit int) (RevisionPage, error) {
	var result RevisionPage
	err := e.read(func(tx *sql.Tx) error { var err error; result, err = e.revisions(tx, id, cursor, limit); return err })
	return result, err
}
func (e *Engine) Search(f SearchFilters) (SearchPage, error) {
	var result SearchPage
	err := e.read(func(tx *sql.Tx) error { var err error; result, err = e.search(tx, f); return err })
	return result, err
}
