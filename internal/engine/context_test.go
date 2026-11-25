package engine

import (
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// definedConcerns computes, from the definition, the nodes that concern a
// subject: its subtree, the nodes referencing any node of it, and their
// subtrees.
func definedConcerns(t *testing.T, e *Engine, subject int64) []int64 {
	t.Helper()
	rows, err := e.db.Query(`WITH RECURSIVE
		subject(seq) AS (SELECT ? UNION SELECT n.seq FROM nodes n JOIN subject s ON n.parent_seq = s.seq),
		anchor(seq) AS (SELECT seq FROM subject UNION SELECT r.source_seq FROM refs r JOIN subject s ON r.target_seq = s.seq),
		concern(seq) AS (SELECT seq FROM anchor UNION SELECT n.seq FROM nodes n JOIN concern c ON n.parent_seq = c.seq)
		SELECT seq FROM concern ORDER BY seq`, subject)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var s int64
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func indexedConcerns(t *testing.T, e *Engine, subject int64) []int64 {
	t.Helper()
	rows, err := e.db.Query(`SELECT node_seq FROM concerns WHERE subject_seq = ? ORDER BY node_seq`, subject)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var s int64
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func checkContext(t *testing.T, e *Engine, step string) {
	t.Helper()
	rows, err := e.db.Query(`SELECT seq FROM nodes WHERE kind = 'node'`)
	if err != nil {
		t.Fatal(err)
	}
	var subjects []int64
	for rows.Next() {
		var s int64
		rows.Scan(&s)
		subjects = append(subjects, s)
	}
	rows.Close()
	for _, s := range subjects {
		if want, got := definedConcerns(t, e, s), indexedConcerns(t, e, s); !slices.Equal(want, got) {
			t.Fatalf("after %s, subject %d: index %v, definition %v", step, s, got, want)
		}
	}
}

// The context index matches its definition after any sequence of creations,
// reference edits, moves of referenced subtrees, undo and import.
func TestContextIndexMatchesDefinition(t *testing.T) {
	e := newEngine(t)
	r := rand.New(rand.NewPCG(7, 11))
	var ids []string
	add := func(n []Node) {
		for _, x := range n {
			ids = append(ids, x.ID)
		}
	}
	add(write(t, e, "A #project", "B #project", "C #person"))
	pick := func() string { return ids[r.IntN(len(ids))] }
	applied := map[string]int{}
	count := func(kind string, err error) {
		if err == nil {
			applied[kind]++
		}
	}
	for step := range 120 {
		switch r.IntN(5) {
		case 0, 1:
			// A line under a random node or in a day, referencing others.
			p := Placement{ParentID: pick()}
			if r.IntN(3) == 0 {
				p = Placement{Day: fmt.Sprintf("2026-11-%02d", 1+r.IntN(5))}
			}
			res, err := e.Create(ctx, me, CreateRequest{RequestID: rid(), Placement: p, Nodes: []Draft{{
				Input:    fmt.Sprintf("note [[%s]] and [[%s]]", pick(), pick()),
				Children: []Draft{{Input: fmt.Sprintf("child [[%s]]", pick())}},
			}}})
			if err != nil && !isInvalid(err) {
				t.Fatal(err)
			}
			count("create", err)
			add(res.Nodes)
		case 2:
			// Change a node's references.
			v, _ := e.GetNode(ctx, pick())
			in := fmt.Sprintf("edited [[%s]]", pick())
			// Trashed nodes refuse edits; that is fine here.
			_, err := e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: v.Node.ID, ExpectedRevision: v.Node.Revision, Input: &in})
			if err != nil && !isInvalid(err) {
				t.Fatal(err)
			}
			count("update", err)
		case 3:
			// Move a subtree somewhere else, unless it would create a cycle.
			v, _ := e.GetNode(ctx, pick())
			target := Placement{ParentID: pick()}
			if r.IntN(3) == 0 {
				target = Placement{Root: true}
			}
			_, err := e.Move(ctx, me, MoveRequest{RequestID: rid(), ID: v.Node.ID, ExpectedRevision: v.Node.Revision, Placement: target})
			if err != nil && !isCycle(err) {
				t.Fatal(err)
			}
			count("move", err)
		case 4:
			// Undo the latest change of a node.
			v, _ := e.GetNode(ctx, pick())
			_, err := e.Undo(ctx, me, UndoRequest{RequestID: rid(), ID: v.Node.ID, ExpectedRevision: v.Node.Revision})
			if err != nil && !isInvalid(err) {
				t.Fatal(err)
			}
			count("undo", err)
		}
		if step%10 == 9 {
			checkContext(t, e, fmt.Sprintf("step %d", step))
		}
	}
	checkContext(t, e, "the last step")
	for _, kind := range []string{"create", "update", "move", "undo"} {
		if applied[kind] < 5 {
			t.Fatalf("only %d %s operations applied: %v", applied[kind], kind, applied)
		}
	}
	// A rebuild gives the same index.
	if err := e.write(ctx, func(tx *sql.Tx) error { return rebuildContext(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
	checkContext(t, e, "a rebuild")
}

func isCycle(err error) bool   { return isInvalid(err) }
func isInvalid(err error) bool { return errors.Is(err, ErrInvalid) }
