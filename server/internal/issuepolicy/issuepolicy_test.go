package issuepolicy

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestLegacyStateUsesPhaseOutcomeWithoutCollapsingPolicies(t *testing.T) {
	tests := []struct {
		category string
		phase    string
		outcome  string
		terminal bool
		active   bool
	}{
		{"backlog", "unstarted", "", false, false},
		{"todo", "unstarted", "", false, false},
		{"in_progress", "started", "", false, true},
		{"in_review", "started", "", false, false},
		{"blocked", "started", "", false, false},
		{"done", "done", "completed", true, false},
		{"cancelled", "closed", "cancelled", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			state := catalogState(context.Background(), nil, pgtype.UUID{}, tt.category)
			if state.Phase != tt.phase || state.Outcome != tt.outcome {
				t.Fatalf("state = %#v, want phase=%q outcome=%q", state, tt.phase, tt.outcome)
			}
			if state.IsTerminal() != tt.terminal {
				t.Fatalf("IsTerminal = %v, want %v", state.IsTerminal(), tt.terminal)
			}
			if state.AgentOwnsActiveWork() != tt.active {
				t.Fatalf("AgentOwnsActiveWork = %v, want %v", state.AgentOwnsActiveWork(), tt.active)
			}
		})
	}
}

// Custom categories carry terminal semantics, but must not acquire the built-in
// backlog parking, review completion or blocked failure policies.
func TestCustomStatusCategoryPolicies(t *testing.T) {
	for _, tc := range []struct {
		category, resolution string
		terminal             bool
	}{
		{"unstarted", AutopilotNone, false},
		{"started", AutopilotNone, false},
		{"done", AutopilotComplete, true},
		{"closed", AutopilotFail, true},
	} {
		t.Run(tc.category, func(t *testing.T) {
			q := policyQueries{category: tc.category}
			state := ResolveStatus(context.Background(), q, pgtype.UUID{}, pgtype.UUID{}, "custom_gate", false)
			if state.Phase != tc.category || state.IsParked() || state.AgentOwnsActiveWork() ||
				state.IsTerminal() != tc.terminal || state.AutopilotResolution() != tc.resolution ||
				state.AllowsRunTrigger() == tc.terminal {
				t.Fatalf("custom %s policy = %#v", tc.category, state)
			}
		})
	}
}

func TestWorkflowNativeStatusUsesPinnedCategory(t *testing.T) {
	q := policyQueries{node: db.IssueWorkflowStatus{Phase: "done", Outcome: pgtype.Text{String: "completed", Valid: true}}}
	issue := db.Issue{Status: "done", WorkflowID: pgtype.UUID{Valid: true}, WorkflowStatusID: pgtype.UUID{Valid: true}}
	state := ResolveIssue(context.Background(), q, issue, true)
	if state.Phase != "done" || !state.IsTerminal() || state.AutopilotResolution() != AutopilotComplete {
		t.Fatalf("pinned native status = %#v", state)
	}
	// A stale pin must not override a newer status written by an older client.
	issue.Status = "todo"
	state = ResolveIssue(context.Background(), q, issue, true)
	if state.Phase != "unstarted" || state.IsTerminal() {
		t.Fatalf("stale pin = %#v", state)
	}
}

type policyQueries struct {
	Querier
	category string
	node     db.IssueWorkflowStatus
}

func (q policyQueries) GetIssueStatusEntryByKey(context.Context, db.GetIssueStatusEntryByKeyParams) (db.IssueStatus, error) {
	return db.IssueStatus{Category: q.category}, nil
}
func (q policyQueries) GetIssueWorkflowStatusByID(context.Context, db.GetIssueWorkflowStatusByIDParams) (db.IssueWorkflowStatus, error) {
	return q.node, nil
}
