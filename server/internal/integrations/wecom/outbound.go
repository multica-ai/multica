package wecom

// outbound.go — the WeCom EventChatDone subscriber. After an agent finishes
// producing a chat reply on the bus, this subscriber looks up the wecom
// chat_session binding, resolves the live wsSender through the shared
// registry, and pushes the reply back as aibot_send_msg. Mirrors
// slack.Outbound; sessions with no wecom binding are ignored so it
// coexists with Slack / Lark subscribers on the shared bus.
//
// Kept lean: aibot has no threading, no per-bot outbound REST, and no
// mrkdwn conversion — the reply text goes through sendMsgTextBody the
// same way OutboundReplier's messages do (markdown msgtype, which
// renders plaintext without escaping).
//
// REPLICA TOPOLOGY: WeCom's only outbound path is the in-process WebSocket
// held in the sendersRegistry, but EventChatDone / EventInboxNew are
// dispatched on the in-process events.Bus, so the replica that publishes an
// event is not necessarily the one holding the bot's WS lease (Slack/Lark are
// immune — their outbound is stateless HTTP any replica can perform).
//
// With a sharded/dual realtime relay running, a reply or inbox push produced
// off-lease is forwarded to the lease holder over the relay
// (relay_outbound.go) and the single-replica constraint no longer applies to
// routing. Without a relay — legacy mode, or no REDIS_URL — the constraint
// stands: run the WeCom-enabled backend as a single replica. In every mode, a
// delivery produced while NO replica holds a live connection (all of them
// mid-reconnect) is still lost; that residual window is a durability problem
// the relay deliberately does not solve. Boot logs which of the two regimes is
// in effect. See router.go.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// outboundQueries is the slice of generated queries the WeCom outbound
// subscriber needs. *db.Queries satisfies it.
type outboundQueries interface {
	GetChannelTaskDelivery(ctx context.Context, taskID pgtype.UUID) (db.ChannelTaskDelivery, error)
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	FindChannelBindingForMember(ctx context.Context, arg db.FindChannelBindingForMemberParams) (db.ChannelUserBinding, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
	ListAttachmentsByChatMessage(ctx context.Context, arg db.ListAttachmentsByChatMessageParams) ([]db.Attachment, error)
}

// Outbound delivers an agent's chat reply back to WeCom over the same
// aibot WebSocket the inbound loop owns. Registered against the shared
// event bus; sessions with no wecom binding are silently ignored.
type Outbound struct {
	q       outboundQueries
	senders *sendersRegistry
	logger  *slog.Logger

	// objects is the deployment's object storage, or nil when there is none.
	// Non-nil is what turns file delivery on (outbound_media.go).
	objects mediaObjectStore

	// spawn runs an attachment delivery. A field rather than a bare `go` so a
	// test can run it inline and observe the result deterministically.
	spawn func(func())

	// metrics counts what happened to each reply. Nil discards; see
	// outbound_outcome.go for why the drop breakdown exists at all.
	metrics Metrics

	// relay routes a reply to the replica holding the bot's socket when this
	// one does not. Nil on a deployment with no Redis, where it is also
	// unnecessary: one replica publishes and holds the socket both.
	relay *RelayOutbound

	// Two counters bound attachment delivery, and they are two because one
	// cannot be in both places at once.
	//
	// admittedAttachments counts goroutines this subscriber has started and
	// not yet seen return. It is claimed before the spawn, so it bounds the
	// attachment lookup each goroutine runs as well as the goroutine itself.
	// Nothing is known about the turn at that point, so exceeding it can only
	// be logged.
	//
	// pendingAttachments counts deliveries that have looked the turn up and
	// found a file. It is claimed after the lookup, which is what lets a
	// delivery refused for want of capacity be reported to the user without
	// ever warning about a file that never existed.
	//
	// The admitted cap is deliberately the larger of the two, so that a
	// backlog of turns that DO carry a file fills the pending cap first and is
	// shed on the path that can say what was dropped. Reaching the admitted cap
	// does not imply the pending cap is full: admission is held for a
	// goroutine's whole life, including its lookup and including turns that
	// turn out to carry no file, and those never claim a pending slot at all.
	pendingMu           sync.Mutex
	pendingAttachments  int
	admittedAttachments int
}

// NewOutbound builds the WeCom outbound subscriber. senders is the same
// process-wide registry the wecom.ChannelDeps and OutboundReplier were
// built with — reply delivery goes through the live wsSender for the
// binding's installation, so a session whose Supervisor lost the lease
// mid-flight silently drops rather than opening a second connection.
//
// WithAttachments is the one option: pass the deployment's object storage and
// the files an agent produced are delivered into the chat behind the answer.
func NewOutbound(q outboundQueries, senders *sendersRegistry, logger *slog.Logger, opts ...OutboundOption) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	o := &Outbound{
		q:       q,
		senders: senders,
		logger:  logger,
		spawn:   func(f func()) { go f() },
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Register subscribes to the chat-done event on the bus.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
	// Inbox notifications delivered through the smart bot: when the
	// recipient member has a WeCom binding with a live connection, their
	// inbox:new items are pushed to the aibot as a markdown card.
	bus.Subscribe(protocol.EventInboxNew, o.handleInboxNew)
}

func (o *Outbound) handleEvent(e events.Event) {
	// Bus delivery is synchronous — a stuck WS write must not wedge the
	// publish call site. Fresh ctx with a tight timeout, same as Slack.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// One place records an undelivered reply, so a drop is counted exactly
	// once and always carries a reason. The branches inside processEvent that
	// end a turn without an error of their own record themselves and return
	// nil; everything that surfaces here is classified from the error.
	if err := o.processEvent(ctx, e); err != nil {
		if reason := unconfirmedReason(err); reason != "" {
			o.unconfirmed(ctx, e, reason, err)
		} else {
			o.dropped(ctx, e, classifyDrop(err), err)
		}
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	sessionID, err := util.ParseUUID(e.ChatSessionID)
	if err != nil || !sessionID.Valid {
		// Issue / autopilot tasks carry no chat_session.
		return nil
	}
	// An empty completion normally ends the turn here. It does not when the
	// agent produced a file and said nothing about it: the platform writes an
	// assistant message for exactly that case, and returning now would throw
	// the work away.
	content := chatDoneContent(e.Payload)
	if content == "" && !o.mayCarryAttachments(e) {
		o.skipped(ctx, e, skipNothingToSay)
		return nil
	}
	// Only bound, non-empty completions reach here, so classify the task
	// origin before loading credentials or sending. A question asked in the
	// Multica web UI can reuse a session that originated in WeCom — and its
	// answer belongs only in Multica. Without this gate that answer is pushed
	// into the WeCom chat, which in a group means in front of everyone in the
	// room. slack/outbound.go:118 and the lark and dingtalk equivalents all
	// gate here; WeCom was the one that did not.
	//
	// Fails closed: an origin we cannot establish is not delivered.
	//
	// Everything above this point is a read. Keep it that way: the gate has to
	// stay ahead of anything that consumes or mutates WeCom-side state for the
	// turn, because an answer that must not reach the room must not take over
	// the room's message either.
	taskID, ok := chatDoneTaskID(e)
	if !ok {
		o.dropped(ctx, e, dropTaskMissing, nil)
		return nil
	}
	delivery, err := o.q.GetChannelTaskDelivery(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("wecom: lookup task delivery: %w", err)
	}
	if delivery.ChannelType != channelTypeWecom {
		return nil
	}
	binding := wecomBindingFromTaskDelivery(delivery)
	task, err := o.q.GetAgentTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Cancelled and deleted while its completion was in flight.
			o.dropped(ctx, e, dropTaskMissing, nil)
			return nil
		}
		return fmt.Errorf("wecom: load agent task: %w", err)
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, o.q, task)
	if err != nil {
		return fmt.Errorf("wecom: classify task input origin: %w", err)
	}
	if !deliver {
		o.skipped(ctx, e, skipOriginNotChannel)
		return nil
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		return fmt.Errorf("wecom: load installation: %w", err)
	}
	if inst.Status != string(InstallationActive) {
		o.skipped(ctx, e, skipInstallationInactive) // revoked between trigger and reply
		return nil
	}
	if o.senders == nil {
		return errors.New("wecom: sender registry not configured")
	}
	chatType := aibotChatTypeFromChannel(channel.ChatType(binding.ChatType))
	sender := o.senders.get(inst.ID)
	if sender == nil {
		// Before giving up: this reply may simply have been produced on the
		// wrong replica. Hand it to the one holding the socket.
		//
		// Counted by the replica that delivers it, not here — so a reply that
		// is routed and then delivered appears once, on the sender's side.
		// A reply routed while EVERY replica is mid-reconnect is read by
		// nobody and counted by nobody; that window is the durability problem
		// this deliberately does not solve (relay_outbound.go).
		if o.relay.publish(relayFrame{
			Kind:           relayKindReply,
			InstallationID: util.UUIDToString(inst.ID),
			ChatID:         binding.ChannelChatID,
			ChatType:       chatType,
			Content:        content,
			TaskID:         util.UUIDToString(taskID),
			MessageID:      chatDoneMessageID(e.Payload),
			WorkspaceID:    e.WorkspaceID,
			SessionID:      e.ChatSessionID,
			CarriesFiles:   o.mayCarryAttachments(e),
		}, relayEventID(e, taskID)) {
			o.logger.DebugContext(ctx, "wecom outbound: routed to the replica holding the socket",
				"installation_id", util.UUIDToString(inst.ID), "chat_session_id", e.ChatSessionID)
			return nil
		}
		// No live WS for this installation on this replica. Two causes:
		// (1) the Supervisor lost the lease or is mid-reconnect — transient,
		// and the user's next inbound message reaches the reconnected loop;
		// (2) on a multi-replica deployment the lease is held by a DIFFERENT
		// replica than the one that published this event, so it can never be
		// delivered from here (see the single-replica constraint in this
		// file's header). Either way, buffering is wrong — the reply is stale
		// by the time a socket returns — so we surface it to the caller's WARN
		// rather than drop it silently.
		return errNoLiveConnection
	}
	// Words first. An empty completion reaches here only because a file is
	// bound to it, and an empty markdown bubble ahead of that file would be
	// noise the user has to scroll past.
	if content != "" {
		if err := sender.sendTextCtx(ctx, binding.ChannelChatID, chatType, content); err != nil {
			return err
		}
		o.delivered()
	}
	// Then whatever the agent produced alongside them, as its own message — a
	// WeCom reply cannot carry a file inline.
	// carriesTheReply is true when the files ARE the answer: an empty
	// completion reached here only because one is bound to it, so nothing has
	// been counted for this reply yet and the attachment path owes its outcome.
	o.deliverAttachments(e, attachmentTarget{
		InstallationID: binding.InstallationID,
		ChatID:         binding.ChannelChatID,
		ChatType:       chatType,
		SessionID:      e.ChatSessionID,
	}, content == "")
	return nil
}

