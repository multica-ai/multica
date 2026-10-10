package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestWorktreeReadinessPreClaimRecoveryAndIsolation(t *testing.T) {
	ctx := context.Background()
	runtimeID, taskID := seedWorktreeGateClaimFixture(t, ctx, "readiness", batchClaimTestDaemonID, "9.9.9")
	var resourceID, agentID string
	var ref []byte
	if err := testPool.QueryRow(ctx, `SELECT pr.id,pr.resource_ref,t.agent_id FROM agent_task_queue t JOIN issue i ON i.id=t.issue_id JOIN project_resource pr ON pr.project_id=i.project_id WHERE t.id=$1`, taskID).Scan(&resourceID, &ref, &agentID); err != nil {
		t.Fatal(err)
	}
	claim := func() *db.AgentTaskQueue {
		t.Helper()
		task, err := testHandler.TaskService.ClaimTask(ctx, parseUUID(agentID))
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := testPool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DELETE FROM local_worktree_readiness WHERE resource_id=$1`, resourceID)
	for range 2 {
		if got := claim(); got != nil {
			t.Fatal("unmeasured source was claimed")
		}
	}
	report := func(status string, bytes int64, want int) {
		t.Helper()
		body := protocol.WorktreeReadinessReport{WorktreeReadinessResource: protocol.WorktreeReadinessResource{ID: resourceID, ResourceRef: ref}, Measurement: protocol.WorktreeReadiness{Status: status, FileCount: 1, TotalBytes: bytes}}
		req := withURLParam(newDaemonTokenRequest("POST", "/readiness", body, testWorkspaceID, batchClaimTestDaemonID), "runtimeId", runtimeID)
		w := httptest.NewRecorder()
		testHandler.ReportDaemonWorktreeReadiness(w, req)
		if w.Code != want {
			t.Fatalf("report: %d %s", w.Code, w.Body.String())
		}
	}
	report("ready", (200<<20)+1, 200) // server derives blocked, despite claimed ready
	if claim() != nil {
		t.Fatal("excessive bytes claimed")
	}
	// Another project on the same daemon remains claimable while this one waits.
	_, otherTask := seedWorktreeGateClaimFixture(t, ctx, "unrelated", batchClaimTestDaemonID+"-other", "9.9.9")
	var otherAgent string
	if err := testPool.QueryRow(ctx, `SELECT agent_id FROM agent_task_queue WHERE id=$1`, otherTask).Scan(&otherAgent); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE agent SET runtime_id=$2 WHERE id=$1`, otherAgent, runtimeID)
	exec(`UPDATE agent_task_queue SET runtime_id=$2 WHERE id=$1`, otherTask, runtimeID)
	exec(`UPDATE project_resource SET resource_ref=jsonb_set(jsonb_set(resource_ref,'{daemon_id}',to_jsonb($2::text)),'{execution_mode}','"in_place"') WHERE project_id=(SELECT i.project_id FROM issue i JOIN agent_task_queue t ON t.issue_id=i.id WHERE t.id=$1)`, otherTask, batchClaimTestDaemonID)
	got, err := testHandler.TaskService.ClaimTask(ctx, parseUUID(otherAgent))
	if err != nil || got == nil || uuidToString(got.ID) != otherTask {
		t.Fatalf("unrelated resource blocked: %v %v", got, err)
	}
	report("ready", 1, 200)
	exec(`UPDATE local_worktree_readiness SET expires_at=now()-interval '1 second' WHERE resource_id=$1`, resourceID)
	if claim() != nil {
		t.Fatal("stale readiness claimed")
	}
	report("unavailable", 0, 200)
	if claim() != nil {
		t.Fatal("unavailable readiness claimed")
	}
	report("ready", 1, 200)
	exec(`UPDATE project_resource SET resource_ref=jsonb_set(resource_ref,'{local_path}','"/changed"') WHERE id=$1`, resourceID)
	if claim() != nil {
		t.Fatal("old-ref readiness claimed")
	}
	report("ready", 1, 409) // a scan of the previous ref cannot overwrite readiness
	exec(`UPDATE project_resource SET resource_ref=$2 WHERE id=$1`, resourceID, ref)
	report("ready", 1, 200)
	var wg sync.WaitGroup
	results := make(chan *db.AgentTaskQueue, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := testHandler.TaskService.ClaimTask(ctx, parseUUID(agentID))
			results <- got
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	claimed := 0
	for got := range results {
		if got != nil {
			claimed++
			if uuidToString(got.ID) != taskID {
				t.Fatal("recovery replaced task ID")
			}
		}
	}
	if claimed != 1 {
		t.Fatalf("concurrent claims = %d", claimed)
	}
	var failed int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE agent_id=$1 AND status='failed'`, agentID).Scan(&failed); err != nil || failed != 0 {
		t.Fatalf("wait generated failure: %d %v", failed, err)
	}
}

func TestWorktreeReadinessCancellationAndDaemonOwnership(t *testing.T) {
	ctx := context.Background()
	rt, task := seedWorktreeGateClaimFixture(t, ctx, "cancel-readiness", batchClaimTestDaemonID, "9.9.9")
	var resourceID, agentID string
	var ref []byte
	if err := testPool.QueryRow(ctx, `SELECT pr.id,pr.resource_ref,t.agent_id FROM agent_task_queue t JOIN issue i ON i.id=t.issue_id JOIN project_resource pr ON pr.project_id=i.project_id WHERE t.id=$1`, task).Scan(&resourceID, &ref, &agentID); err != nil {
		t.Fatal(err)
	}
	req := withURLParam(newDaemonTokenRequest("GET", "/readiness", nil, testWorkspaceID, "another-daemon"), "runtimeId", rt)
	w := httptest.NewRecorder()
	testHandler.ListDaemonWorktreeResources(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-daemon read: %d", w.Code)
	}
	req = withURLParam(newDaemonTokenRequest("POST", "/readiness", protocol.WorktreeReadinessReport{WorktreeReadinessResource: protocol.WorktreeReadinessResource{ID: resourceID, ResourceRef: ref}, Measurement: protocol.WorktreeReadiness{Status: "ready"}}, testWorkspaceID, "another-daemon"), "runtimeId", rt)
	w = httptest.NewRecorder()
	testHandler.ReportDaemonWorktreeReadiness(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-daemon write: %d", w.Code)
	}
	if _, err := testHandler.TaskService.CancelTask(ctx, parseUUID(task)); err != nil {
		t.Fatal(err)
	}
	got, err := testHandler.TaskService.ClaimTask(ctx, parseUUID(agentID))
	if err != nil || got != nil {
		t.Fatalf("cancelled task resumed: %v %v", got, err)
	}
}

func TestWorktreeReadinessResponseFreshness(t *testing.T) {
	now := time.Now()
	data, _ := json.Marshal(protocol.WorktreeReadiness{Status: "blocked", FileCount: 234, TotalBytes: 1 << 30, MaxFiles: 2000, MaxBytes: 200 << 20, LargestPaths: []protocol.WorktreeReadinessPath{{Path: "debug", FileCount: 234, TotalBytes: 1 << 30}}})
	row := db.LocalWorktreeReadiness{Status: "blocked", Measurement: data, CheckedAt: pgtype.Timestamptz{Time: now, Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: now.Add(time.Second), Valid: true}}
	if got := readinessResponse(&row, now); got.Status != "blocked" || got.LargestPaths[0].Path != "debug" {
		t.Fatalf("bad current diagnostic: %+v", got)
	}
	if got := readinessResponse(&row, now.Add(time.Second)); got.Status != "unavailable" || got.TotalBytes != 1<<30 {
		t.Fatalf("stale metric not distinguished: %+v", got)
	}
	if got := readinessResponse(nil, now); got.Status != "checking" {
		t.Fatal(got)
	}
}

func TestWorktreeReadinessUnknownDoesNotReportZeroUsage(t *testing.T) {
	now := time.Now()
	failed := db.LocalWorktreeReadiness{Status: "unavailable", Measurement: []byte(`{"status":"unavailable"}`), CheckedAt: pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: now.Add(-time.Minute), Valid: true}}
	for _, r := range []*protocol.WorktreeReadiness{readinessResponse(nil, now), {Status: "unavailable", ReasonCode: "inspection_failed"}, readinessResponse(&failed, now)} {
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["file_count"]; ok {
			t.Fatalf("unknown measurement exposed zero usage: %s", data)
		}
	}
}

func TestResourceReadinessAPIBeforeTasksAndConfigurationChanges(t *testing.T) {
	ctx := context.Background()
	runtimeID, taskID := seedWorktreeGateClaimFixture(t, ctx, "resource-api", batchClaimTestDaemonID, "9.9.9")
	var projectID, resourceID string
	if err := testPool.QueryRow(ctx, `SELECT pr.project_id,pr.id FROM project_resource pr JOIN issue i ON i.project_id=pr.project_id JOIN agent_task_queue t ON t.issue_id=i.id WHERE t.id=$1`, taskID).Scan(&projectID, &resourceID); err != nil {
		t.Fatal(err)
	}
	dbfx.Exec(t, `DELETE FROM agent_task_queue WHERE id=$1`, taskID)
	get := func() protocol.WorktreeReadiness {
		t.Helper()
		req := withURLParam(newRequest("GET", "/api/projects/"+projectID+"/resources", nil), "id", projectID)
		w := httptest.NewRecorder()
		testHandler.ListProjectResources(w, req)
		if w.Code != 200 {
			t.Fatalf("list resources: %d %s", w.Code, w.Body.String())
		}
		var response struct {
			Resources []ProjectResourceResponse `json:"resources"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Resources) != 1 || response.Resources[0].WorktreeReadiness == nil {
			t.Fatal("missing pre-task readiness")
		}
		return *response.Resources[0].WorktreeReadiness
	}
	if got := get(); got.Status != "ready" {
		t.Fatalf("fresh ready lost: %+v", got)
	}
	dbfx.Exec(t, `UPDATE project_resource SET resource_ref=jsonb_set(resource_ref,'{local_path}','"/edited"') WHERE id=$1`, resourceID)
	if got := get(); got.Status != "checking" || got.CheckedAt != "" {
		t.Fatalf("edited resource retained measurement: %+v", got)
	}
	dbfx.Exec(t, `UPDATE agent_runtime SET metadata='{}' WHERE id=$1`, runtimeID)
	if got := get(); got.Status != "unavailable" || got.ReasonCode != "daemon_unavailable" {
		t.Fatalf("old daemon shown ready: %+v", got)
	}
	if err := testHandler.Queries.DeleteProjectResource(ctx, parseUUID(resourceID)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM local_worktree_readiness WHERE resource_id=$1`, resourceID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("resource readiness orphaned: %d %v", count, err)
	}
}
