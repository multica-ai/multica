package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testEmbedding(first float64) []float64 {
	v := make([]float64, skillEmbeddingDimension)
	v[0] = first
	v[1] = 1 - first
	return v
}

func createDoneIssueForSkillTest(t *testing.T, title string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "done",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, resp.ID)
	})
	return resp.ID
}

func TestCreateIssueSkillRejectsInvalidIssueID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/not-a-uuid/skills", map[string]any{
		"name": "bad-id-skill",
	})
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.CreateIssueSkill(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateIssueSkillCrossWorkspaceIs404(t *testing.T) {
	ctx := context.Background()
	var otherWorkspaceID, issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix)
		VALUES ('Issue Skill Other', 'issue-skill-other', 'ISO')
		RETURNING id
	`).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, status, number)
		VALUES ($1, 'member', $2, 'foreign done issue', 'done', 1)
		RETURNING id
	`, otherWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create foreign issue: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/skills", map[string]any{
		"name":    "cross-workspace-skill",
		"content": "content",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateIssueSkill(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateIssueSkillRequiresDoneIssue(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "todo skill source",
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	w = httptest.NewRecorder()
	req = newRequest(http.MethodPost, "/api/issues/"+issue.ID+"/skills", map[string]any{
		"name":    "todo-issue-skill",
		"content": "content",
	})
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateIssueSkill(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM skill WHERE workspace_id = $1 AND name = 'todo-issue-skill'`,
		testWorkspaceID,
	).Scan(&count); err != nil {
		t.Fatalf("count skill: %v", err)
	}
	if count != 0 {
		t.Fatalf("non-done issue created %d skills", count)
	}
}

func TestCreateIssueSkillStoresSourceEmbeddingAndPreservesListShape(t *testing.T) {
	issueID := createDoneIssueForSkillTest(t, "done issue skill source")
	name := "issue-sourced-skill-shape"

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/skills", CreateIssueSkillRequest{
		CreateSkillRequest: CreateSkillRequest{
			Name:        name,
			Description: "from issue",
			Content:     "secret list payload must not include this",
			Files: []CreateSkillFileRequest{
				{Path: "README.md", Content: "readme"},
			},
		},
		Embedding: &SkillEmbeddingRequest{
			Model:       "test-model",
			Vector:      testEmbedding(1),
			ContentHash: "hash-1",
		},
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateIssueSkill(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssueSkill: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp IssueSkillResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SourceIssueID != issueID || !resp.EmbeddingStored {
		t.Fatalf("source/embedding mismatch: %+v", resp)
	}

	var sourceCount, embeddingCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue_skill_source WHERE skill_id = $1 AND issue_id = $2 AND workspace_id = $3`,
		resp.Skill.ID, issueID, testWorkspaceID,
	).Scan(&sourceCount); err != nil {
		t.Fatalf("count source: %v", err)
	}
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM skill_embedding WHERE skill_id = $1 AND workspace_id = $2 AND embedding_model = 'test-model'`,
		resp.Skill.ID, testWorkspaceID,
	).Scan(&embeddingCount); err != nil {
		t.Fatalf("count embedding: %v", err)
	}
	if sourceCount != 1 || embeddingCount != 1 {
		t.Fatalf("sourceCount=%d embeddingCount=%d, want 1/1", sourceCount, embeddingCount)
	}

	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/skills?workspace_id="+testWorkspaceID, nil)
	testHandler.ListSkills(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListSkills: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "secret list payload") {
		t.Fatalf("skill list leaked content: %s", w.Body.String())
	}
}

func TestSearchSkillEmbeddingsReturnsNearestInWorkspace(t *testing.T) {
	issueA := createDoneIssueForSkillTest(t, "vector source a")
	issueB := createDoneIssueForSkillTest(t, "vector source b")
	for i, tc := range []struct {
		issueID string
		name    string
		first   float64
	}{
		{issueA, "vector-far-skill", 1},
		{issueB, "vector-near-skill", 2},
	} {
		w := httptest.NewRecorder()
		req := newRequest(http.MethodPost, "/api/issues/"+tc.issueID+"/skills", CreateIssueSkillRequest{
			CreateSkillRequest: CreateSkillRequest{
				Name:    fmt.Sprintf("%s-%d", tc.name, i),
				Content: "content",
			},
			Embedding: &SkillEmbeddingRequest{
				Model:  "test-search-model",
				Vector: testEmbedding(tc.first),
			},
		})
		req = withURLParam(req, "id", tc.issueID)
		testHandler.CreateIssueSkill(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssueSkill %s: expected 201, got %d: %s", tc.name, w.Code, w.Body.String())
		}
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/skills/vector-search", SkillVectorSearchRequest{
		Embedding: SkillEmbeddingRequest{
			Model:  "test-search-model",
			Vector: testEmbedding(2),
		},
		Limit: 2,
	})
	testHandler.SearchSkillEmbeddings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchSkillEmbeddings: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []SkillVectorSearchResult
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(resp), resp)
	}
	if !strings.Contains(resp[0].Name, "vector-near-skill") {
		t.Fatalf("nearest result first = %q, want vector-near-skill", resp[0].Name)
	}
	if resp[0].SourceIssueID == nil || *resp[0].SourceIssueID != issueB {
		t.Fatalf("source issue = %v, want %s", resp[0].SourceIssueID, issueB)
	}
}
