package engine

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var ctx = context.Background()

var me = Actor{Name: "owner", Source: "web"}

// Thursday 5 November 2026, 10:00 in Paris.
var clock = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

// open opens a database with the tags most tests write: nothing is built
// in, so a test workspace creates its own.
func open(t *testing.T, path string) *Engine {
	t.Helper()
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	e, err := Open(path, Options{Location: paris, Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	for _, name := range []string{"task", "meeting", "event", "decision", "project", "person"} {
		if _, err := e.CreateType(ctx, me, rid(), name, name == "task"); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func newEngine(t *testing.T) *Engine {
	return open(t, filepath.Join(t.TempDir(), "vrac.sqlite"))
}

var requests int

func rid() string { requests++; return fmt.Sprintf("r%d", requests) }

func create(t *testing.T, e *Engine, req CreateRequest) Result {
	t.Helper()
	if req.RequestID == "" {
		req.RequestID = rid()
	}
	res, err := e.Create(ctx, me, req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func write(t *testing.T, e *Engine, inputs ...string) []Node {
	t.Helper()
	var drafts []Draft
	for _, in := range inputs {
		drafts = append(drafts, Draft{Input: in})
	}
	return create(t, e, CreateRequest{Nodes: drafts}).Nodes
}

func ids(nodes []Node) []string {
	out := []string{}
	for _, n := range nodes {
		out = append(out, n.ID)
	}
	return out
}

func search(t *testing.T, e *Engine, q Query) []Node {
	t.Helper()
	p, err := e.Search(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	return p.Nodes
}

func TestReopenAndRefuseForeignDatabases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vrac.sqlite")
	e := open(t, path)
	n := write(t, e, "Northwind #project")[0]
	e.Close()
	e = open(t, path)
	v, err := e.GetNode(ctx, n.ID)
	if err != nil || v.Node.Text != "Northwind #project" || !slices.Equal(v.Node.Types, []string{"project"}) {
		t.Fatalf("reopened node = %+v, %v", v.Node, err)
	}
	if err := e.Check(); err != nil {
		t.Fatal(err)
	}

	other := filepath.Join(t.TempDir(), "other.sqlite")
	db, err := sql.Open("sqlite3", other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE t(x)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(other, Options{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign database opened: %v", err)
	}
}

func TestWritesGoToTodaysJournal(t *testing.T) {
	e := newEngine(t)
	write(t, e, "first", "second")
	day, err := e.Day(ctx, "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if day.Day != "2026-11-05" || !day.Exists {
		t.Fatalf("day = %+v", day)
	}
	var texts []string
	for _, n := range day.Children.Nodes {
		texts = append(texts, n.Text)
	}
	if !slices.Equal(texts, []string{"first", "second"}) {
		t.Fatalf("children = %v", texts)
	}
	empty, err := e.Day(ctx, "2026-11-04", "", 0)
	if err != nil || empty.Exists {
		t.Fatalf("reading a day must not create it: %+v %v", empty, err)
	}
}

func TestUnknownNamesAreRejectedAtomically(t *testing.T) {
	e := newEngine(t)
	write(t, e, "Northwind #project")
	_, err := e.Create(ctx, me, CreateRequest{RequestID: rid(), Nodes: []Draft{{Input: "ok"}, {Input: "call [[Suze]] #tsak"}}})
	var u *UnresolvedError
	if !errors.As(err, &u) || !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if len(u.Items) != 2 || u.Items[0].Kind != "type" || u.Items[1].Kind != "node" {
		t.Fatalf("unresolved = %+v", u.Items)
	}
	if u.Items[0].Candidates[0].Name != "task" {
		t.Fatalf("type candidates = %+v", u.Items[0].Candidates)
	}
	day, _ := e.Day(ctx, "", "", 0)
	if len(day.Children.Nodes) != 1 {
		t.Fatalf("failed request wrote nodes: %d", len(day.Children.Nodes))
	}
}

func TestExplicitCreationAndShortcut(t *testing.T) {
	e := newEngine(t)
	res := create(t, e, CreateRequest{
		Nodes:  []Draft{{Input: "Lunch with [[Dupont]] about #project/Northwind"}},
		Create: []Creation{{Name: "Dupont", Types: []string{"person"}}, {Name: "Northwind"}},
	})
	if len(res.Created) != 2 {
		t.Fatalf("created = %+v", res.Created)
	}
	lunch := res.Nodes[0]
	if len(lunch.Types) != 0 || len(lunch.RefIDs) != 2 {
		t.Fatalf("lunch = %+v", lunch)
	}
	if r := res.Refs[lunch.RefIDs[1]]; r.Name != "Northwind" || !slices.Equal(r.Types, []string{"project"}) {
		t.Fatalf("Northwind ref = %+v", r)
	}
	roots, _ := e.Roots(ctx, "", 0)
	if len(roots.Nodes) != 2 {
		t.Fatalf("created references live at the root: %d", len(roots.Nodes))
	}
	// Existing names resolve; aliases too.
	northwind := lunch.RefIDs[1]
	n, _ := e.GetNode(ctx, northwind)
	if _, err := e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: northwind, ExpectedRevision: n.Node.Revision, Aliases: &[]string{"SZ"}}); err != nil {
		t.Fatal(err)
	}
	again := write(t, e, "follow-up [[sz]]")[0]
	if !slices.Equal(again.RefIDs, []string{northwind}) {
		t.Fatalf("alias resolution = %v", again.RefIDs)
	}
}

func TestFreeTypesNeedExplicitCreation(t *testing.T) {
	e := newEngine(t)
	if _, err := e.Create(ctx, me, CreateRequest{RequestID: rid(), Nodes: []Draft{{Input: "Dune #reading"}}}); err == nil {
		t.Fatal("unknown type accepted")
	}
	n := create(t, e, CreateRequest{Nodes: []Draft{{Input: "Dune #reading"}}, CreateTypes: []string{"reading"}}).Nodes[0]
	if !slices.Equal(n.Types, []string{"reading"}) || n.Status != "" {
		t.Fatalf("node = %+v", n)
	}
	types, _ := e.Types(ctx)
	i := slices.IndexFunc(types, func(t Type) bool { return t.Name == "reading" })
	if i < 0 || types[i].Count != 1 || types[i].Todo {
		t.Fatalf("types = %+v", types)
	}
}

// Tags carry no fields: any node may have a date. The checkbox is the
// to-do tag: it comes and goes with it.
func TestCheckboxIsTheTodoTag(t *testing.T) {
	e := newEngine(t)
	book := create(t, e, CreateRequest{Nodes: []Draft{{Input: "Dune #book", DateText: "tomorrow 14h"}}, CreateTypes: []string{"book"}}).Nodes[0]
	if book.Status != "" || book.Date != "2026-11-06T14:00" || book.Text != "Dune #book" {
		t.Fatalf("book = %+v", book)
	}
	open := StatusOpen
	if _, err := e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: book.ID, ExpectedRevision: 1, Status: &open}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("checkbox without a to-do tag accepted: %v", err)
	}
	// Adding the tag opens the checkbox; removing it takes it away.
	in := "Dune #book #task"
	up, err := e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: book.ID, ExpectedRevision: 1, Input: &in})
	if err != nil || up.Nodes[0].Status != StatusOpen {
		t.Fatalf("tagged = %+v, %v", up.Nodes, err)
	}
	done := StatusDone
	up, _ = e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: book.ID, ExpectedRevision: 2, Status: &done})
	in = "Dune #book"
	up, _ = e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: book.ID, ExpectedRevision: 3, Input: &in})
	if up.Nodes[0].Status != "" {
		t.Fatalf("checkbox outlived the tag: %+v", up.Nodes[0])
	}
	none := ""
	up, _ = e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: book.ID, ExpectedRevision: 4, Date: &none})
	if up.Nodes[0].Date != "" {
		t.Fatalf("date not cleared = %+v", up.Nodes[0])
	}
	// The trait given later opens the nodes carrying the tag; removed, it
	// closes them unless another to-do tag remains.
	other := write(t, e, "Foundation #book")[0]
	if _, err := e.SetTypeTodo(ctx, "book", true); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.GetNode(ctx, other.ID); v.Node.Status != StatusOpen {
		t.Fatalf("book after trait = %+v", v.Node)
	}
	both := write(t, e, "Read it #book #task")[0]
	if _, err := e.SetTypeTodo(ctx, "book", false); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.GetNode(ctx, other.ID); v.Node.Status != "" {
		t.Fatalf("book after trait removed = %+v", v.Node)
	}
	if v, _ := e.GetNode(ctx, both.ID); v.Node.Status != StatusOpen {
		t.Fatalf("task after book trait removed = %+v", v.Node)
	}
	if _, err := e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: book.ID, ExpectedRevision: 5, DateText: ptr("someday")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad date phrase accepted: %v", err)
	}
	end := "2026-11-01"
	date := "2026-11-02"
	if _, err := e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: book.ID, ExpectedRevision: 5, Date: &date, End: &end}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("end before date accepted: %v", err)
	}
}

