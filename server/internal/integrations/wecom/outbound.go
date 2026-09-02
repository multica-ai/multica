package wecom

// outbound.go — the WeCom EventChatDone / EventInboxNew subscriber. An agent's
// answer leaves this process the way every other WeCom write does: over the
// aibot WebSocket held in the sendersRegistry. aibot has no outbound REST path,
// so there is nothing else to fall back on.
//
// Two shapes of write, tried in this order. A round that opened a bubble when
// the question arrived has its answer written INTO that bubble and the bubble
// sealed — that is the whole point of the streaming reply, and it is why an
// empty completion still has to reach this far: a spinner nobody closes is
// worse than a short answer (stream_store.go, typing_indicator.go). A round
// with no bubble left — a restart mid-run, a stream past its window, a frame
// the server refused — gets an ordinary message addressed by the task's own
// delivery row.
//
// REPLICA TOPOLOGY: EventChatDone / EventInboxNew are dispatched on the
// in-process events.Bus, so the replica that publishes an event is not
// necessarily the one holding the bot's WS lease (Slack/Lark are immune —
// their outbound is stateless HTTP any replica can perform). With a
// sharded/dual realtime relay running, a reply or inbox push produced
// off-lease is forwarded to the lease holder over the relay
// (relay_outbound.go) and the single-replica constraint no longer applies to
// routing. Without a relay — legacy mode, or no REDIS_URL — the constraint
// stands: run the WeCom-enabled backend as a single replica. In every mode, a
// delivery produced while NO replica holds a live connection (all of them
// mid-reconnect) is still lost; that residual window is a durability problem
// the relay deliberately does not solve. Boot logs which of the two regimes is
// in effect. See router.go.
//
// The bubble path and the relay never contend for the same turn. A bubble is
// writable only on the replica that painted it, which is the replica holding
// the socket; a round whose bubble lives elsewhere finds none here and takes
// the addressed path, which is where the relay is.
//
// They never MEET either, and that is a known gap rather than a property of the
// topology. The replica that takes a relayed reply is by definition the one
// holding the socket, which is the one with the bubble — and deliverRelayed
// pushes the words as an ordinary message without ever looking for it
// (relay_outbound.go). So on a multi-replica deployment the answer arrives, the
// bubble it belonged in stays open, and the guard keeps rotating it until the
// run's last stream runs out its window. Replies are delivered either way; the
// in-place bubble is a single-replica experience until a relayed reply is
// routed through the round store on the replica that takes it.
//
// Sessions with no wecom binding are ignored so this coexists with the Slack /
// Lark subscribers on the shared bus.

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
	// GetChannelTaskDelivery is the route this turn was admitted on, stamped
	// when the question was ingested. Read by task rather than by session
	// because /new and /clear re-point a session at a different binding: an
	// answer still in flight across one of those belongs to the room that
	// asked, not to whatever the session points at by the time it lands.
	GetChannelTaskDelivery(ctx context.Context, taskID pgtype.UUID) (db.ChannelTaskDelivery, error)
	// GetAgentTask serves two readers on this path. The origin gate reads the
	// row to get at the channel_ingested stamp; the round matcher reads it to
	// resolve an auto-retry clone back to the turn that owns its input batch,
	// which is the id the round was bound under.
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	FindChannelBindingForMember(ctx context.Context, arg db.FindChannelBindingForMemberParams) (db.ChannelUserBinding, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
	ListAttachmentsByChatMessage(ctx context.Context, arg db.ListAttachmentsByChatMessageParams) ([]db.Attachment, error)
}

