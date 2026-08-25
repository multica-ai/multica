package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
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

// TestIssueTaskTokensInterpolatesAgentAndTask pins the actor-claim path: a
// template can name the acting agent and task next to the accountable human,
// so a receiving system's audit row can tell delegation from a direct login.
func TestIssueTaskTokensInterpolatesAgentAndTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, `[
	  {"id":"erp","label":"ERP","env":"BOT_TOKEN_ERP","claims":{
	    "sub":"{{identity.email}}",
	    "act_sub":"{{agent.id}}",
	    "act_name":"{{agent.name}}",
	    "task_id":"{{task.id}}"
	  }}
	]`)
	agentID := dbfx.Agent(t, "issue-act-claims", handlerTestRuntimeID(t), testutil.Cols{
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
	got := testHandler.issueTaskTokens(context.Background(), &task, agent, testWorkspaceID)
	claims := decodeJWTPayload(t, got["BOT_TOKEN_ERP"])

	if claims["act_sub"] != agentID {
		t.Errorf("act_sub = %v, want the acting agent id %s", claims["act_sub"], agentID)
	}
	if claims["act_name"] != "issue-act-claims" {
		t.Errorf("act_name = %v, want issue-act-claims", claims["act_name"])
	}
	if claims["task_id"] != taskID {
		t.Errorf("task_id = %v, want %s", claims["task_id"], taskID)
	}
}

// TestIssueTaskTokensWritesAuditRow is the fence for in-product auditability:
// a credential minted in someone's name must be visible in activity_log, not
// only in server logs — and if the audit write fails, no token is handed out.
func TestIssueTaskTokensWritesAuditRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)
	issueID := dbfx.Issue(t, "task-token-audit")
	agentID := dbfx.Agent(t, "issue-audit", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id":             testUserID,
		"task_token_templates": `["erp"]`,
	})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":          handlerTestRuntimeID(t),
		"issue_id":            issueID,
		"originator_source":   "direct_human",
		"accountable_user_id": testUserID,
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM activity_log WHERE action = 'agent_task_tokens_issued' AND issue_id = $1`, parseUUID(issueID))
	})

	task := loadTaskRow(t, taskID)
	agent := loadAgentRow(t, agentID)
	got := testHandler.issueTaskTokens(context.Background(), &task, agent, testWorkspaceID)
	if got["BOT_TOKEN_ERP"] == "" {
		t.Fatalf("issueTaskTokens() = %v, want a BOT_TOKEN_ERP token", got)
	}

	var actorType, actorID, details string
	err := testPool.QueryRow(context.Background(), `
		SELECT actor_type, actor_id::text, details::text FROM activity_log
		WHERE action = 'agent_task_tokens_issued' AND issue_id = $1`, parseUUID(issueID)).
		Scan(&actorType, &actorID, &details)
	if err != nil {
		t.Fatalf("expected one agent_task_tokens_issued activity row: %v", err)
	}
	if actorType != "agent" || actorID != agentID {
		t.Errorf("actor = %s/%s, want agent/%s", actorType, actorID, agentID)
	}
	var parsed struct {
		TaskID string `json:"task_id"`
		UserID string `json:"user_id"`
		Source string `json:"identity_source"`
		Issued []struct {
			TemplateID string `json:"template_id"`
			Env        string `json:"env"`
			JTI        string `json:"jti"`
		} `json:"issued"`
	}
	if err := json.Unmarshal([]byte(details), &parsed); err != nil {
		t.Fatalf("details is not JSON: %v (%s)", err, details)
	}
	if parsed.TaskID != taskID || parsed.UserID != testUserID || parsed.Source != "direct_human" {
		t.Errorf("details = %s, want task_id=%s user_id=%s source=direct_human", details, taskID, testUserID)
	}
	if len(parsed.Issued) != 1 || parsed.Issued[0].TemplateID != "erp" || parsed.Issued[0].Env != "BOT_TOKEN_ERP" {
		t.Fatalf("details.issued = %s, want one erp entry", details)
	}
	if jti := decodeJWTPayload(t, got["BOT_TOKEN_ERP"])["jti"]; parsed.Issued[0].JTI != jti {
		t.Errorf("audit jti = %q, want the token's jti %v", parsed.Issued[0].JTI, jti)
	}
}

// decodeJWTPayload reads a JWT's claims without verifying — these tests assert
// claim contents, not signatures, which the tasktoken package already covers.
func decodeJWTPayload(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	return claims
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
