package engine

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Restore restores content and metadata into the same identity and current
// location, recording a new revision. It can also recover an archived leaf.
func (e *Engine) Restore(id string, revision, expectedRevision int64, date, requestID string, fallbackDate ...string) (Node, error) {
	if revision < 1 || expectedRevision < 1 {
		return Node{}, fmt.Errorf("%w: revision and expected_revision are required", ErrInvalid)
	}
	var result Node
	payload := struct {
		ID                         string
		Revision, ExpectedRevision int64
		Date                       string
	}{id, revision, expectedRevision, date}
	err := e.mutate("restore", requestID, payload, &result, func(tx *sql.Tx) error {
		current, err := getNode(tx, id, true)
		if err != nil {
			return err
		}
		if current.System != "" {
			return ErrProtected
		}
		if current.Revision != expectedRevision {
			return ErrConflict
		}
		if err = validateParent(tx, current.ParentID); err != nil {
			return err
		}
		var raw string
		err = tx.QueryRow("SELECT snapshot FROM revisions WHERE node_id=? AND revision=?", id, revision).Scan(&raw)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%w: revision", ErrNotFound)
		}
		if err != nil {
			return err
		}
		var previous Node
		if err = json.Unmarshal([]byte(raw), &previous); err != nil {
			return err
		}
		for _, ref := range previous.References {
			if _, err = getNode(tx, ref.TargetID, false); err != nil {
				return fmt.Errorf("restore reference %s: %w", ref.TargetID, err)
			}
		}
		if _, err = tx.Exec("DELETE FROM node_references WHERE source_id=?", id); err != nil {
			return err
		}
		for _, ref := range previous.References {
			if _, err = tx.Exec("INSERT INTO node_references(source_id,token,target_id) VALUES(?,?,?)", id, ref.SourceLabel, ref.TargetID); err != nil {
				return err
			}
		}
		if err = e.setContent(tx, id, previous.Text, previous.Tags, nil); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE nodes SET text=?,deleted=0,revision=revision+1,updated_at=? WHERE id=?", previous.Text, now(), id); err != nil {
			return err
		}
		result, err = getNode(tx, id, false)
		if err != nil {
			return err
		}
		summary := fmt.Sprintf("Restored revision %d", revision)
		if err = e.recordRevision(tx, result, summary); err != nil {
			return err
		}
		return e.recordActivity(tx, result, date, summary, referenceIDs(current), fallbackDate...)
	})
	return result, err
}
