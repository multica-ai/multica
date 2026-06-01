// CEREBRO-PATCH(cerebro-orchestration): FIR-2564 cerebro-only package.

package orchestration

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Node/run statuses.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSuccess   = "success"
	StatusFailed    = "failed"
	StatusBlocked   = "blocked"
	StatusCancelled = "cancelled"
)

// NodeResult is what an ExecuteFunc returns for a single attempt.
type NodeResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Summary string `json:"summary,omitempty"`
	Output  any    `json:"output,omitempty"`
}

// DepResult is a finished dependency handed to a node so it can read upstream
// output (e.g. the analysis a build step depends on).
type DepResult struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
	Output  any    `json:"output,omitempty"`
}

// ExecuteFunc actually runs one node. The engine never runs work itself — this
// is the single injection point. It is called once per attempt; the engine
// handles retries, ordering, concurrency and the write lock around it.
type ExecuteFunc func(ctx context.Context, node Node, deps []DepResult) NodeResult

// NodeRun is the live state of a node during/after a run.
type NodeRun struct {
	Node
	Status       string    `json:"status"`
	AttemptCount int       `json:"attemptCount"`
	Summary      string    `json:"summary,omitempty"`
	Error        string    `json:"error,omitempty"`
	Output       any       `json:"output,omitempty"`
	StartedAt    time.Time `json:"startedAt,omitempty"`
	EndedAt      time.Time `json:"endedAt,omitempty"`
}

// RunResult is the outcome of executing a whole plan.
type RunResult struct {
	Status      string     `json:"status"`
	Concurrency int        `json:"concurrency"`
	Waves       []Wave     `json:"waves"`
	Nodes       []*NodeRun `json:"nodes"`
	Logs        []string   `json:"logs"`
	Error       string     `json:"error,omitempty"`
}

// Node returns the run record for an id (nil if absent).
func (r *RunResult) Node(id string) *NodeRun {
	for _, n := range r.Nodes {
		if n.ID == NormalizeID(id) {
			return n
		}
	}
	return nil
}

type completion struct {
	id         string
	status     string // success | failed | cancelled
	summary    string
	err        string
	output     any
	wantsWrite bool
}

// ExecutePlan runs a plan to completion. Scheduling rules, faithful to the
// codexmate pattern:
//   - a node starts only when all its dependencies have succeeded;
//   - up to `concurrency` nodes run at once;
//   - a write node runs exclusively: never alongside any other node, and never
//     while another write node holds the lock (so two agents can't mutate at
//     the same time);
//   - if any dependency fails/blocks/cancels, the node is marked blocked and
//     never runs (failure cascades downstream);
//   - a failed node is retried up to RetryLimit extra attempts;
//   - cancelling ctx stops scheduling; pending nodes become cancelled.
//
// It assumes ValidatePlan already passed; call ValidatePlan first.
func ExecutePlan(ctx context.Context, plan Plan, exec ExecuteFunc) *RunResult {
	if exec == nil {
		exec = func(context.Context, Node, []DepResult) NodeResult {
			return NodeResult{Success: false, Error: "no execute function configured"}
		}
	}
	concurrency := clampInt(plan.Concurrency, defaultConcurrency, minConcurrency, maxConcurrency)

	run := &RunResult{
		Status:      StatusRunning,
		Concurrency: concurrency,
		Waves:       ComputeWaves(plan.Nodes),
		Nodes:       make([]*NodeRun, 0, len(plan.Nodes)),
		Logs:        []string{},
	}
	nodeMap := map[string]*NodeRun{}
	for _, n := range plan.Nodes {
		n.ID = NormalizeID(n.ID)
		n.DependsOn = normalizeDeps(n.DependsOn)
		nr := &NodeRun{Node: n, Status: StatusPending}
		run.Nodes = append(run.Nodes, nr)
		nodeMap[n.ID] = nr
	}

	hasFailedDep := func(nr *NodeRun) bool {
		for _, dep := range nr.DependsOn {
			d := nodeMap[dep]
			if d != nil && (d.Status == StatusFailed || d.Status == StatusBlocked || d.Status == StatusCancelled) {
				return true
			}
		}
		return false
	}
	isReady := func(nr *NodeRun) bool {
		for _, dep := range nr.DependsOn {
			d := nodeMap[dep]
			if d == nil || d.Status != StatusSuccess {
				return false
			}
		}
		return true
	}
	markBlocked := func() {
		for _, nr := range run.Nodes {
			if nr.Status == StatusPending && hasFailedDep(nr) {
				nr.Status = StatusBlocked
				nr.Error = "blocked by failed dependency"
				run.Logs = append(run.Logs, "node blocked: "+nr.ID)
			}
		}
	}

	done := make(chan completion)
	var wg sync.WaitGroup
	active := 0
	writeLock := false
	cancelled := false

	for {
		if ctx.Err() != nil {
			cancelled = true
		}
		if cancelled {
			for _, nr := range run.Nodes {
				if nr.Status == StatusPending {
					nr.Status = StatusCancelled
					nr.Error = "cancelled before start"
				}
			}
			run.Logs = append(run.Logs, "run cancelled")
		}

		markBlocked()

		started := false
		if !cancelled {
			ready := []*NodeRun{}
			for _, nr := range run.Nodes {
				if nr.Status == StatusPending && isReady(nr) {
					ready = append(ready, nr)
				}
			}
			sort.Slice(ready, func(i, j int) bool { return ready[i].ID < ready[j].ID })

			for _, nr := range ready {
				if active >= concurrency {
					break
				}
				wantsWrite := nr.Write
				// Write lock: a write node is exclusive; nothing runs beside it.
				if wantsWrite && (writeLock || active > 0) {
					continue
				}
				if !wantsWrite && writeLock {
					continue
				}
				if wantsWrite {
					writeLock = true
				}
				nr.Status = StatusRunning
				nr.StartedAt = now()
				active++
				started = true

				deps := make([]DepResult, 0, len(nr.DependsOn))
				for _, dep := range nr.DependsOn {
					if d := nodeMap[dep]; d != nil {
						deps = append(deps, DepResult{ID: d.ID, Status: d.Status, Summary: d.Summary, Output: d.Output})
					}
				}
				run.Logs = append(run.Logs, "node started: "+nr.ID)
				wg.Add(1)
				go func(nr *NodeRun, deps []DepResult) {
					defer wg.Done()
					done <- runNodeWithRetry(ctx, exec, nr, deps)
				}(nr, deps)
			}
		}

		if active == 0 {
			pending := false
			for _, nr := range run.Nodes {
				if nr.Status == StatusPending {
					pending = true
					break
				}
			}
			if !pending {
				break // nothing running, nothing pending -> finished
			}
			if !started {
				// Pending nodes but none schedulable and nothing running:
				// only reachable on a cycle (ValidatePlan should prevent it).
				run.Status = StatusFailed
				run.Error = "run stalled: no node can be scheduled (dependency cycle?)"
				run.Logs = append(run.Logs, run.Error)
				break
			}
			continue
		}

		c := <-done
		active--
		if c.wantsWrite {
			writeLock = false
		}
		nr := nodeMap[c.id]
		if nr != nil {
			nr.Status = c.status
			nr.Summary = c.summary
			nr.Error = c.err
			nr.Output = c.output
			nr.EndedAt = now()
			run.Logs = append(run.Logs, "node "+c.status+": "+c.id)
		}
	}

	// Drain any stragglers (e.g. on cancel) so no goroutine leaks.
	go func() { wg.Wait(); close(done) }()
	for range done {
		active--
		if active < 0 {
			active = 0
		}
	}

	finalizeStatus(run, cancelled)
	return run
}

