package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
)

func TestKnowledgeCreateRequiresSource(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge", map[string]any{
		"title": "Missing source",
		"type":  "lesson",
	})
	testHandler.CreateKnowledge(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateKnowledge without source: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestKnowledgeLifecycleListSearchFeedbackAndArchive(t *testing.T) {
	created := createKnowledgeFixture(t, map[string]any{
		"title":                "Deadlock after migration",
		"type":                 "lesson",
		"domain_labels":        []string{"database", "migration"},
		"problem_pattern":      "A migration can deadlock when a long transaction holds a lock.",
		"recommended_practice": "Use short batches and verify lock wait before rollout.",
		"anti_patterns":        "Do not run broad table rewrites during peak traffic.",
		"sources": []map[string]any{{
			"source_type": "commit",
			"source_url":  "https://example.com/commit/deadlock-fix",
		}},
	})

	searchKnowledgeAndExpectFirst(t, "deadlock", created.Item.ID)
	searchKnowledgeAndExpectFirst(t, "short batches", created.Item.ID)
	searchKnowledgeAndExpectFirst(t, "table rewrites", created.Item.ID)

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/knowledge/"+created.Item.ID, map[string]any{
		"lifecycle_status": "published",
	})
	req = withURLParam(req, "id", created.Item.ID)
	testHandler.UpdateKnowledge(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateKnowledge publish: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/knowledge/"+created.Item.ID+"/feedback", map[string]any{
		"value": "helpful",
		"note":  "Used during rollout.",
	})
	req = withURLParam(req, "id", created.Item.ID)
	testHandler.CreateKnowledgeFeedback(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateKnowledgeFeedback: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/knowledge/"+created.Item.ID, nil)
	req = withURLParam(req, "id", created.Item.ID)
	testHandler.DeleteKnowledge(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteKnowledge: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT lifecycle_status FROM knowledge_item WHERE id = $1`, created.Item.ID).Scan(&status); err != nil {
		t.Fatalf("load archived knowledge: %v", err)
	}
	if status != "archived" {
		t.Fatalf("lifecycle_status after delete = %q, want archived", status)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/knowledge", nil)
	testHandler.ListKnowledge(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListKnowledge: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list struct {
		Items []KnowledgeItemResponse `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, item := range list.Items {
		if item.ID == created.Item.ID {
			t.Fatalf("archived knowledge item was returned by default list")
		}
	}
}

func TestKnowledgeRejectsInvalidEnumsAndPublishingWithoutSource(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge", map[string]any{
		"title": "Invalid enum",
		"type":  "runbook",
		"sources": []map[string]any{{
			"source_type": "commit",
			"source_url":  "https://example.com/commit/enum",
		}},
	})
	testHandler.CreateKnowledge(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateKnowledge invalid enum: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var itemID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO knowledge_item (workspace_id, title, type, confidence_status, lifecycle_status)
		VALUES ($1, 'No source yet', 'lesson', 'medium', 'draft')
		RETURNING id
	`, testWorkspaceID).Scan(&itemID); err != nil {
		t.Fatalf("insert source-less knowledge: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM knowledge_item WHERE id = $1`, itemID)
	})

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/knowledge/"+itemID, map[string]any{
		"lifecycle_status": "published",
	})
	req = withURLParam(req, "id", itemID)
	testHandler.UpdateKnowledge(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("publish without source: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestKnowledgeWorkspaceIsolation(t *testing.T) {
	created := createKnowledgeFixture(t, map[string]any{
		"title":           "Visible workspace knowledge",
		"type":            "reference",
		"problem_pattern": "workspace-visible-token",
		"sources": []map[string]any{{
			"source_type": "commit",
			"source_url":  "https://example.com/commit/visible",
		}},
	})

	otherWorkspaceID := createOtherWorkspaceKnowledge(t, "hidden-workspace-token")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge/search", map[string]any{
		"query": "visible-token",
	})
	testHandler.SearchKnowledge(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchKnowledge: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Results []KnowledgeSearchResultResponse `json:"results"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Item.ID != created.Item.ID {
		t.Fatalf("workspace search returned %#v, want only %s", resp.Results, created.Item.ID)
	}

	var hiddenCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_item WHERE workspace_id = $1`, otherWorkspaceID).Scan(&hiddenCount); err != nil {
		t.Fatalf("count other workspace knowledge: %v", err)
	}
	if hiddenCount != 1 {
		t.Fatalf("other workspace fixture missing")
	}
}

