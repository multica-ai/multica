package lark

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// InboundMessage is the normalized shape the WebSocket adapter hands
// to the Dispatcher. The adapter (Phase 2 PR) translates the raw Lark
// event payload into this struct; the Dispatcher does NOT know what a
// Lark event JSON object looks like. This keeps event-schema changes
// from rippling into business logic.
//
// AddressedToBot is the adapter's verdict on whether a group-chat
// message is an interaction with the Bot (@-mention or reply to a
// Bot card). For p2p messages this field is ignored.
type InboundMessage struct {
	EventType      string
	EventID        string
	AppID          string
	ChatID         ChatID
	ChatType       ChatType
	MessageID      string
	SenderOpenID   OpenID
	Body           string
	AddressedToBot bool

	// MessageType is the raw Lark msg_type ("text", "post",
	// "merge_forward", "image", "interactive", …). The decoder
	// populates it so the inbound enricher can decide whether a
	// message needs an HTTP round-trip to expand (merge_forward) while
	// the dispatcher itself stays msg_type-agnostic and only reads Body.
	MessageType string

	// CreateTime is the trigger message's creation time (epoch
	// milliseconds, as Lark sends it). The enricher uses it to anchor the
	// group recent-context window to the moment of the @-mention — it
	// fetches the conversation up to this time rather than whatever is
	// newest when the (slightly later) prefetch HTTP call runs.
	CreateTime string

	// ParentID is the message_id of the message this one quote-replies
	// to, taken verbatim from the receive event's `parent_id`. Empty
	// when the message is not a reply. The enricher fetches it and
	// prepends a <quoted_message> block. RootID is the thread/root
	// anchor Lark also reports; we keep it for completeness but the
	// quoted-reply expansion keys off ParentID (the immediate parent),
	// not the root.
	ParentID string
	RootID   string

	// CommandBody is the user's OWN typed text (the decoded Body before
	// the enricher prepends any <quoted_message> / <forwarded_messages>
	// context). The `/issue` command is parsed from THIS, not from the
	// enriched Body: enrichment prepends context blocks, which would
	// otherwise push the user's `/issue …` off the first line and
	// silently stop creating the issue. The enricher leaves CommandBody
	// untouched while it rewrites Body.
	CommandBody string
}

// Outcome categorizes what the Dispatcher decided to do with an
// inbound message. The WS adapter inspects this and chooses what to
// reply with on the Lark side.
type Outcome string

const (
	// OutcomeDropped — the message was not ingested (identity failed,
	// dedup hit, group filter, etc.). DispatchResult.DropReason holds
	// the audit category.
	OutcomeDropped Outcome = "dropped"

	// OutcomeNeedsBinding — the open_id is unbound; the WS adapter
	// should mint a binding token via BindingTokenService and send
	// the "click here to bind" card. DispatchResult.SenderOpenID and
	// .InstallationID are populated so the adapter can target the
	// reply.
	OutcomeNeedsBinding Outcome = "needs_binding"

	// OutcomeIngested — the message landed in chat_session and an
	// agent task was enqueued. Empty IssueCommand means a plain chat
	// message; non-empty means /issue ran (see IssueID for the new
	// issue's UUID).
	OutcomeIngested Outcome = "ingested"

	// OutcomeAgentOffline — the message landed in chat_session, but
	// the agent has no runtime bound at all (agent.runtime_id IS
	// NULL). The adapter should reply with "agent offline, will run
	// on next online." The chat_message row remains so the agent
	// picks it up on resume.
	//
	// IMPORTANT: this is NOT triggered when a daemon is merely
	// disconnected. If agent.runtime_id IS set, the chat task is
	// enqueued and waits for the daemon to claim it on next online;
	// that path returns OutcomeIngested with a TaskID.
	OutcomeAgentOffline Outcome = "agent_offline"

	// OutcomeAgentArchived — the message landed in chat_session, but
	// the agent has been archived. The adapter should reply with a
	// distinct copy ("this agent has been archived; ask an admin to
	// unarchive or rebind"). Kept separate from OutcomeAgentOffline
	// because the user-facing remediation differs.
	OutcomeAgentArchived Outcome = "agent_archived"
)

