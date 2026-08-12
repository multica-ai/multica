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
	ListAttachmentsByChatMessage(ctx context.Context, arg db.ListAttachmentsByChatMessageParams) ([]db.Attachment, error)
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

	// retryAfter is the first delay before an answer the ledger handed back
	// undelivered is tried again. Negative disables the retry; test-only.
	retryAfter time.Duration

	// objects is the deployment's object storage, or nil when there is none.
	// Non-nil is what turns file delivery on (outbound_media.go).
	objects mediaObjectStore

	// spawn runs an attachment delivery. A field rather than a bare `go` so a
	// test can run it inline and observe the result deterministically.
	spawn func(func())

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
// streams is the same store the typing indicator writes to; nil disables the
// in-place reply and leaves every answer going out as a new message.
//
// WithAttachments is the one option: pass the deployment's object storage and
// the files an agent produced are delivered into the chat behind the answer.
func NewOutbound(q outboundQueries, senders *sendersRegistry, streams *streamStore, logger *slog.Logger, opts ...OutboundOption) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	o := &Outbound{
		q:          q,
		tasks:      q,
		senders:    senders,
		streams:    streams,
		logger:     logger,
		retryAfter: answerRetryAfter,
		spawn:      func(f func()) { go f() },
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
	//
	// It is the ceiling on everything up to the words going out, not on the
	// whole handler: an answer that waits out another delivery for the same run
	// can spend all of this waiting, and the delivery behind the wait then takes
	// a bounded budget of its own rather than the nothing that is left. So the
	// worst case here is this plus one streamCloseTimeout — see deliveryBudget
	// in stream_store.go for why the alternative is losing the answer.
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
	// Refuses a chat:done for a Slack, Lark or web-only session in one query
	// instead of the two the origin gate below costs, and its row is the
	// address an attachment falls back to when the answer itself had nothing to
	// say. The row is read AGAIN in sendAsMessage, which is where a plain
	// message needs it — that lookup sits past the take on purpose, and this
	// one cannot stand in for it because a round can be taken and sealed
	// without ever reaching a plain message.
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
	// An empty completion does NOT end the turn. Two things can still be owed:
	// a file the agent produced and said nothing about, and — since the bubble
	// — the seal on a spinner the asker is watching. deliverAnswer decides
	// which, and answers errNothingToSay when it is neither.
	content := chatDoneContent(e.Payload)
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
	// Asked BEFORE sayEnding, which is the line that consumes the round. Every
	// way a web run could touch this room is on the far side of it: sayEnding
	// takes the bubble the room's own question opened, and deliverAnswer seals
	// it — with the answer, or with streamCopyNoReply when the completion is
	// empty. Sealing is not sending, so a gate placed inside deliverAnswer
	// would still cost the asker in the room the bubble they were waiting on,
	// and they would read a web run's ending in it. An answer that must not
	// reach the room must not take over the room's message either. The failure
	// notice orders its own gate the same way, and for the same reason — see
	// failureBelongsOnWecom in typing_indicator.go.
	//
	// The cost of asking this early is two reads on a chat:done that turns out
	// to belong to another adapter, which used to be refused one query later by
	// the binding lookup. That lookup cannot come first any more: it moved into
	// sendAsMessage, past the take, and nothing may precede the take but this.
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

	// Every way this answer can reach the user runs inside deliverAnswer, and
	// the ledger records the ending only from what deliverAnswer reports. There
	// is no path here that sends without recording, and none that records
	// without sending — see the ending ledger's contract in stream_store.go.
	return o.sayTheAnswer(ctx, e, sessionID, taskIDFromEvent(e), content, attachmentTarget{
		InstallationID: binding.InstallationID,
		ChatID:         binding.ChannelChatID,
		ChatType:       aibotChatTypeFromChannel(channel.ChatType(binding.ChatType)),
	}, endingRetries{})
}

// answerRetryAfter is how long an answer the ledger handed back undelivered
// waits before trying again, and answerRetryAttempts is how many times EACH of
// the two causes may do so. Same shape and the same numbers as the typing
// indicator's endingRetryAfter, and for the same reason — what an attempt is
// waiting out is a delivery bounded by streamCloseTimeout, and every attempt is
// over long inside roundMemory.
//
// The delay doubles over both causes together, so a chain that meets only one
// runs its three attempts 15s, 45s and 105s after the answer was first
// attempted, and even a chain that meets both is finished inside sixteen
// minutes. The counts are kept apart for the reason endingRetries states: one
// shared counter lets a run that lost three waits arrive at a refused send with
// nothing left to spend on it.
const (
	answerRetryAfter    = 15 * time.Second
	answerRetryAttempts = 3
)

