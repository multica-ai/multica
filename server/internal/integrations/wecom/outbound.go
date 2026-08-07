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
// SINGLE-REPLICA CONSTRAINT: WeCom's only outbound path is the in-process
// WebSocket held in the sendersRegistry, but EventChatDone / EventInboxNew are
// dispatched on the in-process events.Bus. On a multi-replica deployment the
// replica that publishes the event is not necessarily the one holding the
// bot's WS lease, so senders.get() returns nil and the reply cannot be
// delivered from here (Slack/Lark are immune — their outbound is stateless
// HTTP any replica can perform). Until outbound is routed to the lease holder,
// a WeCom-enabled backend must run as a single replica; boot emits a warning
// when a multi-replica setup (REDIS_URL) is detected. See router.go.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	// GetAgentTask serves two readers on this path. The origin gate reads the
	// row to get at the channel_ingested stamp; the round matcher reads it to
	// resolve an auto-retry clone back to the turn that owns its input batch,
	// which is the id the round was bound under.
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	FindChannelBindingForMember(ctx context.Context, arg db.FindChannelBindingForMemberParams) (db.ChannelUserBinding, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
}

// Outbound delivers an agent's chat reply back to WeCom over the same
// aibot WebSocket the inbound loop owns. Registered against the shared
// event bus; sessions with no wecom binding are silently ignored.
type Outbound struct {
	q       outboundQueries
	tasks   taskLookup
	senders *sendersRegistry
	streams *streamStore
	logger  *slog.Logger
}

// NewOutbound builds the WeCom outbound subscriber. senders is the same
// process-wide registry the wecom.ChannelDeps and OutboundReplier were
// built with — reply delivery goes through the live wsSender for the
// binding's installation, so a session whose Supervisor lost the lease
// mid-flight silently drops rather than opening a second connection.
//
// streams is the same store the typing indicator writes to; nil disables the
// in-place reply and leaves every answer going out as a new message.
func NewOutbound(q outboundQueries, senders *sendersRegistry, streams *streamStore, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	return &Outbound{q: q, tasks: q, senders: senders, streams: streams, logger: logger}
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
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "wecom outbound: reply delivery failed",
			"error", err, "chat_session_id", e.ChatSessionID)
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	sessionID, err := util.ParseUUID(e.ChatSessionID)
	if err != nil || !sessionID.Valid {
		// Issue / autopilot tasks carry no chat_session.
		return nil
	}
	content := chatDoneContent(e.Payload)

	// Where was this question asked? A question asked in the Multica web UI can
	// reuse a session that originated in WeCom — and its answer belongs only in
	// Multica. Without this gate that answer is pushed into the WeCom chat,
	// which in a group means in front of everyone in the room.
	// slack/outbound.go:118 and the lark and dingtalk equivalents all gate
	// here; WeCom was the one that did not.
	//
	// Fails closed: an origin we cannot establish is not delivered.
	//
	// Asked BEFORE the bubble is taken, and before the empty-completion return
	// below. Taking the bubble consumes it, and sealing a WeCom round's bubble
	// with a web run's ending costs the asker in the room the answer that
	// bubble was opened for — with nothing said about why, because the ending
	// they get is the web run's. An answer that must not reach the room must
	// not take over the room's message either. The failure notice orders its
	// own gate the same way, and for the same reason — see
	// failureBelongsOnWecom in typing_indicator.go.
	//
	// The cost of asking this early is two reads on a chat:done that turns out
	// to belong to another adapter, which used to be refused one query later by
	// the binding lookup. That lookup cannot come first any more: it lives past
	// the bubble take now, and nothing may precede the take except this.
	taskID, ok := chatDoneTaskID(e)
	if !ok {
		return nil
	}
	task, err := o.q.GetAgentTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Cancelled and deleted while its completion was in flight.
			return nil
		}
		return fmt.Errorf("wecom: load agent task: %w", err)
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, o.q, task)
	if err != nil {
		return fmt.Errorf("wecom: classify task input origin: %w", err)
	}
	if !deliver {
		return nil
	}

	// The bubble this run's round opened, if there is one. Taken up front and
	// unconditionally: from here on this turn owns it, and a handle left in
	// the store after the turn ends is a handle pointing at a sealed message.
	if handle, streaming := o.takeStream(ctx, sessionID, e); streaming {
		// A bubble on screen has to end in words. An empty completion is a
		// legitimate outcome — the agent had nothing to add — but an endless
		// spinner is not, so the copy stands in for the silence. For a round
		// that waited in line behind another, the silence has a better
		// explanation: the reply ahead of it already covered this message.
		text := content
		if !hasVisibleChar(text) {
			text = streamCopyNoReply
			if handle.QueuedBehind {
				text = streamCopyMerged
			}
		}
		if err := o.finishStream(ctx, handle, text); err == nil {
			return nil
		}
		// The frame was refused. Say it as a new message instead — and never
		// re-send the stream frame itself, whose req_id will have expired long
		// before another connection could carry it.
		content = text
	}
	if content == "" {
		return nil // nothing to say, no bubble to close, nothing to send
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
	if o.senders == nil {
		return errors.New("wecom: sender registry not configured")
	}
	sender := o.senders.get(inst.ID)
	if sender == nil {
		// No live WS for this installation on this replica. Two causes:
		// (1) the Supervisor lost the lease or is mid-reconnect — transient,
		// and the user's next inbound message reaches the reconnected loop;
		// (2) on a multi-replica deployment the lease is held by a DIFFERENT
		// replica than the one that published this event, so it can never be
		// delivered from here (see the single-replica constraint in this
		// file's header). Either way, buffering is wrong — the reply is stale
		// by the time a socket returns — so we surface it to the caller's WARN
		// rather than drop it silently.
		return errors.New("wecom: connection not ready on this replica")
	}
	chatType := aibotChatTypeFromChannel(channel.ChatType(binding.ChatType))
	return sender.sendTextCtx(ctx, binding.ChannelChatID, chatType, content)
}

