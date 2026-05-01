package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func requireCollaborationRequestTestTable(t *testing.T) {
	t.Helper()
	if testPool == nil {
		t.Skip("database not available")
	}
	var exists bool
	if err := testPool.QueryRow(context.Background(), `SELECT to_regclass('public.collaboration_request') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("check collaboration_request table: %v", err)
	}
	if !exists {
		t.Skip("collaboration_request table not present; run migration 068 on a disposable DB to execute DB-backed tests")
	}
}

func collaborationRuntimeConfig(allowedTargets ...string) string {
	encodedTargets, err := json.Marshal(allowedTargets)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf(`{
		"multica_policy": {
			"schema_version": "mhs19.v1",
			"mode": "supervised_collaboration",
			"collaboration": {
				"enabled": true,
				"scope": "same_issue",
				"allowed_agent_targets": %s,
				"raw_agent_mentions": "deny",
				"collaboration_requests": "allow_audited",
				"max_turns": 2,
				"max_depth": 2,
				"ttl_minutes": 60,
				"prevent_self_handoff": true,
				"prevent_cycles": true
			}
		}
	}`, string(encodedTargets))
}

func createCollaborationTestAgent(t *testing.T, name string, runtimeConfig string) string {
	t.Helper()
	ctx := context.Background()
	uniqueName := fmt.Sprintf("%s-%d", name, time.Now().UnixNano())
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', $3::jsonb, $4, 'private', 1, $5)
		RETURNING id
	`, testWorkspaceID, uniqueName, runtimeConfig, handlerTestRuntimeID(t), testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create collaboration test agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func collaborationAgentName(t *testing.T, agentID string) string {
	t.Helper()
	var name string
	if err := testPool.QueryRow(context.Background(), `SELECT name FROM agent WHERE id = $1`, agentID).Scan(&name); err != nil {
		t.Fatalf("load agent name: %v", err)
	}
	return name
}

func createCollaborationTestIssue(t *testing.T, title string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  fmt.Sprintf("%s %d", title, time.Now().UnixNano()),
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})
	return created.ID
}

func createCollaborationSourceTask(t *testing.T, agentID, issueID, status string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load agent runtime: %v", err)
	}
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, $4, 0)
		RETURNING id
	`, agentID, runtimeID, issueID, status).Scan(&taskID); err != nil {
		t.Fatalf("create source task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func postCollaborationRequest(t *testing.T, issueID, sourceAgentID, sourceTaskID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/collaboration-requests", body)
	req = withURLParam(req, "id", issueID)
	if sourceAgentID != "" {
		req.Header.Set("X-Agent-ID", sourceAgentID)
	}
	if sourceTaskID != "" {
		req.Header.Set("X-Task-ID", sourceTaskID)
	}
	testHandler.CreateCollaborationRequest(w, req)
	return w
}

func listCollaborationRequests(t *testing.T, issueID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/collaboration-requests", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListCollaborationRequests(w, req)
	return w
}

func newCollaborationAgentPair(t *testing.T) (sourceID, targetID, sourceTaskID, issueID string) {
	t.Helper()
	targetID = createCollaborationTestAgent(t, "Reviewer", collaborationRuntimeConfig())
	targetName := collaborationAgentName(t, targetID)
	sourceID = createCollaborationTestAgent(t, "Builder", collaborationRuntimeConfig(targetName))
	issueID = createCollaborationTestIssue(t, "collaboration request test")
	sourceTaskID = createCollaborationSourceTask(t, sourceID, issueID, "running")
	return sourceID, targetID, sourceTaskID, issueID
}

func validCollaborationBody(targetID string) map[string]any {
	return map[string]any{
		"to_agent_id": targetID,
		"purpose":     "Please review the implementation risk on this same issue.",
	}
}

func decodeCollaborationResponse(t *testing.T, w *httptest.ResponseRecorder) CollaborationRequestResponse {
	t.Helper()
	var resp CollaborationRequestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode collaboration response: %v; body=%s", err, w.Body.String())
	}
	return resp
}

func countCollaborationRows(t *testing.T, issueID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM collaboration_request WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count collaboration rows: %v", err)
	}
	return count
}

func countCollaborationTargetTasks(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched', 'running')
	`, issueID, agentID).Scan(&count); err != nil {
		t.Fatalf("count target tasks: %v", err)
	}
	return count
}

