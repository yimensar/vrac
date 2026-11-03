package engine_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/yan-imensar/vrac/internal/engine"
)

func TestCreateOutlineOrderingRetryAndReopen(t *testing.T) {
	e, path := open(t)
	parent, err := e.Create(engine.CreateInput{Text: "Meeting"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := e.Create(engine.CreateInput{ParentID: parent.ID, Text: "Before"})
	if err != nil {
		t.Fatal(err)
	}
	after, err := e.Create(engine.CreateInput{ParentID: parent.ID, Text: "After"})
	if err != nil {
		t.Fatal(err)
	}
	input := engine.OutlineInput{ParentID: parent.ID, Placement: engine.Placement{After: before.ID}, RequestID: "outline-retry", Nodes: []engine.CreateInput{{Text: "A", Children: []engine.CreateInput{{Text: "Decision #decision"}}}, {Text: "B"}}}
	roots, err := e.CreateOutline(input)
	if err != nil {
		t.Fatal(err)
	}
	page, err := e.Children(parent.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, node := range page.Nodes {
		got = append(got, node.ID)
	}
	if !reflect.DeepEqual(got, []string{before.ID, roots[0].ID, roots[1].ID, after.ID}) {
		t.Fatal(got)
	}
	children, err := e.Children(roots[0].ID, "", 100)
	if err != nil || len(children.Nodes) != 1 || !reflect.DeepEqual(children.Nodes[0].Tags, []string{"decision"}) {
		t.Fatal(children, err)
	}
	check(t, e)
	e.Close()
	reopened, err := engine.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	retry, err := reopened.CreateOutline(input)
	if err != nil || !reflect.DeepEqual(retry, roots) {
		t.Fatal(retry, err)
	}
	input.Nodes[0].Text = "Different payload"
	if _, err = reopened.CreateOutline(input); !errors.Is(err, engine.ErrConflict) {
		t.Fatal(err)
	}
	check(t, reopened)
}

func TestCreateOutlineRollbackAndValidation(t *testing.T) {
	e, _ := open(t)
	parent, err := e.Create(engine.CreateInput{Text: "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	count := check(t, e).NodeCount
	_, err = e.CreateOutline(engine.OutlineInput{ParentID: parent.ID, RequestID: "rollback", Nodes: []engine.CreateInput{{Text: "Before [[Rolled-back subject]] #temporary"}, {Text: "Invalid", Children: []engine.CreateInput{{Text: ""}}}}})
	if !errors.Is(err, engine.ErrInvalid) {
		t.Fatal(err)
	}
	if check(t, e).NodeCount != count {
		t.Fatal("failed outline left partial canonical nodes")
	}
	if len(search(t, e, engine.SearchFilters{Query: "Rolled-back subject"}).Items) != 0 {
		t.Fatal("failed outline left a subject")
	}
	for _, in := range []engine.OutlineInput{{RequestID: "empty"}, {Nodes: []engine.CreateInput{{Text: "A"}}}, {RequestID: "root-placement", Nodes: []engine.CreateInput{{Text: "A", Placement: engine.Placement{First: true}}}}} {
		if _, err = e.CreateOutline(in); !errors.Is(err, engine.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err = e.CreateOutline(engine.OutlineInput{ParentID: "missing", RequestID: "missing-parent", Nodes: []engine.CreateInput{{Text: "A"}}}); !errors.Is(err, engine.ErrNotFound) {
		t.Fatal(err)
	}
}