func ptr(s string) *string { return &s }

// Scenario 4 of the specification.
func TestContextIsInherited(t *testing.T) {
	e := newEngine(t)
	write(t, e, "Northwind #project", "Globex #project", "Marc #person")
	northwind := search(t, e, Query{Text: "northwind", Types: []string{"project"}})[0]
	marc := search(t, e, Query{Text: "marc"})[0]
	meeting := create(t, e, CreateRequest{Nodes: []Draft{{
		Input:    "Copil [[Northwind]] with [[Marc]] #meeting",
		DateText: "14h",
		Children: []Draft{
			{Input: "Lot 2 is moved to January #decision"},
			{Input: "Follow up with Marc on the schedule #task", DateText: "Friday"},
		},
	}}}).Nodes
	if meeting[0].Date != "2026-11-05T14:00" || meeting[0].Text != "Copil [["+northwind.ID+"]] with [["+marc.ID+"]] #meeting" {
		t.Fatalf("meeting = %+v", meeting[0])
	}
	if meeting[2].Date != "2026-11-06" || meeting[2].Status != StatusOpen || meeting[2].Text != "Follow up with Marc on the schedule #task" {
		t.Fatalf("task = %+v", meeting[2])
	}
	write(t, e, "We use API v2 rather than CSV [[Globex]] #decision")

	if due := search(t, e, Query{Types: []string{"task"}, To: "2026-11-10", Dated: true}); len(due) != 1 {
		t.Fatalf("dated tasks = %v", ids(due))
	}
	tasks := search(t, e, Query{Concerns: []string{northwind.ID}, Types: []string{"task"}, Status: StatusOpen})
	if !slices.Equal(ids(tasks), []string{meeting[2].ID}) {
		t.Fatalf("open tasks on Northwind = %v", ids(tasks))
	}
	decisions := search(t, e, Query{Concerns: []string{northwind.ID}, Types: []string{"decision"}})
	if !slices.Equal(ids(decisions), []string{meeting[1].ID}) {
		t.Fatalf("decisions on Northwind = %v", ids(decisions))
	}
	both := search(t, e, Query{Concerns: []string{northwind.ID, marc.ID}, Types: []string{"task"}})
	if len(both) != 1 {
		t.Fatalf("Northwind and Marc = %v", ids(both))
	}
	// A node's page gathers, by tag, what concerns it from outside its own
	// outline: the meeting, its decision and its task.
	lists, err := e.NodeLists(ctx, northwind.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lists, []ListCount{{"decision", 1}, {"meeting", 1}, {"task", 1}}) {
		t.Fatalf("lists = %+v", lists)
	}
	write(t, e, "Lunch about [[Northwind]] #meeting")
	create(t, e, CreateRequest{Placement: Placement{ParentID: northwind.ID}, Nodes: []Draft{{Input: "a note inside, not listed"}}})
	if lists, _ = e.NodeLists(ctx, northwind.ID); !slices.Equal(lists, []ListCount{{"meeting", 2}, {"decision", 1}, {"task", 1}}) {
		t.Fatalf("lists = %+v", lists)
	}
	if got := search(t, e, Query{Concerns: []string{northwind.ID}, Types: []string{"meeting"}, Outside: true}); len(got) != 2 {
		t.Fatalf("meetings about Northwind = %v", ids(got))
	}
	if got := search(t, e, Query{Concerns: []string{northwind.ID}, Untagged: true}); len(got) != 1 || got[0].Text != "a note inside, not listed" {
		t.Fatalf("untagged = %v", ids(got))
	}
	if got := search(t, e, Query{Concerns: []string{northwind.ID}, Untagged: true, Outside: true}); len(got) != 0 {
		t.Fatalf("outside untagged = %v", ids(got))
	}
	// Types are not inherited: the task is not a meeting.
	meetings := search(t, e, Query{Types: []string{"meeting"}, Dated: true})
	if len(meetings) != 1 {
		t.Fatalf("meetings = %v", ids(meetings))
	}
}