// DispatchResult is the typed return from Dispatcher.Handle. Callers
// (the WS adapter) consume this to drive their outbound side; nothing
// here implies the adapter MUST reply, only that it CAN.
type DispatchResult struct {
	Outcome        Outcome
	DropReason     DropReason
	InstallationID pgtype.UUID
	ChatSessionID  pgtype.UUID
	SenderOpenID   OpenID
	// TaskID was populated when the dispatcher enqueued the chat task
	// synchronously. With the short-window debounce (MUL-2968) the run is
	// triggered asynchronously at flush time, so Handle no longer knows a
	// task id — this field is left zero for the chat path. Kept on the
	// struct because the emit contract still carries it for any future
	// synchronous enqueue (e.g. /issue follow-ups).
	TaskID      pgtype.UUID
	IssueID     pgtype.UUID
	IssueNumber int32
	// IssueIdentifier is the workspace-qualified human key for the
	// created issue ("MUL-42"). Populated only when /issue produced a
	// new row. The OutcomeReplier uses this verbatim in the "Created
	// [MUL-42]" confirmation message.
	IssueIdentifier string
	// IssueTitle is the title the user supplied on /issue, echoed back
	// in the confirmation message so the chat history reads naturally
	// even when the Multica deep link is not reachable.
	IssueTitle string
}

// IssueCreator is the narrow subset of service.IssueService the
// Dispatcher needs. Declared here as an interface so this package can
// be unit-tested without bringing the full service graph along.
type IssueCreator interface {
	Create(ctx context.Context, p service.IssueCreateParams, opts service.IssueCreateOpts) (service.IssueCreateResult, error)
}

// ChatTaskEnqueuer is the narrow subset of service.TaskService the
// Dispatcher needs. It exists for the same reason as IssueCreator:
// the Dispatcher is small enough that depending on the whole
// TaskService struct is gratuitous.
type ChatTaskEnqueuer interface {
	EnqueueChatTask(ctx context.Context, session db.ChatSession) (db.AgentTaskQueue, error)
}

// DispatcherQueries is the narrow subset of *db.Queries the Dispatcher
// needs for installation routing, identity lookup, dedup, and session
// reload. *db.Queries satisfies it directly; tests substitute a fake.
//
// Dedup is two-phase with owner fencing:
//
//   - ClaimLarkInboundDedup mints a fresh claim_token UUID on insert
//     and on stale-reclaim re-take. The token is the dispatcher's
//     ownership receipt for the row.
//
//   - MarkLarkInboundDedupProcessed and ReleaseLarkInboundDedup are
//     fenced on (message_id, claim_token, processed_at IS NULL). A
//     stale-reclaim that rotates the token invalidates earlier
//     finalizers, so a slow-but-alive worker whose claim was taken
//     over cannot stomp the new holder's row. Both queries return
//     rowsAffected; zero means "your token is no longer the live one"
//     and the dispatcher treats it as a no-op (not an error — the
//     other worker is responsible for the row now).
//
//   - AppendUserMessage invokes the Mark INSIDE its chat_message tx
//     when a claim token is supplied, so the durable write and the
//     Mark commit atomically. That closes the "crashed between
//     commit and Mark" window. See lark_inbound_message_dedup comment
//     in 109_lark_integration.up.sql for the full invariant set.
type DispatcherQueries interface {
	GetLarkInstallationByAppID(ctx context.Context, appID string) (db.LarkInstallation, error)
	GetLarkUserBindingByOpenID(ctx context.Context, arg db.GetLarkUserBindingByOpenIDParams) (db.LarkUserBinding, error)
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
	ClaimLarkInboundDedup(ctx context.Context, arg db.ClaimLarkInboundDedupParams) (db.LarkInboundMessageDedup, error)
	MarkLarkInboundDedupProcessed(ctx context.Context, arg db.MarkLarkInboundDedupProcessedParams) (int64, error)
	ReleaseLarkInboundDedup(ctx context.Context, arg db.ReleaseLarkInboundDedupParams) (int64, error)
	// GetWorkspace is needed to read IssuePrefix so the /issue
	// confirmation message can render the workspace-qualified key
	// ("MUL-42"). A lookup failure is non-fatal — we degrade to
	// emitting just the issue number — so callers handle the error
	// inline rather than aborting the whole dispatch.
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
}

