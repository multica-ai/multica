// CEREBRO-PATCH(cerebro-orchestration): FIR-2564 cerebro-only package.
//
// Package orchestration is the cerebro "orchestration layer": a small,
// dependency-aware engine for running a plan of tasks (agents/commands) where
// some tasks wait on others. It computes which tasks may run concurrently
// ("waves"), refuses impossible plans (cycles, missing/self dependencies),
// runs ready tasks up to a concurrency limit while letting only one *writing*
// task mutate state at a time, blocks downstream work when a dependency fails,
// and retries failed tasks up to a per-node limit.
//
// The pattern is ported from the open-source codexmate task-orchestrator; we
// extracted the algorithm, not the product. The engine is execution-agnostic:
// callers inject an ExecuteFunc that knows how to actually run a node (run an
// agent, promote a sub-issue, call a command). That keeps the scheduling core
// testable in isolation and reusable from the CLI and the server.
package orchestration

import (
	"regexp"
	"sort"
	"strings"
)

// Node is one task in a plan.
type Node struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Kind       string   `json:"kind"`              // "agent" or "command"
	Prompt     string   `json:"prompt,omitempty"`  // for kind=agent
	Command    string   `json:"command,omitempty"` // for kind=command
	DependsOn  []string `json:"dependsOn,omitempty"`
	Write      bool     `json:"write,omitempty"`      // mutates shared state -> exclusive
	RetryLimit int      `json:"retryLimit,omitempty"` // extra attempts on failure (0-5)
}

// Plan is a complete orchestration plan.
type Plan struct {
	Title       string `json:"title,omitempty"`
	Concurrency int    `json:"concurrency,omitempty"` // max parallel nodes (1-8, default 2)
	Nodes       []Node `json:"nodes"`
}

// Wave is a set of node IDs that may run concurrently (all their dependencies
// are satisfied by earlier waves).
type Wave struct {
	Index   int      `json:"index"`
	Label   string   `json:"label"`
	NodeIDs []string `json:"nodeIds"`
}

// Issue is a single validation problem found in a plan.
type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	defaultConcurrency = 2
	minConcurrency     = 1
	maxConcurrency     = 8
	maxRetryLimit      = 5
)

var idCleanup = regexp.MustCompile(`[^a-z0-9._-]+`)
var idDashes = regexp.MustCompile(`-{2,}`)

// NormalizeID lower-cases and slugifies an id so "Build API" and "build-api"
// refer to the same node. Returns "" for empty/garbage input.
func NormalizeID(value string) string {
	text := strings.ToLower(strings.TrimSpace(value))
	text = idCleanup.ReplaceAllString(text, "-")
	text = idDashes.ReplaceAllString(text, "-")
	text = strings.Trim(text, "-")
	return text
}

