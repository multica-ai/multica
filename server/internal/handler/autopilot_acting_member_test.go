package handler

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Every write on the autopilot surface is judged as the human it acts FOR — the
// caller themselves for a member, the run's ORIGINATOR for an agent — never as
// the runtime owner whose task token authenticated the request (MUL-7108).
//
// The escalation these tests exist for: a task token carries its runtime's
// OWNER as X-User-ID (daemon.go mints it from rt.OwnerID), so judging the
// authenticated user made every agent a standing proxy for its machine's owner.
// A member with no grant could @ an agent running on an admin's machine and have
// it pause the autopilot, re-point a webhook, or grant themselves a
// collaborator row — none of which they could do in the UI.
//
// TriggerAutopilot's half of this rule landed first (#8099) and keeps its own
// tests in autopilot_manual_trigger_invoker_test.go, including the code-level
// assertions on its distinct refusal codes.

// actingCaller is who a request comes from on the wire. agentID == "" is a
// member calling the API directly; otherwise it is the CLI speaking from inside
// a task, authenticated as authUserID (the runtime owner) but stamped by the
// auth middleware as a task-token actor.
type actingCaller struct {
	authUserID string
	agentID    string
	taskID     string
}

func (c actingCaller) request(method, path string, body any) *http.Request {
	r := newRequestAs(c.authUserID, method, path, body)
	if c.agentID != "" {
		r.Header.Set("X-Actor-Source", "task_token")
		r.Header.Set("X-Agent-ID", c.agentID)
		r.Header.Set("X-Task-ID", c.taskID)
	}
	return r
}

// actingFixture is one autopilot owned outright by orderingHuman — a plain
// member who is its creator and therefore its only non-admin writer — with the
// trigger rows the per-trigger endpoints need.
type actingFixture struct {
	autopilotID       string
	agentID           string
	orderingHuman     string
	scheduleTriggerID string
	webhookTriggerID  string
	grantTarget       string
}

func newActingFixture(t *testing.T, label string) actingFixture {
	t.Helper()

	var agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)

	orderingHuman := plainMember(t, "ordering-human")
	title := fmt.Sprintf("acting member %s %d", label, time.Now().UnixNano())
	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":         testWorkspaceID,
		"title":                title,
		"assignee_id":          agentID,
		"assignee_type":        "agent",
		"execution_mode":       "create_issue",
		"issue_title_template": title,
		"status":               "active",
		"created_by_type":      "member",
		"created_by_id":        orderingHuman,
	})
	// Rows the handlers themselves write, which dbfx cannot register: cleanup
	// runs in reverse registration order, so these go before the autopilot.
	dbfx.Cleanup(t, `DELETE FROM autopilot_rule_version WHERE autopilot_id = $1`, autopilotID)
	dbfx.Cleanup(t, `DELETE FROM autopilot_collaborator WHERE autopilot_id = $1`, autopilotID)
	dbfx.Cleanup(t, `DELETE FROM autopilot_trigger WHERE autopilot_id = $1`, autopilotID)

	trigger := func(kind string, over testutil.Cols) string {
		cols := testutil.Cols{
			"autopilot_id":      autopilotID,
			"kind":              kind,
			"enabled":           true,
			"created_by_type":   "member",
			"created_by_id":     orderingHuman,
			"published_by_type": "member",
			"published_by_id":   orderingHuman,
		}
		for k, v := range over {
			cols[k] = v
		}
		return dbfx.Insert(t, "autopilot_trigger", cols)
	}

	return actingFixture{
		autopilotID:   autopilotID,
		agentID:       agentID,
		orderingHuman: orderingHuman,
		scheduleTriggerID: trigger("schedule", testutil.Cols{
			"cron_expression": "0 9 * * *",
			"timezone":        "UTC",
		}),
		webhookTriggerID: trigger("webhook", testutil.Cols{
			"provider":      "generic",
			"webhook_token": fmt.Sprintf("tok_%d", time.Now().UnixNano()),
		}),
		grantTarget: plainMember(t, "grant-target"),
	}
}