// Dispatcher is the single per-message entry point on the inbound
// path. It owns the order in which identity check, group filter,
// dedup, ingest, /issue, and task enqueue happen — the WS adapter
// MUST NOT bypass it. That ordering is the invariant that keeps the
// design's §4.3 safety property ("unbound users never reach
// chat_session") true at runtime.
type Dispatcher struct {
	Queries      DispatcherQueries
	Chat         ChatSessionService
	Audit        AuditLogger
	IssueService IssueCreator
	TaskService  ChatTaskEnqueuer

	// FlushReply emits the offline/archived notice that EnqueueChatTask
	// now produces only at debounce-flush time. Before MUL-2968 those
	// outcomes were returned synchronously from Handle and the hub's
	// OutcomeReplier sent the card; with the run trigger debounced, the
	// verdict is not known until the window closes, so the dispatcher
	// drives the reply itself via this callback. Wired to
	// OutcomeReplier.Reply in production; nil disables the notice (the
	// message is still durable, only the card is skipped).
	FlushReply FlushReplyFunc

	// Logger is used by the detached flush path, which cannot return
	// errors to a caller and must log them. Defaults to slog.Default().
	Logger *slog.Logger

	// batcher debounces the per-session run trigger. Installed via
	// EnableRunBatching in production; when nil (unit tests / degenerate
	// config) the run fires inline with no debounce — a zero-length
	// window, not a separate code path.
	batcher *pendingBatcher
}

// FlushReplyFunc matches OutcomeReplier.Reply so the production replier can
// be injected directly. It is invoked from the debounced flush goroutine
// to deliver the agent-offline / agent-archived notice.
type FlushReplyFunc func(ctx context.Context, inst db.LarkInstallation, msg InboundMessage, res DispatchResult)

// chatRunFlushTimeout bounds the detached flush (session reload +
// EnqueueChatTask + offline/archived notice). The flush runs on its own
// fresh context because the inbound request ctx is long cancelled by the
// time the window closes.
const chatRunFlushTimeout = 10 * time.Second

// EnableRunBatching installs the in-memory debouncer in front of the
// per-session run trigger. Call once at boot. A non-positive window uses
// DefaultChatRunBatchWindow. Without this, the dispatcher triggers runs
// inline (used by unit tests that assert the immediate effect).
func (d *Dispatcher) EnableRunBatching(window time.Duration) {
	d.batcher = newPendingBatcher(window)
}

// FlushPendingRuns drains every still-pending run trigger immediately and
// blocks until in-flight flushes finish. The hub calls this on graceful
// shutdown, after inbound delivery has stopped, so a normal restart does
// not silently drop a window's worth of messages. No-op when batching is
// disabled.
func (d *Dispatcher) FlushPendingRuns() {
	if d.batcher != nil {
		d.batcher.FlushAll()
	}
}

func (d *Dispatcher) logger() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

