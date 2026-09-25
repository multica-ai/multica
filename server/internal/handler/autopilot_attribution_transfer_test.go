package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// These tests drive the REAL autopilot update handlers to prove the substantive /
// cosmetic edit boundary that decides when trigger_owner responsibility transfers
// (MUL-4302; Elon must-fix). They deliberately go through UpdateAutopilot /
// UpdateAutopilotTrigger rather than calling SetAutopilotTriggerPublisher* directly,
// because the bug Elon flagged lived in the handler's decision of WHEN to call the
// setter, not in the setter itself. testHandler / testUserID / testWorkspaceID /
// testPool are wired in TestMain (handler_test.go).

// seedTransferMember inserts a workspace member distinct from testUserID, modeling the
// CREATOR of an automation whose responsibility a later editor (testUserID) inherits.
func seedTransferMember(t *testing.T, label string) string {
	t.Helper()
	ctx := context.Background()
	var uid string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		label, fmt.Sprintf("%s-%d@multica.test", label, time.Now().UnixNano())).Scan(&uid); err != nil {
		t.Fatalf("seed %s user: %v", label, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uid) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, uid); err != nil {
		t.Fatalf("seed %s member: %v", label, err)
	}
	return uid
}

// seedScheduleTriggerPublishedBy inserts a schedule trigger whose responsible publisher
// is memberID, so a later handler edit can be observed to transfer (or not) responsibility.
func seedScheduleTriggerPublishedBy(t *testing.T, apID, memberID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression, published_by_type, published_by_id)
		VALUES ($1, 'schedule', true, '0 * * * *', 'member', $2) RETURNING id`,
		apID, memberID).Scan(&id); err != nil {
		t.Fatalf("seed schedule trigger: %v", err)
	}
	return id
}

// triggerPublisherMember returns the member id currently recorded as the trigger's
// responsible publisher, or "" if it is not a member.
func triggerPublisherMember(t *testing.T, triggerID string) string {
	t.Helper()
	row, err := testHandler.Queries.GetAutopilotTrigger(context.Background(), parseUUID(triggerID))
	if err != nil {
		t.Fatalf("load trigger: %v", err)
	}
	if row.PublishedByType.Valid && row.PublishedByType.String == "member" && row.PublishedByID.Valid {
		return uuidToString(row.PublishedByID)
	}
	return ""
}

func patchAutopilot(t *testing.T, apID string, body map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, body)
	req = withURLParam(req, "id", apID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func patchTrigger(t *testing.T, apID, triggerID string, body map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+triggerID, body)
	req = withURLParams(req, "id", apID, "triggerId", triggerID)
	testHandler.UpdateAutopilotTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilotTrigger: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateAutopilot_PromptEditTransfersAllTriggers: the autopilot description IS the
// run prompt (task instruction), so editing it transfers accountability for EVERY
// trigger to the editor — the exact gap Elon flagged (description was omitted from the
// substantive predicate, so a prompt rewrite left runs attributed to the creator).
func TestUpdateAutopilot_PromptEditTransfersAllTriggers(t *testing.T) {
	creatorA := seedTransferMember(t, "prompt-creator")
	agentID := createWebhookTestAgent(t, "PromptEdit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig1 := seedScheduleTriggerPublishedBy(t, apID, creatorA)
	trig2 := seedScheduleTriggerPublishedBy(t, apID, creatorA)

	patchAutopilot(t, apID, map[string]any{"description": "Rewritten prompt: do the new thing"})

	if got := triggerPublisherMember(t, trig1); got != testUserID {
		t.Errorf("trigger1 publisher = %s, want editor %s (prompt edit must transfer)", got, testUserID)
	}
	if got := triggerPublisherMember(t, trig2); got != testUserID {
		t.Errorf("trigger2 publisher = %s, want editor %s (prompt edit bumps all triggers)", got, testUserID)
	}
}

// TestUpdateAutopilot_CosmeticTitleEditDoesNotTransfer: the title is a display label,
// not an instruction — editing it alone must NOT move responsibility.
func TestUpdateAutopilot_CosmeticTitleEditDoesNotTransfer(t *testing.T) {
	creatorA := seedTransferMember(t, "title-creator")
	agentID := createWebhookTestAgent(t, "TitleEdit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := seedScheduleTriggerPublishedBy(t, apID, creatorA)

	patchAutopilot(t, apID, map[string]any{"title": "Renamed automation"})

	if got := triggerPublisherMember(t, trig); got != creatorA {
		t.Errorf("trigger publisher = %s, want unchanged creator %s (cosmetic title edit must not transfer)", got, creatorA)
	}
}

// TestUpdateAutopilotTrigger_SubstantiveEditTransfersOnlyThatTrigger: a cron/enabled
// edit changes what/when THIS trigger fires, so it transfers only this trigger's
// responsibility; a sibling trigger is untouched (per-firing-trigger granularity).
func TestUpdateAutopilotTrigger_SubstantiveEditTransfersOnlyThatTrigger(t *testing.T) {
	creatorA := seedTransferMember(t, "trig-creator")
	agentID := createWebhookTestAgent(t, "TrigEdit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig1 := seedScheduleTriggerPublishedBy(t, apID, creatorA)
	trig2 := seedScheduleTriggerPublishedBy(t, apID, creatorA)

	patchTrigger(t, apID, trig1, map[string]any{"enabled": false})

	if got := triggerPublisherMember(t, trig1); got != testUserID {
		t.Errorf("trigger1 publisher = %s, want editor %s (substantive edit must transfer)", got, testUserID)
	}
	if got := triggerPublisherMember(t, trig2); got != creatorA {
		t.Errorf("trigger2 publisher = %s, want unchanged creator %s (editing a sibling must not transfer)", got, creatorA)
	}
}

// TestUpdateAutopilotTrigger_LabelOnlyEditDoesNotTransfer: label is cosmetic, so a
// label-only PATCH must NOT transfer responsibility — the over-transfer Elon flagged
// (any PATCH used to unconditionally re-stamp the publisher).
func TestUpdateAutopilotTrigger_LabelOnlyEditDoesNotTransfer(t *testing.T) {
	creatorA := seedTransferMember(t, "label-creator")
	agentID := createWebhookTestAgent(t, "LabelEdit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := seedScheduleTriggerPublishedBy(t, apID, creatorA)

	patchTrigger(t, apID, trig, map[string]any{"label": "just a nickname"})

	if got := triggerPublisherMember(t, trig); got != creatorA {
		t.Errorf("trigger publisher = %s, want unchanged creator %s (label-only edit must not transfer)", got, creatorA)
	}
}

// TestUpdateAutopilotTrigger_NoOpEditDoesNotTransfer: a PATCH that re-sends the trigger's
// current values changes nothing, so responsibility must stay put.
func TestUpdateAutopilotTrigger_NoOpEditDoesNotTransfer(t *testing.T) {
	creatorA := seedTransferMember(t, "noop-creator")
	agentID := createWebhookTestAgent(t, "NoOpEdit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := seedScheduleTriggerPublishedBy(t, apID, creatorA)

	// Re-send the same cron the row already has.
	patchTrigger(t, apID, trig, map[string]any{"cron_expression": "0 * * * *"})

	if got := triggerPublisherMember(t, trig); got != creatorA {
		t.Errorf("trigger publisher = %s, want unchanged creator %s (no-op edit must not transfer)", got, creatorA)
	}
}
