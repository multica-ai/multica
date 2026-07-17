package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// This file is the single read-only evaluator for Event Hooks (MUL-4332 PR3,
// ratified decision 2A). The matcher (a later PR3 slice) and the dry-run/explain
// debug surface share it, so an explanation can never drift from real execution.
// It computes only WHETHER an event matches a revision and whether the revision's
// conditions hold against current workspace state; it performs NO action and
// mutates NO durable state (no rising-edge latch, rate bucket, execution or
// effect). The `when` match reads the event's own (historical) payload; the `if`
// conditions read current state via StateReader.

// Stable reason codes. explain/dry-run and (later) the matcher's skip_reason draw
// from this fixed vocabulary so callers can branch on a stable string.
const (
	ReasonMatched           = "matched"             // matches and conditions currently hold
	ReasonEventTypeMismatch = "event_type_mismatch" // the event is not the hook's event type
	ReasonNoMatch           = "no_match"            // a when-clause did not match
	ReasonConditionFalse    = "condition_false"     // matched, but an if-condition is not satisfied
)

// EvaluatedAgainstCurrentState labels the state basis of a condition evaluation
// (2A): conditions read current workspace state, not the event's point in time.
const EvaluatedAgainstCurrentState = "current_state"

// StateReader reads the current value of an issue field for condition evaluation.
// Implemented by the service against workspace-scoped queries; kept as an
// interface so this package stays pure and unit-testable.
type StateReader interface {
	// IssueField returns the current value of field (status | assignee_id |
	// parent_issue_id) for the issue. exists is false when the issue is absent
	// from the workspace, in which case the predicate is treated as unsatisfied.
	IssueField(ctx context.Context, issueID, field string) (value string, exists bool, err error)
}

// EventView is the read-only projection of a domain event the evaluator matches
// against: envelope fields plus the decoded payload object.
type EventView struct {
	Type      string
	SubjectID string
	ActorType string
	ActorID   string
	Payload   map[string]any
}

// EvalRevision is the parsed hook revision configuration the evaluator needs.
type EvalRevision struct {
	EventType  string
	Match      json.RawMessage
	Conditions []ConditionSpec
	FireMode   string
}

// ClauseResult is the per-field outcome of one when-match clause. It records the
// operator, the observed event value and presence, and the expected operand, so a
// single structured result feeds dry-run, explain and (next slice) the matcher's
// stored match_snapshot — no re-derivation, no parallel logic (review point 2).
type ClauseResult struct {
	Field    string   `json:"field"`
	Op       string   `json:"op"`
	Observed string   `json:"observed,omitempty"`
	Present  bool     `json:"present"`
	Expected []string `json:"expected,omitempty"`
	Matched  bool     `json:"matched"`
}

// IssueObserved records the observed current value of one issue field consulted
// by a condition, and whether that issue satisfied the predicate.
type IssueObserved struct {
	ID       string `json:"id"`
	Field    string `json:"field"`
	Observed string `json:"observed,omitempty"`
	Present  bool   `json:"present"`
	Matched  bool   `json:"matched"`
}

// ConditionResult is the structured outcome of one if-condition against current
// state: the operator/expected operand plus every observed input, so the same
// object is the condition_snapshot the matcher will store (review point 2).
type ConditionResult struct {
	Kind     string          `json:"kind"` // issues_status | issue_field
	Matched  bool            `json:"matched"`
	Mode     string          `json:"mode,omitempty"`     // all | any (issues_status)
	Field    string          `json:"field,omitempty"`    // issue field (issue_field)
	Op       string          `json:"op,omitempty"`       // eq | in (issue_field)
	Expected []string        `json:"expected,omitempty"` // target status / eq value / in-set
	Issues   []IssueObserved `json:"issues,omitempty"`   // per-issue observed state
}

// Evaluation is the complete read-only decision for one (event, revision) pair.
//
// Eligible means the when-match held and every if-condition is currently
// satisfied. It is NOT a fire decision: a rising-edge latch and the depth / rate
// / permission / fuse guards are not evaluated in read-only mode, so
// DecisionComplete is false and no field ever claims the rule "will execute"
// (review point 1). The matcher (next slice) will set DecisionComplete and the
// final fire verdict after evaluating the latch and guards.
type Evaluation struct {
	Event            string            `json:"event"`
	HookEvent        string            `json:"hook_event"`
	FireMode         string            `json:"fire_mode"`
	Matched          bool              `json:"matched"`
	MatchClauses     []ClauseResult    `json:"match_clauses"`
	ConditionsMet    bool              `json:"conditions_met"`
	Conditions       []ConditionResult `json:"conditions"`
	Eligible         bool              `json:"eligible"`
	DecisionComplete bool              `json:"decision_complete"`
	Reason           string            `json:"reason"`
	EvaluatedAgainst string            `json:"evaluated_against"`
	Note             string            `json:"note,omitempty"`
}

// MatchSnapshot / ConditionSnapshot serialize the structured inputs+conclusions
// exactly as the matcher will persist them, so dry-run/explain and the stored
// execution snapshot are byte-identical for the same (event, revision, state).
func (e Evaluation) MatchSnapshot() (json.RawMessage, error)     { return json.Marshal(e.MatchClauses) }
func (e Evaluation) ConditionSnapshot() (json.RawMessage, error) { return json.Marshal(e.Conditions) }