// Handle processes one inbound Lark message end-to-end. It never
// returns an error for "this message was dropped" — those are
// reported via Outcome + DropReason and a non-nil err is reserved for
// real infrastructure failures (DB down, etc.) that the WS adapter
// should retry.
//
// Dedup is two-phase. After the installation lookup, ClaimLarkInbound-
// Dedup acquires an in-flight claim on msg.MessageID. After the rest
// of the pipeline runs, the claim is finalized exactly once:
//
//   - MarkLarkInboundDedupProcessed — a durable outcome was reached
//     (audit drop row persisted, OR chat_message + session touched).
//     Future replays of this message_id are dropped as duplicates.
//
//   - ReleaseLarkInboundDedup — an infra error occurred BEFORE any
//     durable side effect. The claim row is deleted so the WS
//     adapter's retry can re-acquire it immediately; otherwise the
//     message would be permanently swallowed as a duplicate even
//     though it never actually landed in chat_session.
func (d *Dispatcher) Handle(ctx context.Context, msg InboundMessage) (DispatchResult, error) {
	// 1. Route to installation. The app_id is the only identifier
	//    that ties an event to its installation row. These two drop
	//    branches run BEFORE the dedup claim because they have no
	//    valid installation row to attach to — see the spec note on
	//    lark_inbound_audit allowing a NULL installation_id.
	inst, err := d.Queries.GetLarkInstallationByAppID(ctx, msg.AppID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_ = d.Audit.RecordDrop(ctx, AuditDropParams{
				EventType:     msg.EventType,
				LarkEventID:   msg.EventID,
				LarkMessageID: msg.MessageID,
				ChatID:        msg.ChatID,
				Reason:        DropReasonInvalidEvent,
			})
			return DispatchResult{Outcome: OutcomeDropped, DropReason: DropReasonInvalidEvent}, nil
		}
		return DispatchResult{}, fmt.Errorf("load installation: %w", err)
	}
	if InstallationStatus(inst.Status) != InstallationActive {
		return d.drop(ctx, msg, inst.ID, DropReasonRevokedInstallation), nil
	}

	// 2. Two-phase dedup claim with owner fencing. Spec §4.3 puts this
	//    before group filter and identity check so a WebSocket
	//    reconnect that replays an event cannot:
	//      a) re-trigger the binding prompt for an unbound user, or
	//      b) re-write the not_addressed_in_group / unbound_user audit
	//         rows, or
	//      c) re-touch the chat_session for a bound message.
	//
	//    The Claim returns claim_token; subsequent Mark / Release calls
	//    are fenced on (message_id, claim_token), and AppendUserMessage
	//    invokes the Mark INSIDE its chat_message tx, so the durable
	//    write + Mark commit atomically. Stale-reclaim by another
	//    worker rotates the token, which invalidates our same-tx Mark
	//    (zero rows → ErrClaimLost → tx rollback).
	//
	//    Empty MessageID means the event has no Lark message_id at all
	//    (non-message events, malformed payloads); skipping dedup is
	//    the safe default — we have no key to deduplicate by, and no
	//    claim to finalize at the end.
	var claimToken pgtype.UUID
	claimed := false
	if msg.MessageID != "" {
		claim, err := d.Queries.ClaimLarkInboundDedup(ctx, db.ClaimLarkInboundDedupParams{
			InstallationID: inst.ID,
			MessageID:      msg.MessageID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Either the row is processed_at IS NOT NULL
				// (terminal) or another worker is actively
				// processing. Either way, the right behavior is to
				// drop without re-doing the work.
				return d.drop(ctx, msg, inst.ID, DropReasonDuplicate), nil
			}
			return DispatchResult{}, fmt.Errorf("dedup claim: %w", err)
		}
		claimToken = claim.ClaimToken
		claimed = true
	}

	res, finalize, err := d.processClaimed(ctx, msg, inst, claimToken)

	if claimed {
		d.applyFinalize(ctx, inst.ID, msg.MessageID, claimToken, finalize)
	}

	// ErrClaimLost is the dispatcher's signal that another worker
	// holds the claim. Surface it as a duplicate drop to the caller —
	// nothing else needs to happen, and the audit row was already
	// written by the in-tx rollback path's caller (see processClaimed).
	if errors.Is(err, ErrClaimLost) {
		return d.drop(ctx, msg, inst.ID, DropReasonDuplicate), nil
	}

	return res, err
}

