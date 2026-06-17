package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
	created = publishKnowledgeForRAG(t, created.Item.ID)

	searchKnowledgeAndExpectFirst(t, "deadlock", created.Item.ID)
	searchKnowledgeAndExpectFirst(t, "short batches", created.Item.ID)
	searchKnowledgeAndExpectFirst(t, "table rewrites", created.Item.ID)

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/knowledge/"+created.Item.ID, map[string]any{
		"lifecycle_status": "published",
	})
	req = withURLParam(req, "id", created.Item.ID)
	testHandler.UpdateKnowledge(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateKnowledge lifecycle change: expected 400, got %d: %s", w.Code, w.Body.String())
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
	req = newRequest("POST", "/api/knowledge/"+itemID+"/review", nil)
	req = withURLParam(req, "id", itemID)
	testHandler.ReviewKnowledge(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("review source-less knowledge: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/knowledge/"+itemID+"/publish", nil)
	req = withURLParam(req, "id", itemID)
	testHandler.PublishKnowledge(w, req)
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
	created = publishKnowledgeForRAG(t, created.Item.ID)

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

func TestKnowledgeUsageFeedbackAndAnalytics(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Knowledge analytics runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Knowledge analytics agent")
	created := createKnowledgeFixture(t, map[string]any{
		"title":                "Knowledge analytics playbook",
		"type":                 "lesson",
		"confidence_status":    "high",
		"problem_pattern":      "Analytics task should cite reusable knowledge.",
		"recommended_practice": "Cite the knowledge ID when it was useful.",
		"sources": []map[string]any{{
			"source_type": "issue",
			"source_id":   issueID,
		}},
	})
	created = publishKnowledgeForRAG(t, created.Item.ID)

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '10 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("insert running task: %v", err)
	}
	result, _ := json.Marshal(TaskCompleteRequest{Output: "Done.\nUsed knowledge: " + created.Item.ID})
	if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), result, "", ""); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens)
		VALUES ($1, 'test', 'unit', 100, 50, 10, 5)
	`, taskID); err != nil {
		t.Fatalf("insert task usage: %v", err)
	}

	var missingTimeTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, completed_at)
		VALUES ($1, $2, $3, 'completed', 0, now())
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&missingTimeTaskID); err != nil {
		t.Fatalf("insert missing-time task: %v", err)
	}
	if _, err := testHandler.KnowledgeService.RecordUsage(ctx, service.KnowledgeUsageParams{
		WorkspaceID:     parseUUID(testWorkspaceID),
		KnowledgeItemID: parseUUID(created.Item.ID),
		AgentTaskID:     parseUUID(missingTimeTaskID),
		UsageSource:     "agent_reference",
		ReferenceText:   "Used knowledge: " + created.Item.ID,
		TaskStatus:      "completed",
	}); err != nil {
		t.Fatalf("record missing-time usage: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge/"+created.Item.ID+"/feedback", map[string]any{
		"value":         "helpful",
		"agent_task_id": taskID,
	})
	req = withURLParam(req, "id", created.Item.ID)
	testHandler.CreateKnowledgeFeedback(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateKnowledgeFeedback: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/knowledge/search", map[string]any{
		"query": "Knowledge analytics playbook",
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	testHandler.SearchKnowledge(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchKnowledge as agent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if _, err := testHandler.KnowledgeService.ListAnalytics(ctx, service.KnowledgeAnalyticsParams{
		WorkspaceID:     parseUUID(testWorkspaceID),
		KnowledgeItemID: parseUUID(created.Item.ID),
		Since:           time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		Until:           time.Now().Add(24 * time.Hour),
		IncludeZero:     true,
		Limit:           50,
	}); err != nil {
		t.Fatalf("ListAnalytics direct: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/knowledge/"+created.Item.ID+"/analytics?since=2000-01-01&include_zero=true", nil)
	req = withURLParam(req, "id", created.Item.ID)
	testHandler.GetKnowledgeAnalytics(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetKnowledgeAnalytics: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []KnowledgeAnalyticsRowResponse `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode analytics: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("analytics items = %d, want 1: %#v", len(resp.Items), resp.Items)
	}
	got := resp.Items[0]
	if got.UsageCount != 3 || got.AgentReferenceCount != 2 || got.ActiveSearchCount != 1 {
		t.Fatalf("usage counts = (%d, %d, %d), want (3, 2, 1): %#v", got.UsageCount, got.AgentReferenceCount, got.ActiveSearchCount, got)
	}
	if got.RetrievalCount < 1 {
		t.Fatalf("retrieval_count = %d, want at least 1: %#v", got.RetrievalCount, got)
	}
	if got.HelpfulCount != 1 {
		t.Fatalf("helpful_count = %d, want 1: %#v", got.HelpfulCount, got)
	}
	if got.SuccessfulTaskCount != 2 {
		t.Fatalf("successful_task_count = %d, want 2: %#v", got.SuccessfulTaskCount, got)
	}
	if got.TotalTaskSeconds < 590 || got.TotalTaskSeconds > 610 {
		t.Fatalf("total_task_seconds = %d, want only the 10-minute task counted: %#v", got.TotalTaskSeconds, got)
	}
	if got.TotalTokens != 165 {
		t.Fatalf("total_tokens = %d, want 165: %#v", got.TotalTokens, got)
	}
}

