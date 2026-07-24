package engine

import (
	"context"
	"errors"

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
	// DropReasonMediaFetchFailed: the message carried media the pipeline
	// tried but could not fetch/stage (download failed, over limits, or a
	// disallowed type). Nothing was appended; the claim is released so a
	// provider redelivery may retry — the condition is potentially transient.
	DropReasonMediaFetchFailed DropReason = "media_fetch_failed"
	// DropReasonMediaUnsupported: the message carried media but the channel
	// has no inbound-media seam at all (object storage unconfigured). Unlike
	// media_fetch_failed this is a PERMANENT capability gap, so the replier
	// tells the user images aren't supported here rather than asking them to
	// resend (which no resend could satisfy).
	DropReasonMediaUnsupported DropReason = "media_unsupported"
	// DropReasonUnsupportedKind: a bound member sent a message kind the
	// bot cannot read (audio/video/file/unknown). Refused with a
	// capability notice instead of silently appending an empty turn.
	DropReasonUnsupportedKind DropReason = "unsupported_message_kind"
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
	// IssueQueued reports the /issue command was accepted onto the
	// quick-create path: a background task authors the issue and the result
	// is posted back into this conversation. Only set for channels carrying
	// the QuickCreate seam; direct-create channels populate IssueID/… instead.
	IssueQueued bool
	// IssueUsage reports a bare /issue with no usable prompt even after the
	// binder's previous-message fallback: nothing was enqueued.
	IssueUsage bool
	// IssueQueueFailed reports the quick-create enqueue failed after the user
	// turn was already durable; the replier posts an internal-error notice and
	// the user retries by re-sending the command.
	IssueQueueFailed bool
	// RunScheduled reports whether this ingest scheduled an agent run. A run
	// eventually clears the typing indicator, so the Router only shows it when
	// this is true; a bare fresh-session reset (/new) schedules none.
	RunScheduled bool
	// FreshReset reports that this ingest was a bare fresh-session reset (a lone
	// "/new"): the session was rotated but no prompt was recorded and no run
	// scheduled. The replier posts a short confirmation for it so the reset is
	// not silent (which reads as "the bot is broken").
	FreshReset bool
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
type AppendParams struct {
	SessionID      pgtype.UUID
	Sender         pgtype.UUID
	InstallationID pgtype.UUID
	Message        channel.InboundMessage
	ClaimToken     pgtype.UUID
	// WorkspaceID scopes the attachment rows created from Staged media.
	// The Router fills it from the resolved installation.
	WorkspaceID pgtype.UUID
	// Staged is the message's media, already fetched and persisted to
	// object storage by the MediaIngester seam. The binder creates the
	// attachment rows for it inside its append transaction.
	Staged []StagedMedia
	// MediaChatBind is true when the attachments belong to this chat turn
	// (bind chat_session_id + chat_message_id). False for /issue turns:
	// their attachments ride the quick-create task onto the issue instead,
	// keeping the rows clear of the chat cascade.
	MediaChatBind bool
}

// AppendResult reports what AppendMessage decided.
type AppendResult struct {
	// IssueCommand is non-nil when the message was an /issue command.
	IssueCommand *IssueCommand
	// DedupMarked is true when AppendMessage finalized the dedup claim in its
	// own tx; the Router then skips the post-pipeline finalize.
	DedupMarked bool
	// AttachmentIDs are the attachment rows created for Staged media in
	// this append, in Staged order. /issue turns thread them into the
	// quick-create task.
	AttachmentIDs []pgtype.UUID
}

// IssueCommand is the parsed /issue command. Title/Description come from the
// image-stripped command text; the direct-create path files them straight,
// while the quick-create path ignores them and builds its prompt from the
// turn's own composed body (see quickCreatePrompt).
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
	Audit        Auditor
	Replier      OutboundReplier
	Typing       TypingNotifier
	OriginType   string
	// QuickCreate switches /issue from the synchronous direct-create path to
	// the quick-create path (background agent authors the issue; the result is
	// posted back into the conversation). Optional: nil keeps direct-create.
	//
	// CONTRACT: a channel that sets QuickCreate must keep its binder's
	// AppendInput.CommandText identical to InboundMessage.Text. The Router
	// pre-parses msg.Text to decide attachment chat-binding while the binder
	// parses CommandText to decide whether the quick-create branch runs; the
	// two decisions must be made over the same text (DingTalk satisfies this;
	// Lark's CommandText differs from Text, so it must not enable QuickCreate
	// without reconciling the two).
	QuickCreate QuickCreator
	// Media fetches inbound media referenced by PendingMedia. Optional:
	// nil means the channel has no inbound media support and messages
	// carrying PendingMedia are refused as media_fetch_failed.
	Media MediaIngester
	// RefuseUnsupportedKinds makes the Router refuse audio/video/file/unknown
	// turns with a capability notice (DropReasonUnsupportedKind) after the
	// identity gate, instead of appending them as an empty turn. Opt-in per
	// channel: a channel enables it only when its Replier renders the refusal
	// (DingTalk). Channels that leave it false keep their prior behavior —
	// those kinds flow through unchanged — so enabling the seam on one channel
	// never silently drops another's messages.
	RefuseUnsupportedKinds bool
	// Attachments removes attachment rows the Router staged for an /issue turn
	// that ultimately produced no issue (empty prompt or enqueue failure), so
	// the rows do not dangle bound to neither a chat nor an issue. Optional:
	// nil skips row cleanup (the staged storage objects are still discarded via
	// Media.Discard).
	Attachments AttachmentDiscarder
}

// AttachmentDiscarder deletes attachment rows by id within a workspace. It is
// the cleanup counterpart to MediaIngester.Discard (which removes the staged
// storage objects): together they undo an /issue turn's media when the issue is
// never created. *db.Queries-backed adapters satisfy it. Best-effort — failures
// are logged, never surfaced.
type AttachmentDiscarder interface {
	DiscardAttachments(ctx context.Context, workspaceID pgtype.UUID, ids []pgtype.UUID)
}

// IssueCreator is the narrow subset of service.IssueService the Router needs
// for the /issue command. Shared across platforms.
type IssueCreator interface {
	Create(ctx context.Context, p service.IssueCreateParams, opts service.IssueCreateOpts) (service.IssueCreateResult, error)
}

// QuickCreator enqueues a chat-originated quick-create task.
// *service.TaskService satisfies it. attachmentIDs are the inbound-media
// attachment rows the created issue should carry (nil for text-only turns).
type QuickCreator interface {
	EnqueueQuickCreateChatTask(ctx context.Context, workspaceID, requesterID, agentID pgtype.UUID, prompt string, chatSessionID pgtype.UUID, attachmentIDs []pgtype.UUID) (db.AgentTaskQueue, error)
}

// MessageAppender writes the transcript rows the Router itself authors (the
// quick-create acknowledgement / failure notes). *db.Queries satisfies it.
type MessageAppender interface {
	CreateChatMessage(ctx context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error)
}

// TaskEnqueuer is the narrow subset of service.TaskService the Router needs to
// trigger a chat run. Shared across platforms.
type TaskEnqueuer interface {
	EnqueueChatTask(ctx context.Context, session db.ChatSession, initiatorUserID pgtype.UUID, forceFreshSession bool) (db.AgentTaskQueue, error)
}

// SessionReader reads the rows the debounced flush + /issue identifier need.
// Shared across platforms; backed by *db.Queries (the channel-backed store).
type SessionReader interface {
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
}