// dedupFinalize captures the dispatcher's instruction to applyFinalize
// after processClaimed returns. The three states correspond to the
// three terminal positions in the inbound pipeline:
//
//   - finalizeMark: a durable side effect landed OUTSIDE
//     AppendUserMessage's tx (audit drop row, or a post-AppendUser-
//     Message error that left the chat_message committed). Token-
//     fenced Mark locks the row terminal.
//
//   - finalizeRelease: the run did not reach durability. Delete the
//     in-flight row so the WS adapter's retry can re-claim it
//     immediately instead of waiting for the 60s staleness TTL.
//
//   - finalizeNone: AppendUserMessage already finalized the row in
//     its own tx (success → Mark in-tx; ErrClaimLost → another worker
//     owns it). The dispatcher does not touch the row again.
type dedupFinalize int

const (
	finalizeNone dedupFinalize = iota
	finalizeMark
	finalizeRelease
)

// processClaimed runs the post-dedup pipeline. It returns the typed
// dispatch result, a dedupFinalize directive telling the caller how to
// land the claim row, and any error.
//
// Boundary contract per step:
//
//   - Group filter / unbound-user drop → audit row written →
//     finalizeMark.
//   - EnsureChatSession error → tx rolled back, no durable side effect
//     → finalizeRelease.
//   - AppendUserMessage success → chat_message committed AND
//     dedup row already Marked in the same tx → finalizeNone.
//   - AppendUserMessage error → tx rolled back, no chat_message →
//     finalizeRelease (ErrClaimLost is treated specially by Handle).
//   - Post-AppendUserMessage error (issue create, session reload,
//     task enqueue) → chat_message already committed but the
//     in-tx Mark also already committed → finalizeNone.
func (d *Dispatcher) processClaimed(ctx context.Context, msg InboundMessage, inst db.LarkInstallation, claimToken pgtype.UUID) (DispatchResult, dedupFinalize, error) {
	// 3. Group-mention filter (group chats only). We do this BEFORE
	//    identity check so that an unbound user's idle group chatter
	//    never produces an "you need to bind" reply card spam — the
	//    Bot is not addressed, so we say nothing.
	if msg.ChatType == ChatTypeGroup && !msg.AddressedToBot {
		return d.drop(ctx, msg, inst.ID, DropReasonNotAddressedInGroup), finalizeMark, nil
	}

	// 4. Identity check. A row in lark_user_binding means the open_id
	//    maps to a current workspace member (the composite FK to
	//    member cascades the binding away on membership revocation).
	binding, err := d.Queries.GetLarkUserBindingByOpenID(ctx, db.GetLarkUserBindingByOpenIDParams{
		InstallationID: inst.ID,
		LarkOpenID:     string(msg.SenderOpenID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_ = d.Audit.RecordDrop(ctx, AuditDropParams{
				InstallationID: inst.ID,
				ChatID:         msg.ChatID,
				EventType:      msg.EventType,
				LarkEventID:    msg.EventID,
				LarkMessageID:  msg.MessageID,
				Reason:         DropReasonUnboundUser,
			})
			return DispatchResult{
				Outcome:        OutcomeNeedsBinding,
				DropReason:     DropReasonUnboundUser,
				InstallationID: inst.ID,
				SenderOpenID:   msg.SenderOpenID,
			}, finalizeMark, nil
		}
		return DispatchResult{}, finalizeRelease, fmt.Errorf("load user binding: %w", err)
	}

	// 5. Resolve the chat_session. For group chats, the session
	//    creator is the INSTALLER (stable workspace identity that
	//    won't cascade-delete when individual group members churn);
	//    for p2p, the sender is the one and only human in the chat
	//    so we use them.
	sessionCreator := binding.MulticaUserID
	if msg.ChatType == ChatTypeGroup {
		sessionCreator = inst.InstallerUserID
	}
	sessionID, err := d.Chat.EnsureChatSession(ctx, EnsureChatSessionParams{
		WorkspaceID:    inst.WorkspaceID,
		InstallationID: inst.ID,
		AgentID:        inst.AgentID,
		ChatID:         msg.ChatID,
		ChatType:       msg.ChatType,
		Sender:         sessionCreator,
	})
	if err != nil {
		// chat_session create + lark_chat_session_binding create are
		// in a single tx; an error here means the tx rolled back and
		// nothing landed. Safe to release the dedup claim.
		return DispatchResult{}, finalizeRelease, fmt.Errorf("ensure chat session: %w", err)
	}

	// 6. Append message + in-tx dedup Mark — the durable transition
	//    point. After this returns nil the chat_message AND the dedup
	//    Mark have committed atomically; any subsequent failure path
	//    must return finalizeNone (the row is already terminal,
	//    re-Marking is a no-op but re-Releasing would undo nothing
	//    and we don't want to call DELETE on a Marked row).
	//
	//    ErrClaimLost = our token was rotated by a stale-reclaim mid-
	//    flight; the deferred Rollback in AppendUserMessage already
	//    undid the chat_message insert, so Handle treats this as a
	//    duplicate drop. finalizeNone — the other holder owns the row.
	appendRes, err := d.Chat.AppendUserMessage(ctx, AppendUserMessageParams{
		ChatSessionID:  sessionID,
		Sender:         binding.MulticaUserID,
		Body:           msg.Body,
		CommandBody:    msg.CommandBody,
		InstallationID: inst.ID,
		LarkMessageID:  msg.MessageID,
		ClaimToken:     claimToken,
	})
	if err != nil {
		if errors.Is(err, ErrClaimLost) {
			return DispatchResult{}, finalizeNone, err
		}
		// AppendUserMessage's transaction either commits or rolls
		// back atomically; an error means rollback, so no
		// chat_message was written. Safe to release.
		return DispatchResult{}, finalizeRelease, fmt.Errorf("append user message: %w", err)
	}

	// Post-AppendUserMessage paths must NOT Release the claim, because
	// the chat_message + dedup Mark are already committed. Mark-again
	// is a no-op (the in-tx Mark already landed), so finalizeNone.
	postAppendFinalize := finalizeNone
	if !appendRes.DedupMarked {
		// Defensive: the dispatcher always passes a valid claim token,
		// but if a future caller wires AppendUserMessage with an
		// invalid token the Mark would not have run in-tx. Fall back
		// to the post-pipeline Mark so the row still terminates.
		postAppendFinalize = finalizeMark
	}

	res := DispatchResult{
		Outcome:        OutcomeIngested,
		InstallationID: inst.ID,
		ChatSessionID:  sessionID,
		SenderOpenID:   msg.SenderOpenID,
	}

	// 7. /issue command, if present. chat_message is already durable
	//    above; from here all error returns must signal finalizeNone
	//    (or finalizeMark in the defensive fallback above).
	if appendRes.IssueCommand != nil {
		issueRes, err := d.createIssueFromCommand(ctx, inst, binding.MulticaUserID, sessionID, *appendRes.IssueCommand)
		if err != nil {
			return DispatchResult{}, postAppendFinalize, fmt.Errorf("create issue from command: %w", err)
		}
		res.IssueID = issueRes.Issue.ID
		res.IssueNumber = issueRes.Issue.Number
		res.IssueTitle = issueRes.Issue.Title
		// Render the workspace-qualified key ("MUL-42") so the
		// outbound confirmation reads like a Linear/Jira identifier
		// rather than a bare number. A workspace lookup failure here
		// degrades gracefully — we still surface the issue number,
		// just without the workspace prefix — so a Postgres blip on
		// the workspace row does not eat the "/issue created" signal.
		if ws, werr := d.Queries.GetWorkspace(ctx, inst.WorkspaceID); werr == nil && ws.IssuePrefix != "" {
			res.IssueIdentifier = fmt.Sprintf("%s-%d", ws.IssuePrefix, issueRes.Issue.Number)
		} else {
			res.IssueIdentifier = fmt.Sprintf("#%d", issueRes.Issue.Number)
		}
	}

	// 8. Debounce the run trigger. The chat_message + dedup Mark are
	//    already durable; the agent run reads the WHOLE session at
	//    execution time, so a burst of messages in this session is
	//    collapsed into ONE run by deferring EnqueueChatTask behind a
	//    short silence window (MUL-2968). The synchronous outcome is
	//    OutcomeIngested with NO TaskID — the task row is created later,
	//    at flush. EnqueueChatTask's productizable verdicts (agent
	//    offline / archived) and infra errors are now handled inside the
	//    flush (see flushChatRun), not returned here.
	//
	//    Note: a daemon that's merely disconnected is NOT an error. As
	//    long as agent.runtime_id is set, the chat task is enqueued at
	//    flush and waits for the daemon to claim it on next online.
	d.scheduleRun(inst, msg, sessionID)
	return res, postAppendFinalize, nil
}

// scheduleRun hands the per-session run trigger to the debouncer (or fires
// it inline when batching is disabled). The flush closure captures this
// message's installation + InboundMessage so the offline/archived notice,
// if any, targets the right chat; the latest message in a window wins.
func (d *Dispatcher) scheduleRun(inst db.LarkInstallation, msg InboundMessage, sessionID pgtype.UUID) {
	flush := func() { d.flushChatRun(inst, msg, sessionID) }
	if d.batcher == nil {
		// Batching disabled (unit tests / degenerate config): trigger the
		// run immediately. Production always installs a batcher via
		// EnableRunBatching, so this branch does not run in prod.
		flush()
		return
	}
	d.batcher.Schedule(keyForSession(sessionID), flush)
}

// flushChatRun is the debounced run-trigger. It runs once per silence
// window per chat session, detached from the inbound path (on its own
// goroutine and fresh context). It reloads the session, enqueues exactly
// one chat task for the whole window's worth of messages, and — because
// EnqueueChatTask's offline/archived verdict is only known here now —
// emits the corresponding notice itself via FlushReply. Errors cannot be
// returned to a caller (the message is already ACKed and durable), so they
// are logged: a failed enqueue leaves the message in the session to be
// picked up by the next message's run.
func (d *Dispatcher) flushChatRun(inst db.LarkInstallation, msg InboundMessage, sessionID pgtype.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), chatRunFlushTimeout)
	defer cancel()

	session, err := d.Queries.GetChatSession(ctx, sessionID)
	if err != nil {
		d.logger().Error("lark dispatcher: flush reload chat session failed",
			"chat_session_id", uuidString(sessionID),
			"err", err.Error(),
		)
		return
	}
	if _, err := d.TaskService.EnqueueChatTask(ctx, session); err != nil {
		switch {
		case errors.Is(err, service.ErrChatTaskAgentNoRuntime):
			d.emitFlushReply(ctx, inst, msg, sessionID, OutcomeAgentOffline)
		case errors.Is(err, service.ErrChatTaskAgentArchived):
			d.emitFlushReply(ctx, inst, msg, sessionID, OutcomeAgentArchived)
		default:
			// Infra failure (DB down, etc.). Nothing to retry against —
			// the inbound frame was ACKed long ago. Log so the gap is
			// visible; the next message in this session re-triggers a run
			// that will read this message too.
			d.logger().Error("lark dispatcher: flush enqueue chat task failed",
				"chat_session_id", uuidString(sessionID),
				"err", err.Error(),
			)
		}
	}
}

