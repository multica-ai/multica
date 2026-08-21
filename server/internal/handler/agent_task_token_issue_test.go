package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestIssueTaskTokensBySource is the regression fence for the issuing gate.
// Signing on a non-precise source would hand the agent owner's identity to a
// run nobody authorized, so every source is pinned here explicitly rather
// than through a helper that could drift with attribution.Source.
func TestIssueTaskTokensBySource(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)

	cases := []struct {
		source    string
		wantToken bool
	}{
		{"direct_human", true},
		{"delegation", true},
		{"comment_source", true},
		{"trigger_owner", true},
		{"rule_owner", true},
		{"owner_fallback", false},
		{"backfill", false},
		{"unattributed", false},
		{"", false},
	}

	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			agentID := dbfx.Agent(t, "issue-src-"+tc.source, handlerTestRuntimeID(t), testutil.Cols{
				"owner_id":             testUserID,
				"task_token_templates": `["erp"]`,
			})
			taskID := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id":          handlerTestRuntimeID(t),
				"originator_source":   tc.source,
				"accountable_user_id": testUserID,
			})

			task := loadTaskRow(t, taskID)
			agent := loadAgentRow(t, agentID)

			got := testHandler.issueTaskTokens(context.Background(), &task, agent, testWorkspaceID)
			if tc.wantToken {
				if len(got) != 1 || got["BOT_TOKEN_ERP"] == "" {
					t.Fatalf("issueTaskTokens() = %v, want a BOT_TOKEN_ERP token for precise source %q", got, tc.source)
				}
			} else if len(got) != 0 {
				t.Fatalf("issueTaskTokens() = %v, want none for non-precise source %q", got, tc.source)
			}
		})
	}
}

func TestIssueTaskTokensSkipsWhenNoTemplatesEnabled(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)
	agentID := dbfx.Agent(t, "issue-none", handlerTestRuntimeID(t), testutil.Cols{"owner_id": testUserID})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":          handlerTestRuntimeID(t),
		"originator_source":   "direct_human",
		"accountable_user_id": testUserID,
	})

	task := loadTaskRow(t, taskID)
	agent := loadAgentRow(t, agentID)
	if got := testHandler.issueTaskTokens(context.Background(), &task, agent, testWorkspaceID); len(got) != 0 {
		t.Errorf("issueTaskTokens() = %v, want none when the agent enables no templates", got)
	}
}

func TestIssueTaskTokensSkipsWhenUnconfigured(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, "")
	agentID := dbfx.Agent(t, "issue-unconfigured", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id":             testUserID,
		"task_token_templates": `["erp"]`,
	})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":          handlerTestRuntimeID(t),
		"originator_source":   "direct_human",
		"accountable_user_id": testUserID,
	})

	task := loadTaskRow(t, taskID)
	agent := loadAgentRow(t, agentID)
	if got := testHandler.issueTaskTokens(context.Background(), &task, agent, testWorkspaceID); len(got) != 0 {
		t.Errorf("issueTaskTokens() = %v, want none when no issuer is configured", got)
	}
}

func TestIssueTaskTokensSkipsWhenAccountableUserMissing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)
	agentID := dbfx.Agent(t, "issue-no-user", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id":             testUserID,
		"task_token_templates": `["erp"]`,
	})
	// Precise source but NULL accountable user: must degrade, not panic.
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":          handlerTestRuntimeID(t),
		"originator_source":   "direct_human",
		"accountable_user_id": nil,
	})

	task := loadTaskRow(t, taskID)
	agent := loadAgentRow(t, agentID)
	if got := testHandler.issueTaskTokens(context.Background(), &task, agent, testWorkspaceID); len(got) != 0 {
		t.Errorf("issueTaskTokens() = %v, want none when accountable_user_id is NULL", got)
	}
}

func loadTaskRow(t *testing.T, id string) db.AgentTaskQueue {
	t.Helper()
	row, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(id))
	if err != nil {
		t.Fatalf("GetAgentTask(%s) error = %v", id, err)
	}
	return row
}

func loadAgentRow(t *testing.T, id string) db.Agent {
	t.Helper()
	row, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(id))
	if err != nil {
		t.Fatalf("GetAgent(%s) error = %v", id, err)
	}
	return row
}
