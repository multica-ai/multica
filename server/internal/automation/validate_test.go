package automation

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	uuidA = "11111111-1111-1111-1111-111111111111"
	uuidB = "22222222-2222-2222-2222-222222222222"
	uuidC = "33333333-3333-3333-3333-333333333333"
	uuidM = "44444444-4444-4444-4444-444444444444"
)

// validPerEvent is the minimal accepted spec: comment.created → add a comment.
func validPerEvent() HookSpec {
	return HookSpec{
		Name: "notify on comment",
		When: WhenSpec{Event: "comment.created"},
		Fire: FireSpec{Mode: FirePerEvent},
		Do:   []ActionSpec{{Type: ActionAddComment, IssueID: uuidC, Message: "hi"}},
	}
}

// validRisingEdge is the design's A2 join: A and B both done → start C + inbox.
func validRisingEdge() HookSpec {
	return HookSpec{
		Name: "A/B done then start C",
		When: WhenSpec{Event: "issue.status_changed", Match: json.RawMessage(`{"subject_id":{"in":["` + uuidA + `","` + uuidB + `"]}}`)},
		If:   []ConditionSpec{{IssuesStatus: &IssuesStatusCond{IDs: []string{uuidA, uuidB}, All: "done"}}},
		Fire: FireSpec{Mode: FireRisingEdge},
		Do: []ActionSpec{
			{Type: ActionSetIssueStatus, IssueID: uuidC, Status: "todo"},
			{Type: ActionSendInbox, MemberID: uuidM, Message: "A/B done, C started"},
		},
	}
}

func TestValidateAcceptsValidSpecs(t *testing.T) {
	for name, spec := range map[string]HookSpec{
		"per_event minimal": validPerEvent(),
		"rising_edge join":  validRisingEdge(),
	} {
		if err := Validate(spec); err != nil {
			t.Errorf("%s: expected valid, got %v", name, err)
		}
	}
}

func TestValidateRejectsInvalidSpecs(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*HookSpec)
		wantSub string // substring the error message must contain
	}{
		{"missing name", func(s *HookSpec) { s.Name = "" }, "name is required"},
		{"unknown event", func(s *HookSpec) { s.When.Event = "issue.exploded" }, "non-authorable event"},
		{"system event not authorable", func(s *HookSpec) { s.When.Event = "issue.stage_completed" }, "non-authorable event"},
		{"undeclared match field", func(s *HookSpec) {
			s.When = WhenSpec{Event: "comment.created", Match: json.RawMessage(`{"nope":"x"}`)}
		}, "not declared"},
		{"uuid match field non-uuid", func(s *HookSpec) {
			s.When = WhenSpec{Event: "comment.created", Match: json.RawMessage(`{"issue_id":"not-a-uuid"}`)}
		}, "expects a uuid"},
		{"empty do", func(s *HookSpec) { s.Do = nil }, "at least one action"},
		{"too many actions", func(s *HookSpec) {
			s.Do = make([]ActionSpec, MaxActionsPerHook+1)
			for i := range s.Do {
				s.Do[i] = ActionSpec{Type: ActionAddComment, IssueID: uuidC, Message: "x"}
			}
		}, "at most"},
		{"system-only action", func(s *HookSpec) {
			s.Do = []ActionSpec{{Type: ActionSetIssueStatusMany}}
		}, "reserved for system"},
		{"unknown action", func(s *HookSpec) {
			s.Do = []ActionSpec{{Type: "delete_workspace"}}
		}, "unknown action type"},
		{"set_issue_status missing status", func(s *HookSpec) {
			s.Do = []ActionSpec{{Type: ActionSetIssueStatus, IssueID: uuidC}}
		}, "requires status"},
		{"trigger_agent bad agent", func(s *HookSpec) {
			s.Do = []ActionSpec{{Type: ActionTriggerAgent, IssueID: uuidC, AgentID: "x"}}
		}, "valid agent_id"},
		{"bad fire mode", func(s *HookSpec) { s.Fire.Mode = "always" }, "fire.mode"},
		{"scope issue without id", func(s *HookSpec) {
			s.Scope = &ScopeSpec{Type: ScopeIssue}
		}, "issue-scoped"},
		{"scope workspace with id", func(s *HookSpec) {
			s.Scope = &ScopeSpec{Type: ScopeWorkspace, ID: uuidA}
		}, "must be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := validPerEvent()
			tc.mutate(&spec)
			err := Validate(spec)
			if err == nil {
				t.Fatalf("expected rejection, got nil")
			}
			if _, ok := AsValidationError(err); !ok {
				t.Fatalf("expected *ValidationError, got %T", err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// The rising-edge dependency check (§5.2) is subtle enough to test on its own.
func TestValidateRisingEdgeCoverage(t *testing.T) {
	t.Run("requires a condition", func(t *testing.T) {
		spec := validRisingEdge()
		spec.If = nil
		if err := Validate(spec); err == nil || !strings.Contains(err.Error(), "at least one condition") {
			t.Fatalf("rising_edge without conditions must be rejected, got %v", err)
		}
	})
	t.Run("must listen to the condition's change event", func(t *testing.T) {
		spec := validRisingEdge()
		// issues_status depends on issue.status_changed; listening to comment.created
		// means the latch could never be re-evaluated.
		spec.When = WhenSpec{Event: "comment.created"}
		err := Validate(spec)
		if err == nil || !strings.Contains(err.Error(), "rising_edge hook must listen to") {
			t.Fatalf("rising_edge listening to the wrong event must be rejected, got %v", err)
		}
	})
	t.Run("assignee condition binds to issue.assigned", func(t *testing.T) {
		spec := HookSpec{
			Name: "assignee gate",
			When: WhenSpec{Event: "issue.assigned"},
			If:   []ConditionSpec{{IssueField: &IssueFieldCond{ID: uuidC, Field: IssueFieldAssigneeID, Eq: uuidM}}},
			Fire: FireSpec{Mode: FireRisingEdge},
			Do:   []ActionSpec{{Type: ActionAddComment, IssueID: uuidC, Message: "assigned"}},
		}
		if err := Validate(spec); err != nil {
			t.Fatalf("assignee rising_edge on issue.assigned should be valid, got %v", err)
		}
	})
}

// ParseMatch is the shared wire→typed step; verify each clause shape.
func TestParseMatch(t *testing.T) {
	m, err := ParseMatch(json.RawMessage(`{"to":"done","subject_id":{"in":["` + uuidA + `"]},"actor_id":{"exists":true}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := m["to"]; got.Op != MatchEq || got.Value != "done" {
		t.Errorf("to clause = %+v, want eq done", got)
	}
	if got := m["subject_id"]; got.Op != MatchIn || len(got.Set) != 1 || got.Set[0] != uuidA {
		t.Errorf("subject_id clause = %+v, want in [%s]", got, uuidA)
	}
	if got := m["actor_id"]; got.Op != MatchExists || !got.Exists {
		t.Errorf("actor_id clause = %+v, want exists true", got)
	}

	if _, err := ParseMatch(json.RawMessage(`{"to":{"in":[]}}`)); err == nil {
		t.Errorf("empty in set must be rejected")
	}
	if _, err := ParseMatch(json.RawMessage(`"notanobject"`)); err == nil {
		t.Errorf("non-object match must be rejected")
	}
}
