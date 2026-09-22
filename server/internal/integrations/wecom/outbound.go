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
// their outbound is stateless HTTP any replica can perform).
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
//
// The bubble path and the relay never contend for the same turn. A bubble is
// writable only on the replica that painted it, which is the replica holding
// the socket; a round whose bubble lives elsewhere finds none here and takes
// the addressed path, which is where the relay is.
//
// They MEET on the replica that takes a relayed reply. That replica is by
// definition the one holding the socket, which is the one that painted the
// bubble — a bubble is writable only where it was painted — so the frame
// carries the task id and deliverRelayed seals the round with it instead of
// pushing a second message underneath (relay_outbound.go). A relayed reply
// whose round is gone, or whose seal is refused, takes the ordinary addressed
// path, which is what a single-replica answer does too.
//
// Sessions with no wecom binding are ignored so this coexists with the Slack /
// Lark subscribers on the shared bus.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
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
	// GetTaskChannelOrigin is the origin gate's whole question — does the task
	// row exist, and did its input arrive over a channel — in one round trip.
	// It answers what GetAgentTask followed by TaskHasChannelIngestedMessages
	// answers, and the query is a transcription of
	// engine.TaskInputIsChannelIngested rather than a second opinion on it.
	GetTaskChannelOrigin(ctx context.Context, id pgtype.UUID) (db.GetTaskChannelOriginRow, error)
	// GetAgentTask is the round matcher's, not the gate's: it resolves an
	// auto-retry clone back to the turn that owns its input batch, which is the
	// id the round was bound under. NewOutbound passes this same value as the
	// roundTaker's taskLookup.
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	FindChannelBindingForMember(ctx context.Context, arg db.FindChannelBindingForMemberParams) (db.ChannelUserBinding, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
	ListAttachmentsByChatMessage(ctx context.Context, arg db.ListAttachmentsByChatMessageParams) ([]db.Attachment, error)
	// Which language this round's bubble is closed in: a 1:1 reads the asker's
	// own Multica profile, a room the deployment's (language.go).
	languageLookup
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
	// noticeRouter rather than *RelayOutbound: publish is the only method used
	// here, it is the seam typing_indicator.go already routes through, and a
	// concrete type makes "was this ending routed at all" untestable — which is
	// exactly the question three gaps hid behind.
	relay noticeRouter

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
	// task:failed and task:cancelled are NOT subscribed here, and that is the
	// one place this adapter deliberately departs from #7952 as merged.
	//
	// A run that ends has a bubble waiting on it, and only the typing
	// indicator can seal that bubble — sending the notice from here would
	// leave the bubble spinning beside it, and subscribing in both places
	// would put two messages in the chat for one failure. So the indicator
	// owns every ending (typing_indicator.go), and it carries the same words
	// #7952 established: the platform's own redacted reason when there is
	// one, through taskFailedContent below.
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
	// chat:done is the only event this subscriber takes — the typing indicator
	// owns task:failed, because only it can seal the bubble that turn opened,
	// and two subscribers would answer the same failure twice. So the reply
	// text comes straight off the chat:done payload rather than through
	// deliverableContent, which exists to serve both.
	content := chatDoneContent(e.Payload)

	// An empty completion does NOT end the turn here, and that is the one
	// place this path has to differ from main. A bubble is already on the
	// asker's screen, and returning now would leave it turning until the
	// server's window runs out. deliverAnswer seals it with words instead —
	// merged, no reply, or no reply but files — and files still go out
	// underneath. The nothing_to_say exit lives there, where it can tell the
	// two cases apart.
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
	// Asked BEFORE the take, which is the line that consumes the round. Every
	// way a web run could touch this room is on the far side of it: the take
	// removes the bubble the room's own question opened, and deliverAnswer seals
	// it — with the answer, or with the copy pack's StreamNoReply when the
	// completion is empty. Sealing is not sending, so a gate placed inside
	// deliverAnswer would still cost the asker in the room the bubble they were
	// waiting on, and they would read a web run's ending in it. An answer that
	// must not reach the room must not take over the room's message either. The
	// failure notice orders its own gate the same way, and for the same reason
	// — see originOf in typing_indicator.go.
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
	// One keyed read, not two: GetTaskChannelOrigin answers what GetAgentTask
	// followed by engine.TaskInputIsChannelIngested answers, and answers it the
	// same way — including for a task whose input batch has no owner, which is
	// channel-ingested. This read is synchronous on the completion response
	// (see the query's comment), so the round trip it saves is one the daemon
	// waits for.
	origin, err := o.q.GetTaskChannelOrigin(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Cancelled and deleted while its completion was in flight. NOT
			// folded into "asked in the web UI": that exit is the ordinary one
			// and this row was owed an answer.
			o.dropped(ctx, e, dropTaskMissing, nil)
			return nil
		}
		// Recorded rather than returned. The caller would file a context error
		// as an unconfirmed delivery — unconfirmedReason maps it to
		// "interrupted", which tells an operator the user may ALREADY HAVE this
		// reply and a resend would duplicate it. Nothing was written to a
		// socket here and nothing was going to be: this gate is upstream of
		// every send. The honest record is the one dropTransport describes.
		o.dropped(ctx, e, dropTransport, err)
		return nil
	}
	if !origin.ChannelIngested {
		// Give the bubble back. A run typed in Multica can hold the room's
		// round — it is bound off task:queued, and a chat task's event carries
		// nothing that tells the two apart — so returning here without
		// releasing leaves the round bound to a run that will never close it:
		// the asker watches the bubble turn until the platform ends it, and
		// their own answer finds no round and degrades to a plain message.
		// The same release the failure and cancellation gates perform.
		if o.streams != nil {
			o.streams.releaseRound(sessionID, taskIDFromEvent(e))
		}
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
	said, err := o.deliverAnswer(ctx, e, taskID, t, content, carriesFiles, origin.BatchOwnerUnknown)
	if errors.Is(err, errOutcomeRecorded) {
		// The branch that tried to speak has already filed its own outcome
		// through recordSend. Counting it here as well is the double count
		// this sentinel exists to make impossible.
		return nil
	}
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
// ownerUnknown is the origin gate's second fact, carried rather than re-read:
// it decides the LOG LEVEL on a turn that turns out to have no delivery row,
// and nothing else. See sendAsMessage.
func (o *Outbound) deliverAnswer(ctx context.Context, e events.Event, taskID pgtype.UUID, t roundTurn, content string, carriesFiles, ownerUnknown bool) (answerOutcome, error) {
	if t.HasBubble {
		// A bubble on screen has to end in words. An empty completion is a
		// legitimate outcome — the agent had nothing to add — but an endless
		// spinner is not, so the copy stands in for the silence. The round's own
		// language, captured when its bubble was opened. And when the agent said
		// nothing but produced files, the silence is not the end of the turn at
		// all: those files arrive as their own messages right underneath, so a
		// bubble reading "nothing to reply this round" would contradict the next
		// thing on screen.
		//
		// A ROUND THAT WAITED IN LINE SAYS THE SAME THING AS ONE THAT DID NOT.
		// Another round being open when this one was painted is not evidence
		// that the reply ahead covered this message, so there is no "merged
		// with the previous reply" notice: that would need a real merge signal.
		text := content
		if !hasVisibleChar(text) {
			c := copyFor(t.Handle.Locale)
			switch {
			case carriesFiles:
				text = c.StreamNoReplyWithFiles
			default:
				text = c.StreamNoReply
			}
		}
		sealErr := o.finishStream(ctx, t.Handle, text)
		switch classifySeal(sealErr) {
		case sealOnScreen:
			o.delivered()
			return answerOutcome{addr: t.Handle.address(), spoke: true}, nil
		case sealUnknown:
			o.unconfirmed(ctx, e, unconfirmedSealReason(sealErr), sealErr)
			return answerOutcome{addr: t.Handle.address(), spoke: true}, nil
		}
		// Proof the words are not on screen: say them as a message, on a budget
		// the seal cannot have already spent. On main the answer always had the
		// full budget for its own message; falling back must not cost it that.
		content = text
		var cancel context.CancelFunc
		ctx, cancel = fallbackBudget(ctx)
		defer cancel()
	}
	if !hasVisibleChar(content) {
		// No bubble to close and nothing to say. That is the end of it: a
		// completion with no words and no file was never a message, and no
		// bubble is waiting on one.
		if !carriesFiles {
			// NOTHING TO SAY IS STILL AN ENDING, and it still has to reach the
			// bubble. Reaching here means no round was found ON THIS REPLICA;
			// off-lease the round is on a sibling, and returning silently left
			// it turning for the rest of the protocol's window over a turn that
			// had already finished.
			o.relaySeal(e, taskID, sealReasonNoReply, false)
			o.skipped(ctx, e, skipNothingToSay)
			return answerOutcome{}, errNothingToSay
		}
		// The agent said nothing but produced files, and those still have to
		// reach the room. sendAsMessage is where the delivery row names it; with
		// no words to carry it sends none, and returns the address the files go
		// to.
	}
	return o.sendAsMessage(ctx, e, taskID, content, carriesFiles, ownerUnknown)
}

// taskAddress resolves the chat a turn's words go to, off the task's own
// delivery row.
//
// Three outcomes, read off the two results together. An address that is
// known() is where to write. A zero address with no skip reason is a turn this
// adapter was never going to answer — no delivery row, or another platform's
// row — which is the silent, ordinary case on a bus every channel publishes
// to. A zero address WITH a reason is a turn that was ours until the
// installation was revoked, and that one is worth counting.
//
// The address comes off channel_task_delivery rather than off the session's
// current binding: /new and /clear re-point a session, and an answer produced
// across one of those belongs to the room that asked. The bubble path is
// already routed that way — its handle carries the address the question came in
// on — so both paths of this adapter answer where they were asked.
func taskAddress(ctx context.Context, q deliveryLookup, taskID pgtype.UUID) (roundAddress, skipReason, bool, error) {
	delivery, err := q.GetChannelTaskDelivery(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// NO ROW AT ALL IS NOT THE SAME ANSWER as a row naming another
			// platform, and the third return is what keeps them apart. A run
			// typed in Multica has no row by design — EnqueueChatTask writes no
			// external delivery snapshot — so "no row" is the one answer that
			// leaves the origin still open, and the only one a caller should
			// spend further reads on.
			return roundAddress{}, "", false, nil
		}
		return roundAddress{}, "", false, fmt.Errorf("wecom: lookup task delivery: %w", err)
	}
	if delivery.ChannelType != channelTypeWecom {
		return roundAddress{}, "", true, nil
	}
	binding := wecomBindingFromTaskDelivery(delivery)
	inst, err := q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		return roundAddress{}, "", true, fmt.Errorf("wecom: load installation: %w", err)
	}
	if inst.Status != string(InstallationActive) {
		// Revoked between trigger and reply.
		return roundAddress{}, skipInstallationInactive, true, nil
	}
	return roundAddress{
		InstallationID: inst.ID,
		ChatID:         binding.ChannelChatID,
		ChatType:       aibotChatTypeFromChannel(channel.ChatType(binding.ChatType)),
	}, "", true, nil
}

