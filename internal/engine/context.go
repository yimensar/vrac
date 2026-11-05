package engine

import (
	"context"
	"database/sql"
)

// The context index. A node concerns X when X, or a descendant of X, is the
// node itself, one of its ancestors, or referenced by it or one of its
// ancestors. The concerns table holds, for every node, the subjects it
// concerns, so context queries are index lookups.
//
// With U(t) the ancestors-or-self of t that are ordinary nodes:
//
//	C(n) = {n} ∪ U(refs(n)) ∪ C(parent(n))
//
// The table is derived: it is rebuilt by migrations and imports, and kept
// current by every change of references or location.

// contextOf returns C(parent), the inherited part of a node's context.
func (s *txState) contextOf(seq int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	rows, err := s.tx.QueryContext(s.ctx, `SELECT subject_seq FROM concerns WHERE node_seq = ?`, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var subject int64
		if err := rows.Scan(&subject); err != nil {
			return nil, err
		}
		out[subject] = true
	}
	return out, rows.Err()
}

// upward returns U(t), cached for one refresh.
func (s *txState) upward(t int64, cache map[int64][]int64) ([]int64, error) {
	if u, ok := cache[t]; ok {
		return u, nil
	}
	rows, err := s.tx.QueryContext(s.ctx, `WITH RECURSIVE up(seq) AS (SELECT ? UNION ALL SELECT n.parent_seq FROM nodes n JOIN up ON n.seq = up.seq WHERE n.parent_seq IS NOT NULL)
		SELECT n.seq FROM up JOIN nodes n ON n.seq = up.seq WHERE n.kind = 'node'`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var u []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			return nil, err
		}
		u = append(u, seq)
	}
	cache[t] = u
	return u, rows.Err()
}

// refreshContext recomputes the context of the subtrees of roots, parents
// before children. Other nodes are untouched.
func (s *txState) refreshContext(roots ...int64) error {
	cache := map[int64][]int64{}
	done := map[int64]bool{}
	for _, root := range roots {
		if done[root] {
			continue
		}
		type item struct {
			seq, parent int64
			kind        string
		}
		rows, err := s.tx.QueryContext(s.ctx, `WITH RECURSIVE sub(seq, depth) AS (SELECT ?, 0 UNION ALL SELECT n.seq, sub.depth + 1 FROM nodes n JOIN sub ON n.parent_seq = sub.seq)
			SELECT n.seq, coalesce(n.parent_seq, 0), n.kind FROM sub JOIN nodes n ON n.seq = sub.seq ORDER BY sub.depth`, root)
		if err != nil {
			return err
		}
		var items []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.seq, &it.parent, &it.kind); err != nil {
				rows.Close()
				return err
			}
			items = append(items, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		contexts := map[int64]map[int64]bool{}
		for _, it := range items {
			done[it.seq] = true
			inherited, ok := contexts[it.parent]
			if !ok && it.parent != 0 {
				if inherited, err = s.contextOf(it.parent); err != nil {
					return err
				}
			}
			c := map[int64]bool{}
			for k := range inherited {
				c[k] = true
			}
			if it.kind == KindNode {
				c[it.seq] = true
				targets, err := s.refTargets(it.seq)
				if err != nil {
					return err
				}
				for _, t := range targets {
					u, err := s.upward(t, cache)
					if err != nil {
						return err
					}
					for _, x := range u {
						c[x] = true
					}
				}
			}
			contexts[it.seq] = c
			if err := s.writeContext(it.seq, c); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *txState) refTargets(seq int64) ([]int64, error) {
	rows, err := s.tx.QueryContext(s.ctx, `SELECT target_seq FROM refs WHERE source_seq = ?`, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var t int64
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *txState) writeContext(seq int64, c map[int64]bool) error {
	if _, err := s.tx.ExecContext(s.ctx, `DELETE FROM concerns WHERE node_seq = ?`, seq); err != nil {
		return err
	}
	for subject := range c {
		if _, err := s.tx.ExecContext(s.ctx, `INSERT INTO concerns(subject_seq, node_seq, date_key) SELECT ?, seq, date_key FROM nodes WHERE seq = ?`, subject, seq); err != nil {
			return err
		}
	}
	return nil
}

// redate keeps the index's copy of a node's date current.
func (s *txState) redate(seq int64) error {
	_, err := s.tx.ExecContext(s.ctx, `UPDATE concerns SET date_key = (SELECT date_key FROM nodes WHERE seq = ?) WHERE node_seq = ?`, seq, seq)
	return err
}

// refreshMoved updates the context after seq changed location: its subtree
// inherits differently, and so do the subtrees of every node referencing
// a node of it.
func (s *txState) refreshMoved(seq int64) error {
	rows, err := s.tx.QueryContext(s.ctx, `WITH RECURSIVE sub(seq) AS (SELECT ? UNION ALL SELECT n.seq FROM nodes n JOIN sub ON n.parent_seq = sub.seq)
		SELECT DISTINCT r.source_seq FROM refs r JOIN sub ON r.target_seq = sub.seq`, seq)
	if err != nil {
		return err
	}
	roots := []int64{seq}
	for rows.Next() {
		var src int64
		if err := rows.Scan(&src); err != nil {
			rows.Close()
			return err
		}
		roots = append(roots, src)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	return s.refreshContext(roots...)
}

// rebuildContext recomputes the whole index in memory, then writes it.
func rebuildContext(ctx context.Context, tx *sql.Tx) error {
	type node struct {
		parent int64
		kind   string
		date   string
	}
	nodes := map[int64]node{}
	children := map[int64][]int64{}
	var roots []int64
	rows, err := tx.QueryContext(ctx, `SELECT seq, coalesce(parent_seq, 0), kind, date_key FROM nodes`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var seq int64
		var n node
		if err := rows.Scan(&seq, &n.parent, &n.kind, &n.date); err != nil {
			rows.Close()
			return err
		}
		nodes[seq] = n
		if n.parent == 0 {
			roots = append(roots, seq)
		} else {
			children[n.parent] = append(children[n.parent], seq)
		}
	}
	rows.Close()
	refs := map[int64][]int64{}
	rows, err = tx.QueryContext(ctx, `SELECT source_seq, target_seq FROM refs`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var src, t int64
		if err := rows.Scan(&src, &t); err != nil {
			rows.Close()
			return err
		}
		refs[src] = append(refs[src], t)
	}
	rows.Close()
	upward := map[int64][]int64{}
	up := func(t int64) []int64 {
		if u, ok := upward[t]; ok {
			return u
		}
		var u []int64
		for x := t; x != 0; x = nodes[x].parent {
			if nodes[x].kind == KindNode {
				u = append(u, x)
			}
		}
		upward[t] = u
		return u
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM concerns`); err != nil {
		return err
	}
	insert, err := tx.PrepareContext(ctx, `INSERT INTO concerns(subject_seq, node_seq, date_key) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insert.Close()
	type frame struct {
		seq       int64
		inherited map[int64]bool
	}
	stack := []frame{}
	for _, r := range roots {
		stack = append(stack, frame{r, nil})
	}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		c := make(map[int64]bool, len(f.inherited)+2)
		for k := range f.inherited {
			c[k] = true
		}
		if nodes[f.seq].kind == KindNode {
			c[f.seq] = true
			for _, t := range refs[f.seq] {
				for _, x := range up(t) {
					c[x] = true
				}
			}
		}
		for subject := range c {
			if _, err := insert.ExecContext(ctx, subject, f.seq, nodes[f.seq].date); err != nil {
				return err
			}
		}
		for _, child := range children[f.seq] {
			stack = append(stack, frame{child, c})
		}
	}
	return nil
}
