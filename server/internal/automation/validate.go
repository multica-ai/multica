package automation

import (
	"errors"
	"fmt"

	"github.com/multica-ai/multica/server/internal/util"
)

// Guardrail limits (§9). Rate/concurrency/depth limits belong to the executor
// slice; these are the shape limits enforced at author time.
const (
	MaxActionsPerHook = 8
	MaxNameLength     = 200
	MaxConditionIDs   = 100
	MaxMatchSetSize   = 100
	MaxMessageLength  = 4000
)

// ValidationError is a user-fixable problem with a hook spec. Handlers map it to
// HTTP 400 (never 500) so a bad rule can never reach a worker (§13).
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

// NewValidationError builds a ValidationError. Exported so callers that extend
// author-time validation with checks this pure package cannot do (e.g. the
// service's workspace-scoped target existence checks) surface the same typed
// error and share the handler's single 400 mapping.
func NewValidationError(format string, args ...any) *ValidationError {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

func verr(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// AsValidationError reports whether err is a ValidationError.
func AsValidationError(err error) (*ValidationError, bool) {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve, true
	}
	return nil, false
}

// Validate performs complete typed validation of a user-authored hook spec:
// event schema, match fields, condition dependency, action schema, fire-mode
// coverage and shape limits. It is the single author-time gate reused by both
// the create and patch paths.
func Validate(spec HookSpec) error {
	if spec.Name == "" {
		return verr("name is required")
	}
	if len(spec.Name) > MaxNameLength {
		return verr("name must be at most %d characters", MaxNameLength)
	}
	if err := validateScope(spec.Scope); err != nil {
		return err
	}

	schema, ok := SchemaFor(spec.When.Event)
	if !ok {
		return verr("unknown or non-authorable event type %q", spec.When.Event)
	}
	if err := validateMatch(spec.When.Match, schema); err != nil {
		return err
	}

	for i, cond := range spec.If {
		if err := validateCondition(cond); err != nil {
			return verr("if[%d]: %s", i, err.Error())
		}
	}

	if err := validateFire(spec); err != nil {
		return err
	}

	if len(spec.Do) == 0 {
		return verr("do must contain at least one action")
	}
	if len(spec.Do) > MaxActionsPerHook {
		return verr("do must contain at most %d actions", MaxActionsPerHook)
	}
	for i, action := range spec.Do {
		if err := validateAction(action); err != nil {
			return verr("do[%d]: %s", i, err.Error())
		}
	}
	return nil
}

func validateScope(scope *ScopeSpec) error {
	if scope == nil {
		return nil
	}
	switch scope.Type {
	case ScopeWorkspace:
		if scope.ID != "" {
			return verr("scope.id must be empty for a workspace-scoped hook")
		}
	case ScopeIssue:
		if !validUUID(scope.ID) {
			return verr("scope.id must be a valid issue id for an issue-scoped hook")
		}
	default:
		return verr("scope.type must be %q or %q", ScopeWorkspace, ScopeIssue)
	}
	return nil
}

func validateMatch(raw []byte, schema EventSchema) error {
	match, err := ParseMatch(raw)
	if err != nil {
		return verr("%s", err.Error())
	}
	for field, clause := range match {
		kind, ok := schema.MatchFields[field]
		if !ok {
			return verr("match field %q is not declared by event %q", field, schema.Type)
		}
		if err := validateClauseValues(field, kind, clause); err != nil {
			return err
		}
	}
	return nil
}

func validateClauseValues(field string, kind FieldKind, clause MatchClause) error {
	if clause.Op == MatchIn && len(clause.Set) > MaxMatchSetSize {
		return verr("match field %q has %d values, at most %d allowed", field, len(clause.Set), MaxMatchSetSize)
	}
	if kind != FieldUUID {
		return nil // string fields accept any scalar; existence needs no value check
	}
	switch clause.Op {
	case MatchEq:
		if !validUUID(clause.Value) {
			return verr("match field %q expects a uuid, got %q", field, clause.Value)
		}
	case MatchIn:
		for _, v := range clause.Set {
			if !validUUID(v) {
				return verr("match field %q expects uuids, got %q", field, v)
			}
		}
	}
	return nil
}

func validateCondition(c ConditionSpec) error {
	set := 0
	if c.IssuesStatus != nil {
		set++
	}
	if c.IssueField != nil {
		set++
	}
	if set != 1 {
		return verr("exactly one of issues_status or issue_field must be set")
	}
	if c.IssuesStatus != nil {
		return validateIssuesStatus(*c.IssuesStatus)
	}
	return validateIssueField(*c.IssueField)
}

func validateIssuesStatus(c IssuesStatusCond) error {
	if len(c.IDs) == 0 {
		return verr("issues_status.ids must not be empty")
	}
	if len(c.IDs) > MaxConditionIDs {
		return verr("issues_status.ids has %d ids, at most %d allowed", len(c.IDs), MaxConditionIDs)
	}
	for _, id := range c.IDs {
		if !validUUID(id) {
			return verr("issues_status.ids must be uuids, got %q", id)
		}
	}
	hasAll, hasAny := c.All != "", c.Any != ""
	if hasAll == hasAny {
		return verr("exactly one of issues_status.all or issues_status.any must be set")
	}
	status := c.All
	if hasAny {
		status = c.Any
	}
	if !isValidIssueStatus(status) {
		return verr("issues_status status %q is not a valid issue status", status)
	}
	return nil
}

func validateIssueField(c IssueFieldCond) error {
	if !validUUID(c.ID) {
		return verr("issue_field.id must be a uuid")
	}
	if !validIssueFields[c.Field] {
		return verr("issue_field.field must be one of status, assignee_id, parent_issue_id")
	}
	hasEq, hasIn := c.Eq != "", len(c.In) > 0
	if hasEq == hasIn {
		return verr("exactly one of issue_field.eq or issue_field.in must be set")
	}
	if len(c.In) > MaxConditionIDs {
		return verr("issue_field.in has %d values, at most %d allowed", len(c.In), MaxConditionIDs)
	}
	switch c.Field {
	case IssueFieldStatus:
		// status is a free string but must be a persistable issue status.
		for _, v := range collectValues(c.Eq, c.In) {
			if !isValidIssueStatus(v) {
				return verr("issue_field status %q is not a valid issue status", v)
			}
		}
	default:
		// id-shaped fields (assignee_id / parent_issue_id) require uuids.
		if hasEq && !validUUID(c.Eq) {
			return verr("issue_field.eq must be a uuid for field %q", c.Field)
		}
		for _, v := range c.In {
			if !validUUID(v) {
				return verr("issue_field.in must be uuids for field %q", c.Field)
			}
		}
	}
	return nil
}

// collectValues returns eq (if set) plus the in slice as one list.
func collectValues(eq string, in []string) []string {
	out := make([]string, 0, len(in)+1)
	if eq != "" {
		out = append(out, eq)
	}
	out = append(out, in...)
	return out
}

func validateFire(spec HookSpec) error {
	switch spec.Fire.Mode {
	case FirePerEvent:
		return nil
	case FireRisingEdge:
		return validateRisingEdgeCoverage(spec)
	default:
		return verr("fire.mode must be %q or %q", FirePerEvent, FireRisingEdge)
	}
}

// validateRisingEdgeCoverage enforces §5.2: a rising_edge hook's latch can only
// be re-evaluated by the event it listens to, so every condition must depend on
// exactly the hook's own event type, and there must be at least one condition to
// gate on. This is the extractable dependency check the design requires at save
// time for the v1 fixed vocabulary.
func validateRisingEdgeCoverage(spec HookSpec) error {
	if len(spec.If) == 0 {
		return verr("rising_edge requires at least one condition in if")
	}
	for i, cond := range spec.If {
		dep, ok := conditionDependencyEvent(cond)
		if !ok {
			return verr("if[%d]: condition has no known change event, cannot use rising_edge", i)
		}
		if dep != spec.When.Event {
			return verr("rising_edge hook must listen to %q so its condition in if[%d] can be re-evaluated, but when.event is %q", dep, i, spec.When.Event)
		}
	}
	return nil
}

// actionAllowedFields declares the exact set of ActionSpec parameter fields each
// action type may set. Any other non-empty field is rejected so a stray param
// (e.g. an agent_id smuggled onto add_comment) can never be persisted onto a
// revision (MUL-4332 PR2 review point 3).
var actionAllowedFields = map[string]map[string]bool{
	ActionSetIssueStatus: {"issue_id": true, "status": true},
	ActionTriggerAgent:   {"issue_id": true, "agent_id": true},
	ActionAddComment:     {"issue_id": true, "message": true},
	ActionSendInbox:      {"member_id": true, "message": true},
	ActionRunAutopilot:   {"autopilot_id": true},
}

func validateAction(a ActionSpec) error {
	if systemActionTypes[a.Type] {
		return verr("action type %q is reserved for system hooks", a.Type)
	}
	if !userActionTypes[a.Type] {
		return verr("unknown action type %q", a.Type)
	}
	if err := rejectUnexpectedActionFields(a); err != nil {
		return err
	}
	if a.Message != "" && len(a.Message) > MaxMessageLength {
		return verr("%s message must be at most %d characters", a.Type, MaxMessageLength)
	}
	switch a.Type {
	case ActionSetIssueStatus:
		if !validUUID(a.IssueID) {
			return verr("set_issue_status requires a valid issue_id")
		}
		if a.Status == "" {
			return verr("set_issue_status requires status")
		}
		if !isValidIssueStatus(a.Status) {
			return verr("set_issue_status status %q is not a valid issue status", a.Status)
		}
	case ActionTriggerAgent:
		if !validUUID(a.IssueID) {
			return verr("trigger_agent requires a valid issue_id")
		}
		if !validUUID(a.AgentID) {
			return verr("trigger_agent requires a valid agent_id")
		}
	case ActionAddComment:
		if !validUUID(a.IssueID) {
			return verr("add_comment requires a valid issue_id")
		}
		if a.Message == "" {
			return verr("add_comment requires message")
		}
	case ActionSendInbox:
		if !validUUID(a.MemberID) {
			return verr("send_inbox requires a valid member_id")
		}
		if a.Message == "" {
			return verr("send_inbox requires message")
		}
	case ActionRunAutopilot:
		if !validUUID(a.AutopilotID) {
			return verr("run_autopilot requires a valid autopilot_id")
		}
	}
	return nil
}

// rejectUnexpectedActionFields fails if the action sets any parameter field not
// allowed for its type — strict "exactly the allowed fields" enforcement.
func rejectUnexpectedActionFields(a ActionSpec) error {
	allowed := actionAllowedFields[a.Type]
	present := map[string]string{
		"issue_id":     a.IssueID,
		"status":       a.Status,
		"agent_id":     a.AgentID,
		"member_id":    a.MemberID,
		"message":      a.Message,
		"autopilot_id": a.AutopilotID,
	}
	for field, value := range present {
		if value != "" && !allowed[field] {
			return verr("%s does not accept field %q", a.Type, field)
		}
	}
	return nil
}

func validUUID(s string) bool {
	if s == "" {
		return false
	}
	_, err := util.ParseUUID(s)
	return err == nil
}
