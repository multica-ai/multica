package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Regressions for #8462: a stage that closed because its sub-issues were
// cancelled must not be announced as complete. The sentence is what the
// parent's agent acts on; the counter is a footnote.

// addStagedChild creates a sibling of fx.child under the same parent with the
// given stage and status, and removes it on cleanup.
func addStagedChild(t *testing.T, fx childDoneFixture, stage int, status string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "child-done sibling " + time.Now().Format(time.RFC3339Nano),
		"status":          status,
		"parent_issue_id": fx.parent.ID,
		"stage":           stage,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create staged child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var sibling IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&sibling); err != nil {
		t.Fatalf("decode staged child: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, sibling.ID)
	})
	return sibling
}

func setChildStage(t *testing.T, childID string, stage int) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET stage = $2 WHERE id = $1`, childID, stage); err != nil {
		t.Fatalf("set stage %d: %v", stage, err)
	}
}

func mustContainAll(t *testing.T, content string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in system comment:\n%s", want, content)
		}
	}
}

func mustContainNone(t *testing.T, content string, unwanted ...string) {
	t.Helper()
	for _, s := range unwanted {
		if strings.Contains(content, s) {
			t.Errorf("did not expect %q in system comment:\n%s", s, content)
		}
	}
}

func TestChildDoneStageClosedByCancellationIsNotReportedComplete(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newChildDoneFixture(t, "in_progress")
	setChildStage(t, fx.child.ID, 1)
	sibling := addStagedChild(t, fx, 1, "in_progress")

	// The first cancellation leaves the stage open; the second closes it.
	updateChildStatus(t, fx.child.ID, "cancelled")
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Fatalf("stage still open, expected no comment yet, got %d", got)
	}
	updateChildStatus(t, sibling.ID, "cancelled")

	content := parentSystemCommentContent(t, fx.parent.ID)
	mustContainAll(t, content,
		"Stage 1 of this issue is closed",
		"— was just cancelled.",
		"Stage 1: 0/2 done, 2 cancelled",
		"Closing this stage does not mean the whole issue is done",
		"2 sub-issues cancelled",
		"post a comment asking for confirmation first",
	)
	mustContainNone(t, content, "is complete", "just finished", "2/2 done")
}

func TestChildDoneMixedStageNamesCancelledWorkBeforeNextStage(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newChildDoneFixture(t, "in_progress")
	setChildStage(t, fx.child.ID, 1)
	sibling := addStagedChild(t, fx, 1, "in_progress")
	addStagedChild(t, fx, 2, "backlog") // parked next stage: the instruction must name it

	updateChildStatus(t, fx.child.ID, "done")
	updateChildStatus(t, sibling.ID, "cancelled")

	content := parentSystemCommentContent(t, fx.parent.ID)
	mustContainAll(t, content,
		"Stage 1 of this issue is closed",
		"— was just cancelled.",
		"Stage 1: 1/2 done, 1 cancelled; Stage 2: 0/1 done (next)",
		"Stage 2 is next",
		"1 sub-issue cancelled",
		"not something Stage 2 depends on",
		"post a comment asking for confirmation instead of promoting",
	)
	mustContainNone(t, content, "is complete", "2/2 done")
}

func TestChildDoneUnstagedCancelledIsNotReportedComplete(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "cancelled")

	content := parentSystemCommentContent(t, fx.parent.ID)
	mustContainAll(t, content,
		"All sub-issues are closed — the last one,",
		", was just cancelled.",
		"The set closed with 1 sub-issue cancelled",
		"confirm that the cancelled work is not still needed",
	)
	mustContainNone(t, content, "are complete", "just finished")
}

// The done path keeps its historical wording byte-for-byte: no "closed", no
// cancellation note. Pins the byte-identical promise for the common case.
func TestChildDoneWithoutCancellationKeepsOriginalWording(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	mustContainAll(t, content, "All sub-issues are complete — the last one,", ", just finished.")
	mustContainNone(t, content, "closed", "cancelled")
}
