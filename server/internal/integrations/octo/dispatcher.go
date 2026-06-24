package octo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DispatcherQueries is the subset of generated queries the dispatcher needs.
// An interface so the dispatcher can be unit-tested with a fake.
type DispatcherQueries interface {
	GetOctoInstallationByRobotID(ctx context.Context, robotID string) (db.OctoInstallation, error)
	ClaimOctoInboundDedup(ctx context.Context, arg db.ClaimOctoInboundDedupParams) (db.OctoInboundDedup, error)
	MarkOctoInboundDedupProcessed(ctx context.Context, arg db.MarkOctoInboundDedupProcessedParams) (int64, error)
	ReleaseOctoInboundDedup(ctx context.Context, arg db.ReleaseOctoInboundDedupParams) (int64, error)
	GetOctoUserBindingByUID(ctx context.Context, arg db.GetOctoUserBindingByUIDParams) (db.OctoUserBinding, error)
}

// ChatService find-or-creates sessions and appends user messages. Satisfied by
// *ChatSessionService.
type ChatService interface {
	EnsureChatSession(ctx context.Context, p EnsureChatSessionParams) (db.ChatSession, error)
	AppendUserMessage(ctx context.Context, p AppendUserMessageParams) (AppendResult, error)
}

// ChatTaskEnqueuer enqueues an agent run for a chat session. Satisfied by
// *service.TaskService.
type ChatTaskEnqueuer interface {
	EnqueueChatTask(ctx context.Context, session db.ChatSession, initiatorUserID pgtype.UUID, forceFreshSession bool) (db.AgentTaskQueue, error)
}

// Dispatcher converts inbound Octo messages into chat_session + chat_message
// rows and enqueues agent tasks. It enforces idempotency (two-phase dedup),
// the group-mention filter, and identity binding before any durable write.
type Dispatcher struct {
	Queries     DispatcherQueries
	Chat        ChatService
	TaskService ChatTaskEnqueuer
	Audit       AuditLogger
	Logger      *slog.Logger
}

func (d *Dispatcher) logger() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