// chatDoneTaskID recovers the task id an EventChatDone belongs to, as the row
// key the origin gate needs.
//
// It reads through taskIDFromEvent rather than repeating the extraction,
// because the gate and the bubble take have to be talking about the same run:
// two rules that disagree would let the gate clear task A while the take
// consumes the round bound to task B, which is the ordering bug with an extra
// step in it. taskIDFromEvent is where that rule lives — the envelope's TaskID
// first, then the payload, since service.broadcastChatDone sets
// ChatDonePayload.TaskID and leaves the envelope's empty.
func chatDoneTaskID(e events.Event) (pgtype.UUID, bool) {
	id, err := util.ParseUUID(taskIDFromEvent(e))
	return id, err == nil && id.Valid
}

// takeStream claims the bubble this run's round opened, so the answer can
// replace it in place. The task id picks it — the one the debounced flush
// bound to the round when it created the run — so a session with several
// rounds open never has to guess which bubble an answer belongs in, and an
// auto-retry's answer still finds the round its first attempt opened.
func (o *Outbound) takeStream(ctx context.Context, sessionID pgtype.UUID, e events.Event) (streamHandle, bool) {
	if o.streams == nil || o.senders == nil {
		return streamHandle{}, false
	}
	taker := roundTaker{streams: o.streams, tasks: o.tasks, log: o.logger}
	return taker.take(ctx, sessionID, taskIDFromEvent(e), roundOver)
}

// finishStream writes the answer into the bubble and seals it. A failure here
// is not fatal to the reply — it means the caller falls back to a new message —
// so it is logged with the one detail that explains it: whether the stream is
// beyond saving (past its window, bad req_id) or the socket simply blinked.
func (o *Outbound) finishStream(ctx context.Context, h streamHandle, text string) error {
	err := o.senders.stream(ctx, h, text, true)
	if err == nil {
		return nil
	}
	o.logger.WarnContext(ctx, "wecom outbound: in-place reply failed, sending a new message instead",
		"installation_id", uuidStringPub(h.InstallationID),
		"stream_unusable", streamUnusable(err), "error", err)
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
	if sender == nil {
		return false // supervisor down or reconnecting — no live connection
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
	// Smart-bot inbox notifications are 1:1 pushes to the bound user. The
	// binding row's channel_user_id is the bot-scoped T-* userid — WeCom
	// treats that as the chatid for a single (chat_type=1) send.
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
