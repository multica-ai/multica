package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type controlPlaneFixture struct {
	agentID    string
	parentID   string
	originalID string
	fallbackID string
	handoffID  string
}

func newControlPlaneFixture(t *testing.T) controlPlaneFixture {
	t.Helper()
	ctx := context.Background()
	var fx controlPlaneFixture
	var parentNumber int32
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace SET issue_counter = issue_counter + 1
		WHERE id = $1
		RETURNING issue_counter
	`, testWorkspaceID).Scan(&parentNumber); err != nil {
		t.Fatalf("reserve parent issue number: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent
		WHERE workspace_id = $1 AND name = 'Handler Test Agent'
	`, testWorkspaceID).Scan(&fx.agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number
		)
		VALUES ($1, $2, 'in_progress', 'none', 'member', $3, 0, $4)
		RETURNING id
	`, testWorkspaceID, "control-plane parent "+t.Name(), testUserID, parentNumber).Scan(&fx.parentID); err != nil {
		t.Fatalf("create parent issue: %v", err)
	}
	for i, out := range []*string{&fx.originalID, &fx.fallbackID} {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, status, priority, issue_id, started_at,
				is_leader_task, parent_task_id
			)
			VALUES ($1, $2, 'running', 0, $3, now(), true, $4)
			RETURNING id
		`, fx.agentID, handlerTestRuntimeID(t), fx.parentID,
			func() any {
				if i == 0 {
					return nil
				}
				return fx.originalID
			}(),
		).Scan(out); err != nil {
			t.Fatalf("create orchestration task %d: %v", i, err)
		}
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO provider_failover_handoff (
			workspace_id, original_task_id, chain_root_task_id, issue_id,
			source_agent_id, source_provider, target_provider, target_agent_id,
			fallback_task_id, trigger_reason, state, mode, would_fail_over,
			side_effects
		)
		VALUES ($1, $2, $2, $3, $4, 'codex', 'claude', $4, $5,
			'provider_quota_limit', 'HANDOFF_DISPATCHED', 'active', true,
			'{}'::jsonb)
		RETURNING id
	`, testWorkspaceID, fx.originalID, fx.parentID, fx.agentID, fx.fallbackID).Scan(&fx.handoffID); err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM control_plane_effect_ledger WHERE chain_root_task_id = $1`, fx.originalID)
		testPool.Exec(context.Background(), `DELETE FROM provider_failover_handoff WHERE id = $1`, fx.handoffID)
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id = $1 OR id IN ($1, $2)`, fx.originalID, fx.fallbackID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE parent_issue_id = $1`, fx.parentID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, fx.parentID)
	})
	return fx
}

func agentIssueRequest(taskID, agentID, method, path string, body map[string]any) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return req
}

func TestConcurrentOriginalFallbackChildCreateIsExactlyOnce(t *testing.T) {
	fx := newControlPlaneFixture(t)
	title := "same child from original and fallback " + t.Name()
	taskIDs := []string{fx.originalID, fx.fallbackID}
	codes := make(chan int, len(taskIDs))
	var wg sync.WaitGroup
	for _, taskID := range taskIDs {
		taskID := taskID
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := agentIssueRequest(taskID, fx.agentID, http.MethodPost,
				"/api/issues?workspace_id="+testWorkspaceID,
				map[string]any{
					"title":           title,
					"status":          "backlog",
					"priority":        "none",
					"assignee_type":   "agent",
					"assignee_id":     fx.agentID,
					"parent_issue_id": fx.parentID,
					"stage":           2,
				})
			testHandler.CreateIssue(w, req)
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)

	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[http.StatusCreated] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("want one 201 and one idempotent 409, got %+v", counts)
	}
	var children, claims int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM issue WHERE parent_issue_id = $1 AND title = $2
	`, fx.parentID, title).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM control_plane_effect_ledger
		WHERE chain_root_task_id = $1 AND effect_type = 'task_spawn'
	`, fx.originalID).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if children != 1 || claims != 1 {
		t.Fatalf("children=%d claims=%d, want exactly one each", children, claims)
	}
}

func TestConcurrentOriginalFallbackStagePromotionIsExactlyOnce(t *testing.T) {
	fx := newControlPlaneFixture(t)
	var childID string
	var childNumber int32
	if err := testPool.QueryRow(context.Background(), `
		UPDATE workspace SET issue_counter = issue_counter + 1
		WHERE id = $1
		RETURNING issue_counter
	`, testWorkspaceID).Scan(&childNumber); err != nil {
		t.Fatalf("reserve child issue number: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (
			workspace_id, title, status, priority, assignee_type, assignee_id,
			creator_type, creator_id, parent_issue_id, stage, position, number
		)
		VALUES ($1, $2, 'backlog', 'none', 'agent', $3, 'agent', $3, $4, 2, 0, $5)
		RETURNING id
	`, testWorkspaceID, "promoted child "+t.Name(), fx.agentID, fx.parentID, childNumber).Scan(&childID); err != nil {
		t.Fatalf("create child: %v", err)
	}

	taskIDs := []string{fx.originalID, fx.fallbackID}
	codes := make(chan int, len(taskIDs))
	var wg sync.WaitGroup
	for _, taskID := range taskIDs {
		taskID := taskID
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := agentIssueRequest(taskID, fx.agentID, http.MethodPut,
				"/api/issues/"+childID, map[string]any{"status": "todo"})
			req = withURLParam(req, "id", childID)
			testHandler.UpdateIssue(w, req)
			if w.Code != http.StatusOK {
				var body map[string]any
				_ = json.Unmarshal(w.Body.Bytes(), &body)
				t.Logf("promotion response %d: %v", w.Code, body)
			}
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("idempotent promotions must both return 200, got %d", code)
		}
	}

	var status string
	var claims, childTasks int
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, childID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM control_plane_effect_ledger
		WHERE chain_root_task_id = $1 AND effect_type = 'stage_promotion'
	`, fx.originalID).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND id NOT IN ($2, $3)
	`, childID, fx.originalID, fx.fallbackID).Scan(&childTasks); err != nil {
		t.Fatal(err)
	}
	if status != "todo" || claims != 1 || childTasks != 1 {
		t.Fatalf("status=%s claims=%d child_tasks=%d, want todo/1/1", status, claims, childTasks)
	}
}

func TestFallbackResolvesOriginalControlPlaneChainRoot(t *testing.T) {
	fx := newControlPlaneFixture(t)
	_, root, err := testHandler.TaskService.ControlPlaneChainRootForTask(context.Background(), parseUUID(fx.fallbackID))
	if err != nil {
		t.Fatal(err)
	}
	if got := uuidToString(root); got != fx.originalID {
		t.Fatalf("fallback chain root = %s, want %s", got, fx.originalID)
	}
}
