package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestIssueTaskTokensBySource is the regression fence for the issuing gate.
//
// Every row sets accountable_user_id — the audit column resolves a human for
// almost every run — so what the table really pins is the other half: whether
// originator_user_id, the authorization column, carries a human. A run that
// nobody authorized must not mint a credential in anyone's name, however
// precise its audit label is.
func TestIssueTaskTokensBySource(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)

	cases := []struct {
		name string
		// source is the audit label stamped on the run.
		source string
		// authorized sets originator_user_id: a human lent their authority.
		authorized bool
		wantToken  bool
	}{
		// A member's own action, or an agent acting under one. originator and
		// accountable name the same person (migration 185's invariant).
		{"direct_human", "direct_human", true, true},
		{"delegation", "delegation", true, true},
		{"comment_source", "comment_source", true, true},
		// Autopilot fires on its own. accountable_user_id still names whoever
		// armed the trigger — that is what the activity UI shows — but
		// originator_user_id is NULL by construction, so nothing may speak in
		// their name. Signing here would hand an unattended 3am run that
		// person's full permissions in the receiving system.
		{"trigger_owner", "trigger_owner", false, false},
		{"rule_owner", "rule_owner", false, false},
		// Degraded audit sources never carried authorization to begin with.
		{"owner_fallback", "owner_fallback", false, false},
		{"backfill", "backfill", false, false},
		{"unattributed", "unattributed", false, false},
		{"empty_source", "", false, false},
		// The fence proper, from both sides: a precise label plus a named
		// accountable human is still not authorization, and a source that only
		// names its human in hindsight does not mint a live credential either.
		{"precise_label_without_authorization", "direct_human", false, false},
		{"backfilled_after_the_fact", "backfill", true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentID := dbfx.Agent(t, "issue-src-"+tc.name, handlerTestRuntimeID(t), testutil.Cols{
				"owner_id":             testUserID,
				"task_token_templates": `["erp"]`,
			})
			cols := testutil.Cols{
				"runtime_id":          handlerTestRuntimeID(t),
				"originator_source":   tc.source,
				"accountable_user_id": testUserID,
			}
			if tc.authorized {
				cols["originator_user_id"] = testUserID
			}
			taskID := dbfx.Task(t, agentID, cols)

			task := loadTaskRow(t, taskID)
			agent := loadAgentRow(t, agentID)

			got := testHandler.issueTaskTokens(context.Background(), &task, agent, testWorkspaceID)
			if tc.wantToken {
				if len(got) != 1 || got["BOT_TOKEN_ERP"] == "" {
					t.Fatalf("issueTaskTokens() = %v, want a BOT_TOKEN_ERP token for an authorized %q run", got, tc.source)
				}
			} else if len(got) != 0 {
				t.Fatalf("issueTaskTokens() = %v, want none for an unauthorized %q run", got, tc.source)
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
		"originator_user_id":  testUserID,
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
		"originator_user_id":  testUserID,
		"accountable_user_id": testUserID,
	})

	task := loadTaskRow(t, taskID)
	agent := loadAgentRow(t, agentID)
	if got := testHandler.issueTaskTokens(context.Background(), &task, agent, testWorkspaceID); len(got) != 0 {
		t.Errorf("issueTaskTokens() = %v, want none when no issuer is configured", got)
	}
}

func TestIssueTaskTokensSkipsWhenAuthorizingUserMissing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)
	agentID := dbfx.Agent(t, "issue-no-user", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id":             testUserID,
		"task_token_templates": `["erp"]`,
	})
	// Precise source but NULL originator: must degrade, not panic.
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":          handlerTestRuntimeID(t),
		"originator_source":   "direct_human",
		"originator_user_id":  nil,
		"accountable_user_id": nil,
	})

	task := loadTaskRow(t, taskID)
	agent := loadAgentRow(t, agentID)
	if got := testHandler.issueTaskTokens(context.Background(), &task, agent, testWorkspaceID); len(got) != 0 {
		t.Errorf("issueTaskTokens() = %v, want none when originator_user_id is NULL", got)
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
		"originator_user_id":  testUserID,
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
		"originator_user_id":  testUserID,
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

// TestClaimTaskByRuntimeGatesTaskTokensOnDaemonCapability is the mixed-version
// fence. Issuing writes an activity row stating a credential was minted in a
// named person's name, while an older daemon json-skips the response field and
// runs the task with no token at all. So the capability is checked BEFORE
// signing: a daemon that cannot inject the tokens must not cause an audit row
// claiming it received them.
func TestClaimTaskByRuntimeGatesTaskTokensOnDaemonCapability(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)

	cases := []struct {
		name         string
		capabilities string
		wantToken    bool
	}{
		{"injects-task-tokens", protocol.DaemonCapabilitySkillBundlesV1 + "," + protocol.DaemonCapabilityTaskIdentityTokensV1, true},
		{"older-daemon-other-capabilities", protocol.DaemonCapabilitySkillBundlesV1, false},
		{"advertises-nothing", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A runtime of its own: claim takes the queue's next task for the
			// runtime, and these cases must not race each other for it.
			runtimeID := dbfx.Runtime(t, "task-token-caps-"+tc.name, testutil.Cols{
				"device_info": "task token capability fixture",
			})
			agentID := dbfx.Agent(t, "task-token-caps-"+tc.name, runtimeID, testutil.Cols{
				"owner_id":             testUserID,
				"task_token_templates": `["erp"]`,
			})
			issueID := dbfx.Issue(t, "task-token-caps-"+tc.name)
			dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id":          runtimeID,
				"issue_id":            issueID,
				"originator_source":   "direct_human",
				"originator_user_id":  testUserID,
				"accountable_user_id": testUserID,
			})
			t.Cleanup(func() {
				testPool.Exec(context.Background(),
					`DELETE FROM activity_log WHERE action = 'agent_task_tokens_issued' AND issue_id = $1`, parseUUID(issueID))
			})

			req := newDaemonTokenRequest(http.MethodPost,
				"/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "task-token-caps")
			if tc.capabilities != "" {
				req.Header.Set("X-Client-Capabilities", tc.capabilities)
			}
			req = withURLParams(req, "runtimeId", runtimeID)

			var out struct {
				Task *AgentTaskResponse `json:"task"`
			}
			testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&out)
			if out.Task == nil {
				t.Fatal("claim returned no task")
			}

			if tc.wantToken {
				if out.Task.TaskTokens["BOT_TOKEN_ERP"] == "" {
					t.Errorf("task_tokens = %v, want a BOT_TOKEN_ERP token", out.Task.TaskTokens)
				}
			} else if len(out.Task.TaskTokens) != 0 {
				t.Errorf("task_tokens = %v, want none for a daemon that cannot inject them", out.Task.TaskTokens)
			}

			var audited int
			dbfx.QueryRow(t,
				`SELECT count(*) FROM activity_log WHERE action = 'agent_task_tokens_issued' AND issue_id = $1`,
				issueID).Scan(&audited)
			want := 0
			if tc.wantToken {
				want = 1
			}
			if audited != want {
				t.Errorf("agent_task_tokens_issued rows = %d, want %d — the audit trail must match what was actually issued", audited, want)
			}
		})
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
