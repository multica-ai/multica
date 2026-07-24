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
	if ev.Reason != ReasonEventTypeMismatch || ev.Matched || ev.Eligible {
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
	if !ev.Matched || !ev.Eligible || ev.DecisionComplete || ev.Reason != ReasonMatched {
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
	if ev.ConditionsMet || ev.Reason != ReasonConditionFalse || ev.Eligible {
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

// The evaluator output must carry observed + expected + op + present so a matcher
// can store one condition/match snapshot without re-reading state (review point 2).
func TestEvaluateStructuredSnapshot(t *testing.T) {
	event := statusChangedEvent(uuidA, "in_progress", "done")
	rev := EvalRevision{
		EventType:  "issue.status_changed",
		Match:      json.RawMessage(`{"to":"done"}`),
		FireMode:   FirePerEvent,
		Conditions: []ConditionSpec{{IssuesStatus: &IssuesStatusCond{IDs: []string{uuidA, uuidB}, All: "done"}}},
	}
	state := fakeState{uuidA: {"status": "done"}, uuidB: {"status": "todo"}}
	ev, err := Evaluate(context.Background(), event, rev, state)
	if err != nil {
		t.Fatal(err)
	}

	// The match clause records op + observed + expected.
	if len(ev.MatchClauses) != 1 {
		t.Fatalf("match clauses = %d, want 1", len(ev.MatchClauses))
	}
	mc := ev.MatchClauses[0]
	if mc.Field != "to" || mc.Op != string(MatchEq) || mc.Observed != "done" || !mc.Present || len(mc.Expected) != 1 || mc.Expected[0] != "done" || !mc.Matched {
		t.Errorf("match clause not fully structured: %+v", mc)
	}

	// The condition records mode + expected + per-issue observed status.
	if len(ev.Conditions) != 1 {
		t.Fatalf("conditions = %d, want 1", len(ev.Conditions))
	}
	c := ev.Conditions[0]
	if c.Kind != "issues_status" || c.Mode != "all" || len(c.Expected) != 1 || c.Expected[0] != "done" || len(c.Issues) != 2 {
		t.Fatalf("condition not fully structured: %+v", c)
	}
	byID := map[string]IssueObserved{}
	for _, io := range c.Issues {
		byID[io.ID] = io
	}
	if a := byID[uuidA]; a.Observed != "done" || !a.Present || !a.Matched {
		t.Errorf("issue A observed wrong: %+v", a)
	}
	if b := byID[uuidB]; b.Observed != "todo" || !b.Present || b.Matched {
		t.Errorf("issue B observed wrong: %+v", b)
	}

	// Snapshots serialize the same structured inputs the matcher will store.
	ms, err := ev.MatchSnapshot()
	if err != nil || len(ms) == 0 {
		t.Fatalf("match snapshot: %v", err)
	}
	cs, err := ev.ConditionSnapshot()
	if err != nil || len(cs) == 0 {
		t.Fatalf("condition snapshot: %v", err)
	}
}

// ProjectPayload is the fail-closed redaction for the correlation debug view.
func TestProjectPayload(t *testing.T) {
	p := ProjectPayload("issue.created", map[string]any{"status": "todo", "title": "secret", "priority": "high", "bogus": 1})
	if _, ok := p["title"]; ok {
		t.Error("free-text title must be redacted")
	}
	if _, ok := p["bogus"]; ok {
		t.Error("undeclared field must be dropped")
	}
	if p["status"] != "todo" || p["priority"] != "high" {
		t.Errorf("declared fields dropped: %v", p)
	}
	if len(ProjectPayload("issue.exploded", map[string]any{"x": 1})) != 0 {
		t.Error("an unknown event type must project to an empty payload (fail-closed)")
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
