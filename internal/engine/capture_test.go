package engine

import (
	"errors"
	"slices"
	"testing"
)

func TestCaptureKeepsUnknownNamesAsPlainWords(t *testing.T) {
	e := newEngine(t)
	northwind := write(t, e, "Northwind #project")[0]
	res, err := e.Capture(ctx, me, "c1", "Call [[Northwind]] about #tsak and [[Suze]], again #tsak #task")
	if err != nil {
		t.Fatal(err)
	}
	n := res.Nodes[0]
	if n.Text != "Call [["+northwind.ID+"]] about tsak and Suze, again tsak #task" {
		t.Fatalf("text = %q", n.Text)
	}
	if !slices.Equal(n.Types, []string{"task"}) || n.Status != StatusOpen || !slices.Equal(n.RefIDs, []string{northwind.ID}) {
		t.Fatalf("node = %+v", n)
	}
	if len(res.Unresolved) != 2 || res.Unresolved[0].Name != "tsak" || res.Unresolved[1].Name != "Suze" {
		t.Fatalf("unresolved = %+v", res.Unresolved)
	}
	if res.Unresolved[0].Candidates[0].Name != "task" {
		t.Fatalf("candidates = %+v", res.Unresolved[0].Candidates)
	}
	types, err := e.Types(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range types {
		if typ.Name == "tsak" {
			t.Fatal("a capture created a type")
		}
	}

	// The capture lands at the end of today's journal, once per request.
	again, err := e.Capture(ctx, me, "c1", "Call [[Northwind]] about #tsak and [[Suze]], again #tsak #task")
	if err != nil || again.Nodes[0].ID != n.ID || len(again.Unresolved) != 2 {
		t.Fatalf("replay = %+v, %v", again, err)
	}
	day, _ := e.Day(ctx, "", "", 0)
	if got := day.Children.Nodes; len(got) != 2 || got[1].ID != n.ID {
		t.Fatalf("journal = %+v", got)
	}
	if _, err := e.Capture(ctx, me, "c1", "something else"); !errors.Is(err, ErrConflict) {
		t.Fatalf("reused request = %v", err)
	}
}

func TestCaptureRejectsEmptyText(t *testing.T) {
	e := newEngine(t)
	if _, err := e.Capture(ctx, me, rid(), " \r\n "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}