func normalizeDeps(deps []string) []string {
	out := make([]string, 0, len(deps))
	seen := map[string]bool{}
	for _, d := range deps {
		id := NormalizeID(d)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func clampInt(v, def, min, max int) int {
	if v == 0 {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// ComputeWaves groups nodes into execution waves via Kahn's topological sort.
// Every node in wave N depends only on nodes in waves < N, so all members of a
// wave can run concurrently. Dependencies that point at unknown nodes are
// ignored here (ValidatePlan is responsible for rejecting them). If a cycle
// makes some nodes unreachable, the leftover nodes are emitted as a final wave
// so callers can see what is stuck — but ValidatePlan rejects cycles up front.
func ComputeWaves(nodes []Node) []Wave {
	byID := map[string]bool{}
	indegree := map[string]int{}
	outgoing := map[string][]string{}
	order := make([]string, 0, len(nodes))

	for _, n := range nodes {
		id := NormalizeID(n.ID)
		if id == "" || byID[id] {
			continue
		}
		byID[id] = true
		indegree[id] = 0
		order = append(order, id)
	}
	for _, n := range nodes {
		id := NormalizeID(n.ID)
		if id == "" {
			continue
		}
		for _, dep := range normalizeDeps(n.DependsOn) {
			if !byID[dep] {
				continue
			}
			indegree[id]++
			outgoing[dep] = append(outgoing[dep], id)
		}
	}

	remaining := map[string]bool{}
	for id := range byID {
		remaining[id] = true
	}

	waves := []Wave{}
	for len(remaining) > 0 {
		ready := []string{}
		for _, id := range order { // stable iteration order
			if remaining[id] && indegree[id] == 0 {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			// Cycle / unreachable remainder — surface what's left.
			leftover := []string{}
			for _, id := range order {
				if remaining[id] {
					leftover = append(leftover, id)
				}
			}
			sort.Strings(leftover)
			waves = append(waves, Wave{NodeIDs: leftover})
			break
		}
		sort.Strings(ready)
		waves = append(waves, Wave{NodeIDs: ready})
		for _, id := range ready {
			delete(remaining, id)
			for _, next := range outgoing[id] {
				if indegree[next] > 0 {
					indegree[next]--
				}
			}
		}
	}

	for i := range waves {
		waves[i].Index = i
		waves[i].Label = "Wave " + itoa(i+1)
	}
	return waves
}

// DetectCycle reports whether the dependency graph contains a cycle
// (e.g. A depends on B and B depends on A). DFS with a visiting/visited set.
func DetectCycle(nodes []Node) bool {
	graph := map[string][]string{}
	for _, n := range nodes {
		id := NormalizeID(n.ID)
		if id == "" {
			continue
		}
		graph[id] = normalizeDeps(n.DependsOn)
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(id string) bool
	visit = func(id string) bool {
		if visited[id] {
			return false
		}
		if visiting[id] {
			return true // back edge -> cycle
		}
		visiting[id] = true
		for _, dep := range graph[id] {
			if _, ok := graph[dep]; !ok {
				continue
			}
			if visit(dep) {
				return true
			}
		}
		delete(visiting, id)
		visited[id] = true
		return false
	}
	for id := range graph {
		if visit(id) {
			return true
		}
	}
	return false
}

// ValidatePlan checks a plan is runnable: at least one node, unique non-empty
// ids, a known kind with the matching payload, every dependency resolvable, no
// self-dependency, and no cycle. It returns every problem it finds (dependency
// and cycle checks run only once the shape is sound, so messages stay useful).
func ValidatePlan(plan Plan) []Issue {
	issues := []Issue{}
	if len(plan.Nodes) == 0 {
		return append(issues, Issue{"plan-nodes-required", "plan must contain at least one node"})
	}

	ids := map[string]bool{}
	for _, n := range plan.Nodes {
		id := NormalizeID(n.ID)
		if id == "" {
			issues = append(issues, Issue{"node-id-required", "every node needs a non-empty id"})
			continue
		}
		if ids[id] {
			issues = append(issues, Issue{"node-id-duplicate", "duplicate node id: " + id})
			continue
		}
		ids[id] = true

		kind := NormalizeID(n.Kind)
		switch kind {
		case "agent":
			if strings.TrimSpace(n.Prompt) == "" {
				issues = append(issues, Issue{"node-prompt-required", "agent node " + id + " needs a prompt"})
			}
		case "command":
			if strings.TrimSpace(n.Command) == "" {
				issues = append(issues, Issue{"node-command-required", "command node " + id + " needs a command"})
			}
		default:
			issues = append(issues, Issue{"node-kind-invalid", "node " + id + " has unsupported kind: " + n.Kind})
		}
	}

	if len(issues) == 0 {
		for _, n := range plan.Nodes {
			id := NormalizeID(n.ID)
			for _, dep := range normalizeDeps(n.DependsOn) {
				if dep == id {
					issues = append(issues, Issue{"node-dependency-self", "node " + id + " cannot depend on itself"})
				}
				if !ids[dep] {
					issues = append(issues, Issue{"node-dependency-missing", "node " + id + " depends on missing node: " + dep})
				}
			}
		}
	}

	if len(issues) == 0 && DetectCycle(plan.Nodes) {
		issues = append(issues, Issue{"plan-cycle-detected", "plan contains a dependency cycle"})
	}
	return issues
}

// itoa is a tiny dependency-free int->string for labels.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
