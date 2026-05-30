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

import "sort"

// ChildState is one sub-issue of an orchestrated parent.
type ChildState struct {
	ID     string // canonical issue UUID string
	Number int32  // human issue number, for summary rendering
	Title  string
	Status string
}

// BlockerState is one issue that blocks a child (the upstream side of a
// `blocks` edge). A blocker may be another child or any other issue; either
// way the child only becomes ready once every blocker is terminal.
type BlockerState struct {
	ID     string
	Status string
}

// terminalStatuses are the issue statuses that count as "blocker satisfied".
// A blocker that is done or cancelled no longer holds back its dependents.
func IsTerminalStatus(status string) bool {
	return status == "done" || status == "cancelled"
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
		if !IsTerminalStatus(b.Status) {
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
