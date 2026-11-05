package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	positionGap   = 1 << 16
	maxDraftNodes = 1000
	maxDepth      = 50
	maxAliasLen   = 200
	maxNameKeyLen = 200
)

// txState carries one mutation transaction.
type txState struct {
	e          *Engine
	ctx        context.Context
	tx         *sql.Tx
	actor      Actor
	now        string
	typeIDs    map[string]int64
	allowTypes map[string]bool
	creations  map[string]Creation
	created    map[string]int64
	briefs     []Brief
	unresolved []Unresolved
	reported   map[string]bool
}

type fields struct{ Status, Due, Start, End string }

func (e *Engine) mutate(ctx context.Context, actor Actor, requestID string, payload any, fn func(*txState) ([]int64, error)) (Result, error) {
	if strings.TrimSpace(actor.Name) == "" || strings.TrimSpace(actor.Source) == "" {
		return Result{}, invalid("actor name and source are required")
	}
	if requestID == "" || len(requestID) > 200 {
		return Result{}, invalid("request_id is required (at most 200 characters)")
	}
	hash := hashJSON(payload)
	var res Result
	err := e.write(ctx, func(tx *sql.Tx) error {
		var storedHash, stored string
		err := tx.QueryRowContext(ctx, `SELECT payload_hash, response FROM requests WHERE actor = ? AND request_id = ?`, actor.Name, requestID).Scan(&storedHash, &stored)
		if err == nil {
			if storedHash != hash {
				return fmt.Errorf("%w: request_id %q was already used with a different payload", ErrConflict, requestID)
			}
			return json.Unmarshal([]byte(stored), &res)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		s := &txState{e: e, ctx: ctx, tx: tx, actor: actor, now: e.stamp(), typeIDs: map[string]int64{},
			allowTypes: map[string]bool{}, creations: map[string]Creation{}, created: map[string]int64{}, reported: map[string]bool{}}
		seqs, err := fn(s)
		if err != nil {
			return err
		}
		nodes := make([]Node, 0, len(seqs))
		for _, seq := range seqs {
			n, err := e.nodeBySeq(ctx, tx, seq)
			if err != nil {
				return err
			}
			nodes = append(nodes, n)
		}
		refs, err := e.hydrate(ctx, tx, nodes)
		if err != nil {
			return err
		}
		// Results also name their parents, so callers know where nodes live.
		var parents []string
		for _, n := range nodes {
			if n.ParentID != "" {
				parents = append(parents, n.ParentID)
			}
		}
		more, err := e.refMap(ctx, tx, parents)
		if err != nil {
			return err
		}
		for id, r := range more {
			if _, ok := refs[id]; !ok {
				refs[id] = r
			}
		}
		if s.briefs == nil {
			s.briefs = []Brief{}
		}
		res = Result{Nodes: nodes, Refs: refs, Created: s.briefs}
		body, err := json.Marshal(res)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO requests(actor, request_id, payload_hash, response, at) VALUES (?, ?, ?, ?, ?)`, actor.Name, requestID, hash, string(body), s.now)
		return err
	})
	return res, err
}

func (s *txState) allow(creations []Creation, types []string) error {
	for _, t := range types {
		t = strings.ToLower(strings.TrimSpace(t))
		if err := validTypeName(t); err != nil {
			return err
		}
		s.allowTypes[t] = true
	}
	for _, c := range creations {
		name := strings.TrimSpace(c.Name)
		if name == "" || len(name) > maxNameKeyLen || strings.Contains(name, "[[") {
			return invalid("creation names have 1 to %d characters and no [[", maxNameKeyLen)
		}
		c.Name = name
		s.creations[nameKey(name)] = c
	}
	return nil
}

func (s *txState) fail() error {
	if len(s.unresolved) == 0 {
		return nil
	}
	return &UnresolvedError{Items: s.unresolved}
}

func (s *txState) report(kind, name string, candidates []Brief) {
	key := kind + "\x00" + nameKey(name)
	if s.reported[key] {
		return
	}
	s.reported[key] = true
	if candidates == nil {
		candidates = []Brief{}
	}
	s.unresolved = append(s.unresolved, Unresolved{Kind: kind, Name: name, Candidates: candidates})
}

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

// normalizeFields applies the fixed field rules of built-in types and,
// when asked, extracts date phrases from text.
func normalizeFields(types []string, f fields, text string, parse bool, day string) (fields, string, error) {
	ref, _ := time.Parse(dateLayout, day)
	if slices.Contains(types, TypeTask) {
		if f.Status == "" {
			f.Status = StatusOpen
		}
		if f.Status != StatusOpen && f.Status != StatusDone {
			return f, text, invalid("task status is open or done")
		}
		if f.Due == "" && parse {
			if rest, date, _ := parseDates(text, ref, false); date != "" {
				f.Due, text = date, rest
			}
		}
		if f.Due != "" {
			if err := validDate(f.Due); err != nil {
				return f, text, err
			}
		}
	} else {
		f.Status, f.Due = "", ""
	}
	if slices.Contains(types, TypeMeeting) || slices.Contains(types, TypeEvent) {
		if f.Start == "" && parse {
			if rest, date, clock := parseDates(text, ref, true); date != "" || clock != "" {
				if date == "" {
					date = day
				}
				f.Start, text = date, rest
				if clock != "" {
					f.Start += "T" + clock
				}
			}
		}
		for _, m := range []string{f.Start, f.End} {
			if m != "" {
				if err := validMoment(m); err != nil {
					return f, text, err
				}
			}
		}
		if f.End != "" && (f.Start == "" || momentKey(f.End) < momentKey(f.Start)) {
			return f, text, invalid("end requires a start and cannot precede it")
		}
	} else {
		f.Start, f.End = "", ""
	}
	return f, text, nil
}

func momentKey(m string) string {
	if len(m) == len(dateLayout) {
		return m + "T00:00"
	}
	return m
}

// placement resolves where new or moved nodes go.
func (s *txState) placement(p Placement) (parent sql.NullInt64, after int64, err error) {
	set := 0
	for _, b := range []bool{p.ParentID != "", p.Day != "", p.Root} {
		if b {
			set++
		}
	}
	if set > 1 {
		return parent, 0, invalid("placement takes one of parent_id, day or root")
	}
	if p.AfterID != "" && p.First {
		return parent, 0, invalid("placement takes after_id or first, not both")
	}
	switch {
	case p.ParentID != "":
		n, err := s.e.nodeByID(s.ctx, s.tx, p.ParentID)
		if err != nil {
			return parent, 0, err
		}
		if n.Trashed {
			return parent, 0, invalid("parent %s is in the trash", p.ParentID)
		}
		if n.Kind == KindJournal {
			return parent, 0, invalid("write into a journal day, not the journal root")
		}
		parent = sql.NullInt64{Int64: n.seq, Valid: true}
	case p.Root:
	default:
		day := p.Day
		if day == "" {
			day = s.e.today()
		}
		seq, err := s.ensureDay(day)
		if err != nil {
			return parent, 0, err
		}
		parent = sql.NullInt64{Int64: seq, Valid: true}
	}
	if p.AfterID != "" {
		n, err := s.e.nodeByID(s.ctx, s.tx, p.AfterID)
		if err != nil {
			return parent, 0, err
		}
		if n.parentSeq != parent.Int64 || n.Trashed {
			return parent, 0, invalid("after_id %s is not a child of the destination", p.AfterID)
		}
		after = n.seq
	} else if p.First {
		after = -1
	}
	return parent, after, nil
}

func (s *txState) ensureDay(day string) (int64, error) {
	if err := validDate(day); err != nil {
		return 0, err
	}
	var seq int64
	err := s.tx.QueryRowContext(s.ctx, `SELECT seq FROM nodes WHERE kind = 'day' AND day = ?`, day).Scan(&seq)
	if !errors.Is(err, sql.ErrNoRows) {
		return seq, err
	}
	pos, _ := strconv.ParseInt(strings.ReplaceAll(day, "-", ""), 10, 64)
	r, err := s.tx.ExecContext(s.ctx, `INSERT INTO nodes(id, kind, parent_seq, position, text, day, revision, created_at, updated_at, created_by, created_source, updated_by, updated_source)
		VALUES (?, 'day', ?, ?, ?, ?, 1, ?, ?, 'vrac', 'vrac', 'vrac', 'vrac')`, newID(), s.e.journal, pos, day, day, s.now, s.now)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// dayOf returns the date of the nearest journal day at or above parent, or
// today.
func (s *txState) dayOf(parent sql.NullInt64) (string, error) {
	if !parent.Valid {
		return s.e.today(), nil
	}
	var day string
	err := s.tx.QueryRowContext(s.ctx, `WITH RECURSIVE up(seq) AS (SELECT ? UNION ALL SELECT n.parent_seq FROM nodes n JOIN up ON n.seq = up.seq WHERE n.parent_seq IS NOT NULL)
		SELECT n.day FROM up JOIN nodes n ON n.seq = up.seq WHERE n.kind = 'day' LIMIT 1`, parent.Int64).Scan(&day)
	if errors.Is(err, sql.ErrNoRows) {
		return s.e.today(), nil
	}
	return day, err
}

// slot returns a free position under parent: after sibling seq after (> 0),
// first (after < 0) or last (after == 0). Siblings are renumbered only when
// no integer is left between neighbours.
func (s *txState) slot(parent sql.NullInt64, after int64, renumbered ...bool) (int64, error) {
	var lo, hi sql.NullInt64
	var err error
	switch {
	case after > 0:
		if err = s.tx.QueryRowContext(s.ctx, `SELECT position FROM nodes WHERE seq = ?`, after).Scan(&lo); err != nil {
			return 0, err
		}
		err = s.tx.QueryRowContext(s.ctx, `SELECT min(position) FROM nodes WHERE parent_seq IS ? AND position > ?`, parent, lo).Scan(&hi)
	case after < 0:
		err = s.tx.QueryRowContext(s.ctx, `SELECT min(position) FROM nodes WHERE parent_seq IS ?`, parent).Scan(&hi)
	default:
		err = s.tx.QueryRowContext(s.ctx, `SELECT max(position) FROM nodes WHERE parent_seq IS ?`, parent).Scan(&lo)
	}
	if err != nil {
		return 0, err
	}
	switch {
	case !lo.Valid && !hi.Valid:
		return positionGap, nil
	case !hi.Valid:
		return lo.Int64 + positionGap, nil
	case !lo.Valid:
		return hi.Int64 - positionGap, nil
	case hi.Int64-lo.Int64 > 1:
		return lo.Int64 + (hi.Int64-lo.Int64)/2, nil
	}
	if len(renumbered) > 0 {
		return 0, errors.New("no position available after renumbering")
	}
	if err := s.renumber(parent); err != nil {
		return 0, err
	}
	return s.slot(parent, after, true)
}

func (s *txState) renumber(parent sql.NullInt64) error {
	rows, err := s.tx.QueryContext(s.ctx, `SELECT seq FROM nodes WHERE parent_seq IS ? ORDER BY position`, parent)
	if err != nil {
		return err
	}
	var seqs []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			rows.Close()
			return err
		}
		seqs = append(seqs, seq)
	}
	rows.Close()
	// Two passes keep (parent, position) unique at every step.
	for pass, base := range []int64{-(1 << 50), 0} {
		for i, seq := range seqs {
			pos := base - int64(i) - 1
			if pass == 1 {
				pos = int64(i+1) * positionGap
			}
			if _, err := s.tx.ExecContext(s.ctx, `UPDATE nodes SET position = ? WHERE seq = ?`, pos, seq); err != nil {
				return err
			}
		}
	}
	return nil
}

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

// Create creates node trees in one transaction.
func (e *Engine) Create(ctx context.Context, actor Actor, req CreateRequest) (Result, error) {
	if len(req.Nodes) == 0 {
		return Result{}, invalid("nodes are required")
	}
	return e.mutate(ctx, actor, req.RequestID, req, func(s *txState) ([]int64, error) {
		if err := s.allow(req.Create, req.CreateTypes); err != nil {
			return nil, err
		}
		type item struct {
			draft  Draft
			parsed parsedInput
			kids   []*item
		}
		count := 0
		var parse func(d Draft, depth int) (*item, error)
		parse = func(d Draft, depth int) (*item, error) {
			if count++; count > maxDraftNodes {
				return nil, invalid("at most %d nodes per request", maxDraftNodes)
			}
			if depth > maxDepth {
				return nil, invalid("drafts are at most %d levels deep", maxDepth)
			}
			p, err := parseInput(d.Input)
			if err != nil {
				return nil, err
			}
			if err := s.check(p); err != nil {
				return nil, err
			}
			it := &item{draft: d, parsed: p}
			for _, c := range d.Children {
				k, err := parse(c, depth+1)
				if err != nil {
					return nil, err
				}
				it.kids = append(it.kids, k)
			}
			return it, nil
		}
		var items []*item
		for _, d := range req.Nodes {
			it, err := parse(d, 0)
			if err != nil {
				return nil, err
			}
			items = append(items, it)
		}
		if err := s.fail(); err != nil {
			return nil, err
		}
		parent, after, err := s.placement(req.Placement)
		if err != nil {
			return nil, err
		}
		day, err := s.dayOf(parent)
		if err != nil {
			return nil, err
		}
		var seqs []int64
		var insert func(it *item, parent sql.NullInt64, pos int64) (int64, error)
		insert = func(it *item, parent sql.NullInt64, pos int64) (int64, error) {
			r, err := s.resolve(it.parsed)
			if err != nil {
				return 0, err
			}
			d := it.draft
			f, text, err := normalizeFields(r.types, fields{d.Status, d.Due, d.Start, d.End}, r.text, req.ParseDates, day)
			if err != nil {
				return 0, err
			}
			seq, _, err := s.insert(parent, pos, text, r.typeIDs, f, d.Aliases, day)
			if err != nil {
				return 0, err
			}
			seqs = append(seqs, seq)
			for i, k := range it.kids {
				if _, err := insert(k, sql.NullInt64{Int64: seq, Valid: true}, int64(i+1)*positionGap); err != nil {
					return 0, err
				}
			}
			return seq, nil
		}
		for _, it := range items {
			pos, err := s.slot(parent, after)
			if err != nil {
				return nil, err
			}
			if after, err = insert(it, parent, pos); err != nil {
				return nil, err
			}
		}
		return seqs, nil
	})
}

func (s *txState) editable(id string, expected int64) (Node, error) {
	n, err := s.e.nodeByID(s.ctx, s.tx, id)
	if err != nil {
		return n, err
	}
	if n.Kind != KindNode {
		return n, fmt.Errorf("%w: journal and day nodes cannot be changed", ErrProtected)
	}
	if expected != n.Revision {
		return n, fmt.Errorf("%w: node %s is at revision %d, not %d", ErrConflict, id, n.Revision, expected)
	}
	nodes := []Node{n}
	_, err = s.e.hydrate(s.ctx, s.tx, nodes)
	return nodes[0], err
}

// Update changes the text, types, fields or aliases of a node.
func (e *Engine) Update(ctx context.Context, actor Actor, req UpdateRequest) (Result, error) {
	return e.mutate(ctx, actor, req.RequestID, req, func(s *txState) ([]int64, error) {
		if err := s.allow(req.Create, req.CreateTypes); err != nil {
			return nil, err
		}
		n, err := s.editable(req.ID, req.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		if n.Trashed {
			return nil, invalid("node %s is in the trash; restore it first", req.ID)
		}
		text, types, typeIDs := n.Text, n.Types, []int64(nil)
		if req.Input != nil {
			p, err := parseInput(*req.Input)
			if err != nil {
				return nil, err
			}
			if err := s.check(p); err != nil {
				return nil, err
			}
			if err := s.fail(); err != nil {
				return nil, err
			}
			r, err := s.resolve(p)
			if err != nil {
				return nil, err
			}
			text, types, typeIDs = r.text, r.types, r.typeIDs
			if req.KeepTypes && len(r.types) == 0 && len(n.Types) > 0 {
				types, typeIDs = n.Types, nil
				for _, t := range types {
					id, _, err := s.typeID(t, false)
					if err != nil {
						return nil, err
					}
					typeIDs = append(typeIDs, id)
					text = strings.TrimSpace(text + " #" + t)
				}
			}
		} else {
			for _, t := range types {
				id, _, err := s.typeID(t, false)
				if err != nil {
					return nil, err
				}
				typeIDs = append(typeIDs, id)
			}
		}
		f := fields{n.Status, n.Due, n.Start, n.End}
		for _, p := range []struct {
			src *string
			dst *string
		}{{req.Status, &f.Status}, {req.Due, &f.Due}, {req.Start, &f.Start}, {req.End, &f.End}} {
			if p.src != nil {
				*p.dst = *p.src
			}
		}
		f, text, err = normalizeFields(types, f, text, req.ParseDates, n.Day)
		if err != nil {
			return nil, err
		}
		aliases := n.Aliases
		if req.Aliases != nil {
			aliases = *req.Aliases
		}
		if text == n.Text && slices.Equal(types, n.Types) && f == (fields{n.Status, n.Due, n.Start, n.End}) && slices.Equal(aliases, n.Aliases) {
			return []int64{n.seq}, nil
		}
		if _, err := s.tx.ExecContext(ctx, `UPDATE nodes SET text = ?, name_key = ?, status = nullif(?, ''), due = nullif(?, ''), start_at = nullif(?, ''), end_at = nullif(?, '') WHERE seq = ?`,
			text, nameKeyOf(text), f.Status, f.Due, f.Start, f.End, n.seq); err != nil {
			return nil, err
		}
		if err := s.setTypes(n.seq, typeIDs); err != nil {
			return nil, err
		}
		if err := s.setAliases(n.seq, aliases); err != nil {
			return nil, err
		}
		if err := s.setRefs(n.seq, text); err != nil {
			return nil, err
		}
		if err := s.reindex(n.seq, text != n.Text || !slices.Equal(aliases, n.Aliases)); err != nil {
			return nil, err
		}
		if err := s.bump(n.seq); err != nil {
			return nil, err
		}
		return []int64{n.seq}, s.record(n.seq, "update")
	})
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

// Move moves a node and its subtree. Descendants are not rewritten.
func (e *Engine) Move(ctx context.Context, actor Actor, req MoveRequest) (Result, error) {
	return e.mutate(ctx, actor, req.RequestID, req, func(s *txState) ([]int64, error) {
		n, err := s.editable(req.ID, req.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		if n.Trashed {
			return nil, invalid("node %s is in the trash; restore it first", req.ID)
		}
		parent, after, err := s.placement(req.Placement)
		if err != nil {
			return nil, err
		}
		if after == n.seq {
			return []int64{n.seq}, nil
		}
		if parent.Valid {
			var cycle bool
			if err := s.tx.QueryRowContext(ctx, `WITH RECURSIVE up(seq) AS (SELECT ? UNION ALL SELECT n.parent_seq FROM nodes n JOIN up ON n.seq = up.seq WHERE n.parent_seq IS NOT NULL)
				SELECT EXISTS (SELECT 1 FROM up WHERE seq = ?)`, parent.Int64, n.seq).Scan(&cycle); err != nil {
				return nil, err
			}
			if cycle {
				return nil, invalid("a node cannot move into itself or its descendants")
			}
		}
		// Detach first so the node's own position does not constrain the slot.
		if _, err := s.tx.ExecContext(ctx, `UPDATE nodes SET parent_seq = NULL, position = (SELECT coalesce(min(position), 0) - 1 FROM nodes WHERE parent_seq IS NULL) WHERE seq = ?`, n.seq); err != nil {
			return nil, err
		}
		pos, err := s.slot(parent, after)
		if err != nil {
			return nil, err
		}
		if _, err := s.tx.ExecContext(ctx, `UPDATE nodes SET parent_seq = ?, position = ? WHERE seq = ?`, parent, pos, n.seq); err != nil {
			return nil, err
		}
		if err := s.bump(n.seq); err != nil {
			return nil, err
		}
		return []int64{n.seq}, s.record(n.seq, "move")
	})
}

// Trash moves a node and its subtree to the trash.
func (e *Engine) Trash(ctx context.Context, actor Actor, req TrashRequest) (Result, error) {
	return e.mutate(ctx, actor, req.RequestID, req, func(s *txState) ([]int64, error) {
		n, err := s.editable(req.ID, req.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		if n.Trashed {
			return nil, invalid("node %s is already in the trash", req.ID)
		}
		if err := s.trash(n.seq); err != nil {
			return nil, err
		}
		return []int64{n.seq}, s.record(n.seq, "trash")
	})
}

func (s *txState) trash(seq int64) error {
	if _, err := s.tx.ExecContext(s.ctx, `WITH RECURSIVE down(seq) AS (SELECT ? UNION ALL SELECT n.seq FROM nodes n JOIN down ON n.parent_seq = down.seq WHERE n.trashed_at IS NULL)
		UPDATE nodes SET trashed_at = ?, trash_root = ? WHERE seq IN (SELECT seq FROM down)`, seq, s.now, seq); err != nil {
		return err
	}
	return s.bump(seq)
}

// Restore brings a trashed node and its subtree back.
func (e *Engine) Restore(ctx context.Context, actor Actor, req TrashRequest) (Result, error) {
	return e.mutate(ctx, actor, req.RequestID, req, func(s *txState) ([]int64, error) {
		n, err := s.editable(req.ID, req.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		if err := s.restore(n); err != nil {
			return nil, err
		}
		return []int64{n.seq}, s.record(n.seq, "restore")
	})
}

func (s *txState) restore(n Node) error {
	var root sql.NullInt64
	if err := s.tx.QueryRowContext(s.ctx, `SELECT trash_root FROM nodes WHERE seq = ?`, n.seq).Scan(&root); err != nil {
		return err
	}
	if !n.Trashed || root.Int64 != n.seq {
		return invalid("node %s is not a trashed root; restore the node that was trashed", n.ID)
	}
	if _, err := s.tx.ExecContext(s.ctx, `UPDATE nodes SET trashed_at = NULL, trash_root = NULL WHERE trash_root = ?`, n.seq); err != nil {
		return err
	}
	return s.bump(n.seq)
}

// Undo reverts the latest revision of a node, as a new revision.
func (e *Engine) Undo(ctx context.Context, actor Actor, req UndoRequest) (Result, error) {
	return e.mutate(ctx, actor, req.RequestID, req, func(s *txState) ([]int64, error) {
		n, err := s.editable(req.ID, req.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		var action string
		if err := s.tx.QueryRowContext(ctx, `SELECT action FROM revisions WHERE node_seq = ? AND revision = ?`, n.seq, n.Revision).Scan(&action); err != nil {
			return nil, err
		}
		switch action {
		case "create", "restore":
			if n.Trashed {
				return nil, invalid("node %s is already in the trash", n.ID)
			}
			if err := s.trash(n.seq); err != nil {
				return nil, err
			}
		case "trash":
			if err := s.restore(n); err != nil {
				return nil, err
			}
		default:
			var raw string
			if err := s.tx.QueryRowContext(ctx, `SELECT snapshot FROM revisions WHERE node_seq = ? AND revision = ?`, n.seq, n.Revision-1).Scan(&raw); err != nil {
				return nil, err
			}
			var snap snapshot
			if err := json.Unmarshal([]byte(raw), &snap); err != nil {
				return nil, err
			}
			if err := s.apply(n, snap); err != nil {
				return nil, err
			}
		}
		return []int64{n.seq}, s.record(n.seq, "undo")
	})
}

// apply restores the content and, when still valid, the location of a
// snapshot.
func (s *txState) apply(n Node, snap snapshot) error {
	var typeIDs []int64
	for _, t := range snap.Types {
		id, _, err := s.typeID(t, false)
		if err != nil {
			return err
		}
		typeIDs = append(typeIDs, id)
	}
	if _, err := s.tx.ExecContext(s.ctx, `UPDATE nodes SET text = ?, name_key = ?, status = nullif(?, ''), due = nullif(?, ''), start_at = nullif(?, ''), end_at = nullif(?, '') WHERE seq = ?`,
		snap.Text, nameKeyOf(snap.Text), snap.Status, snap.Due, snap.Start, snap.End, n.seq); err != nil {
		return err
	}
	if err := s.setTypes(n.seq, typeIDs); err != nil {
		return err
	}
	if err := s.setAliases(n.seq, snap.Aliases); err != nil {
		return err
	}
	if err := s.setRefs(n.seq, snap.Text); err != nil {
		return err
	}
	if err := s.syncTypeTokens(n.seq); err != nil {
		return err
	}
	if err := s.reindex(n.seq, true); err != nil {
		return err
	}
	if snap.ParentID != n.ParentID {
		placement := Placement{Root: snap.ParentID == "", ParentID: snap.ParentID}
		parent, _, err := s.placement(placement)
		if err == nil {
			var cycle bool
			if parent.Valid {
				err = s.tx.QueryRowContext(s.ctx, `WITH RECURSIVE up(seq) AS (SELECT ? UNION ALL SELECT n.parent_seq FROM nodes n JOIN up ON n.seq = up.seq WHERE n.parent_seq IS NOT NULL)
					SELECT EXISTS (SELECT 1 FROM up WHERE seq = ?)`, parent.Int64, n.seq).Scan(&cycle)
			}
			if err == nil && !cycle {
				pos, err := s.slot(parent, 0)
				if err != nil {
					return err
				}
				if _, err := s.tx.ExecContext(s.ctx, `UPDATE nodes SET parent_seq = ?, position = ? WHERE seq = ?`, parent, pos, n.seq); err != nil {
					return err
				}
			}
		}
	}
	return s.bump(n.seq)
}

// CreateType creates a free type explicitly.
func (e *Engine) CreateType(ctx context.Context, actor Actor, requestID, name string) (Type, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if err := validTypeName(name); err != nil {
		return Type{}, err
	}
	_, err := e.mutate(ctx, actor, requestID, struct{ Type string }{name}, func(s *txState) ([]int64, error) {
		s.allowTypes[name] = true
		_, _, err := s.typeID(name, false)
		return nil, err
	})
	if err != nil {
		return Type{}, err
	}
	types, err := e.Types(ctx)
	for _, t := range types {
		if t.Name == name {
			return t, err
		}
	}
	return Type{}, err
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

// migrateTypeTokens is migration 2: types become visible tokens of the text,
// which is from now on their only source.
func migrateTypeTokens(e *Engine, tx *sql.Tx) error {
	s := &txState{e: e, ctx: context.Background(), tx: tx}
	rows, err := tx.Query(`SELECT DISTINCT node_seq FROM node_types`)
	if err != nil {
		return err
	}
	var seqs []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			rows.Close()
			return err
		}
		seqs = append(seqs, seq)
	}
	rows.Close()
	for _, seq := range seqs {
		if err := s.syncTypeTokens(seq); err != nil {
			return err
		}
		if err := s.index(seq); err != nil {
			return err
		}
	}
	return nil
}
