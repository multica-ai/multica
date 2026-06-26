package notification

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Engine is the top-level notification bus. It subscribes to the event bus,
// runs rules, runs detectors, and executes resulting actions via an
// ActionExecutor.
type Engine struct {
	bus      *events.Bus
	queries  *db.Queries
	rules    *RuleSet
	detectors *DetectorSet
	executor ActionExecutor

	mu          sync.Mutex
	// Rule cooldown tracking: key = "ruleID:sourceIssueID:targetIssueID"
	ruleCooldowns map[string]time.Time

	// Child terminal synthesis: when we see a child status→done/cancelled,
	// we synthesize a notification:child_terminal event for the rule engine.
}

// ActionExecutor executes Actions produced by rules and detectors.
type ActionExecutor interface {
	ExecuteAction(ctx context.Context, action Action, actorType, actorID string) error
}

// NewEngine creates an Engine connected to the given event bus and DB.
func NewEngine(bus *events.Bus, queries *db.Queries, executor ActionExecutor) *Engine {
	e := &Engine{
		bus:           bus,
		queries:       queries,
		rules:         NewRuleSet(),
		detectors:     NewDetectorSet(),
		executor:      executor,
		ruleCooldowns: make(map[string]time.Time),
	}

	RegisterBuiltinRules(e.rules)
	RegisterBuiltinDetectors(e.detectors)

	return e
}

// Start subscribes to event bus topics and begins processing events.
func (e *Engine) Start() {
	e.bus.Subscribe("issue:updated", e.handleIssueUpdated)
	e.bus.Subscribe("comment:created", e.handleCommentCreated)
	slog.Info("notification engine started",
		"rules", e.rules.Len(),
		"detectors", len(e.detectors.Detectors()))
}

// handleIssueUpdated is the main event handler for issue state changes.
// It runs rules first, then detectors.
func (e *Engine) handleIssueUpdated(ev events.Event) {
	e.processEvent(ev)

	// Synthesize child_terminal event for rules that depend on it
	_, parentID, _, newStatus, _, _ := parsePayloadIssue(ev.Payload)
	if parentID != "" && (newStatus == "done" || newStatus == "cancelled") {
		childID := ""
		if m, ok := ev.Payload.(map[string]any); ok {
			if v, ok := m["issue_id"].(string); ok {
				childID = v
			}
		}
		if childID != "" {
			synth := events.Event{
				Type:        "notification:child_terminal",
				WorkspaceID: ev.WorkspaceID,
				ActorType:   ev.ActorType,
				ActorID:     ev.ActorID,
				Payload: map[string]any{
					"child_id":        childID,
					"parent_issue_id": parentID,
					"terminal_status": newStatus,
				},
			}
			e.processEvent(synth)
		}
	}
}

// handleCommentCreated handles new comment events.
func (e *Engine) handleCommentCreated(ev events.Event) {
	e.processEvent(ev)
}

