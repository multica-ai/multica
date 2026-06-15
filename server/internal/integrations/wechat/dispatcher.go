package wechat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	errChatTaskAgentArchived  = service.ErrChatTaskAgentArchived
	errChatTaskAgentNoRuntime = service.ErrChatTaskAgentNoRuntime
)

type DispatcherQueries interface {
	GetWechatInstallationByBotID(ctx context.Context, botID string) (db.WechatInstallation, error)
	GetWechatUserBindingByUserID(ctx context.Context, arg db.GetWechatUserBindingByUserIDParams) (db.WechatUserBinding, error)
	ClaimWechatInboundDedup(ctx context.Context, arg db.ClaimWechatInboundDedupParams) (db.WechatInboundMessageDedup, error)
	MarkWechatInboundDedupProcessed(ctx context.Context, arg db.MarkWechatInboundDedupProcessedParams) (int64, error)
	ReleaseWechatInboundDedup(ctx context.Context, arg db.ReleaseWechatInboundDedupParams) (int64, error)
}

type ChatTaskEnqueuer interface {
	EnqueueChatTask(ctx context.Context, session db.ChatSession, initiatorUserID pgtype.UUID) (db.AgentTaskQueue, error)
}

type Dispatcher struct {
	Queries      DispatcherQueries
	Chat         *ChatSessionService
	Audit        *AuditLogger
	TaskService  ChatTaskEnqueuer
	Logger       *slog.Logger
}

func (d *Dispatcher) logger() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

func (d *Dispatcher) Handle(ctx context.Context, msg InboundMessage) (DispatchResult, error) {
	// 1. Route to installation
	inst, err := d.Queries.GetWechatInstallationByBotID(ctx, msg.BotID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_ = d.Audit.RecordDrop(ctx, AuditDropParams{
				EventType:       "message",
				WechatMessageID: msg.MessageID,
				ChatID:          msg.ChatID,
				Reason:          DropReasonInvalidEvent,
			})
			return DispatchResult{Outcome: OutcomeDropped, DropReason: DropReasonInvalidEvent}, nil
		}
		return DispatchResult{}, fmt.Errorf("load installation: %w", err)
	}
	if InstallationStatus(inst.Status) != InstallationActive {
		return d.drop(ctx, msg, uuidString(inst.ID), DropReasonRevokedInstallation), nil
	}

	// 2. Dedup
	dedup, err := d.Queries.ClaimWechatInboundDedup(ctx, db.ClaimWechatInboundDedupParams{
		MessageID:      msg.MessageID,
		InstallationID: inst.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return d.drop(ctx, msg, uuidString(inst.ID), DropReasonDuplicate), nil
		}
		return DispatchResult{}, fmt.Errorf("claim dedup: %w", err)
	}
	claimToken := dedup.ClaimToken

	// 3. Group filter: in group chats, only process if addressed to bot
	if msg.ChatType == ChatTypeGroup && !msg.AddressedToBot {
		d.markProcessed(ctx, msg.MessageID, claimToken)
		return d.drop(ctx, msg, uuidString(inst.ID), DropReasonNotAddressedInGroup), nil
	}

	// 4. Identity check — if the sender has no binding, fall through with
	// the installer's identity so the message still reaches the agent.
	binding, err := d.Queries.GetWechatUserBindingByUserID(ctx, db.GetWechatUserBindingByUserIDParams{
		InstallationID: inst.ID,
		WechatUserid:   msg.SenderUserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			d.logger().Info("wechat: unbound user, using installer identity",
				"wechat_user", msg.SenderUserID, "installer", uuidString(inst.InstallerUserID))
			binding = db.WechatUserBinding{
				MulticaUserID: inst.InstallerUserID,
			}
		} else {
			d.releaseDedup(ctx, msg.MessageID, claimToken)
			return DispatchResult{}, fmt.Errorf("identity check: %w", err)
		}
	}

	// 5. Ensure chat session + append message
	chatID := msg.ChatID
	if chatID == "" {
		chatID = fmt.Sprintf("p2p_%s_%s", msg.BotID, msg.SenderUserID)
	}

	session, err := d.Chat.EnsureChatSession(ctx, EnsureChatSessionParams{
		InstallationID: inst.ID,
		WorkspaceID:    inst.WorkspaceID,
		AgentID:        inst.AgentID,
		WechatChatID:   chatID,
		ChatType:       msg.ChatType,
		CreatorUserID:  binding.MulticaUserID,
	})
	if err != nil {
		d.releaseDedup(ctx, msg.MessageID, claimToken)
		return DispatchResult{}, fmt.Errorf("ensure session: %w", err)
	}

	// Store callback req_id for outbound reply routing
	if msg.CallbackReqID != "" {
		d.Chat.UpdateCallbackReqID(ctx, session.ID, msg.CallbackReqID)
	}

	err = d.Chat.AppendUserMessage(ctx, AppendMessageParams{
		ChatSessionID: session.ID,
		UserID:        binding.MulticaUserID,
		Content:       msg.Body,
		MessageID:     msg.MessageID,
		ClaimToken:    claimToken,
	})
	if err != nil {
		d.releaseDedup(ctx, msg.MessageID, claimToken)
		return DispatchResult{}, fmt.Errorf("append message: %w", err)
	}

	// 6. Enqueue chat task
	if d.TaskService != nil {
		_, err = d.TaskService.EnqueueChatTask(ctx, session, binding.MulticaUserID)
		if err != nil {
			d.logger().Warn("wechat: enqueue chat task failed", "error", err)
			if errors.Is(err, errChatTaskAgentNoRuntime) {
				return DispatchResult{
					Outcome:        OutcomeAgentOffline,
					InstallationID: uuidString(inst.ID),
					ChatSessionID:  uuidString(session.ID),
					SenderUserID:   msg.SenderUserID,
				}, nil
			}
			if errors.Is(err, errChatTaskAgentArchived) {
				return DispatchResult{
					Outcome:        OutcomeAgentArchived,
					InstallationID: uuidString(inst.ID),
					ChatSessionID:  uuidString(session.ID),
					SenderUserID:   msg.SenderUserID,
				}, nil
			}
		}
	}

	return DispatchResult{
		Outcome:        OutcomeIngested,
		InstallationID: uuidString(inst.ID),
		ChatSessionID:  uuidString(session.ID),
		SenderUserID:   msg.SenderUserID,
	}, nil
}

func (d *Dispatcher) drop(ctx context.Context, msg InboundMessage, instID string, reason DropReason) DispatchResult {
	_ = d.Audit.RecordDrop(ctx, AuditDropParams{
		InstallationID:  instID,
		EventType:       "message",
		WechatMessageID: msg.MessageID,
		ChatID:          msg.ChatID,
		Reason:          reason,
	})
	return DispatchResult{Outcome: OutcomeDropped, DropReason: reason}
}

func (d *Dispatcher) markProcessed(ctx context.Context, messageID string, claimToken pgtype.UUID) {
	_, _ = d.Queries.MarkWechatInboundDedupProcessed(ctx, db.MarkWechatInboundDedupProcessedParams{
		MessageID:  messageID,
		ClaimToken: claimToken,
	})
}

func (d *Dispatcher) releaseDedup(ctx context.Context, messageID string, claimToken pgtype.UUID) {
	_, _ = d.Queries.ReleaseWechatInboundDedup(ctx, db.ReleaseWechatInboundDedupParams{
		MessageID:  messageID,
		ClaimToken: claimToken,
	})
}
