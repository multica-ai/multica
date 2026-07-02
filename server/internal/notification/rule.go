// Package notification implements the cross-squad agent collaboration
// notification bus: a rule engine + event-driven detectors that turn issue
// lifecycle events into automatic notifications (comments, @mentions,
// metadata updates, and status transitions).
//
// Architecture:
//
//	events.Bus (existing) → Rule Engine (new) → Action Executor (new)
//	                      → Detectors (new)     → Action Executor
//
// All components are pure event consumers. No timers, no cron, no delay.
// Time-dependent detectors use opportunistic "event piggyback" checks.
package notification

import (
	"context"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
)

// ActionKind enumerates the side-effects a rule or detector may request.
type ActionKind string

const (
	ActionPostComment  ActionKind = "post_comment"
	ActionMentionAgent ActionKind = "mention_agent"
	ActionUpdateStatus ActionKind = "update_status"
	ActionSetMetadata  ActionKind = "set_metadata"
	ActionClearMetadata ActionKind = "clear_metadata"
	ActionEscalate      ActionKind = "escalate"
)

// An Action is one side-effect emitted by a rule or detector.
type Action struct {
	Kind      ActionKind `json:"kind"`
	TargetIssueID string `json:"target_issue_id"` // where the action lands
	// Optional fields per kind:
	Template  string            `json:"template,omitempty"`  // comment body template
	AgentID   string            `json:"agent_id,omitempty"`  // for mention_agent
	NewStatus string            `json:"new_status,omitempty"` // for update_status
	MetaKey   string            `json:"meta_key,omitempty"`   // for set_metadata / clear_metadata
	MetaValue any               `json:"meta_value,omitempty"` // for set_metadata
	TemplateVars map[string]any `json:"-"`                    // populated at trigger time
}

// A Rule is a declarative notification rule.
type Rule struct {
	ID          string
	Description string
	Priority    int // lower = higher priority (R5=10, R2=20, R4=30, R3=80, R1=100)
	// Match returns true when this rule should fire for the given event.
	// The context carries a DB handle (see RuleContext) for queries.
	Match func(ctx *RuleContext, ev events.Event) bool
	// BuildActions produces the side-effects for a matched event.
	BuildActions func(ctx *RuleContext, ev events.Event) []Action
	// Cooldown is the minimum interval between two firings of this rule
	// for the same (source_issue_id, target_issue_id) pair.
	Cooldown time.Duration
	// Enabled can be toggled at runtime (future: workspace-level config).
	Enabled bool
}

// RuleContext provides rules with access to shared infrastructure during
// matching and action building. The concrete implementation lives in engine.go.
type RuleContext struct {
	Ctx      context.Context
	Queries  interface{} // *db.Queries — imported as interface to avoid circular deps
	DB       interface{} // dbExecutor
	Actions  *[]Action   // output accumulator; rules append here
	ActorType string
	ActorID   string
}

// RuleSet is a priority-ordered collection of rules.
type RuleSet struct {
	rules []*Rule
}

// NewRuleSet creates an empty RuleSet.
func NewRuleSet() *RuleSet {
	return &RuleSet{}
}

// Add inserts a rule, maintaining priority order (lowest first = highest priority).
func (rs *RuleSet) Add(r *Rule) {
	idx := 0
	for ; idx < len(rs.rules); idx++ {
		if r.Priority < rs.rules[idx].Priority {
			break
		}
	}
	rs.rules = append(rs.rules, nil)
	copy(rs.rules[idx+1:], rs.rules[idx:])
	rs.rules[idx] = r
}

// Rules returns a copy of the rule slice in priority order.
func (rs *RuleSet) Rules() []*Rule {
	out := make([]*Rule, len(rs.rules))
	copy(out, rs.rules)
	return out
}

// Len returns the number of rules.
func (rs *RuleSet) Len() int {
	return len(rs.rules)
}