// deliveryLookup is the pair of reads that turn a task id into the WeCom chat
// its words go to: the delivery row the engine wrote when the question was
// ingested, and the installation whose socket carries them. Shared with the
// typing indicator's failure notice, which addresses a bubble-less round the
// same way the answer does (taskAddress). *db.Queries satisfies it.
type deliveryLookup interface {
	GetChannelTaskDelivery(ctx context.Context, taskID pgtype.UUID) (db.ChannelTaskDelivery, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// Outbound delivers an agent's chat reply back to WeCom over the same aibot
// WebSocket the inbound loop owns, sealing the round's bubble with it where
// there still is one. Registered against the shared event bus; sessions with no
// wecom binding are silently ignored.
type Outbound struct {
	q outboundQueries
	// tasks is the retry-clone lookup behind rounds(): the same *db.Queries
	// as q, narrowed to the one row the round matcher reads.
	tasks   taskLookup
	senders *sendersRegistry
	streams *streamStore
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
// mid-flight is routed over the relay or dropped, never given a second
// connection.
//
// streams is the same store the typing indicator writes to; nil disables the
// in-place reply and leaves every answer going out as a new message.
//
// WithAttachments is the one option that changes what can be delivered: pass
// the deployment's object storage and the files an agent produced are
// delivered into the chat behind the answer.
func NewOutbound(q outboundQueries, senders *sendersRegistry, streams *streamStore, logger *slog.Logger, opts ...OutboundOption) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	o := &Outbound{
		q:       q,
		tasks:   q,
		senders: senders,
		streams: streams,
		logger:  logger,
		spawn:   func(f func()) { go f() },
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Register subscribes to the chat-done and inbox events on the bus.
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

// answerOutcome is what deliverAnswer reports back about a turn's words, for
// the two decisions its caller still has to make once they are gone.
type answerOutcome struct {
	// addr is where the words went, or the zero value when none did. It is the
	// address the files that follow are sent to.
	addr roundAddress

	// spoke says words reached the user from THIS process. It is what the
	// attachment path needs to know whether the files are the whole answer:
	// a sealed bubble already put text on the screen, so files failing behind
	// it are a file problem, not a reply this adapter owed and lost.
	spoke bool

	// routed says the turn was handed to the replica holding the socket. That
	// replica sends the files too (relayFrame.CarriesFiles), so this one must
	// not, or the user gets each attachment twice.
	routed bool
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
	// Asked BEFORE the take, which is the line that consumes the round. Every
	// way a web run could touch this room is on the far side of it: the take
	// removes the bubble the room's own question opened, and deliverAnswer seals
	// it — with the answer, or with the copy pack's StreamNoReply when the
	// completion is empty. Sealing is not sending, so a gate placed inside
	// deliverAnswer would still cost the asker in the room the bubble they were
	// waiting on, and they would read a web run's ending in it. An answer that
	// must not reach the room must not take over the room's message either. The
	// failure notice orders its own gate the same way, and for the same reason
	// — see failureBelongsOnWecom in typing_indicator.go.
	//
	// Everything up to here is a read. Keep it that way.
	//
	// The empty completion is deliberately NOT short-circuited ahead of this.
	// An agent that finished with nothing to add still owes the room's bubble
	// an ending, and returning early would leave it spinning forever; the turn
	// that genuinely had nothing to say names itself as skipNothingToSay inside
	// deliverAnswer, where it is known that no bubble was waiting on it.
	taskID, ok := chatDoneTaskID(e)
	if !ok {
		o.dropped(ctx, e, dropTaskMissing, nil)
		return nil
	}
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

	// Whether the agent produced files for this turn, decided before the seal
	// because what the seal has to do depends on it. Everything it reads is
	// already in hand, so a deployment with no storage costs no query.
	carriesFiles := o.mayCarryAttachments(e)

	// The take removes the round under the store's lock, which is what makes
	// two closers racing for one run produce one closing frame. Whether it
	// found a bubble is all deliverAnswer needs to know: the bubble is a cache,
	// and a round with none goes down the plain path.
	t, _ := o.rounds().take(ctx, sessionID, byTask(taskIDFromEvent(e)))
	said, err := o.deliverAnswer(ctx, e, taskID, t, content, carriesFiles)
	if errors.Is(err, errNothingToSay) {
		// Counted by whichever branch declined to speak — each one names its
		// own reason, and a blanket count here would file a revoked
		// installation and a silent agent under the same label.
		return nil
	}
	if err != nil {
		return err
	}
	if said.routed {
		// The replica holding the socket owns the rest of this turn, files
		// included — relayFrame.CarriesFiles is how it knows. Sending them from
		// here as well would put every attachment in the chat twice. A
		// published frame is owed an outcome by the relay, which records the
		// drop itself if nobody takes it (RelayOutbound.watchOutcomes).
		return nil
	}
	// Then whatever the agent produced alongside the words, as its own message —
	// a WeCom reply cannot carry a file inline. It goes wherever the answer just
	// went, bubble or plain message, which is the one address this turn has
	// established belongs to the room that asked. An address the round never
	// learned is a no-op inside deliverAttachments.
	//
	// carriesTheReply is true only when nothing has been shown to the user yet,
	// which makes the files the whole answer and their outcome the reply's. A
	// sealed bubble or a sent message has already settled that, so this is read
	// off what deliverAnswer did rather than off the completion being empty:
	// an empty completion under a bubble was answered in words all the same.
	o.deliverAttachments(e, attachmentTarget{
		InstallationID: said.addr.InstallationID,
		ChatID:         said.addr.ChatID,
		ChatType:       said.addr.ChatType,
		SessionID:      e.ChatSessionID,
	}, !said.spoke)
	return nil
}

// deliverAnswer writes an agent's answer wherever this round can still be
// reached, in the order the user would rather have it.
//
// The bubble comes first: the round opened one when the question arrived and
// the whole point of the feature is that the answer replaces it in place.
// Everything else is an ordinary message to the chat the task's delivery row
// names.
//
// Nothing here re-asks where the question came from. processEvent has already
// refused every run that is not this room's, which is what makes it safe for
// this function to write without asking.
func (o *Outbound) deliverAnswer(ctx context.Context, e events.Event, taskID pgtype.UUID, t roundTurn, content string, carriesFiles bool) (answerOutcome, error) {
	if t.HasBubble {
		// A bubble on screen has to end in words. An empty completion is a
		// legitimate outcome — the agent had nothing to add — but an endless
		// spinner is not, so the copy stands in for the silence. For a round
		// that waited in line behind another, the silence has a better
		// explanation: the reply ahead of it already covered this message.
		// The round's own language, captured when its bubble was opened. And
		// when the agent said nothing but produced files, the silence is not the
		// end of the turn at all: those files arrive as their own messages right
		// underneath, so a bubble reading "nothing to reply this round" would
		// contradict the next thing on screen.
		text := content
		if !hasVisibleChar(text) {
			c := copyFor(t.Handle.Locale)
			switch {
			case t.Handle.QueuedBehind:
				text = c.StreamMerged
			case carriesFiles:
				text = c.StreamNoReplyWithFiles
			default:
				text = c.StreamNoReply
			}
		}
		// A bubble the server has disowned mid-run is not written to again: the
		// typing indicator was told this stream takes no frame, has already
		// said so to the reader, and every further attempt is a refusal charged
		// against the whole bot's rate limit. This is the new message it
		// promised.
		if !t.Handle.Unusable {
			if err := o.finishStream(ctx, t.Handle, text); err == nil {
				o.delivered()
				return answerOutcome{addr: t.Handle.address(), spoke: true}, nil
			}
		}
		// The frame was refused for good, or every attempt at it went
		// unanswered, or it was never worth attempting. Say it as a new
		// message instead: 846608 and 846605 both mean this stream will never
		// take another frame, and the retries inside seal have already spent
		// what a lost ack is worth. The plain message is the one route whose
		// outcome this process can actually observe. finishStream counted the
		// refusal.
		//
		// Not because the handle has gone stale. A callback's req_id belongs to
		// the turn rather than to the socket it arrived on, and a stream opened
		// before a reconnect is still writable after it — measured against a
		// live tenant, see senders_registry.go.
		content = text
	}
	if !hasVisibleChar(content) {
		// No bubble to close and nothing to say. That is the end of it: a
		// completion with no words and no file was never a message, and no
		// bubble is waiting on one.
		if !carriesFiles {
			o.skipped(ctx, e, skipNothingToSay)
			return answerOutcome{}, errNothingToSay
		}
		// The agent said nothing but produced files, and those still have to
		// reach the room. sendAsMessage is where the delivery row names it; with
		// no words to carry it sends none, and returns the address the files go
		// to.
	}
	return o.sendAsMessage(ctx, e, taskID, content, carriesFiles)
}

// taskAddress resolves the chat a turn's words go to, off the task's own
// delivery row. The skip reason names a turn that is not this adapter's to
// answer — no row, another platform's row, an installation revoked since —
// so the two callers can count it their own way; the error is a database that
// did not answer.
//
// The address comes off channel_task_delivery rather than off the session's
// current binding: /new and /clear re-point a session, and an answer produced
// across one of those belongs to the room that asked. The bubble path is
// already routed that way — its handle carries the address the question came in
// on — so both paths of this adapter answer where they were asked.
func taskAddress(ctx context.Context, q deliveryLookup, taskID pgtype.UUID) (roundAddress, skipReason, error) {
	delivery, err := q.GetChannelTaskDelivery(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No route was recorded for a turn whose input DID come in over a
			// channel — the origin gate has already established that much. In
			// a steady-state deployment there is no such turn: the delivery
			// row is written inside the same transaction that enqueues a
			// channel task. What produces one is an upgrade, and the answer it
			// belongs to is going nowhere, so the branch is counted and warned
			// about rather than left as a quiet return. See skipNoDeliveryRow.
			return roundAddress{}, skipNoDeliveryRow, nil
		}
		return roundAddress{}, "", fmt.Errorf("wecom: lookup task delivery: %w", err)
	}
	if delivery.ChannelType != channelTypeWecom {
		// Not a wecom turn (Slack / Lark). Named rather than silent so it does
		// not share an exit with the branch above it: the two are one
		// errNothingToSay from outside, and one of them means somebody is
		// waiting.
		return roundAddress{}, skipNotWecomTurn, nil
	}
	binding := wecomBindingFromTaskDelivery(delivery)
	inst, err := q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		return roundAddress{}, "", fmt.Errorf("wecom: load installation: %w", err)
	}
	if inst.Status != string(InstallationActive) {
		// Revoked between trigger and reply.
		return roundAddress{}, skipInstallationInactive, nil
	}
	return roundAddress{
		InstallationID: inst.ID,
		ChatID:         binding.ChannelChatID,
		ChatType:       aibotChatTypeFromChannel(channel.ChatType(binding.ChatType)),
	}, "", nil
}

// sendAsMessage pushes an answer to the chat this turn was admitted on, for a
// round with no bubble left to put it in — a restart mid-run, a stream past its
// window, a frame the server refused. It returns where it spoke, which is where
// the files that follow go.
func (o *Outbound) sendAsMessage(ctx context.Context, e events.Event, taskID pgtype.UUID, content string, carriesFiles bool) (answerOutcome, error) {
	addr, skip, err := taskAddress(ctx, o.q, taskID)
	if err != nil {
		return answerOutcome{}, err
	}
	if skip != "" {
		// Named here rather than left to the caller: from outside, each of
		// these and a silent agent are all errNothingToSay.
		o.skippedFor(ctx, e.ChatSessionID, skip)
		return answerOutcome{}, errNothingToSay
	}
	if o.senders == nil {
		return answerOutcome{}, errors.New("wecom: sender registry not configured")
	}
	sender := o.senders.get(addr.InstallationID)
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
			InstallationID: util.UUIDToString(addr.InstallationID),
			ChatID:         addr.ChatID,
			ChatType:       addr.ChatType,
			Content:        content,
			TaskID:         util.UUIDToString(taskID),
			MessageID:      chatDoneMessageID(e.Payload),
			WorkspaceID:    e.WorkspaceID,
			SessionID:      e.ChatSessionID,
			CarriesFiles:   carriesFiles,
		}, relayEventID(e, taskID)) {
			o.logger.DebugContext(ctx, "wecom outbound: routed to the replica holding the socket",
				"installation_id", util.UUIDToString(addr.InstallationID), "chat_session_id", e.ChatSessionID)
			return answerOutcome{addr: addr, routed: true}, nil
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
		return answerOutcome{}, errNoLiveConnection
	}
	// Words first — and only when there are any. An empty completion reaches
	// here only because a file is bound to the turn, and an empty markdown
	// message ahead of that file would be noise the user has to scroll past.
	if !hasVisibleChar(content) {
		return answerOutcome{addr: addr}, nil
	}
	if err := sender.sendTextCtx(ctx, addr.ChatID, addr.ChatType, content); err != nil {
		return answerOutcome{addr: addr}, err
	}
	o.delivered()
	return answerOutcome{addr: addr, spoke: true}, nil
}