func TestKnowledgeVectorSearchOrdersByCosineSimilarity(t *testing.T) {
	first := createKnowledgeFixture(t, map[string]any{
		"title": "Vector first",
		"type":  "lesson",
		"sources": []map[string]any{{
			"source_type": "commit",
			"source_url":  "https://example.com/commit/vector-first",
		}},
	})
	second := createKnowledgeFixture(t, map[string]any{
		"title": "Vector second",
		"type":  "lesson",
		"sources": []map[string]any{{
			"source_type": "commit",
			"source_url":  "https://example.com/commit/vector-second",
		}},
	})

	firstVector := make([]float32, service.KnowledgeEmbeddingDimensions)
	firstVector[0] = 1
	secondVector := make([]float32, service.KnowledgeEmbeddingDimensions)
	secondVector[1] = 1
	itemID, ok := parseUUIDForTest(first.Item.ID)
	if !ok {
		t.Fatalf("invalid first id")
	}
	if _, err := testHandler.KnowledgeService.UpsertEmbedding(context.Background(), itemID, parseUUID(testWorkspaceID), "test", "unit", "first", firstVector); err != nil {
		t.Fatalf("upsert first embedding: %v", err)
	}
	itemID, ok = parseUUIDForTest(second.Item.ID)
	if !ok {
		t.Fatalf("invalid second id")
	}
	if _, err := testHandler.KnowledgeService.UpsertEmbedding(context.Background(), itemID, parseUUID(testWorkspaceID), "test", "unit", "second", secondVector); err != nil {
		t.Fatalf("upsert second embedding: %v", err)
	}

	results, err := testHandler.KnowledgeService.Search(context.Background(), service.KnowledgeSearchParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		MemberID:    handlerTestMemberID(t),
		Embedding:   firstVector,
		Limit:       2,
	})
	if err != nil {
		t.Fatalf("vector search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("vector search returned %d results, want at least 2", len(results))
	}
	if uuidToString(results[0].Item.ID) != first.Item.ID {
		t.Fatalf("top vector result = %s, want %s", uuidToString(results[0].Item.ID), first.Item.ID)
	}
	if results[0].VectorScore <= results[1].VectorScore {
		t.Fatalf("vector scores not ordered: %f <= %f", results[0].VectorScore, results[1].VectorScore)
	}
}

func TestKnowledgeCandidateManualEvaluateAcceptedAndDeduped(t *testing.T) {
	issueID := createKnowledgeCandidateTestIssue(t, "Knowledge candidate manual", "todo", "medium")

	evaluate := func() KnowledgeCandidateResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/knowledge/candidates/evaluate", map[string]any{
			"source_type": "issue",
			"source_id":   issueID,
		})
		testHandler.EvaluateKnowledgeCandidate(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("EvaluateKnowledgeCandidate: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp KnowledgeCandidateResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode candidate: %v", err)
		}
		return resp
	}

	first := evaluate()
	second := evaluate()
	if first.ID != second.ID {
		t.Fatalf("manual candidate duplicated: first=%s second=%s", first.ID, second.ID)
	}
	if first.Status != "accepted" || first.SignalStrength != "manual" || first.Score != 100 {
		t.Fatalf("manual candidate = status %q strength %q score %d", first.Status, first.SignalStrength, first.Score)
	}
}

