package service

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// distinctAgentIDs drives how many times CancelTasksForIssue and
// CancelTasksByTriggerComment call ReconcileAgentStatus: one call per returned
// id. These tests guard that multiple cancelled rows sharing an agent collapse
// to a single reconcile, while distinct agents each still get one (D#3319).

const (
	dedupAgentA = "11111111-1111-1111-1111-111111111111"
	dedupAgentB = "22222222-2222-2222-2222-222222222222"
	dedupAgentC = "33333333-3333-3333-3333-333333333333"
)

func cancelledRowsForAgents(agentIDs ...string) []db.AgentTaskQueue {
	rows := make([]db.AgentTaskQueue, 0, len(agentIDs))
	for _, id := range agentIDs {
		rows = append(rows, db.AgentTaskQueue{AgentID: util.MustParseUUID(id)})
	}
	return rows
}

func TestDistinctAgentIDs_SameAgentReconciledOnce(t *testing.T) {
	// Three cancelled tasks, all owned by the same agent, must reconcile once.
	rows := cancelledRowsForAgents(dedupAgentA, dedupAgentA, dedupAgentA)

	got := distinctAgentIDs(rows)

	if len(got) != 1 {
		t.Fatalf("expected 1 distinct agent (single reconcile), got %d", len(got))
	}
	if got[0] != util.MustParseUUID(dedupAgentA) {
		t.Fatalf("expected agent %s, got %v", dedupAgentA, got[0])
	}
}

func TestDistinctAgentIDs_MultipleAgentsEachOnce(t *testing.T) {
	// Interleaved rows across three agents: each distinct agent once, in
	// first-seen order, regardless of how many rows each contributes.
	rows := cancelledRowsForAgents(
		dedupAgentA, dedupAgentB, dedupAgentA, dedupAgentC, dedupAgentB, dedupAgentA,
	)

	got := distinctAgentIDs(rows)

	want := []string{dedupAgentA, dedupAgentB, dedupAgentC}
	if len(got) != len(want) {
		t.Fatalf("expected %d distinct agents, got %d", len(want), len(got))
	}
	for i, id := range want {
		if got[i] != util.MustParseUUID(id) {
			t.Fatalf("distinct agent[%d]: expected %s, got %v", i, id, got[i])
		}
	}
}

func TestDistinctAgentIDs_Empty(t *testing.T) {
	if got := distinctAgentIDs(nil); len(got) != 0 {
		t.Fatalf("expected no agents for empty input, got %d", len(got))
	}
}