// wecomBindingFromTaskDelivery reads a turn's route back as the binding row the
// rest of this adapter is written against.
func wecomBindingFromTaskDelivery(delivery db.ChannelTaskDelivery) db.ChannelChatSessionBinding {
	return db.ChannelChatSessionBinding{
		ID: delivery.BindingID, InstallationID: delivery.InstallationID,
		ChannelType: delivery.ChannelType, ChannelChatID: delivery.ChannelChatID,
		ChatType:      delivery.ChatType,
		LastMessageID: delivery.ChannelMessageID, LastThreadID: delivery.ChannelThreadID,
		RouteRevision: delivery.RouteRevision, Config: delivery.Config,
	}
}

// rounds builds the matcher that turns a task id on an event into the round it
// belongs to — the same one the typing indicator's endings go through.
func (o *Outbound) rounds() roundTaker {
	return roundTaker{streams: o.streams, tasks: o.tasks, log: o.logger}
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

// finishStream writes the answer into the bubble and seals it, through the
// store's seal — the one place the closing frame's retry policy lives. A
// failure here is not fatal to the reply — it means the caller falls back to a
// new message — so it is logged with the one detail that explains it: whether
// the stream is beyond saving (past its window, bad req_id) or the socket
// simply blinked.
//
// Both endings are counted inside sendersRegistry.recordEnding, which every
// bubble closer goes through — this one and the typing indicator's. Counted at
// all because from outside the two are indistinguishable: the user gets the
// answer either way, and nobody reports "the bubble I was watching turned into
// a separate message". A bubble that has stopped working at all — a WeCom-side
// change to the stream frame, a req_id convention that drifted — shows up as
// stream_fell_back climbing to meet stream_finished, and nowhere else.
//
// senders and streams are non-nil here: a turn has a bubble only through them.
func (o *Outbound) finishStream(ctx context.Context, h streamHandle, text string) error {
	err := o.streams.seal(ctx, o.senders, h, text)
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
