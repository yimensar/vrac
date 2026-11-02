package engine_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/yan-imensar/vrac/internal/engine"
)

func open(t *testing.T) (*engine.Engine, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "notes.sqlite")
	e, err := engine.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e, p
}
func capture(t *testing.T, e *engine.Engine, date string, inputs ...engine.CreateInput) []engine.Node {
	t.Helper()
	nodes, err := e.Capture(date, inputs, "")
	if err != nil {
		t.Fatal(err)
	}
	return nodes
}
func check(t *testing.T, e *engine.Engine) engine.CheckReport {
	t.Helper()
	report, err := e.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) > 0 {
		t.Fatal(report.Issues)
	}
	return report
}
func get(t *testing.T, e *engine.Engine, id string) engine.Node {
	t.Helper()
	n, err := e.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func search(t *testing.T, e *engine.Engine, f engine.SearchFilters) engine.SearchPage {
	t.Helper()
	r, err := e.Search(f)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestJournalPersistenceAndOrdering(t *testing.T) {
	e, p := open(t)
	day, err := e.EnsureDay("2026-11-02")
	if err != nil {
		t.Fatal(err)
	}
	same, err := e.EnsureDay("2026-11-02")
	if err != nil || same.ID != day.ID {
		t.Fatalf("day identity: %+v %v", same, err)
	}
	for _, date := range []string{"2026-11-01", "2026-11-03", "2026-10-01"} {
		if _, err = e.EnsureDay(date); err != nil {
			t.Fatal(err)
		}
	}
	journal, err := e.Journal()
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.Children(journal.ID, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Children(journal.ID, first.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.Nodes[0].Text != "2026-11-03" || first.Nodes[1].Text != "2026-11-02" || second.Nodes[0].Text != "2026-11-01" || second.Nodes[1].Text != "2026-10-01" {
		t.Fatal(first, second)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := engine.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if get(t, reopened, day.ID).JournalDate != "2026-11-02" {
		t.Fatal("day changed on reopen")
	}
	check(t, reopened)
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("database mode: %o", info.Mode().Perm())
	}
}
func TestCaptureAtomicityAndValidation(t *testing.T) {
	e, _ := open(t)
	before := check(t, e).NodeCount
	_, err := e.Capture("2026-11-02", []engine.CreateInput{{Text: "Meeting [[New subject]]", Children: []engine.CreateInput{{Text: "Good"}, {Text: "Invalid", ReferenceIDs: []string{"missing"}}}}}, "bad")
	if !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if after := check(t, e).NodeCount; after != before {
		t.Fatalf("partial capture: %d -> %d", before, after)
	}
	if _, err = e.Create(engine.CreateInput{ParentID: "missing", Text: "No"}); !errors.Is(err, engine.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = e.Capture("2026-02-30", []engine.CreateInput{{Text: "No"}}, ""); !errors.Is(err, engine.ErrInvalid) {
		t.Fatal(err)
	}
}
func TestSiblingPaginationAndMoves(t *testing.T) {
	e, _ := open(t)
	parent := capture(t, e, "2026-11-02", engine.CreateInput{Text: "Parent"})[0]
	ids := []string{}
	for _, label := range []string{"a", "b", "c", "d", "e"} {
		n, err := e.Create(engine.CreateInput{ParentID: parent.ID, Text: label})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	seen := []string{}
	cursor := ""
	for {
		p, err := e.Children(parent.ID, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range p.Nodes {
			seen = append(seen, n.ID)
		}
		cursor = p.NextCursor
		if cursor == "" {
			break
		}
	}
	if !reflect.DeepEqual(seen, ids) {
		t.Fatal(seen, ids)
	}
	n := get(t, e, ids[4])
	n, err := e.Move(n.ID, engine.MoveInput{ParentID: parent.ID, Placement: engine.Placement{Before: ids[1]}, ExpectedRevision: n.Revision, JournalDate: "2026-11-02"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.Children(parent.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, n := range p.Nodes {
		got = append(got, n.ID)
	}
	want := []string{ids[0], ids[4], ids[1], ids[2], ids[3]}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	if _, err = e.Move(parent.ID, engine.MoveInput{ParentID: n.ID, ExpectedRevision: parent.Revision}); !errors.Is(err, engine.ErrInvalid) {
		t.Fatalf("cycle: %v", err)
	}
	current := get(t, e, parent.ID)
	if current.ParentID != parent.ParentID || current.Revision != parent.Revision {
		t.Fatal("failed move changed parent")
	}
	if _, err = e.Move(n.ID, engine.MoveInput{ParentID: parent.ID, Placement: engine.Placement{Before: "missing"}, ExpectedRevision: n.Revision}); !errors.Is(err, engine.ErrNotFound) {
		t.Fatal(err)
	}
	check(t, e)
}
func TestTagsReferencesContextAndDates(t *testing.T) {
	e, _ := open(t)
	meeting := capture(t, e, "2026-10-28", engine.CreateInput{Text: "Planning [[Atlas]] #Meeting", Children: []engine.CreateInput{{Text: "Use SSO #Decision"}, {Text: "Accesses #task"}}})[0]
	other := capture(t, e, "2026-11-02", engine.CreateInput{Text: "Other [[Orion]] #decision"})[0]
	children, err := e.Children(meeting.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	decision := children.Nodes[0]
	project := meeting.References[0].TargetID
	if meeting.Text != "Planning [[Atlas]]" || !reflect.DeepEqual(meeting.Tags, []string{"meeting"}) {
		t.Fatal(meeting)
	}
	r := search(t, e, engine.SearchFilters{Tag: "decision", SubjectID: project})
	if len(r.Items) != 1 || r.Items[0].Node.ID != decision.ID || r.Items[0].Node.JournalDate != "2026-10-28" {
		t.Fatal(r)
	}
	r = search(t, e, engine.SearchFilters{Tag: "decision", From: "2026-11-02", To: "2026-11-02"})
	if len(r.Items) != 1 || r.Items[0].Node.ID != other.ID {
		t.Fatal(r)
	}
	r = search(t, e, engine.SearchFilters{Tag: "decision", SubjectID: project, From: "2026-11-02"})
	if len(r.Items) != 0 {
		t.Fatal(r)
	}
	r = search(t, e, engine.SearchFilters{Query: "sso"})
	if len(r.Items) != 1 || r.Items[0].Node.ID != decision.ID {
		t.Fatal(r)
	}
	updated, err := e.Update(decision.ID, engine.UpdateInput{Text: "Use SSO for external accounts", ExpectedRevision: decision.Revision, JournalDate: "2026-11-02", Summary: "Clarify scope"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != decision.ID || updated.Revision != 2 {
		t.Fatal(updated)
	}
	r = search(t, e, engine.SearchFilters{Tag: "decision", SubjectID: project})
	if len(r.Items) != 1 || r.Items[0].Node.ID != decision.ID {
		t.Fatal("duplicate current decisions", r)
	}
	r = search(t, e, engine.SearchFilters{Tag: "decision", SubjectID: project, ActivityFrom: "2026-11-02", ActivityTo: "2026-11-02"})
	if len(r.Items) != 1 || r.Items[0].Node.ID != decision.ID {
		t.Fatal(r)
	}
	r = search(t, e, engine.SearchFilters{Tag: "activity", SubjectID: project, From: "2026-11-02"})
	if len(r.Items) != 1 {
		t.Fatal("activity lost project context", r)
	}
	if _, err = e.Update(decision.ID, engine.UpdateInput{Text: "Overwrite", ExpectedRevision: 1, JournalDate: "2026-11-02"}); !errors.Is(err, engine.ErrConflict) {
		t.Fatal(err)
	}
	check(t, e)
}
func TestStableReferencesAfterRenameAndRestore(t *testing.T) {
	e, _ := open(t)
	n := capture(t, e, "2026-11-02", engine.CreateInput{Text: "Use [[Atlas]] #decision"})[0]
	originalTarget := n.References[0].TargetID
	target := get(t, e, originalTarget)
	if _, err := e.Update(target.ID, engine.UpdateInput{Text: "Atlas renamed", ExpectedRevision: target.Revision, JournalDate: "2026-11-02"}); err != nil {
		t.Fatal(err)
	}
	updated, err := e.Update(n.ID, engine.UpdateInput{Text: "Still use [[Atlas]]", ExpectedRevision: n.Revision, JournalDate: "2026-11-02"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.References) != 1 || updated.References[0].TargetID != originalTarget || updated.References[0].Label != "Atlas renamed" {
		t.Fatal(updated)
	}
	updated, err = e.Update(n.ID, engine.UpdateInput{Text: "Removed mention", ExpectedRevision: updated.Revision, JournalDate: "2026-11-02"})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := e.Restore(n.ID, 1, updated.Revision, "2026-11-02", "restore-1")
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID != n.ID || restored.Revision != 4 || len(restored.References) != 1 || restored.References[0].TargetID != originalTarget {
		t.Fatal(restored)
	}
	revs, err := e.Revisions(n.ID, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	older, err := e.Revisions(n.ID, revs.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs.Revisions) != 2 || len(older.Revisions) != 2 || older.Revisions[1].Node.References[0].Label != "Atlas" {
		t.Fatal(revs, older)
	}
	check(t, e)
}
func TestIdempotencyAndConcurrentUpdates(t *testing.T) {
	e, _ := open(t)
	inputs := []engine.CreateInput{{Text: "A note"}}
	a, err := e.Capture("2026-11-02", inputs, "capture-1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Capture("2026-11-02", inputs, "capture-1")
	if err != nil || a[0].ID != b[0].ID {
		t.Fatal(a, b, err)
	}
	if _, err = e.Capture("2026-11-02", []engine.CreateInput{{Text: "Changed"}}, "capture-1"); !errors.Is(err, engine.ErrConflict) {
		t.Fatal(err)
	}
	input := engine.UpdateInput{Text: "Updated", ExpectedRevision: 1, JournalDate: "2026-11-02", RequestID: "edit-1"}
	n, err := e.Update(a[0].ID, input)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.Update(a[0].ID, input)
	if err != nil || again.Revision != n.Revision {
		t.Fatal(again, err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, text := range []string{"one", "two"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			_, err := e.Update(n.ID, engine.UpdateInput{Text: text, ExpectedRevision: n.Revision, JournalDate: "2026-11-02"})
			results <- err
		}(text)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, engine.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	check(t, e)
}
func TestBackupExportImportRecovery(t *testing.T) {
	e, _ := open(t)
	n := capture(t, e, "2026-11-02", engine.CreateInput{Text: "Meeting [[Atlas]] #meeting", Children: []engine.CreateInput{{Text: "Decision #decision"}}})[0]
	if _, err := e.Update(n.ID, engine.UpdateInput{Text: "Meeting notes [[Atlas]]", ExpectedRevision: n.Revision, JournalDate: "2026-11-03"}); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.sqlite")
	if err := e.Backup(backup); err != nil {
		t.Fatal(err)
	}
	copy, err := engine.Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	check(t, copy)
	if get(t, copy, n.ID).Text != "Meeting notes [[Atlas]]" {
		t.Fatal("backup lost update")
	}
	if err = e.Backup(backup); !errors.Is(err, engine.ErrConflict) {
		t.Fatal(err)
	}
	var exported bytes.Buffer
	if err = e.Export(&exported); err != nil {
		t.Fatal(err)
	}
	restored, _ := open(t)
	if err = restored.Import(bytes.NewReader(exported.Bytes())); err != nil {
		t.Fatal(err)
	}
	check(t, restored)
	var actual bytes.Buffer
	if err = restored.Export(&actual); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(exported.Bytes(), actual.Bytes()) {
		t.Fatal("export roundtrip changed canonical data")
	}
	if err = restored.Import(bytes.NewReader(exported.Bytes())); !errors.Is(err, engine.ErrConflict) {
		t.Fatal(err)
	}
	empty, _ := open(t)
	before := check(t, empty).NodeCount
	broken := bytes.Replace(exported.Bytes(), []byte(`"format":"vrac-hosted"`), []byte(`"format":"invalid"`), 1)
	if err = empty.Import(bytes.NewReader(broken)); !errors.Is(err, engine.ErrInvalid) {
		t.Fatal(err)
	}
	if check(t, empty).NodeCount != before {
		t.Fatal("failed import left partial data")
	}
	if err = e.RebuildSearch(); err != nil {
		t.Fatal(err)
	}
	check(t, e)
}
func TestArchiveAndRestoreLeaf(t *testing.T) {
	e, _ := open(t)
	n := capture(t, e, "2026-11-02", engine.CreateInput{Text: "Temporary"})[0]
	if err := e.Delete(n.ID, n.Revision, "2026-11-02", "delete-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Get(n.ID); !errors.Is(err, engine.ErrNotFound) {
		t.Fatal(err)
	}
	revisions, err := e.Revisions(n.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := e.Restore(n.ID, 1, revisions.Revisions[0].Revision, "2026-11-02", "")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Text != n.Text || restored.ID != n.ID {
		t.Fatal(restored)
	}
	check(t, e)
}
func TestCursorScopesAndLiteralReferenceLabels(t *testing.T) {
	e, _ := open(t)
	a := capture(t, e, "2026-11-02", engine.CreateInput{Text: "Parent", Children: []engine.CreateInput{{Text: "One"}, {Text: "Two"}}})[0]
	p, err := e.Children(a.ID, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Children("", p.NextCursor, 1); !errors.Is(err, engine.ErrInvalid) {
		t.Fatal(err)
	}
	nodes := capture(t, e, "2026-11-02", engine.CreateInput{Text: "[[Subject #special]]"}, engine.CreateInput{Text: "[[Subject #special]]"})
	if nodes[0].References[0].TargetID != nodes[1].References[0].TargetID || nodes[0].References[0].Label != "Subject #special" {
		t.Fatal(nodes)
	}
}

func TestConcurrentReadsAreConsistentSnapshots(t *testing.T) {
	e, p := open(t)
	n := capture(t, e, "2026-11-02", engine.CreateInput{Text: "state-1", Tags: []string{"state-1"}})[0]
	reader, err := engine.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	done := make(chan error, 1)
	go func() {
		current := n
		for i := 2; i <= 80; i++ {
			text := fmt.Sprintf("state-%d", i)
			next, err := e.Update(n.ID, engine.UpdateInput{Text: text, Tags: []string{text}, ExpectedRevision: current.Revision, JournalDate: "2026-11-02"})
			if err != nil {
				done <- err
				return
			}
			current = next
		}
		done <- nil
	}()
	for i := 0; i < 80; i++ {
		got, err := reader.Get(n.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Tags) != 1 || got.Tags[0] != got.Text {
			t.Fatalf("mixed read: %+v", got)
		}
		page, err := reader.Search(engine.SearchFilters{Query: "state", Limit: 5})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if item.Node.ID == n.ID {
				if item.Node.Tags[0] != item.Node.Text || item.Node.Revision != item.Path[len(item.Path)-1].Revision {
					t.Fatalf("mixed search snapshot: %+v", item)
				}
			}
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	check(t, e)
}

func TestIdempotencyAcrossFallbackDateChanges(t *testing.T) {
	e, _ := open(t)
	inputs := []engine.CreateInput{{Text: "Captured before midnight"}}
	first, err := e.Capture("", inputs, "midnight-capture", "2026-11-02")
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.Capture("", inputs, "midnight-capture", "2026-11-03")
	if err != nil || again[0].ID != first[0].ID || again[0].JournalDate != "2026-11-02" {
		t.Fatal(first, again, err)
	}
	edit := engine.UpdateInput{Text: "Edited before midnight", ExpectedRevision: first[0].Revision, RequestID: "midnight-edit"}
	changed, err := e.Update(first[0].ID, edit, "2026-11-02")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := e.Update(first[0].ID, edit, "2026-11-03")
	if err != nil || replay.Revision != changed.Revision {
		t.Fatal(replay, err)
	}
	edit.JournalDate = "2026-11-03"
	if _, err = e.Update(first[0].ID, edit, "2026-11-03"); !errors.Is(err, engine.ErrConflict) {
		t.Fatal(err)
	}
	activity := search(t, e, engine.SearchFilters{Tag: "activity", From: "2026-11-03", To: "2026-11-03"})
	if len(activity.Items) != 0 {
		t.Fatal("retry created another day's activity", activity)
	}
	check(t, e)
}

func TestBranchIndexMaintenanceAndSubjectDifferential(t *testing.T) {
	e, _ := open(t)
	project, err := e.Create(engine.CreateInput{Text: "Project"})
	if err != nil {
		t.Fatal(err)
	}
	roots := capture(t, e, "2026-11-02", engine.CreateInput{Text: "A", ReferenceIDs: []string{project.ID}}, engine.CreateInput{Text: "B"})
	ids := []string{project.ID, roots[0].ID, roots[1].ID}
	for i := 0; i < 60; i++ {
		parent := ids[1+(i*7)%(len(ids)-1)]
		in := engine.CreateInput{ParentID: parent, Text: fmt.Sprintf("node-%d", i), Tags: []string{"probe"}}
		if i%11 == 0 {
			in.ReferenceIDs = []string{project.ID}
		}
		n, err := e.Create(in)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	assertScope := func() {
		t.Helper()
		check(t, e)
		want := map[string]bool{}
		for _, id := range ids {
			node, err := e.Get(id)
			if errors.Is(err, engine.ErrNotFound) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			p, err := e.Path(id)
			if err != nil {
				t.Fatal(err)
			}
			for _, ancestor := range p {
				if ancestor.ID == project.ID {
					want[id] = true
				}
				for _, ref := range ancestor.References {
					if ref.TargetID == project.ID {
						want[id] = true
					}
				}
			}
			_ = node
		}
		got := map[string]bool{}
		cursor := ""
		for {
			r := search(t, e, engine.SearchFilters{SubjectID: project.ID, Limit: 7, Cursor: cursor})
			for _, item := range r.Items {
				for _, id := range ids {
					if item.Node.ID == id {
						got[id] = true
					}
				}
			}
			cursor = r.NextCursor
			if cursor == "" {
				break
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("subject scope differs from ancestor oracle: got=%v want=%v", got, want)
		}
	}
	assertScope()
	// Move a whole internal branch, preserving descendant rows and revisions.
	branch := get(t, e, ids[3])
	child, err := e.Create(engine.CreateInput{ParentID: branch.ID, Text: "Kept descendant", Tags: []string{"probe"}})
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, child.ID)
	destination := roots[1].ID
	if branch.ParentID == destination {
		destination = roots[0].ID
	}
	if _, err = e.Move(branch.ID, engine.MoveInput{ParentID: destination, ExpectedRevision: branch.Revision, JournalDate: "2026-11-02"}); err != nil {
		t.Fatal(err)
	}
	after := get(t, e, child.ID)
	if after.Revision != child.Revision || after.ParentID != child.ParentID {
		t.Fatal("subtree move rewrote a descendant")
	}
	assertScope()
	// First and last active child changes update both branch membership and scope.
	leafParent, err := e.Create(engine.CreateInput{ParentID: roots[0].ID, Text: "Last child parent"})
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, leafParent.ID)
	leaf, err := e.Create(engine.CreateInput{ParentID: leafParent.ID, Text: "Only child"})
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, leaf.ID)
	assertScope()
	if err = e.Delete(leaf.ID, leaf.Revision, "2026-11-02", ""); err != nil {
		t.Fatal(err)
	}
	assertScope()
	if _, err = e.Restore(leaf.ID, 1, 2, "2026-11-02", ""); err != nil {
		t.Fatal(err)
	}
	assertScope()
	if err = e.RebuildSearch(); err != nil {
		t.Fatal(err)
	}
	assertScope()
}

func TestImportRebuildsBranchesWithChildrenBeforeParents(t *testing.T) {
	e, _ := open(t)
	root := capture(t, e, "2026-11-02", engine.CreateInput{Text: "Meeting [[Project]]", Children: []engine.CreateInput{{Text: "Decision #decision", Children: []engine.CreateInput{{Text: "Deep"}}}}})[0]
	var original bytes.Buffer
	if err := e.Export(&original); err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(original.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(doc["nodes"], &rows); err != nil {
		t.Fatal(err)
	}
	depths := map[string]int{}
	for _, row := range rows {
		p, err := e.Path(row["id"].(string))
		if err != nil {
			t.Fatal(err)
		}
		depths[row["id"].(string)] = len(p)
	}
	sort.Slice(rows, func(i, j int) bool { return depths[rows[i]["id"].(string)] > depths[rows[j]["id"].(string)] })
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	doc["nodes"] = encoded
	payload, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	restored, _ := open(t)
	if err = restored.Import(bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	check(t, restored)
	r := search(t, restored, engine.SearchFilters{Tag: "decision", SubjectID: root.References[0].TargetID})
	if len(r.Items) != 1 {
		t.Fatal(r)
	}
}

func TestRejectForeignDatabaseWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE old_nodes(id TEXT PRIMARY KEY); PRAGMA application_id=1448231235; PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if e, err := engine.Open(path); !errors.Is(err, engine.ErrInvalid) {
		if e != nil {
			e.Close()
		}
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rejecting an old database changed its bytes")
	}
}

func TestActivityContextSurvivesArchiveAndLaterMoves(t *testing.T) {
	e, _ := open(t)
	a, err := e.Create(engine.CreateInput{Text: "Project A"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Create(engine.CreateInput{Text: "Project B"})
	if err != nil {
		t.Fatal(err)
	}
	meetings := capture(t, e, "2026-11-01", engine.CreateInput{Text: "Meeting A", ReferenceIDs: []string{a.ID}, Children: []engine.CreateInput{{Text: "Decision", Tags: []string{"decision"}}}}, engine.CreateInput{Text: "Meeting B", ReferenceIDs: []string{b.ID}})
	children, err := e.Children(meetings[0].ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	decision := children.Nodes[0]
	decision, err = e.Update(decision.ID, engine.UpdateInput{Text: "Clarified decision", ExpectedRevision: decision.Revision, JournalDate: "2026-11-02"})
	if err != nil {
		t.Fatal(err)
	}
	oldEvents := search(t, e, engine.SearchFilters{Tag: "activity", SubjectID: a.ID, From: "2026-11-02", To: "2026-11-02"})
	if len(oldEvents.Items) != 1 {
		t.Fatal(oldEvents)
	}
	oldEvent := oldEvents.Items[0].Node
	decision, err = e.Move(decision.ID, engine.MoveInput{ParentID: meetings[1].ID, ExpectedRevision: decision.Revision, JournalDate: "2026-11-03"})
	if err != nil {
		t.Fatal(err)
	}
	if got := search(t, e, engine.SearchFilters{Tag: "decision", SubjectID: a.ID}); len(got.Items) != 0 {
		t.Fatal("current decision retained old inherited project", got)
	}
	if got := search(t, e, engine.SearchFilters{Tag: "activity", SubjectID: a.ID, From: "2026-11-02", To: "2026-11-02"}); len(got.Items) != 1 || got.Items[0].Node.ID != oldEvent.ID {
		t.Fatal("old event lost its original context", got)
	}
	if got := search(t, e, engine.SearchFilters{Tag: "activity", SubjectID: b.ID, From: "2026-11-02", To: "2026-11-02"}); len(got.Items) != 0 {
		t.Fatal("old event acquired future context", got)
	}
	for _, project := range []string{a.ID, b.ID} {
		if got := search(t, e, engine.SearchFilters{Tag: "activity", SubjectID: project, From: "2026-11-03", To: "2026-11-03"}); len(got.Items) != 1 {
			t.Fatal("move must capture both contexts", got)
		}
	}
	if err = e.Delete(decision.ID, decision.Revision, "2026-11-04", ""); err != nil {
		t.Fatal(err)
	}
	if get(t, e, meetings[1].ID).HasChildren {
		t.Fatal("last archived child kept parent as branch")
	}
	if got := search(t, e, engine.SearchFilters{Tag: "activity", SubjectID: b.ID, From: "2026-11-04", To: "2026-11-04"}); len(got.Items) != 1 {
		t.Fatal("last-child archive lost project context", got)
	}
	for _, ref := range get(t, e, oldEvent.ID).References {
		target, err := e.Get(ref.TargetID)
		if errors.Is(err, engine.ErrNotFound) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if target.System != "" {
			t.Fatal("activity copied protected ancestors", target)
		}
	}
	check(t, e)
}

func TestBoundedSubjectBrowseMatchesFullAncestorOracle(t *testing.T) {
	e, _ := open(t)
	project, err := e.Create(engine.CreateInput{Text: "Browse project"})
	if err != nil {
		t.Fatal(err)
	}
	for branch := 0; branch < 3; branch++ {
		children := []engine.CreateInput{}
		for child := 0; child < 23; child++ {
			in := engine.CreateInput{Text: fmt.Sprintf("child-%d-%d", branch, child)}
			if child%5 == 0 {
				in.ReferenceIDs = []string{project.ID}
			}
			children = append(children, in)
		}
		capture(t, e, "2026-11-02", engine.CreateInput{Text: fmt.Sprintf("Meeting %d [[%s]] [[Browse project]]", branch, project.ID), ReferenceIDs: []string{project.ID}, Children: children})
	}
	leaf := capture(t, e, "2026-11-02", engine.CreateInput{Text: "Direct leaf", ReferenceIDs: []string{project.ID}})[0]
	if _, err = e.Update(leaf.ID, engine.UpdateInput{Text: "Changed leaf", ExpectedRevision: leaf.Revision, JournalDate: "2026-11-02"}); err != nil {
		t.Fatal(err)
	}
	want := []string{}
	cursor := ""
	for {
		page := search(t, e, engine.SearchFilters{Cursor: cursor, Limit: 100})
		for _, item := range page.Items {
			matches := item.Node.ID == project.ID
			for _, ancestor := range item.Path {
				for _, ref := range ancestor.References {
					matches = matches || ref.TargetID == project.ID
				}
			}
			if matches {
				want = append(want, item.Node.ID)
			}
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	got := []string{}
	cursor = ""
	for {
		page := search(t, e, engine.SearchFilters{SubjectID: project.ID, Cursor: cursor, Limit: 7})
		for _, item := range page.Items {
			got = append(got, item.Node.ID)
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bounded streams differ, got %d want %d\ngot=%v\nwant=%v", len(got), len(want), got, want)
	}
	empty, err := e.Create(engine.CreateInput{Text: "Empty subject"})
	if err != nil {
		t.Fatal(err)
	}
	page := search(t, e, engine.SearchFilters{SubjectID: empty.ID, Limit: 1})
	if len(page.Items) != 1 || page.Items[0].Node.ID != empty.ID || page.NextCursor != "" {
		t.Fatal(page)
	}
}

func TestIdempotencyDistinguishesOmittedAndEmptyReferences(t *testing.T) {
	e, _ := open(t)
	project, err := e.Create(engine.CreateInput{Text: "Reference target"})
	if err != nil {
		t.Fatal(err)
	}
	n := capture(t, e, "2026-11-02", engine.CreateInput{Text: "Decision", ReferenceIDs: []string{project.ID}})[0]
	input := engine.UpdateInput{Text: "Changed decision", ExpectedRevision: n.Revision, JournalDate: "2026-11-02", RequestID: "keep-references"}
	changed, err := e.Update(n.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.References) != 1 {
		t.Fatal(changed)
	}
	input.ReferenceIDs = []string{}
	if _, err = e.Update(n.ID, input); !errors.Is(err, engine.ErrConflict) {
		t.Fatalf("changed intent replayed as identical: %v", err)
	}
	if len(get(t, e, n.ID).References) != 1 {
		t.Fatal("failed conflicting replay changed references")
	}
	input.ExpectedRevision = changed.Revision
	input.RequestID = "clear-references"
	cleared, err := e.Update(n.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.References) != 0 {
		t.Fatal(cleared)
	}
	replay, err := e.Update(n.ID, input)
	if err != nil || replay.Revision != cleared.Revision {
		t.Fatal(replay, err)
	}
	input.ReferenceIDs = nil
	if _, err = e.Update(n.ID, input); !errors.Is(err, engine.ErrConflict) {
		t.Fatalf("omitted references replayed as explicit empty: %v", err)
	}
	check(t, e)
}
