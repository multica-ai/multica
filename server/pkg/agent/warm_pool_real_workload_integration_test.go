//go:build agentintegration

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Drives a real agent through a long coding task: implement four stubbed Go
// packages against their tests, then, in a resumed turn, satisfy two new test
// files. The harness checks the result independently by running the tests
// itself and confirming the agent left every test file untouched. Scenarios:
//
//   - "pool disabled": every turn spawns a fresh process.
//   - "pool miss then hit": turn 1 spawns and parks, turn 2 reuses it.
//   - "cancel then resume": turn 1 is cancelled mid-task and must not be
//     parked; turn 2 resumes the session in a new process and finishes.
//
// Each scenario takes several minutes per turn and consumes real quota:
//
//	MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags agentintegration -timeout 120m \
//	  -run 'TestCodexRealWorkload/pool_disabled' -v ./pkg/agent/
//
// MULTICA_REAL_PARITY_MODEL optionally overrides the model, and
// MULTICA_REAL_WORKLOAD_CANCEL_AFTER (default 2m) sets when the cancel
// scenario cancels turn 1.
func TestCodexRealWorkload(t *testing.T) {
	runRealWorkload(t, "codex")
}

func TestClaudeRealWorkload(t *testing.T) {
	runRealWorkload(t, "claude")
}

const (
	realWorkloadFixture = "testdata/real_workload"

	realWorkloadBuildPrompt = "This Go module has four packages: expr, lru, ledger and semver. " +
		"Each package has a *_test.go file that specifies the required behavior and a stub file whose functions panic. " +
		"Implement every package so that `go test -race ./...` passes. Do not modify, rename or delete any existing *_test.go file. " +
		"Then add your own tests in new files named extra_test.go so that every package reaches at least 90% statement coverage " +
		"under `go test -cover ./...`, and make `go vet ./...` and `gofmt -l .` clean. " +
		"Run the checks yourself and keep iterating until they all pass. Finish with a short summary of what you implemented."

	realWorkloadFollowupPrompt = "I added two test files: expr/ternary_test.go and ledger/interest_test.go. " +
		"Extend the code you wrote in the previous turn so they pass, keeping every existing test green " +
		"and every package at 90% or more statement coverage; you may edit your own extra_test.go files but no other *_test.go file. " +
		"Run `go test -race -cover ./...` before you finish. " +
		"In your final reply, also name which of the four packages from the previous turn was the hardest and why."

	realWorkloadContinuePrompt = "You were interrupted before finishing. Continue the same task: " +
		"make `go test -race ./...` pass and bring every package to 90% or more statement coverage with tests in new extra_test.go files, " +
		"without modifying any provided *_test.go file, then summarize what you implemented."
)

type realWorkloadTurn struct {
	Scenario     string             `json:"scenario"`
	Turn         string             `json:"turn"`
	Status       string             `json:"status"`
	Error        string             `json:"error,omitempty"`
	Seconds      float64            `json:"seconds"`
	SessionID    string             `json:"session_id"`
	WarmReused   bool               `json:"warm_reused"`
	Messages     map[string]int     `json:"messages"`
	ToolCalls    int                `json:"tool_calls"`
	ToolResults  int                `json:"tool_results"`
	Unanswered   int                `json:"tool_calls_without_result"`
	StreamedText int                `json:"streamed_text_bytes"`
	OutputBytes  int                `json:"output_bytes"`
	Usage        TokenUsage         `json:"usage"`
	Models       []string           `json:"models"`
	TestsPass    bool               `json:"tests_pass"`
	Coverage     map[string]float64 `json:"coverage"`
	TestsTouched []string           `json:"tests_touched,omitempty"`
	PoolLen      int                `json:"pool_len"`
	Tools        map[string]int     `json:"tools"`
}

