package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/yan-imensar/vrac/internal/engine"
)

func main() {
	nodes := flag.Int("nodes", 5000000, "number of historical nodes")
	reuse := flag.Bool("reuse", false, "measure an existing generated fixture (mutates benchmark data)")
	iterations := flag.Int("iterations", 1000, "iterations per operation")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: vrac-bench [-nodes 5000000] [-iterations 100] <new-database>")
		os.Exit(2)
	}
	var report engine.PerformanceReport
	var err error
	if *reuse {
		report, err = engine.MeasurePerformanceScenario(flag.Arg(0), *iterations)
	} else {
		report, err = engine.RunPerformanceScenario(flag.Arg(0), *nodes, *iterations, func(count int) {
			if count%100000 == 0 || count == *nodes {
				fmt.Fprintf(os.Stderr, "generated %d/%d nodes\n", count, *nodes)
			}
		})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !report.Passed {
		os.Exit(1)
	}
}