// Evaluate is the shared read-only decision. It never mutates durable state.
func Evaluate(ctx context.Context, event EventView, rev EvalRevision, state StateReader) (Evaluation, error) {
	ev := Evaluation{
		Event:            event.Type,
		HookEvent:        rev.EventType,
		FireMode:         rev.FireMode,
		EvaluatedAgainst: EvaluatedAgainstCurrentState,
	}

	// A revision only matches events of its own type.
	if event.Type != rev.EventType {
		ev.Reason = ReasonEventTypeMismatch
		return ev, nil
	}

	match, err := ParseMatch(rev.Match)
	if err != nil {
		return Evaluation{}, fmt.Errorf("parse stored match: %w", err)
	}
	ev.Matched, ev.MatchClauses = evalMatch(event, match)
	if !ev.Matched {
		ev.Reason = ReasonNoMatch
		return ev, nil
	}

	ev.ConditionsMet = true
	for _, c := range rev.Conditions {
		cr, err := evalCondition(ctx, c, state)
		if err != nil {
			return Evaluation{}, err
		}
		ev.Conditions = append(ev.Conditions, cr)
		if !cr.Matched {
			ev.ConditionsMet = false
		}
	}
	if !ev.ConditionsMet {
		ev.Reason = ReasonConditionFalse
		return ev, nil
	}

	// The event matches and conditions currently hold. This makes the rule
	// ELIGIBLE, but not a completed fire decision: read-only evaluation does not
	// consult the rising-edge latch or the depth/rate/permission/fuse guards, so
	// DecisionComplete stays false and no field claims execution (review point 1).
	ev.Eligible = true
	ev.Reason = ReasonMatched
	if rev.FireMode == FireRisingEdge {
		ev.Note = "eligible now, but rising_edge fires only on a false→true edge; the durable latch and guards are not evaluated in read-only mode"
	} else {
		ev.Note = "eligible now; the depth/rate/permission/fuse guards are not evaluated in read-only mode"
	}
	return ev, nil
}

// field resolves an event field path to a normalized string value.
func (e EventView) field(path string) (string, bool) {
	switch path {
	case "subject_id":
		return e.SubjectID, e.SubjectID != ""
	case "actor_type":
		return e.ActorType, e.ActorType != ""
	case "actor_id":
		return e.ActorID, e.ActorID != ""
	}
	raw, ok := e.Payload[path]
	if !ok {
		return "", false
	}
	return normalizeScalar(raw)
}

func evalMatch(event EventView, m Match) (bool, []ClauseResult) {
	matched := true
	results := make([]ClauseResult, 0, len(m))
	for field, clause := range m {
		val, present := event.field(field)
		ok := evalClause(clause, val, present)
		if !ok {
			matched = false
		}
		results = append(results, ClauseResult{
			Field:    field,
			Op:       string(clause.Op),
			Observed: val,
			Present:  present,
			Expected: clauseExpected(clause),
			Matched:  ok,
		})
	}
	// Stable order for deterministic snapshots / responses.
	sort.Slice(results, func(i, j int) bool { return results[i].Field < results[j].Field })
	return matched, results
}

func evalClause(c MatchClause, val string, present bool) bool {
	switch c.Op {
	case MatchEq:
		return present && val == c.Value
	case MatchIn:
		return present && contains(c.Set, val)
	case MatchExists:
		return present == c.Exists
	}
	return false
}

func clauseExpected(c MatchClause) []string {
	switch c.Op {
	case MatchEq:
		return []string{c.Value}
	case MatchIn:
		return c.Set
	case MatchExists:
		return []string{fmt.Sprintf("%t", c.Exists)}
	}
	return nil
}

func evalCondition(ctx context.Context, c ConditionSpec, state StateReader) (ConditionResult, error) {
	if c.IssuesStatus != nil {
		return evalIssuesStatus(ctx, *c.IssuesStatus, state)
	}
	if c.IssueField != nil {
		return evalIssueField(ctx, *c.IssueField, state)
	}
	// A stored, validated condition always has exactly one variant.
	return ConditionResult{Kind: "unknown", Matched: false}, nil
}

func evalIssuesStatus(ctx context.Context, c IssuesStatusCond, state StateReader) (ConditionResult, error) {
	target, mode := c.All, "all"
	if c.Any != "" {
		target, mode = c.Any, "any"
	}
	allHit, anyHit := true, false
	observed := make([]IssueObserved, 0, len(c.IDs))
	for _, id := range c.IDs {
		status, exists, err := state.IssueField(ctx, id, IssueFieldStatus)
		if err != nil {
			return ConditionResult{}, err
		}
		hit := exists && status == target
		if hit {
			anyHit = true
		} else {
			allHit = false
		}
		observed = append(observed, IssueObserved{ID: id, Field: IssueFieldStatus, Observed: status, Present: exists, Matched: hit})
	}
	met := allHit
	if mode == "any" {
		met = anyHit
	}
	return ConditionResult{
		Kind:     "issues_status",
		Matched:  met,
		Mode:     mode,
		Field:    IssueFieldStatus,
		Op:       string(MatchEq),
		Expected: []string{target},
		Issues:   observed,
	}, nil
}

func evalIssueField(ctx context.Context, c IssueFieldCond, state StateReader) (ConditionResult, error) {
	val, exists, err := state.IssueField(ctx, c.ID, c.Field)
	if err != nil {
		return ConditionResult{}, err
	}
	op, expected := string(MatchEq), []string{c.Eq}
	met := false
	if c.Eq == "" {
		op, expected = string(MatchIn), c.In
	}
	if exists {
		if c.Eq != "" {
			met = val == c.Eq
		} else {
			met = contains(c.In, val)
		}
	}
	return ConditionResult{
		Kind:     "issue_field",
		Matched:  met,
		Field:    c.Field,
		Op:       op,
		Expected: expected,
		Issues:   []IssueObserved{{ID: c.ID, Field: c.Field, Observed: val, Present: exists, Matched: met}},
	}, nil
}

func normalizeScalar(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		return fmt.Sprintf("%t", t), true
	case float64:
		return formatNumber(t), true
	default:
		// null, arrays and objects are not matchable scalars.
		return "", false
	}
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}