func TestSearchListsSubjects(t *testing.T) {
	e := newEngine(t)
	northwind := write(t, e, "Northwind #project", "Marc #person")[0]
	lot := create(t, e, CreateRequest{Placement: Placement{ParentID: northwind.ID}, Nodes: []Draft{{Input: "Lot 2 #project"}}}).Nodes[0]
	meeting := create(t, e, CreateRequest{Nodes: []Draft{{
		Input:    "Point [[Lot 2]] with [[Marc]] #meeting",
		Children: []Draft{{Input: "Send the plan #task"}},
	}}}).Nodes
	loose := write(t, e, "Buy milk #task")[0]
	p, err := e.Search(ctx, Query{Types: []string{"task"}, Status: StatusOpen, Subjects: true})
	if err != nil {
		t.Fatal(err)
	}
	// The task inherits its meeting's context, most specific first: the
	// meeting, then Lot 2 before its parent Northwind, and Marc; never
	// itself.
	got := p.Subjects[meeting[1].ID]
	if len(got) != 4 || got[0] != meeting[0].ID || slices.Index(got, lot.ID) > slices.Index(got, northwind.ID) {
		t.Fatalf("subjects = %v (meeting %s, lot %s, northwind %s)", got, meeting[0].ID, lot.ID, northwind.ID)
	}
	if len(p.Subjects[loose.ID]) != 0 {
		t.Fatalf("loose task concerns %v", p.Subjects[loose.ID])
	}
	if r, ok := p.Refs[lot.ID]; !ok || r.Name != "Lot 2" || !slices.Contains(r.Types, "project") {
		t.Fatalf("subjects are not described: %+v", p.Refs[lot.ID])
	}
	if p, _ := e.Search(ctx, Query{Types: []string{"task"}}); p.Subjects != nil {
		t.Fatal("subjects listed without being asked")
	}
}