// emitFlushReply delivers an offline/archived notice for a flushed run.
func (d *Dispatcher) emitFlushReply(ctx context.Context, inst db.LarkInstallation, msg InboundMessage, sessionID pgtype.UUID, outcome Outcome) {
	if d.FlushReply == nil {
		return
	}
	d.FlushReply(ctx, inst, msg, DispatchResult{
		Outcome:        outcome,
		InstallationID: inst.ID,
		ChatSessionID:  sessionID,
		SenderOpenID:   msg.SenderOpenID,
	})
}

// keyForSession is the batcher key. chat_session_id is a globally-unique
// UUID, so it alone disambiguates sessions across installations.
func keyForSession(sessionID pgtype.UUID) string {
	return string(sessionID.Bytes[:])
}

// applyFinalize flips the in-flight claim row to its terminal state,
// token-fenced so a slow-but-alive worker whose claim was reclaimed
// cannot stomp the new holder's row.
//
// Best-effort by design for the I/O layer: a transport failure here
// cannot abort the outcome (the user's message is already in
// chat_session or the audit row already exists), and the worst case
// is a stuck in-flight row that the 60-second staleness fallback in
// ClaimLarkInboundDedup re-takes on retry. zero-rows-affected is the
// EXPECTED outcome whenever our token was rotated; it is not an error.
func (d *Dispatcher) applyFinalize(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID, action dedupFinalize) {
	switch action {
	case finalizeMark:
		_, _ = d.Queries.MarkLarkInboundDedupProcessed(ctx, db.MarkLarkInboundDedupProcessedParams{
			InstallationID: installationID,
			MessageID:      messageID,
			ClaimToken:     claimToken,
		})
	case finalizeRelease:
		_, _ = d.Queries.ReleaseLarkInboundDedup(ctx, db.ReleaseLarkInboundDedupParams{
			InstallationID: installationID,
			MessageID:      messageID,
			ClaimToken:     claimToken,
		})
	case finalizeNone:
		// AppendUserMessage already finalized the row in-tx, or our
		// claim was lost to a concurrent reclaim. Do not touch it.
	}
}

