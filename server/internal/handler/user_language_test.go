package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newLanguageTestUser(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Language Test", email,
	).Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

func newPatchMeRequest(userID, body string) *http.Request {
	req := httptest.NewRequest("PATCH", "/api/me", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	return req
}

func TestUpdateMeAcceptsLanguage(t *testing.T) {
	userID := newLanguageTestUser(t, "lang-set@multica.ai")

	w := httptest.NewRecorder()
	req := newPatchMeRequest(userID, `{"language":"zh-Hans"}`)
	testHandler.UpdateMe(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var lang *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT language FROM "user" WHERE id = $1`, userID,
	).Scan(&lang); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if lang == nil || *lang != "zh-Hans" {
		t.Fatalf("expected language=zh-Hans, got %v", lang)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, _ := resp["language"].(string); got != "zh-Hans" {
		t.Fatalf("expected response language=zh-Hans, got %v", resp["language"])
	}
}

func TestUpdateMeRejectsUnsupportedLanguage(t *testing.T) {
	userID := newLanguageTestUser(t, "lang-reject@multica.ai")

	w := httptest.NewRecorder()
	req := newPatchMeRequest(userID, `{"language":"<script>"}`)
	testHandler.UpdateMe(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var lang *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT language FROM "user" WHERE id = $1`, userID,
	).Scan(&lang); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if lang != nil {
		t.Fatalf("expected language unchanged (NULL), got %v", *lang)
	}
}

// COALESCE semantics: omitting language must NOT clear an existing value.
func TestUpdateMePreservesLanguageWhenNotProvided(t *testing.T) {
	userID := newLanguageTestUser(t, "lang-preserve@multica.ai")

	if _, err := testPool.Exec(context.Background(),
		`UPDATE "user" SET language = 'en' WHERE id = $1`, userID,
	); err != nil {
		t.Fatalf("preset language: %v", err)
	}

	w := httptest.NewRecorder()
	req := newPatchMeRequest(userID, `{"name":"Updated Name"}`)
	testHandler.UpdateMe(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var lang *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT language FROM "user" WHERE id = $1`, userID,
	).Scan(&lang); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if lang == nil || *lang != "en" {
		t.Fatalf("expected language=en preserved, got %v", lang)
	}
}
