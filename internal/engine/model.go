// Package engine owns Vrac's canonical data and business rules.
package engine

import "errors"

var (
	ErrNotFound  = errors.New("node not found")
	ErrConflict  = errors.New("revision conflict")
	ErrInvalid   = errors.New("invalid input")
	ErrProtected = errors.New("protected journal node")
)

type Reference struct {
	TargetID    string `json:"target_id"`
	Label       string `json:"label"`
	SourceLabel string `json:"source_label,omitempty"`
}

type Node struct {
	ID          string      `json:"id"`
	ParentID    string      `json:"parent_id,omitempty"`
	Text        string      `json:"text"`
	Tags        []string    `json:"tags"`
	References  []Reference `json:"references"`
	Revision    int64       `json:"revision"`
	CreatedAt   string      `json:"created_at"`
	UpdatedAt   string      `json:"updated_at"`
	HasChildren bool        `json:"has_children"`
	System      string      `json:"system,omitempty"`
	JournalDate string      `json:"journal_date,omitempty"`
	Deleted     bool        `json:"deleted,omitempty"`
}

type Placement struct {
	First  bool   `json:"first,omitempty"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

type CreateInput struct {
	ParentID     string        `json:"parent_id,omitempty"`
	Text         string        `json:"text"`
	Tags         []string      `json:"tags,omitempty"`
	ReferenceIDs []string      `json:"reference_ids,omitempty"`
	Children     []CreateInput `json:"children,omitempty"`
	Placement    Placement     `json:"placement,omitempty"`
	RequestID    string        `json:"request_id,omitempty"`
}

type UpdateInput struct {
	Text             string   `json:"text"`
	Tags             []string `json:"tags"`
	ReferenceIDs     []string `json:"reference_ids,omitempty"`
	ExpectedRevision int64    `json:"expected_revision"`
	Summary          string   `json:"summary,omitempty"`
	JournalDate      string   `json:"journal_date,omitempty"`
	RequestID        string   `json:"request_id,omitempty"`
}

type MoveInput struct {
	ParentID         string    `json:"parent_id,omitempty"`
	Placement        Placement `json:"placement,omitempty"`
	ExpectedRevision int64     `json:"expected_revision"`
	JournalDate      string    `json:"journal_date,omitempty"`
	RequestID        string    `json:"request_id,omitempty"`
}

type NodePage struct {
	Nodes      []Node `json:"nodes"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type SearchFilters struct {
	Query        string   `json:"query,omitempty"`
	Tag          string   `json:"tag,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	SubjectID    string   `json:"subject_id,omitempty"`
	From         string   `json:"from,omitempty"`
	To           string   `json:"to,omitempty"`
	ActivityFrom string   `json:"activity_from,omitempty"`
	ActivityTo   string   `json:"activity_to,omitempty"`
	Cursor       string   `json:"cursor,omitempty"`
	Limit        int      `json:"limit,omitempty"`
}

type SearchItem struct {
	Node       Node   `json:"node"`
	Path       []Node `json:"path"`
	MatchedVia string `json:"matched_via,omitempty"`
}

type SearchPage struct {
	Items      []SearchItem `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type Revision struct {
	Revision int64  `json:"revision"`
	At       string `json:"at"`
	Summary  string `json:"summary"`
	Node     Node   `json:"node"`
}

type RevisionPage struct {
	Revisions  []Revision `json:"revisions"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

type CheckReport struct {
	NodeCount int64    `json:"node_count"`
	Issues    []string `json:"issues"`
}