func TestKnowledgeDismissGovernanceClearsReviewFinding(t *testing.T) {
	created := createKnowledgeFixture(t, map[string]any{
		"title":                "Governance finding",
		"type":                 "lesson",
		"problem_pattern":      "A stale pattern should be manually reviewed.",
		"recommended_practice": "Review and either update or dismiss the finding.",
		"sources": []map[string]any{{
			"source_type": "commit",
			"source_url":  "https://example.com/commit/governance",
		}},
	})
	if _, err := testPool.Exec(context.Background(), `
		UPDATE knowledge_item
		SET review_reason = 'outdated feedback',
		    update_suggestion = 'refresh the diagnostic steps',
		    review_needed_at = now(),
		    conflict_group = 'conflict:test'
		WHERE id = $1
	`, created.Item.ID); err != nil {
		t.Fatalf("seed governance finding: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge/"+created.Item.ID+"/governance/dismiss", nil)
	req = withURLParam(req, "id", created.Item.ID)
	testHandler.DismissKnowledgeGovernance(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DismissKnowledgeGovernance: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp KnowledgeItemResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode dismiss response: %v", err)
	}
	if resp.ReviewReason != nil || resp.UpdateSuggestion != nil || resp.ReviewNeededAt != nil || resp.ConflictGroup != nil {
		t.Fatalf("governance finding was not cleared: %#v", resp)
	}
}

func TestKnowledgePublishWikiSkillTargetsAndSourceDetails(t *testing.T) {
	issueID := createKnowledgeCandidateTestIssue(t, "Knowledge source issue", "done", "medium")
	created := createKnowledgeFixture(t, map[string]any{
		"title":                "Android App Links verification",
		"type":                 "playbook",
		"problem_pattern":      "Android App Links can open the browser when assetlinks.json does not match the installed certificate.",
		"trigger_conditions":   "Use this when https issue links do not open the installed app.",
		"diagnostic_steps":     "Check pm get-app-links and dumpsys package.",
		"recommended_practice": "Compare installed signing certificate with the live assetlinks.json response.",
		"sources": []map[string]any{{
			"source_type":  "issue",
			"source_id":    issueID,
			"source_title": "Original App Links issue",
		}},
	})
	reviewKnowledge(t, created.Item.ID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge/"+created.Item.ID+"/publish/wiki", map[string]any{})
	req = withURLParam(req, "id", created.Item.ID)
	testHandler.PublishKnowledgeToWiki(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PublishKnowledgeToWiki: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var wikiResp KnowledgeDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&wikiResp); err != nil {
		t.Fatalf("decode wiki publish: %v", err)
	}
	if wikiResp.Item.LifecycleStatus != "published" {
		t.Fatalf("wiki publish lifecycle = %q, want published", wikiResp.Item.LifecycleStatus)
	}
	if !hasPublishTarget(wikiResp.PublishTargets, "wiki") {
		t.Fatalf("wiki publish targets = %#v, want wiki target", wikiResp.PublishTargets)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/knowledge/"+created.Item.ID+"/publish/skill", map[string]any{
		"name": "android-app-links-verification",
	})
	req = withURLParam(req, "id", created.Item.ID)
	testHandler.PublishKnowledgeToSkill(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PublishKnowledgeToSkill: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var skillResp KnowledgeDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&skillResp); err != nil {
		t.Fatalf("decode skill publish: %v", err)
	}
	if !hasPublishTarget(skillResp.PublishTargets, "wiki") || !hasPublishTarget(skillResp.PublishTargets, "skill") {
		t.Fatalf("skill publish targets = %#v, want wiki and skill targets", skillResp.PublishTargets)
	}
	var sourceMapCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM skill_file sf
		JOIN knowledge_publish_target kpt ON kpt.target_id = sf.skill_id
		WHERE kpt.knowledge_item_id = $1
		  AND kpt.target_type = 'skill'
		  AND sf.path = 'references/source-map.md'
	`, created.Item.ID).Scan(&sourceMapCount); err != nil {
		t.Fatalf("count skill source map: %v", err)
	}
	if sourceMapCount != 1 {
		t.Fatalf("source map files = %d, want 1", sourceMapCount)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/knowledge/"+created.Item.ID+"/sources", nil)
	req = withURLParam(req, "id", created.Item.ID)
	testHandler.GetKnowledgeSources(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetKnowledgeSources: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var sourcesResp struct {
		Sources []KnowledgeSourceDetailResponse `json:"sources"`
	}
	if err := json.NewDecoder(w.Body).Decode(&sourcesResp); err != nil {
		t.Fatalf("decode sources: %v", err)
	}
	if len(sourcesResp.Sources) != 1 || sourcesResp.Sources[0].ResolvedTitle == nil || *sourcesResp.Sources[0].ResolvedTitle != "Knowledge source issue" {
		t.Fatalf("source details = %#v, want resolved issue title", sourcesResp.Sources)
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
	first = publishKnowledgeForRAG(t, first.Item.ID)
	second = publishKnowledgeForRAG(t, second.Item.ID)

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

func hasPublishTarget(targets []KnowledgePublishTargetResponse, targetType string) bool {
	for _, target := range targets {
		if target.TargetType == targetType && target.TargetID != nil {
			return true
		}
	}
	return false
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

func TestKnowledgeDraftFromIssueCreatesDraftWithSources(t *testing.T) {
	issueID := createKnowledgeCandidateTestIssue(t, "Knowledge draft from issue", "done", "high")
	withStaticCuratorEngine(t, service.CuratorDraft{
		Title:               "Workspace token mismatch",
		Type:                "lesson",
		DomainLabels:        []string{"runtime", "workspace"},
		ProblemPattern:      "Agent task fails because the workspace token points at the wrong workspace.",
		TriggerConditions:   "A local run is healthy but task API calls return workspace-specific errors.",
		DiagnosticSteps:     "Check the active workspace id and token before debugging the runtime.",
		RecommendedPractice: "Refresh the CLI profile and rerun the task after confirming the workspace id.",
		AntiPatterns:        "Do not assume localhost health means the CLI profile is correct.",
		Applicability:       "Use when a task failure mentions workspace or token mismatch.",
		ConfidenceStatus:    "medium",
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge/drafts/from-issue", map[string]any{"issue_id": issueID})
	testHandler.CreateKnowledgeDraftFromIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateKnowledgeDraftFromIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp KnowledgeDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode draft: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM knowledge_item WHERE id = $1`, resp.Item.ID)
	})
	if resp.Item.LifecycleStatus != "draft" || resp.Item.Title != "Workspace token mismatch" {
		t.Fatalf("draft item = %#v", resp.Item)
	}
	if len(resp.Sources) == 0 || resp.Sources[0].SourceType != "issue" || resp.Sources[0].SourceID == nil || *resp.Sources[0].SourceID != issueID {
		t.Fatalf("draft sources = %#v", resp.Sources)
	}
}

