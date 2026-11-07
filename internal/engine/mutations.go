package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

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
			todo, err := s.anyTodo(r.typeIDs)
			if err != nil {
				return 0, err
			}
			f, err := normalizeFields(todo, fields{d.Status, d.Date, d.End}, d.DateText, day)
			if err != nil {
				return 0, err
			}
			seq, _, err := s.insert(parent, pos, r.text, r.typeIDs, f, d.Aliases, day)
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
		f := fields{n.Status, n.Date, n.End}
		for _, p := range []struct {
			src *string
			dst *string
		}{{req.Status, &f.Status}, {req.Date, &f.Date}, {req.End, &f.End}} {
			if p.src != nil {
				*p.dst = *p.src
			}
		}
		// A to-do tag gives a checkbox only when the node gains the tag,
		// so unchecking a to-do never comes back by itself.
		todo := false
		if !slices.Equal(types, n.Types) {
			if todo, err = s.anyTodo(typeIDs); err != nil {
				return nil, err
			}
		}
		dateText := ""
		if req.DateText != nil {
			dateText = *req.DateText
		}
		f, err = normalizeFields(todo, f, dateText, n.Day)
		if err != nil {
			return nil, err
		}
		aliases := n.Aliases
		if req.Aliases != nil {
			aliases = *req.Aliases
		}
		if text == n.Text && slices.Equal(types, n.Types) && f == (fields{n.Status, n.Date, n.End}) && slices.Equal(aliases, n.Aliases) {
			return []int64{n.seq}, nil
		}
		if _, err := s.tx.ExecContext(ctx, `UPDATE nodes SET text = ?, name_key = ?, status = nullif(?, ''), start_at = nullif(?, ''), end_at = nullif(?, '') WHERE seq = ?`,
			text, nameKeyOf(text), f.Status, f.Date, f.End, n.seq); err != nil {
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
		if text != n.Text {
			if err := s.refreshContext(n.seq); err != nil {
				return nil, err
			}
		}
		if f.Date != n.Date {
			if err := s.redate(n.seq); err != nil {
				return nil, err
			}
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
		if err := s.refreshMoved(n.seq); err != nil {
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
	if _, err := s.tx.ExecContext(s.ctx, `UPDATE nodes SET text = ?, name_key = ?, status = nullif(?, ''), start_at = nullif(?, ''), end_at = nullif(?, '') WHERE seq = ?`,
		snap.Text, nameKeyOf(snap.Text), snap.Status, snap.date(), snap.End, n.seq); err != nil {
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
	// Text and location may both have changed.
	if err := s.refreshMoved(n.seq); err != nil {
		return err
	}
	return s.bump(n.seq)
}

// CreateType creates a tag explicitly. With todo, its nodes get a checkbox.
func (e *Engine) CreateType(ctx context.Context, actor Actor, requestID, name string, todo bool) (Type, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if err := validTypeName(name); err != nil {
		return Type{}, err
	}
	_, err := e.mutate(ctx, actor, requestID, struct {
		Type string
		Todo bool
	}{name, todo}, func(s *txState) ([]int64, error) {
		s.allowTypes[name] = true
		id, _, err := s.typeID(name, false)
		if err != nil || !todo {
			return nil, err
		}
		_, err = s.tx.ExecContext(ctx, `UPDATE types SET todo = 1 WHERE id = ?`, id)
		return nil, err
	})
	if err != nil {
		return Type{}, err
	}
	return e.TypeNamed(ctx, name)
}

// SetTypeTodo gives or removes the to-do trait of a tag. Existing nodes
// keep their checkbox state; new ones follow the trait.
func (e *Engine) SetTypeTodo(ctx context.Context, name string, todo bool) (Type, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	err := e.write(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `UPDATE types SET todo = ? WHERE name = ?`, todo, name)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return fmt.Errorf("%w: type %s", ErrNotFound, name)
		}
		return nil
	})
	if err != nil {
		return Type{}, err
	}
	return e.TypeNamed(ctx, name)
}

// TypeNamed returns one tag.
func (e *Engine) TypeNamed(ctx context.Context, name string) (Type, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	types, err := e.Types(ctx)
	if err != nil {
		return Type{}, err
	}
	for _, t := range types {
		if t.Name == name {
			return t, nil
		}
	}
	return Type{}, fmt.Errorf("%w: type %s", ErrNotFound, name)
}

// anyTodo reports whether one of the types carries the to-do trait.
func (s *txState) anyTodo(typeIDs []int64) (bool, error) {
	for _, id := range typeIDs {
		var todo bool
		if err := s.tx.QueryRowContext(s.ctx, `SELECT todo FROM types WHERE id = ?`, id).Scan(&todo); err != nil {
			return false, err
		}
		if todo {
			return true, nil
		}
	}
	return false, nil
}
