// Package domainevent is the transactional-outbox event layer for the Event
// Hooks MVP (MUL-4332). It defines the versioned v1 domain event catalog and a
// tx-aware writer that persists one immutable row into the `domain_event` table
// IN THE SAME TRANSACTION as the domain fact that produced it.
//
// The contract is deliberately narrow:
//
//   - A caller that already writes a domain fact inside a pgx.Tx obtains the
//     tx-bound *db.Queries (via Queries.WithTx) and calls Write(ctx, qtx, evt)
//     before committing. Fact and event commit atomically — a crash between
//     them is impossible, which is what makes the outbox durable.
//   - A caller whose write is a bare autocommit statement uses WriteInTx, which
//     wraps the write + event in one transaction.
//
// PR1 has NO consumer: rows land dispatch_status='pending' and nothing reads
// them, so wiring Write into a domain path is a zero-behavior-change addition.
// The matcher/executor that claims pending rows arrives in PR3.
//
// This package is intentionally separate from internal/events (the in-memory
// events.Bus). The Bus stays best-effort, post-commit, and serves realtime UI;
// domain_event is the durable, transactional source of truth for automation.
package domainevent

import "github.com/jackc/pgx/v5/pgtype"

// Event type names (the `type` column). Dotted noun.verb, distinct from the
// colon-delimited events.Bus protocol names so the two namespaces never blur.
const (
	TypeIssueCreated       = "issue.created"
	TypeIssueStatusChanged = "issue.status_changed"
	TypeIssueAssigned      = "issue.assigned"
	TypeCommentCreated     = "comment.created"
	TypeTaskCompleted      = "task.completed"
	TypeTaskFailed         = "task.failed"

	// TypeIssueStageCompleted is a derived sensor event emitted by the PR5
	// stage frontier sensor, not by any v1 domain write. The constant is
	// declared here so the catalog is complete and validators recognise it.
	TypeIssueStageCompleted = "issue.stage_completed"
)

// Subject types (the `subject_type` column): what entity the event is about.
const (
	SubjectIssue   = "issue"
	SubjectComment = "comment"
	SubjectTask    = "task"
)

// Actor types (the `actor_type` column): who caused the event.
const (
	ActorMember = "member"
	ActorAgent  = "agent"
	ActorSystem = "system"
	ActorHook   = "hook"
)

// Dispatch statuses (the `dispatch_status` column). Only DispatchPending is
// produced in PR1; the rest are advanced by the PR3 matcher/executor.
const (
	DispatchPending     = "pending"
	DispatchDispatching = "dispatching"
	DispatchDispatched  = "dispatched"
	DispatchFailed      = "failed"
)

// typeSpec pins the invariants of one event type so Write can reject a
// malformed envelope before it reaches the DB.
type typeSpec struct {
	subject string
	version int32
}

// catalog is the authoritative v1 registry. schema_version is 1 for every type
// in v1; a breaking payload change bumps the version here and the payload's
// schemaVersion() together.
var catalog = map[string]typeSpec{
	TypeIssueCreated:        {subject: SubjectIssue, version: 1},
	TypeIssueStatusChanged:  {subject: SubjectIssue, version: 1},
	TypeIssueAssigned:       {subject: SubjectIssue, version: 1},
	TypeCommentCreated:      {subject: SubjectComment, version: 1},
	TypeTaskCompleted:       {subject: SubjectTask, version: 1},
	TypeTaskFailed:          {subject: SubjectTask, version: 1},
	TypeIssueStageCompleted: {subject: SubjectIssue, version: 1},
}

var validActorTypes = map[string]bool{
	ActorMember: true,
	ActorAgent:  true,
	ActorSystem: true,
	ActorHook:   true,
}

// Actor identifies who caused an event. The ID is invalid (NULL) for a system
// actor, which has no member/agent identity.
type Actor struct {
	Type string
	ID   pgtype.UUID
}

// MemberActor / AgentActor / HookActor / SystemActor build an Actor from an
// identity the call site already holds.
func MemberActor(id pgtype.UUID) Actor { return Actor{Type: ActorMember, ID: id} }
func AgentActor(id pgtype.UUID) Actor  { return Actor{Type: ActorAgent, ID: id} }
func HookActor(id pgtype.UUID) Actor   { return Actor{Type: ActorHook, ID: id} }
func SystemActor() Actor               { return Actor{Type: ActorSystem} }

// ActorFrom builds an Actor from a raw (type, id) pair — for call sites that
// carry an existing creator_type/creator_id or author_type/author_id. An empty
// or unknown type degrades to a system actor so a mislabelled caller can never
// fabricate a member/agent identity.
func ActorFrom(actorType string, id pgtype.UUID) Actor {
	if !validActorTypes[actorType] {
		return SystemActor()
	}
	if actorType == ActorSystem {
		return SystemActor()
	}
	return Actor{Type: actorType, ID: id}
}
