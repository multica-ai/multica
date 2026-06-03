// CEREBRO-PATCH(cerebro-orchestration): FIR-2564 cerebro-only package.
//
// subissue.go maps a Multica parent issue's sub-issues onto the orchestration
// engine. The sub-issues ARE the tasks; their `blocks` dependency edges ARE the
// "waits on" arrows. This is the data model the server-side label trigger drives:
// label a parent `orchestrate`, and the engine reads its children + their blocker
// edges, computes waves, and (in the handler seam) promotes each child the moment
// all of its blockers are terminal. These functions are pure so the wave/ready
// logic is unit-tested without a database.
package orchestration

import (
	"sort"
	"strings"
)

// HasAcceptanceCriteria reports whether a sub-issue description carries
// acceptance criteria the verifier can judge against: at least one markdown
// task-list item (`- [ ]` / `- [x]`, with `*` or `+` bullets and any indent
// accepted). The orchestrate plan-adequacy precheck refuses to run a plan whose
// sub-issues lack criteria — without them, "verify the deliverable" has nothing
// to check, so the verification gate would be meaningless.
func HasAcceptanceCriteria(description string) bool {
	for _, line := range strings.Split(description, "\n") {
		t := strings.TrimSpace(line)
		if len(t) < 5 {
			continue
		}
		if t[0] != '-' && t[0] != '*' && t[0] != '+' {
			continue
		}
		rest := strings.TrimSpace(t[1:])
		// rest must start with "[ ]", "[x]" or "[X]" followed by content.
		if strings.HasPrefix(rest, "[ ]") || strings.HasPrefix(rest, "[x]") || strings.HasPrefix(rest, "[X]") {
			return true
		}
	}
	return false
}

// ChildState is one sub-issue of an orchestrated parent.
type ChildState struct {
	ID     string // canonical issue UUID string
	Number int32  // human issue number, for summary rendering
	Title  string
	Status string
}

// BlockerState is one issue that blocks a child (the upstream side of a
// `blocks` edge). A blocker may be another child or any other issue; either
// way the child only becomes ready once every blocker is satisfied.
//
// Verification gate (FIR-2564): a blocker that is itself an orchestrated child
// of the same parent is only "satisfied" by a `done` status once it has ALSO
// been independently verified (RequiresVerification + Verified). This is the
// trust-but-verify core — the orchestrator never releases a sub-issue's
// dependents on the worker's own `done` alone; an independent judge must
// confirm the deliverable first. A `cancelled` blocker is always satisfied
// (nothing delivered, nothing to verify); a non-child blocker keeps the old
// behavior (any terminal status satisfies it).
type BlockerState struct {
	ID     string
	Status string
	// RequiresVerification is true when this blocker is an orchestrated child
	// of the same parent and the verification gate is active. Such a blocker
	// must be verified (not merely done) before it stops holding back
	// dependents.
	RequiresVerification bool
	// Verified is true when this blocker carries the `orch-verified` label,
	// i.e. an independent verifier confirmed its deliverable.
	Verified bool
}

// terminalStatuses are the issue statuses that count as "blocker satisfied".
// A blocker that is done or cancelled no longer holds back its dependents.
func IsTerminalStatus(status string) bool {
	return status == "done" || status == "cancelled"
}

// BlockerSatisfied reports whether a blocker no longer holds back its
// dependents. A cancelled blocker is always satisfied. A done blocker is
// satisfied unless it is verification-gated and not yet verified. Any other
// status (todo, in_progress, in_review, backlog) never satisfies.
func BlockerSatisfied(b BlockerState) bool {
	switch b.Status {
	case "cancelled":
		return true
	case "done":
		if b.RequiresVerification {
			return b.Verified
		}
		return true
	default:
		return false
	}
}

// startableStatuses are the statuses a child can be promoted/enqueued from.
// A child already in_progress / in_review / done / cancelled is left alone —
// orchestration never restarts work that has begun or finished.
func IsStartableStatus(status string) bool {
	return status == "backlog" || status == "todo"
}

// PlanFromChildren builds an orchestration Plan from a parent's children and
// their blocker edges. Only blocker edges that point at another child are kept
// as plan dependencies — cross-tree blockers still gate readiness at runtime
// but are not part of this parent's wave graph (the engine validates a closed
// graph). Every node is kind=agent with a placeholder prompt; the server seam,
// not the engine, performs the real promotion.
func PlanFromChildren(parentTitle string, children []ChildState, blockersByChild map[string][]BlockerState) Plan {
	childIDs := map[string]bool{}
	for _, c := range children {
		childIDs[NormalizeID(c.ID)] = true
	}

	nodes := make([]Node, 0, len(children))
	for _, c := range children {
		id := NormalizeID(c.ID)
		deps := []string{}
		seen := map[string]bool{}
		for _, b := range blockersByChild[c.ID] {
			bid := NormalizeID(b.ID)
			if bid == "" || bid == id || seen[bid] || !childIDs[bid] {
				continue
			}
			seen[bid] = true
			deps = append(deps, bid)
		}
		sort.Strings(deps)
		nodes = append(nodes, Node{
			ID:        id,
			Title:     c.Title,
			Kind:      "agent",
			Prompt:    "promote sub-issue",
			DependsOn: deps,
		})
	}
	return Plan{Title: parentTitle, Nodes: nodes}
}

// ReadyToStart returns the IDs of children that may be started right now: the
// child is in a startable status and every one of its blockers is terminal.
// Blockers outside the children set are honored — a child blocked by an
// unrelated open issue is NOT ready. Result is ordered by issue number for
// stable, human-readable output.
func ReadyToStart(children []ChildState, blockersByChild map[string][]BlockerState) []ChildState {
	ready := []ChildState{}
	for _, c := range children {
		if !IsStartableStatus(c.Status) {
			continue
		}
		if !allBlockersTerminal(blockersByChild[c.ID]) {
			continue
		}
		ready = append(ready, c)
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].Number < ready[j].Number })
	return ready
}

func allBlockersTerminal(blockers []BlockerState) bool {
	for _, b := range blockers {
		if !BlockerSatisfied(b) {
			return false
		}
	}
	return true
}

// RenderWaves turns a Plan's computed waves into a slice of human-readable
// lines like "Wave 1: #12, #13" using each child's issue number. Nodes whose
// id is unknown (should not happen for a child-derived plan) fall back to the
// raw id. Used to build the summary comment the trigger posts on the parent.
func RenderWaves(children []ChildState, plan Plan) []string {
	numberByID := map[string]int32{}
	for _, c := range children {
		numberByID[NormalizeID(c.ID)] = c.Number
	}
	waves := ComputeWaves(plan.Nodes)
	lines := make([]string, 0, len(waves))
	for _, w := range waves {
		refs := make([]string, 0, len(w.NodeIDs))
		for _, id := range w.NodeIDs {
			if n, ok := numberByID[id]; ok {
				refs = append(refs, "#"+itoa(int(n)))
			} else {
				refs = append(refs, id)
			}
		}
		lines = append(lines, w.Label+": "+joinComma(refs))
	}
	return lines
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