// sayTheAnswer puts one completion in the round that asked for it, and comes
// back for it whenever the ledger hands the answer back still undelivered.
//
// The retry is what this subscriber owes the answer it is carrying. Nothing else
// holds it: chat:done fires once, the completion is not persisted anywhere this
// process can go back to, and every other publisher of this round's ending has
// only an apology to offer. The guard cannot even offer that — it is refused a
// round whose run is over (refuseTakeLocked) — so an attempt that gives up here
// is an answer nobody says, and a bubble nothing seals until the sweep retires
// it.
//
// So the default is to come back, and answerRetryCause names the one outcome
// that stops it. That direction is the point rather than a preference. Four
// rounds of this were spent adding an error class to a list of outcomes worth
// another attempt, and each of them was followed by the class that was not on
// the list: a wait that expired, a wait that was WON with the budget spent, a
// send that failed on a dead socket. A list of what may be repeated is never
// closed under a failure nobody has met yet, and every gap in it reads as
// "drop the answer".
//
// Two shapes are worth naming for what they are, though both simply take the
// default:
//
//   - errEndingDeferred. The wait for a delivery already speaking for this run
//     ran past the budget the bus handed this subscriber, which says nothing
//     whatever about whether the user has an answer. Not a rare shape either:
//     the budget is taken at the top of handleEvent, before the binding lookup
//     and both origin reads, while the guard takes a fresh streamCloseTimeout of
//     its own when it fires, so ordinary database latency is enough to meet a
//     guard that has just started sending with all of its. And the guard is the
//     worst publisher to be waiting behind, because its words land and end
//     nothing — it files streamCopyStillWorking, and the answer IS the separate
//     reply that copy has just promised.
//   - both routes failing on a socket that took neither. The ledger puts the
//     round back on its list holding the bubble (endEnding), and this is the
//     publisher that comes for it: the attempt behind the reconnect seals the
//     spinner the asker is watching, in place, with the answer.
//
// The wait is not the only way the budget goes. Winning it costs the same time,
// and what would be left for the binding row, the installation row and the ack
// is then nothing — a delivery that fails on a context error with the round
// already consumed. That one is fixed a layer down: the ledger hands the
// delivery a budget of its own rather than the remainder (deliveryBudget in
// stream_store.go), so this path never has to tell a context error apart from a
// refusal.
//
// WHAT A BOOKED RETRY COSTS. Returning nil once one is booked reports the
// chat:done handled while this subscriber is still holding the answer. The
// in-process bus has no redelivery, so returning an error instead would only add
// a WARN — but for as long as the chain runs, the answer exists nowhere but in a
// time.AfterFunc closure. A restart or a panic in that window loses it with no
// log line and no metric, and chat:done does not fire again.
func (o *Outbound) sayTheAnswer(ctx context.Context, e events.Event, sessionID pgtype.UUID, taskID, content string, fallback attachmentTarget, retries endingRetries) error {
	retries = retries.begin(time.Now())
	// Asked on EVERY attempt, and ahead of the take.
	//
	// Ahead of the take because the bubble path writes its closing frame
	// through the registered socket without ever loading the installation —
	// sendAsMessage's check (below) only guards the plain-message fallback. A
	// socket the reaper has not got to yet is not permission to keep writing:
	// revoking an installation is a person withdrawing it.
	//
	// On every attempt because an attempt can be fifteen minutes behind the one
	// that scheduled it, and the withdrawal can land in between. Inheriting the
	// first attempt's answer would write to a chat whose owner has since said
	// no.
	switch o.mayStillWrite(ctx, fallback.InstallationID) {
	case installationGone:
		// Established: nobody may write here again. The answer is not owed to
		// anyone, so reporting the event handled is the truth.
		return nil
	case installationUnreadable:
		// Nothing was established. Refusing to write is right; reporting the
		// work finished is not — chat:done fires once and this subscriber holds
		// the only copy. Book an attempt on the same bounded schedule a refused
		// send uses, and let the last one fall through to the caller's WARN.
		if o.bookAnswerRetry(e, sessionID, taskID, content, fallback, retries, retryRefused) {
			return nil
		}
		return errors.New("wecom: could not read the installation's permission, and the answer is out of attempts")
	}
	// deliveredTo is set only by an attempt of OURS that landed. sayEnding
	// returns a verdict rather than an address, and the difference matters
	// here: roundToldAlready means somebody else already said this round's
	// ending, and a file sent behind words we did not write would arrive with
	// no answer in front of it.
	var deliveredTo roundAddress
	delivered := false
	_, err := o.rounds().sayEnding(ctx, sessionID, byTask(taskID), roundOver,
		func(ctx context.Context, t roundTurn) (roundAddress, error) {
			addr, err := o.deliverAnswer(ctx, sessionID, t, content)
			if err == nil {
				deliveredTo, delivered = addr, true
			}
			return addr, err
		})
	if err == nil || errors.Is(err, errNoWordsOwed) || errors.Is(err, errNothingToSay) {
		// Whatever the agent produced alongside the words goes out behind them
		// as its own message — a WeCom reply cannot carry a file inline.
		//
		// delivered is set only inside the send closure, so it is the proof
		// that THIS call put something on the screen. sayEnding answers
		// roundToldAlready — somebody else said this round's ending — without
		// running the closure at all, and with a nil error; read as success
		// that sends a file behind words we did not write, and a repeated
		// chat:done delivers the text once and the file twice.
		//
		// The verdict itself is not read. It cannot add anything here: it is
		// roundToldAlready in exactly the cases the closure did not run, which
		// are exactly the cases delivered is false, so a condition on it could
		// never fire on its own.
		//
		// errNoWordsOwed is the other way a file legitimately travels alone:
		// the turn was fine and the completion was empty, which is what the
		// platform does when the agent produced a file and said nothing about
		// it.
		//
		// A repeat is not recoverable here. Duplicate copy is a line the user
		// scrolls past; a duplicate attachment is a file sent twice into
		// somebody's chat, and nothing takes it back.
		//
		// Re-checked rather than inherited: the send may have taken a while and
		// the answer's own gate was passed before it.
		// Re-read on a budget of its own. The words may have taken most of the
		// caller's, and a read that ran out of time would otherwise look like a
		// permission that was withdrawn — the same conflation this gate exists
		// to avoid, arriving by a different road.
		var permitted installationCheck
		if delivered || errors.Is(err, errNoWordsOwed) {
			reCtx, done := deliveryBudget(context.WithoutCancel(ctx))
			permitted = o.mayStillWrite(reCtx, fallback.InstallationID)
			done()
			if permitted == installationUnreadable {
				// The words are already out, so re-running the whole answer
				// would say them twice. The file is not sent and the failure is
				// surfaced rather than swallowed: this is the one outcome the
				// operator has to see, because nothing else will speak for it.
				o.logger.WarnContext(ctx, "wecom outbound: the file was not sent — the installation's permission could not be read after the answer landed",
					"chat_session_id", util.UUIDToString(sessionID))
				return fmt.Errorf("wecom: answer delivered, attachment withheld: permission unreadable")
			}
		}
		if (delivered || errors.Is(err, errNoWordsOwed)) && permitted == installationOK {
			// Addressed from the delivery when there was one, and from the
			// binding when the file travels alone — that answer had no address
			// of its own to give.
			target := fallback
			if delivered {
				target = attachmentTarget{
					InstallationID: deliveredTo.InstallationID,
					ChatID:         deliveredTo.ChatID,
					ChatType:       deliveredTo.ChatType,
				}
			}
			o.deliverAttachments(e, target)
		}
		return nil
	}
	if cause, again := answerRetryCause(err); again &&
		o.bookAnswerRetry(e, sessionID, taskID, content, fallback, retries, cause) {
		return nil
	}
	// Out of attempts for this cause, or retries disabled. The answer is lost
	// either way, and the caller's WARN is the only place that can say so.
	return err
}

