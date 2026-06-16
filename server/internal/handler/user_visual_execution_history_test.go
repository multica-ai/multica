package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newGetMeRequest(userID string) *http.Request {
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.Header.Set("X-User-ID", userID)
	return req
}

func newVisualExecHistoryTestUser(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Visual Exec History Test", email,
	).Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

// Round-trip: PATCH /api/me with the toggle on persists the column and the
// response echoes it, so it can land in the frontend auth store and drive
// the execution-log gate without a refetch.
func TestUpdateMeAcceptsVisualExecutionHistory(t *testing.T) {
	userID := newVisualExecHistoryTestUser(t, "veh-set@multica.ai")

	w := httptest.NewRecorder()
	req := newPatchMeRequest(userID, `{"visual_execution_history":true}`)
	testHandler.UpdateMe(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stored bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT visual_execution_history FROM "user" WHERE id = $1`, userID,
	).Scan(&stored); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if !stored {
		t.Fatalf("expected visual_execution_history=true, got %v", stored)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, _ := resp["visual_execution_history"].(bool); !got {
		t.Fatalf("expected response visual_execution_history=true, got %v", resp["visual_execution_history"])
	}
}

// New users default to false (the raw log view).
func TestUpdateMeVisualExecutionHistoryDefaultsFalse(t *testing.T) {
	userID := newVisualExecHistoryTestUser(t, "veh-default@multica.ai")

	w := httptest.NewRecorder()
	testHandler.GetMe(w, newGetMeRequest(userID))
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, _ := resp["visual_execution_history"].(bool); got {
		t.Fatalf("expected default visual_execution_history=false, got %v", resp["visual_execution_history"])
	}
}

// COALESCE semantics — omitting the field must NOT reset an existing value.
func TestUpdateMePreservesVisualExecutionHistoryWhenNotProvided(t *testing.T) {
	userID := newVisualExecHistoryTestUser(t, "veh-preserve@multica.ai")

	if _, err := testPool.Exec(context.Background(),
		`UPDATE "user" SET visual_execution_history = true WHERE id = $1`, userID,
	); err != nil {
		t.Fatalf("preset flag: %v", err)
	}

	w := httptest.NewRecorder()
	req := newPatchMeRequest(userID, `{"name":"Updated Name"}`)
	testHandler.UpdateMe(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stored bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT visual_execution_history FROM "user" WHERE id = $1`, userID,
	).Scan(&stored); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if !stored {
		t.Fatalf("expected visual_execution_history preserved as true, got %v", stored)
	}
}

// Explicitly turning it back off persists false (not a no-op).
func TestUpdateMeTurnsOffVisualExecutionHistory(t *testing.T) {
	userID := newVisualExecHistoryTestUser(t, "veh-off@multica.ai")

	if _, err := testPool.Exec(context.Background(),
		`UPDATE "user" SET visual_execution_history = true WHERE id = $1`, userID,
	); err != nil {
		t.Fatalf("preset flag: %v", err)
	}

	w := httptest.NewRecorder()
	req := newPatchMeRequest(userID, `{"visual_execution_history":false}`)
	testHandler.UpdateMe(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stored bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT visual_execution_history FROM "user" WHERE id = $1`, userID,
	).Scan(&stored); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if stored {
		t.Fatalf("expected visual_execution_history=false, got %v", stored)
	}
}
