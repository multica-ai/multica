package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	seedTestInstallTitle = "Seed Test Step 1 — Connect a runtime"
	seedTestGuideTitle   = "Seed Test Step 2 — Create your first agent"
	zeroUUIDString       = "00000000-0000-0000-0000-000000000000"
)

func seedTestRequestBody() map[string]any {
	return map[string]any{
		"workspace_id": testWorkspaceID,
		"install_issue": map[string]string{
			"title":       seedTestInstallTitle,
			"description": "Install a runtime first.",
		},
		"agent_guide_issue": map[string]string{
			"title":       seedTestGuideTitle,
			"description": "Once the runtime is online (see {{install_issue_ref}}), create an agent.",
		},
		"followup_comment": map[string]string{
			"content": "Your next step: {{agent_guide_ref}}",
		},
	}
}

func cleanupSeedTestBundle(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	clean := func() {
		testPool.Exec(ctx, `
			DELETE FROM comment WHERE issue_id IN (
				SELECT id FROM issue WHERE workspace_id = $1 AND title IN ($2, $3)
			)
		`, testWorkspaceID, seedTestInstallTitle, seedTestGuideTitle)
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND title IN ($2, $3)`,
			testWorkspaceID, seedTestInstallTitle, seedTestGuideTitle,
		)
	}
	clean()
	t.Cleanup(clean)
}

func TestSeedOnboardingNoRuntimeCreatesSystemAttributedBundle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	cleanupSeedTestBundle(t)

	w := httptest.NewRecorder()
	testHandler.SeedOnboardingNoRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/no-runtime-seed", seedTestRequestBody()))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp seedOnboardingNoRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.InstallIssue.ID == "" || resp.AgentGuideIssue.ID == "" {
		t.Fatalf("expected both issue ids, got %+v", resp)
	}

	type issueRow struct {
		creatorType  string
		creatorID    string
		assigneeType string
		assigneeID   string
		status       string
		priority     string
		description  string
	}
	loadIssue := func(id string) issueRow {
		var row issueRow
		if err := testPool.QueryRow(ctx, `
			SELECT creator_type, creator_id, assignee_type, assignee_id, status, priority, description
			  FROM issue WHERE id = $1
		`, id).Scan(&row.creatorType, &row.creatorID, &row.assigneeType, &row.assigneeID, &row.status, &row.priority, &row.description); err != nil {
			t.Fatalf("load issue %s: %v", id, err)
		}
		return row
	}

	install := loadIssue(resp.InstallIssue.ID)
	if install.creatorType != "system" || install.creatorID != zeroUUIDString {
		t.Fatalf("install creator = %s/%s, want system/zero-uuid", install.creatorType, install.creatorID)
	}
	if install.assigneeType != "member" || install.assigneeID != testUserID {
		t.Fatalf("install assignee = %s/%s, want member/%s", install.assigneeType, install.assigneeID, testUserID)
	}
	if install.status != "in_progress" || install.priority != "high" {
		t.Fatalf("install status/priority = %s/%s, want in_progress/high", install.status, install.priority)
	}

	guide := loadIssue(resp.AgentGuideIssue.ID)
	if guide.creatorType != "system" || guide.creatorID != zeroUUIDString {
		t.Fatalf("guide creator = %s/%s, want system/zero-uuid", guide.creatorType, guide.creatorID)
	}
	if guide.status != "todo" || guide.priority != "medium" {
		t.Fatalf("guide status/priority = %s/%s, want todo/medium", guide.status, guide.priority)
	}
	wantInstallChip := "[" + resp.InstallIssue.Identifier + "](mention://issue/" + resp.InstallIssue.ID + ")"
	if !strings.Contains(guide.description, wantInstallChip) {
		t.Fatalf("guide description should embed install mention chip %q, got %q", wantInstallChip, guide.description)
	}
	if strings.Contains(guide.description, "{{install_issue_ref}}") {
		t.Fatalf("guide description still contains raw placeholder: %q", guide.description)
	}

	var (
		commentCount int
		authorType   string
		authorID     string
		commentType  string
		commentBody  string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM comment WHERE issue_id = $1
	`, resp.InstallIssue.ID).Scan(&commentCount); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("expected exactly 1 follow-up comment, got %d", commentCount)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT author_type, author_id, type, content FROM comment WHERE issue_id = $1
	`, resp.InstallIssue.ID).Scan(&authorType, &authorID, &commentType, &commentBody); err != nil {
		t.Fatalf("load comment: %v", err)
	}
	if authorType != "system" || authorID != zeroUUIDString {
		t.Fatalf("comment author = %s/%s, want system/zero-uuid", authorType, authorID)
	}
	if commentType != "comment" {
		t.Fatalf("comment type = %s, want comment", commentType)
	}
	wantGuideChip := "[" + resp.AgentGuideIssue.Identifier + "](mention://issue/" + resp.AgentGuideIssue.ID + ")"
	if !strings.Contains(commentBody, wantGuideChip) {
		t.Fatalf("comment should embed guide mention chip %q, got %q", wantGuideChip, commentBody)
	}

	// Re-entry (StrictMode double effect / client retry): same rows come
	// back, no duplicate issues, no stacked follow-up comment.
	w2 := httptest.NewRecorder()
	testHandler.SeedOnboardingNoRuntime(w2, newRequest(http.MethodPost, "/api/me/onboarding/no-runtime-seed", seedTestRequestBody()))
	if w2.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 seedOnboardingNoRuntimeResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if resp2.InstallIssue.ID != resp.InstallIssue.ID || resp2.AgentGuideIssue.ID != resp.AgentGuideIssue.ID {
		t.Fatalf("seed should be idempotent: first=%+v second=%+v", resp, resp2)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM comment WHERE issue_id = $1
	`, resp.InstallIssue.ID).Scan(&commentCount); err != nil {
		t.Fatalf("recount comments: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("re-entry should not add another follow-up comment, got %d", commentCount)
	}
}

