package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newWaitlistTestUser inserts a fresh user row, returns its id, and
// registers a cleanup. Uses the test pool directly so we don't depend
// on the handler-under-test for fixture setup.
func newWaitlistTestUser(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Waitlist Test", email,
	).Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

func newWaitlistRequest(userID string, body map[string]string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(
		"POST", "/api/me/onboarding/cloud-waitlist", &buf,
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	return req
}

func TestJoinCloudWaitlistRecordsEmailAndReason(t *testing.T) {
	userID := newWaitlistTestUser(t, "waitlist-ok@multica.ai")

	w := httptest.NewRecorder()
	req := newWaitlistRequest(userID, map[string]string{
		"email":  "Someone@Example.COM",
		"reason": "evaluating for our team",
	})
	testHandler.JoinCloudWaitlist(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var (
		waitlistEmail  *string
		waitlistReason *string
		onboardedAt    *time.Time
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT cloud_waitlist_email, cloud_waitlist_reason, onboarded_at
		  FROM "user" WHERE id = $1
	`, userID).Scan(&waitlistEmail, &waitlistReason, &onboardedAt); err != nil {
		t.Fatalf("lookup user: %v", err)
	}

	if waitlistEmail == nil || *waitlistEmail != "someone@example.com" {
		t.Fatalf("email not normalized/stored: %v", waitlistEmail)
	}
	if waitlistReason == nil || *waitlistReason != "evaluating for our team" {
		t.Fatalf("reason not stored: %v", waitlistReason)
	}
	// Waitlist is a pure side effect — onboarding is NOT marked
	// complete here. The user still has to pick a real Step 3
	// path (CLI / Skip) before onboarded_at gets set.
	if onboardedAt != nil {
		t.Fatalf("onboarded_at should stay NULL, got %v", *onboardedAt)
	}
}

func TestJoinCloudWaitlistAllowsEmptyReason(t *testing.T) {
	userID := newWaitlistTestUser(t, "waitlist-noreason@multica.ai")

	w := httptest.NewRecorder()
	req := newWaitlistRequest(userID, map[string]string{
		"email": "noreason@example.com",
	})
	testHandler.JoinCloudWaitlist(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var waitlistReason *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT cloud_waitlist_reason FROM "user" WHERE id = $1`, userID,
	).Scan(&waitlistReason); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if waitlistReason != nil {
		t.Fatalf("expected NULL reason, got %q", *waitlistReason)
	}
}

func TestJoinCloudWaitlistMissingEmailReturns400(t *testing.T) {
	userID := newWaitlistTestUser(t, "waitlist-missing@multica.ai")

	cases := []map[string]string{
		{},               // empty body
		{"email": ""},    // blank
		{"email": "   "}, // whitespace only
		{"reason": "no email here"},
	}

	for i, body := range cases {
		w := httptest.NewRecorder()
		req := newWaitlistRequest(userID, body)
		testHandler.JoinCloudWaitlist(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("case %d: expected 400, got %d: %s", i, w.Code, w.Body.String())
		}
	}
}

func TestJoinCloudWaitlistRejectsOverlongReason(t *testing.T) {
	userID := newWaitlistTestUser(t, "waitlist-long@multica.ai")

	w := httptest.NewRecorder()
	req := newWaitlistRequest(userID, map[string]string{
		"email":  "long@example.com",
		"reason": strings.Repeat("x", cloudWaitlistReasonMaxLen+1),
	})
	testHandler.JoinCloudWaitlist(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong reason, got %d", w.Code)
	}
}

func TestJoinCloudWaitlistSecondCallOverwrites(t *testing.T) {
	userID := newWaitlistTestUser(t, "waitlist-overwrite@multica.ai")

	// First submission.
	w := httptest.NewRecorder()
	req := newWaitlistRequest(userID, map[string]string{
		"email":  "first@example.com",
		"reason": "first",
	})
	testHandler.JoinCloudWaitlist(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first call: expected 200, got %d", w.Code)
	}

	// Second submission with different values.
	w = httptest.NewRecorder()
	req = newWaitlistRequest(userID, map[string]string{
		"email":  "second@example.com",
		"reason": "changed my mind",
	})
	testHandler.JoinCloudWaitlist(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d", w.Code)
	}

	var (
		waitlistEmail  string
		waitlistReason string
		onboardedAt    *time.Time
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT cloud_waitlist_email, cloud_waitlist_reason, onboarded_at
		  FROM "user" WHERE id = $1
	`, userID).Scan(&waitlistEmail, &waitlistReason, &onboardedAt); err != nil {
		t.Fatalf("lookup user: %v", err)
	}

	if waitlistEmail != "second@example.com" {
		t.Fatalf("second email should overwrite; got %q", waitlistEmail)
	}
	if waitlistReason != "changed my mind" {
		t.Fatalf("second reason should overwrite; got %q", waitlistReason)
	}
	// onboarded_at is never touched by the waitlist path — stays NULL
	// across any number of submissions.
	if onboardedAt != nil {
		t.Fatalf("onboarded_at should stay NULL, got %v", *onboardedAt)
	}
}

// ---------------------------------------------------------------------------
// Shim endpoint tests — guard the BootstrapOnboarding* handlers that were
// restored for desktop < v3 compatibility. Once telemetry confirms no
// pre-v3 desktops remain, delete both these tests AND the handlers in
// onboarding_shim.go in the same commit.
// ---------------------------------------------------------------------------

func TestBootstrapOnboardingRuntimeCreatesSingleGuideIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `
			DELETE FROM agent_task_queue
			 WHERE agent_id IN (
			       SELECT id FROM agent
			        WHERE workspace_id = $1 AND name = $2
			 )
		`, testWorkspaceID, onboardingAssistantName)
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
			testWorkspaceID, onboardingIssueTitle,
		)
		testPool.Exec(ctx,
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, onboardingAssistantName,
		)
		testPool.Exec(ctx,
			`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`,
			testUserID,
		)
	})
	testPool.Exec(ctx,
		`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, onboardingIssueTitle,
	)
	testPool.Exec(ctx,
		`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, onboardingAssistantName,
	)
	testPool.Exec(ctx,
		`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`,
		testUserID,
	)

	body := map[string]string{
		"workspace_id": testWorkspaceID,
		"runtime_id":   testRuntimeID,
	}
	w := httptest.NewRecorder()
	testHandler.BootstrapOnboardingRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/runtime-bootstrap", body))
	if w.Code != http.StatusOK {
		t.Fatalf("BootstrapOnboardingRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp bootstrapOnboardingRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WorkspaceID != testWorkspaceID || resp.AgentID == "" || resp.IssueID == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	var (
		agentName    string
		agentRuntime string
		instructions string
		avatarURL    *string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT name, runtime_id, instructions, avatar_url
		  FROM agent
		 WHERE id = $1
	`, resp.AgentID).Scan(&agentName, &agentRuntime, &instructions, &avatarURL); err != nil {
		t.Fatalf("lookup assistant: %v", err)
	}
	if agentName != onboardingAssistantName {
		t.Fatalf("agent name = %q, want %q", agentName, onboardingAssistantName)
	}
	if agentRuntime != testRuntimeID {
		t.Fatalf("agent runtime = %q, want %q", agentRuntime, testRuntimeID)
	}
	if !strings.Contains(instructions, "built-in AI assistant") {
		t.Fatalf("assistant instructions were not seeded with the new identity: %q", instructions)
	}
	if avatarURL == nil || *avatarURL != onboardingAssistantAvatarURL {
		t.Fatalf("agent avatar_url = %v, want seeded Multica Helper avatar", avatarURL)
	}

	var (
		issueTitle    string
		assigneeType  string
		assigneeID    string
		issueStatus   string
		issuePriority string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT title, assignee_type, assignee_id, status, priority
		  FROM issue
		 WHERE id = $1
	`, resp.IssueID).Scan(&issueTitle, &assigneeType, &assigneeID, &issueStatus, &issuePriority); err != nil {
		t.Fatalf("lookup onboarding issue: %v", err)
	}
	if issueTitle != onboardingIssueTitle {
		t.Fatalf("issue title = %q, want %q", issueTitle, onboardingIssueTitle)
	}
	if assigneeType != "agent" || assigneeID != resp.AgentID {
		t.Fatalf("issue assignee = %s/%s, want agent/%s", assigneeType, assigneeID, resp.AgentID)
	}
	if issueStatus != "todo" || issuePriority != "high" {
		t.Fatalf("issue status/priority = %s/%s, want todo/high", issueStatus, issuePriority)
	}

	var (
		onboardedAt         *time.Time
		starterContentState *string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT onboarded_at, starter_content_state
		  FROM "user"
		 WHERE id = $1
	`, testUserID).Scan(&onboardedAt, &starterContentState); err != nil {
		t.Fatalf("lookup user onboarding state: %v", err)
	}
	if onboardedAt == nil {
		t.Fatal("expected onboarded_at to be set")
	}
	if starterContentState == nil || *starterContentState != "imported" {
		t.Fatalf("starter_content_state = %v, want imported", starterContentState)
	}

	var taskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		  FROM agent_task_queue
		 WHERE issue_id = $1 AND agent_id = $2
	`, resp.IssueID, resp.AgentID).Scan(&taskCount); err != nil {
		t.Fatalf("count queued tasks: %v", err)
	}
	if taskCount == 0 {
		t.Fatal("expected onboarding issue to enqueue an agent task")
	}

	w2 := httptest.NewRecorder()
	testHandler.BootstrapOnboardingRuntime(w2, newRequest(http.MethodPost, "/api/me/onboarding/runtime-bootstrap", body))
	if w2.Code != http.StatusOK {
		t.Fatalf("second BootstrapOnboardingRuntime: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 bootstrapOnboardingRuntimeResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if resp2.AgentID != resp.AgentID || resp2.IssueID != resp.IssueID {
		t.Fatalf("bootstrap should be idempotent: first=%+v second=%+v", resp, resp2)
	}
}

func TestBootstrapOnboardingRuntime_WithStarterPrompt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `
			DELETE FROM agent_task_queue
			 WHERE agent_id IN (
			       SELECT id FROM agent
			        WHERE workspace_id = $1 AND name = $2
			 )
		`, testWorkspaceID, onboardingAssistantName)
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
			testWorkspaceID, onboardingIssueTitle,
		)
		testPool.Exec(ctx,
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, onboardingAssistantName,
		)
		testPool.Exec(ctx,
			`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`,
			testUserID,
		)
	})
	testPool.Exec(ctx,
		`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, onboardingIssueTitle,
	)
	testPool.Exec(ctx,
		`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, onboardingAssistantName,
	)
	testPool.Exec(ctx,
		`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`,
		testUserID,
	)

	const wantPrompt = "Introduce Multica to me, please."
	body := map[string]string{
		"workspace_id":   testWorkspaceID,
		"runtime_id":     testRuntimeID,
		"starter_prompt": wantPrompt,
	}
	w := httptest.NewRecorder()
	testHandler.BootstrapOnboardingRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/runtime-bootstrap", body))
	if w.Code != http.StatusOK {
		t.Fatalf("BootstrapOnboardingRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp bootstrapOnboardingRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var description *string
	if err := testPool.QueryRow(ctx, `
		SELECT description FROM issue WHERE id = $1
	`, resp.IssueID).Scan(&description); err != nil {
		t.Fatalf("lookup issue description: %v", err)
	}
	if description == nil || *description != wantPrompt {
		t.Fatalf("issue description = %v, want %q", description, wantPrompt)
	}
}

func TestBootstrapOnboardingRuntime_NoStarterPrompt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `
			DELETE FROM agent_task_queue
			 WHERE agent_id IN (
			       SELECT id FROM agent
			        WHERE workspace_id = $1 AND name = $2
			 )
		`, testWorkspaceID, onboardingAssistantName)
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
			testWorkspaceID, onboardingIssueTitle,
		)
		testPool.Exec(ctx,
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, onboardingAssistantName,
		)
		testPool.Exec(ctx,
			`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`,
			testUserID,
		)
	})
	testPool.Exec(ctx,
		`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, onboardingIssueTitle,
	)
	testPool.Exec(ctx,
		`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, onboardingAssistantName,
	)
	testPool.Exec(ctx,
		`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`,
		testUserID,
	)

	body := map[string]string{
		"workspace_id": testWorkspaceID,
		"runtime_id":   testRuntimeID,
	}
	w := httptest.NewRecorder()
	testHandler.BootstrapOnboardingRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/runtime-bootstrap", body))
	if w.Code != http.StatusOK {
		t.Fatalf("BootstrapOnboardingRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp bootstrapOnboardingRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var description *string
	if err := testPool.QueryRow(ctx, `
		SELECT description FROM issue WHERE id = $1
	`, resp.IssueID).Scan(&description); err != nil {
		t.Fatalf("lookup issue description: %v", err)
	}
	if description == nil || *description != onboardingIssueDescription {
		t.Fatalf("issue description = %v, want fallback onboardingIssueDescription", description)
	}
}

func TestBootstrapOnboardingNoRuntimeCreatesSingleGuideIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
			testWorkspaceID, noRuntimeIssueTitle,
		)
		testPool.Exec(ctx,
			`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL, language = NULL WHERE id = $1`,
			testUserID,
		)
	})
	testPool.Exec(ctx,
		`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, noRuntimeIssueTitle,
	)
	testPool.Exec(ctx,
		`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL, language = 'en' WHERE id = $1`,
		testUserID,
	)

	body := map[string]string{
		"workspace_id": testWorkspaceID,
	}
	w := httptest.NewRecorder()
	testHandler.BootstrapOnboardingNoRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/no-runtime-bootstrap", body))
	if w.Code != http.StatusOK {
		t.Fatalf("BootstrapOnboardingNoRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp bootstrapOnboardingNoRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WorkspaceID != testWorkspaceID || resp.IssueID == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	var (
		issueTitle    string
		assigneeType  string
		assigneeID    string
		issueStatus   string
		issuePriority string
		description   string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT title, assignee_type, assignee_id, status, priority, description
		  FROM issue
		 WHERE id = $1
	`, resp.IssueID).Scan(&issueTitle, &assigneeType, &assigneeID, &issueStatus, &issuePriority, &description); err != nil {
		t.Fatalf("lookup no-runtime onboarding issue: %v", err)
	}
	if issueTitle != noRuntimeIssueTitle {
		t.Fatalf("issue title = %q, want %q", issueTitle, noRuntimeIssueTitle)
	}
	if assigneeType != "member" || assigneeID != testUserID {
		t.Fatalf("issue assignee = %s/%s, want member/%s", assigneeType, assigneeID, testUserID)
	}
	if issueStatus != "todo" || issuePriority != "high" {
		t.Fatalf("issue status/priority = %s/%s, want todo/high", issueStatus, issuePriority)
	}
	for _, want := range []string{
		"Try Multica first",
		"https://multica.ai/docs/install-agent-runtime",
		"npm i -g @openai/codex",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("issue description missing %q: %q", want, description)
		}
	}
	if !strings.Contains(description, "Agents need a runtime before they can execute work") {
		t.Fatalf("issue description was not seeded: %q", description)
	}

	var (
		onboardedAt         *time.Time
		starterContentState *string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT onboarded_at, starter_content_state
		  FROM "user"
		 WHERE id = $1
	`, testUserID).Scan(&onboardedAt, &starterContentState); err != nil {
		t.Fatalf("lookup user onboarding state: %v", err)
	}
	if onboardedAt == nil {
		t.Fatal("expected onboarded_at to be set")
	}
	if starterContentState == nil || *starterContentState != "imported" {
		t.Fatalf("starter_content_state = %v, want imported", starterContentState)
	}

	var taskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		  FROM agent_task_queue
		 WHERE issue_id = $1
	`, resp.IssueID).Scan(&taskCount); err != nil {
		t.Fatalf("count queued tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected no agent tasks for no-runtime issue, got %d", taskCount)
	}

	w2 := httptest.NewRecorder()
	testHandler.BootstrapOnboardingNoRuntime(w2, newRequest(http.MethodPost, "/api/me/onboarding/no-runtime-bootstrap", body))
	if w2.Code != http.StatusOK {
		t.Fatalf("second BootstrapOnboardingNoRuntime: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 bootstrapOnboardingNoRuntimeResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if resp2.IssueID != resp.IssueID {
		t.Fatalf("bootstrap should be idempotent: first=%+v second=%+v", resp, resp2)
	}
}

func TestBootstrapOnboardingNoRuntimeUsesChineseGuideForChineseUsers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
			testWorkspaceID, noRuntimeIssueTitle,
		)
		testPool.Exec(ctx,
			`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL, language = NULL WHERE id = $1`,
			testUserID,
		)
	})
	testPool.Exec(ctx,
		`DELETE FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, noRuntimeIssueTitle,
	)
	testPool.Exec(ctx,
		`UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL, language = 'zh-Hans' WHERE id = $1`,
		testUserID,
	)

	body := map[string]string{
		"workspace_id": testWorkspaceID,
	}
	w := httptest.NewRecorder()
	testHandler.BootstrapOnboardingNoRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/no-runtime-bootstrap", body))
	if w.Code != http.StatusOK {
		t.Fatalf("BootstrapOnboardingNoRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp bootstrapOnboardingNoRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var description string
	if err := testPool.QueryRow(ctx, `
		SELECT description
		  FROM issue
		 WHERE id = $1
	`, resp.IssueID).Scan(&description); err != nil {
		t.Fatalf("lookup no-runtime onboarding issue: %v", err)
	}
	for _, want := range []string{
		"先体验项目管理功能",
		"https://multica.ai/docs/install-agent-runtime",
		"中文用户建议先装 Kimi CLI",
		"kimi --version",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("Chinese issue description missing %q: %q", want, description)
		}
	}
}
