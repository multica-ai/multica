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
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// Outbound delivers an agent's chat reply back to DingTalk — the outbound half
// of the round trip. On EventChatDone / EventTaskFailed it finds the DingTalk
// chat binding for the task's session and posts the reply (or failure notice)
// into the originating conversation. Sessions with no DingTalk binding are
// ignored, so it coexists with the Feishu and Slack subscribers on the shared
// event bus. Registered only when DingTalk is configured.
//
// On EventTaskCancelled it posts nothing through the binding: it withdraws the
// processing ack instead, through the notifier that made it. See
// withdrawProcessingAck.
type Outbound struct {
	q       outboundQueries
	decrypt Decrypter
	client  *Client
	ack     *ackNotifier
	logger  *slog.Logger
}

// NewOutbound builds the DingTalk outbound subscriber over the generated queries,
// the AppSecret decrypter, the shared token-caching Client, and the ack notifier
// the inbound side posts the processing message through. It must be the same
// notifier instance the resolver set was built with — that is the only record of
// which conversations are still owed a reply. A nil notifier leaves cancelled
// runs unannounced.
func NewOutbound(q outboundQueries, decrypt Decrypter, client *Client, ack *ackNotifier, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = NewClient(nil, "")
	}
	return &Outbound{q: q, decrypt: decrypt, client: client, ack: ack, logger: logger}
}

// Register subscribes to the three events that end a run. Task-failed and
// task-cancelled keep the DingTalk conversation consistent with the web
// transcript — without them the run ends in silence and the user is left
// staring at the "👀 On it" ack forever.
//
// DingTalk is the odd one out among the channel adapters. Slack and Lark put a
// reaction on the user's own message and take it off again; the classic robot
// API this adapter sends through exposes no reaction, so ack.go's indicator is
// a real, non-retractable message that promised a reply. Closing it can only
// mean posting a second message withdrawing that promise. A badge we remove was
// ours to remove; a message we post is not, so the cancel path posts only where
// an ack is outstanding.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, o.handleEvent)
	bus.Subscribe(protocol.EventTaskCancelled, o.handleEvent)
}

func (o *Outbound) handleEvent(e events.Event) {
	// Bus delivery is synchronous, so a stuck DingTalk HTTP call must not wedge
	// the publish call site: use a fresh ctx with a tight timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "dingtalk outbound: delivery failed",
			"error", err, "chat_session_id", e.ChatSessionID)
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	taskID, sessionID, ok := taskAndSessionFromEvent(e)
	if !ok || !sessionID.Valid {
		// Issue / autopilot tasks carry no chat_session.
		return nil
	}
	if e.Type == protocol.EventTaskCancelled {
		return o.withdrawProcessingAck(ctx, sessionID)
	}
	content := eventContent(e)
	if content == "" {
		return nil // nothing to say (empty completion, or a retry-pending failure)
	}
	binding, err := o.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   string(TypeDingTalk),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // not a DingTalk session (Feishu / Slack / web-only)
		}
		return fmt.Errorf("lookup dingtalk chat binding: %w", err)
	}
	task, err := o.q.GetAgentTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load agent task: %w", err)
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, o.q, task)
	if err != nil {
		return fmt.Errorf("classify task input origin: %w", err)
	}
	if !deliver {
		return nil
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
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
	// The ack promised a reply in this conversation and one has now landed, so
	// a later cancel on the session must not withdraw a promise already kept.
	if o.ack != nil {
		o.ack.takeOutstandingAck(sessionID)
	}
	return nil
}

// withdrawProcessingAck closes the processing ack for a cancelled run.
//
// What decides whether anything is posted is whether an ack is outstanding for
// the session, not what the cancelled task was. Deriving it from the task is
// what #6611's production case broke on: a channel task can own an input batch
// holding no channel-ingested rows, and the provenance query then reports it as
// a web run, so the room that is holding "👀 On it" hears nothing. An ack, by
// contrast, is only ever posted by the inbound path, into a real conversation —
// if one is outstanding, a message is owed there and nowhere else. A run started
// in the browser never made a promise on DingTalk, so its cancel stays silent
// without needing a gate at all.
//
// Taking the promise is what keeps a bulk cancel to one message: the event
// arrives once per cancelled task row, and the promise is gone after the first.
// It is taken before the send rather than after, so a failed send does not
// re-open it — this is a non-retractable message, and posting the notice twice
// is worse than not posting it. The failure is logged by the caller.
//
// The installation comes from the promise instead of the session's binding
// because the binding may already be gone: deleting a chat session drops it in
// the same transaction that cancels the session's tasks, and the cancels are
// broadcast after that transaction commits.
func (o *Outbound) withdrawProcessingAck(ctx context.Context, sessionID pgtype.UUID) error {
	if o.ack == nil {
		return nil
	}
	promise, ok := o.ack.takeOutstandingAck(sessionID)
	if !ok {
		return nil // no unanswered promise in this conversation
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          promise.installationID,
		ChannelType: string(TypeDingTalk),
	})
	if err != nil {
		return fmt.Errorf("load dingtalk installation: %w", err)
	}
	if inst.Status != "active" {
		return nil // revoked between trigger and cancel
	}
	creds, err := decodeCredentials(inst.Config, o.decrypt)
	if err != nil {
		return fmt.Errorf("decode dingtalk credentials: %w", err)
	}
	s := &sender{client: o.client, robotCode: creds.RobotCode, appKey: creds.AppKey, appSecret: creds.AppSecret}
	if _, err := s.send(ctx, promise.target, ackCancelledText); err != nil {
		return fmt.Errorf("post dingtalk cancellation notice: %w", err)
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
//
// EventTaskCancelled never reaches here: it carries no text of its own —
// broadcastTaskEvent publishes the task row's ids and status and nothing else —
// and its message is the fixed withdrawal in withdrawProcessingAck.
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
