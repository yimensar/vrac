// vrac-bench measures file-backed search on reproducible synthetic data or an
// existing corpus. It does not print private note contents or query snippets.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yimensar/vrac/internal/model"
	"github.com/yimensar/vrac/internal/store"
)

type distribution struct {
	P50 float64 `json:"p50Ms"`
	P95 float64 `json:"p95Ms"`
	Max float64 `json:"maxMs"`
}
type result struct {
	Notes            int           `json:"notes"`
	DatabaseBytes    int64         `json:"databaseBytes"`
	ImportMs         float64       `json:"importMs,omitempty"`
	Search           distribution  `json:"fulltext"`
	Baseline         distribution  `json:"substringBaseline"`
	List             distribution  `json:"timeline"`
	Read             distribution  `json:"read"`
	Create           *distribution `json:"create,omitempty"`
	Update           *distribution `json:"update,omitempty"`
	Queries          int           `json:"queriesPerRun"`
	Iterations       int           `json:"iterations"`
	CountsEquivalent bool          `json:"singleTokenCountsEquivalent"`
}
type report struct {
	At         string   `json:"at"`
	Go         string   `json:"go"`
	OS         string   `json:"os"`
	Arch       string   `json:"arch"`
	CPUCount   int      `json:"cpuCount"`
	Conditions string   `json:"conditions"`
	Results    []result `json:"results"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Benchmark failed:", err)
		os.Exit(1)
	}
}
func run() error {
	sizes := flag.String("sizes", "1000,10000,100000", "Synthetic corpus sizes")
	dbPath := flag.String("db", "", "Read-only workload on existing database instead of synthetic data")
	iterations := flag.Int("iterations", 30, "Measured iterations per query after warmup")
	output := flag.String("output", "", "Write JSON report path instead of stdout")
	flag.Parse()
	if *iterations < 1 || *iterations > 1000 {
		return fmt.Errorf("iterations must be 1..1000")
	}
	out := report{At: time.Now().UTC().Format(time.RFC3339), Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, CPUCount: runtime.NumCPU(), Conditions: "File-backed SQLite, WAL, synchronous FULL, one connection, warm OS/SQLite caches, serial in-process calls; includes count and first 50 complete notes. Substring baseline uses case-insensitive literal contains on same active corpus, with identical decoded response fields. No network, browser or embedding/model latency. Existing corpus mode does not mutate notes.", Results: []result{}}
	if *dbPath != "" {
		s, err := store.Open(*dbPath)
		if err != nil {
			return err
		}
		defer s.Close()
		r, err := measure(s, *dbPath, *iterations, false)
		if err != nil {
			return err
		}
		out.Results = append(out.Results, r)
	} else {
		for _, sizeText := range strings.Split(*sizes, ",") {
			size, err := strconv.Atoi(sizeText)
			if err != nil || size < 10 || size > 1000000 {
				return fmt.Errorf("invalid synthetic size")
			}
			dir, err := os.MkdirTemp("", "vrac-bench-")
			if err != nil {
				return err
			}
			p := filepath.Join(dir, "benchmark.db")
			s, err := store.Open(p)
			if err != nil {
				os.RemoveAll(dir)
				return err
			}
			start := time.Now()
			if err = seed(s, size); err != nil {
				s.Close()
				os.RemoveAll(dir)
				return err
			}
			seedMs := float64(time.Since(start).Microseconds()) / 1000
			s.DB().Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			r, err := measure(s, p, *iterations, true)
			s.Close()
			os.RemoveAll(dir)
			if err != nil {
				return err
			}
			r.ImportMs = seedMs
			out.Results = append(out.Results, r)
		}
	}
	f := os.Stdout
	if *output != "" {
		var err error
		f, err = os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}
func seed(s *store.Store, size int) error {
	topics := []string{"Salesforce JWT Bitbucket deployment authentication", "NixOS infrastructure flake service configuration", "Kopia Restic backup restore retention", "React Markdown editor keyboard mobile", "Dataverse PowerPlatform migration flow monitoring", "PostgreSQL query index database performance", "OAuth MCP authorization PKCE token", "Docker container reverse proxy nginx", "Go SQLite transaction persistence concurrency", "Project meeting decision documentation knowledge"}
	raws := make([]json.RawMessage, 0, size)
	for i := 0; i < size; i++ {
		topic := topics[i%len(topics)]
		raws = append(raws, json.RawMessage(fmt.Sprintf(`{"uid":"synthetic-%06d","content":%s,"createTime":"2026-11-03T12:00:00Z","updateTime":"2026-11-03T12:00:00Z","state":"NORMAL"}`, i, strconv.Quote(fmt.Sprintf("# %s\n\nIncident%06d records a tested decision for environment %d. %s. Keep exact identifiers and source evidence.\n\n#topic/%d #synthetic", strings.Fields(topic)[0], i, i%4, topic, i%len(topics))))))
	}
	_, err := s.ImportMemos(context.Background(), raws)
	return err
}
func measure(s *store.Store, path string, iterations int, mutate bool) (result, error) {
	ctx := context.Background()
	stats, err := s.Stats(ctx)
	if err != nil {
		return result{}, err
	}
	r := result{Notes: stats.Active + stats.Archived, Iterations: iterations, CountsEquivalent: true}
	if st, err := os.Stat(path); err == nil {
		r.DatabaseBytes = st.Size()
	}
	queries := []string{"salesforce", "JWT", "restic", "nixos", "OAuth", "incident000042", "missinguniquetoken"}
	r.Queries = len(queries)
	fulltext := []float64{}
	baseline := []float64{}
	for _, q := range queries {
		page, err := s.List(ctx, model.Query{Q: q, Limit: 50})
		if err != nil {
			return r, err
		}
		count, err := baselineContains(ctx, s.DB(), q)
		if err != nil {
			return r, err
		}
		if count != page.Total {
			r.CountsEquivalent = false
		}
		for i := 0; i < 3; i++ {
			if _, err = s.List(ctx, model.Query{Q: q, Limit: 50}); err != nil {
				return r, err
			}
			if _, err = baselineContains(ctx, s.DB(), q); err != nil {
				return r, err
			}
		}
		for i := 0; i < iterations; i++ {
			start := time.Now()
			if _, err = s.List(ctx, model.Query{Q: q, Limit: 50}); err != nil {
				return r, err
			}
			fulltext = append(fulltext, ms(start))
			start = time.Now()
			if _, err = baselineContains(ctx, s.DB(), q); err != nil {
				return r, err
			}
			baseline = append(baseline, ms(start))
		}
	}
	r.Search = dist(fulltext)
	r.Baseline = dist(baseline)
	timeline := []float64{}
	read := []float64{}
	page, err := s.List(ctx, model.Query{Limit: 50})
	if err != nil {
		return r, err
	}
	for i := 0; i < iterations*3; i++ {
		start := time.Now()
		if _, err = s.List(ctx, model.Query{Limit: 50}); err != nil {
			return r, err
		}
		timeline = append(timeline, ms(start))
		if len(page.Notes) > 0 {
			start = time.Now()
			if _, err = s.Get(ctx, page.Notes[i%len(page.Notes)].ID); err != nil {
				return r, err
			}
			read = append(read, ms(start))
		}
	}
	r.List = dist(timeline)
	r.Read = dist(read)
	if mutate {
		creates := []float64{}
		updates := []float64{}
		for i := 0; i < iterations; i++ {
			start := time.Now()
			n, err := s.Create(ctx, store.CreateInput{Content: fmt.Sprintf("Benchmark capture %d #measurement", i)})
			if err != nil {
				return r, err
			}
			creates = append(creates, ms(start))
			content := n.Content + " updated"
			start = time.Now()
			if _, err = s.Update(ctx, n.ID, store.UpdateInput{Content: &content, Version: n.Version}); err != nil {
				return r, err
			}
			updates = append(updates, ms(start))
		}
		a, b := dist(creates), dist(updates)
		r.Create = &a
		r.Update = &b
	}
	return r, nil
}
func baselineContains(ctx context.Context, db *sql.DB, q string) (int, error) {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM notes WHERE state='active' AND instr(lower(content),lower(?))>0", q).Scan(&count); err != nil {
		return 0, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id,content,title,tags,created_at,updated_at,state,pinned,version,source,source_id,attachments,relations FROM notes WHERE state='active' AND instr(lower(content),lower(?))>0 ORDER BY pinned DESC,created_sort DESC,id DESC LIMIT 50`, q)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var n model.Note
		var tags, attachments, relations string
		if err = rows.Scan(&n.ID, &n.Content, &n.Title, &tags, &n.CreatedAt, &n.UpdatedAt, &n.State, &n.Pinned, &n.Version, &n.Source, &n.SourceID, &attachments, &relations); err != nil {
			return 0, err
		}
		if err = json.Unmarshal([]byte(tags), &n.Tags); err != nil {
			return 0, err
		}
		if err = json.Unmarshal([]byte(attachments), &n.Attachments); err != nil {
			return 0, err
		}
		if err = json.Unmarshal([]byte(relations), &n.Relations); err != nil {
			return 0, err
		}
	}
	return count, rows.Err()
}
func ms(start time.Time) float64 { return float64(time.Since(start).Nanoseconds()) / 1e6 }
func dist(values []float64) distribution {
	if len(values) == 0 {
		return distribution{}
	}
	sort.Float64s(values)
	percentile := func(p float64) float64 {
		idx := int(math.Ceil(p*float64(len(values)))) - 1
		if idx < 0 {
			idx = 0
		}
		return values[idx]
	}
	return distribution{P50: percentile(.5), P95: percentile(.95), Max: values[len(values)-1]}
}