// runNodeWithRetry runs one node, retrying on failure up to RetryLimit extra
// attempts. It mutates only AttemptCount on the passed *NodeRun (the scheduler
// owns Status), then returns the final outcome over the channel.
func runNodeWithRetry(ctx context.Context, exec ExecuteFunc, nr *NodeRun, deps []DepResult) completion {
	maxAttempts := 1 + clampInt(nr.RetryLimit, 0, 0, maxRetryLimit)
	var res NodeResult
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		nr.AttemptCount = attempt
		if ctx.Err() != nil {
			return completion{id: nr.ID, status: StatusCancelled, err: "cancelled while running", wantsWrite: nr.Write}
		}
		res = exec(ctx, nr.Node, deps)
		if res.Success {
			return completion{id: nr.ID, status: StatusSuccess, summary: res.Summary, output: res.Output, wantsWrite: nr.Write}
		}
		if ctx.Err() != nil {
			return completion{id: nr.ID, status: StatusCancelled, err: "cancelled while running", wantsWrite: nr.Write}
		}
	}
	errMsg := res.Error
	if errMsg == "" {
		errMsg = "node failed: " + nr.ID
	}
	return completion{id: nr.ID, status: StatusFailed, summary: res.Summary, err: errMsg, output: res.Output, wantsWrite: nr.Write}
}

func finalizeStatus(run *RunResult, cancelled bool) {
	if run.Status != StatusRunning {
		return // already failed/stalled
	}
	anyFailed, anyCancelled, anyBlocked := false, false, false
	allDone := true
	for _, nr := range run.Nodes {
		switch nr.Status {
		case StatusFailed:
			anyFailed = true
		case StatusCancelled:
			anyCancelled = true
		case StatusBlocked:
			anyBlocked = true
		}
		if nr.Status != StatusSuccess {
			allDone = false
		}
	}
	switch {
	case allDone:
		run.Status = StatusSuccess
	case anyFailed || anyBlocked:
		run.Status = StatusFailed
	case cancelled || anyCancelled:
		run.Status = StatusCancelled
	default:
		run.Status = StatusFailed
	}
	if run.Error == "" && run.Status != StatusSuccess {
		for _, nr := range run.Nodes {
			if nr.Status == StatusFailed || nr.Status == StatusBlocked || nr.Status == StatusCancelled {
				if nr.Error != "" {
					run.Error = nr.Error
				} else {
					run.Error = "run did not complete successfully"
				}
				break
			}
		}
	}
}

// now is overridable in tests; production uses wall clock.
var now = func() time.Time { return time.Now() }