// installationCheck is what one permission read established. Three outcomes,
// because collapsing them is how the first version of this gate lost answers:
// a lookup that failed and a row that says revoked are the same refusal for THIS
// attempt and opposite facts about every later one.
type installationCheck int

const (
	// installationOK — active. Write.
	installationOK installationCheck = iota
	// installationGone — revoked, or the row is not there. Final: nobody may
	// write to it again, and an answer held for it is not owed to anyone.
	installationGone
	// installationUnreadable — the read itself failed. Nothing is established.
	// Refuse this attempt, and do NOT report the work finished: the answer is
	// still the only copy and still this subscriber's to deliver.
	installationUnreadable
)

// mayStillWrite reads the installation's permission for one attempt.
//
// The three-way split follows the convention the rest of this file already
// keeps: pgx.ErrNoRows is a fact, every other error is a failed question. The
// gate that preceded this returned a bool and answered "no" to both, so a
// database blip on one attempt confirmed a one-shot chat:done as handled and
// threw the answer away.
func (o *Outbound) mayStillWrite(ctx context.Context, id pgtype.UUID) installationCheck {
	if !id.Valid {
		return installationGone
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          id,
		ChannelType: channelTypeWecom,
	})
	switch {
	case err == nil:
	case errors.Is(err, pgx.ErrNoRows):
		return installationGone
	default:
		return installationUnreadable
	}
	if inst.Status != string(InstallationActive) {
		return installationGone
	}
	return installationOK
}

