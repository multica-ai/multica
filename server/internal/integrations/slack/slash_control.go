package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type slashControlTasks interface {
	PrepareChatTaskEnqueue(ctx context.Context, agentID, initiator pgtype.UUID) (service.PreparedChatTaskEnqueue, error)
	EnqueuePreparedChannelChatTaskInTx(ctx context.Context, tx pgx.Tx, session db.ChatSession, initiator pgtype.UUID, forceFresh bool, contextRevision int64, prepared service.PreparedChatTaskEnqueue) (db.AgentTaskQueue, error)
	FinalizeChatTaskEnqueue(ctx context.Context, task db.AgentTaskQueue)
}

type slackDMControlStarter struct {
	q         *db.Queries
	session   *engine.ChatSession
	tasks     slashControlTasks
	lifecycle engine.ChannelChatLifecycle
}

const maxSlackControlRouteRetries = 8

func NewSlackDMControlStarter(q *db.Queries, tx engine.TxStarter, tasks slashControlTasks, lifecycle engine.ChannelChatLifecycle) slashControlStarter {
	return &slackDMControlStarter{
		q: q, tasks: tasks, lifecycle: lifecycle,
		session: engine.NewChatSession(q, tx, TypeSlack, engine.SessionTitles{}),
	}
}

func (s *slackDMControlStarter) StartSlackDMChat(ctx context.Context, inst engine.ResolvedInstallation, userID pgtype.UUID, cmd slack.SlashCommand, envelopeID string) error {
	if envelopeID == "" {
		return errors.New("slack /new: missing socket envelope id")
	}
	d := &deduper{q: s.q}
	claim, err := d.Claim(ctx, inst.ID, envelopeID)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = d.Release(context.WithoutCancel(ctx), inst.ID, envelopeID, claim)
		}
	}()

	body := strings.TrimSpace(cmd.Text)
	var prepared service.PreparedChatTaskEnqueue
	if body != "" {
		prepared, err = s.tasks.PrepareChatTaskEnqueue(ctx, inst.AgentID, userID)
		if err != nil {
			return fmt.Errorf("slack /new: prepare task: %w", err)
		}
	}
	var task db.AgentTaskQueue
	var started engine.StartSessionResult
	for attempt := 0; ; attempt++ {
		task = db.AgentTaskQueue{}
		started, err = s.session.StartSession(ctx, engine.StartSessionInput{
			EnsureSessionInput: engine.EnsureSessionInput{
				WorkspaceID: inst.WorkspaceID, AgentID: inst.AgentID, InstallationID: inst.ID,
				Sender: userID, BindingKey: cmd.ChannelID, ChatType: channel.ChatTypeP2P,
			},
			Initiator: userID,
			Body:      body, DedupMessageID: envelopeID, ClaimToken: claim,
			PersistMessage: body != "", HistoryBoundaryPending: true,
			BeforeCommit: func(ctx context.Context, tx pgx.Tx, session db.ChatSession) error {
				if body == "" {
					return nil
				}
				var enqueueErr error
				task, enqueueErr = s.tasks.EnqueuePreparedChannelChatTaskInTx(ctx, tx, session, userID, false, 1, prepared)
				return enqueueErr
			},
		})
		if !errors.Is(err, engine.ErrRouteChanged) {
			break
		}
		slog.InfoContext(ctx, "slack /new route changed; retrying",
			"outcome", "chat_route_retry", "channel_type", string(TypeSlack),
			"installation_id", inst.ID, "attempt", attempt+1,
		)
		if attempt+1 >= maxSlackControlRouteRetries {
			return fmt.Errorf("slack /new route did not stabilize after %d retries: %w", maxSlackControlRouteRetries, err)
		}
	}
	if err != nil {
		return err
	}
	committed = true
	if s.lifecycle != nil {
		s.lifecycle.ChannelChatStarted(engine.ChannelChatStartedEvent{
			WorkspaceID: inst.WorkspaceID, CreatorID: userID, AgentID: inst.AgentID,
			SessionID: started.SessionID, InstallationID: inst.ID,
			ChannelType: TypeSlack, RouteRevision: started.RouteRevision,
			Title: started.Append.InitialTitle,
		})
		if started.Append.InitialTitle != "" {
			s.lifecycle.GenerateChannelChatTitle(inst.WorkspaceID, userID, started.SessionID, started.Append.InitialTitle, body)
		}
	}
	if body == "" {
		return nil
	}
	s.tasks.FinalizeChatTaskEnqueue(ctx, task)
	return nil
}

func (s *slackDMControlStarter) ClearSlackDMContext(ctx context.Context, inst engine.ResolvedInstallation, userID pgtype.UUID, cmd slack.SlashCommand, envelopeID string) error {
	if envelopeID == "" {
		return errors.New("slack /clear: missing socket envelope id")
	}
	d := &deduper{q: s.q}
	claim, err := d.Claim(ctx, inst.ID, envelopeID)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = d.Release(context.WithoutCancel(ctx), inst.ID, envelopeID, claim)
		}
	}()

	body := strings.TrimSpace(cmd.Text)
	var prepared service.PreparedChatTaskEnqueue
	if body != "" {
		prepared, err = s.tasks.PrepareChatTaskEnqueue(ctx, inst.AgentID, userID)
		if err != nil {
			return fmt.Errorf("slack /clear: prepare task: %w", err)
		}
	}

	var task db.AgentTaskQueue
	for attempt := 0; ; attempt++ {
		sessionID, ensureErr := s.session.EnsureSession(ctx, engine.EnsureSessionInput{
			WorkspaceID: inst.WorkspaceID, AgentID: inst.AgentID, InstallationID: inst.ID,
			Sender: userID, BindingKey: cmd.ChannelID, ChatType: channel.ChatTypeP2P,
		})
		if ensureErr != nil {
			return ensureErr
		}

		task = db.AgentTaskQueue{}
		if body == "" {
			err = s.session.MarkPendingFreshWithDedup(ctx, sessionID, "", inst.ID, envelopeID, claim)
		} else {
			_, err = s.session.AppendUserMessage(ctx, engine.AppendInput{
				SessionID: sessionID, Sender: userID, InstallationID: inst.ID,
				Body: body, CommandText: clearSlashCommand,
				DedupMessageID: envelopeID, ClaimToken: claim, ForceFresh: true,
				BeforeCommit: func(ctx context.Context, tx pgx.Tx, session db.ChatSession, contextRevision int64, _ pgtype.UUID, _ int64) error {
					var enqueueErr error
					task, enqueueErr = s.tasks.EnqueuePreparedChannelChatTaskInTx(
						ctx, tx, session, userID, true, contextRevision, prepared,
					)
					return enqueueErr
				},
			})
		}
		if !errors.Is(err, engine.ErrRouteChanged) {
			break
		}
		slog.InfoContext(ctx, "slack /clear route changed; retrying",
			"outcome", "context_clear_route_retry", "channel_type", string(TypeSlack),
			"installation_id", inst.ID, "attempt", attempt+1,
		)
		if attempt+1 >= maxSlackControlRouteRetries {
			return fmt.Errorf("slack /clear route did not stabilize after %d retries: %w", maxSlackControlRouteRetries, err)
		}
	}
	if err != nil {
		return err
	}
	committed = true
	if task.ID.Valid {
		s.tasks.FinalizeChatTaskEnqueue(ctx, task)
	}
	return nil
}