func TestKnowledgeDraftFromCandidateFailureRecordsRetryState(t *testing.T) {
	issueID := createKnowledgeCandidateTestIssue(t, "Knowledge draft invalid schema", "done", "medium")
	candidate := evaluateManualKnowledgeCandidate(t, issueID)
	withStaticCuratorEngine(t, service.CuratorDraft{
		Title:             "Invalid draft",
		Type:              "lesson",
		ProblemPattern:    "Missing required fields.",
		ConfidenceStatus:  "medium",
		Applicability:     "Tests",
		DiagnosticSteps:   "Look at the response.",
		TriggerConditions: "A malformed engine response.",
	})

	before := countKnowledgeItems(t)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge/candidates/"+candidate.ID+"/draft", map[string]any{})
	req = withURLParam(req, "id", candidate.ID)
	testHandler.CreateKnowledgeDraftFromCandidate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateKnowledgeDraftFromCandidate invalid: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if after := countKnowledgeItems(t); after != before {
		t.Fatalf("knowledge items after invalid draft = %d, want %d", after, before)
	}
	stored := loadKnowledgeCandidateResponse(t, candidate.ID)
	if stored.Status != "accepted" {
		t.Fatalf("candidate status after failure = %q, want accepted", stored.Status)
	}
	meta := stored.Metadata.(map[string]any)
	if meta["draft_error"] == nil {
		t.Fatalf("candidate metadata missing draft_error: %#v", meta)
	}
}

