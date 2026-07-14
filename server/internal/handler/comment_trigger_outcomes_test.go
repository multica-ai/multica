package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func findCommentOutcome(t *testing.T, outcomes []CommentTriggerOutcome, targetID string) CommentTriggerOutcome {
	t.Helper()
	for _, o := range outcomes {
		if o.TargetID == targetID {
			return o
		}
	}
	t.Fatalf("no trigger outcome for target %s in %+v", targetID, outcomes)
	return CommentTriggerOutcome{}
}

// TestCreateComment_MixedMentionSurfacesPartialTriggerOutcomes is the MUL-4525 §2
// acceptance test for Bohan's exact scenario: a comment @mentions an agent the
// author can invoke AND a squad whose private leader they cannot. The comment is
// still saved (one blocked mention must not reject it), and the response carries
// per-target outcomes — queued for the allowed agent, blocked +
// invocation_not_allowed for the squad — so the client can show partial success
// instead of a silent no-op. The preview surfaces the same split before sending.
func TestCreateComment_MixedMentionSurfacesPartialTriggerOutcomes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	allowedAgentID := createHandlerTestAgent(t, "Outcome Allowed Agent", nil)
	// A private leader owned by someone other than testUserID: the workspace
	// owner can VIEW it but cannot INVOKE it (no admin bypass).
	privateLeaderID, _, _ := privateAgentTestFixture(t)
	squadID := createCommentTriggerPreviewSquad(t, "Outcome Private Squad", privateLeaderID)
	issueID := createCommentTriggerPreviewIssue(t, "mixed mention partial outcomes", "", "")

	content := fmt.Sprintf(
		"[@Allowed](mention://agent/%s) [@Squad](mention://squad/%s) please take a look",
		allowedAgentID, squadID,
	)

	// Preview surfaces both the allowed agent and the blocked squad.
	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": content})
	requirePreviewAgents(t, preview, allowedAgentID)
	if len(preview.Blocked) != 1 {
		t.Fatalf("preview blocked = %+v, want 1 entry", preview.Blocked)
	}
	if b := preview.Blocked[0]; b.TargetType != "squad" || b.TargetID != squadID ||
		b.Status != DispatchBlocked || b.ReasonCode != ReasonInvocationNotAllowed {
		t.Fatalf("preview blocked[0] = %+v, want squad %s blocked/invocation_not_allowed", b, squadID)
	}

	// Create the comment: it must save (201) and report partial outcomes.
	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201 (comment must save despite blocked mention), got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	if resp.ID == "" {
		t.Fatal("comment was not saved")
	}
	if len(resp.TriggerOutcomes) != 2 {
		t.Fatalf("trigger_outcomes = %+v, want 2 (one queued, one blocked)", resp.TriggerOutcomes)
	}

	allowed := findCommentOutcome(t, resp.TriggerOutcomes, allowedAgentID)
	if allowed.TargetType != "agent" || allowed.Status != DispatchQueued {
		t.Errorf("allowed outcome = %+v, want agent/queued", allowed)
	}
	blocked := findCommentOutcome(t, resp.TriggerOutcomes, squadID)
	if blocked.TargetType != "squad" || blocked.Status != DispatchBlocked || blocked.ReasonCode != ReasonInvocationNotAllowed {
		t.Errorf("blocked outcome = %+v, want squad/blocked/invocation_not_allowed", blocked)
	}

	// The allowed agent ran; the private leader was never enqueued.
	if got := countQueuedCommentTriggerTasks(t, issueID, allowedAgentID); got != 1 {
		t.Errorf("allowed agent queued tasks = %d, want 1", got)
	}
	var leaderTasks int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issueID, privateLeaderID).Scan(&leaderTasks); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderTasks != 0 {
		t.Errorf("blocked private leader tasks = %d, want 0", leaderTasks)
	}
}

// TestCreateComment_BlockedMentionReasonDoesNotEnumeratePrivateAgent pins the
// enumeration-safety rule (MUL-4525 §2): a mention the author cannot invoke and a
// mention of a truly nonexistent agent both return the same generic
// invocation_not_allowed, so a blocked reason can never confirm a private
// agent's existence.
func TestCreateComment_BlockedMentionReasonDoesNotEnumeratePrivateAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	privateAgentID, _, _ := privateAgentTestFixture(t)
	issueID := createCommentTriggerPreviewIssue(t, "blocked mention enumeration safety", "", "")
	nonexistentID := "00000000-0000-0000-0000-0000000000ff"

	content := fmt.Sprintf(
		"[@Private](mention://agent/%s) [@Ghost](mention://agent/%s) ping",
		privateAgentID, nonexistentID,
	)
	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": content})
	if len(preview.Agents) != 0 {
		t.Fatalf("preview agents = %+v, want none", preview.Agents)
	}
	if len(preview.Blocked) != 2 {
		t.Fatalf("preview blocked = %+v, want 2", preview.Blocked)
	}
	for _, b := range preview.Blocked {
		if b.ReasonCode != ReasonInvocationNotAllowed {
			t.Errorf("blocked %s reason = %q, want invocation_not_allowed (must not distinguish private-exists from not-found)", b.TargetID, b.ReasonCode)
		}
	}
}
