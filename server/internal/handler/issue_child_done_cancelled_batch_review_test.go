package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBatchChildDoneNamedStageHistoricalCancellationStillWarns(t *testing.T) {
	fx := newStagedBatchFixture(t)

	// Add a third Stage 2 child so one can already be cancelled while at least
	// two remaining children transition to done together in the closing batch.
	cw := httptest.NewRecorder()
	creq := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "batch-stage historical-cancel child " + time.Now().Format(time.RFC3339Nano),
		"status":          "in_progress",
		"parent_issue_id": fx.parent.ID,
	})
	testHandler.CreateIssue(cw, creq)
	if cw.Code != 201 {
		t.Fatalf("create extra child: expected 201, got %d: %s", cw.Code, cw.Body.String())
	}
	var extra IssueResponse
	if err := json.NewDecoder(cw.Body).Decode(&extra); err != nil {
		t.Fatalf("decode extra child: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET stage = $2 WHERE id = $1`, extra.ID, 2); err != nil {
		t.Fatalf("set extra child stage: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, extra.ID)
	})

	// This cancellation is historical by the time the later batch closes Stage 2.
	// Stage 1 is still open, so it must not emit a parent completion comment yet.
	batchSetStatus(t, []string{fx.stage2[0].ID}, "cancelled")
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Fatalf("historical cancellation must not close the blocked stage, got %d comments", got)
	}

	// Close Stage 1 and finish the two remaining Stage 2 children in one batch.
	// The named Stage 2 already contains the cancelled child above, so the final
	// advance instruction must still include the dependency-confirmation warning.
	batchSetStatus(t, []string{
		fx.stage1[0].ID,
		fx.stage1[1].ID,
		fx.stage2[1].ID,
		extra.ID,
	}, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("expected exactly 1 final system comment, got %d", got)
	}
	content, _, _, _ := systemCommentOn(t, fx.parent.ID)
	if !strings.Contains(content, "Stage 2 of this issue is closed") {
		t.Errorf("expected Stage 2 closed announcement, got: %s", content)
	}
	if !strings.Contains(content, "Stage 2: 2/3 done, 1 cancelled") {
		t.Errorf("expected final Stage 2 cancellation summary, got: %s", content)
	}
	if !strings.Contains(content, "confirm that the cancelled work is not a dependency") {
		t.Errorf("expected cancellation dependency warning, got: %s", content)
	}
}