// routeFrame hands a frame to the relay, and answers false when there is none.
//
// THE NIL CHECK IS HERE BECAUSE THE FIELD IS AN INTERFACE. It used to be a
// *RelayOutbound, whose publish begins with `if r == nil`, so a call through a
// nil pointer was safe and the call sites relied on that without saying so.
// A nil interface has no receiver to run that guard, so widening the field
// silently removed the safety three call sites were standing on. One place to
// check it, so the next call site cannot forget.
func (o *Outbound) routeFrame(f relayFrame, eventID string) bool {
	if o.relay == nil {
		return false
	}
	return o.relay.publish(f, eventID)
}

// relaySeal asks whichever replica holds this round to close it. Routed by
// round ownership, so it needs no address — see deliverRelayed.
func (o *Outbound) relaySeal(e events.Event, taskID pgtype.UUID, reason string, carriesFiles bool) {
	id := util.UUIDToString(taskID)
	if id == "" {
		return
	}
	o.routeFrame(relayFrame{
		Kind:         relayKindSeal,
		SealReason:   reason,
		TaskID:       id,
		SessionID:    e.ChatSessionID,
		CarriesFiles: carriesFiles,
	}, id)
}

// sendAsMessage pushes an answer to the chat this turn was admitted on, for a
// round with no bubble left to put it in — a restart mid-run, a stream past its
// window, a frame the server refused. It returns where it spoke, which is where
// the files that follow go.
func (o *Outbound) sendAsMessage(ctx context.Context, e events.Event, taskID pgtype.UUID, content string, carriesFiles, ownerUnknown bool) (answerOutcome, error) {
	addr, skip, hadRow, err := taskAddress(ctx, o.q, taskID)
	if err != nil {
		return answerOutcome{}, err
	}
	if skip != "" {
		o.skippedFor(ctx, e.ChatSessionID, skip)
		return answerOutcome{}, errNothingToSay
	}
	if !addr.known() {
		// The two reasons to have no address are one quiet return from the
		// outside, and only one of them means somebody is waiting. Every turn
		// that reaches here is past the origin gate, so its question DID come
		// in over a channel — which is what makes a missing row worth a word
		// rather than the ordinary traffic of a shared bus. Ahead of the gate
		// this same branch also caught every question ever typed in the Multica
		// web UI, and an exit shared with those could only be silent.
		//
		// Behind the gate, a missing row means a turn the channel ingested and
		// nobody can now address. In a steady-state deployment there is no such
		// turn: the row is written inside the same transaction that enqueues a
		// channel task. What produces one is an upgrade, and the answer it
		// belongs to is going nowhere — so it is counted and warned about
		// rather than left as a quiet return. See skipNoDeliveryRow.
		switch {
		case hadRow:
			o.skippedFor(ctx, e.ChatSessionID, skipNotWecomTurn)
		case ownerUnknown:
			// The gate delivered this turn on the open side of an unanswerable
			// verdict: its input batch has no owner, so nothing here ever
			// established it was a channel's. Warning would put the loudest
			// line this adapter has on a pre-158 web turn that auto-retried.
			o.skippedFor(ctx, e.ChatSessionID, skipRouteUnattributable)
		default:
			o.skippedFor(ctx, e.ChatSessionID, skipNoDeliveryRow)
		}
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
		if o.routeFrame(relayFrame{
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
	//
	// Empty is hasVisibleChar's sense of it, not `!= ""`. A completion of "\n"
	// is a bubble with nothing in it on the reader's screen, and counting it as
	// the words that answered the turn also tells the file below it that the
	// reply has already been accounted for.
	if !hasVisibleChar(content) {
		return answerOutcome{addr: addr}, nil
	}
	err = sender.sendTextCtx(ctx, addr.ChatID, addr.ChatType, content)
	// Recorded here rather than returned, so this send and the relay's go
	// through the one mapping in recordSend (#8344).
	o.recordSend(ctx, e.ChatSessionID, e.Type, err)
	if err != nil && !errors.Is(err, errPartiallySent) {
		// Nothing of the answer landed. The files are not an answer on their
		// own, so the turn ends here.
		//
		// errOutcomeRecorded, NOT err: recordSend above already filed this
		// send, and handleEvent counts every error processEvent returns. The
		// same refusal would land on the counters twice — measured, once, as
		// platform_refused = 2.
		return answerOutcome{addr: addr}, errOutcomeRecorded
	}
	// errPartiallySent still spoke: part of the answer is on the reader's
	// screen, and the files below it are not the reply.
	return answerOutcome{addr: addr, spoke: true}, nil
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

// taskFailedPrefix marks a failure notice apart from a reply in the chat, the
// way DingTalk's and Lark's do.
const taskFailedPrefix = "⚠️ "

// taskFailedContent is the platform's own redacted reason for a failed run —
// the same text the web transcript shows — and nothing while an auto-retry is
// pending: the retry attempt reports its own outcome, and announcing a failure
// the next attempt may undo would be noise. Empty means the failure arrived
// without a reason, and the copy pack's generic line stands in for it.
//
// The reader is the typing indicator, which owns every ending this adapter
// announces (see Register above for why the subscription is not here).
func taskFailedContent(payload any) string {
	p, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	if pending, _ := p["retry_pending"].(bool); pending {
		return ""
	}
	msg, _ := p["error"].(string)
	if strings.TrimSpace(msg) == "" {
		return ""
	}
	return taskFailedPrefix + msg
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
		if o.routeFrame(relayFrame{
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
