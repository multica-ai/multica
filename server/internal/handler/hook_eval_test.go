package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/automation"
)

// seedStatusChangedEvent inserts an issue.status_changed domain_event whose
// subject is issueID, with a from/to payload and the given correlation id.
func seedStatusChangedEvent(t *testing.T, issueID, from, to, correlationID string) string {
	t.Helper()
	var id string
	payload := fmt.Sprintf(`{"from":%q,"to":%q}`, from, to)
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO domain_event (workspace_id, type, schema_version, subject_type, subject_id, actor_type, actor_id, payload, correlation_id)
		VALUES ($1, 'issue.status_changed', 1, 'issue', $2, 'member', $3, $4::jsonb, $5)
		RETURNING id`,
		testWorkspaceID, issueID, testUserID, payload, correlationID).Scan(&id); err != nil {
		t.Fatalf("seed domain_event: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM domain_event WHERE id = $1`, id) })
	return id
}

func decodeEvaluation(t *testing.T, w *httptest.ResponseRecorder) automation.Evaluation {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var ev automation.Evaluation
	if err := json.NewDecoder(w.Body).Decode(&ev); err != nil {
		t.Fatalf("decode evaluation: %v", err)
	}
	return ev
}

func TestHookDryRun(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	issueID := seedHookIssue(t)
	eventID := seedStatusChangedEvent(t, issueID, "in_progress", "done", issueID)

	// A spec matching to=done fires; evaluated against current state.
	w := httptest.NewRecorder()
	testHandler.DryRunHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks/dry-run",
		map[string]any{"hook": sampleHookSpec("dr", "hi", issueID), "event_id": eventID}))
	ev := decodeEvaluation(t, w)
	if !ev.Matched || !ev.WouldFire || ev.Reason != automation.ReasonMatched {
		t.Fatalf("expected match, got %+v", ev)
	}
	if ev.EvaluatedAgainst != automation.EvaluatedAgainstCurrentState {
		t.Errorf("evaluated_against = %q, want current_state", ev.EvaluatedAgainst)
	}

	// A spec whose match requires to=blocked does not fire.
	spec := sampleHookSpec("dr2", "hi", issueID)
	spec["when"].(map[string]any)["match"] = map[string]any{"to": "blocked"}
	w = httptest.NewRecorder()
	testHandler.DryRunHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks/dry-run",
		map[string]any{"hook": spec, "event_id": eventID}))
	ev = decodeEvaluation(t, w)
	if ev.Matched || ev.Reason != automation.ReasonNoMatch {
		t.Errorf("expected no_match, got %+v", ev)
	}

	// A missing event is 404.
	w = httptest.NewRecorder()
	testHandler.DryRunHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks/dry-run",
		map[string]any{"hook": sampleHookSpec("dr3", "hi", issueID), "event_id": "99999999-9999-9999-9999-999999999999"}))
	if w.Code != http.StatusNotFound {
		t.Errorf("dry-run missing event: status %d, want 404", w.Code)
	}

	// An invalid spec is 400 (shape validation), never reaching evaluation.
	bad := sampleHookSpec("bad", "hi", issueID)
	bad["do"] = []any{map[string]any{"type": "set_issue_status_many"}}
	w = httptest.NewRecorder()
	testHandler.DryRunHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks/dry-run",
		map[string]any{"hook": bad, "event_id": eventID}))
	if w.Code != http.StatusBadRequest {
		t.Errorf("dry-run invalid spec: status %d, want 400", w.Code)
	}
}

