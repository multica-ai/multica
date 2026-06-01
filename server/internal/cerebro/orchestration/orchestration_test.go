// CEREBRO-PATCH(cerebro-orchestration): FIR-2564 cerebro-only package.

package orchestration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waveIDs(w Wave) string {
	s := ""
	for i, id := range w.NodeIDs {
		if i > 0 {
			s += ","
		}
		s += id
	}
	return s
}

func TestComputeWaves(t *testing.T) {
	// analyse -> (build-frontend, build-api) -> test
	plan := []Node{
		{ID: "analyse"},
		{ID: "build-frontend", DependsOn: []string{"analyse"}},
		{ID: "build-api", DependsOn: []string{"analyse"}},
		{ID: "test", DependsOn: []string{"build-frontend", "build-api"}},
	}
	waves := ComputeWaves(plan)
	if len(waves) != 3 {
		t.Fatalf("expected 3 waves, got %d: %+v", len(waves), waves)
	}
	if got := waveIDs(waves[0]); got != "analyse" {
		t.Errorf("wave 1 = %q, want analyse", got)
	}
	if got := waveIDs(waves[1]); got != "build-api,build-frontend" {
		t.Errorf("wave 2 = %q, want build-api,build-frontend", got)
	}
	if got := waveIDs(waves[2]); got != "test" {
		t.Errorf("wave 3 = %q, want test", got)
	}
	if waves[0].Label != "Wave 1" || waves[2].Label != "Wave 3" {
		t.Errorf("labels wrong: %q %q", waves[0].Label, waves[2].Label)
	}
}

func TestDetectCycle(t *testing.T) {
	none := []Node{{ID: "a"}, {ID: "b", DependsOn: []string{"a"}}}
	if DetectCycle(none) {
		t.Error("false positive cycle on acyclic graph")
	}
	cyc := []Node{{ID: "a", DependsOn: []string{"b"}}, {ID: "b", DependsOn: []string{"a"}}}
	if !DetectCycle(cyc) {
		t.Error("failed to detect A<->B cycle")
	}
}

func TestValidatePlan(t *testing.T) {
	cases := []struct {
		name string
		plan Plan
		code string // expected issue code ("" = valid)
	}{
		{"empty", Plan{}, "plan-nodes-required"},
		{"valid", Plan{Nodes: []Node{
			{ID: "a", Kind: "agent", Prompt: "do x"},
			{ID: "b", Kind: "command", Command: "echo hi", DependsOn: []string{"a"}},
		}}, ""},
		{"missing dep", Plan{Nodes: []Node{
			{ID: "a", Kind: "agent", Prompt: "x", DependsOn: []string{"ghost"}},
		}}, "node-dependency-missing"},
		{"self dep", Plan{Nodes: []Node{
			{ID: "a", Kind: "agent", Prompt: "x", DependsOn: []string{"a"}},
		}}, "node-dependency-self"},
		{"duplicate id", Plan{Nodes: []Node{
			{ID: "a", Kind: "agent", Prompt: "x"},
			{ID: "a", Kind: "agent", Prompt: "y"},
		}}, "node-id-duplicate"},
		{"bad kind", Plan{Nodes: []Node{{ID: "a", Kind: "wizard"}}}, "node-kind-invalid"},
		{"agent no prompt", Plan{Nodes: []Node{{ID: "a", Kind: "agent"}}}, "node-prompt-required"},
		{"cycle", Plan{Nodes: []Node{
			{ID: "a", Kind: "agent", Prompt: "x", DependsOn: []string{"b"}},
			{ID: "b", Kind: "agent", Prompt: "y", DependsOn: []string{"a"}},
		}}, "plan-cycle-detected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := ValidatePlan(tc.plan)
			if tc.code == "" {
				if len(issues) != 0 {
					t.Fatalf("expected valid, got %+v", issues)
				}
				return
			}
			found := false
			for _, is := range issues {
				if is.Code == tc.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected issue %q, got %+v", tc.code, issues)
			}
		})
	}
}

// recordingExec tracks concurrency and write-exclusivity invariants.
type recordingExec struct {
	mu          sync.Mutex
	current     int
	maxParallel int
	writeAlone  bool // true if a write node ever ran with others -> violation flips false-ok below
	order       []string
}

func (r *recordingExec) run(failIDs map[string]int) ExecuteFunc {
	attempts := map[string]int{}
	var amu sync.Mutex
	return func(ctx context.Context, node Node, deps []DepResult) NodeResult {
		r.mu.Lock()
		r.current++
		if r.current > r.maxParallel {
			r.maxParallel = r.current
		}
		// A write node must be the only thing running.
		if node.Write && r.current > 1 {
			r.writeAlone = true // records a violation
		}
		r.order = append(r.order, node.ID)
		r.mu.Unlock()

		time.Sleep(15 * time.Millisecond)

		r.mu.Lock()
		r.current--
		r.mu.Unlock()

		amu.Lock()
		attempts[node.ID]++
		n := attempts[node.ID]
		amu.Unlock()
		if need, ok := failIDs[node.ID]; ok && n <= need {
			return NodeResult{Success: false, Error: "boom"}
		}
		return NodeResult{Success: true, Summary: "ok:" + node.ID}
	}
}

