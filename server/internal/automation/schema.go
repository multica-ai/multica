package automation

import "github.com/multica-ai/multica/server/internal/domainevent"

// FieldKind classifies a matchable event field so the validator can range-check
// clause values (e.g. a uuid field rejects a non-uuid literal).
type FieldKind int

const (
	FieldString FieldKind = iota
	FieldUUID
)

// EventSchema declares, for one domain event type, which envelope/payload field
// paths a hook may match on. The design (§5.3/§6) requires that "字段路径必须由
// 对应 event schema 声明" — this registry is that declaration, kept in lockstep
// with the domainevent payload structs. Only user-triggerable events are listed;
// system-derived events (issue.stage_completed) are not user-authorable in v1.
type EventSchema struct {
	Type        string
	MatchFields map[string]FieldKind
}

// envelopeMatchFields are common to every event type (the domain_event envelope
// columns a hook may match on). subject_id is the primary join key; actor_* let
// a rule scope to who caused the event.
var envelopeMatchFields = map[string]FieldKind{
	"subject_id": FieldUUID,
	"actor_type": FieldString,
	"actor_id":   FieldUUID,
}

// eventSchemas is the authoritative match-field registry, one entry per
// user-authorable v1 event type. Payload fields mirror the domainevent payload
// structs' json tags exactly.
var eventSchemas = buildEventSchemas(map[string]map[string]FieldKind{
	domainevent.TypeIssueCreated: {
		"status":          FieldString,
		"priority":        FieldString,
		"parent_issue_id": FieldUUID,
		"assignee_type":   FieldString,
		"assignee_id":     FieldUUID,
		"origin_type":     FieldString,
	},
	domainevent.TypeIssueStatusChanged: {
		"from": FieldString,
		"to":   FieldString,
	},
	domainevent.TypeIssueAssigned: {
		"from_assignee_type": FieldString,
		"from_assignee_id":   FieldUUID,
		"to_assignee_type":   FieldString,
		"to_assignee_id":     FieldUUID,
	},
	domainevent.TypeCommentCreated: {
		"issue_id":    FieldUUID,
		"author_type": FieldString,
		"author_id":   FieldUUID,
		"parent_id":   FieldUUID,
	},
	domainevent.TypeTaskCompleted: {
		"issue_id": FieldUUID,
		"agent_id": FieldUUID,
	},
	domainevent.TypeTaskFailed: {
		"issue_id":       FieldUUID,
		"agent_id":       FieldUUID,
		"retry_eligible": FieldString,
		"error_code":     FieldString,
	},
})

func buildEventSchemas(payloadFields map[string]map[string]FieldKind) map[string]EventSchema {
	out := make(map[string]EventSchema, len(payloadFields))
	for evtType, fields := range payloadFields {
		merged := make(map[string]FieldKind, len(fields)+len(envelopeMatchFields))
		for k, v := range envelopeMatchFields {
			merged[k] = v
		}
		for k, v := range fields {
			merged[k] = v
		}
		out[evtType] = EventSchema{Type: evtType, MatchFields: merged}
	}
	return out
}

// SchemaFor returns the match-field schema for a user-authorable event type.
func SchemaFor(eventType string) (EventSchema, bool) {
	s, ok := eventSchemas[eventType]
	return s, ok
}

// PayloadFields returns the declared payload field names for an event type — its
// schema's match fields minus the common envelope fields. An unknown event type
// returns an empty set. Free-text / undeclared payload keys (e.g. an issue title)
// are intentionally absent, so a projection over this set is fail-closed.
func PayloadFields(eventType string) map[string]bool {
	schema, ok := eventSchemas[eventType]
	if !ok {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(schema.MatchFields))
	for f := range schema.MatchFields {
		if _, isEnvelope := envelopeMatchFields[f]; !isEnvelope {
			out[f] = true
		}
	}
	return out
}

// ProjectPayload returns a copy of payload keeping ONLY the event type's declared
// payload fields (fail-closed redaction, §10): undeclared, sensitive or free-text
// keys are dropped, and an unknown event type yields an empty object. Used to
// project a domain event's payload for the read-only correlation debug surface.
func ProjectPayload(eventType string, payload map[string]any) map[string]any {
	allowed := PayloadFields(eventType)
	out := make(map[string]any, len(allowed))
	for k, v := range payload {
		if allowed[k] {
			out[k] = v
		}
	}
	return out
}

// Action types. User actions are creatable through the public API; system-only
// actions are reserved for managed system hooks (PR5) and are rejected on a
// user-authored spec.
const (
	ActionSetIssueStatus = "set_issue_status"
	ActionTriggerAgent   = "trigger_agent"
	ActionAddComment     = "add_comment"
	ActionSendInbox      = "send_inbox"
	ActionRunAutopilot   = "run_autopilot"

	ActionSetIssueStatusMany   = "set_issue_status_many"  // system-only
	ActionTriggerIssueAssignee = "trigger_issue_assignee" // system-only
)

// userActionTypes is the set of action types a user-authored hook may use.
var userActionTypes = map[string]bool{
	ActionSetIssueStatus: true,
	ActionTriggerAgent:   true,
	ActionAddComment:     true,
	ActionSendInbox:      true,
	ActionRunAutopilot:   true,
}

// systemActionTypes is the set of action types reserved for managed system
// hooks; a user spec that names one is rejected (§8 — user hooks do not inherit
// the system routing bypass).
var systemActionTypes = map[string]bool{
	ActionSetIssueStatusMany:   true,
	ActionTriggerIssueAssignee: true,
}

// issue field names valid in an issue_field condition (§5.4).
const (
	IssueFieldStatus        = "status"
	IssueFieldAssigneeID    = "assignee_id"
	IssueFieldParentIssueID = "parent_issue_id"
)

var validIssueFields = map[string]bool{
	IssueFieldStatus:        true,
	IssueFieldAssigneeID:    true,
	IssueFieldParentIssueID: true,
}

// validIssueStatuses mirrors the issue.status DB CHECK (migrations/001) and the
// handler's validIssueStatuses. A status-valued condition or action is rejected
// unless its value is one of these, so a hook can never persist an unreachable
// status (MUL-4332 PR2 review point 3).
var validIssueStatuses = map[string]bool{
	"backlog":     true,
	"todo":        true,
	"in_progress": true,
	"in_review":   true,
	"done":        true,
	"blocked":     true,
	"cancelled":   true,
}

// isValidIssueStatus reports whether s is a persistable issue status.
func isValidIssueStatus(s string) bool { return validIssueStatuses[s] }

// conditionDependencyEvent maps a condition to the single v1 domain event that
// can change its truth value. A rising_edge hook must listen to exactly that
// event so its latch can be re-evaluated (§5.2). In the v1 fixed vocabulary
// each condition depends on exactly one event type.
func conditionDependencyEvent(c ConditionSpec) (string, bool) {
	switch {
	case c.IssuesStatus != nil:
		return domainevent.TypeIssueStatusChanged, true
	case c.IssueField != nil:
		switch c.IssueField.Field {
		case IssueFieldStatus:
			return domainevent.TypeIssueStatusChanged, true
		case IssueFieldAssigneeID:
			return domainevent.TypeIssueAssigned, true
		case IssueFieldParentIssueID:
			return domainevent.TypeIssueCreated, true
		}
	}
	return "", false
}
