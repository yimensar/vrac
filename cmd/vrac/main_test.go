package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Regression: flags after a positional argument were ignored, so
// "vrac token NAME -data DIR" wrote to the default data directory.
func TestFlagsAfterArguments(t *testing.T) {
	dir := t.TempDir()
	if err := run([]string{"token", "eval", "-data", dir}); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "vrac.sqlite"))
	if len(matches) != 1 {
		t.Fatal("the token was not written to -data")
	}
	if err := run([]string{"token", "-data", dir}); err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("missing name accepted: %v", err)
	}
}
