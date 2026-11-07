package engine

// Tag colors, given in order of creation.
var palette = []string{"blue", "violet", "amber", "emerald", "rose", "cyan", "lime", "orange", "fuchsia", "teal", "sky", "pink"}

// Checkbox states. A node without a checkbox has an empty status.
const (
	StatusOpen = "open"
	StatusDone = "done"
)

// Node kinds.
const (
	KindNode    = "node"
	KindJournal = "journal"
	KindDay     = "day"
)

// Actor identifies who performs a mutation and from which source.
type Actor struct {
	// Name is the authenticated principal, such as "owner" or "Claude".
	Name string
	// Source is where the information came from, such as "web" or "outlook".
	Source string
}

// Type is a tag: a name, a color, and one optional trait, Todo, which
// gives the nodes carrying it a checkbox. Tags have no other fields; they
// are the user's own.
type Type struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Todo  bool   `json:"todo"`
	Count int    `json:"count"`
}

// Node is the public representation of a node. Text is canonical: references
// are encoded as [[ID]] and resolved through the Refs map of the response.
type Node struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	ParentID string   `json:"parent_id,omitempty"`
	Text     string   `json:"text"`
	Types    []string `json:"types"`
	// Status is the checkbox: empty for none, open or done.
	Status string `json:"status,omitempty"`
	// Date is a day or a day and time (YYYY-MM-DD or YYYY-MM-DDTHH:MM); End
	// closes a range. Any node may carry them.
	Date       string   `json:"date,omitempty"`
	End        string   `json:"end,omitempty"`
	Aliases    []string `json:"aliases"`
	Day        string   `json:"day"`
	Revision   int64    `json:"revision"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
	CreatedBy  string   `json:"created_by"`
	UpdatedBy  string   `json:"updated_by"`
	Source     string   `json:"source"`
	ChildCount int      `json:"child_count"`
	Trashed    bool     `json:"trashed,omitempty"`
	RefIDs     []string `json:"ref_ids"`
	seq        int64
	parentSeq  int64
	position   int64
}

// Ref describes a referenced node for display.
type Ref struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Label is the name keeping the #type tokens written inside it.
	Label   string   `json:"label"`
	Types   []string `json:"types"`
	Trashed bool     `json:"trashed,omitempty"`
}

// Brief is a compact node description used in candidates and paths.
type Brief struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Label string   `json:"label,omitempty"`
	Types []string `json:"types"`
	Path  []string `json:"path,omitempty"`
}

// Page is a bounded list of nodes with the references they use. Subjects
// is set by searches asking for it: the typed nodes each result concerns.
type Page struct {
	Nodes      []Node              `json:"nodes"`
	Refs       map[string]Ref      `json:"refs"`
	Subjects   map[string][]string `json:"subjects,omitempty"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

// Draft describes a node to create. Input uses the Vrac syntax: #tag marks
// a tag, [[Name]] or [[ID]] references a node, [[Parent/Child]] a child of
// a named node, and #tag/Name references Name, which carries that tag.
// DateText is a phrase such as "Friday 14h" read relative to the day
// written, an alternative to Date.
type Draft struct {
	Input    string   `json:"input"`
	Status   string   `json:"status,omitempty"`
	Date     string   `json:"date,omitempty"`
	End      string   `json:"end,omitempty"`
	DateText string   `json:"date_text,omitempty"`
	Aliases  []string `json:"aliases,omitempty"`
	Children []Draft  `json:"children,omitempty"`
}

// Creation explicitly allows creating a referenced node that does not exist.
// Name may be "Parent/Child" to create Child under an existing or created
// Parent. Without a parent the node is created at the root.
type Creation struct {
	Name  string   `json:"name"`
	Types []string `json:"types,omitempty"`
}

// Placement locates new or moved nodes. With no parent, nodes go to the
// journal day Day (today by default). Root places them at the root. After
// places them right after a sibling; First at the beginning; otherwise they
// are appended.
type Placement struct {
	ParentID string `json:"parent_id,omitempty"`
	Day      string `json:"day,omitempty"`
	Root     bool   `json:"root,omitempty"`
	AfterID  string `json:"after_id,omitempty"`
	First    bool   `json:"first,omitempty"`
}

// CreateRequest creates one or more node trees in one transaction.
type CreateRequest struct {
	RequestID   string     `json:"request_id"`
	Placement   Placement  `json:"placement"`
	Nodes       []Draft    `json:"nodes"`
	Create      []Creation `json:"create,omitempty"`
	CreateTypes []string   `json:"create_types,omitempty"`
}

// UpdateRequest changes one node. Nil fields are left unchanged; an empty
// string clears a field. When Input is given, it replaces the text and the
// types.
type UpdateRequest struct {
	RequestID        string     `json:"request_id"`
	ID               string     `json:"id"`
	ExpectedRevision int64      `json:"expected_revision"`
	Input            *string    `json:"input,omitempty"`
	Status           *string    `json:"status,omitempty"`
	Date             *string    `json:"date,omitempty"`
	End              *string    `json:"end,omitempty"`
	DateText         *string    `json:"date_text,omitempty"`
	Aliases          *[]string  `json:"aliases,omitempty"`
	Create           []Creation `json:"create,omitempty"`
	CreateTypes      []string   `json:"create_types,omitempty"`
	// KeepTypes keeps the node's types when Input writes none, appending
	// their tokens. AI clients rewriting a sentence then cannot drop a type,
	// and its fields, by omission.
	KeepTypes bool `json:"keep_types,omitempty"`
}

// MoveRequest moves a node and its subtree.
type MoveRequest struct {
	RequestID        string    `json:"request_id"`
	ID               string    `json:"id"`
	ExpectedRevision int64     `json:"expected_revision"`
	Placement        Placement `json:"placement"`
}

// TrashRequest moves a node and its subtree to the trash, or restores it.
type TrashRequest struct {
	RequestID        string `json:"request_id"`
	ID               string `json:"id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

// UndoRequest reverts the latest revision of a node.
type UndoRequest struct {
	RequestID        string `json:"request_id"`
	ID               string `json:"id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

// Result is returned by mutations.
type Result struct {
	Nodes []Node         `json:"nodes"`
	Refs  map[string]Ref `json:"refs"`
	// Created lists nodes created through Creation entries.
	Created []Brief `json:"created,omitempty"`
}

// Unresolved is returned, wrapped in an UnresolvedError, when input names
// types or nodes that do not exist and were not explicitly created.
type Unresolved struct {
	Kind       string  `json:"kind"`
	Name       string  `json:"name"`
	Candidates []Brief `json:"candidates"`
}

// UnresolvedError lists every unresolved name of a request.
type UnresolvedError struct{ Items []Unresolved }

func (e *UnresolvedError) Error() string {
	s := "unresolved names:"
	for _, u := range e.Items {
		s += " " + u.Kind + " " + u.Name + ";"
	}
	return s + " reuse a candidate or create explicitly"
}

func (e *UnresolvedError) Unwrap() error { return ErrInvalid }
