package engine

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
)

func TestReviewKeepsATodoUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vrac.sqlite")
	e := open(t, path)
	nodes := write(t, e, "Call the bank #task", "A plain note")
	task, note := nodes[0], nodes[1]

	res, err := e.Review(ctx, me, ReviewRequest{RequestID: "rv1", ID: task.ID, ExpectedRevision: task.Revision})
	if err != nil {
		t.Fatal(err)
	}
	got := res.Nodes[0]
	if got.ReviewedAt == "" || got.Revision != task.Revision || got.Text != task.Text || got.UpdatedAt != task.UpdatedAt {
		t.Fatalf("reviewed = %+v", got)
	}
	if again, err := e.Review(ctx, me, ReviewRequest{RequestID: "rv1", ID: task.ID, ExpectedRevision: task.Revision}); err != nil || again.Nodes[0].ReviewedAt != got.ReviewedAt {
		t.Fatalf("replay = %+v, %v", again, err)
	}
	if _, err := e.Review(ctx, me, ReviewRequest{RequestID: rid(), ID: note.ID, ExpectedRevision: note.Revision}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a note was reviewed: %v", err)
	}
	if _, err := e.Review(ctx, me, ReviewRequest{RequestID: rid(), ID: task.ID, ExpectedRevision: task.Revision + 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}

	// The review survives reopening, and an export and import.
	e.Close()
	e = open(t, path)
	if n, err := e.GetNode(ctx, task.ID); err != nil || n.Node.ReviewedAt != got.ReviewedAt {
		t.Fatalf("reopened = %+v, %v", n.Node, err)
	}
	var buf bytes.Buffer
	if err := e.Export(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	fresh, err := Open(filepath.Join(t.TempDir(), "vrac.sqlite"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if err := fresh.Import(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	if n, err := fresh.GetNode(ctx, task.ID); err != nil || n.Node.ReviewedAt != got.ReviewedAt {
		t.Fatalf("imported = %+v, %v", n.Node, err)
	}
}
