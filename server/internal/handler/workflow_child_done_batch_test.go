package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

type batchWorkflowOutcome struct {
	RunStatus       string
	CurrentStage    int32
	TransitionKinds []string
	FromStages      []int32
	ToStages        []int32
}

func batchFinishWorkflowChildren(t *testing.T, issueIDs []string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
		"issue_ids": issueIDs,
		"updates":   map[string]any{"status": "done"},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch update status = %d: %s", w.Code, w.Body.String())
	}
}
func TestBatchChildDoneWorkflowReconcilesOnceForClosedStage(t *testing.T) {
	parentID := dbfx.Issue(t, "batch workflow parent", testutil.Cols{"status": "in_progress"})
	s1a := dbfx.Issue(t, "batch stage1 a", testutil.Cols{"parent_issue_id": parentID, "stage": 1, "status": "backlog"})
	s1b := dbfx.Issue(t, "batch stage1 b", testutil.Cols{"parent_issue_id": parentID, "stage": 1, "status": "backlog"})
	s2 := dbfx.Issue(t, "batch stage2", testutil.Cols{"parent_issue_id": parentID, "stage": 2, "status": "backlog"})
	startChildDoneWorkflowRaw(t, parentID, `{"schema_version":1,"stages":[{"key":"one","name":"One"},{"key":"two","name":"Two"}]}`)

	batchFinishWorkflowChildren(t, []string{s1a, s1b})
	assertWorkflowIssueStatusHandler(t, s2, "todo")

	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM workflow_transition wt
		JOIN workflow_run wr ON wr.id=wt.workflow_run_id
		WHERE wr.issue_id=$1 AND wt.kind='stage_advanced'`, parentID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("stage_advanced transitions = %d, want 1", n)
	}
}
func runBatchWorkflowScenario(t *testing.T, order []int) batchWorkflowOutcome {
	t.Helper()
	parentID := dbfx.Issue(t, "batch order parent", testutil.Cols{"status": "in_progress"})
	s1a := dbfx.Issue(t, "batch order stage1 a", testutil.Cols{"parent_issue_id": parentID, "stage": 1, "status": "backlog"})
	s1b := dbfx.Issue(t, "batch order stage1 b", testutil.Cols{"parent_issue_id": parentID, "stage": 1, "status": "backlog"})
	s2 := dbfx.Issue(t, "batch order stage2", testutil.Cols{"parent_issue_id": parentID, "stage": 2, "status": "backlog"})
	startChildDoneWorkflowRaw(t, parentID, `{"schema_version":1,"stages":[{"key":"one","name":"One"},{"key":"two","name":"Two"}]}`)

	children := []string{s1a, s1b}
	ids := []string{children[order[0]], children[order[1]]}
	batchFinishWorkflowChildren(t, ids)
	assertWorkflowIssueStatusHandler(t, s2, "todo")

	status, current := workflowRunForIssue(t, parentID)
	out := batchWorkflowOutcome{RunStatus: status, CurrentStage: current}
	rows, err := testPool.Query(context.Background(), `
		SELECT wt.kind, COALESCE(wt.from_stage,0), COALESCE(wt.to_stage,0)
		FROM workflow_transition wt JOIN workflow_run wr ON wr.id=wt.workflow_run_id
		WHERE wr.issue_id=$1 AND wt.kind <> 'started'
		ORDER BY wt.created_at, wt.id`, parentID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seenKeys := map[string]struct{}{}
	for rows.Next() {
		var kind string
		var fromStage, toStage int32
		if err := rows.Scan(&kind, &fromStage, &toStage); err != nil {
			t.Fatal(err)
		}
		out.TransitionKinds = append(out.TransitionKinds, kind)
		out.FromStages = append(out.FromStages, fromStage)
		out.ToStages = append(out.ToStages, toStage)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	keyRows, err := testPool.Query(context.Background(), `
		SELECT wt.idempotency_key FROM workflow_transition wt
		JOIN workflow_run wr ON wr.id=wt.workflow_run_id WHERE wr.issue_id=$1`, parentID)
	if err != nil {
		t.Fatal(err)
	}
	defer keyRows.Close()
	for keyRows.Next() {
		var key string
		if err := keyRows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		if _, dup := seenKeys[key]; dup {
			t.Fatalf("duplicate workflow transition idempotency key %q", key)
		}
		seenKeys[key] = struct{}{}
	}
	return out
}

func TestBatchChildDoneWorkflowIsOrderIndependent(t *testing.T) {
	a := runBatchWorkflowScenario(t, []int{0, 1})
	b := runBatchWorkflowScenario(t, []int{1, 0})
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("order changed outcome: a=%+v b=%+v", a, b)
	}
	wantKinds := []string{"stage_advanced"}
	if !reflect.DeepEqual(a.TransitionKinds, wantKinds) {
		t.Fatalf("transition kinds = %v, want %v", a.TransitionKinds, wantKinds)
	}
	if a.RunStatus != "running" || a.CurrentStage != 2 {
		t.Fatalf("run = %s stage %d, want running stage 2", a.RunStatus, a.CurrentStage)
	}
}

func TestBatchChildDoneWorkflowRejectsCrossStageBatchWithoutPartialWrites(t *testing.T) {
	for _, order := range [][]int{{1, 2}, {2, 1}} {
		t.Run(fmt.Sprintf("order-%d-%d", order[0], order[1]), func(t *testing.T) {
			parentID := dbfx.Issue(t, "batch cross-stage parent", testutil.Cols{"status": "in_progress"})
			s1 := dbfx.Issue(t, "batch cross-stage stage1", testutil.Cols{"parent_issue_id": parentID, "stage": 1, "status": "backlog"})
			s2 := dbfx.Issue(t, "batch cross-stage stage2", testutil.Cols{"parent_issue_id": parentID, "stage": 2, "status": "backlog"})
			startChildDoneWorkflowRaw(t, parentID, `{"schema_version":1,"stages":[{"key":"one","name":"One"},{"key":"two","name":"Two"}]}`)
			byStage := map[int]string{1: s1, 2: s2}
			w := httptest.NewRecorder()
			req := newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
				"issue_ids": []string{byStage[order[0]], byStage[order[1]]},
				"updates":   map[string]any{"status": "done"},
			})
			testHandler.BatchUpdateIssues(w, req)
			if w.Code != http.StatusConflict {
				t.Fatalf("batch status = %d, want 409: %s", w.Code, w.Body.String())
			}
			assertWorkflowIssueStatusHandler(t, s1, "todo")
			assertWorkflowIssueStatusHandler(t, s2, "backlog")
			status, current := workflowRunForIssue(t, parentID)
			if status != "running" || current != 1 {
				t.Fatalf("run = %s stage %d, want running stage 1", status, current)
			}
		})
	}
}

func TestBatchChildDoneLegacyHighestClosedStageUnchanged(t *testing.T) {
	parentID := dbfx.Issue(t, "batch legacy parent", testutil.Cols{"status": "in_progress"})
	s1 := dbfx.Issue(t, "batch legacy stage1", testutil.Cols{"parent_issue_id": parentID, "stage": 1, "status": "in_progress"})
	s2 := dbfx.Issue(t, "batch legacy stage2", testutil.Cols{"parent_issue_id": parentID, "stage": 2, "status": "backlog"})
	_ = dbfx.Issue(t, "batch legacy stage3", testutil.Cols{"parent_issue_id": parentID, "stage": 3, "status": "backlog"})
	batchFinishWorkflowChildren(t, []string{s1, s2})
	content := parentSystemCommentContent(t, parentID)
	if !strings.Contains(content, "Stage 2 of this issue is complete") || !strings.Contains(content, "Stage 3 is next") {
		t.Fatalf("legacy batch comment changed: %q", content)
	}
}
