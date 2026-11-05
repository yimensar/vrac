// Command load fills an empty Vrac database from a JSON plan: free types,
// entities (projects, people) with their notes, and journal days. It is
// meant for one-off migrations whose mapping was prepared elsewhere, such
// as an import from another tool.
//
//	go run ./scripts/load -data data plan.json
//
// Plan format:
//
//	{
//	  "actor":    {"name": "Memos", "source": "memos"},
//	  "types":    ["reference"],
//	  "entities": [{"name": "Northwind/Atlas", "types": ["project"], "aliases": [], "notes": [Draft]}],
//	  "days":     [{"day": "2026-10-26", "nodes": [Draft]}]
//	}
//
// Entities are created in order, at the root or under their parent path;
// days reference them by name. Drafts are engine drafts (input, status, due,
// start, children).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yan-imensar/vrac/internal/engine"
)

type plan struct {
	Actor    engine.Actor `json:"actor"`
	Types    []string     `json:"types"`
	Entities []struct {
		Name    string         `json:"name"`
		Types   []string       `json:"types"`
		Aliases []string       `json:"aliases"`
		Notes   []engine.Draft `json:"notes"`
	} `json:"entities"`
	Days []struct {
		Day   string         `json:"day"`
		Nodes []engine.Draft `json:"nodes"`
	} `json:"days"`
}

func main() {
	data := flag.String("data", "data", "data directory")
	zone := flag.String("timezone", "Europe/Paris", "time zone of journal days")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: load [-data DIR] plan.json")
		os.Exit(2)
	}
	if err := run(filepath.Join(*data, "vrac.sqlite"), *zone, flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		os.Exit(1)
	}
}

func run(path, zone, planPath string) error {
	var p plan
	b, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("plan: %w", err)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return err
	}
	e, err := engine.Open(path, engine.Options{Location: loc})
	if err != nil {
		return err
	}
	defer e.Close()
	ctx := context.Background()
	if days, _ := e.Days(ctx, "", 1); len(days) > 0 {
		return errors.New("the database is not empty; load only fills a new one")
	}
	if roots, _ := e.Roots(ctx, "", 1); len(roots.Nodes) > 0 {
		return errors.New("the database is not empty; load only fills a new one")
	}
	req := 0
	rid := func() string { req++; return fmt.Sprintf("load-%d", req) }
	for _, t := range p.Types {
		if _, err := e.CreateType(ctx, p.Actor, rid(), t); err != nil {
			return fmt.Errorf("type %s: %w", t, err)
		}
	}
	ids := map[string]string{}
	count := 0
	for _, ent := range p.Entities {
		placement := engine.Placement{Root: true}
		name := ent.Name
		if i := strings.LastIndex(name, "/"); i > 0 {
			parent, ok := ids[name[:i]]
			if !ok {
				return fmt.Errorf("entity %s comes before its parent", name)
			}
			placement, name = engine.Placement{ParentID: parent}, name[i+1:]
		}
		input := name
		for _, t := range ent.Types {
			input += " #" + t
		}
		res, err := e.Create(ctx, p.Actor, engine.CreateRequest{RequestID: rid(), Placement: placement,
			Nodes: []engine.Draft{{Input: input, Aliases: ent.Aliases, Children: ent.Notes}}})
		if err != nil {
			return fmt.Errorf("entity %s: %w", ent.Name, err)
		}
		ids[ent.Name] = res.Nodes[0].ID
		count += len(res.Nodes)
	}
	for _, d := range p.Days {
		res, err := e.Create(ctx, p.Actor, engine.CreateRequest{RequestID: rid(), Placement: engine.Placement{Day: d.Day}, Nodes: d.Nodes})
		if err != nil {
			return fmt.Errorf("day %s: %w", d.Day, err)
		}
		count += len(res.Nodes)
	}
	fmt.Printf("loaded %d nodes: %d entities, %d days\n", count, len(p.Entities), len(p.Days))
	return e.Check()
}
