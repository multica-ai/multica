package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func callStarPrompt(t *testing.T, userID, action string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/me/star-prompt/"+action, nil)
	req.Header.Set("X-User-ID", userID)
	w := httptest.NewRecorder()
	switch action {
	case "claim":
		testHandler.ClaimStarPromptShowing(w, req)
	case "starred":
		testHandler.MarkStarPromptStarred(w, req)
	default:
		t.Fatalf("unknown action %q", action)
	}
	return w
}

func starPromptUser(t *testing.T, email string, accountAgeDays int) string {
	t.Helper()
	userID := newWaitlistTestUser(t, email)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE "user" SET created_at = now() - make_interval(days => $2) WHERE id = $1`,
		userID, accountAgeDays,
	); err != nil {
		t.Fatalf("backdate user: %v", err)
	}
	return userID
}

func storedStarPromptShownCount(t *testing.T, userID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT star_prompt_shown_count FROM "user" WHERE id = $1`, userID,
	).Scan(&n); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	return n
}

func backdateStarPromptShowing(t *testing.T, userID string, days int) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE "user" SET star_prompt_last_shown_at = now() - make_interval(days => $2) WHERE id = $1`,
		userID, days,
	); err != nil {
		t.Fatalf("backdate showing: %v", err)
	}
}

func TestClaimStarPromptShowingFrequency(t *testing.T) {
	userID := starPromptUser(t, "star-prompt-frequency@multica.ai", 4)

	w := callStarPrompt(t, userID, "claim")
	if w.Code != http.StatusOK {
		t.Fatalf("first claim: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp UserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.StarPromptShownCount != 1 || resp.StarPromptLastShownAt == nil {
		t.Fatalf("first claim: got count %d, last shown %v", resp.StarPromptShownCount, resp.StarPromptLastShownAt)
	}

	if w := callStarPrompt(t, userID, "claim"); w.Code != http.StatusConflict {
		t.Fatalf("claim inside cooldown: expected 409, got %d", w.Code)
	}

	backdateStarPromptShowing(t, userID, 31)
	if w := callStarPrompt(t, userID, "claim"); w.Code != http.StatusOK {
		t.Fatalf("claim after cooldown: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	backdateStarPromptShowing(t, userID, 31)
	if w := callStarPrompt(t, userID, "claim"); w.Code != http.StatusConflict {
		t.Fatalf("claim past the last showing: expected 409, got %d", w.Code)
	}
	if n := storedStarPromptShownCount(t, userID); n != 2 {
		t.Fatalf("stored count = %d, want 2", n)
	}
}

func TestClaimStarPromptShowingWaitsForAccountAge(t *testing.T) {
	userID := starPromptUser(t, "star-prompt-new-account@multica.ai", 1)

	if w := callStarPrompt(t, userID, "claim"); w.Code != http.StatusConflict {
		t.Fatalf("claim on a new account: expected 409, got %d", w.Code)
	}
	if n := storedStarPromptShownCount(t, userID); n != 0 {
		t.Fatalf("refused claim must not write, got count %d", n)
	}
}

func TestStarPromptStarredRetiresThePrompt(t *testing.T) {
	userID := starPromptUser(t, "star-prompt-starred@multica.ai", 4)

	w := callStarPrompt(t, userID, "starred")
	if w.Code != http.StatusOK {
		t.Fatalf("starred: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var first UserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if first.StarPromptStarredAt == nil {
		t.Fatal("starred_at should be set")
	}

	var again UserResponse
	if err := json.Unmarshal(callStarPrompt(t, userID, "starred").Body.Bytes(), &again); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if again.StarPromptStarredAt == nil || *again.StarPromptStarredAt != *first.StarPromptStarredAt {
		t.Fatalf("repeat click must keep the first time: %v vs %v", again.StarPromptStarredAt, first.StarPromptStarredAt)
	}

	if w := callStarPrompt(t, userID, "claim"); w.Code != http.StatusConflict {
		t.Fatalf("claim after starring: expected 409, got %d", w.Code)
	}
}

func TestClaimStarPromptShowingHonoursDeploymentSwitch(t *testing.T) {
	userID := starPromptUser(t, "star-prompt-disabled@multica.ai", 4)
	prev := testHandler.cfg.DisableStarPrompt
	t.Cleanup(func() { testHandler.cfg.DisableStarPrompt = prev })
	testHandler.cfg.DisableStarPrompt = true

	if w := callStarPrompt(t, userID, "claim"); w.Code != http.StatusConflict {
		t.Fatalf("claim while disabled: expected 409, got %d", w.Code)
	}
	if n := storedStarPromptShownCount(t, userID); n != 0 {
		t.Fatalf("refused claim must not write, got count %d", n)
	}

	req := httptest.NewRequest("GET", "/api/config", nil)
	rec := httptest.NewRecorder()
	testHandler.GetConfig(rec, req)
	var cfg AppConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if !cfg.StarPromptDisabled {
		t.Fatal("config should report the star prompt as disabled")
	}
}

func TestGetMeAlwaysSerializesStarPromptCount(t *testing.T) {
	userID := newWaitlistTestUser(t, "star-prompt-getme@multica.ai")

	req := httptest.NewRequest("GET", "/api/me", nil)
	req.Header.Set("X-User-ID", userID)
	w := httptest.NewRecorder()
	testHandler.GetMe(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, ok := raw["star_prompt_shown_count"]; !ok || string(got) != "0" {
		t.Fatalf("star_prompt_shown_count must be present and 0 for a fresh user, got %s (present=%v)", got, ok)
	}
}

// The star prompt counts an agent's finished issues by lifecycle category, so
// a workspace that finishes work in a custom done status still qualifies.
func TestListIssuesCountsAgentDoneCategoryIncludingCustomStatuses(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	customKey := fmt.Sprintf("shipped_%d", suffix)
	agentID := uuid.NewString()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_status (workspace_id, key, name, description, category, color, position)
		VALUES ($1, $2, 'Shipped', '', 'done', '#00aa00', 1)
	`, testWorkspaceID, customKey); err != nil {
		t.Fatalf("create custom status: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE assignee_id = $1`, agentID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_status WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, customKey)
	})

	var firstNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(
			issue_counter,
			(SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)
		) + 4
		WHERE id = $1
		RETURNING issue_counter - 3
	`, testWorkspaceID).Scan(&firstNumber); err != nil {
		t.Fatalf("reserve issue numbers: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, position, number)
		VALUES
			($1, 'built-in done', 'done',      'none', 'member', $3, 'agent', $4, 1, $5),
			($1, 'custom done',   $2,          'none', 'member', $3, 'agent', $4, 2, $5 + 1),
			($1, 'cancelled',     'cancelled', 'none', 'member', $3, 'agent', $4, 3, $5 + 2),
			($1, 'in review',     'in_review', 'none', 'member', $3, 'agent', $4, 4, $5 + 3)
	`, testWorkspaceID, customKey, testUserID, agentID, firstNumber); err != nil {
		t.Fatalf("seed issues: %v", err)
	}

	path := fmt.Sprintf("/api/issues?workspace_id=%s&status_categories=done&assignee_filters=agent:%s&limit=1",
		testWorkspaceID, agentID)
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Total int64 `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("done-category total = %d, want 2 (built-in done + custom done)", resp.Total)
	}
}
