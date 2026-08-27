package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// MUL-6704. #7571 stopped a reclaimed private runtime from EXECUTING other
// people's agents; these tests pin what happens to the state it leaves behind,
// and the two invariants that keep the teardown from doing collateral damage: the
// owner's own agents must be untouched, and the confirmed plan must still be the
// live one.

// TestSplitForeignAgents_Classification: which foreign agents get unbound, which
// keep their binding, and when the dialog must warn about Mika.
func TestSplitForeignAgents_Classification(t *testing.T) {
	other := mustUUID(t, "22222222-2222-4222-8222-222222222222")
	agents := []db.Agent{
		{ID: mustUUID(t, "aaaaaaaa-0000-4000-8000-000000000001"), Kind: "user", OwnerID: other, Name: "teammate agent"},
		{ID: mustUUID(t, "aaaaaaaa-0000-4000-8000-000000000002"), Kind: "user", OwnerID: other, Name: "archived",
			ArchivedAt: pgtype.Timestamptz{Valid: true}},
		{ID: mustUUID(t, "aaaaaaaa-0000-4000-8000-000000000003"), Kind: "system", OwnerID: other, Name: "builder carrier",
			SystemKey: pgtype.Text{String: "agent_builder:abc", Valid: true}},
		{ID: mustUUID(t, "aaaaaaaa-0000-4000-8000-000000000004"), Kind: "user", OwnerID: other, Name: "Mika",
			SystemKey: pgtype.Text{String: service.MikaSystemKey, Valid: true}},
	}

	plan, unboundIDs, retainedIDs := splitForeignAgents(agents)

	if len(plan.UnboundAgents) != 2 {
		t.Fatalf("confirmed set = %d agents, want the 2 ACTIVE user agents", len(plan.UnboundAgents))
	}
	if plan.ArchivedCount != 1 || plan.RetainedSystemCount != 1 {
		t.Fatalf("archived=%d retained=%d, want 1/1", plan.ArchivedCount, plan.RetainedSystemCount)
	}
	// Mika is kind='user', so she is unbound like anyone else — and there is one
	// per workspace, so the dialog must raise the workspace-wide warning.
	if !plan.MikaAffected {
		t.Fatalf("mika_affected = false for an agent carrying the mika system_key")
	}
	// The archived row is in the id lists too: leaving it bound to a machine that
	// refuses it is the stuck state this teardown removes.
	if len(unboundIDs) != 3 || len(retainedIDs) != 1 {
		t.Fatalf("unbound ids = %d, retained ids = %d; want 3/1", len(unboundIDs), len(retainedIDs))
	}
	if plan.empty() {
		t.Fatalf("plan.empty() = true for a non-empty plan")
	}
	if emptyPlan, u, r := splitForeignAgents(nil); !emptyPlan.empty() || len(u) != 0 || len(r) != 0 {
		t.Fatalf("owner-only runtime must produce an empty plan, got %+v", emptyPlan)
	}
}

// TestUpdateRuntimeVisibility_RefusesWithPlan: the plain PATCH must not tear
// anything down as a side effect of a field write.
func TestUpdateRuntimeVisibility_RefusesWithPlan(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID, foreignUserID := publicRuntimeWithForeignAgent(t, ctx, "Revoke Plan Runtime")
	foreignAgentID := dbfx.Agent(t, "Revoke Plan Foreign Agent", runtimeID, ownedBy(foreignUserID))

	w := patchVisibilityPrivate(t, runtimeID)
	if w.Code != http.StatusConflict {
		t.Fatalf("PATCH visibility=private: got %d, want 409: %s", w.Code, w.Body.String())
	}
	body := decodeRevokePlan(t, w.Body.Bytes())
	if body.Code != runtimeVisibilityHasForeignAgentsCode {
		t.Fatalf("code = %q, want %q", body.Code, runtimeVisibilityHasForeignAgentsCode)
	}
	if len(body.ActiveAgents) != 1 || body.ActiveAgents[0].ID != foreignAgentID {
		t.Fatalf("409 must carry the affected agent so the dialog needs no second round trip, got %+v", body.ActiveAgents)
	}
	if got := runtimeVisibility(t, runtimeID); got != "public" {
		t.Fatalf("visibility = %q after a refused PATCH, want unchanged 'public'", got)
	}
}

