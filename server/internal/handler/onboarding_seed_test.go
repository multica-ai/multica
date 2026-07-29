package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const zeroUUIDString = "00000000-0000-0000-0000-000000000000"

type noRuntimeTestFixture struct {
	userID      string
	workspaceID string
}

func newNoRuntimeTestFixture(t *testing.T, role string) noRuntimeTestFixture {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()

	var fixture noRuntimeTestFixture
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('No Runtime Test', $1)
		RETURNING id
	`, "no-runtime-"+suffix+"@multica.ai").Scan(&fixture.userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('No Runtime Test', $1, '', 'NRT')
		RETURNING id
	`, "no-runtime-"+suffix).Scan(&fixture.workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
	`, fixture.workspaceID, fixture.userID, role); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1`, fixture.workspaceID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE workspace_id = $1`, fixture.workspaceID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, fixture.workspaceID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, fixture.userID)
	})
	return fixture
}

func noRuntimeRequestBody(workspaceID, locale string) map[string]any {
	return map[string]any{
		"workspace_id": workspaceID,
		"locale":       locale,
	}
}

func completeNoRuntimeForTest(t *testing.T, fixture noRuntimeTestFixture, locale string) (*httptest.ResponseRecorder, completeOnboardingNoRuntimeResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CompleteOnboardingNoRuntime(w, newRequestAs(
		fixture.userID,
		http.MethodPost,
		"/api/me/onboarding/no-runtime-complete",
		noRuntimeRequestBody(fixture.workspaceID, locale),
	))
	var response completeOnboardingNoRuntimeResponse
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return w, response
}

func TestCompleteOnboardingNoRuntimeCreatesServerOwnedSystemBundle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fixture := newNoRuntimeTestFixture(t, "owner")

	w, response := completeNoRuntimeForTest(t, fixture, "en")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if response.User.OnboardedAt == nil {
		t.Fatal("expected onboarding to complete in the same transaction")
	}
	if response.InstallIssue.ID == "" || response.AgentGuideIssue.ID == "" {
		t.Fatalf("expected both issue ids, got %+v", response)
	}
	content := onboardingSeedContents["en"]
	if response.InstallIssue.Title != content.InstallTitle {
		t.Fatalf("install title = %q, want server content %q", response.InstallIssue.Title, content.InstallTitle)
	}
	if response.AgentGuideIssue.Title != content.AgentGuideTitle {
		t.Fatalf("guide title = %q, want server content %q", response.AgentGuideIssue.Title, content.AgentGuideTitle)
	}

	type issueRow struct {
		creatorType  string
		creatorID    string
		assigneeType string
		assigneeID   string
		status       string
		priority     string
		description  string
		originType   string
		originID     string
	}
	loadIssue := func(id string) issueRow {
		var row issueRow
		if err := testPool.QueryRow(ctx, `
			SELECT creator_type, creator_id, assignee_type, assignee_id,
			       status, priority, description, origin_type, origin_id
			FROM issue WHERE id = $1
		`, id).Scan(
			&row.creatorType, &row.creatorID, &row.assigneeType, &row.assigneeID,
			&row.status, &row.priority, &row.description, &row.originType, &row.originID,
		); err != nil {
			t.Fatalf("load issue %s: %v", id, err)
		}
		return row
	}

	install := loadIssue(response.InstallIssue.ID)
	if install.creatorType != "system" || install.creatorID != zeroUUIDString {
		t.Fatalf("install creator = %s/%s, want system/zero-uuid", install.creatorType, install.creatorID)
	}
	if install.assigneeType != "member" || install.assigneeID != fixture.userID {
		t.Fatalf("install assignee = %s/%s, want member/%s", install.assigneeType, install.assigneeID, fixture.userID)
	}
	if install.status != "in_progress" || install.priority != "high" {
		t.Fatalf("install status/priority = %s/%s, want in_progress/high", install.status, install.priority)
	}
	if install.originType != installSeedOriginType || install.originID != fixture.userID {
		t.Fatalf("install origin = %s/%s, want %s/%s", install.originType, install.originID, installSeedOriginType, fixture.userID)
	}

	guide := loadIssue(response.AgentGuideIssue.ID)
	if guide.creatorType != "system" || guide.creatorID != zeroUUIDString {
		t.Fatalf("guide creator = %s/%s, want system/zero-uuid", guide.creatorType, guide.creatorID)
	}
	if guide.status != "todo" || guide.priority != "medium" {
		t.Fatalf("guide status/priority = %s/%s, want todo/medium", guide.status, guide.priority)
	}
	if guide.originType != guideSeedOriginType || guide.originID != fixture.userID {
		t.Fatalf("guide origin = %s/%s, want %s/%s", guide.originType, guide.originID, guideSeedOriginType, fixture.userID)
	}
	wantInstallChip := "[" + response.InstallIssue.Identifier + "](mention://issue/" + response.InstallIssue.ID + ")"
	if !strings.Contains(guide.description, wantInstallChip) {
		t.Fatalf("guide description should embed install mention chip %q", wantInstallChip)
	}
	if strings.Contains(guide.description, installIssueRefToken) {
		t.Fatalf("guide description still contains raw placeholder: %q", guide.description)
	}

	var (
		commentCount int
		authorType   string
		authorID     string
		commentBody  string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM comment WHERE issue_id = $1
	`, response.InstallIssue.ID).Scan(&commentCount); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("expected exactly 1 follow-up comment, got %d", commentCount)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT author_type, author_id, content FROM comment WHERE issue_id = $1
	`, response.InstallIssue.ID).Scan(&authorType, &authorID, &commentBody); err != nil {
		t.Fatalf("load comment: %v", err)
	}
	if authorType != "system" || authorID != zeroUUIDString {
		t.Fatalf("comment author = %s/%s, want system/zero-uuid", authorType, authorID)
	}
	wantGuideChip := "[" + response.AgentGuideIssue.Identifier + "](mention://issue/" + response.AgentGuideIssue.ID + ")"
	if !strings.Contains(commentBody, wantGuideChip) {
		t.Fatalf("comment should embed guide mention chip %q", wantGuideChip)
	}

	// A response-loss retry happens after onboarded_at is set. Durable origins
	// return the same bundle and do not stack another comment.
	w2, response2 := completeNoRuntimeForTest(t, fixture, "ja")
	if w2.Code != http.StatusOK {
		t.Fatalf("retry: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	if response2.InstallIssue.ID != response.InstallIssue.ID || response2.AgentGuideIssue.ID != response.AgentGuideIssue.ID {
		t.Fatalf("retry should return origin-linked rows: first=%+v second=%+v", response, response2)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, response.InstallIssue.ID).Scan(&commentCount); err != nil {
		t.Fatalf("recount comments: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("retry should not add another comment, got %d", commentCount)
	}
}

func TestCompleteOnboardingNoRuntimeDoesNotReuseSameTitleIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fixture := newNoRuntimeTestFixture(t, "owner")
	content := onboardingSeedContents["en"]

	var unrelatedID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, description, status, priority,
			creator_type, creator_id, position, number
		) VALUES ($1, $2, 'member-owned content', 'todo', 'medium', 'member', $3, 0, 999)
		RETURNING id
	`, fixture.workspaceID, content.InstallTitle, fixture.userID).Scan(&unrelatedID); err != nil {
		t.Fatalf("insert unrelated issue: %v", err)
	}

	w, response := completeNoRuntimeForTest(t, fixture, "en")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if response.InstallIssue.ID == unrelatedID {
		t.Fatal("system seed reused an unrelated same-title issue")
	}
	var comments int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, unrelatedID).Scan(&comments); err != nil {
		t.Fatalf("count unrelated comments: %v", err)
	}
	if comments != 0 {
		t.Fatalf("same-title issue received %d injected comments", comments)
	}
}

