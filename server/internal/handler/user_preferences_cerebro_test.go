package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateMyPreferencesWritesOnlyAuthenticatedUser(t *testing.T) {
	ctx := context.Background()
	var secondUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email, preferences)
		VALUES ('Preference isolation', 'preference-isolation@example.test', '{"theme":"dark"}'::jsonb)
		RETURNING id
	`).Scan(&secondUserID); err != nil {
		t.Fatalf("create second user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, secondUserID)
		_, _ = testPool.Exec(ctx,
			`UPDATE "user" SET preferences = '{}'::jsonb WHERE id = $1`,
			testUserID,
		)
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPatch, "/api/me/preferences", map[string]any{
		"cerebro_editor_toolbar_order": map[string]any{
			"order":  []string{"bold", "lists"},
			"hidden": []string{"comment"},
		},
	})
	testHandler.UpdateMyPreferences(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateMyPreferences: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var authenticatedPreferences, secondUserPreferences []byte
	if err := testPool.QueryRow(ctx,
		`SELECT preferences FROM "user" WHERE id = $1`, testUserID,
	).Scan(&authenticatedPreferences); err != nil {
		t.Fatalf("read authenticated user preferences: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT preferences FROM "user" WHERE id = $1`, secondUserID,
	).Scan(&secondUserPreferences); err != nil {
		t.Fatalf("read second user preferences: %v", err)
	}

	var authenticated map[string]any
	if err := json.Unmarshal(authenticatedPreferences, &authenticated); err != nil {
		t.Fatalf("decode authenticated user preferences: %v", err)
	}
	if _, ok := authenticated["cerebro_editor_toolbar_order"]; !ok {
		t.Fatal("authenticated user's toolbar preference was not stored")
	}
	if string(secondUserPreferences) != `{"theme": "dark"}` {
		t.Fatalf("second user's preferences changed: %s", secondUserPreferences)
	}
}

func TestUpdateMyPreferencesCerebroRejectsUnauthenticatedRequest(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"cerebro_editor_toolbar_order": map[string]any{
			"order":  []string{"bold", "lists"},
			"hidden": []string{},
		},
	})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/me/preferences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	testHandler.UpdateMyPreferences(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without authentication, got %d: %s", w.Code, w.Body.String())
	}
}