// TestUpdateRuntimeVisibility_PlanDisclosesOnlyIdAndName is the disclosure
// boundary. The machine owner here is a plain member who does not own — and
// cannot read — the private agent in the plan, yet merely ATTEMPTING to make
// their own runtime private renders it. Growing this body back to the full
// AgentResponse would hand out the teammate's instructions, runtime config, MCP
// servers and Composio allowlist.
func TestUpdateRuntimeVisibility_PlanDisclosesOnlyIdAndName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID, foreignUserID := publicRuntimeWithForeignAgent(t, ctx, "Revoke Disclosure Runtime")
	foreignAgentID := dbfx.Agent(t, "Revoke Disclosure Foreign Agent", runtimeID, testutil.Cols{
		"owner_id":                   foreignUserID,
		"visibility":                 "private",
		"instructions":               "SENTINEL_INSTRUCTIONS do not disclose",
		"mcp_config":                 testutil.Raw(`'{"servers":{"secret":{"command":"SENTINEL_MCP_COMMAND"}}}'::jsonb`),
		"runtime_config":             testutil.Raw(`'{"gateway":{"token":"SENTINEL_TOKEN"}}'::jsonb`),
		"composio_toolkit_allowlist": testutil.Raw(`ARRAY['sentinel_toolkit']::text[]`),
	})

	w := patchVisibilityPrivate(t, runtimeID)
	if w.Code != http.StatusConflict {
		t.Fatalf("PATCH visibility=private: got %d, want 409: %s", w.Code, w.Body.String())
	}

	raw := w.Body.String()
	// Field names as well as values: an empty or redacted value still means the
	// shape grew back, and the next config written to it would leak.
	for _, sentinel := range []string{
		"SENTINEL_INSTRUCTIONS", "SENTINEL_MCP_COMMAND", "SENTINEL_TOKEN", "sentinel_toolkit",
		"instructions", "mcp_config", "runtime_config", "composio_toolkit_allowlist",
	} {
		if strings.Contains(raw, sentinel) {
			t.Fatalf("409 plan discloses %q; it must carry only id and name.\nbody: %s", sentinel, raw)
		}
	}

	var parsed struct {
		ActiveAgents []map[string]any `json:"active_agents"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode 409 body: %v", err)
	}
	if len(parsed.ActiveAgents) != 1 {
		t.Fatalf("active_agents = %d, want 1", len(parsed.ActiveAgents))
	}
	entry := parsed.ActiveAgents[0]
	if len(entry) != 2 || entry["id"] != foreignAgentID || entry["name"] != "Revoke Disclosure Foreign Agent" {
		t.Fatalf("agent entry = %v, want exactly the affected agent's id + name", entry)
	}
}

// TestRevokeAndMakePrivate_TearsDownForeignState is the main path: one confirmed
// call leaves no half-revoked state — the foreign agent unbound, every
// non-terminal status settled with a reason, its task token revoked, its
// automation paused — while the owner's own agent and work continue untouched.
func TestRevokeAndMakePrivate_TearsDownForeignState(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID, foreignUserID := publicRuntimeWithForeignAgent(t, ctx, "Revoke Teardown Runtime")
	foreignAgentID := dbfx.Agent(t, "Revoke Teardown Foreign", runtimeID, ownedBy(foreignUserID))
	ownAgentID := dbfx.Agent(t, "Revoke Teardown Own", runtimeID)

	// Every non-terminal status, so a future status added to one query and missed
	// in the other is caught here rather than in production.
	foreignTasks := map[string]string{}
	for _, status := range []string{"queued", "dispatched", "running", "waiting_local_directory", "deferred"} {
		foreignTasks[status] = insertFixtureTask(t, ctx, runtimeID, foreignAgentID, status, false)
	}
	ownQueued := insertFixtureTask(t, ctx, runtimeID, ownAgentID, "queued", false)

	// A live task token for the running task: cancellation must revoke it, or a
	// process that keeps running would keep a usable credential.
	dbfx.Exec(t, `
		INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
		VALUES ('revoke-teardown-token', $1, $2, $3, $4, now() + interval '1 hour')
	`, foreignTasks["running"], foreignAgentID, testWorkspaceID, foreignUserID)

	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "revoke teardown autopilot",
		"assignee_type":   "agent",
		"assignee_id":     foreignAgentID,
		"status":          "active",
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   foreignUserID,
	})

	w := confirmRevoke(t, runtimeID, []string{foreignAgentID})
	if w.Code != http.StatusOK {
		t.Fatalf("RevokeAndMakePrivateRuntime: got %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := runtimeVisibility(t, runtimeID); got != "private" {
		t.Fatalf("visibility = %q, want 'private'", got)
	}

	if runtimeBound(t, foreignAgentID) {
		t.Fatalf("the foreign agent must be unbound")
	}
	// The regression this guards: UnbindUserAgentsFromRuntime has no owner filter,
	// so reusing it here would unbind the owner's own agents — reclaiming your
	// machine would break your own agents first.
	if !runtimeBound(t, ownAgentID) {
		t.Fatalf("the runtime owner's own agent must stay bound")
	}

	for status, taskID := range foreignTasks {
		gotStatus, reason, errText := taskOutcome(t, taskID)
		if gotStatus != "cancelled" {
			t.Fatalf("foreign %s task: status = %q, want cancelled", status, gotStatus)
		}
		if reason != string(taskfailure.ReasonAgentRuntimeRequired) {
			t.Fatalf("foreign %s task: failure_reason = %q, want %q", status, reason, taskfailure.ReasonAgentRuntimeRequired)
		}
		if errText == "" {
			t.Fatalf("foreign %s task has a machine reason but no sentence a user can read", status)
		}
	}
	if gotStatus, _, _ := taskOutcome(t, ownQueued); gotStatus != "queued" {
		t.Fatalf("the owner's own task = %q, want it left queued", gotStatus)
	}

	if n := dbfx.Count(t, `SELECT count(*) FROM task_token WHERE task_id = $1`, foreignTasks["running"]); n != 0 {
		t.Fatalf("task_token rows = %d after cancellation, want 0 (broadcast must revoke the token)", n)
	}

	var autopilotStatus string
	var pauseReason pgtype.Text
	dbfx.QueryRow(t, `SELECT status, pause_reason FROM autopilot WHERE id = $1`, autopilotID).Scan(&autopilotStatus, &pauseReason)
	if autopilotStatus != "paused" || pauseReason.String != "agent_runtime_required" {
		t.Fatalf("autopilot = (%q, %q), want (paused, agent_runtime_required): an active schedule would append one doomed run per tick",
			autopilotStatus, pauseReason.String)
	}
}

// TestRevokeAndMakePrivate_RefusesStalePlan: the set the user confirmed must be
// the set the server tears down, so a teammate binding another agent while the
// dialog is open forces a re-confirmation with zero writes in between.
func TestRevokeAndMakePrivate_RefusesStalePlan(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID, foreignUserID := publicRuntimeWithForeignAgent(t, ctx, "Revoke Stale Plan Runtime")
	firstAgentID := dbfx.Agent(t, "Revoke Stale First", runtimeID, ownedBy(foreignUserID))
	dbfx.Agent(t, "Revoke Stale Second", runtimeID, ownedBy(foreignUserID))

	w := confirmRevoke(t, runtimeID, []string{firstAgentID})
	if w.Code != http.StatusConflict {
		t.Fatalf("stale plan: got %d, want 409: %s", w.Code, w.Body.String())
	}
	body := decodeRevokePlan(t, w.Body.Bytes())
	if body.Code != runtimeVisibilityPlanChangedCode {
		t.Fatalf("code = %q, want %q", body.Code, runtimeVisibilityPlanChangedCode)
	}
	if len(body.ActiveAgents) != 2 {
		t.Fatalf("the refusal must carry the LATEST plan so the dialog can re-render it, got %d agents", len(body.ActiveAgents))
	}
	boundAgents := dbfx.Count(t, `SELECT count(*) FROM agent WHERE runtime_id = $1`, runtimeID)
	if got := runtimeVisibility(t, runtimeID); got != "public" || boundAgents != 2 {
		t.Fatalf("a refused confirmation must write nothing: visibility=%q boundAgents=%d", got, boundAgents)
	}
}

// TestRevokeAndMakePrivate_RetainsSystemCarrier: an Agent Builder carrier has no
// rebind affordance in the agent UI, so it keeps its binding (unbinding strands
// it, deleting it destroys the user's builder conversation). Its work is still
// cancelled, and admission refuses new work — see
// TestAgentReadinessRuntimeAccess.
func TestRevokeAndMakePrivate_RetainsSystemCarrier(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID, foreignUserID := publicRuntimeWithForeignAgent(t, ctx, "Revoke Carrier Runtime")
	carrierID := dbfx.Agent(t, "Revoke Carrier", runtimeID, testutil.Cols{
		"owner_id":   foreignUserID,
		"kind":       "system",
		"system_key": "agent_builder:revoke-test",
	})
	carrierTask := insertFixtureTask(t, ctx, runtimeID, carrierID, "queued", false)

	w := confirmRevoke(t, runtimeID, []string{}, 0, 1)
	if w.Code != http.StatusOK {
		t.Fatalf("RevokeAndMakePrivateRuntime: got %d, want 200: %s", w.Code, w.Body.String())
	}
	if !runtimeBound(t, carrierID) {
		t.Fatalf("a kind='system' carrier must keep its binding; unbound it is unrepairable")
	}
	status, reason, _ := taskOutcome(t, carrierTask)
	if status != "cancelled" || reason != string(taskfailure.ReasonRuntimeAccessRevoked) {
		t.Fatalf("carrier task = (%q, %q), want (cancelled, %s)", status, reason, taskfailure.ReasonRuntimeAccessRevoked)
	}
}

// --- helpers ---------------------------------------------------------------

// publicRuntimeWithForeignAgent creates a PUBLIC runtime owned by the test user
// plus a second member to own the "someone else's agent" side of these tests.
func publicRuntimeWithForeignAgent(t *testing.T, ctx context.Context, name string) (string, string) {
	t.Helper()
	runtimeID := dbfx.Runtime(t, name, testutil.Cols{"visibility": "public"})
	otherUserID := dbfx.User(t, name+" Teammate", "teammate-"+runtimeID+"@multica.ai")
	dbfx.Member(t, testWorkspaceID, otherUserID, "member")
	return runtimeID, otherUserID
}

// ownedBy is shorthand for "an agent owned by someone else".
func ownedBy(ownerID string) testutil.Cols {
	return testutil.Cols{"owner_id": ownerID}
}

func patchVisibilityPrivate(t *testing.T, runtimeID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/runtimes/"+runtimeID, map[string]any{"visibility": "private"})
	testHandler.UpdateAgentRuntime(w, withURLParam(req, "runtimeId", runtimeID))
	return w
}

// confirmRevoke submits a full confirmation: the named agents plus the archived
// and retained counts the dialog showed.
func confirmRevoke(t *testing.T, runtimeID string, expected []string, counts ...int) *httptest.ResponseRecorder {
	t.Helper()
	archived, retained := 0, 0
	if len(counts) > 0 {
		archived = counts[0]
	}
	if len(counts) > 1 {
		retained = counts[1]
	}
	return postRevoke(t, runtimeID, map[string]any{
		"expected_active_agent_ids":     expected,
		"expected_archived_agent_count": archived,
		"expected_retained_agent_count": retained,
	})
}

func postRevoke(t *testing.T, runtimeID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/revoke-and-make-private", body)
	testHandler.RevokeAndMakePrivateRuntime(w, withURLParam(req, "runtimeId", runtimeID))
	return w
}

type revokePlanBody struct {
	Code         string `json:"code"`
	MikaAffected bool   `json:"mika_affected"`
	ActiveAgents []struct {
		ID string `json:"id"`
	} `json:"active_agents"`
}

func decodeRevokePlan(t *testing.T, raw []byte) revokePlanBody {
	t.Helper()
	var body revokePlanBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode 409 body: %v", err)
	}
	return body
}

func runtimeVisibility(t *testing.T, runtimeID string) string {
	t.Helper()
	var visibility string
	dbfx.QueryRow(t, `SELECT visibility FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&visibility)
	return visibility
}