// Regression: types were removed from the text, so "#meeting with Alice
// about Globex" became "with Alice about Globex".
func TestTypesStayWhereWritten(t *testing.T) {
	e := newEngine(t)
	write(t, e, "Alice #person", "Globex #project")
	m := write(t, e, "#meeting with [[Alice]] about [[Globex]] 10h")[0]
	if m.Text[:9] != "#meeting " || !slices.Equal(m.Types, []string{"meeting"}) {
		t.Fatalf("meeting = %q %v", m.Text, m.Types)
	}
	// Names ignore trailing types and keep inner ones as words.
	if got := search(t, e, Query{Text: "meeting alice"}); len(got) != 1 {
		t.Fatalf("search = %v", ids(got))
	}
	if c, _ := e.Resolve(ctx, "Globex", 1); len(c) != 1 || c[0].Name != "Globex" {
		t.Fatalf("resolve = %+v", c)
	}
	child := create(t, e, CreateRequest{Placement: Placement{ParentID: m.ID}, Nodes: []Draft{{Input: "x"}}}).Nodes[0]
	v, _ := e.GetNode(ctx, child.ID)
	if v.Path[len(v.Path)-1].Name != "meeting with Alice about Globex 10h" {
		t.Fatalf("path name = %q", v.Path[len(v.Path)-1].Name)
	}
}

// Any node can be referenced: completion finds meetings and tasks by their
// words, and references carry a label keeping inner types.
func TestCompletionFindsAnyNode(t *testing.T) {
	e := newEngine(t)
	write(t, e, "Northwind #project", "Bruno #person")
	m := write(t, e, "#meeting with [[Bruno]] about [[Northwind]]")[0]
	c, err := e.Resolve(ctx, "bruno northwind", 10)
	if err != nil || len(c) != 1 || c[0].ID != m.ID {
		t.Fatalf("candidates = %+v, %v", c, err)
	}
	if c[0].Label != "#meeting with Bruno about Northwind" || c[0].Name != "meeting with Bruno about Northwind" {
		t.Fatalf("label = %q, name = %q", c[0].Label, c[0].Name)
	}
	// Named nodes still come first.
	if c, _ := e.Resolve(ctx, "northwind", 10); c[0].Name != "Northwind" || c[0].Label != "Northwind" || len(c) != 2 {
		t.Fatalf("northwind candidates = %+v", c)
	}
	ref := write(t, e, "see [["+m.ID+"]]")[0]
	page, _ := e.Search(ctx, Query{Mentions: m.ID})
	if r := page.Refs[m.ID]; len(page.Nodes) != 1 || page.Nodes[0].ID != ref.ID || r.Label != "#meeting with Bruno about Northwind" {
		t.Fatalf("ref = %+v", r)
	}
}