// Handle processes one inbound message. Business outcomes (dropped, needs
// binding, ingested, …) are reported via DispatchResult; a non-nil error is
// reserved for infra failures the caller may retry.
func (d *Dispatcher) Handle(ctx context.Context, msg InboundMessage) (DispatchResult, error) {
	// 1. Route to installation by robot_id. Runs before the dedup claim because
	//    a routing failure has no installation row to attach a claim to.
	inst, err := d.Queries.GetOctoInstallationByRobotID(ctx, msg.RobotID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_ = d.Audit.RecordDrop(ctx, AuditDropParams{
				ChannelID: msg.ChannelID,
				MessageID: msg.MessageID,
				Reason:    DropReasonInvalidEvent,
			})
			return DispatchResult{Outcome: OutcomeDropped, DropReason: DropReasonInvalidEvent}, nil
		}
		return DispatchResult{}, fmt.Errorf("load installation: %w", err)
	}
	if InstallationStatus(inst.Status) != InstallationActive {
		return d.drop(ctx, msg, inst.ID, DropReasonRevokedInstallation), nil
	}

	// 2. Two-phase dedup claim with owner fencing, before the group filter and
	//    identity check so a WS reconnect replay cannot re-trigger binding
	//    prompts, re-write audit rows, or re-touch the session. Empty MessageID
	//    means no dedup key — skip the gate.
	var claimToken pgtype.UUID
	claimed := false
	if msg.MessageID != "" {
		claim, err := d.Queries.ClaimOctoInboundDedup(ctx, db.ClaimOctoInboundDedupParams{
			InstallationID: inst.ID,
			MessageID:      msg.MessageID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Terminal (already processed) or actively being processed by
				// another worker — drop without redoing the work.
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
	if errors.Is(err, ErrClaimLost) {
		return d.drop(ctx, msg, inst.ID, DropReasonDuplicate), nil
	}
	return res, err
}

// dedupFinalize tells Handle how to land the dedup claim after processClaimed.
type dedupFinalize int

const (
	finalizeNone    dedupFinalize = iota // AppendUserMessage already finalized in-tx
	finalizeMark                         // durable side effect outside the tx → lock terminal
	finalizeRelease                      // no durable side effect → free the claim for retry
)

// processClaimed runs the post-dedup pipeline: group filter → identity → ensure
// session → append message → enqueue task. It returns the result, a finalize
// directive, and any error.
func (d *Dispatcher) processClaimed(ctx context.Context, msg InboundMessage, inst db.OctoInstallation, claimToken pgtype.UUID) (DispatchResult, dedupFinalize, error) {
	// 3. Group-mention filter. A group message not addressed to the bot is
	//    silently dropped (no binding spam). Done before identity check.
	if msg.ChannelType == ChannelGroup && !msg.AddressedToBot {
		return d.drop(ctx, msg, inst.ID, DropReasonNotAddressedInGroup), finalizeMark, nil
	}

	// 4. Identity check. A binding row proves the uid maps to a current
	//    workspace member (composite FK cascades the binding away on removal).
	binding, err := d.Queries.GetOctoUserBindingByUID(ctx, db.GetOctoUserBindingByUIDParams{
		InstallationID: inst.ID,
		OctoUid:        string(msg.SenderUID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_ = d.Audit.RecordDrop(ctx, AuditDropParams{
				InstallationID: inst.ID,
				ChannelID:      msg.ChannelID,
				MessageID:      msg.MessageID,
				Reason:         DropReasonUnboundUser,
			})
			return DispatchResult{
				Outcome:        OutcomeNeedsBinding,
				DropReason:     DropReasonUnboundUser,
				InstallationID: inst.ID,
				SenderUID:      msg.SenderUID,
			}, finalizeMark, nil
		}
		return DispatchResult{}, finalizeRelease, fmt.Errorf("load user binding: %w", err)
	}

	// 5. Resolve the chat_session. For group chats the creator is the installer
	//    (stable workspace identity that won't cascade away as members churn);
	//    for DMs the sender is the only human, so use them.
	creator := binding.MulticaUserID
	if msg.ChannelType == ChannelGroup {
		creator = inst.InstallerUserID
	}
	session, err := d.Chat.EnsureChatSession(ctx, EnsureChatSessionParams{
		WorkspaceID:    inst.WorkspaceID,
		InstallationID: inst.ID,
		AgentID:        inst.AgentID,
		ChannelID:      msg.ChannelID,
		ChannelType:    msg.ChannelType,
		Creator:        creator,
	})
	if err != nil {
		return DispatchResult{}, finalizeRelease, fmt.Errorf("ensure chat session: %w", err)
	}

	// 6. Append message + in-tx dedup Mark — the durable transition. After this
	//    returns nil the chat_message AND the dedup Mark committed atomically;
	//    any later failure must return finalizeNone (re-Mark is a no-op, and we
	//    must not Release a row that is already terminal).
	appendRes, err := d.Chat.AppendUserMessage(ctx, AppendUserMessageParams{
		ChatSessionID:  session.ID,
		Body:           msg.Body,
		InstallationID: inst.ID,
		MessageID:      msg.MessageID,
		ClaimToken:     claimToken,
	})
	if err != nil {
		if errors.Is(err, ErrClaimLost) {
			return DispatchResult{}, finalizeNone, err
		}
		return DispatchResult{}, finalizeRelease, fmt.Errorf("append user message: %w", err)
	}

	postAppendFinalize := finalizeNone
	if !appendRes.DedupMarked {
		// Defensive: if a future caller passes no claim token, the in-tx Mark
		// did not run; fall back to the post-pipeline Mark.
		postAppendFinalize = finalizeMark
	}

	res := DispatchResult{
		Outcome:        OutcomeIngested,
		InstallationID: inst.ID,
		ChatSessionID:  session.ID,
		SenderUID:      msg.SenderUID,
	}

	// 7. Enqueue the agent run. The chat_message is already durable, so all
	//    paths here return postAppendFinalize (never Release). EnsureChatSession
	//    already handed back the full session row, so there is no reload here. A
	//    daemon that is merely disconnected is not an error — as long as the
	//    agent has a runtime, the task waits to be claimed.
	task, err := d.TaskService.EnqueueChatTask(ctx, session, binding.MulticaUserID, false)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrChatTaskAgentNoRuntime):
			res.Outcome = OutcomeAgentOffline
		case errors.Is(err, service.ErrChatTaskAgentArchived):
			res.Outcome = OutcomeAgentArchived
		default:
			// Infra failure. The message is durable; log and let the next
			// message re-trigger a run.
			d.logger().Error("octo dispatcher: enqueue chat task failed",
				"chat_session_id", uuidString(session.ID), "err", err.Error())
		}
		return res, postAppendFinalize, nil
	}
	res.TaskID = task.ID
	return res, postAppendFinalize, nil
}

// drop records an audit row and returns a dropped result.
func (d *Dispatcher) drop(ctx context.Context, msg InboundMessage, instID pgtype.UUID, reason DropReason) DispatchResult {
	_ = d.Audit.RecordDrop(ctx, AuditDropParams{
		InstallationID: instID,
		ChannelID:      msg.ChannelID,
		MessageID:      msg.MessageID,
		Reason:         reason,
	})
	return DispatchResult{Outcome: OutcomeDropped, DropReason: reason, InstallationID: instID}
}

// applyFinalize lands the dedup claim per the directive from processClaimed.
func (d *Dispatcher) applyFinalize(ctx context.Context, instID pgtype.UUID, messageID string, claimToken pgtype.UUID, f dedupFinalize) {
	switch f {
	case finalizeMark:
		if _, err := d.Queries.MarkOctoInboundDedupProcessed(ctx, db.MarkOctoInboundDedupProcessedParams{
			InstallationID: instID,
			MessageID:      messageID,
			ClaimToken:     claimToken,
		}); err != nil {
			d.logger().Error("octo dispatcher: mark dedup failed", "message_id", messageID, "err", err.Error())
		}
	case finalizeRelease:
		if _, err := d.Queries.ReleaseOctoInboundDedup(ctx, db.ReleaseOctoInboundDedupParams{
			InstallationID: instID,
			MessageID:      messageID,
			ClaimToken:     claimToken,
		}); err != nil {
			d.logger().Error("octo dispatcher: release dedup failed", "message_id", messageID, "err", err.Error())
		}
	case finalizeNone:
		// AppendUserMessage already finalized the row in its own transaction.
	}
}

func uuidString(u pgtype.UUID) string { return util.UUIDToString(u) }