func TestKnowledgeDraftFromCandidateRetrySucceeds(t *testing.T) {
	issueID := createKnowledgeCandidateTestIssue(t, "Knowledge draft retry", "done", "medium")
	candidate := evaluateManualKnowledgeCandidate(t, issueID)
	withStaticCuratorEngine(t, service.CuratorDraft{
		Title:               "Retry succeeds",
		Type:                "invalid",
		ProblemPattern:      "Invalid enum.",
		TriggerConditions:   "The engine returns an invalid type.",
		DiagnosticSteps:     "Validate schema.",
		RecommendedPractice: "Return a valid knowledge type.",
		Applicability:       "Tests",
		ConfidenceStatus:    "medium",
	})
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge/candidates/"+candidate.ID+"/draft", map[string]any{})
	req = withURLParam(req, "id", candidate.ID)
	testHandler.CreateKnowledgeDraftFromCandidate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("first draft attempt: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	withStaticCuratorEngine(t, service.CuratorDraft{
		Title:               "Retry succeeds",
		Type:                "lesson",
		ProblemPattern:      "The first curator response can be invalid.",
		TriggerConditions:   "A retry is requested after draft_error is recorded.",
		DiagnosticSteps:     "Inspect candidate metadata and rerun generation.",
		RecommendedPractice: "Keep the candidate accepted and allow another draft attempt.",
		AntiPatterns:        "Do not mark failed generation as drafted.",
		Applicability:       "Knowledge candidate draft retries.",
		ConfidenceStatus:    "high",
	})
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/knowledge/candidates/"+candidate.ID+"/draft", map[string]any{})
	req = withURLParam(req, "id", candidate.ID)
	testHandler.CreateKnowledgeDraftFromCandidate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("retry draft attempt: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp KnowledgeDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode retry draft: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM knowledge_item WHERE id = $1`, resp.Item.ID)
	})
	stored := loadKnowledgeCandidateResponse(t, candidate.ID)
	if stored.Status != "drafted" {
		t.Fatalf("candidate status after retry = %q, want drafted", stored.Status)
	}
	meta := stored.Metadata.(map[string]any)
	if meta["knowledge_item_id"] != resp.Item.ID || meta["draft_error"] != nil {
		t.Fatalf("candidate metadata after retry = %#v, draft id %s", meta, resp.Item.ID)
	}
}

func TestKnowledgeDraftFromCandidateMissingEngineIsDiagnostic(t *testing.T) {
	issueID := createKnowledgeCandidateTestIssue(t, "Knowledge draft missing engine", "done", "medium")
	candidate := evaluateManualKnowledgeCandidate(t, issueID)
	testHandler.KnowledgeCurator.Engine = service.MissingCuratorEngine{}
	before := countKnowledgeItems(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge/candidates/"+candidate.ID+"/draft", map[string]any{})
	req = withURLParam(req, "id", candidate.ID)
	testHandler.CreateKnowledgeDraftFromCandidate(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing curator engine: expected 503, got %d: %s", w.Code, w.Body.String())
	}
	if after := countKnowledgeItems(t); after != before {
		t.Fatalf("knowledge items after missing engine = %d, want %d", after, before)
	}
	stored := loadKnowledgeCandidateResponse(t, candidate.ID)
	meta := stored.Metadata.(map[string]any)
	if meta["draft_error"] == nil {
		t.Fatalf("candidate metadata missing draft_error: %#v", meta)
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

func reviewKnowledge(t *testing.T, itemID string) KnowledgeItemResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge/"+itemID+"/review", nil)
	req = withURLParam(req, "id", itemID)
	testHandler.ReviewKnowledge(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ReviewKnowledge: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp KnowledgeItemResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode ReviewKnowledge: %v", err)
	}
	return resp
}

func publishKnowledgeForRAG(t *testing.T, itemID string) KnowledgeDetailResponse {
	t.Helper()
	reviewKnowledge(t, itemID)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/knowledge/"+itemID+"/publish", nil)
	req = withURLParam(req, "id", itemID)
	testHandler.PublishKnowledge(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PublishKnowledge: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp KnowledgeDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode PublishKnowledge: %v", err)
	}
	return resp
}

func withStaticCuratorEngine(t *testing.T, draft service.CuratorDraft) {
	t.Helper()
	previous := testHandler.KnowledgeCurator.Engine
	testHandler.KnowledgeCurator.Engine = service.StaticCuratorEngine{
		Draft:   draft,
		Summary: "Curator test summary",
		Engine:  service.CuratorEngineInfo{Provider: "test", Model: "unit"},
	}
	t.Cleanup(func() {
		testHandler.KnowledgeCurator.Engine = previous
	})
}

func evaluateManualKnowledgeCandidate(t *testing.T, issueID string) KnowledgeCandidateResponse {
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

func loadKnowledgeCandidateResponse(t *testing.T, id string) KnowledgeCandidateResponse {
	t.Helper()
	candidate, err := testHandler.Queries.GetKnowledgeCandidate(context.Background(), db.GetKnowledgeCandidateParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load candidate: %v", err)
	}
	return knowledgeCandidateToResponse(candidate)
}

func countKnowledgeItems(t *testing.T) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_item WHERE workspace_id = $1`, testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count knowledge items: %v", err)
	}
	return count
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