// autopilotWriteEndpoint is one mutating endpoint behind requireAutopilotWrite
// or requireAutopilotAccessManagement. wantAdmitted is what the endpoint answers
// once the gate lets the request through — the assertion is on the gate, so a
// non-403 that proves admission is enough where the success path costs more
// setup than it is worth.
type autopilotWriteEndpoint struct {
	name         string
	wantAdmitted int
	send         func(fx actingFixture, c actingCaller) *http.Request
	handler      func(w http.ResponseWriter, r *http.Request)
}

func autopilotWriteEndpoints() []autopilotWriteEndpoint {
	ws := "?workspace_id=" + testWorkspaceID
	return []autopilotWriteEndpoint{
		{
			name:         "update-autopilot",
			wantAdmitted: http.StatusOK,
			handler:      testHandler.UpdateAutopilot,
			send: func(fx actingFixture, c actingCaller) *http.Request {
				return withURLParams(c.request("PATCH", "/api/autopilots/"+fx.autopilotID+ws,
					map[string]any{"status": "paused"}), "id", fx.autopilotID)
			},
		},
		{
			name:         "delete-autopilot",
			wantAdmitted: http.StatusNoContent,
			handler:      testHandler.DeleteAutopilot,
			send: func(fx actingFixture, c actingCaller) *http.Request {
				return withURLParams(c.request("DELETE", "/api/autopilots/"+fx.autopilotID+ws, nil),
					"id", fx.autopilotID)
			},
		},
		{
			name: "replay-delivery",
			// Admission lands on "delivery not found" for a delivery id that
			// never existed: the gate ran, and building a real delivery row
			// would test the replay path rather than the gate.
			wantAdmitted: http.StatusNotFound,
			handler:      testHandler.ReplayAutopilotDelivery,
			send: func(fx actingFixture, c actingCaller) *http.Request {
				deliveryID := uuid.NewString()
				return withURLParams(c.request("POST",
					"/api/autopilots/"+fx.autopilotID+"/deliveries/"+deliveryID+"/replay"+ws, nil),
					"id", fx.autopilotID, "deliveryId", deliveryID)
			},
		},
		{
			name:         "create-trigger",
			wantAdmitted: http.StatusCreated,
			handler:      testHandler.CreateAutopilotTrigger,
			send: func(fx actingFixture, c actingCaller) *http.Request {
				return withURLParams(c.request("POST", "/api/autopilots/"+fx.autopilotID+"/triggers"+ws,
					map[string]any{"kind": "schedule", "cron_expression": "0 10 * * *", "timezone": "UTC"}),
					"id", fx.autopilotID)
			},
		},
		{
			name:         "update-trigger",
			wantAdmitted: http.StatusOK,
			handler:      testHandler.UpdateAutopilotTrigger,
			send: func(fx actingFixture, c actingCaller) *http.Request {
				return withURLParams(c.request("PATCH",
					"/api/autopilots/"+fx.autopilotID+"/triggers/"+fx.scheduleTriggerID+ws,
					map[string]any{"enabled": false}),
					"id", fx.autopilotID, "triggerId", fx.scheduleTriggerID)
			},
		},
		{
			name:         "delete-trigger",
			wantAdmitted: http.StatusNoContent,
			handler:      testHandler.DeleteAutopilotTrigger,
			send: func(fx actingFixture, c actingCaller) *http.Request {
				return withURLParams(c.request("DELETE",
					"/api/autopilots/"+fx.autopilotID+"/triggers/"+fx.scheduleTriggerID+ws, nil),
					"id", fx.autopilotID, "triggerId", fx.scheduleTriggerID)
			},
		},
		{
			name:         "rotate-webhook-token",
			wantAdmitted: http.StatusOK,
			handler:      testHandler.RotateAutopilotTriggerWebhookToken,
			send: func(fx actingFixture, c actingCaller) *http.Request {
				return withURLParams(c.request("POST",
					"/api/autopilots/"+fx.autopilotID+"/triggers/"+fx.webhookTriggerID+"/rotate-token"+ws, nil),
					"id", fx.autopilotID, "triggerId", fx.webhookTriggerID)
			},
		},
		{
			name:         "set-signing-secret",
			wantAdmitted: http.StatusOK,
			handler:      testHandler.SetAutopilotTriggerSigningSecret,
			send: func(fx actingFixture, c actingCaller) *http.Request {
				return withURLParams(c.request("PUT",
					"/api/autopilots/"+fx.autopilotID+"/triggers/"+fx.webhookTriggerID+"/signing-secret"+ws,
					map[string]any{"signing_secret": "0123456789abcdef"}),
					"id", fx.autopilotID, "triggerId", fx.webhookTriggerID)
			},
		},
		{
			name:         "add-collaborator",
			wantAdmitted: http.StatusCreated,
			handler:      testHandler.AddAutopilotCollaborator,
			send: func(fx actingFixture, c actingCaller) *http.Request {
				return withURLParams(c.request("POST", "/api/autopilots/"+fx.autopilotID+"/collaborators"+ws,
					map[string]any{"user_id": fx.grantTarget}), "id", fx.autopilotID)
			},
		},
		{
			name:         "remove-collaborator",
			wantAdmitted: http.StatusOK,
			handler:      testHandler.RemoveAutopilotCollaborator,
			send: func(fx actingFixture, c actingCaller) *http.Request {
				return withURLParams(c.request("DELETE",
					"/api/autopilots/"+fx.autopilotID+"/collaborators/"+fx.grantTarget+ws, nil),
					"id", fx.autopilotID, "userId", fx.grantTarget)
			},
		},
	}
}