// errNoWordsOwed is the one outcome that says the turn is fine and there were
// simply no words for it: a round nothing promised, with an empty completion.
// It is separate from errNothingToSay on purpose. That one also means revoked,
// no binding row, and a session that is not WeCom's — three cases where NOTHING
// may be sent — and reading a file's permission off a value that carries them
// sent attachments into chats we had just refused to write words to.
var errNoWordsOwed = errors.New("wecom: no words owed for this round")

// answerRetryCause reads what the ledger handed back and says whether the answer
// is still this subscriber's to deliver, and under which cause the next attempt
// would be booked.
//
// It is a deny-list, and that is the whole of it. endingRetryCause, which the
// typing indicator's notices go through, asks whether an outcome is one of the
// failures this package knows to be safe to repeat; this asks whether it is the
// one failure that is not. They agree on every failure either of them was
// written for, and differ on the rest: a context that ran out ahead of a send, a
// registry that answered "no socket" under a name of its own, and every class
// nobody has produced yet. Each of those means nothing was shown to anybody, and
// this one says so without having had to meet it first — which is the difference
// between the two, because dropping the only copy of an answer on the strength
// of an unrecognised value is how the last four rounds went.
//
// The notices keep the narrower rule deliberately. A failure or a cancellation
// this manager drops still has a sweeper tick, a repeat on the bus or WeCom's
// own redelivery behind it, and the words it would repeat are one sentence of
// apology. An answer has no second publisher and the words are the reply itself.
//
// Three outcomes stop it, and only the third is a judgement call:
//
//   - nil. The words were accepted for sending and the run is on told.
//   - errNothingToSay. A delivery that declined to speak on purpose — a session
//     that is not WeCom's, an installation revoked between trigger and reply, an
//     empty completion nobody was owed. There is nothing to come back for.
//   - errWordsMayBeOnScreen. The answer may be in front of the asker right now,
//     put there by a frame that reached the wire and lost only its verdict. A
//     retry there is not a second attempt at delivery, it is a second copy of the
//     reply — printed beside a bubble that already carries it, because the round
//     was consumed and the repeat goes out as an ordinary message. Raised at the
//     two ack waits that can leave words in front of somebody unseen (ws_sender.go)
//     and carried up by deliverAnswer when the first of two routes ended that way.
func answerRetryCause(err error) (retryCause, bool) {
	switch {
	case err == nil,
		errors.Is(err, errNothingToSay),
		errors.Is(err, errNoWordsOwed),
		errors.Is(err, errWordsMayBeOnScreen):
		return retryNone, false
	case errors.Is(err, errEndingDeferred):
		return retryDeferred, true
	}
	return retryRefused, true
}

