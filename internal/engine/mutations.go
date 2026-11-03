package engine

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

func (e *Engine) Create(input CreateInput) (Node, error) {
	var n Node
	err := e.mutate("create", input.RequestID, input, &n, func(tx *sql.Tx) error {
		if err := validateParent(tx, input.ParentID); err != nil {
			return err
		}
		var err error
		n, err = e.createNode(tx, input, input.ParentID, true)
		return err
	})
	return n, err
}

// CreateOutline inserts adjacent roots and their descendants in one transaction.
func (e *Engine) CreateOutline(input OutlineInput) ([]Node, error) {
	if len(input.Nodes) == 0 || len(input.Nodes) > 1000 || strings.TrimSpace(input.RequestID) == "" {
		return nil, fmt.Errorf("%w: outline requires 1 to 1000 roots and a request_id", ErrInvalid)
	}
	var result []Node
	err := e.mutate("outline", input.RequestID, input, &result, func(tx *sql.Tx) error {
		if err := validateParent(tx, input.ParentID); err != nil {
			return err
		}
		placement := input.Placement
		for _, in := range input.Nodes {
			if in.Placement != (Placement{}) {
				return fmt.Errorf("%w: root placement is determined by the outline", ErrInvalid)
			}
			in.Placement = placement
			n, err := e.createNode(tx, in, input.ParentID, true)
			if err != nil {
				return err
			}
			result = append(result, n)
			placement = Placement{After: n.ID}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (e *Engine) Capture(date string, inputs []CreateInput, requestID string, fallbackDate ...string) ([]Node, error) {
	var result []Node
	if len(inputs) == 0 || len(inputs) > 1000 {
		return nil, fmt.Errorf("%w: capture requires 1 to 1000 root nodes", ErrInvalid)
	}
	if date != "" {
		if err := validDate(date); err != nil {
			return nil, err
		}
	}
	payload := struct {
		Date  string
		Nodes []CreateInput
	}{date, inputs}
	err := e.mutate("capture", requestID, payload, &result, func(tx *sql.Tx) error {
		var err error
		date, err = defaultDate(date, fallbackDate...)
		if err != nil {
			return err
		}
		day, err := e.ensureDay(tx, date)
		if err != nil {
			return err
		}
		for _, in := range inputs {
			if in.ParentID != "" && in.ParentID != day.ID {
				return fmt.Errorf("%w: capture parent is determined by its journal date", ErrInvalid)
			}
			n, err := e.createNode(tx, in, day.ID, true)
			if err != nil {
				return err
			}
			n.JournalDate = date
			result = append(result, n)
		}
		return nil
	})
	return result, err
}
func (e *Engine) createNode(tx *sql.Tx, input CreateInput, parentID string, children bool) (Node, error) {
	if input.ParentID != "" && input.ParentID != parentID {
		return Node{}, fmt.Errorf("%w: nested node parent conflicts with its containing node", ErrInvalid)
	}
	text, tags, err := contentInput(input.Text, append([]string{}, input.Tags...))
	if err != nil {
		return Node{}, err
	}
	id, err := newID()
	if err != nil {
		return Node{}, err
	}
	pos, err := positionFor(tx, parentID, input.Placement, "")
	if err != nil {
		return Node{}, err
	}
	var parent any
	if parentID != "" {
		parent = parentID
	}
	at := now()
	if _, err = tx.Exec("INSERT INTO nodes(id,parent_id,position,text,revision,created_at,updated_at) VALUES(?,?,?,?,1,?,?)", id, parent, pos, text, at, at); err != nil {
		return Node{}, err
	}
	if err = e.setContent(tx, id, text, tags, input.ReferenceIDs); err != nil {
		return Node{}, err
	}
	if children {
		for _, child := range input.Children {
			if _, err = e.createNode(tx, child, id, true); err != nil {
				return Node{}, err
			}
		}
	}
	n, err := getNode(tx, id, false)
	if err != nil {
		return n, err
	}
	if err = e.recordRevision(tx, n, "Created"); err != nil {
		return n, err
	}
	return n, nil
}
func (e *Engine) recordRevision(tx *sql.Tx, n Node, summary string) error {
	b, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO revisions(node_id,revision,at,summary,snapshot) VALUES(?,?,?,?,?)", n.ID, n.Revision, n.UpdatedAt, summary, string(b))
	return err
}
func (e *Engine) Update(id string, input UpdateInput, fallbackDate ...string) (Node, error) {
	if input.ExpectedRevision < 1 {
		return Node{}, fmt.Errorf("%w: expected_revision is required", ErrInvalid)
	}
	var result Node
	// omitempty collapses nil and empty slices in JSON, but these inputs
	// respectively preserve and clear explicit references. Keep that intent
	// in the durable receipt identity without changing the wire format.
	payload := struct {
		ID                  string
		Input               UpdateInput
		ReferenceIDsPresent bool
	}{id, input, input.ReferenceIDs != nil}
	err := e.mutate("update", input.RequestID, payload, &result, func(tx *sql.Tx) error {
		old, err := getNode(tx, id, false)
		if err != nil {
			return err
		}
		if old.System != "" {
			return ErrProtected
		}
		if old.Revision != input.ExpectedRevision {
			return fmt.Errorf("%w: current revision is %d", ErrConflict, old.Revision)
		}
		tags := input.Tags
		if tags == nil {
			tags = old.Tags
		}
		text, tags, err := contentInput(input.Text, append([]string{}, tags...))
		if err != nil {
			return err
		}
		if err = e.setContent(tx, id, text, tags, input.ReferenceIDs); err != nil {
			return err
		}
		current, err := getNode(tx, id, false)
		if err != nil {
			return err
		}
		if old.Text == text && reflect.DeepEqual(old.Tags, current.Tags) && reflect.DeepEqual(old.References, current.References) {
			result = old
			return nil
		}
		if _, err = tx.Exec("UPDATE nodes SET text=?,revision=revision+1,updated_at=? WHERE id=?", text, now(), id); err != nil {
			return err
		}
		result, err = getNode(tx, id, false)
		if err != nil {
			return err
		}
		summary := strings.TrimSpace(input.Summary)
		if summary == "" {
			summary = "Updated note"
		}
		if len(summary) > 4096 {
			return fmt.Errorf("%w: summary must be at most 4096 bytes", ErrInvalid)
		}
		if err = e.recordRevision(tx, result, summary); err != nil {
			return err
		}
		return e.recordActivity(tx, result, input.JournalDate, summary, referenceIDs(old), fallbackDate...)
	})
	return result, err
}
func (e *Engine) recordActivity(tx *sql.Tx, target Node, date, summary string, previousContext []string, fallbackDate ...string) error {
	date, err := defaultDate(date, fallbackDate...)
	if err != nil {
		return err
	}
	// Activity notes do not recursively create another activity trail.
	var isActivity int
	if err = tx.QueryRow("SELECT count(*) FROM activities WHERE node_id=?", target.ID).Scan(&isActivity); err != nil {
		return err
	}
	if isActivity > 0 {
		return nil
	}
	day, err := e.ensureDay(tx, date)
	if err != nil {
		return err
	}
	context, err := activityContext(tx, target.ID)
	if err != nil {
		return err
	}
	set := map[string]bool{}
	for _, id := range append(context, previousContext...) {
		if id != target.ID {
			set[id] = true
		}
	}
	context = context[:0]
	for id := range set {
		context = append(context, id)
	}
	activity, err := e.createNode(tx, CreateInput{Text: summary + " [[" + target.ID + "]]", Tags: []string{"activity"}, ReferenceIDs: context}, day.ID, false)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO activities(node_id,target_id,target_revision,journal_date) VALUES(?,?,?,?)", activity.ID, target.ID, target.Revision, date)
	return err
}
func (e *Engine) Move(id string, input MoveInput, fallbackDate ...string) (Node, error) {
	if input.ExpectedRevision < 1 {
		return Node{}, fmt.Errorf("%w: expected_revision is required", ErrInvalid)
	}
	var result Node
	payload := struct {
		ID    string
		Input MoveInput
	}{id, input}
	err := e.mutate("move", input.RequestID, payload, &result, func(tx *sql.Tx) error {
		old, err := getNode(tx, id, false)
		if err != nil {
			return err
		}
		if old.System != "" {
			return ErrProtected
		}
		if old.Revision != input.ExpectedRevision {
			return fmt.Errorf("%w: current revision is %d", ErrConflict, old.Revision)
		}
		if err = validateParent(tx, input.ParentID); err != nil {
			return err
		}
		var cycle int
		err = tx.QueryRow(`WITH RECURSIVE ancestors(id,parent_id) AS (SELECT id,parent_id FROM nodes WHERE id=? UNION ALL SELECT n.id,n.parent_id FROM nodes n JOIN ancestors a ON n.id=a.parent_id) SELECT count(*) FROM ancestors WHERE id=?`, input.ParentID, id).Scan(&cycle)
		if err != nil {
			return err
		}
		if cycle > 0 || input.ParentID == id {
			return fmt.Errorf("%w: moving this node would create a cycle", ErrInvalid)
		}
		previousContext, err := activityContext(tx, id)
		if err != nil {
			return err
		}
		pos, err := positionFor(tx, input.ParentID, input.Placement, id)
		if err != nil {
			return err
		}
		var parent any
		if input.ParentID != "" {
			parent = input.ParentID
		}
		if _, err = tx.Exec("UPDATE nodes SET parent_id=?,position=?,revision=revision+1,updated_at=? WHERE id=?", parent, pos, now(), id); err != nil {
			return err
		}
		result, err = getNode(tx, id, false)
		if err != nil {
			return err
		}
		if err = e.recordRevision(tx, result, "Moved note"); err != nil {
			return err
		}
		return e.recordActivity(tx, result, input.JournalDate, "Moved note", previousContext, fallbackDate...)
	})
	return result, err
}

// Delete archives one leaf. Its revisions remain available through Revisions
// and export. Nonempty or externally referenced nodes must be reorganized first.
func (e *Engine) Delete(id string, expectedRevision int64, date, requestID string, fallbackDate ...string) error {
	payload := struct {
		ID       string
		Revision int64
		Date     string
	}{id, expectedRevision, date}
	return e.mutate("delete", requestID, payload, nil, func(tx *sql.Tx) error {
		n, err := getNode(tx, id, false)
		if err != nil {
			return err
		}
		if n.System != "" {
			return ErrProtected
		}
		if n.Revision != expectedRevision {
			return ErrConflict
		}
		if n.HasChildren {
			return fmt.Errorf("%w: move or archive children first", ErrConflict)
		}
		var count int
		err = tx.QueryRow("SELECT count(*) FROM node_references r JOIN nodes s ON s.id=r.source_id WHERE r.target_id=? AND r.source_id<>? AND s.deleted=0 AND NOT EXISTS(SELECT 1 FROM activities a WHERE a.node_id=s.id)", id, id).Scan(&count)
		if err != nil {
			return err
		}
		// Recorded activity links are intentionally retained as historical evidence.
		if count > 0 {
			return fmt.Errorf("%w: node has incoming references", ErrConflict)
		}
		activityTarget := n
		activityTarget.Revision++
		if err = e.recordActivity(tx, activityTarget, date, "Archived note", nil, fallbackDate...); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE nodes SET deleted=1,revision=revision+1,updated_at=? WHERE id=?", now(), id); err != nil {
			return err
		}
		n, err = getNode(tx, id, true)
		if err != nil {
			return err
		}
		return e.recordRevision(tx, n, "Archived note")
	})
}