// TestAutopilotWrites_JudgeTheActingHuman runs every write endpoint through the
// four cases that define the rule. The two agent-caller cases are deliberately
// cross-identity: the ordering human and the runtime owner hold OPPOSITE
// permissions in each, so neither outcome can be explained by the request's
// authenticated user.
func TestAutopilotWrites_JudgeTheActingHuman(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	for _, ep := range autopilotWriteEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			t.Run("ordering human holds the grant", func(t *testing.T) {
				fx := newActingFixture(t, ep.name+" admitted")
				// The machine belongs to a plain member with no grant on this
				// autopilot, so only the ordering human's authority can carry
				// this request.
				caller := actingCaller{
					authUserID: plainMember(t, "machine-owner"),
					agentID:    fx.agentID,
					taskID:     callerTask(t, fx.agentID, fx.orderingHuman),
				}
				testutil.Call(t, ep.handler, ep.send(fx, caller)).Want(ep.wantAdmitted)
			})

			t.Run("ordering human holds nothing", func(t *testing.T) {
				fx := newActingFixture(t, ep.name+" refused")
				// Authenticated as the workspace owner, who DOES hold write
				// here — so a pass could only come from judging the token's
				// bound user, which is the escalation itself.
				caller := actingCaller{
					authUserID: testUserID,
					agentID:    fx.agentID,
					taskID:     callerTask(t, fx.agentID, plainMember(t, "outsider")),
				}
				var body triggerErrorBody
				testutil.Call(t, ep.handler, ep.send(fx, caller)).Want(http.StatusForbidden).JSON(&body)
				if body.Code != autopilotForbiddenCode {
					t.Errorf("error code = %q, want %q", body.Code, autopilotForbiddenCode)
				}
			})

			t.Run("no ordering human at all", func(t *testing.T) {
				fx := newActingFixture(t, ep.name+" unattributed")
				caller := actingCaller{
					authUserID: testUserID,
					agentID:    fx.agentID,
					taskID:     callerTask(t, fx.agentID, ""),
				}
				var body triggerErrorBody
				testutil.Call(t, ep.handler, ep.send(fx, caller)).Want(http.StatusForbidden).JSON(&body)
				// Its own code: nothing was denied, there was nobody to judge —
				// the one refusal here a workspace can act on.
				if body.Code != autopilotNoOriginatorCode {
					t.Errorf("error code = %q, want %q", body.Code, autopilotNoOriginatorCode)
				}
			})

			t.Run("member calling directly", func(t *testing.T) {
				// The half that was never broken, and must stay unbroken: a
				// member is judged as themselves, with no originator lookup in
				// the way.
				fx := newActingFixture(t, ep.name+" member")
				testutil.Call(t, ep.handler, ep.send(fx, actingCaller{authUserID: testUserID})).Want(ep.wantAdmitted)
			})
		})
	}
}

