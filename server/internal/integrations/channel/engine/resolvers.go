package engine

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file defines the pluggable seams the Router runs the inbound pipeline
// through. Everything platform-specific lives behind these interfaces; a
// platform registers a ResolverSet and the channel-agnostic Router stays
// unchanged. The Feishu implementation is the first ResolverSet.

// Outcome categorizes what the Router decided to do with an inbound message.
// Values match the legacy lark outcomes 1:1 so behavior and dashboards carry
// over unchanged.
type Outcome string

const (
	OutcomeDropped       Outcome = "dropped"
	OutcomeNeedsBinding  Outcome = "needs_binding"
	OutcomeIngested      Outcome = "ingested"
	OutcomeAgentOffline  Outcome = "agent_offline"
	OutcomeAgentArchived Outcome = "agent_archived"
)

// DropReason enumerates the drop-audit categories. Values match the legacy
// lark drop reasons 1:1.
type DropReason string

const (
	DropReasonUnboundUser         DropReason = "unbound_user"
	DropReasonNonWorkspaceMember  DropReason = "non_workspace_member"
	DropReasonNotAddressedInGroup DropReason = "not_addressed_in_group"
	DropReasonDuplicate           DropReason = "duplicate"
	DropReasonRevokedInstallation DropReason = "revoked_installation"
	DropReasonInvalidEvent        DropReason = "invalid_event"
)

// Result is the typed verdict the Router produces for one inbound message,
// consumed by the outbound side (OutboundReplier / typing). It mirrors the
// legacy lark.DispatchResult.
type Result struct {
	Outcome        Outcome
	DropReason     DropReason
	InstallationID pgtype.UUID
	ChatSessionID  pgtype.UUID
	// Sender is the platform-native sender id (e.g. Lark open_id), so the
	// replier can target a binding prompt back to the sender.
	Sender          string
	IssueID         pgtype.UUID
	IssueNumber     int32
	IssueIdentifier string
	IssueTitle      string
}

// ResolvedInstallation is the channel-agnostic installation context the Router
// needs after routing. Platform carries the adapter's own installation value
// opaquely so the set's other ports (binder, replier, typing) reuse it without
// a re-fetch; the Router never reads Platform.
type ResolvedInstallation struct {
	ID              pgtype.UUID
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	InstallerUserID pgtype.UUID
	Active          bool
	Platform        any
}

// ResolvedIdentity is the sender mapped to a Multica user.
type ResolvedIdentity struct {
	UserID pgtype.UUID
}

// EnsureSessionParams carries the inputs for SessionBinder.EnsureSession.
// Sender is the resolved session creator (the sole human for p2p, the
// installer for group chats — the Router decides which and passes it here).
type EnsureSessionParams struct {
	Installation ResolvedInstallation
	Sender       pgtype.UUID
	Message      channel.InboundMessage
}

// AppendParams carries the inputs for SessionBinder.AppendMessage. ClaimToken
// is the dedup owner-fence token; the binder runs the dedup Mark INSIDE its
// chat_message+session tx so the durable write and the Mark commit atomically.
// MediaPendingUntil persists the placeholder fallback deadline.
type AppendParams struct {
	SessionID         pgtype.UUID
	Sender            pgtype.UUID
	InstallationID    pgtype.UUID
	Message           channel.InboundMessage
	ClaimToken        pgtype.UUID
	MediaPendingUntil pgtype.Timestamptz
}

// AppendResult reports what AppendMessage decided.
type AppendResult struct {
	// MessageID is the durable chat_message row created by AppendMessage.
	// Detached media processing uses it to link attachments after the
	// connector ACK path has completed.
	MessageID pgtype.UUID
	// IssueCommand is non-nil when the message was an /issue command.
	IssueCommand *IssueCommand
	// DedupMarked is true when AppendMessage finalized the dedup claim in its
	// own tx; the Router then skips the post-pipeline finalize.
	DedupMarked bool
}

// BindMediaParams carries stored media references to the post-append
// attachment transaction. MessageID is the durable chat_message created by
// AppendMessage; media downloads must never run inside this transaction.
type BindMediaParams struct {
	MessageID   pgtype.UUID
	SessionID   pgtype.UUID
	WorkspaceID pgtype.UUID
	Sender      pgtype.UUID
	MediaRefs   []channel.MediaRef
}

