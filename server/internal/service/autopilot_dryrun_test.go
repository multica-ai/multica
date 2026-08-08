package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestPayloadJSON pins the omit-empty contract for DispatchPlanTrigger.Payload:
// an empty payload must serialize to a nil (omitted) field so a manual preview
// does not echo a spurious null, while a real webhook payload round-trips
// verbatim as raw JSON.
func TestPayloadJSON(t *testing.T) {
	if got := payloadJSON(nil); got != nil {
		t.Fatalf("payloadJSON(nil) = %v, want nil", got)
	}
	if got := payloadJSON([]byte{}); got != nil {
		t.Fatalf("payloadJSON(empty) = %v, want nil", got)
	}

	raw := []byte(`{"event":"github.pull_request.opened"}`)
	got := payloadJSON(raw)
	if string(got) != string(raw) {
		t.Fatalf("payloadJSON = %q, want %q", got, raw)
	}
	// Must marshal as raw JSON, not a base64 []byte.
	out, err := json.Marshal(struct {
		Payload json.RawMessage `json:"payload,omitempty"`
	}{Payload: got})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"event":"github.pull_request.opened"`) {
		t.Fatalf("payload did not marshal as raw JSON: %s", out)
	}
}

// TestDispatchPlanAgent maps the resolved agent row to its plan representation,
// including archived detection (ArchivedAt.Valid) and the squad-resolved flag.
func TestDispatchPlanAgent(t *testing.T) {
	id := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4}, Valid: true}
	rt := pgtype.UUID{Bytes: [16]byte{5, 6, 7, 8}, Valid: true}

	t.Run("active agent with runtime", func(t *testing.T) {
		a := db.Agent{ID: id, Name: "worker", RuntimeID: rt}
		got := dispatchPlanAgent(a, false)
		if got.ID != util.UUIDToString(id) || got.Name != "worker" {
			t.Fatalf("unexpected agent mapping: %+v", got)
		}
		if got.RuntimeID != util.UUIDToString(rt) {
			t.Fatalf("runtime id = %q, want %q", got.RuntimeID, util.UUIDToString(rt))
		}
		if got.Archived {
			t.Fatalf("active agent must not report archived")
		}
		if got.SquadResolved {
			t.Fatalf("direct assignee must not report squad_resolved")
		}
	})

	t.Run("archived agent", func(t *testing.T) {
		a := db.Agent{ID: id, Name: "retired", ArchivedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}}
		got := dispatchPlanAgent(a, false)
		if !got.Archived {
			t.Fatalf("archived agent must report archived=true")
		}
	})

	t.Run("squad-resolved leader", func(t *testing.T) {
		a := db.Agent{ID: id, Name: "leader"}
		got := dispatchPlanAgent(a, true)
		if !got.SquadResolved {
			t.Fatalf("squad leader must report squad_resolved=true")
		}
	})

	t.Run("no runtime bound", func(t *testing.T) {
		a := db.Agent{ID: id, Name: "runtimeless"}
		got := dispatchPlanAgent(a, false)
		if got.RuntimeID != "" {
			t.Fatalf("absent runtime must serialize empty, got %q", got.RuntimeID)
		}
	})
}

// TestRenderDispatchPlanOutput_CreateIssue verifies the create_issue preview
// renders the interpolated title and full description from a synthetic run,
// without any DB access (dry-run passes an invalid triggerID so the timezone
// resolver short-circuits to UTC).
func TestRenderDispatchPlanOutput_CreateIssue(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		Description:        pgtype.Text{String: "watch the dashboard", Valid: true},
		IssueTitleTemplate: pgtype.Text{String: "nightly report {{date}}", Valid: true},
		ExecutionMode:      "create_issue",
	}
	plan := &DispatchPlan{}
	s.renderDispatchPlanOutput(nil, ap, pgtype.UUID{}, "schedule", nil, plan)

	if plan.TaskPrompt != "" {
		t.Fatalf("create_issue must not populate task_prompt: %q", plan.TaskPrompt)
	}
	if !strings.HasPrefix(plan.IssueTitle, "nightly report ") {
		t.Fatalf("issue title not rendered from template: %q", plan.IssueTitle)
	}
	if !strings.Contains(plan.IssueDescription, "watch the dashboard") {
		t.Fatalf("description must preserve autopilot description: %q", plan.IssueDescription)
	}
	if !strings.Contains(plan.IssueDescription, "Autopilot run triggered at") {
		t.Fatalf("description must include the run system note: %q", plan.IssueDescription)
	}
}

// TestRenderDispatchPlanOutput_RunOnly verifies the run_only preview surfaces
// the autopilot description as the task prompt the agent would receive.
func TestRenderDispatchPlanOutput_RunOnly(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		Description:   pgtype.Text{String: "summarize today's PRs", Valid: true},
		ExecutionMode: "run_only",
	}
	plan := &DispatchPlan{}
	s.renderDispatchPlanOutput(nil, ap, pgtype.UUID{}, "manual", nil, plan)

	if plan.TaskPrompt != "summarize today's PRs" {
		t.Fatalf("run_only task_prompt = %q, want the autopilot description", plan.TaskPrompt)
	}
	if plan.IssueTitle != "" || plan.IssueDescription != "" {
		t.Fatalf("run_only must not populate issue fields: title=%q desc=%q", plan.IssueTitle, plan.IssueDescription)
	}
}

// TestRenderDispatchPlanOutput_WebhookPayload verifies the webhook-source
// preview renders the webhook event block in the issue description, matching
// what a real webhook-triggered create_issue dispatch would produce.
func TestRenderDispatchPlanOutput_WebhookPayload(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		Description:   pgtype.Text{String: "triage PRs", Valid: true},
		ExecutionMode: "create_issue",
	}
	payload := []byte(`{"event":"github.pull_request.opened","eventPayload":{"number":42}}`)
	plan := &DispatchPlan{}
	s.renderDispatchPlanOutput(nil, ap, pgtype.UUID{}, "webhook", payload, plan)

	if !strings.Contains(plan.IssueDescription, "Webhook event: github.pull_request.opened") {
		t.Fatalf("webhook preview must include the event line: %q", plan.IssueDescription)
	}
	if !strings.Contains(plan.IssueDescription, "42") {
		t.Fatalf("webhook preview must include the payload: %q", plan.IssueDescription)
	}
}

// TestRenderDispatchPlanOutput_UnknownModeLeavesFieldsEmpty is a defensive
// guard: a future execution_mode that bypasses the CHECK constraint must not
// crash the preview - it renders no output and the plan still serializes.
func TestRenderDispatchPlanOutput_UnknownModeLeavesFieldsEmpty(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{ExecutionMode: "future_mode"}
	plan := &DispatchPlan{}
	s.renderDispatchPlanOutput(nil, ap, pgtype.UUID{}, "manual", nil, plan)

	if plan.IssueTitle != "" || plan.IssueDescription != "" || plan.TaskPrompt != "" {
		t.Fatalf("unknown mode must leave output fields empty: %+v", plan)
	}
}