// TestCreateAutopilot_StampsTheOrderingHuman covers the create endpoint, which
// has no autopilot to check a grant against but stamps the most important
// identity on the surface: created_by IS the autopilot's write grant.
//
// Stamping the runtime owner would both hand that owner a grant nobody gave
// them and lock the requesting member out of the autopilot they just asked for —
// the gate would then refuse them every later edit, because they are neither
// creator nor collaborator. The gate and the stamp have to name one person.
func TestCreateAutopilot_StampsTheOrderingHuman(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	var agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)
	orderingHuman := plainMember(t, "creating-human")
	title := fmt.Sprintf("acting member create %d", time.Now().UnixNano())

	// Authenticated as the workspace owner; ordered by a plain member.
	caller := actingCaller{
		authUserID: testUserID,
		agentID:    agentID,
		taskID:     callerTask(t, agentID, orderingHuman),
	}
	var ap AutopilotResponse
	testutil.Call(t, testHandler.CreateAutopilot, caller.request("POST", "/api/autopilots?workspace_id="+testWorkspaceID,
		map[string]any{
			"title":                title,
			"assignee_id":          agentID,
			"execution_mode":       "create_issue",
			"issue_title_template": title,
		})).Want(http.StatusCreated).JSON(&ap)
	dbfx.Cleanup(t, `DELETE FROM autopilot_rule_version WHERE autopilot_id = $1`, ap.ID)
	dbfx.Cleanup(t, `DELETE FROM autopilot WHERE id = $1`, ap.ID)

	var createdBy string
	dbfx.QueryRow(t, `SELECT created_by_id FROM autopilot WHERE id = $1`, ap.ID).Scan(&createdBy)
	if createdBy != orderingHuman {
		t.Fatalf("created_by_id = %s, want the ordering human %s (runtime owner is %s)", createdBy, orderingHuman, testUserID)
	}
	var publishedBy string
	dbfx.QueryRow(t, `SELECT published_by_id FROM autopilot_rule_version WHERE autopilot_id = $1 ORDER BY created_at ASC LIMIT 1`, ap.ID).Scan(&publishedBy)
	if publishedBy != orderingHuman {
		t.Errorf("rule version published_by_id = %s, want the ordering human %s", publishedBy, orderingHuman)
	}

	// The whole point of stamping the same human the gate judges: the member who
	// asked for this autopilot can go on editing it through the agent.
	editCaller := actingCaller{
		authUserID: plainMember(t, "another-machine-owner"),
		agentID:    agentID,
		taskID:     callerTask(t, agentID, orderingHuman),
	}
	testutil.Call(t, testHandler.UpdateAutopilot, withURLParams(
		editCaller.request("PATCH", "/api/autopilots/"+ap.ID+"?workspace_id="+testWorkspaceID,
			map[string]any{"status": "paused"}), "id", ap.ID)).Want(http.StatusOK)
}