// processEvent runs all rules and detectors against a single event.
func (e *Engine) processEvent(ev events.Event) {
	rctx := &RuleContext{
		Ctx:       context.Background(),
		Queries:   e.queries,
		Actions:   &[]Action{},
		ActorType: ev.ActorType,
		ActorID:   ev.ActorID,
	}

	var allActions []Action

	// ---- Run rules (priority order) ----
	// Rules may suppress lower-priority rules when their content overlaps.
	type ruleResult struct {
		rule    *Rule
		actions []Action
	}
	var ruleResults []ruleResult

	for _, rule := range e.rules.Rules() {
		if !rule.Enabled {
			continue
		}
		if !rule.Match(rctx, ev) {
			continue
		}
		actions := rule.BuildActions(rctx, ev)
		if len(actions) == 0 {
			continue
		}

		// Check cooldown
		for _, a := range actions {
			if e.isRuleOnCooldown(rule.ID, ev, a.TargetIssueID, rule.Cooldown) {
				slog.Debug("rule on cooldown, skipping",
					"rule", rule.ID,
					"target", a.TargetIssueID)
				goto nextRule
			}
		}

		ruleResults = append(ruleResults, ruleResult{rule: rule, actions: actions})
	nextRule:
	}

	// Apply suppression: higher-priority rules suppress lower-priority ones
	// when they target the same issue.
	suppressed := make(map[string]bool) // "targetIssueID"
	for i, rr := range ruleResults {
		// Check if any higher-priority rule already targeted the same issue
		shouldSuppress := false
		for j := 0; j < i; j++ {
			for _, prevAction := range ruleResults[j].actions {
				for _, curAction := range rr.actions {
					if prevAction.TargetIssueID == curAction.TargetIssueID &&
						prevAction.Kind == ActionPostComment &&
						curAction.Kind == ActionPostComment {
						suppressed[curAction.TargetIssueID] = true
						shouldSuppress = true
						slog.Debug("rule suppressed by higher-priority rule",
							"suppressed", rr.rule.ID,
							"by", ruleResults[j].rule.ID,
							"target", curAction.TargetIssueID)
						break
					}
				}
				if shouldSuppress {
					break
				}
			}
			if shouldSuppress {
				break
			}
		}

		if !shouldSuppress {
			for _, a := range rr.actions {
				// Keep metadata updates even if comment is suppressed
				if a.Kind == ActionSetMetadata || a.Kind == ActionClearMetadata {
					allActions = append(allActions, a)
				} else if !suppressed[a.TargetIssueID] {
					allActions = append(allActions, a)
				}
			}
			// Mark cooldown
			for _, a := range rr.actions {
				e.markRuleCooldown(rr.rule.ID, ev, a.TargetIssueID)
			}
		} else {
			// Keep metadata updates
			for _, a := range rr.actions {
				if a.Kind == ActionSetMetadata || a.Kind == ActionClearMetadata {
					allActions = append(allActions, a)
				}
			}
		}
	}

	// ---- Run detectors ----
	for _, det := range e.detectors.Detectors() {
		if !det.Match(rctx, ev) {
			continue
		}
		detActions := det.Check(rctx, ev)
		if len(detActions) == 0 {
			continue
		}
		for _, a := range detActions {
			if e.detectors.IsCoolingDown(det.ID, a.TargetIssueID, 30*time.Minute) {
				continue
			}
			allActions = append(allActions, a)
			e.detectors.MarkCooled(det.ID, a.TargetIssueID)
		}
	}

	// ---- Execute actions ----
	for _, a := range allActions {
		if err := e.executor.ExecuteAction(context.Background(), a, ev.ActorType, ev.ActorID); err != nil {
			slog.Warn("notification action failed",
				"action", a.Kind,
				"target", a.TargetIssueID,
				"error", err)
		}
	}
}

// isRuleOnCooldown checks whether a rule has recently fired for the same (source, target) pair.
func (e *Engine) isRuleOnCooldown(ruleID string, ev events.Event, targetID string, cooldown time.Duration) bool {
	if cooldown <= 0 {
		return false
	}
	issueID := ""
	if m, ok := ev.Payload.(map[string]any); ok {
		if v, ok := m["issue_id"].(string); ok {
			issueID = v
		}
	}
	key := ruleID + ":" + issueID + ":" + targetID

	e.mu.Lock()
	defer e.mu.Unlock()
	last, ok := e.ruleCooldowns[key]
	if !ok {
		return false
	}
	return time.Since(last) < cooldown
}

// markRuleCooldown records that a rule has just fired.
func (e *Engine) markRuleCooldown(ruleID string, ev events.Event, targetID string) {
	issueID := ""
	if m, ok := ev.Payload.(map[string]any); ok {
		if v, ok := m["issue_id"].(string); ok {
			issueID = v
		}
	}
	key := ruleID + ":" + issueID + ":" + targetID

	e.mu.Lock()
	defer e.mu.Unlock()
	e.ruleCooldowns[key] = time.Now()
}
