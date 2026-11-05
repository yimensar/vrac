package engine

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

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
func (s *txState) slot(parent sql.NullInt64, after int64) (int64, error) {
	pos, ok, err := s.between(parent, after)
	if err != nil || ok {
		return pos, err
	}
	if err := s.renumber(parent); err != nil {
		return 0, err
	}
	if pos, ok, err = s.between(parent, after); err == nil && !ok {
		err = errors.New("no position available after renumbering")
	}
	return pos, err
}

// between finds a position between the neighbours of a slot; ok is false
// when they are adjacent.
func (s *txState) between(parent sql.NullInt64, after int64) (pos int64, ok bool, err error) {
	var lo, hi sql.NullInt64
	switch {
	case after > 0:
		if err = s.tx.QueryRowContext(s.ctx, `SELECT position FROM nodes WHERE seq = ?`, after).Scan(&lo); err != nil {
			return 0, false, err
		}
		err = s.tx.QueryRowContext(s.ctx, `SELECT min(position) FROM nodes WHERE parent_seq IS ? AND position > ?`, parent, lo).Scan(&hi)
	case after < 0:
		err = s.tx.QueryRowContext(s.ctx, `SELECT min(position) FROM nodes WHERE parent_seq IS ?`, parent).Scan(&hi)
	default:
		err = s.tx.QueryRowContext(s.ctx, `SELECT max(position) FROM nodes WHERE parent_seq IS ?`, parent).Scan(&lo)
	}
	switch {
	case err != nil:
		return 0, false, err
	case !lo.Valid && !hi.Valid:
		return positionGap, true, nil
	case !hi.Valid:
		return lo.Int64 + positionGap, true, nil
	case !lo.Valid:
		return hi.Int64 - positionGap, true, nil
	case hi.Int64-lo.Int64 > 1:
		return lo.Int64 + (hi.Int64-lo.Int64)/2, true, nil
	}
	return 0, false, nil
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