func TestCompleteOnboardingNoRuntimeRequiresOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	fixture := newNoRuntimeTestFixture(t, "member")
	w, _ := completeNoRuntimeForTest(t, fixture, "en")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	var onboarded bool
	if err := testPool.QueryRow(context.Background(), `SELECT onboarded_at IS NOT NULL FROM "user" WHERE id = $1`, fixture.userID).Scan(&onboarded); err != nil {
		t.Fatalf("load user: %v", err)
	}
	if onboarded {
		t.Fatal("non-owner request marked onboarding complete")
	}
}

func TestCompleteOnboardingNoRuntimeRejectsLateFirstSeed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	fixture := newNoRuntimeTestFixture(t, "owner")
	if _, err := testPool.Exec(context.Background(), `UPDATE "user" SET onboarded_at = now() WHERE id = $1`, fixture.userID); err != nil {
		t.Fatalf("mark user onboarded: %v", err)
	}
	w, _ := completeNoRuntimeForTest(t, fixture, "en")
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE workspace_id = $1`, fixture.workspaceID).Scan(&count); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if count != 0 {
		t.Fatalf("late seed created %d issues", count)
	}
}

func TestCompleteOnboardingNoRuntimeRejectsClientContentAndInvalidLocale(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	fixture := newNoRuntimeTestFixture(t, "owner")
	cases := []map[string]any{
		{"workspace_id": fixture.workspaceID, "locale": "fr"},
		{"workspace_id": fixture.workspaceID, "locale": "en", "install_issue": map[string]string{"title": "spoofed"}},
		{"locale": "en"},
	}
	for _, body := range cases {
		w := httptest.NewRecorder()
		testHandler.CompleteOnboardingNoRuntime(w, newRequestAs(
			fixture.userID,
			http.MethodPost,
			"/api/me/onboarding/no-runtime-complete",
			body,
		))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %#v: expected 400, got %d: %s", body, w.Code, w.Body.String())
		}
	}
}

func TestOnboardingSeedContentCoversSupportedLocales(t *testing.T) {
	for _, locale := range []string{"en", "zh", "ko", "ja"} {
		content, ok := onboardingSeedContentForLocale(locale)
		if !ok {
			t.Fatalf("missing %s content", locale)
		}
		if content.InstallTitle == "" || content.InstallDescription == "" || content.AgentGuideTitle == "" || content.AgentGuideDescription == "" || content.FollowupComment == "" {
			t.Fatalf("%s content has an empty field", locale)
		}
		if !strings.Contains(content.AgentGuideDescription, installIssueRefToken) {
			t.Fatalf("%s guide is missing install issue placeholder", locale)
		}
		if !strings.Contains(content.FollowupComment, agentGuideRefToken) {
			t.Fatalf("%s follow-up is missing guide issue placeholder", locale)
		}
	}
}
