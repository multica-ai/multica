package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// outboundQueries is the slice of generated queries the DingTalk outbound
// subscriber needs. *db.Queries satisfies it.
type outboundQueries interface {
	WithTx(tx pgx.Tx) outboundQueries
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
	GetActiveDingTalkConnectorInWorkspace(ctx context.Context, arg db.GetActiveDingTalkConnectorInWorkspaceParams) (db.DingtalkConnector, error)
	LockWorkspaceForChatSessionCreate(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
	LockActiveDingTalkConnectorForReply(ctx context.Context, connectorID pgtype.UUID) (pgtype.UUID, error)
	LockDingTalkActiveAgentMemberForRoute(ctx context.Context, arg db.LockDingTalkActiveAgentMemberForRouteParams) (pgtype.UUID, error)
	LockActiveDingTalkGrantForRoute(ctx context.Context, arg db.LockActiveDingTalkGrantForRouteParams) (pgtype.UUID, error)
	LockDingTalkGroupOutboundRoute(ctx context.Context, arg db.LockDingTalkGroupOutboundRouteParams) (int64, error)
	LockDingTalkDirectOutboundRoute(ctx context.Context, arg db.LockDingTalkDirectOutboundRouteParams) (int64, error)
}

type dbOutboundQueries struct{ *db.Queries }

func (q dbOutboundQueries) WithTx(tx pgx.Tx) outboundQueries {
	return dbOutboundQueries{q.Queries.WithTx(tx)}
}

// Outbound delivers an agent's chat reply back to DingTalk — the outbound half
// of the round trip. On EventChatDone / EventTaskFailed
// it finds the DingTalk chat binding for the task's session and posts the reply
// (or failure notice) into the originating conversation. Sessions with no
// DingTalk binding are ignored, so it coexists with the Feishu and Slack
// subscribers on the shared event bus. Registered only when DingTalk is
// configured.
type Outbound struct {
	q       outboundQueries
	tx      engine.TxStarter
	decrypt Decrypter
	client  *Client
	logger  *slog.Logger
}

// NewOutbound builds the DingTalk outbound subscriber over the generated queries,
// the AppSecret decrypter, and the shared token-caching Client.
func NewOutbound(q *db.Queries, tx engine.TxStarter, decrypt Decrypter, client *Client, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = NewClient(nil, "")
	}
	return &Outbound{q: dbOutboundQueries{q}, tx: tx, decrypt: decrypt, client: client, logger: logger}
}

// Register subscribes to chat-done and task-failed. Task-failed keeps the DingTalk
// conversation consistent with the web transcript — without it a failed run
// leaves the user staring at the "👀 On it" ack forever.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, o.handleEvent)
}

func (o *Outbound) handleEvent(e events.Event) {
	// Bus delivery is synchronous, so a stuck DingTalk HTTP call must not wedge
	// the publish call site: use a fresh ctx with a tight timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "dingtalk outbound: reply delivery failed",
			"error", err, "chat_session_id", e.ChatSessionID)
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	taskID, sessionID, ok := taskAndSessionFromEvent(e)
	if !ok || !sessionID.Valid {
		// Issue / autopilot tasks carry no chat_session.
		return nil
	}
	content := eventContent(e)
	if content == "" {
		return nil // nothing to say (empty completion, or a retry-pending failure)
	}
	if o.tx == nil {
		return errors.New("dingtalk outbound: transaction starter unavailable")
	}
	tx, err := o.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin dingtalk outbound tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := o.q.WithTx(tx)
	binding, err := qtx.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   string(TypeDingTalk),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // not a DingTalk session (Feishu / Slack / web-only)
		}
		return fmt.Errorf("lookup dingtalk chat binding: %w", err)
	}
	task, err := qtx.GetAgentTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load agent task: %w", err)
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, qtx, task)
	if err != nil {
		return fmt.Errorf("classify task input origin: %w", err)
	}
	if !deliver {
		return nil
	}
	if !task.InitiatorUserID.Valid {
		return nil
	}
	session, err := qtx.GetChatSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load dingtalk chat session: %w", err)
	}
	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, session.WorkspaceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lock dingtalk outbound workspace: %w", err)
	}
	target := outboundTarget(binding)
	if err := lockDingTalkOutboundRoute(ctx, qtx, binding, session, task.InitiatorUserID, target); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	// Re-read after taking the route generation fence. An A→B→A switch can
	// restore the same workspace/agent identity while still deleting the old
	// session binding. A fresh READ COMMITTED statement makes that intermediate
	// cutover visible; never revive the old session merely because the route's
	// visible target happens to match again.
	currentBinding, err := qtx.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   string(TypeDingTalk),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recheck dingtalk chat binding: %w", err)
	}
	if currentBinding.ID != binding.ID || currentBinding.InstallationID != binding.InstallationID || outboundTarget(currentBinding) != target {
		return nil
	}
	inst, err := qtx.GetActiveDingTalkConnectorInWorkspace(ctx, db.GetActiveDingTalkConnectorInWorkspaceParams{
		ConnectorID: binding.InstallationID,
		WorkspaceID: session.WorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("load dingtalk installation: %w", err)
	}
	creds, err := decodeCredentials(inst.Config, o.decrypt)
	if err != nil {
		return fmt.Errorf("decode dingtalk credentials: %w", err)
	}
	s := &sender{client: o.client, robotCode: creds.RobotCode, appKey: creds.AppKey, appSecret: creds.AppSecret}
	if _, err := s.send(ctx, target, content); err != nil {
		return fmt.Errorf("post dingtalk reply: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit dingtalk outbound fence: %w", err)
	}
	return nil
}

