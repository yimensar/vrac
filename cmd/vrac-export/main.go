package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/yimensar/vrac/internal/store"
	"os"
)

func main() {
	db := flag.String("db", "data/vrac.db", "SQLite database")
	output := flag.String("output", "", "Export path; defaults to stdout")
	flag.Parse()
	if err := run(*db, *output); err != nil {
		fmt.Fprintln(os.Stderr, "Export failed:", err)
		os.Exit(1)
	}
}
func run(db, output string) error {
	s, err := store.Open(db)
	if err != nil {
		return err
	}
	defer s.Close()
	data, err := s.Export(context.Background())
	if err != nil {
		return err
	}
	out := os.Stdout
	if output != "" {
		out, err = os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		defer out.Close()
	}
	e := json.NewEncoder(out)
	e.SetIndent("", "  ")
	return e.Encode(data)
}
