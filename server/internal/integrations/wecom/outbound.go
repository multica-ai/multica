package wecom

// outbound.go — the WeCom EventChatDone / EventInboxNew subscriber. It runs on
// whichever replica published the event and does not try to write to WeCom at
// all: it enqueues onto channel_outbound_queue and nudges the local consumer.
// The replica holding the bot's WebSocket lease drains the row (see
// outbox_sender.go).
//
// That indirection is the whole point. aibot has no outbound REST path, so a
// reply can only leave the process that holds the socket — but the bus is
// in-process, so the publishing replica is frequently not that one. Pushing
// straight to the sendersRegistry from here therefore dropped every reply
// produced off-lease, which is why this adapter previously carried a
// single-replica constraint. Enqueueing instead makes the handoff durable and
// replica-agnostic.
//
// Sessions with no wecom binding are ignored so this coexists with the Slack /
// Lark subscribers on the shared bus.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/outbox"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// outboundQueries is the slice of generated queries the WeCom outbound
// subscriber needs. *db.Queries satisfies it.
type outboundQueries interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	FindChannelBindingForMember(ctx context.Context, arg db.FindChannelBindingForMemberParams) (db.ChannelUserBinding, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
}

// Outbound enqueues agent replies and inbox notifications for delivery to
// WeCom. Registered against the shared event bus.
type Outbound struct {
	q        outboundQueries
	producer *outbox.Producer
	logger   *slog.Logger
}

// NewOutbound builds the WeCom outbound subscriber over the shared outbound
// queue producer.
func NewOutbound(q outboundQueries, producer *outbox.Producer, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	return &Outbound{q: q, producer: producer, logger: logger}
}

// Register subscribes to the chat-done and inbox events on the bus.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
	// Inbox notifications delivered through the smart bot: when the recipient
	// member has a WeCom binding, their inbox:new items are enqueued as a
	// markdown card addressed to their 1:1 chat with the bot.
	bus.Subscribe(protocol.EventInboxNew, o.handleInboxNew)
}

func (o *Outbound) handleEvent(e events.Event) {
	// Bus delivery is synchronous — a slow INSERT must not wedge the publish
	// call site. Fresh ctx with a tight timeout, same as Slack.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "wecom outbound: enqueue reply failed",
			"error", err, "chat_session_id", e.ChatSessionID)
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	if o.producer == nil {
		return errors.New("wecom: outbound producer not configured")
	}
	sessionID, err := util.ParseUUID(e.ChatSessionID)
	if err != nil || !sessionID.Valid {
		// Issue / autopilot tasks carry no chat_session.
		return nil
	}
	taskID, err := util.ParseUUID(chatDoneTaskID(e))
	if err != nil || !taskID.Valid {
		// Without a task id there is no stable business key, and enqueueing
		// under a synthetic one would let a redelivered event send the same
		// reply twice. The reconciler covers the task-derived case anyway.
		return nil
	}
	binding, err := o.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   channelTypeWecom,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // not a wecom session (Slack / Lark / web-only)
		}
		return fmt.Errorf("wecom: lookup chat binding: %w", err)
	}
	content := chatDoneContent(e.Payload)
	if content == "" {
		return nil // nothing to say (empty completion)
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		return fmt.Errorf("wecom: load installation: %w", err)
	}
	if inst.Status != string(InstallationActive) {
		return nil // revoked between trigger and reply
	}

	payload, err := json.Marshal(outboundPayload{Content: content})
	if err != nil {
		return fmt.Errorf("wecom: marshal chat_done payload: %w", err)
	}
	_, err = o.producer.Enqueue(ctx, outbox.Request{
		InstallationID: inst.ID,
		WorkspaceID:    inst.WorkspaceID,
		ChatSessionID:  binding.ChatSessionID,
		SourceKind:     sourceKindChatDone,
		SourceID:       util.UUIDToString(taskID),
		TargetChatID:   binding.ChannelChatID,
		TargetChatType: int16(aibotChatTypeFromChannel(channel.ChatType(binding.ChatType))),
		MsgType:        msgTypeMarkdown,
		Payload:        payload,
	}, outbox.EnqueuePathRealtime)
	return err
}

// chatDoneContent extracts the reply text from an EventChatDone payload
// (the typed payload, or its map form after a serialization round trip).
func chatDoneContent(payload any) string {
	switch p := payload.(type) {
	case protocol.ChatDonePayload:
		return p.Content
	case map[string]any:
		if s, ok := p["content"].(string); ok {
			return s
		}
	}
	return ""
}

