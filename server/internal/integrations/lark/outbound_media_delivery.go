package lark

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type mediaDeliveryQueries interface {
	GetChannelReplyTurn(context.Context, pgtype.UUID) (db.GetChannelReplyTurnRow, error)
	AcquireChannelReplyDelivery(context.Context, db.AcquireChannelReplyDeliveryParams) (db.ChannelReplyDelivery, error)
	MarkChannelReplyDeliverySending(context.Context, db.MarkChannelReplyDeliverySendingParams) (int64, error)
	RenewChannelReplyDelivery(context.Context, db.RenewChannelReplyDeliveryParams) (int64, error)
	SettleChannelReplyDelivery(context.Context, db.SettleChannelReplyDeliveryParams) (int64, error)
}

type mediaDeliveryContextKey struct{}
type mediaWorkspaceContextKey struct{}
type mediaDeliveryOwner struct {
	taskID, turnID, token pgtype.UUID
	interrupted           bool
	started               bool
}

// The existing reply ledger fences duplicate completion events and retry
// attempts across replicas and restarts, beyond Feishu's one-hour UUID window.
// A crashed in-flight batch is never blindly replayed: per-file receipts are
// unavailable, so a successor reports the uncertainty and leaves files in Multica.
func (p *Patcher) claimMediaDelivery(ctx context.Context, taskID pgtype.UUID, binding ChatSessionBinding) (context.Context, func(), bool) {
	turn, err := p.queries.GetChannelReplyTurn(ctx, taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		turn.TurnID, err = taskID, nil
	}
	if err != nil || !turn.TurnID.Valid {
		return ctx, nil, false
	}
	owner := &mediaDeliveryOwner{taskID: taskID, turnID: turn.TurnID, token: pgtype.UUID{Bytes: uuid.New(), Valid: true}}
	row, err := p.queries.AcquireChannelReplyDelivery(ctx, db.AcquireChannelReplyDeliveryParams{
		TurnID: owner.turnID, TaskID: taskID, AttemptDepth: turn.AttemptDepth,
		BindingID: binding.ID, InstallationID: binding.InstallationID,
		ChannelType: channelTypeFeishu, ChatID: binding.ChannelChatID, Phase: "terminal",
		OwnerToken: owner.token,
		// One bounded transfer plus its detached notice and settlement must
		// finish before another process can take ownership.
		LeaseSeconds: (defaultRequestTimeout + p.mediaBudget + attachmentNoticeBudget + 10*time.Second).Seconds(),
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			p.cfg.Logger.Warn("lark outbound: attachment ownership unavailable")
		}
		return ctx, nil, false
	}
	owner.interrupted = row.SendState == "in_flight" || row.SendState == "unknown"
	ctx = context.WithValue(ctx, mediaDeliveryContextKey{}, owner)
	finish := func() { p.finishMediaDelivery(ctx) }
	return ctx, finish, true
}

func (p *Patcher) finishMediaDelivery(ctx context.Context) {
	owner, ok := ctx.Value(mediaDeliveryContextKey{}).(*mediaDeliveryOwner)
	if !ok {
		return
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if n, err := p.queries.SettleChannelReplyDelivery(recordCtx, db.SettleChannelReplyDeliveryParams{
		TurnID: owner.turnID, OwnerToken: owner.token, SettledReason: "attachment_attempt_ended",
	}); err != nil || n != 1 {
		p.cfg.Logger.Warn("lark outbound: attachment settlement unconfirmed")
	}
}

func (p *Patcher) markMediaSending(ctx context.Context) bool {
	owner, ok := ctx.Value(mediaDeliveryContextKey{}).(*mediaDeliveryOwner)
	if !ok {
		return true
	}
	if owner.interrupted {
		return false
	}
	if owner.started {
		return true
	}
	n, err := p.queries.MarkChannelReplyDeliverySending(ctx, db.MarkChannelReplyDeliverySendingParams{TurnID: owner.turnID, OwnerToken: owner.token})
	owner.started = err == nil && n == 1
	return owner.started
}

// The frozen task delivery remains authoritative after /new; never look up
// the session's newest binding or a different account to choose a destination.
// Recheck before uploads, sends, retries and notices so queueing does not extend
// the lifetime of a revoked installation or a deleted/archived conversation.
func (p *Patcher) currentMediaCredentials(ctx context.Context, original InstallationCredentials, binding ChatSessionBinding, workspaceID pgtype.UUID) (InstallationCredentials, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if !workspaceID.Valid {
		workspaceID, _ = ctx.Value(mediaWorkspaceContextKey{}).(pgtype.UUID)
	}
	if err := ctx.Err(); err != nil {
		return InstallationCredentials{}, err
	}
	inst, err := p.queries.GetLarkInstallation(ctx, binding.InstallationID)
	if err != nil || !inst.WorkspaceID.Valid || InstallationStatus(inst.Status) != InstallationActive || inst.AppID != original.AppID ||
		inst.TenantKey.String != original.TenantKey || RegionOrDefault(inst.Region) != RegionOrDefault(string(original.Region)) ||
		(workspaceID.Valid && inst.WorkspaceID != workspaceID) {
		return InstallationCredentials{}, errors.New("attachment connection unavailable")
	}
	session, err := p.queries.GetChatSession(ctx, binding.ChatSessionID)
	if err != nil || session.Status != "active" || session.WorkspaceID != inst.WorkspaceID {
		return InstallationCredentials{}, errors.New("attachment conversation unavailable")
	}
	if owner, ok := ctx.Value(mediaDeliveryContextKey{}).(*mediaDeliveryOwner); ok {
		delivery, err := p.queries.GetChannelTaskDelivery(ctx, owner.taskID)
		if err != nil || delivery.ChannelType != channelTypeFeishu || delivery.BindingID != binding.ID ||
			delivery.InstallationID != binding.InstallationID || delivery.ChannelChatID != binding.ChannelChatID {
			return InstallationCredentials{}, errors.New("attachment destination changed")
		}
		task, err := p.queries.GetAgentTask(ctx, owner.taskID)
		if err != nil || (task.AgentID.Valid && task.AgentID != inst.AgentID) {
			return InstallationCredentials{}, errors.New("attachment agent changed")
		}
		// Re-prove the database owner before each external write, including
		// timeout notices. A previous process must stop after takeover.
		n, err := p.queries.RenewChannelReplyDelivery(ctx, db.RenewChannelReplyDeliveryParams{
			TurnID: owner.turnID, OwnerToken: owner.token,
			LeaseSeconds: (defaultRequestTimeout + p.mediaBudget + attachmentNoticeBudget + 10*time.Second).Seconds(),
		})
		if err != nil || n != 1 {
			return InstallationCredentials{}, errors.New("attachment delivery ownership lost")
		}
	}
	return p.installationCredentials(inst)
}
