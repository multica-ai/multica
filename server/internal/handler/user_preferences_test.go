package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUpdateMyPreferencesPersistsValue verifies that PATCH /api/me/preferences
// stores the supplied keys on the user and returns the merged blob in the
// response.
func TestUpdateMyPreferencesPersistsValue(t *testing.T) {
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`UPDATE "user" SET preferences = '{}'::jsonb WHERE id = $1`,
			testUserID,
		)
	})

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/me/preferences", map[string]any{
		"submit_on_enter": true,
	})
	testHandler.UpdateMyPreferences(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateMyPreferences: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp UserResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Preferences["submit_on_enter"] != true {
		t.Fatalf("response preferences.submit_on_enter: expected true, got %v", resp.Preferences["submit_on_enter"])
	}

	// Verify it actually landed in the database.
	var stored []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT preferences FROM "user" WHERE id = $1`, testUserID,
	).Scan(&stored); err != nil {
		t.Fatalf("read stored preferences: %v", err)
	}
	var storedMap map[string]any
	if err := json.Unmarshal(stored, &storedMap); err != nil {
		t.Fatalf("decode stored preferences: %v", err)
	}
	if storedMap["submit_on_enter"] != true {
		t.Fatalf("stored preferences.submit_on_enter: expected true, got %v", storedMap["submit_on_enter"])
	}
}

// TestUpdateMyPreferencesPartialMergePreservesOtherKeys verifies that a
// PATCH containing only one key does not blow away unrelated keys already
// present on the user — the whole point of partial-merge semantics.
func TestUpdateMyPreferencesPartialMergePreservesOtherKeys(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx,
			`UPDATE "user" SET preferences = '{}'::jsonb WHERE id = $1`,
			testUserID,
		)
	})

	// Seed two keys directly so we can confirm only the targeted one changes.
	if _, err := testPool.Exec(ctx,
		`UPDATE "user" SET preferences = $1::jsonb WHERE id = $2`,
		`{"submit_on_enter":false,"theme":"dark"}`, testUserID,
	); err != nil {
		t.Fatalf("seed preferences: %v", err)
	}

	// Patch only submit_on_enter — theme must remain untouched.
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/me/preferences", map[string]any{
		"submit_on_enter": true,
	})
	testHandler.UpdateMyPreferences(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateMyPreferences: %d %s", w.Code, w.Body.String())
	}

	var resp UserResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Preferences["submit_on_enter"] != true {
		t.Fatalf("submit_on_enter: expected true, got %v", resp.Preferences["submit_on_enter"])
	}
	if resp.Preferences["theme"] != "dark" {
		t.Fatalf("theme should be preserved across partial patch, got %v", resp.Preferences["theme"])
	}
}

// TestUpdateMyPreferencesEmptyBodyIsNoop verifies that PATCH with an empty
// JSON object returns 200 and leaves preferences unchanged — keeps the
// endpoint safe for naive clients that re-send on every render.
func TestUpdateMyPreferencesEmptyBodyIsNoop(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx,
			`UPDATE "user" SET preferences = '{}'::jsonb WHERE id = $1`,
			testUserID,
		)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE "user" SET preferences = $1::jsonb WHERE id = $2`,
		`{"submit_on_enter":true}`, testUserID,
	); err != nil {
		t.Fatalf("seed preferences: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/me/preferences", map[string]any{})
	testHandler.UpdateMyPreferences(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateMyPreferences: %d %s", w.Code, w.Body.String())
	}

	var resp UserResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Preferences["submit_on_enter"] != true {
		t.Fatalf("submit_on_enter should survive empty patch, got %v", resp.Preferences["submit_on_enter"])
	}
}

// TestUpdateMyPreferencesInvalidJSONReturnsBadRequest verifies that malformed
// JSON in the request body is rejected with a 400.
func TestUpdateMyPreferencesInvalidJSONReturnsBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", "/api/me/preferences", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	testHandler.UpdateMyPreferences(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateMyPreferencesRequiresAuth verifies that requests with no
// X-User-ID header are rejected with 401.
func TestUpdateMyPreferencesRequiresAuth(t *testing.T) {
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{"submit_on_enter": true})
	req := httptest.NewRequest("PATCH", "/api/me/preferences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit X-User-ID.
	testHandler.UpdateMyPreferences(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetMeReturnsPreferences verifies that GET /api/me surfaces the user's
// preferences blob — front-end relies on it to decide composer behaviour.
func TestGetMeReturnsPreferences(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx,
			`UPDATE "user" SET preferences = '{}'::jsonb WHERE id = $1`,
			testUserID,
		)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE "user" SET preferences = $1::jsonb WHERE id = $2`,
		`{"submit_on_enter":true}`, testUserID,
	); err != nil {
		t.Fatalf("seed preferences: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/me", nil)
	testHandler.GetMe(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetMe: %d %s", w.Code, w.Body.String())
	}

	var user UserResponse
	if err := json.NewDecoder(w.Body).Decode(&user); err != nil {
		t.Fatalf("decode GetMe: %v", err)
	}
	if user.Preferences["submit_on_enter"] != true {
		t.Fatalf("GetMe preferences.submit_on_enter: expected true, got %v", user.Preferences["submit_on_enter"])
	}
}