// IssueCommand is the parsed /issue command.
type IssueCommand struct {
	Title       string
	Description string
}

// Sentinel errors the resolvers return so the Router can map them to the right
// product outcome instead of an infrastructure failure.
var (
	// ErrInstallationNotFound: no installation matches the message's routing
	// key → invalid_event drop.
	ErrInstallationNotFound = errors.New("engine: installation not found")
	// ErrSenderUnbound: the sender has no identity binding → needs_binding.
	ErrSenderUnbound = errors.New("engine: sender unbound")
	// ErrSenderNotMember: the sender is bound but not a workspace member →
	// non_workspace_member drop.
	ErrSenderNotMember = errors.New("engine: sender not a workspace member")
	// ErrDuplicate: Claim found the message already processed / in flight →
	// duplicate drop.
	ErrDuplicate = errors.New("engine: duplicate message")
	// ErrClaimLost: a concurrent reclaim rotated the dedup token mid-flight →
	// treated as a duplicate.
	ErrClaimLost = errors.New("engine: dedup claim lost")
)

// InstallationResolver routes an inbound message to its installation. The
// adapter reads whatever platform routing key it needs from the message
// (Source or Raw). Return ErrInstallationNotFound when none matches; return a
// ResolvedInstallation with Active=false when it exists but is revoked.
type InstallationResolver interface {
	ResolveInstallation(ctx context.Context, msg channel.InboundMessage) (ResolvedInstallation, error)
}

// IdentityResolver maps the message sender to a Multica user within the
// installation, re-checking workspace membership. Return ErrSenderUnbound or
// ErrSenderNotMember for the product cases.
type IdentityResolver interface {
	ResolveSender(ctx context.Context, inst ResolvedInstallation, msg channel.InboundMessage) (ResolvedIdentity, error)
}

