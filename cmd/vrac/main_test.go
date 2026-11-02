package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yan-imensar/vrac/internal/engine"
)

func TestDevelopmentModeCannotBindPublicly(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8080", ":8080", "192.0.2.1:8080", "[::]:8080"} {
		if err := run([]string{"serve", "--dev", "--listen", address}); err == nil || !strings.Contains(err.Error(), "loopback") {
			t.Fatalf("public development address accepted: %s, %v", address, err)
		}
	}
}

func TestRecoveryCommandsPreserveExportAndIdentity(t *testing.T) {
	dir := t.TempDir()
	database := filepath.Join(dir, "source.sqlite")
	e, err := engine.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := e.Capture("2026-11-02", []engine.CreateInput{{Text: "Meeting [[Atlas]]", Tags: []string{"meeting"}, Children: []engine.CreateInput{{Text: "Keep identities", Tags: []string{"decision"}}}}}, "capture")
	if err != nil {
		t.Fatal(err)
	}
	e.Close()
	export := filepath.Join(dir, "export.json")
	if err := run([]string{"export", "--db", database, "--output", export}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(export)
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"export", "--db", database, "--output", export}); err == nil {
		t.Fatal("existing export was overwritten")
	}
	after, _ := os.ReadFile(export)
	if string(before) != string(after) {
		t.Fatal("existing export changed")
	}
	restored := filepath.Join(dir, "restored.sqlite")
	if err := run([]string{"import", "--db", restored, "--input", export}); err != nil {
		t.Fatal(err)
	}
	copy, err := engine.Open(restored)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	node, err := copy.Get(nodes[0].ID)
	if err != nil || node.ID != nodes[0].ID || node.References[0].TargetID != nodes[0].References[0].TargetID {
		t.Fatalf("round-trip identity mismatch: %#v %v", node, err)
	}
	if err := run([]string{"backup", "--db", database, "--output", filepath.Join(dir, "backup.sqlite")}); err != nil {
		t.Fatal(err)
	}
}