// bookAnswerRetry schedules another attempt at an answer, carrying the
// completion with it, and reports whether it booked one.
//
// Bounded twice over, the same way the typing indicator's retries are:
// answerRetryAttempts caps each cause and the delay doubles over both of them
// together, so a chain that meets only one cause falls at 15s, 45s and 105s and
// even one that meets both is well inside roundMemory.
//
// Measured from the answer's FIRST attempt rather than from the one that just
// failed. An attempt can spend its whole budget parked on the delivery it is
// waiting out before it defers, so a schedule measured from the end restarts its
// clock after every wait: the same three attempts then land at 15s, 55s and
// 125s. See endingRetries.next.
//
// The attempt takes a budget of its own because the caller's may be what ran
// out, and it goes back through sayEnding like any other publisher — an answer
// something else delivered in the meantime finds the run on told and says
// nothing, and one whose round was handed back finds the bubble and seals it.
func (o *Outbound) bookAnswerRetry(e events.Event, sessionID pgtype.UUID, taskID, content string, fallback attachmentTarget, retries endingRetries, cause retryCause) bool {
	if o.retryAfter <= 0 {
		return false
	}
	next, wait, ok := retries.next(cause, o.retryAfter, answerRetryAttempts, time.Now())
	if !ok {
		return false
	}
	time.AfterFunc(wait, func() {
		ctx, cancel := context.WithTimeout(context.Background(), streamCloseTimeout)
		defer cancel()
		if err := o.sayTheAnswer(ctx, e, sessionID, taskID, content, fallback, next); err != nil {
			o.logger.WarnContext(ctx, "wecom outbound: reply delivery failed",
				"error", err, "chat_session_id", util.UUIDToString(sessionID))
		}
	})
	return true
}

// deliverAnswer writes an agent's answer wherever this round can still be
// reached, in the order the user would rather have it.
//
// The bubble comes first: the round opened one when the question arrived and
// the whole point of the feature is that the answer replaces it in place. The
// round's own address is next, for the one case where an empty completion still
// owes the user words. Everything else is an ordinary message to the chat the
// binding row names.
//
// Nothing here re-asks where the question came from. processEvent has already
// refused every run that is not this room's, which is what makes it safe for
// this function to write without asking. That holds for a booked retry too:
// every attempt descends from the one processEvent that gated this event, so a
// later attempt inherits that decision rather than paying for it again.
func (o *Outbound) deliverAnswer(ctx context.Context, sessionID pgtype.UUID, t roundTurn, content string) (roundAddress, error) {
	// What the closing frame did, kept until the end. A delivery made of two
	// sends is accounted for by both of them: the second one's error alone can
	// say "nothing reached the user" about an attempt whose first half is on the
	// screen with its verdict missing. writeClosing carries the same pair for
	// the same reason (typing_indicator.go).
	var streamErr error
	if t.HasBubble {
		// A bubble on screen has to end in words. An empty completion is a
		// legitimate outcome — the agent had nothing to add — but an endless
		// spinner is not, so the copy stands in for the silence. For a round
		// that waited in line behind another, the silence has a better
		// explanation: the reply ahead of it already covered this message.
		text := content
		if !hasVisibleChar(text) {
			text = streamCopyNoReply
			if t.Handle.QueuedBehind {
				text = streamCopyMerged
			}
		}
		err := o.finishStream(ctx, t.Handle, text)
		if err == nil {
			return t.Handle.address(), nil
		}
		// The frame did not go. Say it as a new message instead, and do not
		// re-send the stream frame INSIDE THIS ATTEMPT: an ordinary message is a
		// second ROUTE and worth trying now, while a second frame on the stream
		// that just failed is the same route twice — and for an errcode it is
		// not even that, since 846608 and 846605 both mean this stream will
		// never take another frame.
		//
		// The bubble is not written off with it. A callback's req_id belongs to
		// the turn rather than to the socket it arrived on, so a stream opened
		// before a reconnect is still writable after it — measured against a
		// live tenant, see senders_registry.go — and since errFrameNotOnTheWire
		// (ws_sender.go) a failed socket write is known not to have reached the
		// peer. So when the plain message below fails too and the failure is one
		// of those, the ledger puts this round back on its list with the handle
		// intact rather than consuming it (endEnding, stream_store.go), and
		// whichever publisher comes next seals the spinner the user is watching
		// instead of speaking beside it.
		//
		// Which publisher that is, is this subscriber: sayTheAnswer books an
		// attempt for this outcome like any other it cannot place on the
		// screen, and it is the only publisher that can, because it is the only
		// one holding the answer. A failure or a cancellation arriving in the
		// meantime writes into the same bubble; the guard does not, being
		// refused a round whose run is over (refuseTakeLocked) — its sentence
		// promises a reply that is still coming, and this run has already
		// answered.
		streamErr = err
		content = text
	}
	if !hasVisibleChar(content) {
		// No bubble to close and nothing to say. Ordinarily that is the end of
		// it — but if the guard closed this round's bubble it said "还在处理，
		// 完成后我再单独回复你", and returning here is that promise broken in
		// silence: the user is left waiting for a reply that has already
		// happened. The bubble path above ends an empty completion in words for
		// the same reason; after the guard the words go out as the separate
		// reply instead.
		//
		// The promise is what makes this safe to send at all. One is filed only
		// for a round this adapter opened a bubble for — whichever of the three
		// ways left it (roundTurn.Promised) — so it is itself the proof that a
		// WeCom round is waiting on these words: no binding row is consulted
		// and no session that never asked anything here is written to.
		if !t.Promised || !t.Addr.known() || o.senders == nil {
			return roundAddress{}, errNoWordsOwed
		}
		return t.Addr, o.senders.sendTextCtx(ctx, t.Addr.InstallationID, t.Addr.ChatID, t.Addr.ChatType, streamCopyNoReply)
	}
	addr, err := o.sendAsMessage(ctx, sessionID, content)
	if err != nil && errors.Is(streamErr, errWordsMayBeOnScreen) {
		// The closing frame reached the wire and lost only its verdict, so the
		// answer may be sealing the bubble this very moment. What the fallback
		// came back with says nothing about that, and on its own it reads as
		// "the user has nothing" — which is how a round gets handed back for a
		// later publisher to print the same answer into a second time, and how
		// the attempt this subscriber books becomes a second copy of the reply.
		//
		// Read as the mark the ack wait raised rather than as "an outcome not on
		// the list of repeatable ones" (ws_sender.go). The two agree on every
		// failure this package produces today; where they differ is a failure it
		// does not, and that one is a send that never happened, not a send that
		// might have.
		return addr, errors.Join(errWordsMayBeOnScreen, streamErr, err)
	}
	return addr, err
}