func TestSeedOnboardingNoRuntimeRejectsNonMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	cleanupSeedTestBundle(t)

	var outsiderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Seed Outsider', 'seed-outsider@multica.ai') RETURNING id
	`).Scan(&outsiderID); err != nil {
		t.Fatalf("insert outsider: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, outsiderID)
	})

	w := httptest.NewRecorder()
	testHandler.SeedOnboardingNoRuntime(w, newRequestAs(outsiderID, http.MethodPost, "/api/me/onboarding/no-runtime-seed", seedTestRequestBody()))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %d: %s", w.Code, w.Body.String())
	}

	var issueCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue WHERE workspace_id = $1 AND title IN ($2, $3)`,
		testWorkspaceID, seedTestInstallTitle, seedTestGuideTitle,
	).Scan(&issueCount); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if issueCount != 0 {
		t.Fatalf("non-member call must not create issues, got %d", issueCount)
	}
}

func TestSeedOnboardingNoRuntimeValidatesContent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupSeedTestBundle(t)

	mutate := func(fn func(m map[string]any)) map[string]any {
		body := seedTestRequestBody()
		fn(body)
		return body
	}
	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing install title", mutate(func(m map[string]any) {
			m["install_issue"].(map[string]string)["title"] = "  "
		})},
		{"missing guide description", mutate(func(m map[string]any) {
			m["agent_guide_issue"].(map[string]string)["description"] = ""
		})},
		{"missing comment content", mutate(func(m map[string]any) {
			m["followup_comment"].(map[string]string)["content"] = ""
		})},
		{"overlong title", mutate(func(m map[string]any) {
			m["install_issue"].(map[string]string)["title"] = strings.Repeat("长", seedTitleMaxRunes+1)
		})},
		{"missing workspace", mutate(func(m map[string]any) {
			m["workspace_id"] = ""
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			testHandler.SeedOnboardingNoRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/no-runtime-seed", tc.body))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
