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
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Outbound delivers an agent's chat reply back to DingTalk — the outbound half
// of the round trip. On EventChatDone / EventTaskFailed
// it finds the DingTalk chat binding for the task's session and posts the reply
// (or failure notice) into the originating conversation. Sessions with no
// DingTalk binding are ignored, so it coexists with the Feishu and Slack
// subscribers on the shared event bus. Registered only when DingTalk is
// configured.
type Outbound struct {
	q       *db.Queries
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
	return &Outbound{q: q, tx: tx, decrypt: decrypt, client: client, logger: logger}
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
	if o.q == nil || o.tx == nil {
		return errors.New("dingtalk outbound: database is not configured")
	}
	tx, err := o.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin dingtalk outbound target fence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := o.q.WithTx(tx)

	binding, err := q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   string(TypeDingTalk),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // not a DingTalk session (Feishu / Slack / web-only)
		}
		return fmt.Errorf("lookup dingtalk chat binding: %w", err)
	}
	task, err := q.GetAgentTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load agent task: %w", err)
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, q, task)
	if err != nil {
		return fmt.Errorf("classify task input origin: %w", err)
	}
	if !deliver {
		return nil
	}
	session, err := q.GetChatSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load dingtalk chat session: %w", err)
	}
	routing := bindingRouting(binding)
	targetID := task.AgentID
	if routing.TargetID != "" {
		targetID, err = util.ParseUUID(routing.TargetID)
		if err != nil {
			return fmt.Errorf("decode dingtalk binding target: %w", err)
		}
	}
	targetType := engine.TargetType(routing.TargetType)
	if targetType != engine.TargetAgent && targetType != engine.TargetSquad {
		return fmt.Errorf("decode dingtalk binding target: unsupported target type %q", routing.TargetType)
	}

	if routing.ConversationType == convTypeP2P {
		_, err = q.LockDingTalkDirectOutboundTarget(ctx, db.LockDingTalkDirectOutboundTargetParams{
			InstallationID: binding.InstallationID,
			WorkspaceID:    session.WorkspaceID,
			TargetType:     string(targetType),
			TargetID:       targetID,
			TargetRevision: routing.TargetRevision,
			AgentID:        task.AgentID,
		})
	} else {
		_, err = q.LockDingTalkGroupOutboundTarget(ctx, db.LockDingTalkGroupOutboundTargetParams{
			InstallationID: binding.InstallationID,
			WorkspaceID:    session.WorkspaceID,
			ConversationID: routing.ConversationID,
			TargetType:     string(targetType),
			TargetID:       targetID,
			TargetRevision: routing.TargetRevision,
			AgentID:        task.AgentID,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // target, Leader, route, or installation changed after enqueue
	}
	if err != nil {
		return fmt.Errorf("lock dingtalk outbound target: %w", err)
	}

	// A route reassignment deletes the binding while holding the same route
	// lock. Re-read it after acquiring the fence so a stale lookup can never
	// deliver through a newly assigned conversation.
	currentBinding, err := q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   string(TypeDingTalk),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recheck dingtalk chat binding: %w", err)
	}
	if currentBinding.ID != binding.ID || currentBinding.InstallationID != binding.InstallationID {
		return nil
	}

	inst, err := q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeDingTalk),
	})
	if err != nil {
		return fmt.Errorf("load dingtalk installation: %w", err)
	}
	if inst.Status != "active" {
		return nil // revoked between trigger and reply
	}
	creds, err := decodeCredentials(inst.Config, o.decrypt)
	if err != nil {
		return fmt.Errorf("decode dingtalk credentials: %w", err)
	}
	s := &sender{client: o.client, robotCode: creds.RobotCode, appKey: creds.AppKey, appSecret: creds.AppSecret}
	if _, err := s.send(ctx, outboundTarget(binding), content); err != nil {
		return fmt.Errorf("post dingtalk reply: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit dingtalk outbound target fence: %w", err)
	}
	return nil
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