// TestCreateAutopilot_NoOrderingHumanRefused: an autopilot created with nobody
// behind it would have a NULL-equivalent creator and no accountable human at
// dispatch time, which is exactly the state MUL-4302 §3.4 exists to prevent.
func TestCreateAutopilot_NoOrderingHumanRefused(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	var agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)
	title := fmt.Sprintf("acting member create unattributed %d", time.Now().UnixNano())
	caller := actingCaller{
		authUserID: testUserID,
		agentID:    agentID,
		taskID:     callerTask(t, agentID, ""),
	}

	var body triggerErrorBody
	testutil.Call(t, testHandler.CreateAutopilot, caller.request("POST", "/api/autopilots?workspace_id="+testWorkspaceID,
		map[string]any{
			"title":          title,
			"assignee_id":    agentID,
			"execution_mode": "create_issue",
		})).Want(http.StatusForbidden).JSON(&body)
	if body.Code != autopilotNoOriginatorCode {
		t.Errorf("error code = %q, want %q", body.Code, autopilotNoOriginatorCode)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM autopilot WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title); n != 0 {
		t.Errorf("autopilot rows = %d, want 0 — the refusal must land before the insert", n)
	}
}

// TestCreateAutopilot_OrderingHumanMustBeAWorkspaceMember covers create's own
// refusal. It is a different fact from the other endpoints' "you hold no grant
// on this autopilot" — there is no autopilot yet — so it carries its own code:
// telling someone to ask for a collaborator grant would send them after
// something that could not help (MUL-7108).
func TestCreateAutopilot_OrderingHumanMustBeAWorkspaceMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	var agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)
	// A user of the platform, but not of THIS workspace — the shape an
	// originator has after leaving it.
	stranger := dbfx.User(t, "Autopilot Stranger", fmt.Sprintf("autopilot-stranger-%d@multica.test", time.Now().UnixNano()))
	title := fmt.Sprintf("acting member create stranger %d", time.Now().UnixNano())

	caller := actingCaller{
		authUserID: testUserID,
		agentID:    agentID,
		taskID:     callerTask(t, agentID, stranger),
	}
	var body triggerErrorBody
	testutil.Call(t, testHandler.CreateAutopilot, caller.request("POST", "/api/autopilots?workspace_id="+testWorkspaceID,
		map[string]any{
			"title":          title,
			"assignee_id":    agentID,
			"execution_mode": "create_issue",
		})).Want(http.StatusForbidden).JSON(&body)
	if body.Code != autopilotActorNotMemberCode {
		t.Errorf("error code = %q, want %q", body.Code, autopilotActorNotMemberCode)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM autopilot WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title); n != 0 {
		t.Errorf("autopilot rows = %d, want 0", n)
	}
}

// TestCreateAutopilotTrigger_StampsTheOrderingHuman pins the durable half of the
// escalation. A trigger's created_by is the immutable authorization principal
// every future firing acts as (MUL-6951, "the run acts as this member forever"),
// so a trigger stamped with the runtime owner is not a one-off unauthorized
// edit — it is a standing execution right in that owner's name, minted by
// someone who holds nothing, and outliving the run that asked for it.
func TestCreateAutopilotTrigger_StampsTheOrderingHuman(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newActingFixture(t, "trigger stamp")
	caller := actingCaller{
		authUserID: testUserID, // workspace owner: holds write, but is not who asked
		agentID:    fx.agentID,
		taskID:     callerTask(t, fx.agentID, fx.orderingHuman),
	}
	var trigger AutopilotTriggerResponse
	testutil.Call(t, testHandler.CreateAutopilotTrigger, withURLParams(
		caller.request("POST", "/api/autopilots/"+fx.autopilotID+"/triggers?workspace_id="+testWorkspaceID,
			map[string]any{"kind": "schedule", "cron_expression": "0 11 * * *", "timezone": "UTC"}),
		"id", fx.autopilotID)).Want(http.StatusCreated).JSON(&trigger)

	var createdBy, publishedBy string
	dbfx.QueryRow(t, `SELECT created_by_id, published_by_id FROM autopilot_trigger WHERE id = $1`, trigger.ID).
		Scan(&createdBy, &publishedBy)
	if createdBy != fx.orderingHuman {
		t.Fatalf("trigger created_by_id = %s, want the ordering human %s — every future firing acts as this member",
			createdBy, fx.orderingHuman)
	}
	if publishedBy != fx.orderingHuman {
		t.Errorf("trigger published_by_id = %s, want the ordering human %s", publishedBy, fx.orderingHuman)
	}
}

