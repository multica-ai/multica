package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The edit dialog clears a prompt by sending an explicit null; leaving the
// field out of the PATCH must keep it.
func TestUpdateAutopilot_NullDescriptionClearsPrompt(t *testing.T) {
	agentID := createWebhookTestAgent(t, "ClearPrompt Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	patch := func(body map[string]any) *string {
		t.Helper()
		req := newRequest("PATCH", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, body)
		var resp AutopilotResponse
		testutil.Call(t, testHandler.UpdateAutopilot, withURLParam(req, "id", apID)).Want(http.StatusOK).JSON(&resp)
		return resp.Description
	}

	if got := patch(map[string]any{"description": "Old prompt"}); got == nil || *got != "Old prompt" {
		t.Fatalf("set description = %v, want Old prompt", got)
	}
	if got := patch(map[string]any{"title": "Renamed"}); got == nil || *got != "Old prompt" {
		t.Fatalf("description after a title-only PATCH = %v, want Old prompt", got)
	}
	if got := patch(map[string]any{"description": nil}); got != nil {
		t.Fatalf("description after a null PATCH = %q, want nil", *got)
	}
}