// Regression: an AI rewriting a task's sentence without "#task" turned it
// into a plain note and lost its status and due date.
func TestKeepTypesOnRewrite(t *testing.T) {
	e := newEngine(t)
	n := create(t, e, CreateRequest{Nodes: []Draft{{Input: "Send the estimate #task", DateText: "Friday"}}}).Nodes[0]
	in := "Send the revised estimate"
	up, err := e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: n.ID, ExpectedRevision: 1, Input: &in, KeepTypes: true})
	if err != nil {
		t.Fatal(err)
	}
	got := up.Nodes[0]
	if got.Text != "Send the revised estimate #task" || got.Date != "2026-11-06" || got.Status != StatusOpen {
		t.Fatalf("rewritten task = %+v", got)
	}
	// Written types replace the old ones.
	in = "Send the revised estimate #decision"
	up, _ = e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: n.ID, ExpectedRevision: 2, Input: &in, KeepTypes: true})
	if !slices.Equal(up.Nodes[0].Types, []string{"decision"}) {
		t.Fatalf("types = %v", up.Nodes[0].Types)
	}
	// Without KeepTypes, the interface's rule: the text is the truth.
	in = "plain"
	up, _ = e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: n.ID, ExpectedRevision: 3, Input: &in})
	if len(up.Nodes[0].Types) != 0 {
		t.Fatalf("types = %v", up.Nodes[0].Types)
	}
}

func TestSubProjectsAndRenames(t *testing.T) {
	e := newEngine(t)
	northwind := write(t, e, "Northwind #project")[0]
	res := create(t, e, CreateRequest{Nodes: []Draft{{Input: "Prepare the [[Northwind/Lot 2]] kickoff #task"}}, Create: []Creation{{Name: "Northwind/Lot 2", Types: []string{"project"}}}})
	lot := res.Created[0]
	v, _ := e.GetNode(ctx, lot.ID)
	if v.Node.ParentID != northwind.ID || v.Node.Text != "Lot 2 #project" {
		t.Fatalf("Lot 2 = %+v", v.Node)
	}
	tasks := search(t, e, Query{Concerns: []string{northwind.ID}, Types: []string{"task"}})
	if len(tasks) != 1 {
		t.Fatalf("sub-project task does not concern its parent project")
	}
	// The nested name resolves without a new creation.
	if _, err := e.Create(ctx, me, CreateRequest{RequestID: rid(), Nodes: []Draft{{Input: "see [[Northwind/Lot 2]]"}}}); err != nil {
		t.Fatal(err)
	}
	if projects := search(t, e, Query{Types: []string{"project"}}); len(projects) != 2 {
		t.Fatalf("projects = %v", ids(projects))
	}
	var path []string
	for _, p := range v.Path {
		path = append(path, p.Name)
	}
	if !slices.Equal(path, []string{"Journal", "2026-11-05", "Northwind"}) {
		t.Fatalf("path = %v", path)
	}
	// Renaming keeps references and refreshes search.
	n, _ := e.GetNode(ctx, northwind.ID)
	in := "Northwind Traders #project"
	if _, err := e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: northwind.ID, ExpectedRevision: n.Node.Revision, Input: &in}); err != nil {
		t.Fatal(err)
	}
	if got := search(t, e, Query{Text: "traders"}); len(got) != 1 {
		t.Fatalf("rename not searchable: %v", ids(got))
	}
	sub, _ := e.GetNode(ctx, lot.ID)
	if sub.Path[len(sub.Path)-1].Name != "Northwind Traders" {
		t.Fatalf("path after rename = %+v", sub.Path)
	}
}

