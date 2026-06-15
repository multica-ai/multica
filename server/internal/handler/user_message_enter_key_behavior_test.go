package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newMessageEnterKeyBehaviorTestUser(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Message Enter Key Behavior Test", email,
	).Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

func TestUpdateMeAcceptsMessageEnterKeyBehavior(t *testing.T) {
	userID := newMessageEnterKeyBehaviorTestUser(t, "enter-behavior-set@multica.ai")

	w := httptest.NewRecorder()
	req := newPatchMeRequest(userID, `{"message_enter_key_behavior":"newline"}`)
	testHandler.UpdateMe(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT message_enter_key_behavior FROM "user" WHERE id = $1`, userID,
	).Scan(&stored); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if stored != "newline" {
		t.Fatalf("expected message_enter_key_behavior=newline, got %q", stored)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, _ := resp["message_enter_key_behavior"].(string); got != "newline" {
		t.Fatalf("expected response message_enter_key_behavior=newline, got %v", resp["message_enter_key_behavior"])
	}
}

func TestUpdateMeRejectsInvalidMessageEnterKeyBehavior(t *testing.T) {
	userID := newMessageEnterKeyBehaviorTestUser(t, "enter-behavior-reject@multica.ai")

	w := httptest.NewRecorder()
	req := newPatchMeRequest(userID, `{"message_enter_key_behavior":"maybe"}`)
	testHandler.UpdateMe(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT message_enter_key_behavior FROM "user" WHERE id = $1`, userID,
	).Scan(&stored); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if stored != "newline" {
		t.Fatalf("expected default message_enter_key_behavior=newline, got %q", stored)
	}
}

func TestUpdateMePreservesMessageEnterKeyBehaviorWhenNotProvided(t *testing.T) {
	userID := newMessageEnterKeyBehaviorTestUser(t, "enter-behavior-preserve@multica.ai")

	if _, err := testPool.Exec(context.Background(),
		`UPDATE "user" SET message_enter_key_behavior = 'newline' WHERE id = $1`, userID,
	); err != nil {
		t.Fatalf("preset message_enter_key_behavior: %v", err)
	}

	w := httptest.NewRecorder()
	req := newPatchMeRequest(userID, `{"name":"Updated Name"}`)
	testHandler.UpdateMe(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT message_enter_key_behavior FROM "user" WHERE id = $1`, userID,
	).Scan(&stored); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if stored != "newline" {
		t.Fatalf("expected message_enter_key_behavior preserved as newline, got %q", stored)
	}
}