// dry-run reads conditions against CURRENT workspace state, not the event moment.
func TestHookDryRunConditionsUseCurrentState(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	ctx := context.Background()
	issueID := seedHookIssue(t) // seeded as status 'todo'
	eventID := seedStatusChangedEvent(t, issueID, "todo", "in_progress", issueID)

	spec := sampleHookSpec("cond", "hi", issueID)
	spec["when"].(map[string]any)["match"] = map[string]any{}
	spec["if"] = []any{map[string]any{"issues_status": map[string]any{"ids": []any{issueID}, "all": "done"}}}

	// Issue is currently 'todo' → condition (all done) is false.
	w := httptest.NewRecorder()
	testHandler.DryRunHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks/dry-run",
		map[string]any{"hook": spec, "event_id": eventID}))
	if ev := decodeEvaluation(t, w); ev.ConditionsMet || ev.Reason != automation.ReasonConditionFalse {
		t.Fatalf("expected condition_false with issue todo, got %+v", ev)
	}

	// Move the issue to done → the same dry-run now reports conditions met.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET status = 'done' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("update issue: %v", err)
	}
	w = httptest.NewRecorder()
	testHandler.DryRunHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks/dry-run",
		map[string]any{"hook": spec, "event_id": eventID}))
	if ev := decodeEvaluation(t, w); !ev.ConditionsMet || ev.Reason != automation.ReasonMatched {
		t.Fatalf("expected conditions met after issue moved to done, got %+v", ev)
	}
}

func TestHookExplain(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	issueID := seedHookIssue(t)
	hook := createHookAs(t, testUserID, sampleHookSpec("explain hook", "hi", issueID)) // matches to=done

	matchEvent := seedStatusChangedEvent(t, issueID, "in_progress", "done", issueID)
	noMatchEvent := seedStatusChangedEvent(t, issueID, "in_progress", "todo", issueID)

	w := httptest.NewRecorder()
	testHandler.ExplainHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks/explain",
		map[string]any{"hook_id": hook.ID, "event_id": matchEvent}))
	if ev := decodeEvaluation(t, w); ev.Reason != automation.ReasonMatched {
		t.Errorf("explain matching event: reason %q, want matched (%+v)", ev.Reason, ev)
	}

	w = httptest.NewRecorder()
	testHandler.ExplainHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks/explain",
		map[string]any{"hook_id": hook.ID, "event_id": noMatchEvent}))
	if ev := decodeEvaluation(t, w); ev.Reason != automation.ReasonNoMatch {
		t.Errorf("explain non-matching event: reason %q, want no_match", ev.Reason)
	}

	// Unknown hook is 404.
	w = httptest.NewRecorder()
	testHandler.ExplainHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks/explain",
		map[string]any{"hook_id": "88888888-8888-8888-8888-888888888888", "event_id": matchEvent}))
	if w.Code != http.StatusNotFound {
		t.Errorf("explain unknown hook: status %d, want 404", w.Code)
	}
}

func TestHookEventsByCorrelation(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	issueID := seedHookIssue(t)
	const correlation = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	seedStatusChangedEvent(t, issueID, "todo", "in_progress", correlation)
	seedStatusChangedEvent(t, issueID, "in_progress", "done", correlation)
	// A different correlation must not leak in.
	seedStatusChangedEvent(t, issueID, "todo", "done", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	w := httptest.NewRecorder()
	testHandler.ListEventsByCorrelation(w, newMemberHookRequest(http.MethodGet, "/api/events?correlation_id="+correlation, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var events []DomainEventResponse
	json.NewDecoder(w.Body).Decode(&events)
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (the correlation chain only)", len(events))
	}
	for _, e := range events {
		if e.CorrelationID != correlation {
			t.Errorf("leaked event from correlation %s", e.CorrelationID)
		}
	}
}

// The whole read-only surface is invisible unless the feature flag is on.
func TestHookEvalRequiresFeatureFlag(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	prev := testHandler.FeatureFlags
	testHandler.FeatureFlags = nil
	t.Cleanup(func() { testHandler.FeatureFlags = prev })

	for _, tc := range []struct {
		name string
		call func(w *httptest.ResponseRecorder)
	}{
		{"dry-run", func(w *httptest.ResponseRecorder) {
			testHandler.DryRunHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks/dry-run", map[string]any{}))
		}},
		{"explain", func(w *httptest.ResponseRecorder) {
			testHandler.ExplainHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks/explain", map[string]any{}))
		}},
		{"events", func(w *httptest.ResponseRecorder) {
			testHandler.ListEventsByCorrelation(w, newMemberHookRequest(http.MethodGet, "/api/events", nil))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.call(w)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s with flag off: status %d, want 404", tc.name, w.Code)
			}
		})
	}
}
