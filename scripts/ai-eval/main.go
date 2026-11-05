// Command ai-eval measures how well an AI uses Vrac through MCP. Each
// scenario starts from a fresh sample workspace (scripts/seed), runs one
// non-interactive Claude Code session connected to it with the vrac skill,
// then checks the resulting database and answer.
//
//	go run ./scripts/ai-eval [-only name] [-model sonnet]
//
// It uses the local `claude` CLI and its account; each scenario is one
// session. Results print as a table and are written to ai-eval.json.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yan-imensar/vrac/internal/engine"
)

var ctx = context.Background()

type run struct {
	Scenario string   `json:"scenario"`
	Passed   bool     `json:"passed"`
	Failures []string `json:"failures"`
	Tools    []string `json:"tools"`
	Turns    int      `json:"turns"`
	Seconds  float64  `json:"seconds"`
	Cost     float64  `json:"cost_usd"`
	Input    int      `json:"input_tokens"`
	Output   int      `json:"output_tokens"`
	Answer   string   `json:"answer"`
}

func main() {
	only := flag.String("only", "", "run only scenarios whose name contains this")
	model := flag.String("model", "", "Claude model (default: the CLI's)")
	keep := flag.Bool("keep", false, "keep workspaces")
	out := flag.String("out", "ai-eval.json", "results file")
	flag.Parse()
	root, err := os.MkdirTemp("", "vrac-ai-eval-")
	if err != nil {
		fail(err)
	}
	if !*keep {
		defer os.RemoveAll(root)
	}
	bin := filepath.Join(root, "bin")
	for _, b := range [][2]string{{"vrac", "./cmd/vrac"}, {"seed", "./scripts/seed"}} {
		if out, err := exec.Command("go", "build", "-o", filepath.Join(bin, b[0]), b[1]).CombinedOutput(); err != nil {
			fail(fmt.Errorf("build %s: %v\n%s", b[0], err, out))
		}
	}
	skill, err := filepath.Abs("skills/vrac/SKILL.md")
	if err != nil {
		fail(err)
	}
	var runs []run
	for i, s := range scenarios {
		if *only != "" && !strings.Contains(s.name, *only) {
			continue
		}
		r, err := execute(s, filepath.Join(root, s.name), bin, skill, *model, 18300+i)
		if err != nil {
			fail(fmt.Errorf("%s: %w", s.name, err))
		}
		runs = append(runs, r)
		status := "pass"
		if !r.Passed {
			status = "FAIL " + strings.Join(r.Failures, "; ")
		}
		fmt.Printf("%-28s %3d tools %5.1fs $%.3f  %s\n", s.name, len(r.Tools), r.Seconds, r.Cost, status)
	}
	report(runs)
	b, _ := json.MarshalIndent(runs, "", " ")
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fail(err)
	}
	if *keep {
		fmt.Println("workspaces kept in", root)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "ai-eval:", err)
	os.Exit(1)
}

// execute prepares a workspace, runs one Claude session and checks it.
func execute(s scenario, dir, bin, skill, model string, port int) (run, error) {
	r := run{Scenario: s.name}
	data := filepath.Join(dir, "data")
	if out, err := exec.Command(filepath.Join(bin, "seed"), "-data", data).CombinedOutput(); err != nil {
		return r, fmt.Errorf("seed: %v %s", err, out)
	}
	token, err := exec.Command(filepath.Join(bin, "vrac"), "token", "eval", "-data", data).Output()
	if err != nil {
		return r, fmt.Errorf("token: %w", err)
	}
	e, err := engine.Open(filepath.Join(data, "vrac.sqlite"), engine.Options{Location: paris})
	if err != nil {
		return r, err
	}
	w := &world{e: e}
	before := s.prepare(w)
	server := exec.Command(filepath.Join(bin, "vrac"), "serve", "-data", data, "-listen", fmt.Sprintf("127.0.0.1:%d", port), "-timezone", "Europe/Paris")
	if err := server.Start(); err != nil {
		return r, err
	}
	defer func() { server.Process.Signal(os.Interrupt); server.Wait() }()
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	for range 50 {
		if res, err := http.Get(url + "/healthz"); err == nil {
			res.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	config := filepath.Join(dir, "mcp.json")
	cfg := fmt.Sprintf(`{"mcpServers":{"vrac":{"type":"http","url":"%s/mcp","headers":{"Authorization":"Bearer %s"}}}}`, url, strings.TrimSpace(string(token)))
	if err := os.WriteFile(config, []byte(cfg), 0o600); err != nil {
		return r, err
	}
	prompt := s.prompt
	if s.promptf != nil {
		prompt = s.promptf()
	}
	args := []string{"-p", prompt, "--mcp-config", config, "--strict-mcp-config", "--tools", "", "--allowedTools", "mcp__vrac",
		"--setting-sources", "project", "--no-session-persistence", "--append-system-prompt-file", skill,
		"--output-format", "stream-json", "--verbose"}
	if model != "" {
		args = append(args, "--model", model)
	}
	cmd := exec.Command("claude", args...)
	cmd.Dir = dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return r, err
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return r, err
	}
	scan := bufio.NewScanner(stdout)
	scan.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for scan.Scan() {
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"content"`
			} `json:"message"`
			Result string  `json:"result"`
			Turns  int     `json:"num_turns"`
			Cost   float64 `json:"total_cost_usd"`
			Usage  struct {
				Input       int `json:"input_tokens"`
				CacheCreate int `json:"cache_creation_input_tokens"`
				CacheRead   int `json:"cache_read_input_tokens"`
				Output      int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(scan.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "assistant":
			for _, c := range ev.Message.Content {
				if c.Type == "tool_use" {
					r.Tools = append(r.Tools, strings.TrimPrefix(c.Name, "mcp__vrac__"))
				}
			}
		case "result":
			r.Answer, r.Turns, r.Cost = ev.Result, ev.Turns, ev.Cost
			r.Input = ev.Usage.Input + ev.Usage.CacheCreate + ev.Usage.CacheRead
			r.Output = ev.Usage.Output
		}
	}
	if err := cmd.Wait(); err != nil {
		r.Failures = append(r.Failures, "claude: "+err.Error())
	}
	r.Seconds = time.Since(start).Seconds()
	w.answer = r.Answer
	w.changes = changes(e)
	r.Failures = append(r.Failures, s.check(w, before)...)
	r.Passed = len(r.Failures) == 0
	return r, e.Close()
}

func report(runs []run) {
	passed, tools, cost, secs := 0, 0, 0.0, 0.0
	for _, r := range runs {
		if r.Passed {
			passed++
		}
		tools += len(r.Tools)
		cost += r.Cost
		secs += r.Seconds
	}
	if len(runs) == 0 {
		return
	}
	n := float64(len(runs))
	fmt.Printf("\n%d/%d passed · %.1f tool calls, %.1fs and $%.3f per scenario on average\n", passed, len(runs), float64(tools)/n, secs/n, cost/n)
}