func (d *Dispatcher) drop(ctx context.Context, msg InboundMessage, instID pgtype.UUID, reason DropReason) DispatchResult {
	_ = d.Audit.RecordDrop(ctx, AuditDropParams{
		InstallationID: instID,
		ChatID:         msg.ChatID,
		EventType:      msg.EventType,
		LarkEventID:    msg.EventID,
		LarkMessageID:  msg.MessageID,
		Reason:         reason,
	})
	return DispatchResult{
		Outcome:        OutcomeDropped,
		DropReason:     reason,
		InstallationID: instID,
	}
}

func (d *Dispatcher) createIssueFromCommand(
	ctx context.Context,
	inst db.LarkInstallation,
	creatorUserID pgtype.UUID,
	sessionID pgtype.UUID,
	cmd IssueCommand,
) (service.IssueCreateResult, error) {
	// Empty title at this point means the /issue alone fallback found
	// no previous user message either. The product copy ("请填标题")
	// belongs in the WS adapter's reply card; we surface this to the
	// caller as ErrEmptyIssueTitle so the dispatcher can short-circuit
	// without paying the IssueService cost.
	if cmd.Title == "" {
		return service.IssueCreateResult{}, ErrEmptyIssueTitle
	}
	params := service.IssueCreateParams{
		WorkspaceID:  inst.WorkspaceID,
		Title:        cmd.Title,
		Description:  pgtype.Text{String: cmd.Description, Valid: cmd.Description != ""},
		Status:       "todo",
		Priority:     "none",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   inst.AgentID,
		CreatorType:  "member",
		CreatorID:    creatorUserID,
		OriginType:   pgtype.Text{String: originLarkChat, Valid: true},
		OriginID:     sessionID,
	}
	return d.IssueService.Create(ctx, params, service.IssueCreateOpts{})
}

// originLarkChat is the issue.origin_type label written for issues
// created via the Lark `/issue` command. The analytics classifier in
// service.classifyOrigin currently maps unknown origin_type values to
// SourceManual with a warning — that is acceptable for MVP. A
// dedicated analytics source label can be added when product asks for
// it.
const originLarkChat = "lark_chat"

// ErrEmptyIssueTitle is returned by createIssueFromCommand when the
// caller invoked /issue with no title AND the previous-user-message
// fallback found nothing usable. The WS adapter translates this into
// the "please supply a title" reply card per §2.3.
var ErrEmptyIssueTitle = errors.New("issue title is empty")