func TestExecutePlanConcurrencyAndWaves(t *testing.T) {
	plan := Plan{
		Concurrency: 4,
		Nodes: []Node{
			{ID: "analyse", Kind: "agent", Prompt: "x"},
			{ID: "build-frontend", Kind: "agent", Prompt: "x", DependsOn: []string{"analyse"}},
			{ID: "build-api", Kind: "agent", Prompt: "x", DependsOn: []string{"analyse"}},
			{ID: "test", Kind: "agent", Prompt: "x", DependsOn: []string{"build-frontend", "build-api"}},
		},
	}
	rec := &recordingExec{}
	run := ExecutePlan(context.Background(), plan, rec.run(nil))
	if run.Status != StatusSuccess {
		t.Fatalf("status = %s err=%s", run.Status, run.Error)
	}
	if rec.maxParallel < 2 {
		t.Errorf("expected the two build nodes to run in parallel, maxParallel=%d", rec.maxParallel)
	}
	for _, n := range run.Nodes {
		if n.Status != StatusSuccess {
			t.Errorf("node %s = %s", n.ID, n.Status)
		}
	}
}

func TestExecutePlanWriteLockIsExclusive(t *testing.T) {
	// Two independent write nodes + two readers, high concurrency.
	plan := Plan{
		Concurrency: 8,
		Nodes: []Node{
			{ID: "read-a", Kind: "agent", Prompt: "x"},
			{ID: "read-b", Kind: "agent", Prompt: "x"},
			{ID: "write-a", Kind: "command", Command: "x", Write: true},
			{ID: "write-b", Kind: "command", Command: "x", Write: true},
		},
	}
	rec := &recordingExec{}
	run := ExecutePlan(context.Background(), plan, rec.run(nil))
	if run.Status != StatusSuccess {
		t.Fatalf("status = %s", run.Status)
	}
	if rec.writeAlone {
		t.Error("a write node ran concurrently with another node — write lock violated")
	}
}

func TestExecutePlanRetry(t *testing.T) {
	plan := Plan{Nodes: []Node{
		{ID: "flaky", Kind: "agent", Prompt: "x", RetryLimit: 2},
	}}
	rec := &recordingExec{}
	// fail first 2 attempts, succeed on the 3rd (within 1+2 attempts)
	run := ExecutePlan(context.Background(), plan, rec.run(map[string]int{"flaky": 2}))
	if run.Status != StatusSuccess {
		t.Fatalf("expected success after retries, got %s", run.Status)
	}
	if n := run.Node("flaky"); n == nil || n.AttemptCount != 3 {
		t.Fatalf("expected 3 attempts, got %+v", n)
	}
}

func TestExecutePlanRetryExhausted(t *testing.T) {
	plan := Plan{Nodes: []Node{
		{ID: "doomed", Kind: "agent", Prompt: "x", RetryLimit: 1},
	}}
	rec := &recordingExec{}
	run := ExecutePlan(context.Background(), plan, rec.run(map[string]int{"doomed": 99}))
	if run.Status != StatusFailed {
		t.Fatalf("expected failed, got %s", run.Status)
	}
	if n := run.Node("doomed"); n == nil || n.AttemptCount != 2 {
		t.Fatalf("expected 2 attempts (1+1 retry), got %+v", n)
	}
}

func TestExecutePlanBlocksDownstream(t *testing.T) {
	// analyse fails -> build & test must be blocked, never run.
	plan := Plan{Nodes: []Node{
		{ID: "analyse", Kind: "agent", Prompt: "x"},
		{ID: "build", Kind: "agent", Prompt: "x", DependsOn: []string{"analyse"}},
		{ID: "test", Kind: "agent", Prompt: "x", DependsOn: []string{"build"}},
	}}
	rec := &recordingExec{}
	run := ExecutePlan(context.Background(), plan, rec.run(map[string]int{"analyse": 99}))
	if run.Status != StatusFailed {
		t.Fatalf("expected failed run, got %s", run.Status)
	}
	if n := run.Node("build"); n.Status != StatusBlocked {
		t.Errorf("build = %s, want blocked", n.Status)
	}
	if n := run.Node("test"); n.Status != StatusBlocked {
		t.Errorf("test = %s, want blocked", n.Status)
	}
	// build/test executors must never have been invoked.
	for _, id := range rec.order {
		if id == "build" || id == "test" {
			t.Errorf("downstream node %s ran despite failed dependency", id)
		}
	}
}

func TestExecutePlanCancel(t *testing.T) {
	plan := Plan{Nodes: []Node{
		{ID: "a", Kind: "agent", Prompt: "x"},
		{ID: "b", Kind: "agent", Prompt: "x", DependsOn: []string{"a"}},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	var ran int32
	exec := func(ctx context.Context, node Node, deps []DepResult) NodeResult {
		atomic.AddInt32(&ran, 1)
		cancel() // cancel during the first node
		time.Sleep(5 * time.Millisecond)
		return NodeResult{Success: true}
	}
	run := ExecutePlan(ctx, plan, exec)
	if run.Status != StatusCancelled && run.Status != StatusFailed {
		t.Fatalf("expected cancelled/failed, got %s", run.Status)
	}
	if n := run.Node("b"); n.Status != StatusCancelled {
		t.Errorf("b = %s, want cancelled", n.Status)
	}
}

func TestNormalizeID(t *testing.T) {
	cases := map[string]string{
		"Build API":  "build-api",
		"  a b  ":    "a-b",
		"已取消":        "",
		"build--api": "build-api",
		"-x-":        "x",
	}
	for in, want := range cases {
		if got := NormalizeID(in); got != want {
			t.Errorf("NormalizeID(%q) = %q, want %q", in, got, want)
		}
	}
}