func lockDingTalkOutboundRoute(ctx context.Context, q outboundQueries, binding db.ChannelChatSessionBinding, session db.ChatSession, userID pgtype.UUID, target sendTarget) error {
	if _, err := q.LockActiveDingTalkConnectorForReply(ctx, binding.InstallationID); err != nil {
		return err
	}
	if _, err := q.LockDingTalkActiveAgentMemberForRoute(ctx, db.LockDingTalkActiveAgentMemberForRouteParams{
		AgentID:       session.AgentID,
		WorkspaceID:   session.WorkspaceID,
		MulticaUserID: userID,
	}); err != nil {
		return err
	}
	if _, err := q.LockActiveDingTalkGrantForRoute(ctx, db.LockActiveDingTalkGrantForRouteParams{
		ConnectorID: binding.InstallationID,
		WorkspaceID: session.WorkspaceID,
	}); err != nil {
		return err
	}
	if target.ConversationType == convTypeP2P {
		if target.StaffID == "" {
			return errors.New("dingtalk outbound: direct route has no staff id")
		}
		_, err := q.LockDingTalkDirectOutboundRoute(ctx, db.LockDingTalkDirectOutboundRouteParams{
			MulticaUserID: userID,
			ConnectorID:   binding.InstallationID,
			ChannelUserID: target.StaffID,
			WorkspaceID:   session.WorkspaceID,
			AgentID:       session.AgentID,
		})
		return err
	}
	_, err := q.LockDingTalkGroupOutboundRoute(ctx, db.LockDingTalkGroupOutboundRouteParams{
		MulticaUserID:  userID,
		InstallationID: binding.InstallationID,
		ConversationID: target.ConversationID,
		WorkspaceID:    session.WorkspaceID,
		AgentID:        session.AgentID,
	})
	return err
}

// eventContent extracts the deliverable text from an EventChatDone payload
// (typed, or its map form after a serialization round trip) or an
// EventTaskFailed payload. Empty means stay silent.
//
// For task-failed the text mirrors the web transcript's failure chat_message:
// the broadcast's `error` field carries the same redacted failure text and is
// omitted while an auto-retry is pending (the retry attempt reports its own
// outcome), so error-present means deliverable.
func eventContent(e events.Event) string {
	switch p := e.Payload.(type) {
	case protocol.ChatDonePayload:
		return p.Content
	case map[string]any:
		if e.Type == protocol.EventTaskFailed {
			if retryPending, _ := p["retry_pending"].(bool); retryPending {
				return ""
			}
			if s, _ := p["error"].(string); s != "" {
				return "⚠️ " + s
			}
			return ""
		}
		if s, ok := p["content"].(string); ok {
			return s
		}
	}
	return ""
}

func taskAndSessionFromEvent(e events.Event) (taskID, sessionID pgtype.UUID, ok bool) {
	if e.TaskID != "" {
		_ = taskID.Scan(e.TaskID)
	}
	if e.ChatSessionID != "" {
		_ = sessionID.Scan(e.ChatSessionID)
	}
	switch p := e.Payload.(type) {
	case protocol.ChatDonePayload:
		if !taskID.Valid {
			_ = taskID.Scan(p.TaskID)
		}
		if !sessionID.Valid {
			_ = sessionID.Scan(p.ChatSessionID)
		}
	case map[string]any:
		if !taskID.Valid {
			if raw, _ := p["task_id"].(string); raw != "" {
				_ = taskID.Scan(raw)
			}
		}
		if !sessionID.Valid {
			if raw, _ := p["chat_session_id"].(string); raw != "" {
				_ = sessionID.Scan(raw)
			}
		}
	}
	return taskID, sessionID, taskID.Valid
}