// chatDoneTaskID extracts the task id that becomes the queue row's business
// key.
//
// It reads the PAYLOAD first, not the envelope: service.broadcastChatDone
// leaves events.Event.TaskID empty and carries the id in ChatDonePayload, so
// keying off the envelope alone would silently skip every realtime enqueue and
// leave the reconciler — which lags on purpose — as the only delivery path. The
// envelope is still consulted as a fallback in case a future publisher sets it.
func chatDoneTaskID(e events.Event) string {
	switch p := e.Payload.(type) {
	case protocol.ChatDonePayload:
		if p.TaskID != "" {
			return p.TaskID
		}
	case map[string]any:
		if s, ok := p["task_id"].(string); ok && s != "" {
			return s
		}
	}
	return e.TaskID
}

// handleInboxNew is the inbox:new subscriber that enqueues a member
// notification for delivery via the smart bot. On any miss — non-member
// recipient, no wecom binding, nothing renderable — the handler is a no-op and
// the member simply receives the notification through the in-app inbox as
// usual.
func (o *Outbound) handleInboxNew(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return
	}
	// Only member recipients — agents receive nothing via chat channels.
	if rt, _ := item["recipient_type"].(string); rt != "member" {
		return
	}
	recipientIDStr, _ := item["recipient_id"].(string)
	workspaceIDStr, _ := item["workspace_id"].(string)
	itemIDStr, _ := item["id"].(string)
	if recipientIDStr == "" || workspaceIDStr == "" || itemIDStr == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	o.tryEnqueueInbox(ctx, item, itemIDStr, recipientIDStr, workspaceIDStr)
}

// tryEnqueueInbox is the enqueue core. Returns true iff a row was written.
func (o *Outbound) tryEnqueueInbox(ctx context.Context, item map[string]any, itemIDStr, recipientIDStr, workspaceIDStr string) bool {
	if o.producer == nil {
		return false
	}
	recipientID, err := util.ParseUUID(recipientIDStr)
	if err != nil || !recipientID.Valid {
		return false
	}
	workspaceID, err := util.ParseUUID(workspaceIDStr)
	if err != nil || !workspaceID.Valid {
		return false
	}
	binding, err := o.q.FindChannelBindingForMember(ctx, db.FindChannelBindingForMemberParams{
		WorkspaceID:   workspaceID,
		MulticaUserID: recipientID,
		ChannelType:   channelTypeWecom,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			o.logger.WarnContext(ctx, "wecom outbound: lookup member binding failed",
				"error", err, "workspace_id", workspaceIDStr, "recipient_id", recipientIDStr)
		}
		return false // no binding → nothing to deliver via bot
	}

	// Resolve slug for the link. Best-effort — a missing slug just falls
	// back to the workspace UUID in the URL.
	slug := ""
	if ws, err := o.q.GetWorkspace(ctx, workspaceID); err == nil {
		slug = ws.Slug
	}
	content := buildInboxMarkdown(item, workspaceIDStr, slug)
	if content == "" {
		return false
	}
	payload, err := json.Marshal(outboundPayload{Content: content})
	if err != nil {
		return false
	}

	// Smart-bot inbox notifications are 1:1 pushes to the bound user. The
	// binding row's channel_user_id is the bot-scoped T-* userid — WeCom
	// treats that as the chatid for a single (chat_type=1) send.
	//
	// The business key is the inbox item id, so a redelivered inbox:new event
	// cannot notify the same member about the same item twice.
	inserted, err := o.producer.Enqueue(ctx, outbox.Request{
		InstallationID: binding.InstallationID,
		WorkspaceID:    workspaceID,
		SourceKind:     sourceKindInboxNotify,
		SourceID:       itemIDStr,
		TargetChatID:   binding.ChannelUserID,
		TargetChatType: int16(chatTypeSingleInt),
		MsgType:        msgTypeMarkdown,
		Payload:        payload,
	}, outbox.EnqueuePathRealtime)
	if err != nil {
		o.logger.WarnContext(ctx, "wecom outbound: enqueue inbox push failed",
			"error", err, "installation_id", util.UUIDToString(binding.InstallationID),
			"recipient_id", recipientIDStr)
		return false
	}
	if inserted {
		o.logger.DebugContext(ctx, "wecom outbound: inbox notification enqueued",
			"installation_id", util.UUIDToString(binding.InstallationID),
			"recipient_id", recipientIDStr,
			"inbox_type", item["type"])
	}
	return inserted
}