func wecomBindingFromTaskDelivery(delivery db.ChannelTaskDelivery) db.ChannelChatSessionBinding {
	return db.ChannelChatSessionBinding{
		ID: delivery.BindingID, InstallationID: delivery.InstallationID,
		ChannelType: delivery.ChannelType, ChannelChatID: delivery.ChannelChatID,
		ChatType:      delivery.ChatType,
		LastMessageID: delivery.ChannelMessageID, LastThreadID: delivery.ChannelThreadID,
		RouteRevision: delivery.RouteRevision, Config: delivery.Config,
	}
}

// chatDoneTaskID recovers the task id an EventChatDone belongs to. The
// envelope's TaskID is preferred, with the payload as the fallback —
// service.broadcastChatDone sets ChatDonePayload.TaskID and leaves the
// envelope's empty, so in practice the fallback is the live path.
func chatDoneTaskID(e events.Event) (pgtype.UUID, bool) {
	raw := e.TaskID
	if raw == "" {
		switch p := e.Payload.(type) {
		case protocol.ChatDonePayload:
			raw = p.TaskID
		case map[string]any:
			raw, _ = p["task_id"].(string)
		}
	}
	id, err := util.ParseUUID(raw)
	return id, err == nil && id.Valid
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

// handleInboxNew is the inbox:new subscriber that delivers a member
// notification via the smart bot. When the recipient member has a WeCom
// binding with a live connection, the notification is pushed to the aibot.
// On any miss — non-member recipient, no wecom binding, no live sender,
// send failure — the handler is a no-op and the member simply receives the
// notification through the in-app inbox as usual.
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
	if recipientIDStr == "" || workspaceIDStr == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	o.tryDeliverInbox(ctx, item, recipientIDStr, workspaceIDStr)
}

