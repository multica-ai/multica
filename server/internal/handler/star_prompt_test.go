package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
