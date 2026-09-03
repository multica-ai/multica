package handler

import (
	"context"
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
	s1 := dbfx.Issue(t, "batch order stage1", testutil.Cols{"parent_issue_id": parentID, "stage": 1, "status": "backlog"})
	s2 := dbfx.Issue(t, "batch order stage2", testutil.Cols{"parent_issue_id": parentID, "stage": 2, "status": "backlog"})
	s3 := dbfx.Issue(t, "batch order stage3", testutil.Cols{"parent_issue_id": parentID, "stage": 3, "status": "backlog"})
	startChildDoneWorkflowRaw(t, parentID, `{"schema_version":1,"stages":[{"key":"one","name":"One"},{"key":"two","name":"Two"},{"key":"three","name":"Three"}]}`)

	byStage := map[int]string{1: s1, 2: s2}
	ids := make([]string, 0, len(order))
	for _, stage := range order {
		ids = append(ids, byStage[stage])
	}
	batchFinishWorkflowChildren(t, ids)
	assertWorkflowIssueStatusHandler(t, s3, "todo")

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
	a := runBatchWorkflowScenario(t, []int{1, 2})
	b := runBatchWorkflowScenario(t, []int{2, 1})
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("order changed outcome: a=%+v b=%+v", a, b)
	}
	wantKinds := []string{"stage_satisfied", "stage_advanced"}
	if !reflect.DeepEqual(a.TransitionKinds, wantKinds) {
		t.Fatalf("transition kinds = %v, want %v", a.TransitionKinds, wantKinds)
	}
	if a.RunStatus != "running" || a.CurrentStage != 3 {
		t.Fatalf("run = %s stage %d, want running stage 3", a.RunStatus, a.CurrentStage)
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
