package engine

import (
	"database/sql"
	"fmt"
	"math"
)

func validateParent(tx *sql.Tx, parentID string) error {
	if parentID == "" {
		return nil
	}
	var system string
	err := tx.QueryRow("SELECT COALESCE(system,'') FROM nodes WHERE id=? AND deleted=0", parentID).Scan(&system)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: parent %s", ErrNotFound, parentID)
	}
	if err != nil {
		return err
	}
	if system == "journal" {
		return fmt.Errorf("%w: only journal days belong directly under Journal", ErrProtected)
	}
	return nil
}
func positionFor(tx *sql.Tx, parentID string, p Placement, movingID string) (int64, error) {
	return positionAttempt(tx, parentID, p, movingID, true)
}
func positionAttempt(tx *sql.Tx, parentID string, p Placement, movingID string, canRenumber bool) (int64, error) {
	count := 0
	if p.First {
		count++
	}
	if p.Before != "" {
		count++
	}
	if p.After != "" {
		count++
	}
	if count > 1 {
		return 0, fmt.Errorf("%w: choose one placement", ErrInvalid)
	}
	retry := func() (int64, error) {
		if !canRenumber {
			return 0, fmt.Errorf("sibling positions exhausted")
		}
		if err := renumber(tx, parentID, movingID); err != nil {
			return 0, err
		}
		return positionAttempt(tx, parentID, p, movingID, false)
	}
	var parent any
	if parentID != "" {
		parent = parentID
	}
	target := p.Before
	if target == "" {
		target = p.After
	}
	if target != "" {
		var targetParent sql.NullString
		var pos int64
		err := tx.QueryRow("SELECT parent_id,position FROM nodes WHERE id=? AND deleted=0", target).Scan(&targetParent, &pos)
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("%w: placement sibling", ErrNotFound)
		}
		if err != nil {
			return 0, err
		}
		if target == movingID || targetParent.String != parentID {
			return 0, fmt.Errorf("%w: placement target must be a different sibling", ErrInvalid)
		}
		var neighbor sql.NullInt64
		if p.Before != "" {
			err = tx.QueryRow("SELECT max(position) FROM nodes WHERE parent_id IS ? AND deleted=0 AND id<>? AND position<?", parent, movingID, pos).Scan(&neighbor)
			if err != nil {
				return 0, err
			}
			if !neighbor.Valid {
				if pos < math.MinInt64+1024 {
					return retry()
				}
				return pos - 1024, nil
			}
			if pos-neighbor.Int64 <= 1 || pos < neighbor.Int64 {
				return retry()
			}
			return neighbor.Int64 + (pos-neighbor.Int64)/2, nil
		}
		err = tx.QueryRow("SELECT min(position) FROM nodes WHERE parent_id IS ? AND deleted=0 AND id<>? AND position>?", parent, movingID, pos).Scan(&neighbor)
		if err != nil {
			return 0, err
		}
		if !neighbor.Valid {
			if pos > math.MaxInt64-1024 {
				return retry()
			}
			return pos + 1024, nil
		}
		if neighbor.Int64-pos <= 1 || neighbor.Int64 < pos {
			return retry()
		}
		return pos + (neighbor.Int64-pos)/2, nil
	}
	var edge sql.NullInt64
	agg := "max"
	if p.First {
		agg = "min"
	}
	err := tx.QueryRow("SELECT "+agg+"(position) FROM nodes WHERE parent_id IS ? AND deleted=0 AND id<>?", parent, movingID).Scan(&edge)
	if err != nil {
		return 0, err
	}
	if !edge.Valid {
		return 1024, nil
	}
	if p.First {
		if edge.Int64 < math.MinInt64+1024 {
			return retry()
		}
		return edge.Int64 - 1024, nil
	}
	if edge.Int64 > math.MaxInt64-1024 {
		return retry()
	}
	return edge.Int64 + 1024, nil
}
func renumber(tx *sql.Tx, parentID, movingID string) error {
	var parent any
	if parentID != "" {
		parent = parentID
	}
	rows, err := tx.Query("SELECT id FROM nodes WHERE parent_id IS ? AND deleted=0 AND id<>? ORDER BY position,id", parent, movingID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i, id := range ids {
		if _, err = tx.Exec("UPDATE nodes SET position=? WHERE id=?", int64(i+1)*1024, id); err != nil {
			return err
		}
	}
	return nil
}
