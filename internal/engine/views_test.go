package engine_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/yan-imensar/vrac/internal/engine"
)

func TestReadViewsPreserveOrderContextAndPageBounds(t *testing.T) {
	e, _ := open(t)
	nodes := capture(t, e, "2026-11-02", engine.CreateInput{Text: "Meeting [[Atlas]] #meeting", Children: []engine.CreateInput{{Text: "Decision #decision"}, {Text: "Task #task"}}}, engine.CreateInput{Text: "Another note"})
	views, err := e.Read([]string{nodes[1].ID, nodes[0].ID, nodes[1].ID}, true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 3 || views[0].Node.ID != nodes[1].ID || views[1].Node.ID != nodes[0].ID || views[2].Node.ID != nodes[1].ID {
		t.Fatalf("read order: %+v", views)
	}
	for _, view := range views {
		if len(view.Path) == 0 || view.Path[len(view.Path)-1].ID != view.Node.ID || view.Node.JournalDate != "2026-11-02" || view.Children == nil {
			t.Fatalf("incomplete view: %+v", view)
		}
	}
	if len(views[1].Children.Nodes) != 1 || views[1].Children.NextCursor == "" {
		t.Fatalf("unbounded child page: %+v", views[1].Children)
	}
	without, err := e.Read([]string{nodes[0].ID}, false, 1)
	if err != nil || without[0].Children != nil {
		t.Fatal(without, err)
	}
	if partial, err := e.Read([]string{nodes[0].ID, "missing"}, true, 1); !errors.Is(err, engine.ErrNotFound) || partial != nil {
		t.Fatalf("missing read returned partial success: %+v %v", partial, err)
	}
	for _, ids := range [][]string{nil, {""}, make([]string, 11)} {
		if _, err := e.Read(ids, false, 1); !errors.Is(err, engine.ErrInvalid) {
			t.Fatalf("invalid IDs %v: %v", ids, err)
		}
	}
	if _, err := e.Read([]string{nodes[0].ID}, true, 101); !errors.Is(err, engine.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestReadViewsShareOneSnapshotWithConcurrentWriter(t *testing.T) {
	e, path := open(t)
	nodes := capture(t, e, "2026-11-02", engine.CreateInput{Text: "state-1", Tags: []string{"state-1"}}, engine.CreateInput{Text: "Changing children"})
	writer, err := engine.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	done := make(chan error, 1)
	go func() {
		current := nodes[0]
		for i := 2; i <= 60; i++ {
			text := fmt.Sprintf("state-%d", i)
			updated, err := writer.Update(current.ID, engine.UpdateInput{Text: text, Tags: []string{text}, ExpectedRevision: current.Revision, JournalDate: "2026-11-02"})
			if err != nil {
				done <- err
				return
			}
			current = updated
			child, err := writer.Create(engine.CreateInput{ParentID: nodes[1].ID, Text: "Temporary child"})
			if err == nil {
				err = writer.Delete(child.ID, child.Revision, "2026-11-02", "")
			}
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 100; i++ {
		views, err := e.Read([]string{nodes[0].ID, nodes[1].ID}, true, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, view := range views {
			last := view.Path[len(view.Path)-1]
			if !reflect.DeepEqual(view.Node, last) {
				t.Fatalf("node and path disagree: %+v / %+v", view.Node, last)
			}
			if view.Node.HasChildren != (len(view.Children.Nodes) > 0) {
				t.Fatalf("node and child page disagree: %+v / %+v", view.Node, view.Children)
			}
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	check(t, e)
}

func TestReadRevisionPreservesRecordedStateAndArchivedHistory(t *testing.T) {
	e, _ := open(t)
	n := capture(t, e, "2026-11-02", engine.CreateInput{Text: "Use [[Atlas]] #decision"})[0]
	target := get(t, e, n.References[0].TargetID)
	if _, err := e.Update(target.ID, engine.UpdateInput{Text: "Renamed Atlas", ExpectedRevision: target.Revision, JournalDate: "2026-11-02"}); err != nil {
		t.Fatal(err)
	}
	updated, err := e.Update(n.ID, engine.UpdateInput{Text: "Updated decision", ExpectedRevision: n.Revision, JournalDate: "2026-11-02"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Delete(updated.ID, updated.Revision, "2026-11-02", ""); err != nil {
		t.Fatal(err)
	}
	original, err := e.ReadRevision(n.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if original.Node.Text != n.Text || original.Node.References[0].Label != "Atlas" || original.Node.References[0].TargetID != target.ID || original.Revision != 1 {
		t.Fatal(original)
	}
	archived, err := e.ReadRevision(n.ID, 3)
	if err != nil || !archived.Node.Deleted {
		t.Fatal(archived, err)
	}
	if _, err := e.ReadRevision(n.ID, 999); !errors.Is(err, engine.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := e.ReadRevision(n.ID, 0); !errors.Is(err, engine.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestTagPagesEnumerateAllDistinctTagsAndBindCursorToPrefix(t *testing.T) {
	e, _ := open(t)
	tags := make([]string, 215)
	for i := range tags {
		tags[i] = fmt.Sprintf("topic_%03d", i)
	}
	inputs := []engine.CreateInput{}
	for start := 0; start < len(tags); start += 50 {
		end := start + 50
		if end > len(tags) {
			end = len(tags)
		}
		inputs = append(inputs, engine.CreateInput{Text: "Tagged note", Tags: tags[start:end]})
	}
	inputs = append(inputs, engine.CreateInput{Text: "Repeated tags", Tags: tags[:12]}, engine.CreateInput{Text: "Other tag", Tags: []string{"unrelated"}})
	capture(t, e, "2026-11-02", inputs...)
	first, err := e.TagsPage("#TOPIC_", "", 100)
	if err != nil || len(first.Tags) != 100 || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	if _, err := e.TagsPage("unrelated", first.NextCursor, 100); !errors.Is(err, engine.ErrInvalid) {
		t.Fatal(err)
	}
	all := append([]string{}, first.Tags...)
	cursor := first.NextCursor
	for cursor != "" {
		page, err := e.TagsPage("topic_", cursor, 70)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, page.Tags...)
		cursor = page.NextCursor
	}
	if !reflect.DeepEqual(all, tags) {
		t.Fatalf("tags duplicated or omitted: got %v, want %v", all, tags)
	}
	empty, err := e.TagsPage("absent", "", 10)
	if err != nil || len(empty.Tags) != 0 || empty.NextCursor != "" {
		t.Fatal(empty, err)
	}
	if _, err := e.TagsPage("topic_", "invalid", 10); !errors.Is(err, engine.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := e.TagsPage("topic_", "", 101); !errors.Is(err, engine.ErrInvalid) {
		t.Fatal(err)
	}
}