func runRealWorkload(t *testing.T, provider string) {
	requireRealAgentSmoke(t)
	model := os.Getenv("MULTICA_REAL_PARITY_MODEL")
	cancelAfter := 2 * time.Minute
	if raw := os.Getenv("MULTICA_REAL_WORKLOAD_CANCEL_AFTER"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			t.Fatalf("MULTICA_REAL_WORKLOAD_CANCEL_AFTER: %v", err)
		}
		cancelAfter = d
	}

	for _, scenario := range []string{"pool disabled", "pool miss then hit", "cancel then resume"} {
		t.Run(scenario, func(t *testing.T) {
			pooled := scenario != "pool disabled"
			var logs synchronizedBuffer
			logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
			var pool *WarmSessionPool
			if pooled {
				pool = NewWarmSessionPool(WarmSessionPoolConfig{IdleTTL: 30 * time.Minute, MaxIdle: 2, Logger: logger})
				defer pool.Close()
			}

			root := t.TempDir()
			project := filepath.Join(root, "project")
			copyFixtureTree(t, filepath.Join(realWorkloadFixture, "project"), project)
			goEnv := map[string]string{
				"GOCACHE":     filepath.Join(project, ".cache", "go-build"),
				"GOTMPDIR":    filepath.Join(project, ".cache", "tmp"),
				"GOTOOLCHAIN": "local",
				"GOFLAGS":     "-mod=mod",
			}
			if err := os.MkdirAll(goEnv["GOTMPDIR"], 0o755); err != nil {
				t.Fatal(err)
			}

			reusedLogs := 0
			runTurn := func(turn, prompt, resume string, ctx context.Context) (Result, realWorkloadTurn) {
				t.Helper()
				env := map[string]string{"MULTICA_TASK_ID": scenario + "/" + turn}
				for k, v := range goEnv {
					env[k] = v
				}
				b, err := New(provider, Config{Logger: logger, TaskID: turn, WarmPool: pool, Env: env})
				if err != nil {
					t.Fatalf("New(%s): %v", provider, err)
				}
				started := time.Now()
				sess, err := b.Execute(ctx, prompt, ExecOptions{
					Cwd:             project,
					Model:           model,
					ResumeSessionID: resume,
					Timeout:         45 * time.Minute,
				})
				if err != nil {
					t.Fatalf("Execute(%s): %v", turn, err)
				}
				rec := realWorkloadTurn{Scenario: scenario, Turn: turn, Messages: map[string]int{}, Tools: map[string]int{}}
				open := map[string]bool{}
				for msg := range sess.Messages {
					rec.Messages[string(msg.Type)]++
					switch msg.Type {
					case MessageText:
						rec.StreamedText += len(msg.Content)
					case MessageToolUse:
						rec.ToolCalls++
						rec.Tools[msg.Tool]++
						if msg.CallID != "" {
							open[msg.CallID] = true
						}
					case MessageToolResult:
						rec.ToolResults++
						delete(open, msg.CallID)
					case MessageError:
						t.Logf("%s: error message: %s", turn, msg.Content)
					}
				}
				res := <-sess.Result
				rec.Seconds = time.Since(started).Round(time.Second).Seconds()
				rec.Status, rec.Error, rec.SessionID = res.Status, res.Error, res.SessionID
				rec.OutputBytes = len(res.Output)
				rec.Unanswered = len(open)
				for m, u := range res.Usage {
					rec.Models = append(rec.Models, m)
					rec.Usage.InputTokens += u.InputTokens
					rec.Usage.OutputTokens += u.OutputTokens
					rec.Usage.CacheReadTokens += u.CacheReadTokens
					rec.Usage.CacheWriteTokens += u.CacheWriteTokens
				}
				now := strings.Count(logs.String(), provider+" warm session reused")
				rec.WarmReused = now > reusedLogs
				reusedLogs = now
				if pool != nil {
					rec.PoolLen = pool.Len()
				}
				rec.TestsTouched = changedTestFiles(t, project)
				rec.TestsPass, rec.Coverage = runProjectTests(t, project, goEnv)
				line, _ := json.Marshal(rec)
				t.Logf("REAL_WORKLOAD %s %s", provider, line)
				t.Logf("%s final output:\n%s", turn, res.Output)
				return res, rec
			}

			requireCompleted := func(rec realWorkloadTurn) {
				t.Helper()
				if rec.Status != "completed" {
					t.Fatalf("%s: status = %q, error = %q", rec.Turn, rec.Status, rec.Error)
				}
				if rec.SessionID == "" || rec.Usage == (TokenUsage{}) || rec.StreamedText == 0 || rec.ToolCalls == 0 {
					t.Fatalf("%s: incomplete result %+v", rec.Turn, rec)
				}
				if len(rec.TestsTouched) > 0 {
					t.Fatalf("%s: agent modified test files %v", rec.Turn, rec.TestsTouched)
				}
				if !rec.TestsPass {
					t.Fatalf("%s: go test fails after the turn", rec.Turn)
				}
				for _, pkg := range []string{"expr", "ledger", "lru", "semver"} {
					if rec.Coverage[pkg] < 90 {
						t.Fatalf("%s: %s coverage = %.1f%%, want >= 90%%", rec.Turn, pkg, rec.Coverage[pkg])
					}
				}
			}

			var first Result
			var firstRec realWorkloadTurn
			if scenario == "cancel then resume" {
				ctx, cancel := context.WithCancel(context.Background())
				stop := time.AfterFunc(cancelAfter, cancel)
				first, firstRec = runTurn("turn-1", realWorkloadBuildPrompt, "", ctx)
				stop.Stop()
				cancel()
				if firstRec.Status == "completed" {
					t.Fatalf("turn 1 completed before the %s cancel; lower MULTICA_REAL_WORKLOAD_CANCEL_AFTER", cancelAfter)
				}
				if firstRec.SessionID == "" {
					t.Fatal("cancelled turn reported no session id to resume")
				}
				if firstRec.PoolLen != 0 {
					t.Fatalf("cancelled turn left %d sessions parked", firstRec.PoolLen)
				}
			} else {
				first, firstRec = runTurn("turn-1", realWorkloadBuildPrompt, "", context.Background())
				requireCompleted(firstRec)
				if firstRec.WarmReused {
					t.Fatal("turn 1 reused a warm session")
				}
				if pooled && firstRec.PoolLen != 1 {
					t.Fatalf("pool holds %d sessions after turn 1, want 1", firstRec.PoolLen)
				}
			}

			prompt := realWorkloadFollowupPrompt
			if scenario == "cancel then resume" {
				prompt = realWorkloadContinuePrompt
			} else {
				copyFixtureTree(t, filepath.Join(realWorkloadFixture, "followup"), project)
			}
			_, secondRec := runTurn("turn-2", prompt, first.SessionID, context.Background())
			requireCompleted(secondRec)
			if secondRec.SessionID != first.SessionID {
				t.Fatalf("turn 2 session = %q, want resumed %q", secondRec.SessionID, first.SessionID)
			}
			if want := scenario == "pool miss then hit"; secondRec.WarmReused != want {
				t.Fatalf("turn 2 warm reuse = %v, want %v", secondRec.WarmReused, want)
			}
		})
	}
}

func copyFixtureTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture %s: %v", src, err)
	}
}

// changedTestFiles lists fixture test files the agent altered or removed.
func changedTestFiles(t *testing.T, project string) []string {
	t.Helper()
	var changed []string
	for _, dir := range []string{"project", "followup"} {
		src := filepath.Join(realWorkloadFixture, dir)
		_ = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(src, path)
			want, _ := os.ReadFile(path)
			got, readErr := os.ReadFile(filepath.Join(project, rel))
			if dir == "followup" && os.IsNotExist(readErr) {
				return nil
			}
			if readErr != nil || !bytes.Equal(got, want) {
				changed = append(changed, rel)
			}
			return nil
		})
	}
	return changed
}

// runProjectTests runs the project's tests and returns per-package coverage.
func runProjectTests(t *testing.T, project string, goEnv map[string]string) (bool, map[string]float64) {
	t.Helper()
	cmd := exec.Command("go", "test", "-race", "-count=1", "-cover", "./...")
	cmd.Dir = project
	cmd.Env = os.Environ()
	for k, v := range goEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	coverage := map[string]float64{}
	for _, m := range coverageLine.FindAllStringSubmatch(string(out), -1) {
		pct, _ := strconv.ParseFloat(m[2], 64)
		coverage[m[1]] = pct
	}
	if err != nil {
		t.Logf("go test in %s failed: %v\n%s", project, err, out)
		return false, coverage
	}
	return true, coverage
}

var coverageLine = regexp.MustCompile(`(?m)^ok\s+workbench/(\w+)\s.*coverage: ([0-9.]+)% of statements`)
