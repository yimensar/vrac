// Command bench builds a large, realistic Vrac database and measures the
// engine operations behind the interface and MCP, in process.
//
//	go run ./scripts/bench -nodes 200000
//
// It reports p50, p95 and p99 per operation. Times exclude HTTP.
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yan-imensar/vrac/internal/engine"
)

var ctx = context.Background()

func main() {
	nodes := flag.Int("nodes", 200_000, "approximate number of nodes to create")
	dir := flag.String("data", "", "data directory (a temporary one by default)")
	runs := flag.Int("runs", 200, "measurements per operation")
	flag.Parse()
	if err := run(*nodes, *dir, *runs); err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}

type world struct {
	projects, people []engine.Node
	days             []string
}

func run(target int, dir string, runs int) error {
	if dir == "" {
		d, err := os.MkdirTemp("", "vrac-bench-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(d)
		dir = d
	}
	loc, _ := time.LoadLocation("Europe/Paris")
	e, err := engine.Open(filepath.Join(dir, "vrac.sqlite"), engine.Options{Location: loc})
	if err != nil {
		return err
	}
	defer e.Close()
	r := rand.New(rand.NewPCG(1, 2))
	start := time.Now()
	w, count, err := build(e, r, target)
	if err != nil {
		return err
	}
	fmt.Printf("built %d nodes in %s (%d projects, %d people, %d days)\n\n", count, time.Since(start).Round(time.Second), len(w.projects), len(w.people), len(w.days))
	measure(e, r, w, runs)
	return nil
}

var actor = engine.Actor{Name: "bench", Source: "bench"}
var req int

func rid() string { req++; return fmt.Sprintf("b%d", req) }

// build writes projects, people, then journal days of meetings, tasks,
// decisions and notes referencing them, until about target nodes exist.
func build(e *engine.Engine, r *rand.Rand, target int) (world, int, error) {
	var w world
	count := 0
	roots := func(n int, typ, prefix string) ([]engine.Node, error) {
		var drafts []engine.Draft
		for i := range n {
			drafts = append(drafts, engine.Draft{Input: fmt.Sprintf("%s %d #%s", prefix, i, typ)})
		}
		res, err := e.Create(ctx, actor, engine.CreateRequest{RequestID: rid(), Placement: engine.Placement{Root: true}, Nodes: drafts})
		count += len(res.Nodes)
		return res.Nodes, err
	}
	var err error
	if w.projects, err = roots(60, "project", "Project"); err != nil {
		return w, 0, err
	}
	if w.people, err = roots(300, "person", "Person"); err != nil {
		return w, 0, err
	}
	ref := func(list []engine.Node) string {
		n := list[r.IntN(len(list))]
		return "[[" + n.ID + "]]"
	}
	// Projects are unevenly active: the first ones get most of the work.
	project := func() string {
		i := min(int(r.ExpFloat64()*8), len(w.projects)-1)
		return "[[" + w.projects[i].ID + "]]"
	}
	words := strings.Fields("estimate schedule review import migration notification portal export budget design release contract")
	word := func() string { return words[r.IntN(len(words))] }
	day := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	for count < target {
		date := day.Format("2006-01-02")
		w.days = append(w.days, date)
		var drafts []engine.Draft
		for range 3 + r.IntN(4) {
			m := engine.Draft{Input: fmt.Sprintf("#meeting %s with %s about %s", word(), ref(w.people), project())}
			for range 2 + r.IntN(5) {
				switch r.IntN(4) {
				case 0:
					// Most tasks get done; one in ten stays open.
					status := "done"
					if r.IntN(10) == 0 {
						status = "open"
					}
					m.Children = append(m.Children, engine.Draft{Input: fmt.Sprintf("Send the %s to %s #task", word(), ref(w.people)), Status: status, Due: day.AddDate(0, 0, r.IntN(20)).Format("2006-01-02")})
				case 1:
					m.Children = append(m.Children, engine.Draft{Input: fmt.Sprintf("We keep the current %s #decision", word())})
				default:
					m.Children = append(m.Children, engine.Draft{Input: fmt.Sprintf("Notes on the %s and the %s", word(), word())})
				}
			}
			drafts = append(drafts, m)
		}
		for range 3 + r.IntN(8) {
			drafts = append(drafts, engine.Draft{Input: fmt.Sprintf("Thinking about the %s of %s", word(), project())})
		}
		res, err := e.Create(ctx, actor, engine.CreateRequest{RequestID: rid(), Placement: engine.Placement{Day: date}, Nodes: drafts})
		if err != nil {
			return w, 0, err
		}
		count += len(res.Nodes)
		day = day.AddDate(0, 0, 1)
	}
	return w, count, nil
}

type op struct {
	name string
	fn   func() error
}

func measure(e *engine.Engine, r *rand.Rand, w world, runs int) {
	busiest := w.projects[0].ID
	quiet := w.projects[len(w.projects)-1].ID
	person := w.people[0].ID
	recent := w.days[len(w.days)-1]
	page, _ := e.Day(ctx, recent, "", 0)
	var lines []engine.Node
	for _, n := range page.Children.Nodes {
		lines = append(lines, n)
	}
	revision := map[string]int64{}
	for _, n := range lines {
		revision[n.ID] = n.Revision
	}
	ops := []op{
		{"write a journal line", func() error {
			_, err := e.Create(ctx, actor, engine.CreateRequest{RequestID: rid(), Nodes: []engine.Draft{{Input: "A new note about [[" + busiest + "]] #task"}}})
			return err
		}},
		{"edit a line", func() error {
			n := lines[r.IntN(len(lines))]
			in := "Edited " + fmt.Sprint(r.IntN(1000))
			res, err := e.Update(ctx, actor, engine.UpdateRequest{RequestID: rid(), ID: n.ID, ExpectedRevision: revision[n.ID], Input: &in, KeepTypes: true})
			if err == nil {
				revision[n.ID] = res.Nodes[0].Revision
			}
			return err
		}},
		{"read a journal day", func() error { _, err := e.Day(ctx, w.days[r.IntN(len(w.days))], "", 0); return err }},
		{"read children", func() error { _, err := e.Children(ctx, lines[r.IntN(len(lines))].ID, "", 0); return err }},
		{"search words", func() error { _, err := e.Search(ctx, engine.Query{Text: "migration review", Limit: 50}); return err }},
		{"open tasks of the busiest project", func() error {
			_, err := e.Search(ctx, engine.Query{Concerns: []string{busiest}, Types: []string{"task"}, Status: "open", Limit: 50})
			return err
		}},
		{"decisions of a quiet project", func() error {
			_, err := e.Search(ctx, engine.Query{Concerns: []string{quiet}, Types: []string{"decision"}, Limit: 20})
			return err
		}},
		{"tasks of a project and a person", func() error {
			_, err := e.Search(ctx, engine.Query{Concerns: []string{busiest, person}, Types: []string{"task"}, Limit: 50})
			return err
		}},
		{"sections of the busiest project", func() error { _, err := e.NodeSections(ctx, busiest); return err }},
		{"decisions of the busiest project", func() error {
			_, err := e.Search(ctx, engine.Query{Concerns: []string{busiest}, Types: []string{"decision"}, Limit: 20})
			return err
		}},
		{"past meetings of the busiest project", func() error {
			_, err := e.Search(ctx, engine.Query{Concerns: []string{busiest}, Types: []string{"meeting"}, To: recent, Limit: 10})
			return err
		}},
		{"upcoming of the busiest project", func() error {
			_, err := e.Search(ctx, engine.Query{Concerns: []string{busiest}, Types: []string{"meeting", "event"}, From: recent, Scheduled: true, Sort: "date_asc", Limit: 10})
			return err
		}},
		{"mentions of the busiest project", func() error { _, err := e.Search(ctx, engine.Query{Mentions: busiest, Limit: 30}); return err }},
		{"open tasks due this month", func() error {
			_, err := e.Search(ctx, engine.Query{Types: []string{"task"}, Status: "open", From: recent[:8] + "01", To: recent, Scheduled: true, Limit: 100})
			return err
		}},
		{"complete a name", func() error { _, err := e.Resolve(ctx, "Project 1", 10); return err }},
		{"complete words", func() error { _, err := e.Resolve(ctx, "budget portal", 10); return err }},
		{"project table", func() error { _, err := e.TypeTable(ctx, "project"); return err }},
		{"move the busiest project", func() error {
			v, err := e.GetNode(ctx, busiest)
			if err != nil {
				return err
			}
			// To the root and back: two moves rewriting its context.
			_, err = e.Move(ctx, actor, engine.MoveRequest{RequestID: rid(), ID: busiest, ExpectedRevision: v.Node.Revision, Placement: engine.Placement{ParentID: w.projects[1].ID}})
			if err != nil {
				return err
			}
			_, err = e.Move(ctx, actor, engine.MoveRequest{RequestID: rid(), ID: busiest, ExpectedRevision: v.Node.Revision + 1, Placement: engine.Placement{Root: true}})
			return err
		}},
	}
	fmt.Printf("%-36s %10s %10s %10s\n", "operation", "p50", "p95", "p99")
	for _, o := range ops {
		n := runs
		switch o.name {
		case "project table", "sections of the busiest project":
			n = max(runs/10, 10)
		case "move the busiest project":
			n = 3
		}
		var d []time.Duration
		for range n {
			t := time.Now()
			if err := o.fn(); err != nil {
				fmt.Printf("%-36s error: %v\n", o.name, err)
				break
			}
			d = append(d, time.Since(t))
		}
		if len(d) == 0 {
			continue
		}
		slices.Sort(d)
		q := func(p float64) time.Duration { return d[min(int(float64(len(d))*p), len(d)-1)] }
		fmt.Printf("%-36s %10s %10s %10s\n", o.name, round(q(0.5)), round(q(0.95)), round(q(0.99)))
	}
}

func round(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000)
	}
	return d.Round(100 * time.Microsecond).String()
}