func TestKnowledgeCandidateIssueDoneWithoutAgentTaskRejected(t *testing.T) {
	issueID := createKnowledgeCandidateTestIssue(t, "Knowledge candidate no agent task", "todo", "low")

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/issues/"+issueID, map[string]any{
		"status": "done",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue done: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/knowledge/candidates?issue_id="+issueID, nil)
	testHandler.ListKnowledgeCandidates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListKnowledgeCandidates: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Candidates []KnowledgeCandidateResponse `json:"candidates"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode candidates: %v", err)
	}
	if len(resp.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(resp.Candidates))
	}
	candidate := resp.Candidates[0]
	if candidate.Status != "rejected" || candidate.SignalStrength != "none" {
		t.Fatalf("done-without-task candidate = status %q strength %q", candidate.Status, candidate.SignalStrength)
	}
}

func TestKnowledgeCandidateRetryFollowUpSuccessStrong(t *testing.T) {
	issueID := createKnowledgeCandidateTestIssue(t, "Knowledge candidate retry success", "in_progress", "high")
	agentID := createHandlerTestAgent(t, "knowledge-candidate-agent", nil)

	var commentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, '还是失败，正确应该是先检查 workspace token')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert trigger comment: %v", err)
	}

	var parentTaskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, error, failure_reason
		)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '10 minutes', now() - interval '9 minutes', 'runtime failed', 'timeout')
		RETURNING id
	`, agentID, handlerTestRuntimeID(t), issueID).Scan(&parentTaskID); err != nil {
		t.Fatalf("insert parent task: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, trigger_comment_id,
			started_at, completed_at, result, attempt, parent_task_id
		)
		VALUES (
			$1, $2, $3, 'completed', 0, $4,
			now() - interval '8 minutes', now(), $5::jsonb, 2, $6
		)
		RETURNING id
	`, agentID, handlerTestRuntimeID(t), issueID, commentID, `{"output":"Root cause: workspace token mismatch. Fix: refresh config and rerun test command."}`, parentTaskID).Scan(&taskID); err != nil {
		t.Fatalf("insert completed retry task: %v", err)
	}

	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	candidate, err := testHandler.KnowledgeService.EvaluateTaskCompletedCandidate(context.Background(), task, task.Result)
	if err != nil {
		t.Fatalf("EvaluateTaskCompletedCandidate: %v", err)
	}
	if candidate.Status != "accepted" || candidate.SignalStrength != "strong" || candidate.Score < 80 {
		t.Fatalf("candidate = status %q strength %q score %d", candidate.Status, candidate.SignalStrength, candidate.Score)
	}
	wantSignals := map[string]bool{"retry_success": true, "follow_up_task_success": true, "user_correction": true}
	for _, signal := range candidate.Signals {
		delete(wantSignals, signal)
	}
	if len(wantSignals) != 0 {
		t.Fatalf("candidate missing signals: %#v; got %#v", wantSignals, candidate.Signals)
	}
}

func createKnowledgeFixture(t *testing.T, body map[string]any) KnowledgeDetailResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge", body)
	testHandler.CreateKnowledge(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateKnowledge: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp KnowledgeDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode CreateKnowledge: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM knowledge_item WHERE id = $1`, resp.Item.ID)
	})
	return resp
}

func createKnowledgeCandidateTestIssue(t *testing.T, title, status, priority string) string {
	t.Helper()
	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, title+" project").Scan(&projectID); err != nil {
		t.Fatalf("insert knowledge candidate project: %v", err)
	}
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (
			workspace_id, project_id, title, status, priority, creator_type, creator_id, number, position
		)
		VALUES (
			$1, $2, $3, $4, $5, 'member', $6,
			COALESCE((SELECT MAX(number) + 1 FROM issue WHERE workspace_id = $1), 1),
			0
		)
		RETURNING id
	`, testWorkspaceID, projectID, title, status, priority, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert knowledge candidate issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	return issueID
}

func searchKnowledgeAndExpectFirst(t *testing.T, query string, itemID string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge/search", map[string]any{"query": query})
	testHandler.SearchKnowledge(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchKnowledge(%q): expected 200, got %d: %s", query, w.Code, w.Body.String())
	}
	var resp struct {
		Results []KnowledgeSearchResultResponse `json:"results"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(resp.Results) == 0 || resp.Results[0].Item.ID != itemID {
		t.Fatalf("SearchKnowledge(%q) first result = %#v, want %s", query, resp.Results, itemID)
	}
}

func createOtherWorkspaceKnowledge(t *testing.T, token string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Knowledge Other User', 'knowledge-other-' || gen_random_uuid()::text || '@example.com')
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatalf("insert other user: %v", err)
	}
	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix)
		VALUES ('Knowledge Other Workspace', 'knowledge-other-' || replace(gen_random_uuid()::text, '-', ''), 'KNO')
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("insert other workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, userID); err != nil {
		t.Fatalf("insert other member: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		WITH item AS (
			INSERT INTO knowledge_item (workspace_id, title, type, problem_pattern, confidence_status, lifecycle_status)
			VALUES ($1, 'Hidden knowledge', 'lesson', $2, 'medium', 'draft')
			RETURNING id, workspace_id
		)
		INSERT INTO knowledge_source (knowledge_item_id, workspace_id, source_type, source_url)
		SELECT id, workspace_id, 'commit', 'https://example.com/commit/hidden'
		FROM item
	`, workspaceID, token); err != nil {
		t.Fatalf("insert other knowledge: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return workspaceID
}

func parseUUIDForTest(s string) (pgtype.UUID, bool) {
	u := parseUUID(s)
	return u, u.Valid
}

func handlerTestMemberID(t *testing.T) pgtype.UUID {
	t.Helper()
	var memberID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id
		FROM member
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID).Scan(&memberID); err != nil {
		t.Fatalf("load handler test member: %v", err)
	}
	return parseUUID(memberID)
}
