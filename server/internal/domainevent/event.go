package domainevent

import (
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// Event is a fully-formed, validated-on-write domain event ready to persist.
// Construct it through a typed constructor (IssueStatusChanged, CommentCreated,
// …) rather than by hand so Type / SubjectType / SchemaVersion / Payload always
// agree with the catalog.
//
// A zero CorrelationID marks a root event: Write assigns correlation_id = id and
// hop_count = 0. The Causation* / HopCount fields stay zero for every v1 domain
// write (only the PR3 executor, replaying a reaction, sets them).
type Event struct {
	WorkspaceID   pgtype.UUID
	Type          string
	SchemaVersion int32
	SubjectType   string
	SubjectID     pgtype.UUID
	ActorType     string
	ActorID       pgtype.UUID
	Payload       []byte

	CorrelationID        pgtype.UUID
	CausationExecutionID pgtype.UUID
	CausationActionIndex pgtype.Int4
	HopCount             int32

	// buildErr carries a payload-marshal failure from the constructor so call
	// sites stay error-free; Write surfaces it and aborts the transaction.
	buildErr error
}

// payload is implemented by every typed payload struct so a single generic
// builder can stamp the envelope fields from the payload itself.
type payload interface {
	eventType() string
	subjectType() string
	schemaVersion() int32
}

func newEvent(workspaceID, subjectID pgtype.UUID, actor Actor, p payload) Event {
	raw, err := json.Marshal(p)
	return Event{
		WorkspaceID:   workspaceID,
		Type:          p.eventType(),
		SchemaVersion: p.schemaVersion(),
		SubjectType:   p.subjectType(),
		SubjectID:     subjectID,
		ActorType:     actor.Type,
		ActorID:       actor.ID,
		Payload:       raw,
		buildErr:      err,
	}
}

// validate rejects an envelope that disagrees with the catalog before it hits
// the DB. It is a safety net beneath the typed constructors, and the guard the
// future public REST/CLI create path will reuse.
func (e Event) validate() error {
	if e.buildErr != nil {
		return fmt.Errorf("domainevent: marshal payload: %w", e.buildErr)
	}
	spec, ok := catalog[e.Type]
	if !ok {
		return fmt.Errorf("domainevent: unknown event type %q", e.Type)
	}
	if e.SchemaVersion != spec.version {
		return fmt.Errorf("domainevent: %s schema_version %d, want %d", e.Type, e.SchemaVersion, spec.version)
	}
	if e.SubjectType != spec.subject {
		return fmt.Errorf("domainevent: %s subject_type %q, want %q", e.Type, e.SubjectType, spec.subject)
	}
	if !validActorTypes[e.ActorType] {
		return fmt.Errorf("domainevent: invalid actor_type %q", e.ActorType)
	}
	// Fail-closed actor identity (MUL-4332 review point 6): a system actor must
	// carry NO id, and every other actor type must carry a valid one — so a
	// dropped / unparsable id can never be silently recorded as a null or
	// system actor. The caller's transaction aborts instead.
	if e.ActorType == ActorSystem {
		if e.ActorID.Valid {
			return fmt.Errorf("domainevent: %s: system actor must not carry an actor_id", e.Type)
		}
	} else if !e.ActorID.Valid {
		return fmt.Errorf("domainevent: %s: %s actor requires a valid actor_id", e.Type, e.ActorType)
	}
	if !e.WorkspaceID.Valid {
		return fmt.Errorf("domainevent: %s missing workspace_id", e.Type)
	}
	if !e.SubjectID.Valid {
		return fmt.Errorf("domainevent: %s missing subject_id", e.Type)
	}
	if len(e.Payload) == 0 {
		return fmt.Errorf("domainevent: %s empty payload", e.Type)
	}
	return nil
}

// ---- v1 payloads ----------------------------------------------------------
//
// UUID-valued fields are JSON strings (empty + omitempty when absent). Call
// sites convert a pgtype.UUID with util.UUIDToString, which yields "" for an
// invalid/NULL value — so an absent parent/assignee is omitted, not null.

// IssueCreatedPayload — subject is the new issue.
type IssueCreatedPayload struct {
	Status        string `json:"status"`
	Title         string `json:"title"`
	Priority      string `json:"priority,omitempty"`
	ParentIssueID string `json:"parent_issue_id,omitempty"`
	AssigneeType  string `json:"assignee_type,omitempty"`
	AssigneeID    string `json:"assignee_id,omitempty"`
	OriginType    string `json:"origin_type,omitempty"`
}

func (IssueCreatedPayload) eventType() string    { return TypeIssueCreated }
func (IssueCreatedPayload) subjectType() string  { return SubjectIssue }
func (IssueCreatedPayload) schemaVersion() int32 { return 1 }

// IssueStatusChangedPayload — subject is the issue whose status moved.
type IssueStatusChangedPayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (IssueStatusChangedPayload) eventType() string    { return TypeIssueStatusChanged }
func (IssueStatusChangedPayload) subjectType() string  { return SubjectIssue }
func (IssueStatusChangedPayload) schemaVersion() int32 { return 1 }

// IssueAssignedPayload — subject is the issue whose assignee changed.
type IssueAssignedPayload struct {
	FromAssigneeType string `json:"from_assignee_type,omitempty"`
	FromAssigneeID   string `json:"from_assignee_id,omitempty"`
	ToAssigneeType   string `json:"to_assignee_type,omitempty"`
	ToAssigneeID     string `json:"to_assignee_id,omitempty"`
}

func (IssueAssignedPayload) eventType() string    { return TypeIssueAssigned }
func (IssueAssignedPayload) subjectType() string  { return SubjectIssue }
func (IssueAssignedPayload) schemaVersion() int32 { return 1 }

// CommentCreatedPayload — subject is the comment; issue_id locates its thread.
type CommentCreatedPayload struct {
	IssueID    string `json:"issue_id"`
	AuthorType string `json:"author_type"`
	AuthorID   string `json:"author_id,omitempty"`
	ParentID   string `json:"parent_id,omitempty"`
}

func (CommentCreatedPayload) eventType() string    { return TypeCommentCreated }
func (CommentCreatedPayload) subjectType() string  { return SubjectComment }
func (CommentCreatedPayload) schemaVersion() int32 { return 1 }

// TaskCompletedPayload — subject is the task; issue_id/agent_id locate it.
type TaskCompletedPayload struct {
	IssueID string `json:"issue_id,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
}

func (TaskCompletedPayload) eventType() string    { return TypeTaskCompleted }
func (TaskCompletedPayload) subjectType() string  { return SubjectTask }
func (TaskCompletedPayload) schemaVersion() int32 { return 1 }

// TaskFailedPayload — subject is the task. Retryable reports whether this failure
// is eligible for an automatic retry (an infra-shaped reason still within the
// attempt budget). The single FailTask path creates the retry child in the SAME
// transaction, so there it is exact; the bulk sweeper paths create it immediately
// after commit, so there it means "a retry is expected" — both derive it from the
// shared retryEligible predicate so the event never contradicts the actual retry
// decision. A consumer should read retryable=true as "not yet terminal, a fresh
// attempt is coming" rather than reacting to the failure.
type TaskFailedPayload struct {
	IssueID   string `json:"issue_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	Retryable bool   `json:"retryable"`
	ErrorCode string `json:"error_code,omitempty"`
}

func (TaskFailedPayload) eventType() string    { return TypeTaskFailed }
func (TaskFailedPayload) subjectType() string  { return SubjectTask }
func (TaskFailedPayload) schemaVersion() int32 { return 1 }

// ---- typed constructors ---------------------------------------------------

// IssueCreated builds an issue.created event for the given new issue.
func IssueCreated(workspaceID, issueID pgtype.UUID, actor Actor, p IssueCreatedPayload) Event {
	return newEvent(workspaceID, issueID, actor, p)
}

// IssueStatusChanged builds an issue.status_changed event.
func IssueStatusChanged(workspaceID, issueID pgtype.UUID, actor Actor, p IssueStatusChangedPayload) Event {
	return newEvent(workspaceID, issueID, actor, p)
}

// IssueAssigned builds an issue.assigned event.
func IssueAssigned(workspaceID, issueID pgtype.UUID, actor Actor, p IssueAssignedPayload) Event {
	return newEvent(workspaceID, issueID, actor, p)
}

// CommentCreated builds a comment.created event; subjectID is the comment id.
func CommentCreated(workspaceID, commentID pgtype.UUID, actor Actor, p CommentCreatedPayload) Event {
	return newEvent(workspaceID, commentID, actor, p)
}

// TaskCompleted builds a task.completed event; subjectID is the task id.
func TaskCompleted(workspaceID, taskID pgtype.UUID, actor Actor, p TaskCompletedPayload) Event {
	return newEvent(workspaceID, taskID, actor, p)
}

// TaskFailed builds a task.failed event; subjectID is the task id.
func TaskFailed(workspaceID, taskID pgtype.UUID, actor Actor, p TaskFailedPayload) Event {
	return newEvent(workspaceID, taskID, actor, p)
}