// Deduper is the two-phase idempotency seam. Claim mints an owner-fence token
// (ErrDuplicate when already processed / in flight); Mark/Release are fenced on
// the token (a no-op on token mismatch is not an error).
type Deduper interface {
	Claim(ctx context.Context, installationID pgtype.UUID, messageID string) (claimToken pgtype.UUID, err error)
	Mark(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error
	Release(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error
}

// SessionBinder ensures the chat_session and appends the message (with the
// in-tx dedup Mark). AppendMessage returns ErrClaimLost when the token was
// rotated mid-flight.
type SessionBinder interface {
	EnsureSession(ctx context.Context, p EnsureSessionParams) (pgtype.UUID, error)
	AppendMessage(ctx context.Context, p AppendParams) (AppendResult, error)
	BindMedia(ctx context.Context, p BindMediaParams) error
}

// MediaResolver resolves platform media after the user message and dedup mark
// are durable. The Router runs it off the connector ACK path and binds any
// returned MediaRefs; the independently scheduled task remains deferred until
// binding finishes or the persisted deadline expires. Implementations are
// best-effort: failures leave the stored placeholder text intact and NEVER
// delete anything inline — every uploaded object is covered by an intent-
// ledger row written before the PUT (see MediaIntentLedger), and the
// asynchronous reconciler settles whatever binding did not claim.
type MediaResolver interface {
	// HasMedia reports whether msg references platform media that
	// ResolveMedia would fetch. The Router calls it synchronously on the
	// connector ACK path to decide whether to persist a media deadline and
	// queue a resolution job at all, so implementations must be pure
	// in-memory checks (no I/O). A false result keeps the message on the
	// plain ingest path: no marker, no deferred run, no semaphore slot.
	HasMedia(msg channel.InboundMessage) bool
	// ResolveMedia downloads the platform media and uploads it to object
	// storage. chatMessageID is the durable chat_message the refs will bind
	// to; the intent ledger keys the reconciler's reference check on it.
	ResolveMedia(ctx context.Context, inst ResolvedInstallation, sender ResolvedIdentity, sessionID, chatMessageID pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage
}

// MediaIntentLedger persists upload intent BEFORE the object is written. The
// row is the only artifact any failure path leaves behind: upload error,
// resolve deadline, bind failure, ambiguous commit, or a crash all simply
// leave it for the reconciler, which settles it long after any in-flight PUT
// or COMMIT can still land. This is what makes "did my side effect happen?"
// a question nobody has to answer inline.
type MediaIntentLedger interface {
	// RecordPendingMediaObject upserts the intent row. ok=false means the
	// key has left 'pending' (the reconciler owns it) — the caller must skip
	// the upload entirely rather than resurrect the row.
	RecordPendingMediaObject(ctx context.Context, p RecordPendingMediaObjectParams) (ok bool, err error)
}

// RecordPendingMediaObjectParams identifies one intended object. StorageURL
// is the URL the attachment row will carry (pure function of the key), so
// the reconciler can check for a durable reference. InstallationID is an
// ops-diagnostic only.
type RecordPendingMediaObjectParams struct {
	StorageKey     string
	WorkspaceID    pgtype.UUID
	ChatMessageID  pgtype.UUID
	StorageURL     string
	InstallationID pgtype.UUID
}

// NewDBMediaIntentLedger adapts *db.Queries to MediaIntentLedger.
func NewDBMediaIntentLedger(q *db.Queries) MediaIntentLedger {
	return dbMediaIntentLedger{q: q}
}

type dbMediaIntentLedger struct{ q *db.Queries }

func (l dbMediaIntentLedger) RecordPendingMediaObject(ctx context.Context, p RecordPendingMediaObjectParams) (bool, error) {
	_, err := l.q.RecordChannelMediaPendingObject(ctx, db.RecordChannelMediaPendingObjectParams{
		StorageKey:     p.StorageKey,
		WorkspaceID:    p.WorkspaceID,
		ChatMessageID:  p.ChatMessageID,
		StorageUrl:     p.StorageURL,
		InstallationID: p.InstallationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The state-guarded upsert matched a 'deleting' row: the reconciler
		// owns this key and it must not be resurrected.
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Auditor records a dropped inbound event (no message body — drop-audit
// policy). instID may be the zero UUID for installation-less events.
type Auditor interface {
	RecordDrop(ctx context.Context, instID pgtype.UUID, msg channel.InboundMessage, reason DropReason) error
}

// OutboundReplier delivers the verdict-driven reply (binding prompt, offline /
// archived notice, /issue confirmation). Optional; nil disables outbound
// replies. Driven off the ACK critical path by the Router.
type OutboundReplier interface {
	Reply(ctx context.Context, inst ResolvedInstallation, msg channel.InboundMessage, res Result)
}

// TypingNotifier shows a "processing" indicator when a message is ingested and
// clears it once the message reaches a terminal outcome. Optional; nil disables
// it.
type TypingNotifier interface {
	// OnIngested shows the indicator for a successfully ingested message.
	OnIngested(ctx context.Context, inst ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID)
	// OnSettled clears the indicator for a session whose run trigger produced no
	// task (agent offline / archived, or an enqueue failure). In that case no
	// task lifecycle event is ever published, so the platform's own bus-driven
	// clear (on chat-done / task-failed) would never fire and the indicator would
	// stick. The Router calls this from the debounced flush. Idempotent: a
	// session with no indicator is a no-op.
	OnSettled(ctx context.Context, sessionID pgtype.UUID)
}

// ResolverSet is the per-platform bundle the Router runs the pipeline through.
// Installation/Identity/Dedup/Session/Audit are required; Replier/Typing are
// optional. OriginType is the issue.origin_type label written for /issue
// commands from this channel (Feishu: "lark_chat").
type ResolverSet struct {
	Installation InstallationResolver
	Identity     IdentityResolver
	Dedup        Deduper
	Session      SessionBinder
	Media        MediaResolver
	Audit        Auditor
	Replier      OutboundReplier
	Typing       TypingNotifier
	OriginType   string
}

// IssueCreator is the narrow subset of service.IssueService the Router needs
// for the /issue command. Shared across platforms.
type IssueCreator interface {
	Create(ctx context.Context, p service.IssueCreateParams, opts service.IssueCreateOpts) (service.IssueCreateResult, error)
}

// TaskEnqueuer is the narrow subset of service.TaskService the Router needs to
// trigger a chat run. Shared across platforms.
type TaskEnqueuer interface {
	EnqueueChatTask(ctx context.Context, session db.ChatSession, initiatorUserID pgtype.UUID, forceFreshSession bool) (db.AgentTaskQueue, error)
	PromoteChannelChatTasksIfMediaReady(ctx context.Context, sessionID pgtype.UUID) error
}

// SessionReader reads the rows the debounced flush + /issue identifier need.
// Shared across platforms; backed by *db.Queries (the channel-backed store).
type SessionReader interface {
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
}