func TestRevisionsIdempotencyAndUndo(t *testing.T) {
	e := newEngine(t)
	n := write(t, e, "draft")[0]
	in := "final"
	req := UpdateRequest{RequestID: "same", ID: n.ID, ExpectedRevision: 1, Input: &in}
	first, err := e.Update(ctx, me, req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := e.Update(ctx, me, req)
	if err != nil || replay.Nodes[0].Revision != first.Nodes[0].Revision {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	other := "other"
	req.Input = &other
	if _, err := e.Update(ctx, me, req); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed payload accepted: %v", err)
	}
	if _, err := e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: n.ID, ExpectedRevision: 1, Input: &other}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	undone, err := e.Undo(ctx, Actor{Name: "owner", Source: "web"}, UndoRequest{RequestID: rid(), ID: n.ID, ExpectedRevision: 2})
	if err != nil || undone.Nodes[0].Text != "draft" || undone.Nodes[0].Revision != 3 {
		t.Fatalf("undo = %+v, %v", undone, err)
	}
	act, err := e.Activity(ctx, "", "", 0)
	if err != nil || len(act.Items) != 3 || act.Items[0].Action != "undo" || !act.Items[0].Latest {
		t.Fatalf("activity = %+v, %v", act, err)
	}
	// Undoing a creation trashes the node.
	if _, err := e.Undo(ctx, me, UndoRequest{RequestID: rid(), ID: write(t, e, "oops")[0].ID, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if got := search(t, e, Query{Text: "oops"}); len(got) != 0 {
		t.Fatal("undone creation still visible")
	}
}

func TestMovesTrashAndOrder(t *testing.T) {
	e := newEngine(t)
	nodes := write(t, e, "a", "b", "c")
	a, b, c := nodes[0], nodes[1], nodes[2]
	// Insert many nodes between a and b to force renumbering.
	after := a.ID
	for i := range 20 {
		n := create(t, e, CreateRequest{Placement: Placement{AfterID: after}, Nodes: []Draft{{Input: fmt.Sprint("x", i)}}}).Nodes[0]
		_ = n
	}
	day, _ := e.Day(ctx, "", "", 0)
	got := []string{}
	for _, n := range day.Children.Nodes {
		got = append(got, n.Text)
	}
	if got[0] != "a" || got[1] != "x19" || got[20] != "x0" || got[21] != "b" || got[22] != "c" {
		t.Fatalf("order = %v", got)
	}
	if _, err := e.Move(ctx, me, MoveRequest{RequestID: rid(), ID: a.ID, ExpectedRevision: 1, Placement: Placement{ParentID: c.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Move(ctx, me, MoveRequest{RequestID: rid(), ID: c.ID, ExpectedRevision: 1, Placement: Placement{ParentID: a.ID}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cycle accepted: %v", err)
	}
	if _, err := e.Move(ctx, me, MoveRequest{RequestID: rid(), ID: b.ID, ExpectedRevision: 1, Placement: Placement{First: true}}); err != nil {
		t.Fatal(err)
	}
	day, _ = e.Day(ctx, "", "", 0)
	if day.Children.Nodes[0].ID != b.ID {
		t.Fatal("first placement ignored")
	}
	if _, err := e.Trash(ctx, me, TrashRequest{RequestID: rid(), ID: c.ID, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if got := search(t, e, Query{Text: "a"}); len(got) != 0 {
		t.Fatalf("trashed subtree visible: %v", ids(got))
	}
	if _, err := e.Restore(ctx, me, TrashRequest{RequestID: rid(), ID: a.ID, ExpectedRevision: 2}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("restoring a non-root accepted: %v", err)
	}
	if _, err := e.Restore(ctx, me, TrashRequest{RequestID: rid(), ID: c.ID, ExpectedRevision: 2}); err != nil {
		t.Fatal(err)
	}
	if got := search(t, e, Query{Text: "a"}); len(got) != 1 {
		t.Fatalf("restored subtree = %v", ids(got))
	}
}

func TestSearchPagination(t *testing.T) {
	e := newEngine(t)
	for i := range 25 {
		write(t, e, fmt.Sprintf("note %d", i))
	}
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		p, err := e.Search(ctx, Query{Text: "note", Limit: 10, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, n := range p.Nodes {
			if seen[n.ID] {
				t.Fatalf("duplicate %s", n.ID)
			}
			seen[n.ID] = true
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	if len(seen) != 25 || pages != 3 {
		t.Fatalf("seen %d in %d pages", len(seen), pages)
	}
}

func TestProtectedNodes(t *testing.T) {
	e := newEngine(t)
	day := write(t, e, "x")[0].ParentID
	in := "renamed"
	if _, err := e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: day, ExpectedRevision: 1, Input: &in}); !errors.Is(err, ErrProtected) {
		t.Fatalf("day edited: %v", err)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	e := newEngine(t)
	write(t, e, "Northwind #project")
	create(t, e, CreateRequest{Nodes: []Draft{{Input: "Call [[Northwind]] #task", DateText: "Friday", Children: []Draft{{Input: "child"}}}}})
	trashed := write(t, e, "gone")[0]
	if _, err := e.Trash(ctx, me, TrashRequest{RequestID: rid(), ID: trashed.ID, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := e.Export(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	f := newEngine(t)
	if err := f.Import(ctx, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	var again bytes.Buffer
	if err := f.Export(ctx, &again); err != nil {
		t.Fatal(err)
	}
	strip := func(b []byte) []byte { return b[bytes.Index(b, []byte(`"types"`)):] }
	if !bytes.Equal(strip(buf.Bytes()), strip(again.Bytes())) {
		t.Fatal("export after import differs")
	}
	northwind := search(t, f, Query{Text: "northwind", Types: []string{"project"}})[0]
	if got := search(t, f, Query{Concerns: []string{northwind.ID}, Types: []string{"task"}}); len(got) != 1 || got[0].Date != "2026-11-06" {
		t.Fatalf("imported context = %+v", got)
	}
	if err := f.Import(ctx, bytes.NewReader(buf.Bytes())); !errors.Is(err, ErrConflict) {
		t.Fatalf("import into a non-empty workspace: %v", err)
	}
	// A broken export is rejected without writing anything.
	g := newEngine(t)
	broken := bytes.Replace(buf.Bytes(), []byte(`"kind": "day"`), []byte(`"kind": "page"`), 1)
	if err := g.Import(ctx, bytes.NewReader(broken)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("broken import: %v", err)
	}
	if roots, _ := g.Roots(ctx, "", 0); len(roots.Nodes) != 0 {
		t.Fatal("broken import wrote nodes")
	}
	write(t, g, "still usable")
}

func TestBackupIsAConsistentSnapshot(t *testing.T) {
	dir := t.TempDir()
	e := open(t, filepath.Join(dir, "vrac.sqlite"))
	n := write(t, e, "keep me")[0]
	backup := filepath.Join(dir, "backup.sqlite")
	if err := e.Backup(backup); err != nil {
		t.Fatal(err)
	}
	b := open(t, backup)
	if _, err := b.GetNode(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
}

// Migration 2 writes the tokens of types that version 1 kept out of the text,
// after a snapshot backup.
func TestMigrationWritesTypeTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vrac.sqlite")
	e := open(t, path)
	n := write(t, e, "#meeting with Bruno", "Northwind #project")
	task := write(t, e, "Call Bruno #task")[0]
	// Recreate the version 1 representation: types outside the text, a
	// task's due date in its own column, the task type built in.
	for _, stmt := range []string{
		`UPDATE nodes SET text = 'with Bruno' WHERE id = '` + n[0].ID + `'`,
		`UPDATE nodes SET text = 'Northwind' WHERE id = '` + n[1].ID + `'`,
		`UPDATE nodes SET due = '2026-11-20', start_at = NULL WHERE id = '` + task.ID + `'`,
		`UPDATE types SET builtin = 1, todo = 0 WHERE name = 'task'`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := e.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	e.Close()
	e = open(t, path)
	for id, want := range map[string]string{n[0].ID: "with Bruno #meeting", n[1].ID: "Northwind #project"} {
		v, err := e.GetNode(ctx, id)
		if err != nil || v.Node.Text != want {
			t.Fatalf("migrated text = %q, %v", v.Node.Text, err)
		}
	}
	if c, _ := e.Resolve(ctx, "northwind", 1); len(c) != 1 || c[0].Name != "Northwind" {
		t.Fatalf("resolve after migration = %+v", c)
	}
	// Migration 7: the due date became the node's date, task is a to-do
	// tag of the user's, unused types are gone.
	if v, _ := e.GetNode(ctx, task.ID); v.Node.Date != "2026-11-20" {
		t.Fatalf("migrated date = %+v", v.Node)
	}
	// (open recreates the test tags afterwards, with new IDs, so the
	// survivors come first.)
	types, _ := e.Types(ctx)
	if types[0].Name != "task" || !types[0].Todo || types[1].Name != "meeting" || types[2].Name != "project" || types[3].Count != 0 {
		t.Fatalf("types after migration = %+v", types)
	}
	backups, _ := filepath.Glob(path + ".v1-*.backup")
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}
	if err := e.Check(); err != nil {
		t.Fatal(err)
	}
}

// Types given outside the text, such as a creation, are written in it.
func TestTypesAlwaysAppearInText(t *testing.T) {
	e := newEngine(t)
	res := create(t, e, CreateRequest{Nodes: []Draft{{Input: "Lunch with [[Dupont]] about #project/Northwind"}},
		Create: []Creation{{Name: "Dupont", Types: []string{"person"}}, {Name: "Northwind"}}})
	for _, b := range res.Created {
		v, _ := e.GetNode(ctx, b.ID)
		if v.Node.Text != b.Name+" #"+b.Types[0] {
			t.Fatalf("created text = %q", v.Node.Text)
		}
	}
	// Removing the token removes the type.
	v, _ := e.GetNode(ctx, res.Created[0].ID)
	in := "Dupont"
	up, err := e.Update(ctx, me, UpdateRequest{RequestID: rid(), ID: v.Node.ID, ExpectedRevision: v.Node.Revision, Input: &in})
	if err != nil || len(up.Nodes[0].Types) != 0 {
		t.Fatalf("update = %+v, %v", up.Nodes, err)
	}
}

func TestPreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vrac.sqlite")
	e := open(t, path)
	if p, err := e.Preferences(ctx); err != nil || p != DefaultPreferences {
		t.Fatalf("defaults = %+v, %v", p, err)
	}
	if _, err := e.SetPreferences(ctx, Preferences{Fold: "sideways", Theme: "dark"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid fold accepted: %v", err)
	}
	want := Preferences{Fold: "collapsed", Theme: "dark", Scale: "large"}
	if _, err := e.SetPreferences(ctx, want); err != nil {
		t.Fatal(err)
	}
	e.Close()
	e = open(t, path)
	if p, _ := e.Preferences(ctx); p != want {
		t.Fatalf("reopened preferences = %+v", p)
	}
}

func TestOwnerAndPasskeys(t *testing.T) {
	e := newEngine(t)
	o := Owner{Username: "yan", PasswordHash: "h1", UserHandle: NewUserHandle()}
	first := &Passkey{ID: "cred-1", Name: "MacBook", Credential: json.RawMessage(`{"id":"1"}`)}
	if err := e.CreateOwner(ctx, o, first); err != nil {
		t.Fatal(err)
	}
	if err := e.CreateOwner(ctx, o, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("second owner: %v", err)
	}
	got, _ := e.Owner(ctx)
	if got.Username != "yan" || len(got.UserHandle) != 32 {
		t.Fatalf("owner = %+v", got)
	}
	if err := e.DeletePasskey(ctx, "cred-1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("deleted the last passkey: %v", err)
	}
	if err := e.AddPasskey(ctx, Passkey{ID: "cred-2", Credential: json.RawMessage(`{"id":"2"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := e.UsePasskey(ctx, "cred-2", json.RawMessage(`{"id":"2","count":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := e.DeletePasskey(ctx, "cred-1"); err != nil {
		t.Fatal(err)
	}
	list, _ := e.Passkeys(ctx)
	if len(list) != 1 || list[0].Name != "Passkey" || string(list[0].Credential) != `{"id":"2","count":1}` || list[0].LastUsedAt == "" {
		t.Fatalf("passkeys = %+v", list)
	}
	if n, err := e.ResetPasskeys(ctx); err != nil || n != 1 {
		t.Fatalf("reset = %d, %v", n, err)
	}
	if err := e.SetOwnerPassword(ctx, "", "h2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.Owner(ctx); got.Username != "yan" || got.PasswordHash != "h2" {
		t.Fatalf("after password change: %+v", got)
	}
}

// Migration 6 keeps an existing password-only owner, named "owner".
func TestMigrationKeepsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vrac.sqlite")
	e := open(t, path)
	for _, stmt := range []string{
		`DROP TABLE passkeys`,
		`CREATE TABLE owner_v5 (singleton INTEGER PRIMARY KEY CHECK (singleton = 1), password_hash TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`INSERT INTO owner_v5 VALUES (1, 'old-hash', '2026-11-01')`,
		`DROP TABLE owner`,
		`ALTER TABLE owner_v5 RENAME TO owner`,
		`PRAGMA user_version = 5`,
	} {
		if _, err := e.db.Exec(stmt); err != nil {
			t.Fatal(stmt, err)
		}
	}
	e.Close()
	e = open(t, path)
	o, err := e.Owner(ctx)
	if err != nil || o.Username != "owner" || o.PasswordHash != "old-hash" || len(o.UserHandle) != 32 {
		t.Fatalf("migrated owner = %+v, %v", o, err)
	}
	if p, _ := e.Passkeys(ctx); len(p) != 0 {
		t.Fatalf("passkeys = %v", p)
	}
}