// TestAddAutopilotCollaborator_GrantedByIsTheOrderingHuman: a grant is the
// longest-lived write here — it shows up in the access UI and survives the run
// that made it — so its audit column must name the human who authorized it.
func TestAddAutopilotCollaborator_GrantedByIsTheOrderingHuman(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newActingFixture(t, "grant stamp")
	caller := actingCaller{
		authUserID: plainMember(t, "machine-owner"),
		agentID:    fx.agentID,
		taskID:     callerTask(t, fx.agentID, fx.orderingHuman),
	}
	testutil.Call(t, testHandler.AddAutopilotCollaborator, withURLParams(
		caller.request("POST", "/api/autopilots/"+fx.autopilotID+"/collaborators?workspace_id="+testWorkspaceID,
			map[string]any{"user_id": fx.grantTarget}), "id", fx.autopilotID)).Want(http.StatusCreated)

	var grantedBy string
	dbfx.QueryRow(t, `SELECT granted_by FROM autopilot_collaborator WHERE autopilot_id = $1 AND user_id = $2`,
		fx.autopilotID, fx.grantTarget).Scan(&grantedBy)
	if grantedBy != fx.orderingHuman {
		t.Errorf("granted_by = %s, want the ordering human %s", grantedBy, fx.orderingHuman)
	}
}

// TestGetAutopilot_WebhookTokenFollowsTheActingHuman closes the door the write
// gates leave open on their own. A webhook token IS a trigger — the built-in
// skill says so in as many words — and it is handed out on the READ path, gated
// by the same can_write predicate. Judging the authenticated user there would
// let an agent read its runtime owner's tokens and fire the autopilot from
// outside the permission system entirely, which no write gate would ever see.
func TestGetAutopilot_WebhookTokenFollowsTheActingHuman(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newActingFixture(t, "token exposure")
	get := func(c actingCaller) (bool, bool) {
		t.Helper()
		var got struct {
			Autopilot AutopilotResponse          `json:"autopilot"`
			Triggers  []AutopilotTriggerResponse `json:"triggers"`
		}
		testutil.Call(t, testHandler.GetAutopilot, withURLParams(
			c.request("GET", "/api/autopilots/"+fx.autopilotID+"?workspace_id="+testWorkspaceID, nil),
			"id", fx.autopilotID)).Want(http.StatusOK).JSON(&got)
		tokenVisible := false
		for _, tr := range got.Triggers {
			if tr.WebhookToken != nil {
				tokenVisible = true
			}
		}
		return got.Autopilot.CanWrite != nil && *got.Autopilot.CanWrite, tokenVisible
	}

	// Authenticated as the workspace owner, who may read these tokens; ordered
	// by a member who may not.
	canWrite, tokenVisible := get(actingCaller{
		authUserID: testUserID,
		agentID:    fx.agentID,
		taskID:     callerTask(t, fx.agentID, plainMember(t, "token-outsider")),
	})
	if canWrite || tokenVisible {
		t.Errorf("can_write = %v, webhook token visible = %v; want both false — the token would be a trigger the write gates never see",
			canWrite, tokenVisible)
	}

	// The ordering human owns this autopilot, so the same request is a writer's
	// read and the token comes back.
	canWrite, tokenVisible = get(actingCaller{
		authUserID: plainMember(t, "machine-owner"),
		agentID:    fx.agentID,
		taskID:     callerTask(t, fx.agentID, fx.orderingHuman),
	})
	if !canWrite || !tokenVisible {
		t.Errorf("can_write = %v, webhook token visible = %v; want both true for the autopilot's own creator", canWrite, tokenVisible)
	}

	// A member reading directly is judged as themselves, unchanged.
	if canWrite, _ := get(actingCaller{authUserID: testUserID}); !canWrite {
		t.Error("can_write = false for the workspace owner reading directly; the member path must be unchanged")
	}
}