func TestCreateCollaborationRequestRejectsActorAndTaskViolations(t *testing.T) {
	requireCollaborationRequestTestTable(t)

	t.Run("member actor", func(t *testing.T) {
		_, targetID, _, issueID := newCollaborationAgentPair(t)
		w := postCollaborationRequest(t, issueID, "", "", validCollaborationBody(targetID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
		if got := countCollaborationRows(t, issueID); got != 0 {
			t.Fatalf("expected no collaboration rows, got %d", got)
		}
	})

	t.Run("missing source task", func(t *testing.T) {
		sourceID, targetID, _, issueID := newCollaborationAgentPair(t)
		w := postCollaborationRequest(t, issueID, sourceID, "", validCollaborationBody(targetID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("task agent mismatch", func(t *testing.T) {
		sourceID, targetID, _, issueID := newCollaborationAgentPair(t)
		otherTask := createCollaborationSourceTask(t, targetID, issueID, "running")
		w := postCollaborationRequest(t, issueID, sourceID, otherTask, validCollaborationBody(targetID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("task issue mismatch", func(t *testing.T) {
		sourceID, targetID, _, issueID := newCollaborationAgentPair(t)
		otherIssueID := createCollaborationTestIssue(t, "other collaboration issue")
		otherTask := createCollaborationSourceTask(t, sourceID, otherIssueID, "running")
		w := postCollaborationRequest(t, issueID, sourceID, otherTask, validCollaborationBody(targetID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("inactive source task", func(t *testing.T) {
		sourceID, targetID, _, issueID := newCollaborationAgentPair(t)
		inactiveTask := createCollaborationSourceTask(t, sourceID, issueID, "completed")
		w := postCollaborationRequest(t, issueID, sourceID, inactiveTask, validCollaborationBody(targetID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestCreateCollaborationRequestRejectsPolicyAndRequestViolations(t *testing.T) {
	requireCollaborationRequestTestTable(t)

	t.Run("source policy does not allow audited requests", func(t *testing.T) {
		targetID := createCollaborationTestAgent(t, "Reviewer", collaborationRuntimeConfig())
		sourceID := createCollaborationTestAgent(t, "Operator", `{}`)
		issueID := createCollaborationTestIssue(t, "source policy rejection")
		sourceTaskID := createCollaborationSourceTask(t, sourceID, issueID, "running")
		w := postCollaborationRequest(t, issueID, sourceID, sourceTaskID, validCollaborationBody(targetID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("target policy is not supervised discussion only", func(t *testing.T) {
		targetID := createCollaborationTestAgent(t, "Plain Target", `{}`)
		targetName := collaborationAgentName(t, targetID)
		sourceID := createCollaborationTestAgent(t, "Builder", collaborationRuntimeConfig(targetName))
		issueID := createCollaborationTestIssue(t, "target policy rejection")
		sourceTaskID := createCollaborationSourceTask(t, sourceID, issueID, "running")
		w := postCollaborationRequest(t, issueID, sourceID, sourceTaskID, validCollaborationBody(targetID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("target outside allowlist", func(t *testing.T) {
		targetID := createCollaborationTestAgent(t, "Reviewer", collaborationRuntimeConfig())
		sourceID := createCollaborationTestAgent(t, "Builder", collaborationRuntimeConfig("Some Other Agent"))
		issueID := createCollaborationTestIssue(t, "allowlist rejection")
		sourceTaskID := createCollaborationSourceTask(t, sourceID, issueID, "running")
		w := postCollaborationRequest(t, issueID, sourceID, sourceTaskID, validCollaborationBody(targetID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("self handoff", func(t *testing.T) {
		sourceID := createCollaborationTestAgent(t, "Self Target", collaborationRuntimeConfig("Self Target"))
		issueID := createCollaborationTestIssue(t, "self handoff rejection")
		sourceTaskID := createCollaborationSourceTask(t, sourceID, issueID, "running")
		w := postCollaborationRequest(t, issueID, sourceID, sourceTaskID, validCollaborationBody(sourceID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unsupported mode", func(t *testing.T) {
		sourceID, targetID, sourceTaskID, issueID := newCollaborationAgentPair(t)
		body := validCollaborationBody(targetID)
		body["mode"] = "handoff"
		w := postCollaborationRequest(t, issueID, sourceID, sourceTaskID, body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("raw mention purpose", func(t *testing.T) {
		sourceID, targetID, sourceTaskID, issueID := newCollaborationAgentPair(t)
		for _, purpose := range []string{
			fmt.Sprintf("[@Reviewer](mention://agent/%s) please review", targetID),
			"please review mention://agent/00000000-0000-0000-0000-000000000001",
		} {
			body := validCollaborationBody(targetID)
			body["purpose"] = purpose
			w := postCollaborationRequest(t, issueID, sourceID, sourceTaskID, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("purpose %q: expected 400, got %d: %s", purpose, w.Code, w.Body.String())
			}
		}
	})

	t.Run("bounds", func(t *testing.T) {
		sourceID, targetID, sourceTaskID, issueID := newCollaborationAgentPair(t)
		for _, tc := range []struct {
			name string
			key  string
			val  int
			want int
		}{
			{name: "max turns too high", key: "max_turns", val: 3, want: http.StatusForbidden},
			{name: "max turns non-positive", key: "max_turns", val: -1, want: http.StatusBadRequest},
			{name: "ttl too high", key: "ttl_minutes", val: 61, want: http.StatusForbidden},
			{name: "ttl non-positive", key: "ttl_minutes", val: -1, want: http.StatusBadRequest},
		} {
			t.Run(tc.name, func(t *testing.T) {
				body := validCollaborationBody(targetID)
				body[tc.key] = tc.val
				w := postCollaborationRequest(t, issueID, sourceID, sourceTaskID, body)
				if w.Code != tc.want {
					t.Fatalf("expected %d, got %d: %s", tc.want, w.Code, w.Body.String())
				}
			})
		}
	})
}

func TestCreateCollaborationRequestSuccessCreatesArtifactsAndList(t *testing.T) {
	requireCollaborationRequestTestTable(t)
	sourceID, targetID, sourceTaskID, issueID := newCollaborationAgentPair(t)

	w := postCollaborationRequest(t, issueID, sourceID, sourceTaskID, validCollaborationBody(targetID))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	created := decodeCollaborationResponse(t, w)
	if created.Status != "queued" || created.Mode != "discussion_only" {
		t.Fatalf("unexpected response status/mode: %+v", created)
	}
	if created.TriggerCommentID == nil || created.TargetTaskID == nil {
		t.Fatalf("expected trigger_comment_id and target_task_id to be set: %+v", created)
	}
	if created.FromAgentID != sourceID || created.ToAgentID != targetID || created.IssueID != issueID {
		t.Fatalf("unexpected response ids: %+v", created)
	}

	var auditCommentCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM comment
		WHERE id = $1 AND issue_id = $2 AND author_type = 'agent' AND type = 'system' AND content LIKE 'COLLABORATION_REQUEST%'
	`, *created.TriggerCommentID, issueID).Scan(&auditCommentCount); err != nil {
		t.Fatalf("count audit comments: %v", err)
	}
	if auditCommentCount != 1 {
		t.Fatalf("expected one audit comment, got %d", auditCommentCount)
	}
	if got := countCollaborationTargetTasks(t, issueID, targetID); got != 1 {
		t.Fatalf("expected one target task, got %d", got)
	}

	var activityCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM activity_log
		WHERE issue_id = $1 AND action = 'collaboration_request.created'
	`, issueID).Scan(&activityCount); err != nil {
		t.Fatalf("count activity rows: %v", err)
	}
	if activityCount != 1 {
		t.Fatalf("expected one activity row, got %d", activityCount)
	}

	list := listCollaborationRequests(t, issueID)
	if list.Code != http.StatusOK {
		t.Fatalf("list expected 200, got %d: %s", list.Code, list.Body.String())
	}
	var items []CollaborationRequestResponse
	if err := json.NewDecoder(list.Body).Decode(&items); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(items) == 0 || items[0].ID != created.ID {
		t.Fatalf("expected list to include created request, got %+v", items)
	}
}

func TestCreateCollaborationRequestRejectsDuplicateAndReverseCycle(t *testing.T) {
	requireCollaborationRequestTestTable(t)

	t.Run("duplicate active same direction", func(t *testing.T) {
		sourceID, targetID, sourceTaskID, issueID := newCollaborationAgentPair(t)
		w := postCollaborationRequest(t, issueID, sourceID, sourceTaskID, validCollaborationBody(targetID))
		if w.Code != http.StatusCreated {
			t.Fatalf("initial request expected 201, got %d: %s", w.Code, w.Body.String())
		}
		w = postCollaborationRequest(t, issueID, sourceID, sourceTaskID, validCollaborationBody(targetID))
		if w.Code != http.StatusConflict {
			t.Fatalf("duplicate expected 409, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("reverse active cycle", func(t *testing.T) {
		agentB := createCollaborationTestAgent(t, "Reviewer", collaborationRuntimeConfig())
		agentBName := collaborationAgentName(t, agentB)
		agentA := createCollaborationTestAgent(t, "Builder", collaborationRuntimeConfig(agentBName))
		agentAName := collaborationAgentName(t, agentA)
		if _, err := testPool.Exec(context.Background(), `UPDATE agent SET runtime_config = $1::jsonb WHERE id = $2`, collaborationRuntimeConfig(agentAName), agentB); err != nil {
			t.Fatalf("update reverse agent policy: %v", err)
		}
		issueID := createCollaborationTestIssue(t, "reverse cycle rejection")
		sourceTaskA := createCollaborationSourceTask(t, agentA, issueID, "running")
		w := postCollaborationRequest(t, issueID, agentA, sourceTaskA, validCollaborationBody(agentB))
		if w.Code != http.StatusCreated {
			t.Fatalf("initial request expected 201, got %d: %s", w.Code, w.Body.String())
		}
		created := decodeCollaborationResponse(t, w)
		if created.TargetTaskID == nil {
			t.Fatalf("expected target task id")
		}
		if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, *created.TargetTaskID); err != nil {
			t.Fatalf("mark target task running: %v", err)
		}
		w = postCollaborationRequest(t, issueID, agentB, *created.TargetTaskID, validCollaborationBody(agentA))
		if w.Code != http.StatusForbidden {
			t.Fatalf("reverse cycle expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestCollaborationTargetTaskCommentScope(t *testing.T) {
	requireCollaborationRequestTestTable(t)
	sourceID, targetID, sourceTaskID, issueID := newCollaborationAgentPair(t)
	w := postCollaborationRequest(t, issueID, sourceID, sourceTaskID, validCollaborationBody(targetID))
	if w.Code != http.StatusCreated {
		t.Fatalf("collaboration request expected 201, got %d: %s", w.Code, w.Body.String())
	}
	created := decodeCollaborationResponse(t, w)
	if created.TargetTaskID == nil {
		t.Fatalf("expected target task id")
	}
	targetTaskID := *created.TargetTaskID

	postTargetComment := func(issueID, content string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
			"content": content,
		})
		req = withURLParam(req, "id", issueID)
		req.Header.Set("X-Agent-ID", targetID)
		req.Header.Set("X-Task-ID", targetTaskID)
		testHandler.CreateComment(w, req)
		return w
	}

	w = postTargetComment(issueID, "Review complete: this stays on the requested issue.")
	if w.Code != http.StatusCreated {
		t.Fatalf("same issue target comment expected 201, got %d: %s", w.Code, w.Body.String())
	}

	otherIssueID := createCollaborationTestIssue(t, "forbidden different issue")
	w = postTargetComment(otherIssueID, "Trying to comment outside the requested issue.")
	if w.Code != http.StatusForbidden {
		t.Fatalf("different issue target comment expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = postTargetComment(issueID, "Please ask mention://agent/00000000-0000-0000-0000-000000000001 next.")
	if w.Code != http.StatusForbidden {
		t.Fatalf("agent mention target comment expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "agent") {
		t.Fatalf("expected agent mention error, got %s", w.Body.String())
	}
}
