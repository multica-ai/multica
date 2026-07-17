package automation

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeState is an in-memory StateReader: issueID -> field -> value.
type fakeState map[string]map[string]string

func (f fakeState) IssueField(_ context.Context, id, field string) (string, bool, error) {
	m, ok := f[id]
	if !ok {
		return "", false, nil
	}
	v, ok := m[field]
	return v, ok, nil
}

func statusChangedEvent(subjectID, from, to string) EventView {
	return EventView{
		Type:      "issue.status_changed",
		SubjectID: subjectID,
		ActorType: "member",
		ActorID:   uuidM,
		Payload:   map[string]any{"from": from, "to": to},
	}
}

func TestEvaluateEventTypeMismatch(t *testing.T) {
	ev, err := Evaluate(context.Background(),
		EventView{Type: "comment.created"},
		EvalRevision{EventType: "issue.status_changed", FireMode: FirePerEvent},
		fakeState{})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Reason != ReasonEventTypeMismatch || ev.Matched || ev.WouldFire {
		t.Fatalf("unexpected: %+v", ev)
	}
}

func TestEvaluateMatchClauses(t *testing.T) {
	event := statusChangedEvent(uuidA, "in_progress", "done")
	rev := EvalRevision{
		EventType: "issue.status_changed",
		Match:     json.RawMessage(`{"to":"done","subject_id":{"in":["` + uuidA + `"]},"from":{"exists":true}}`),
		FireMode:  FirePerEvent,
	}
	ev, err := Evaluate(context.Background(), event, rev, fakeState{})
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Matched || !ev.WouldFire || ev.Reason != ReasonMatched {
		t.Fatalf("expected match, got %+v", ev)
	}
	if len(ev.MatchClauses) != 3 {
		t.Errorf("clauses = %d, want 3", len(ev.MatchClauses))
	}

	// A wrong `to` value fails the match.
	rev.Match = json.RawMessage(`{"to":"blocked"}`)
	ev, _ = Evaluate(context.Background(), event, rev, fakeState{})
	if ev.Matched || ev.Reason != ReasonNoMatch {
		t.Errorf("expected no_match, got %+v", ev)
	}

	// exists:false on a present field fails.
	rev.Match = json.RawMessage(`{"to":{"exists":false}}`)
	ev, _ = Evaluate(context.Background(), event, rev, fakeState{})
	if ev.Matched {
		t.Errorf("exists:false on a present field should not match")
	}
}

func TestEvaluateConditionsAgainstCurrentState(t *testing.T) {
	event := statusChangedEvent(uuidA, "in_progress", "done")
	rev := EvalRevision{
		EventType:  "issue.status_changed",
		Match:      json.RawMessage(`{"to":"done"}`),
		FireMode:   FirePerEvent,
		Conditions: []ConditionSpec{{IssuesStatus: &IssuesStatusCond{IDs: []string{uuidA, uuidB}, All: "done"}}},
	}
	// Both done → conditions met.
	state := fakeState{uuidA: {"status": "done"}, uuidB: {"status": "done"}}
	ev, err := Evaluate(context.Background(), event, rev, state)
	if err != nil {
		t.Fatal(err)
	}
	if !ev.ConditionsMet || ev.Reason != ReasonMatched || ev.EvaluatedAgainst != EvaluatedAgainstCurrentState {
		t.Fatalf("expected conditions met against current state, got %+v", ev)
	}

	// B not done → all-condition fails.
	state[uuidB] = map[string]string{"status": "todo"}
	ev, _ = Evaluate(context.Background(), event, rev, state)
	if ev.ConditionsMet || ev.Reason != ReasonConditionFalse || ev.WouldFire {
		t.Fatalf("expected condition_false, got %+v", ev)
	}

	// A missing issue is treated as unsatisfied for `all`.
	rev.Conditions = []ConditionSpec{{IssuesStatus: &IssuesStatusCond{IDs: []string{uuidC}, All: "done"}}}
	ev, _ = Evaluate(context.Background(), event, rev, fakeState{})
	if ev.ConditionsMet {
		t.Errorf("missing issue must not satisfy an all-condition")
	}

	// `any` semantics: at least one match suffices.
	rev.Conditions = []ConditionSpec{{IssuesStatus: &IssuesStatusCond{IDs: []string{uuidA, uuidB}, Any: "done"}}}
	ev, _ = Evaluate(context.Background(), event, rev, fakeState{uuidA: {"status": "done"}, uuidB: {"status": "todo"}})
	if !ev.ConditionsMet {
		t.Errorf("any-condition should be met when one issue matches")
	}
}

func TestEvaluateIssueFieldCondition(t *testing.T) {
	event := statusChangedEvent(uuidA, "todo", "in_progress")
	rev := EvalRevision{
		EventType:  "issue.status_changed",
		Match:      json.RawMessage(`{}`),
		FireMode:   FirePerEvent,
		Conditions: []ConditionSpec{{IssueField: &IssueFieldCond{ID: uuidA, Field: "assignee_id", Eq: uuidM}}},
	}
	ev, _ := Evaluate(context.Background(), event, rev, fakeState{uuidA: {"assignee_id": uuidM}})
	if !ev.ConditionsMet {
		t.Errorf("issue_field eq should match")
	}
	ev, _ = Evaluate(context.Background(), event, rev, fakeState{uuidA: {"assignee_id": uuidB}})
	if ev.ConditionsMet {
		t.Errorf("issue_field eq should not match a different assignee")
	}
}

func TestEvaluateRisingEdgeNote(t *testing.T) {
	event := statusChangedEvent(uuidA, "in_progress", "done")
	rev := EvalRevision{
		EventType:  "issue.status_changed",
		Match:      json.RawMessage(`{"to":"done"}`),
		FireMode:   FireRisingEdge,
		Conditions: []ConditionSpec{{IssuesStatus: &IssuesStatusCond{IDs: []string{uuidA}, All: "done"}}},
	}
	ev, _ := Evaluate(context.Background(), event, rev, fakeState{uuidA: {"status": "done"}})
	if ev.Reason != ReasonMatched || ev.Note == "" {
		t.Fatalf("rising_edge matched should carry a read-only latch note, got %+v", ev)
	}
}
