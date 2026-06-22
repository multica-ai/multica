package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestRuntimeClaimWindow_DefaultsNull(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, _, _ := runtimeVisibilityFixture(t)
	var start pgtype.Time
	var timezone pgtype.Text
	if err := testPool.QueryRow(context.Background(), `
		SELECT claim_window_start, claim_window_timezone
		FROM agent_runtime
		WHERE id = $1
	`, runtimeID).Scan(&start, &timezone); err != nil {
		t.Fatalf("load runtime claim window: %v", err)
	}
	if start.Valid || timezone.Valid {
		t.Fatalf("claim window defaults = start valid %v, timezone valid %v; want both false", start.Valid, timezone.Valid)
	}
}

func TestUpdateAgentRuntime_ClaimWindowSetClearAndPermissions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, plainMemberID := runtimeVisibilityFixture(t)

	patch := func(actorID string, body map[string]any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := newRequestAs(actorID, http.MethodPatch, "/api/runtimes/"+runtimeID, body)
		req = withURLParam(req, "runtimeId", runtimeID)
		testHandler.UpdateAgentRuntime(w, req)
		return w
	}
	assertStored := func(wantStart, wantTimezone string) {
		t.Helper()
		var start, timezone string
		if err := testPool.QueryRow(context.Background(), `
			SELECT to_char(claim_window_start, 'HH24:MI'), claim_window_timezone
			FROM agent_runtime
			WHERE id = $1
		`, runtimeID).Scan(&start, &timezone); err != nil {
			t.Fatalf("load stored claim window: %v", err)
		}
		if start != wantStart || timezone != wantTimezone {
			t.Fatalf("stored claim window = %s %s; want %s %s", start, timezone, wantStart, wantTimezone)
		}
	}

	w := patch(runtimeOwnerID, map[string]any{
		"claim_window": map[string]any{
			"start_time": "02:00",
			"timezone":   "Europe/Warsaw",
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("set claim window as runtime owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var setResp AgentRuntimeResponse
	decodeJSON(t, w, &setResp)
	if setResp.ClaimWindowStart == nil || *setResp.ClaimWindowStart != "02:00" {
		t.Fatalf("response start = %v; want 02:00", setResp.ClaimWindowStart)
	}
	if setResp.ClaimWindowTimezone == nil || *setResp.ClaimWindowTimezone != "Europe/Warsaw" {
		t.Fatalf("response timezone = %v; want Europe/Warsaw", setResp.ClaimWindowTimezone)
	}
	if setResp.ClaimWindowDurationMinutes != 300 {
		t.Fatalf("duration = %d; want 300", setResp.ClaimWindowDurationMinutes)
	}
	if setResp.ClaimWindowOpen == nil || setResp.ClaimWindowTransitionAt == nil {
		t.Fatalf("scheduled response missing state: open=%v transition=%v", setResp.ClaimWindowOpen, setResp.ClaimWindowTransitionAt)
	}
	assertStored("02:00", "Europe/Warsaw")

	w = patch(plainMemberID, map[string]any{
		"claim_window": map[string]any{"start_time": "03:00", "timezone": "UTC"},
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("set claim window as plain member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	assertStored("02:00", "Europe/Warsaw")

	w = patch(testUserID, map[string]any{
		"claim_window": map[string]any{"start_time": "03:00", "timezone": "UTC"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("set claim window as workspace owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertStored("03:00", "UTC")

	// Omitting claim_window must leave the existing schedule untouched.
	w = patch(runtimeOwnerID, map[string]any{"visibility": "public"})
	if w.Code != http.StatusOK {
		t.Fatalf("visibility-only patch: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertStored("03:00", "UTC")

	w = patch(runtimeOwnerID, map[string]any{"claim_window": nil})
	if w.Code != http.StatusOK {
		t.Fatalf("clear claim window: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var clearResp AgentRuntimeResponse
	decodeJSON(t, w, &clearResp)
	if clearResp.ClaimWindowStart != nil || clearResp.ClaimWindowTimezone != nil || clearResp.ClaimWindowOpen != nil || clearResp.ClaimWindowTransitionAt != nil {
		t.Fatalf("cleared response still scheduled: %+v", clearResp)
	}
	var start pgtype.Time
	var timezone pgtype.Text
	if err := testPool.QueryRow(context.Background(), `
		SELECT claim_window_start, claim_window_timezone
		FROM agent_runtime
		WHERE id = $1
	`, runtimeID).Scan(&start, &timezone); err != nil {
		t.Fatalf("load cleared claim window: %v", err)
	}
	if start.Valid || timezone.Valid {
		t.Fatalf("cleared claim window = start valid %v, timezone valid %v", start.Valid, timezone.Valid)
	}
}

func TestUpdateAgentRuntime_ClaimWindowRejectsInvalidValues(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{
			name: "non-padded start",
			body: map[string]any{"claim_window": map[string]any{
				"start_time": "2:00",
				"timezone":   "Europe/Warsaw",
			}},
		},
		{
			name: "invalid timezone",
			body: map[string]any{"claim_window": map[string]any{
				"start_time": "02:00",
				"timezone":   "Mars/Olympus",
			}},
		},
		{
			name: "missing timezone",
			body: map[string]any{"claim_window": map[string]any{
				"start_time": "02:00",
			}},
		},
		{
			name: "server local timezone",
			body: map[string]any{"claim_window": map[string]any{
				"start_time": "02:00",
				"timezone":   "Local",
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtimeID, runtimeOwnerID, _ := runtimeVisibilityFixture(t)
			w := httptest.NewRecorder()
			req := newRequestAs(runtimeOwnerID, http.MethodPatch, "/api/runtimes/"+runtimeID, tc.body)
			req = withURLParam(req, "runtimeId", runtimeID)
			testHandler.UpdateAgentRuntime(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestUpdateAgentRuntime_CombinedPatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, _ := runtimeVisibilityFixture(t)
	w := httptest.NewRecorder()
	req := newRequestAs(runtimeOwnerID, http.MethodPatch, "/api/runtimes/"+runtimeID, map[string]any{
		"visibility": "public",
		"claim_window": map[string]any{
			"start_time": "03:00",
			"timezone":   "UTC",
		},
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("combined patch: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentRuntimeResponse
	decodeJSON(t, w, &resp)
	if resp.Visibility != "public" || resp.ClaimWindowStart == nil || *resp.ClaimWindowStart != "03:00" || resp.ClaimWindowTimezone == nil || *resp.ClaimWindowTimezone != "UTC" {
		t.Fatalf("combined response = %+v", resp)
	}
}

func TestRuntimeToResponse_ClaimWindow(t *testing.T) {
	scheduled := db.AgentRuntime{
		ID:                  util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
		ClaimWindowStart:    pgtype.Time{Microseconds: int64(2 * time.Hour / time.Microsecond), Valid: true},
		ClaimWindowTimezone: pgtype.Text{String: "UTC", Valid: true},
	}
	now, _ := time.Parse(time.RFC3339, "2026-06-22T03:00:00Z")
	resp := runtimeToResponseAt(scheduled, now)
	if resp.ClaimWindowStart == nil || *resp.ClaimWindowStart != "02:00" {
		t.Fatalf("start = %v", resp.ClaimWindowStart)
	}
	if resp.ClaimWindowOpen == nil || !*resp.ClaimWindowOpen {
		t.Fatalf("open = %v", resp.ClaimWindowOpen)
	}
	if resp.ClaimWindowTransitionAt == nil || *resp.ClaimWindowTransitionAt != "2026-06-22T07:00:00Z" {
		t.Fatalf("transition = %v", resp.ClaimWindowTransitionAt)
	}
	if resp.ClaimWindowDurationMinutes != 300 {
		t.Fatalf("duration = %d", resp.ClaimWindowDurationMinutes)
	}
	closedNow, _ := time.Parse(time.RFC3339, "2026-06-22T01:00:00Z")
	closed := runtimeToResponseAt(scheduled, closedNow)
	if closed.ClaimWindowOpen == nil || *closed.ClaimWindowOpen {
		t.Fatalf("closed open = %v", closed.ClaimWindowOpen)
	}
	if closed.ClaimWindowTransitionAt == nil || *closed.ClaimWindowTransitionAt != "2026-06-22T02:00:00Z" {
		t.Fatalf("closed transition = %v", closed.ClaimWindowTransitionAt)
	}

	unscheduled := runtimeToResponseAt(db.AgentRuntime{}, now)
	if unscheduled.ClaimWindowStart != nil || unscheduled.ClaimWindowTimezone != nil || unscheduled.ClaimWindowOpen != nil || unscheduled.ClaimWindowTransitionAt != nil {
		t.Fatalf("unscheduled response has schedule state: %+v", unscheduled)
	}
	if unscheduled.ClaimWindowDurationMinutes != 300 {
		t.Fatalf("unscheduled duration = %d", unscheduled.ClaimWindowDurationMinutes)
	}

	malformed := scheduled
	malformed.ClaimWindowTimezone = pgtype.Text{String: "Mars/Olympus", Valid: true}
	malformedResp := runtimeToResponseAt(malformed, now)
	if malformedResp.ClaimWindowOpen == nil || *malformedResp.ClaimWindowOpen {
		t.Fatalf("malformed open = %v; want false", malformedResp.ClaimWindowOpen)
	}
	if malformedResp.ClaimWindowTransitionAt != nil {
		t.Fatalf("malformed transition = %v; want nil", malformedResp.ClaimWindowTransitionAt)
	}
}
