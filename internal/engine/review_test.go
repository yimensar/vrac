package engine

import (
	"bytes"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
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

func TestReviewQueue(t *testing.T) {
	e := open(t, filepath.Join(t.TempDir(), "vrac.sqlite"))
	if _, err := e.CreateType(ctx, me, rid(), "someday", false); err != nil {
		t.Fatal(err)
	}
	project := write(t, e, "Northwind #project")[0]
	nodes := write(t, e, "Call the bank #task", "Learn Rust #task #someday", "Send the deck [["+project.ID+"]] #task", "Done already #task", "A plain note")
	bank, rust, deck, done := nodes[0], nodes[1], nodes[2], nodes[3]
	if _, err := e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: done.ID, ExpectedRevision: done.Revision, Status: ptr(StatusDone)}); err != nil {
		t.Fatal(err)
	}
	queue := func(days int) []string {
		t.Helper()
		e.now = func() time.Time { return clock.Add(time.Duration(days) * 24 * time.Hour) }
		page, err := e.ReviewQueue(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return ids(page.Nodes)
	}
	if got := queue(1); len(got) != 0 {
		t.Fatalf("fresh to-dos queued: %v", got)
	}
	// A week later the open to-dos come back, but not the someday one.
	if got := queue(8); !slices.Equal(got, []string{bank.ID, deck.ID}) {
		t.Fatalf("week queue = %v", got)
	}
	// Looking at one sends it back a week; the longest unseen comes first.
	if _, err := e.Review(ctx, me, ReviewRequest{RequestID: rid(), ID: bank.ID, ExpectedRevision: bank.Revision}); err != nil {
		t.Fatal(err)
	}
	if got := queue(9); !slices.Equal(got, []string{deck.ID}) {
		t.Fatalf("after review = %v", got)
	}
	page, _ := e.ReviewQueue(ctx)
	if s := page.Subjects[deck.ID]; len(s) != 1 || s[0] != project.ID {
		t.Fatalf("subjects = %v", page.Subjects)
	}
	if got := queue(31); !slices.Equal(got, []string{rust.ID, deck.ID, bank.ID}) {
		t.Fatalf("month queue = %v", got)
	}
}

func TestRetag(t *testing.T) {
	for _, c := range []struct{ in, add, want string }{
		{"Call [[n1]] #task #someday", "next", "Call [[n1]] #task #next"},
		{"#next Call #task", "next", "#next Call #task"},
		{"Call #task #next", "", "Call #task"},
		{"#someday Learn #task", "next", "Learn #task #next"},
	} {
		got, err := Retag(c.in, c.add, "next", "someday")
		if err != nil || got != c.want {
			t.Errorf("Retag(%q, %q) = %q, %v; want %q", c.in, c.add, got, err, c.want)
		}
	}
}

func TestReviewDecisions(t *testing.T) {
	e := open(t, filepath.Join(t.TempDir(), "vrac.sqlite"))
	for _, name := range []string{"next", "someday"} {
		if _, err := e.CreateType(ctx, me, rid(), name, false); err != nil {
			t.Fatal(err)
		}
	}
	nodes := write(t, e, "Call the bank #task #someday", "Send the deck #task", "Old idea #task", "Book the train #task")
	bank, deck, idea, train := nodes[0], nodes[1], nodes[2], nodes[3]
	decide := func(n Node, action, date string) Node {
		t.Helper()
		res, err := e.Review(ctx, me, ReviewRequest{RequestID: rid(), ID: n.ID, ExpectedRevision: n.Revision, Action: action, DateText: date})
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		return res.Nodes[0]
	}
	if got := decide(bank, ReviewNext, ""); got.Text != "Call the bank #task #next" || !slices.Equal(got.Types, []string{"task", "next"}) || got.Status != StatusOpen {
		t.Fatalf("next = %+v", got)
	}
	if got := decide(deck, ReviewSomeday, ""); got.Text != "Send the deck #task #someday" {
		t.Fatalf("someday = %+v", got)
	}
	if got := decide(idea, ReviewDone, ""); got.Status != StatusDone {
		t.Fatalf("done = %+v", got)
	}
	if got := decide(train, ReviewDate, "2026-11-20"); got.Date != "2026-11-20" {
		t.Fatalf("date = %+v", got)
	}
	train, _ = nodeOf(t, e, train.ID)
	decide(train, ReviewTrash, "")
	if n, err := e.GetNode(ctx, train.ID); err != nil || !n.Node.Trashed {
		t.Fatalf("trash = %+v, %v", n.Node, err)
	}
	if _, err := e.Review(ctx, me, ReviewRequest{RequestID: rid(), ID: idea.ID, ExpectedRevision: idea.Revision, Action: "later"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown action: %v", err)
	}
}

func nodeOf(t *testing.T, e *Engine, id string) (Node, error) {
	t.Helper()
	v, err := e.GetNode(ctx, id)
	return v.Node, err
}
