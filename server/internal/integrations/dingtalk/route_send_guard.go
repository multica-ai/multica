package dingtalk

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// routeSendGuard keeps product replies and processing acknowledgements on the
// exact route generation that accepted the inbound message. The route lock is
// held through the network send: a switch that wins first suppresses the stale
// reply, while a send that wins first completes before the switch is visible.
type routeSendGuard struct {
	q  *db.Queries
	tx engine.TxStarter
}

func newRouteSendGuard(q *db.Queries, tx engine.TxStarter) *routeSendGuard {
	if q == nil || tx == nil {
		return nil
	}
	return &routeSendGuard{q: q, tx: tx}
}

func (g *routeSendGuard) withRoute(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, userID pgtype.UUID, send func(engine.ResolvedInstallation) error) error {
	if g == nil {
		return send(inst)
	}
	tx, err := g.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin dingtalk reply fence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := g.q.WithTx(tx)
	if !userID.Valid {
		binding, bindErr := qtx.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
			InstallationID: inst.ID,
			ChannelUserID:  msg.Source.SenderID,
		})
		if errors.Is(bindErr, pgx.ErrNoRows) {
			return nil
		}
		if bindErr != nil {
			return fmt.Errorf("resolve dingtalk reply sender: %w", bindErr)
		}
		userID = binding.MulticaUserID
	}
	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, inst.WorkspaceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lock dingtalk reply workspace: %w", err)
	}
	if _, err := qtx.LockActiveDingTalkConnectorForReply(ctx, inst.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lock dingtalk reply connector: %w", err)
	}
	if _, err := qtx.LockDingTalkMemberForRoute(ctx, db.LockDingTalkMemberForRouteParams{
		WorkspaceID:   inst.WorkspaceID,
		MulticaUserID: userID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lock dingtalk reply member: %w", err)
	}
	if _, err := qtx.LockActiveDingTalkGrantForRoute(ctx, db.LockActiveDingTalkGrantForRouteParams{
		ConnectorID: inst.ID,
		WorkspaceID: inst.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lock dingtalk reply grant: %w", err)
	}
	if msg.Source.ChatType == channel.ChatTypeP2P {
		_, err = qtx.LockDingTalkDirectReplyRoute(ctx, db.LockDingTalkDirectReplyRouteParams{
			MulticaUserID: userID,
			ConnectorID:   inst.ID,
			ChannelUserID: msg.Source.SenderID,
			WorkspaceID:   inst.WorkspaceID,
			AgentID:       inst.AgentID,
			RouteRevision: inst.RouteRevision,
		})
	} else {
		_, err = qtx.LockDingTalkGroupReplyRoute(ctx, db.LockDingTalkGroupReplyRouteParams{
			MulticaUserID:  userID,
			InstallationID: inst.ID,
			ConversationID: msg.Source.ChatID,
			WorkspaceID:    inst.WorkspaceID,
			AgentID:        inst.AgentID,
			RouteRevision:  inst.RouteRevision,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock dingtalk reply route: %w", err)
	}
	return g.sendAndCommit(ctx, tx, qtx, inst, send)
}

func (g *routeSendGuard) withConnector(ctx context.Context, inst engine.ResolvedInstallation, workspaceID pgtype.UUID, send func(engine.ResolvedInstallation, *db.Queries) error) error {
	if g == nil {
		return send(inst, nil)
	}
	tx, err := g.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin dingtalk connector reply fence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := g.q.WithTx(tx)
	if workspaceID.Valid {
		if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, workspaceID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("lock dingtalk connector reply workspace: %w", err)
		}
		_, err = qtx.LockActiveDingTalkConnectorGrantForReply(ctx, db.LockActiveDingTalkConnectorGrantForReplyParams{
			ConnectorID: inst.ID,
			WorkspaceID: workspaceID,
		})
	} else {
		_, err = qtx.LockActiveDingTalkConnectorForReply(ctx, inst.ID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock dingtalk connector for reply: %w", err)
	}
	connector, err := qtx.GetDingTalkConnector(ctx, inst.ID)
	if err != nil {
		return fmt.Errorf("load fenced dingtalk connector: %w", err)
	}
	inst.Platform = connector
	if err := send(inst, qtx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit dingtalk reply fence: %w", err)
	}
	return nil
}

func (g *routeSendGuard) sendAndCommit(ctx context.Context, tx pgx.Tx, qtx *db.Queries, inst engine.ResolvedInstallation, send func(engine.ResolvedInstallation) error) error {
	connector, err := qtx.GetDingTalkConnector(ctx, inst.ID)
	if err != nil {
		return fmt.Errorf("load fenced dingtalk connector: %w", err)
	}
	inst.Platform = connector
	if err := send(inst); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit dingtalk reply fence: %w", err)
	}
	return nil
}
