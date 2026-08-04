// Package automation implements the Event Hooks MVP policy layer (issue
// MUL-4332): the user-authored hook specification, the versioned event schema
// registry every rule binds against, and the typed validator that rejects an
// illegal rule at the API boundary rather than deep inside a worker.
//
// This package is pure policy/config: it parses and validates hook specs and
// declares the fixed vocabulary of events, conditions and actions. It performs
// NO matching and NO execution — the durable matcher/executor is a later slice
// (PR3+) and will reuse the parsed spec and the schema registry defined here.
package automation

import (
	"encoding/json"
	"fmt"
)

// Fire modes (hook_revision.fire_mode).
const (
	FirePerEvent   = "per_event"
	FireRisingEdge = "rising_edge"
)

// Scope types (hook.scope_type). An issue-scoped hook is owned by / displayed
// on an issue; it does not implicitly restrict the event subject — that is the
// job of `when`.
const (
	ScopeWorkspace = "workspace"
	ScopeIssue     = "issue"
)

// HookSpec is the user-authored hook definition — the POST/PATCH request body.
// It maps onto two rows: the hook (name, scope) and its immutable revision
// (event_type, match, conditions, fire_mode, actions).
type HookSpec struct {
	Name  string          `json:"name"`
	Scope *ScopeSpec      `json:"scope,omitempty"`
	When  WhenSpec        `json:"when"`
	If    []ConditionSpec `json:"if,omitempty"`
	Fire  FireSpec        `json:"fire"`
	Do    []ActionSpec    `json:"do"`
}

// ScopeSpec is the hook lifecycle owner. Absent means workspace scope.
type ScopeSpec struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

// WhenSpec selects the triggering event and an optional set of match clauses on
// its envelope/payload fields. Match is kept raw here and parsed/validated by
// ParseMatch against the event's schema so the wire form and the typed model
// stay decoupled.
type WhenSpec struct {
	Event string          `json:"event"`
	Match json.RawMessage `json:"match,omitempty"`
}

// FireSpec selects per_event or rising_edge semantics.
type FireSpec struct {
	Mode string `json:"mode"`
}

// ConditionSpec is one predicate over current workspace state. Exactly one of
// the fixed-vocabulary variants must be set (§5.4).
type ConditionSpec struct {
	IssuesStatus *IssuesStatusCond `json:"issues_status,omitempty"`
	IssueField   *IssueFieldCond   `json:"issue_field,omitempty"`
}

// IssuesStatusCond asserts every (all) or any of the given issues is in a
// status. Exactly one of All/Any is set.
type IssuesStatusCond struct {
	IDs []string `json:"ids"`
	All string   `json:"all,omitempty"`
	Any string   `json:"any,omitempty"`
}

// IssueFieldCond asserts a single issue field equals / is in a set of values.
type IssueFieldCond struct {
	ID    string   `json:"id"`
	Field string   `json:"field"`
	Eq    string   `json:"eq,omitempty"`
	In    []string `json:"in,omitempty"`
}

// ActionSpec is one action in the ordered `do` list. Fields are a superset over
// all action types; the validator enforces the required/allowed set per type.
// Parameters are literals in v1 (event-field binding and templating land with
// the executor slice).
type ActionSpec struct {
	Type        string `json:"type"`
	IssueID     string `json:"issue_id,omitempty"`
	Status      string `json:"status,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
	MemberID    string `json:"member_id,omitempty"`
	Message     string `json:"message,omitempty"`
	AutopilotID string `json:"autopilot_id,omitempty"`
}

// MatchOp is the operator of a single match clause.
type MatchOp string

const (
	MatchEq     MatchOp = "eq"
	MatchIn     MatchOp = "in"
	MatchExists MatchOp = "exists"
)

// MatchClause is one parsed match predicate on a single event field path.
type MatchClause struct {
	Op     MatchOp
	Value  string   // for eq
	Set    []string // for in
	Exists bool     // for exists
}

// Match is the parsed set of clauses keyed by event field path.
type Match map[string]MatchClause

// ParseMatch decodes the raw `when.match` object into typed clauses. A value is
// either a JSON scalar (→ eq), an object {"in": [...]} (→ in) or an object
// {"exists": true|false} (→ exists). It does NOT check field paths against a
// schema — that is Validator.validateMatch's job — so it can be reused by the
// executor once the field set is already known-valid.
func ParseMatch(raw json.RawMessage) (Match, error) {
	if len(raw) == 0 {
		return Match{}, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("match must be an object: %w", err)
	}
	out := make(Match, len(obj))
	for field, rawVal := range obj {
		clause, err := parseMatchClause(rawVal)
		if err != nil {
			return nil, fmt.Errorf("match.%s: %w", field, err)
		}
		out[field] = clause
	}
	return out, nil
}

func parseMatchClause(raw json.RawMessage) (MatchClause, error) {
	// Try an object form first: {"in": [...]} or {"exists": bool}.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		switch {
		case len(obj) != 1:
			return MatchClause{}, fmt.Errorf("clause object must have exactly one of \"in\" or \"exists\"")
		case obj["in"] != nil:
			var set []string
			if err := json.Unmarshal(obj["in"], &set); err != nil {
				return MatchClause{}, fmt.Errorf("\"in\" must be a string array: %w", err)
			}
			if len(set) == 0 {
				return MatchClause{}, fmt.Errorf("\"in\" must not be empty")
			}
			return MatchClause{Op: MatchIn, Set: set}, nil
		case obj["exists"] != nil:
			var exists bool
			if err := json.Unmarshal(obj["exists"], &exists); err != nil {
				return MatchClause{}, fmt.Errorf("\"exists\" must be a boolean: %w", err)
			}
			return MatchClause{Op: MatchExists, Exists: exists}, nil
		default:
			return MatchClause{}, fmt.Errorf("unsupported clause; use a scalar, {\"in\": [...]} or {\"exists\": bool}")
		}
	}
	// Otherwise a scalar equality. Accept string/number/bool, normalised to string.
	var scalar any
	if err := json.Unmarshal(raw, &scalar); err != nil {
		return MatchClause{}, fmt.Errorf("value must be a scalar or clause object: %w", err)
	}
	switch v := scalar.(type) {
	case string:
		return MatchClause{Op: MatchEq, Value: v}, nil
	case bool:
		return MatchClause{Op: MatchEq, Value: fmt.Sprintf("%t", v)}, nil
	case float64:
		return MatchClause{Op: MatchEq, Value: formatNumber(v)}, nil
	default:
		return MatchClause{}, fmt.Errorf("equality value must be a string, number or boolean")
	}
}

func formatNumber(f float64) string {
	// Integers render without a trailing .0 so a match on an int field reads naturally.
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}
