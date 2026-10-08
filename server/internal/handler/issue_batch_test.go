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

// TestBatchUpdateNoMutationReturnsZero — regression for #1660.
//
// When the request payload has valid issue_ids but the "updates" field
// is empty, missing, or doesn't decode any known mutation field, the
// handler used to walk every issue, run a no-op UPDATE, and increment
// `updated` for each one — returning {"updated": N} despite changing
// nothing. Reporters saw 200 + a positive count and assumed the call
// worked, then chased a phantom persistence bug.
//
// The fix is "tell the truth": when no mutation field is present, return
// {"updated": 0} immediately so the count matches reality.
func TestBatchUpdateNoMutationReturnsZero(t *testing.T) {
	// Two fresh issues so we can also assert no fields actually changed.
	a := createTestIssue(t, "BU-no-mut A", "todo", "low")
	b := createTestIssue(t, "BU-no-mut B", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	t.Cleanup(func() { deleteTestIssue(t, b) })

	cases := []struct {
		desc string
		body map[string]any
	}{
		{
			desc: "updates_missing",
			// Most common reporter pattern: status at top level.
			body: map[string]any{"issue_ids": []string{a, b}, "status": "in_progress"},
		},
		{
			desc: "updates_empty_object",
			body: map[string]any{"issue_ids": []string{a, b}, "updates": map[string]any{}},
		},
		{
			desc: "updates_misnamed",
			// Singular "update" instead of plural "updates".
			body: map[string]any{"issue_ids": []string{a, b}, "update": map[string]any{"status": "done"}},
		},
		{
			desc: "updates_unknown_field_only",
			// Payload IS nested correctly, but every key inside `updates` is
			// unknown to the handler — same class of caller mistake as the
			// shapes above. hasMutation must stay false; behavior is already
			// correct, this case locks it in against future regressions.
			body: map[string]any{"issue_ids": []string{a, b}, "updates": map[string]any{"foo": "bar"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/issues/batch-update", tc.body)
			testHandler.BatchUpdateIssues(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			var resp struct {
				Updated int `json:"updated"`
			}
			json.NewDecoder(w.Body).Decode(&resp)
			if resp.Updated != 0 {
				t.Errorf("expected updated=0 when no mutation field present, got %d", resp.Updated)
			}

			// Belt and braces: confirm the issues weren't touched.
			for _, id := range []string{a, b} {
				gw := httptest.NewRecorder()
				gr := newRequest("GET", "/api/issues/"+id, nil)
				gr = withURLParam(gr, "id", id)
				testHandler.GetIssue(gw, gr)
				var got IssueResponse
				json.NewDecoder(gw.Body).Decode(&got)
				if got.Status != "todo" {
					t.Errorf("issue %s: status changed to %q despite no-mutation request", id, got.Status)
				}
			}
		})
	}
}

// TestBatchUpdateValidUpdatesPersistAndCount — positive case to lock in
// happy-path behavior alongside the regression test above.
func TestBatchUpdateValidUpdatesPersistAndCount(t *testing.T) {
	a := createTestIssue(t, "BU-ok A", "todo", "low")
	b := createTestIssue(t, "BU-ok B", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	t.Cleanup(func() { deleteTestIssue(t, b) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{a, b},
		"updates":   map[string]any{"status": "in_progress"},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Updated != 2 {
		t.Errorf("expected updated=2, got %d", resp.Updated)
	}
	for _, id := range []string{a, b} {
		gw := httptest.NewRecorder()
		gr := newRequest("GET", "/api/issues/"+id, nil)
		gr = withURLParam(gr, "id", id)
		testHandler.GetIssue(gw, gr)
		var got IssueResponse
		json.NewDecoder(gw.Body).Decode(&got)
		if got.Status != "in_progress" {
			t.Errorf("issue %s: expected status=in_progress, got %q", id, got.Status)
		}
	}
}

// TestBatchUpdateStageOnly — regression for the stage barrier feature: a
// batch update whose only field is `stage` must count as a mutation (hasMutation
// includes "stage") and actually persist, not silently return {"updated": 0}.
func TestBatchUpdateStageOnly(t *testing.T) {
	a := createTestIssue(t, "BU-stage A", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{a},
		"updates":   map[string]any{"stage": 2},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Updated != 1 {
		t.Fatalf("expected updated=1 for a stage-only batch update, got %d", resp.Updated)
	}

	gw := httptest.NewRecorder()
	gr := newRequest("GET", "/api/issues/"+a, nil)
	gr = withURLParam(gr, "id", a)
	testHandler.GetIssue(gw, gr)
	var got IssueResponse
	json.NewDecoder(gw.Body).Decode(&got)
	if got.Stage == nil || *got.Stage != 2 {
		t.Errorf("expected stage=2 to persist, got %v", got.Stage)
	}
}

// createTestIssue is a small helper to keep the table-driven cases clean.
// Returns the new issue's id; caller is responsible for cleanup.
func createTestIssue(t *testing.T, title, status, priority string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    title,
		"status":   status,
		"priority": priority,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	return issue.ID
}

func deleteTestIssue(t *testing.T, id string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/issues/"+id, nil)
	req = withURLParam(req, "id", id)
	testHandler.DeleteIssue(w, req)
}

// --- MUL-4155: batch cross-stage child-done aggregation ---
//
// A single batch that finishes sub-issues spanning multiple stages must
// evaluate the parent stage barrier ONCE against the batch's final committed
// state, not per-child mid-batch. The mid-batch behaviour fired one comment per
// intermediate stage, pinned the parent assignee's wake to a stale "advance the
// next stage" instruction, and produced order-dependent output. These tests
// lock in the aggregated behaviour: at most one accurate comment + one wake per
// parent, identical regardless of issue_ids order.

// stagedBatchFixture is a parent (agent-assigned, in_progress) with two ordered
// stages of two in_progress children each, so a single batch that finishes all
// four exercises the cross-stage barrier aggregation.
type stagedBatchFixture struct {
	parent  IssueResponse
	agentID string
	stage1  []IssueResponse
	stage2  []IssueResponse
}

func newStagedBatchFixture(t *testing.T) stagedBatchFixture {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}

	pw := httptest.NewRecorder()
	preq := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "batch-stage parent " + time.Now().Format(time.RFC3339Nano),
		"status": "in_progress",
	})
	testHandler.CreateIssue(pw, preq)
	if pw.Code != http.StatusCreated {
		t.Fatalf("create parent: expected 201, got %d: %s", pw.Code, pw.Body.String())
	}
	var parent IssueResponse
	json.NewDecoder(pw.Body).Decode(&parent)

	// Assign the parent to the ready test agent via direct SQL so the child-done
	// wake actually enqueues a task we can pin to the final comment — without the
	// assignment trigger queuing an unrelated task at setup.
	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("locate test agent: %v", err)
	}
	setIssueAssigneeDirect(t, parent.ID, "agent", agentID)

	mkChild := func(stage int32) IssueResponse {
		cw := httptest.NewRecorder()
		creq := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":           "batch-stage child " + time.Now().Format(time.RFC3339Nano),
			"status":          "in_progress",
			"parent_issue_id": parent.ID,
		})
		testHandler.CreateIssue(cw, creq)
		if cw.Code != http.StatusCreated {
			t.Fatalf("create child: expected 201, got %d: %s", cw.Code, cw.Body.String())
		}
		var child IssueResponse
		json.NewDecoder(cw.Body).Decode(&child)
		// Set the stage directly — the barrier logic only reads issue.stage.
		if _, err := testPool.Exec(context.Background(),
			`UPDATE issue SET stage = $2 WHERE id = $1`, child.ID, stage); err != nil {
			t.Fatalf("set child stage: %v", err)
		}
		return child
	}

	fx := stagedBatchFixture{parent: parent, agentID: agentID}
	fx.stage1 = []IssueResponse{mkChild(1), mkChild(1)}
	fx.stage2 = []IssueResponse{mkChild(2), mkChild(2)}

	t.Cleanup(func() {
		for _, c := range append(append([]IssueResponse{}, fx.stage1...), fx.stage2...) {
			cleanupChildDoneIssue(c.ID)
		}
		cleanupChildDoneIssue(parent.ID)
	})

	return fx
}

// batchSetStatus drives BatchUpdateIssues over the given ids, in order.
func batchSetStatus(t *testing.T, ids []string, status string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": ids,
		"updates":   map[string]any{"status": status},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues status=%q: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
}

// TestBatchChildDoneCrossStage_OneWake is the MUL-4155 core. A single batch
// that finishes children across two stages must wake the parent once, from
// the final state: every sub-issue closed, never a stale "Stage 2 is next",
// regardless of id order.
func TestBatchChildDoneCrossStage_OneWake(t *testing.T) {
	assertFinal := func(t *testing.T, parentID, agentID string) {
		t.Helper()
		entries := childDoneEntries(t, parentID)
		if len(entries) != 1 || entries[0].Stage != nil || entries[0].Total != 4 || entries[0].Outcome != "woke" {
			t.Fatalf("entries = %+v, want one wrap-up wake", entries)
		}
		runs := childDoneRuns(t, parentID)
		if len(runs) != 1 || runs[0].ID != entries[0].TaskID || strings.Contains(runs[0].Note, "next_stage") || !strings.Contains(runs[0].Note, `"all":true`) {
			t.Fatalf("runs = %+v, want one run for the final state", runs)
		}
		if got := countPendingTasksForAgent(t, parentID, agentID); got != 1 {
			t.Fatalf("expected exactly 1 pending parent task, got %d", got)
		}
	}

	t.Run("forward order [stage1, stage2]", func(t *testing.T) {
		fx := newStagedBatchFixture(t)
		batchSetStatus(t, []string{fx.stage1[0].ID, fx.stage1[1].ID, fx.stage2[0].ID, fx.stage2[1].ID}, "done")
		assertFinal(t, fx.parent.ID, fx.agentID)
	})

	t.Run("reverse order [stage2, stage1]", func(t *testing.T) {
		fx := newStagedBatchFixture(t)
		batchSetStatus(t, []string{fx.stage2[0].ID, fx.stage2[1].ID, fx.stage1[0].ID, fx.stage1[1].ID}, "done")
		assertFinal(t, fx.parent.ID, fx.agentID)
	})
}

// TestBatchChildDoneCrossStage_Cancelled — cancelling every stage in one batch
// closes them too; the facts count the cancellations apart from finished work.
func TestBatchChildDoneCrossStage_Cancelled(t *testing.T) {
	fx := newStagedBatchFixture(t)
	batchSetStatus(t, []string{fx.stage1[0].ID, fx.stage1[1].ID, fx.stage2[0].ID, fx.stage2[1].ID}, "cancelled")

	runs := childDoneRuns(t, fx.parent.ID)
	if len(runs) != 1 || !strings.Contains(runs[0].Note, `"cancelled":4`) || strings.Contains(runs[0].Note, "next_stage") {
		t.Fatalf("runs = %+v, want one wake counting 4 cancellations", runs)
	}
}

// TestBatchChildDoneClosesLowerStageOnly — when a batch finishes only the lower
// stage, the parent is told Stage 1 closed and pointed at Stage 2.
func TestBatchChildDoneClosesLowerStageOnly(t *testing.T) {
	fx := newStagedBatchFixture(t)
	batchSetStatus(t, []string{fx.stage1[0].ID, fx.stage1[1].ID}, "done")

	entries := childDoneEntries(t, fx.parent.ID)
	if len(entries) != 1 || entries[0].Stage == nil || *entries[0].Stage != 1 || entries[0].Total != 2 {
		t.Fatalf("entries = %+v, want stage 1", entries)
	}
	runs := childDoneRuns(t, fx.parent.ID)
	if len(runs) != 1 || !strings.Contains(runs[0].Note, `"next_stage":2`) {
		t.Fatalf("runs = %+v, want the next stage named", runs)
	}
}
