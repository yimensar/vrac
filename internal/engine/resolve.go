package engine

import (
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
)

// typeID returns the ID of a type, creating it when allowed. With check
// set it never writes and only reports.
func (s *txState) typeID(name string, check bool) (int64, bool, error) {
	if id, ok := s.typeIDs[name]; ok {
		return id, true, nil
	}
	var id int64
	err := s.tx.QueryRowContext(s.ctx, `SELECT id FROM types WHERE name = ?`, name).Scan(&id)
	if err == nil {
		s.typeIDs[name] = id
		return id, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	if !s.allowTypes[name] {
		candidates, err := s.typeCandidates(name)
		if err != nil {
			return 0, false, err
		}
		s.report("type", name, candidates)
		return 0, false, nil
	}
	if check {
		return 0, true, nil
	}
	var count int
	if err := s.tx.QueryRowContext(s.ctx, `SELECT count(*) FROM types`).Scan(&count); err != nil {
		return 0, false, err
	}
	r, err := s.tx.ExecContext(s.ctx, `INSERT INTO types(name, builtin, color, created_at) VALUES (?, 0, ?, ?)`, name, palette[count%len(palette)], s.now)
	if err != nil {
		return 0, false, err
	}
	id, _ = r.LastInsertId()
	s.typeIDs[name] = id
	return id, true, nil
}

func (s *txState) typeCandidates(name string) ([]Brief, error) {
	rows, err := s.tx.QueryContext(s.ctx, `SELECT name FROM types ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Brief{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		if strings.Contains(t, name) || strings.Contains(name, t) || levenshtein(t, name) <= 2 {
			out = append(out, Brief{Name: t, Types: []string{}})
		}
	}
	return out, rows.Err()
}

// lookupName finds the node a name designates. It returns 0 and the
// ambiguous matches when several nodes qualify.
func (s *txState) lookupName(name string) (int64, []Brief, error) {
	key := nameKey(name)
	rows, err := s.tx.QueryContext(s.ctx, `SELECT DISTINCT n.seq,
			EXISTS (SELECT 1 FROM node_types WHERE node_seq = n.seq) OR n.parent_seq IS NULL OR EXISTS (SELECT 1 FROM refs WHERE target_seq = n.seq) OR a.node_seq IS NOT NULL
		FROM nodes n LEFT JOIN aliases a ON a.node_seq = n.seq AND a.alias_key = ?1
		WHERE n.kind = 'node' AND n.trashed_at IS NULL AND (n.name_key = ?1 OR a.node_seq IS NOT NULL)
		LIMIT 20`, key)
	if err != nil {
		return 0, nil, err
	}
	var all, entities []int64
	for rows.Next() {
		var seq int64
		var entity bool
		if err := rows.Scan(&seq, &entity); err != nil {
			rows.Close()
			return 0, nil, err
		}
		all = append(all, seq)
		if entity {
			entities = append(entities, seq)
		}
	}
	rows.Close()
	switch {
	case len(all) == 1:
		return all[0], nil, nil
	case len(entities) == 1:
		return entities[0], nil, nil
	case len(all) > 1:
		c, err := s.e.candidates(s.ctx, s.tx, name, 10)
		return 0, c, err
	}
	parentName, child, nested := cutLast(name)
	if !nested {
		return 0, nil, nil
	}
	parent, _, err := s.lookupName(parentName)
	if err != nil || parent == 0 {
		return 0, nil, err
	}
	err = s.tx.QueryRowContext(s.ctx, `SELECT n.seq FROM nodes n WHERE n.parent_seq = ?1 AND n.trashed_at IS NULL
		AND (n.name_key = ?2 OR EXISTS (SELECT 1 FROM aliases a WHERE a.node_seq = n.seq AND a.alias_key = ?2))
		ORDER BY n.position LIMIT 1`, parent, nameKey(child)).Scan(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, nil
	}
	return parent, nil, err
}

func cutLast(name string) (string, string, bool) {
	i := strings.LastIndex(name, "/")
	if i <= 0 || i == len(name)-1 {
		return "", "", false
	}
	return strings.TrimSpace(name[:i]), strings.TrimSpace(name[i+1:]), true
}

// checkRef reports an unresolvable reference without writing.
func (s *txState) checkRef(t refToken) error {
	if t.ID != "" {
		var seq int64
		err := s.tx.QueryRowContext(s.ctx, `SELECT seq FROM nodes WHERE id = ?`, t.ID).Scan(&seq)
		if errors.Is(err, sql.ErrNoRows) {
			s.report("node", t.ID, nil)
			return nil
		}
		return err
	}
	seq, candidates, err := s.lookupName(t.Name)
	if err != nil || seq != 0 {
		return err
	}
	c, ok := s.creations[nameKey(t.Name)]
	if !ok {
		if candidates == nil {
			if candidates, err = s.e.candidates(s.ctx, s.tx, t.Name, 5); err != nil {
				return err
			}
		}
		s.report("node", t.Name, candidates)
		return nil
	}
	for _, typ := range creationTypes(c, t.Type) {
		if _, _, err := s.typeID(typ, true); err != nil {
			return err
		}
	}
	if parentName, _, nested := cutLast(c.Name); nested {
		return s.checkRef(refToken{Name: parentName})
	}
	return nil
}

func creationTypes(c Creation, extra string) []string {
	var types []string
	for _, t := range c.Types {
		types = append(types, strings.ToLower(strings.TrimSpace(t)))
	}
	if extra != "" && !slices.Contains(types, extra) {
		types = append(types, extra)
	}
	return types
}

// resolveRef returns the node a checked reference designates, creating it
// when an explicit creation allows it.
func (s *txState) resolveRef(t refToken) (string, error) {
	if t.ID != "" {
		return t.ID, nil
	}
	seq, _, err := s.lookupName(t.Name)
	if err != nil {
		return "", err
	}
	if seq == 0 {
		if seq, err = s.createNamed(s.creations[nameKey(t.Name)], t.Type); err != nil {
			return "", err
		}
	}
	var id string
	err = s.tx.QueryRowContext(s.ctx, `SELECT id FROM nodes WHERE seq = ?`, seq).Scan(&id)
	return id, err
}

func (s *txState) createNamed(c Creation, extraType string) (int64, error) {
	key := nameKey(c.Name)
	if seq, ok := s.created[key]; ok {
		return seq, nil
	}
	parent := sql.NullInt64{}
	text := c.Name
	if parentName, child, nested := cutLast(c.Name); nested {
		id, err := s.resolveRef(refToken{Name: parentName})
		if err != nil {
			return 0, err
		}
		p, err := s.e.nodeByID(s.ctx, s.tx, id)
		if err != nil {
			return 0, err
		}
		parent, text = sql.NullInt64{Int64: p.seq, Valid: true}, child
	}
	var typeIDs []int64
	var names []string
	for _, t := range creationTypes(c, extraType) {
		id, ok, err := s.typeID(t, false)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, s.fail()
		}
		typeIDs = append(typeIDs, id)
		names = append(names, t)
	}
	pos, err := s.slot(parent, 0)
	if err != nil {
		return 0, err
	}
	day, err := s.dayOf(parent)
	if err != nil {
		return 0, err
	}
	name := text
	for _, t := range names {
		text += " #" + t
	}
	f, _, err := normalizeFields(names, fields{}, text, false, day)
	if err != nil {
		return 0, err
	}
	seq, id, err := s.insert(parent, pos, text, typeIDs, f, nil, day)
	if err != nil {
		return 0, err
	}
	s.created[key] = seq
	s.briefs = append(s.briefs, Brief{ID: id, Name: name, Types: append([]string{}, names...)})
	return seq, nil
}

// resolved is parsed input bound to existing types and nodes.
type resolved struct {
	text    string
	typeIDs []int64
	types   []string
}

func (s *txState) check(p parsedInput) error {
	for _, t := range p.Types {
		if _, _, err := s.typeID(t, true); err != nil {
			return err
		}
	}
	for _, r := range p.Refs {
		if err := s.checkRef(r); err != nil {
			return err
		}
	}
	return nil
}

func (s *txState) resolve(p parsedInput) (resolved, error) {
	var r resolved
	for _, t := range p.Types {
		id, ok, err := s.typeID(t, false)
		if err != nil {
			return r, err
		}
		if !ok {
			return r, s.fail()
		}
		r.typeIDs = append(r.typeIDs, id)
		r.types = append(r.types, t)
	}
	text := p.Text
	for i, ref := range p.Refs {
		id, err := s.resolveRef(ref)
		if err != nil {
			return r, err
		}
		text = strings.Replace(text, "\x00"+strconv.Itoa(i)+"\x00", "[["+id+"]]", 1)
	}
	r.text = text
	return r, nil
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