// tryDeliverInbox is the delivery core. Returns true iff the bot pushed
// the notification.
func (o *Outbound) tryDeliverInbox(ctx context.Context, item map[string]any, recipientIDStr, workspaceIDStr string) bool {
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
	if o.senders == nil {
		return false
	}
	sender := o.senders.get(binding.InstallationID)

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
	// Smart-bot inbox notifications are 1:1 pushes to the bound user. The
	// binding row's channel_user_id is the bot-scoped T-* userid — WeCom
	// treats that as the chatid for a single (chat_type=1) send.
	if sender == nil {
		// No socket here. Same shape as the reply path: hand it to the replica
		// that holds one. An inbox push is as user-visible as an answer, and
		// leaving it local was the reason the single-replica constraint had to
		// stay even with replies routed.
		if o.relay.publish(relayFrame{
			Kind:           relayKindInbox,
			InstallationID: util.UUIDToString(binding.InstallationID),
			ChatID:         binding.ChannelUserID,
			ChatType:       chatTypeSingleInt,
			Content:        content,
		}, relayInboxEventID(itemIDOf(item), recipientIDStr)) {
			o.logger.DebugContext(ctx, "wecom outbound: routed an inbox push to the replica holding the socket",
				"installation_id", uuidStringPub(binding.InstallationID))
			return true
		}
		// Logged, not counted on the reply counters. Their documented unit is
		// AGENT REPLIES, and an inbox notification recorded there would show up
		// as a reply this adapter owed somebody and failed to deliver — the
		// same unit error the relayed-inbox path in deliverRelayed already
		// avoids, and the reason the delivered/dropped ratio can be read as an
		// outcome at all. The member still receives this in the in-app inbox,
		// which is what makes a missed bot push a degradation rather than a
		// loss.
		o.logger.WarnContext(ctx, "wecom outbound: inbox push not delivered and not routable",
			"installation_id", uuidStringPub(binding.InstallationID),
			"recipient_id", recipientIDStr)
		return false // supervisor down or reconnecting — no live connection
	}
	if err := sender.sendTextCtx(ctx, binding.ChannelUserID, chatTypeSingleInt, content); err != nil {
		o.logger.WarnContext(ctx, "wecom outbound: inbox push failed",
			"error", err, "installation_id", uuidStringPub(binding.InstallationID),
			"recipient_id", recipientIDStr)
		return false // send failed → no bot delivery
	}
	o.logger.DebugContext(ctx, "wecom outbound: inbox delivered via bot",
		"installation_id", uuidStringPub(binding.InstallationID),
		"recipient_id", recipientIDStr,
		"inbox_type", item["type"])
	return true
}

// uuidStringPub renders a pgtype.UUID for a log line without depending on
// engine.uuidString (a different package).
func uuidStringPub(u pgtype.UUID) string {
	return util.UUIDToString(u)
}