func runtimeBound(t *testing.T, agentID string) bool {
	t.Helper()
	var bound bool
	dbfx.QueryRow(t, `SELECT runtime_id IS NOT NULL FROM agent WHERE id = $1`, agentID).Scan(&bound)
	return bound
}

// taskOutcome returns status, failure_reason and error for one task row.
func taskOutcome(t *testing.T, taskID string) (string, string, string) {
	t.Helper()
	var status string
	var reason, errText pgtype.Text
	dbfx.QueryRow(t, `SELECT status, failure_reason, error FROM agent_task_queue WHERE id = $1`, taskID).
		Scan(&status, &reason, &errText)
	return status, reason.String, errText.String
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u := parseUUID(s)
	if !u.Valid {
		t.Fatalf("parse uuid %q", s)
	}
	return u
}

// TestRevokeAndMakePrivate_ConfirmsEveryDisplayedCategory: the dialog shows named
// agents AND two counts, so all three are part of the confirmation. Comparing only
// the ids let an archived agent, or a builder session moved in while the dialog was
// open, be torn down without the user ever seeing it — they would have approved a
// smaller impact than the one that ran.
func TestRevokeAndMakePrivate_ConfirmsEveryDisplayedCategory(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		// seed adds whatever appeared after the dialog was opened.
		seed func(t *testing.T, runtimeID, foreignUserID string)
	}{
		{
			name: "an archived agent appeared",
			seed: func(t *testing.T, runtimeID, foreignUserID string) {
				dbfx.Agent(t, "Revoke Coverage Archived "+runtimeID, runtimeID, testutil.Cols{
					"owner_id":    foreignUserID,
					"archived_at": testutil.Raw("now()"),
				})
			},
		},
		{
			name: "a builder carrier moved in",
			seed: func(t *testing.T, runtimeID, foreignUserID string) {
				dbfx.Agent(t, "Revoke Coverage Carrier "+runtimeID, runtimeID, testutil.Cols{
					"owner_id":   foreignUserID,
					"kind":       "system",
					"system_key": "agent_builder:coverage",
				})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtimeID, foreignUserID := publicRuntimeWithForeignAgent(t, ctx, "Revoke Coverage "+tc.name)
			activeAgentID := dbfx.Agent(t, "Revoke Coverage Active "+tc.name, runtimeID, ownedBy(foreignUserID))
			task := insertFixtureTask(t, ctx, runtimeID, activeAgentID, "queued", false)

			// The user's snapshot: one named agent, nothing in either count.
			tc.seed(t, runtimeID, foreignUserID)

			w := confirmRevoke(t, runtimeID, []string{activeAgentID})
			if w.Code != http.StatusConflict {
				t.Fatalf("got %d, want 409 — the active ids still match, so only the counts can catch this: %s",
					w.Code, w.Body.String())
			}
			if body := decodeRevokePlan(t, w.Body.Bytes()); body.Code != runtimeVisibilityPlanChangedCode {
				t.Fatalf("code = %q, want %q", body.Code, runtimeVisibilityPlanChangedCode)
			}
			// Zero writes: nothing unbound, nothing cancelled, still shared.
			if got := runtimeVisibility(t, runtimeID); got != "public" {
				t.Fatalf("visibility = %q; a refused confirmation must write nothing", got)
			}
			if !runtimeBound(t, activeAgentID) {
				t.Fatalf("the named agent must still be bound after a refused confirmation")
			}
			if status, _, _ := taskOutcome(t, task); status != "queued" {
				t.Fatalf("task = %q, want it untouched", status)
			}
		})
	}

	// And the same plan confirmed in full goes through.
	t.Run("confirming the counts too succeeds", func(t *testing.T) {
		runtimeID, foreignUserID := publicRuntimeWithForeignAgent(t, ctx, "Revoke Coverage Full")
		activeAgentID := dbfx.Agent(t, "Revoke Coverage Full Active", runtimeID, ownedBy(foreignUserID))
		dbfx.Agent(t, "Revoke Coverage Full Archived", runtimeID, testutil.Cols{
			"owner_id":    foreignUserID,
			"archived_at": testutil.Raw("now()"),
		})
		carrierID := dbfx.Agent(t, "Revoke Coverage Full Carrier", runtimeID, testutil.Cols{
			"owner_id":   foreignUserID,
			"kind":       "system",
			"system_key": "agent_builder:coverage-full",
		})

		w := confirmRevoke(t, runtimeID, []string{activeAgentID}, 1, 1)
		if w.Code != http.StatusOK {
			t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
		}
		if runtimeBound(t, activeAgentID) {
			t.Fatalf("the named agent must be unbound")
		}
		if !runtimeBound(t, carrierID) {
			t.Fatalf("the carrier keeps its binding")
		}
	})
}
