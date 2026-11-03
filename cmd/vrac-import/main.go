package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yimensar/vrac/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Import failed:", err)
		os.Exit(1)
	}
}
func run() error {
	db := flag.String("db", "data/vrac.db", "Destination SQLite database")
	input := flag.String("input", "", "Memos native ZIP or JSON export")
	attachmentDir := flag.String("attachments-dir", "", "Attachment destination; defaults to database directory/attachments")
	metadata := flag.String("metadata-json", "", "Optional connector JSON export to preserve relations omitted from native ZIP")
	flag.Parse()
	if *input == "" {
		return fmt.Errorf("--input is required")
	}
	if *attachmentDir == "" {
		*attachmentDir = filepath.Join(filepath.Dir(*db), "attachments")
	}
	var raws []json.RawMessage
	if strings.EqualFold(filepath.Ext(*input), ".zip") {
		var extra []json.RawMessage
		if *metadata != "" {
			data, err := os.ReadFile(*metadata)
			if err != nil {
				return err
			}
			extra, err = store.ReadMemosJSON(data)
			if err != nil {
				return err
			}
		}
		var err error
		raws, err = store.ReadMemosZIP(*input, *attachmentDir, extra)
		if err != nil {
			return err
		}
	} else {
		data, err := os.ReadFile(*input)
		if err != nil {
			return err
		}
		raws, err = store.ReadMemosJSON(data)
		if err != nil {
			return err
		}
	}
	s, err := store.Open(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	report, err := s.ImportMemos(context.Background(), raws)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