// sendAsMessage pushes an answer to the chat this session is bound to, for a
// round with no bubble left to put it in — a restart mid-run, a stream past its
// window, a frame the server refused. It returns where it spoke, so a round
// whose note never held an address learns one.
//
// For a round the guard closed at nine minutes this message IS the separate
// reply it promised, which is why the ledger settles on the strength of it:
// left owed, the promise would be claimed by the next repeat of this run's
// failure and tell the user "这次没跑通" underneath the answer they just read.
func (o *Outbound) sendAsMessage(ctx context.Context, sessionID pgtype.UUID, content string) (roundAddress, error) {
	binding, err := o.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   channelTypeWecom,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Not a wecom session (Slack / Lark / web-only).
			return roundAddress{}, errNothingToSay
		}
		return roundAddress{}, fmt.Errorf("wecom: lookup chat binding: %w", err)
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		return roundAddress{}, fmt.Errorf("wecom: load installation: %w", err)
	}
	if inst.Status != string(InstallationActive) {
		// Revoked between trigger and reply.
		return roundAddress{}, errNothingToSay
	}
	if o.senders == nil {
		return roundAddress{}, errors.New("wecom: sender registry not configured")
	}
	sender := o.senders.get(inst.ID)
	if sender == nil {
		// No live WS for this installation on this replica. Two causes:
		// (1) the Supervisor lost the lease or is mid-reconnect — transient,
		// and the socket is usually back within seconds;
		// (2) on a multi-replica deployment the lease is held by a DIFFERENT
		// replica than the one that published this event, so it can never be
		// delivered from here (see the single-replica constraint in this
		// file's header).
		//
		// An ANSWER carrying this comes back for it. This comment used to say
		// the opposite — that buffering is wrong because the reply is stale by
		// the time a socket returns — and answerRetryCause overrules that for
		// the answer specifically: a stale reply beats none, the chain is over
		// inside sixteen minutes against a one-hour roundMemory, and case (1)
		// is exactly what an attempt behind a reconnect is for. Case (2) spends
		// its attempts and gives up, which costs nothing a delivery from here
		// was ever going to have.
		//
		// The caller's WARN therefore arrives at the END of the chain rather
		// than on the first try, and sayTheAnswer returns nil until then. That
		// is what a booked retry costs everywhere, and sayTheAnswer's own doc
		// states the price: the answer lives in a time.AfterFunc closure and a
		// restart in that window loses it silently.
		return roundAddress{}, errors.New("wecom: connection not ready on this replica")
	}
	addr := roundAddress{
		InstallationID: inst.ID,
		ChatID:         binding.ChannelChatID,
		ChatType:       aibotChatTypeFromChannel(channel.ChatType(binding.ChatType)),
	}
	return addr, sender.sendTextCtx(ctx, addr.ChatID, addr.ChatType, content)
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
