package wecom

// typing_indicator.go — WeCom's answer to "the bot heard you".
//
// Slack stamps 👀 on the user's message, Feishu a typing badge. WeCom has
// neither: the smart-bot protocol publishes no reaction, no read receipt and
// no typing signal at all. What it does have is the streaming message, so the
// same engine.TypingNotifier interface means something different here — the
// indicator IS the reply, opened early and filled in later. Two consequences
// follow from that, and both shape this file:
//
//   - The bubble MUST be closed. A reaction that never clears is untidy; a
//     stream that never finishes is a spinner sitting in the user's chat for
//     good. So every way a turn can end — answered, failed, never started,
//     outlived its window — writes a closing frame carrying visible text.
//   - OnSettled is not the normal ending. As on the other platforms the Router
//     only calls it when the flush produced no task run; the answer closes the
//     bubble from the chat-done subscriber in outbound.go, which is the only
//     place that has the answer to close it with.

import (
	"context"
	"errors"
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

// The four ways a streaming reply ends in something other than an answer. Each
// one closes the loading bubble the question opened, so each one has to carry
// visible text — WeCom discards a closing frame it considers empty and the
// bubble spins on forever (see hasVisibleChar in ws_frame.go).
//
// Chinese, unconditionally, the same way the rest of this adapter's
// user-facing strings are: WeCom deployments are China-only.
const (
	// streamCopyNoReply — the agent finished with nothing to say.
	streamCopyNoReply = "（这轮没有需要回复的内容）"
	// streamCopyMerged closes a QUEUED round's bubble whose run finished with
	// nothing of its own to say — the reply ahead of it already covered this
	// message. A first round's empty finish keeps streamCopyNoReply; this one
	// has an earlier answer to point at.
	streamCopyMerged = "✅ 这条已并入上一条回复一起处理了。"
	// streamCopyNotStarted — no run was triggered at all (agent offline or
	// archived, or the enqueue failed); the replier's own notice follows as a
	// separate message with the detail.
	streamCopyNotStarted = "已收到，但这条暂时没能开始处理。"
	// streamCopyFailed — the run failed.
	streamCopyFailed = "⚠️ 这次没跑通，请稍后再试一次。"
	// streamCopyCancelled — the run was cancelled, so no answer is coming.
	// Separate copy from streamCopyFailed on purpose: "试一次" invites a retry
	// of something the user just stopped on purpose.
	streamCopyCancelled = "⏹️ 这次处理已取消。"
	// streamCopyStillWorking — the run outlived the protocol's stream window,
	// so we close the bubble ourselves and answer separately later.
	streamCopyStillWorking = "还在处理，完成后我再单独回复你。"
)

// streamCloseTimeout bounds a closing frame written from a timer or a bus
// subscriber, neither of which has a caller's context to inherit.
//
// It is also the budget the ledger hands every delivery (deliveryBudget in
// stream_store.go), which is what puts a ceiling on the bus subscribers here: a
// subscriber that spends its whole budget waiting out another delivery for the
// same run, and then speaks, holds the publishing goroutine for this twice over.
// The alternative is a delivery with nothing to speak in and news nobody else
// holds.
const streamCloseTimeout = 10 * time.Second

// endingRetryAfter is how long an ending this manager could not deliver waits
// before trying again, and endingRetryAttempts is how many times EACH of the two
// causes that book may do so. The delay doubles, so a chain that meets only one
// cause runs its three attempts 15s, 45s and 105s after the ending was first
// attempted — long enough for a Supervisor reconnect to have happened, short
// enough that all of them are over well inside roundMemory.
//
// Measured from the FIRST attempt, not from the one that just failed. An attempt
// can spend its whole streamCloseTimeout parked on another delivery before it
// gives up, so a schedule measured from the end restarts its clock after every
// wait: the same three attempts then land at 15s, 55s and 125s, and the chain
// runs half a minute past what this says. See endingRetries.next.
//
// A delivery that never reports cannot exhaust a chain of deferrals, and the
// numbers are why: sweepLocked retires a reservation at inFlightMaxAge, which is
// 60s, so the attempt at 105s finds it gone and speaks. The two before it do
// not — at 15s and 45s the holder is still inside its age and they park and
// defer again — so the third attempt is doing the work here, not the second.
//
// Two things book them, and they are counted apart because they are two
// different failures with two different cures. A send that nothing accepted —
// which on a settled round has no other publisher at all (see settleRound) — is
// waiting out a socket. An ending whose wait for another delivery ran past its
// caller's budget (errEndingDeferred) is waiting out a publisher that is still
// speaking; the news is then still this one's to deliver and it has no time left
// to do it in, so it comes back with a budget of its own. Sharing one counter
// let a round that lost three waits arrive at a refused send with nothing left,
// which is the case the schedule was built for in the first place.
const (
	endingRetryAfter    = 15 * time.Second
	endingRetryAttempts = 3
)

// retryCause is why another attempt is being booked. Each bookable cause has its
// own count on endingRetries; the delay comes off the total.
type retryCause int

const (
	// retryNone — no attempt may be booked at all, which is the zero value on
	// purpose: an outcome nobody has classified books nothing rather than
	// spending whichever cause happened to be first in this list. next refuses
	// it, so a caller that ignores the ok alongside it still cannot repeat words
	// that may be on the screen.
	retryNone retryCause = iota
	// retryDeferred — the wait for another delivery of the same run outlasted
	// this attempt's budget, so nothing was said and nothing recorded.
	retryDeferred
	// retryRefused — the delivery was turned away, by the server or by the
	// socket. Only ever booked where this package can name the words as never
	// having reached the user: an errcode the server stated, or a socket write
	// that failed before the frame was on the wire. An outcome that is merely
	// unknown is not repeated (deliveryCanBeRepeated).
	//
	// "Refused" rather than "sent and rejected" on purpose — errFrameNotOnTheWire
	// books here too, and in that case nothing went out at all.
	retryRefused
)

// endingRetries is what one round's ending has already spent trying to reach the
// user: when it was first attempted, and how many attempts each cause has taken.
// It travels with the retry rather than living on the manager, so two rounds
// retrying at once cannot spend each other's attempts.
type endingRetries struct {
	// first is when the ending was first attempted, which is what every slot in
	// the schedule is measured from.
	first time.Time
	// deferred and refused are the two causes' counts. Capped separately at
	// endingRetryAttempts, so at most six attempts exist and the last of them is
	// inside sixteen minutes of the first — still far inside roundMemory, past
	// which the note holding the debt is gone and there is nothing left to say
	// it against.
	deferred int
	refused  int
}

// begin stamps when this ending was first attempted. Idempotent: every later
// attempt in the chain carries the same stamp, which is what keeps the schedule
// from restarting.
func (r endingRetries) begin(now time.Time) endingRetries {
	if r.first.IsZero() {
		r.first = now
	}
	return r
}

// next books one more attempt for cause and reports how long from now it should
// run, along with the counts to carry into it. ok is false once that cause has
// spent its attempts, and for retryNone, which is not a cause anything may be
// booked under.
//
// The slot is cumulative over BOTH causes — base, then base*3, then base*7 —
// so a round that keeps failing keeps backing off however it is failing. A slot
// already in the past, which is what an attempt that outlasted its own slot
// leaves, runs as soon as the timer can rather than in the past.
func (r endingRetries) next(cause retryCause, base time.Duration, attempts int, now time.Time) (endingRetries, time.Duration, bool) {
	switch cause {
	case retryRefused:
		if r.refused >= attempts {
			return r, 0, false
		}
		r.refused++
	case retryDeferred:
		if r.deferred >= attempts {
			return r, 0, false
		}
		r.deferred++
	default:
		return r, 0, false
	}
	at := r.first.Add(base * time.Duration(1<<(r.deferred+r.refused)-1))
	if wait := at.Sub(now); wait > 0 {
		return r, wait, true
	}
	return r, 0, true
}

// endingRetryCause reads what the ledger handed back and says which cause the
// next attempt would be booked under, or that there is no safe next attempt.
//
// Three answers, and the third is the one worth naming: a delivery whose
// outcome this package cannot account for may already be on the user's screen,
// so saying it again is how one ending becomes two messages. That group is an
// ack that never came back and a context that ran out — both leave a frame that
// did reach the wire with no verdict on it.
//
// It is NOT every failure. A server's errcode is an answer, and since
// errFrameNotOnTheWire a socket write that failed is one too: the frame never
// left. Both are repeatable. See deliveryCanBeRepeated.
//
// This is the notices' rule and not the answer's. A notice dropped here still
// has a sweeper tick, a repeat on the bus or WeCom's own redelivery behind it,
// which is what makes it affordable to answer "no" to a failure nobody has met
// before. An answer has no publisher behind it at all, so it reads the question
// from the other end — see answerRetryCause (outbound.go).
func endingRetryCause(err error) (retryCause, bool) {
	if errors.Is(err, errEndingDeferred) {
		return retryDeferred, true
	}
	if deliveryCanBeRepeated(err) {
		return retryRefused, true
	}
	return retryNone, false
}

// taskLookup resolves a task id to the chat session it belongs to. Both
// publishers of task:failed stamp the session whenever the task row has one,
// so this is the fallback for a payload that does not, and the row the round
// matcher reads to resolve an auto-retry clone. *db.Queries satisfies it.
type taskLookup interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
}

// taskOrigin is the task row plus where the run's input came from. The typing
// indicator needs both halves: the row to resolve an auto-retry clone and to
// recover a session no publisher put on the event, and the provenance stamp to
// decide whether a failed run's notice belongs in the WeCom room at all.
// outbound.go asks the same stamp about an answer, through its own
// outboundQueries. Kept apart from taskLookup because the round matcher only
// ever needs the row. *db.Queries satisfies it.
type taskOrigin interface {
	taskLookup
	engine.ChannelProvenanceQueries
}

// chatBindingLookup finds which WeCom chat a session belongs to. It is the
// address of last resort for a failure notice: the handle is gone and this
// process has no note of the round, which is what a restart mid-run looks like.
// The two queries are the ones outbound.go already makes on the same path for
// the answer, so *db.Queries satisfies both interfaces without a new query.
type chatBindingLookup interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// TypingIndicatorManager opens a streaming bubble per round when messages are
// ingested and owns each one until something closes it.
type TypingIndicatorManager struct {
	senders  *sendersRegistry
	streams  *streamStore
	tasks    taskOrigin
	bindings chatBindingLookup
	log      *slog.Logger

	// guardAfter is when the manager closes a bubble nobody else has. Zero
	// disables the guard (tests that drive the clock themselves).
	guardAfter time.Duration

	// endingRetryAfter is the first delay before an ending that did not reach
	// the user is tried again. Negative disables the retry.
	endingRetryAfter time.Duration
}

// TypingIndicatorConfig wires the manager. Senders and Streams are the same
// two instances the outbound subscriber holds — the bubble is opened on one
// side of the process and closed on the other.
type TypingIndicatorConfig struct {
	Senders *sendersRegistry
	Streams *streamStore
	Logger  *slog.Logger

	// Tasks answers where a run's input came from, and resolves a task id to
	// its chat session for a task:failed that carries none. Nil leaves the
	// origin question unanswerable, so every failed run this process holds no
	// round for is refused rather than announced — see failureBelongsOnWecom
	// for what this manager does when it cannot ask.
	Tasks taskOrigin

	// Bindings finds the chat behind a session when nothing else can — a run
	// that failed after this process was restarted, or after a bubble that was
	// never painted. Nil keeps the failure notice to the rounds this process
	// has a handle or a note for.
	Bindings chatBindingLookup

	// GuardAfter overrides streamGuardAfter. Test-only.
	GuardAfter time.Duration

	// EndingRetryAfter overrides endingRetryAfter. Test-only.
	EndingRetryAfter time.Duration
}

var _ engine.TypingNotifier = (*TypingIndicatorManager)(nil)

// NewTypingIndicator builds the manager.
func NewTypingIndicator(cfg TypingIndicatorConfig) *TypingIndicatorManager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	guard := cfg.GuardAfter
	if guard == 0 {
		guard = streamGuardAfter
	}
	retry := cfg.EndingRetryAfter
	if retry == 0 {
		retry = endingRetryAfter
	}
	return &TypingIndicatorManager{
		senders:          cfg.Senders,
		streams:          cfg.Streams,
		tasks:            cfg.Tasks,
		bindings:         cfg.Bindings,
		log:              logger,
		guardAfter:       guard,
		endingRetryAfter: retry,
	}
}

// TypingIndicatorWiring reports which of the four dependencies a manager holds.
//
// Every one of them is optional and every one of them narrows the manager
// silently when it is missing: nothing panics, nothing logs, Register still
// subscribes, and the events still arrive — the closing frame just never gets
// written, which the user sees as a bubble that spins until the guard replaces
// it with a promise nobody keeps. That makes "is it wired" unfalsifiable from
// the outside, and a boot path that drops one looks exactly like a healthy
// one. This is the inspection point that makes it falsifiable.
type TypingIndicatorWiring struct {
	// Senders is the live WebSocket registry. Without it no closing frame and
	// no plain-message fallback can be written at all.
	Senders bool
	// Streams is the round store shared with the outbound subscriber. Without
	// it every handler returns on its first line, so no bubble is ever closed
	// by any ending.
	Streams bool
	// Tasks reads the run's input batch, which is what establishes that the
	// question was asked over WeCom and not by the installer in their own
	// browser. Without it every failed run this process holds no round for is
	// refused instead of announced, so a run that outlived its bubble tells
	// the user nothing. It also resolves an auto-retry clone to the round its
	// parent opened, and recovers the session for a task:failed carrying none.
	Tasks bool
	// Bindings finds the chat behind a session when no round is on file.
	// Without it a run that fails after its bubble is gone (guard closed it at
	// nine minutes, or the process restarted mid-run) tells the user nothing,
	// and the guard's "I'll reply separately" is never answered.
	Bindings bool
}

// Wiring reports the dependencies this manager was built with. For boot-wiring
// guards; it copies four booleans and hands out no references.
func (m *TypingIndicatorManager) Wiring() TypingIndicatorWiring {
	return TypingIndicatorWiring{
		Senders:  m.senders != nil,
		Streams:  m.streams != nil,
		Tasks:    m.tasks != nil,
		Bindings: m.bindings != nil,
	}
}

// OnIngested paints a "working on it" bubble for the run this message belongs
// to and records what it takes to come back and fill it in. Which run that is
// comes from batch — the engine debouncer's own verdict, decided under the
// lock that arms the window — so the first message of a run paints a bubble
// and the rest join it, and a message the debouncer gave a run of its own gets
// a bubble of its own immediately, because a wait with nothing on screen reads
// as a message that was lost. The bubble carries no words while it waits: the
// think tag renders as the client's own animated dots, which is the receipt,
// and words would need a language before there is anything to say.
//
// The Router calls this on a detached goroutine with its own deadline, so
// nothing here needs to be quick for the ACK's sake — but everything here is
// best-effort: a bubble that fails to open costs the user a few seconds of
// uncertainty, and the answer still arrives as a plain message.
func (m *TypingIndicatorManager) OnIngested(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID, batch engine.RunBatchID) {
	if m.senders == nil || m.streams == nil || !sessionID.Valid || batch == 0 {
		return
	}
	// A standalone /issue is answered by the replier and deliberately never
	// triggers an agent run, so no chat-done event would ever arrive to close
	// a bubble opened for it.
	if msg.SkipAgentRun {
		return
	}
	wm, err := wecomMsgFromRaw(msg)
	if err != nil {
		m.log.WarnContext(ctx, "wecom typing: cannot read the inbound envelope",
			"chat_session_id", util.UUIDToString(sessionID), "error", err)
		return
	}
	// Without the callback's req_id there is no stream to open: the server
	// refuses a frame carrying anything else, and an event callback's req_id
	// is refused outright (846605).
	if wm.ReqID == "" {
		return
	}
	chatID := msg.Source.ChatID
	if chatID == "" {
		chatID = wm.ChatID
	}
	if chatID == "" {
		return
	}

	h := streamHandle{
		ReqID:          wm.ReqID,
		StreamID:       newStreamID(),
		InstallationID: inst.ID,
		ChatID:         chatID,
		ChatType:       aibotChatTypeFromChannel(msg.Source.ChatType),
	}
	if m.streams.open(sessionID, batch, h) != roundOpened {
		// roundJoined — the batcher folded this message into a run whose
		// bubble is already on screen, and that bubble is this message's
		// receipt too. roundFinished — this goroutine outlived the run it was
		// painting for, and a bubble now would be one nothing ever closes.
		return
	}

	// Three ways the opening frame does not land, and only one of them is a
	// reason to give the bubble up.
	//
	// An ack that did not come back says nothing about whether the frame did:
	// re-sending the same stream id later creates the message if the opening
	// frame was lost, so the worst case is a user who waits without a spinner
	// rather than one who never gets an answer.
	//
	// Busy and superseded mean something stronger. Both say another frame on
	// this req_id got to the socket first, and that frame carried a stream id
	// of its own, which is exactly how a bubble is created. So the spinner is
	// on the user's screen. Dropping the handle there arms no guard and leaves
	// nothing that could ever close it.
	//
	// A verdict from the server is the one that ends it: 846605 and 846608 mean
	// this stream will never take a frame, so no bubble was painted and keeping
	// the handle would swallow the answer rather than deliver it.
	if err := m.senders.stream(ctx, h, streamThinkingPlaceholder, false); err != nil {
		switch {
		case errors.Is(err, errStreamAckTimeout),
			errors.Is(err, errStreamBusy),
			errors.Is(err, errStreamSuperseded):
			m.log.DebugContext(ctx, "wecom typing: opening frame did not land, keeping the handle",
				"chat_session_id", util.UUIDToString(sessionID), "error", err)
		default:
			m.streams.drop(sessionID, batch)
			m.log.WarnContext(ctx, "wecom typing: opening frame refused",
				"chat_session_id", util.UUIDToString(sessionID), "error", err)
			return
		}
	}
	m.armGuard(sessionID, batch)
}

// OnRunStarted files the task the debounced flush created for this run. It is
// the binding every later ending is matched on: the answer, the failure and
// the cancellation all name a task, and this is what turns that name into "the
// bubble this question opened" without reading anything off arrival order.
//
// It can arrive before OnIngested has painted the bubble — the Router detaches
// the ingest goroutine and the flush runs on the batcher's timer — so the
// store files the run either way and the bubble attaches to it when it lands.
func (m *TypingIndicatorManager) OnRunStarted(_ context.Context, sessionID pgtype.UUID, batch engine.RunBatchID, taskID pgtype.UUID) {
	if m.streams == nil || !sessionID.Valid || !taskID.Valid {
		return
	}
	m.streams.bind(sessionID, batch, util.UUIDToString(taskID))
}

// OnSettled closes the bubble of a round that never became a run — agent
// offline or archived, or an enqueue that failed. This is the only chance to
// stop that spinner: with no task there is no task lifecycle event, so neither
// the chat-done subscriber nor the failure subscriber will ever fire. The copy
// is deliberately thin because the replier's own notice follows as a separate
// message with the reason.
//
// batch names which bubble: the flush that settled reports the run it was
// answering, so a session with several rounds open closes the right one
// instead of whichever happens to be newest.
func (m *TypingIndicatorManager) OnSettled(ctx context.Context, sessionID pgtype.UUID, batch engine.RunBatchID) {
	if m.senders == nil || m.streams == nil || !sessionID.Valid {
		return
	}
	m.settleRound(ctx, sessionID, batch, endingRetries{})
}

// settleRound says the settled flush's ending, and books itself again when it
// can establish that nothing reached the user.
//
// The retry is the part this path cannot do without. Every other ending has a
// second publisher — a sweeper tick, a repeat on the bus, WeCom's own
// redelivery — so a send refused during a reconnect window is one the next one
// makes good. This one has none: exactly one of OnRunStarted and OnSettled fires
// per batch (engine/resolvers.go), and with no task there is no lifecycle event
// behind it either. So when both routes fail, the ledger keeps the round under
// the batch's own name — the bubble itself when the socket took nothing, a debt
// otherwise — and this books the publisher that comes for it.
//
// It goes back through sayEnding rather than re-sending on its own, which is
// what keeps it inside the ledger: if anything else closed the round in the
// meantime the repeat finds nothing owed and says nothing, and if this attempt
// fails too what it left goes back for the next one — the debt, or, when the
// socket took nothing at all, the round and the bubble it was holding.
func (m *TypingIndicatorManager) settleRound(ctx context.Context, sessionID pgtype.UUID, batch engine.RunBatchID, retries endingRetries) {
	retries = retries.begin(time.Now())
	_, err := m.streams.sayEnding(ctx, sessionID, byBatch(batch), roundOver, nil,
		func(ctx context.Context, t roundTurn) (roundAddress, error) {
			if t.HasBubble {
				return t.Handle.address(), m.writeClosing(ctx, sessionID, t.Handle, streamCopyNotStarted, "settled")
			}
			// No bubble: an earlier attempt consumed it and could not account
			// for the words, so what is on the user's screen is a spinner
			// nothing can seal any more. The chat it is in is on the note, and
			// the same sentence goes there as an ordinary message under it.
			if !t.Promised || !t.Addr.known() {
				return roundAddress{}, errNothingToSay
			}
			return t.Addr, m.sayAsPlainMessage(ctx, sessionID, t.Addr, streamCopyNotStarted)
		})
	if err == nil || errors.Is(err, errNothingToSay) {
		return
	}
	// Everything else leaves this round still owing the user words, and the two
	// ways it can are the same instruction. A send nothing accepted left the
	// debt on the note for whoever spends it next. A wait that outlasted this
	// attempt's budget (errEndingDeferred) recorded nothing at all — the guard
	// is the one publisher this round can be waiting behind, and its promise
	// ends nothing, so this settle is still the only closer a run that never
	// started will ever have.
	//
	// The third way is the one that must NOT come back: a delivery that reached
	// the wire and then lost its ack. Those words may be on the screen already,
	// and a settle is the one ending whose repeat nothing would deduplicate —
	// the bubble is gone, so the repeat goes out as another plain message.
	if !m.sayItAgain(retries, err, func(ctx context.Context, retries endingRetries) {
		m.settleRound(ctx, sessionID, batch, retries)
	}) {
		m.log.WarnContext(ctx, "wecom typing: a settled round's ending will not be said again",
			"chat_session_id", util.UUIDToString(sessionID), "batch", uint64(batch),
			"deferred", retries.deferred, "refused", retries.refused, "error", err)
	}
}

// sayItAgain books another attempt at an ending the ledger handed back
// undelivered, and reports whether one is coming. It is the one place the two
// halves of that decision meet: WHETHER these words can safely be said again,
// and whether that cause has any attempts left.
func (m *TypingIndicatorManager) sayItAgain(retries endingRetries, err error, again func(context.Context, endingRetries)) bool {
	cause, ok := endingRetryCause(err)
	if !ok {
		return false
	}
	return m.bookEndingRetry(retries, cause, again)
}

// bookEndingRetry schedules another attempt at an ending that did not reach the
// user, and reports whether it booked one.
//
// Bounded twice over: endingRetryAttempts caps each cause, and the delay doubles
// over both of them together, so the last attempt of even a chain that meets
// both lands inside sixteen minutes of the first. That is the shape of what it
// is waiting out — a Supervisor reconnect takes seconds, and a delivery it lost
// the wait to is bounded by streamCloseTimeout — and it stays far inside
// roundMemory, past which the note holding the debt is gone and there is nothing
// left to say it against.
//
// The attempt runs on a budget of its own rather than the caller's, because the
// caller's is what ran out. It goes back through sayEnding like every other
// publisher, so an ending anything else delivered in the meantime finds nothing
// owed and says nothing.
func (m *TypingIndicatorManager) bookEndingRetry(retries endingRetries, cause retryCause, again func(context.Context, endingRetries)) bool {
	if m.endingRetryAfter <= 0 {
		return false
	}
	next, wait, ok := retries.next(cause, m.endingRetryAfter, endingRetryAttempts, time.Now())
	if !ok {
		return false
	}
	time.AfterFunc(wait, func() {
		ctx, cancel := context.WithTimeout(context.Background(), streamCloseTimeout)
		defer cancel()
		again(ctx, next)
	})
	return true
}

// Register subscribes the manager to the two ways a run ends without an
// answer. Both have to be here or the bubble outlives its run: a failure and a
// cancellation each publish nothing the outbound subscriber reads, so nothing
// else would ever seal that stream.
//
// EventChatDone is deliberately NOT subscribed here: the answer belongs in the
// bubble, and only the outbound subscriber holds the answer. Registering for
// it here would close the bubble first and leave the reply to arrive
// underneath it.
func (m *TypingIndicatorManager) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventTaskFailed, m.handleTaskFailed)
	bus.Subscribe(protocol.EventTaskCancelled, m.handleTaskCancelled)
}

// handleTaskFailed says a run died, in the bubble if there still is one and as
// a plain message if there is not.
//
// Both publishers of task:failed stamp chat_session_id when the task row has
// one: service.taskEvent, which FailTask goes through, and the sweeper's own
// envelope in HandleFailedTasks (recover-orphans and the daemon heartbeat
// timeout come down that path). So the session is normally on the event, and
// sessionFor's read of it off the task row is a fallback for a payload neither
// publisher produces today. It is kept because a bubble left spinning is a
// failure nobody reports: nine minutes on, the guard replaces it with "still
// working, I'll reply separately" — a promise about a run that has been dead
// the whole time.
//
// The bubble is not the whole of it. A handle is consumed by whichever ending
// gets there first, and the guard is allowed to be that one at the nine-minute
// mark while the run carries on — so every run longer than nine minutes that
// then failed finds no handle here. This notice is the only "that run did not
// go through" WeCom ever produces: the replier speaks for needs_binding,
// offline, archived and issue_created and for nothing else. So the handle is
// one address among three, and sayTheRunFailed works down the rest.
func (m *TypingIndicatorManager) handleTaskFailed(e events.Event) {
	if m.streams == nil {
		return
	}
	// An attempt the platform is already retrying is not an ending. FailTask
	// publishes task:failed for it anyway — the web card has to clear — and
	// flags it retry_pending so consumers stay quiet; taskFailedFields even
	// withholds the error text, and dingtalk's outbound already honours it.
	// Closing the bubble here would tell the user "这次没跑通" about an attempt
	// whose replacement is already queued, and the retry's answer would then
	// land underneath a bubble that had declared failure. The round stays open
	// for the attempt that reports the real outcome; the retry clone's own
	// events find it through the batch owner it inherited (roundTaker).
	if retryPending(e) {
		return
	}
	if m.bindings == nil && !m.streams.holding() {
		// Nothing on file, and no way to find a chat: no reason to read a row
		// for someone else's run.
		return
	}
	sessionID, ok := m.sessionFor(e)
	if !ok {
		return // an issue / autopilot run, with no chat session and no bubble
	}
	taskID := taskIDFromEvent(e)

	// Everything this handler asks a database runs on the goroutine that
	// published the event, so it gets the subscriber's own budget rather than
	// the ten seconds a closing frame may spend on the wire. See
	// taskLookupTimeout.
	dbCtx, cancelDB := context.WithTimeout(context.Background(), taskLookupTimeout)
	defer cancelDB()

	// Is this session WeCom's at all? Asked first, and cheapest first, because
	// task:failed fires for every run in the deployment — Slack's, Lark's,
	// DingTalk's, the web UI's — and none of them are worth a query here.
	//
	// A session this process holds a round or a note for is ours on local
	// evidence, at no cost. Everything else has to ask the binding row, and
	// that one lookup is the answer: engine.TaskInputIsChannelIngested cannot
	// give it, because it reports whether the input came from A channel, not
	// from this one — a failed Slack run passes it. The address is carried
	// down to sayTheRunFailed so the same row is not read twice.
	var bound roundAddress
	if !m.streams.knowsSession(sessionID) {
		found, ours := m.addressFromBinding(dbCtx, sessionID)
		if !ours {
			return
		}
		bound = found
	}

	// A run started in the browser against this same session gets its notice
	// there. Announcing it here would tell everyone in the room that something
	// they never saw has gone wrong. Asked BEFORE the bubble is taken, because
	// a gate placed after the take has already sealed a WeCom round's bubble
	// with a web run's ending. The answer path orders its own gate the same
	// way, ahead of sayEnding — see the block above the gate in
	// outbound.go's processEvent.
	if !m.failureBelongsOnWecom(dbCtx, sessionID, taskID) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), streamCloseTimeout)
	defer cancel()
	m.sayFailedRun(ctx, sessionID, taskID, bound, endingRetries{})
}

// sayFailedRun is handleTaskFailed past its gates: the ledger call and what to
// do when the ledger hands the notice back undelivered.
//
// Kept apart from the handler because the retry has to re-enter HERE. The gates
// above answered where this run was asked, and that answer cannot change; asking
// them again would be two more reads per attempt on a bus event this deployment
// publishes for every run in it.
//
// The deferral is one of the two cases worth naming. task:failed has two
// publishers, so a notice can arrive while another delivery for the same run is
// on the wire, and waiting it out is what keeps the two from both speaking. A
// wait that loses to this attempt's budget reserved nothing and recorded nothing
// — so unless something else says this run's ending, streamCopyFailed is still
// owed, and this publisher is still the one holding it. It is also the reply the
// guard's promise promised whenever the delivery ahead was the guard's.
//
// The other is a send the socket turned away, which is the commoner failure of
// the two: a reconnect window refuses everything, and the second publisher this
// notice has may not fire again at all — the sweeper repeats a run it timed out,
// not one FailTask already reported. So a refusal comes back on the same
// schedule as a deferral, under a count of its own, and only when the refusal is
// one this package can name as never having reached the user.
func (m *TypingIndicatorManager) sayFailedRun(ctx context.Context, sessionID pgtype.UUID, taskID string, bound roundAddress, retries endingRetries) {
	retries = retries.begin(time.Now())
	_, err := m.rounds().sayEnding(ctx, sessionID, byTask(taskID), roundOver,
		func(ctx context.Context, t roundTurn) (roundAddress, error) {
			if !t.HasBubble && !t.Addr.known() && bound.known() {
				t.Addr = bound
			}
			return m.sayTheRunFailed(ctx, sessionID, t)
		})
	if err == nil || errors.Is(err, errNothingToSay) {
		return
	}
	if !m.sayItAgain(retries, err, func(ctx context.Context, retries endingRetries) {
		m.sayFailedRun(ctx, sessionID, taskID, bound, retries)
	}) {
		m.log.WarnContext(ctx, "wecom typing: a failed run's notice will not be said again",
			"chat_session_id", util.UUIDToString(sessionID), "task_id", taskID,
			"deferred", retries.deferred, "refused", retries.refused, "error", err)
	}
}

// failureBelongsOnWecom asks where this run's input came from: the channel, or
// somewhere else? The engine makes the INSTALLER the creator of a group's
// chat_session, so that session appears in their own Multica chat list and they
// can ask it something in a browser. Both runs fail the same way, on the same
// bus, carrying the same session — nothing in the event says which surface
// asked.
//
// The answer has the same exposure, and outbound.go makes the same
// engine.TaskInputIsChannelIngested call before it consumes the round. The two
// endings of a run are decided by one stamp.
//
// This is an authorization check on writing into somebody else's group chat,
// so uncertainty is not permission. A lookup that did not answer is not
// evidence the question came from WeCom, and "one line of copy naming no
// question and no answer" still tells a room that activity it cannot see went
// wrong — the existence of the activity is the disclosure. So an origin that
// cannot be established refuses, and says so at WARN.
//
// That costs nothing on the case worth protecting, because that case has local
// evidence. A round sitting on the guard's "还在处理，完成后我再单独回复你" has
// this run on its owed list, and a round still open has it bound; both are
// written only by the inbound path, and both are read here before anything is
// consumed. So a WeCom round whose promise is outstanding is delivered while
// the database is down, and it is only the runs this process has never seen
// that have to produce a row to be spoken for.
func (m *TypingIndicatorManager) failureBelongsOnWecom(ctx context.Context, sessionID pgtype.UUID, taskID string) bool {
	// Positive proof of origin, held in memory. Asked first, so the paths
	// below never decide the one case where silence breaks a promise.
	if m.streams.knowsRound(sessionID, taskID) {
		return true
	}
	if taskID == "" {
		// Both task:failed publishers carry one in production — see the block
		// comment above handleTaskFailed — so this is a payload shape nothing
		// real produces, and it names no run to attribute.
		m.refuseUnknownOrigin(ctx, sessionID, taskID, "no task id on the event")
		return false
	}
	if m.tasks == nil {
		m.refuseUnknownOrigin(ctx, sessionID, taskID, "no task lookup configured")
		return false
	}
	id, err := util.ParseUUID(taskID)
	if err != nil || !id.Valid {
		m.refuseUnknownOrigin(ctx, sessionID, taskID, "unparseable task id")
		return false
	}
	task, err := m.tasks.GetAgentTask(ctx, id)
	if err != nil {
		m.refuseUnknownOrigin(ctx, sessionID, taskID, "cannot read the task row: "+err.Error())
		return false
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, m.tasks, task)
	if err != nil {
		m.refuseUnknownOrigin(ctx, sessionID, taskID, "cannot read the channel-ingested stamp: "+err.Error())
		return false
	}
	return deliver
}

// refuseUnknownOrigin logs a failure notice this process declined to put in a
// WeCom room. WARN because it is a real outcome for the asker — a run of
// theirs ended and they were not told — and the only signal that a database
// the origin check depends on has stopped answering.
func (m *TypingIndicatorManager) refuseUnknownOrigin(ctx context.Context, sessionID pgtype.UUID, taskID, reason string) {
	m.log.WarnContext(ctx, "wecom typing: refusing to announce a failed run whose origin cannot be established",
		"chat_session_id", util.UUIDToString(sessionID), "task_id", taskID, "reason", reason)
}

// handleTaskCancelled seals the bubble of a run the user stopped.
//
// Cancellation is a terminal state that publishes no chat:done and no
// task:failed, so without this the bubble spins for the full nine minutes and
// the guard then promises a separate reply — about a run the user cancelled
// themselves, that will never come. A session with several rounds open gets one
// closing frame per cancelled run, each on its own bubble, because the round is
// matched by the task id the flush bound to it.
//
// This handler is only ever as complete as its publishers. It sees a cancelled
// run when service.TaskService broadcasts task:cancelled for the row —
// CancelTask, CancelQueuedChatTasks for the follow-ups behind it, the
// agent-level and issue-level bulk cancels, and BroadcastCancelledTasks for the
// handlers that cancel inside a transaction. A cancel path that flips the row
// and publishes nothing is invisible here, and the bubble it strands has no
// other closer: the daemon's own completion arrives after the row is already
// cancelled, where CompleteAgentTask's status = 'running' guard matches nothing
// and the answer is discarded without a chat:done. Archiving an agent used to
// be exactly that path (handler.ArchiveAgent).
//
// Unlike a failure this does NOT go looking in the binding row when no round
// is on file. streamCopyFailed is the only "that run did not go through" WeCom
// ever produces, which is why a failure is worth chasing an address for; a
// cancellation was performed by the user, and chasing it would turn one
// "cancel all tasks" click into a message in every chat that agent serves —
// including sessions where WeCom never showed a bubble at all.
func (m *TypingIndicatorManager) handleTaskCancelled(e events.Event) {
	if m.streams == nil {
		return
	}
	// Anything on file at all, not just anything painted. A round bound to a
	// run whose opening frame is still in flight has no bubble yet; returning
	// here would leave it on the open list, and the frame landing afterwards
	// would paint a spinner with no ending left to close it — the cancel is
	// the last event this run produces. Retiring the round instead makes that
	// late paint a no-op.
	if !m.streams.holding() {
		return
	}
	sessionID, ok := m.sessionFor(e)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), streamCloseTimeout)
	defer cancel()
	m.sayCancelledRun(ctx, sessionID, taskIDFromEvent(e), endingRetries{})
}

// sayCancelledRun is handleTaskCancelled's ledger call, and what to do when the
// ledger hands the cancellation back undelivered.
//
// This is the ending with the fewest publishers of all — one broadcast, no
// sweeper repeat, no redelivery — so an attempt that does not reach the user is
// the whole of it unless this comes back. What the asker is left with otherwise
// is whatever the delivery ahead put on the screen: a spinner nothing has
// sealed, or the guard's streamCopyStillWorking promise about a run the user
// themselves stopped.
//
// That reasoning does not distinguish between a wait this attempt lost and a
// send the socket turned away, so neither does this: both come back, on counts
// of their own. A refusal is in fact the commoner of the two — a reconnect
// window refuses every route a cancellation has.
func (m *TypingIndicatorManager) sayCancelledRun(ctx context.Context, sessionID pgtype.UUID, taskID string, retries endingRetries) {
	retries = retries.begin(time.Now())
	_, err := m.rounds().sayEnding(ctx, sessionID, byTask(taskID), roundOver,
		func(ctx context.Context, t roundTurn) (roundAddress, error) {
			if m.senders == nil {
				return roundAddress{}, errNothingToSay
			}
			if t.HasBubble {
				return t.Handle.address(), m.writeClosing(ctx, sessionID, t.Handle, streamCopyCancelled, "task cancelled")
			}
			// No bubble left. If the guard already closed one for this run it
			// promised a separate reply, and that promise is now void — say so,
			// in the chat the promise was made in. Only for THIS round's own
			// promise: a session can hold several, the copy differs per
			// outcome, and announcing a cancel against another round's promise
			// would tell its asker "已取消" about a run nobody stopped.
			if !t.Promised || !t.Addr.known() {
				return roundAddress{}, errNothingToSay
			}
			return t.Addr, m.sayAsPlainMessage(ctx, sessionID, t.Addr, streamCopyCancelled)
		})
	if err == nil || errors.Is(err, errNothingToSay) {
		return
	}
	if !m.sayItAgain(retries, err, func(ctx context.Context, retries endingRetries) {
		m.sayCancelledRun(ctx, sessionID, taskID, retries)
	}) {
		m.log.WarnContext(ctx, "wecom typing: a cancelled run's notice will not be said again",
			"chat_session_id", util.UUIDToString(sessionID), "task_id", taskID,
			"deferred", retries.deferred, "refused", retries.refused, "error", err)
	}
}

// rounds builds the matcher that turns a task id on an event into the round it
// belongs to.
func (m *TypingIndicatorManager) rounds() roundTaker {
	return roundTaker{streams: m.streams, tasks: m.tasks, log: m.log}
}

// retryPending reports whether FailTask has already created a retry child for
// this attempt. taskFailedFields sets it on every task:failed payload.
func retryPending(e events.Event) bool {
	p, ok := e.Payload.(map[string]any)
	if !ok {
		return false
	}
	pending, _ := p["retry_pending"].(bool)
	return pending
}

// sayTheRunFailed delivers a failed run's ending wherever the round can still
// be reached, in the order of how much each address is trusted.
//
// The bubble first, while the round still has one. Then the note the handle
// left behind, which is the chat the question came from and what the guard's
// promise was made in — the binding row may no longer point at it. Failing both
// — a restart mid-run, a turn whose opening frame the server refused — the
// binding row is the only address there is.
//
// A round the store says is accounted for never reaches here at all: the ledger
// answers roundToldAlready and runs no delivery. That is the whole of the
// not-twice rule, and it is per run, so a task:failed arriving behind this run's
// own delivered answer stays quiet while a second run's failure is still told.
func (m *TypingIndicatorManager) sayTheRunFailed(ctx context.Context, sessionID pgtype.UUID, t roundTurn) (roundAddress, error) {
	if m.senders == nil {
		return roundAddress{}, errNothingToSay
	}
	if t.HasBubble {
		return t.Handle.address(), m.writeClosing(ctx, sessionID, t.Handle, streamCopyFailed, "task failed")
	}
	addr := t.Addr
	if !addr.known() {
		found, ok := m.addressFromBinding(ctx, sessionID)
		if !ok {
			return roundAddress{}, errNothingToSay
		}
		addr = found
	}
	return addr, m.sayAsPlainMessage(ctx, sessionID, addr, streamCopyFailed)
}

func (m *TypingIndicatorManager) sayAsPlainMessage(ctx context.Context, sessionID pgtype.UUID, addr roundAddress, text string) error {
	err := m.senders.sendTextCtx(ctx, addr.InstallationID, addr.ChatID, addr.ChatType, text)
	if err != nil {
		m.log.WarnContext(ctx, "wecom typing: could not deliver a run's ending",
			"chat_session_id", util.UUIDToString(sessionID),
			"installation_id", util.UUIDToString(addr.InstallationID), "error", err)
	}
	return err
}

// addressFromBinding reads the chat a session belongs to off the binding row —
// the same two queries outbound.go makes when an answer has no bubble to land
// in. A session with no wecom binding is not ours to speak in: this subscriber
// sees every failed run on a shared bus, including Slack's and the web UI's.
// That makes it the ownership test as much as the address, which is why
// handleTaskFailed asks it before spending anything on the task row.
func (m *TypingIndicatorManager) addressFromBinding(ctx context.Context, sessionID pgtype.UUID) (roundAddress, bool) {
	if m.bindings == nil {
		return roundAddress{}, false
	}
	binding, err := m.bindings.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   channelTypeWecom,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			m.log.WarnContext(ctx, "wecom typing: cannot find the chat a failed run belongs to",
				"chat_session_id", util.UUIDToString(sessionID), "error", err)
		}
		return roundAddress{}, false
	}
	// The installation row answers whether the bot is still installed. The
	// binding row outlives a revoke, so a session keeps looking reachable
	// after the bot has been removed.
	inst, err := m.bindings.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		m.log.WarnContext(ctx, "wecom typing: cannot load the installation a failed run belongs to",
			"installation_id", util.UUIDToString(binding.InstallationID), "error", err)
		return roundAddress{}, false
	}
	if inst.Status != string(InstallationActive) {
		return roundAddress{}, false
	}
	return roundAddress{
		InstallationID: binding.InstallationID,
		ChatID:         binding.ChannelChatID,
		ChatType:       aibotChatTypeFromChannel(channel.ChatType(binding.ChatType)),
	}, true
}

// sessionFor finds the chat session behind a task lifecycle event. The
// sweeper's task:failed does not carry one, so it comes off the task row.
//
// Everything here runs on the goroutine that published the event: the daemon's
// own HTTP handler, or a sweeper tick. Hence the short deadline.
func (m *TypingIndicatorManager) sessionFor(e events.Event) (pgtype.UUID, bool) {
	if sessionID, ok := sessionIDFromEvent(e); ok {
		return sessionID, true
	}
	if m.tasks == nil {
		return pgtype.UUID{}, false
	}
	taskID := taskIDFromEvent(e)
	if taskID == "" {
		return pgtype.UUID{}, false
	}
	id, err := util.ParseUUID(taskID)
	if err != nil || !id.Valid {
		return pgtype.UUID{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskLookupTimeout)
	defer cancel()
	task, err := m.tasks.GetAgentTask(ctx, id)
	if err != nil {
		return pgtype.UUID{}, false
	}
	return task.ChatSessionID, task.ChatSessionID.Valid
}

// taskLookupTimeout is the whole database budget this subscriber may spend on
// somebody else's goroutine — the session lookup, the binding row and the
// origin gate together. The bus is synchronous: a task:failed subscriber runs
// on the goroutine that published the event, and a sweeper tick is not ours to
// hold while a loaded pool answers. streamCloseTimeout is the separate, longer
// budget for putting words on the wire once the decision has been made.
//
// A pool too slow to answer inside it costs a failure notice for a run this
// process holds no record of. The case worth protecting does not depend on it:
// a round with an open bubble or an outstanding promise is proved ours from
// memory and never reaches these queries — see failureBelongsOnWecom.
const taskLookupTimeout = 800 * time.Millisecond

// taskIDFromEvent prefers the envelope's routing hint and falls back to the
// payload. ChatDonePayload matters most: broadcastChatDone sets no TaskID on
// the envelope, and on the in-process bus the payload stays typed — miss it
// and every answer takes the HEAD round unconditionally, which is the wrong
// bubble whenever a guard has already closed the running round's.
func taskIDFromEvent(e events.Event) string {
	if e.TaskID != "" {
		return e.TaskID
	}
	switch p := e.Payload.(type) {
	case protocol.ChatDonePayload:
		return p.TaskID
	case protocol.TaskProgressPayload:
		return p.TaskID
	case map[string]any:
		s, _ := p["task_id"].(string)
		return s
	}
	return ""
}

// armGuard schedules the close that happens when nothing else does. WeCom
// stops accepting frames for a stream past streamMaxAge, so a bubble that
// outlives the window — a long run, or a round stuck in the queue behind one —
// would otherwise become a spinner we can no longer touch. The guard closes
// exactly the round it was armed for, by batch: with several bubbles open in
// one session, a timer that took the head could seal a newer round's bubble
// with an older round's promise.
//
// This is the one closer that does not end the round. Its copy says the reply
// is coming separately, and the run is still going — so the handle it consumes
// leaves a note behind, and whatever the run does next is said against that.
func (m *TypingIndicatorManager) armGuard(sessionID pgtype.UUID, batch engine.RunBatchID) {
	if m.guardAfter <= 0 {
		return
	}
	t := time.AfterFunc(m.guardAfter, func() {
		ctx, cancel := context.WithTimeout(context.Background(), streamCloseTimeout)
		defer cancel()
		m.fireGuard(ctx, sessionID, batch)
	})
	m.streams.arm(sessionID, batch, t)
}

// fireGuard is what the timer does, kept apart from the timer so the guard's
// behaviour has one definition and a test can run it without waiting out the
// nine minutes.
//
// The promise is filed by the ledger, not here, and either way the run comes
// out of this owed an ending. Words that landed put "还在处理，完成后我再单独
// 回复你" on the screen and the promise is that sentence. Words that did not —
// frame refused, fallback unsendable, socket dead — still cost the round its
// bubble, which the ledger consumed under the lock that found it, so the user
// is left watching a spinner nothing can seal; I3 records that as owed too.
// This is the one ending the ledger does not hand its round back to even when
// the words provably went nowhere, and the paragraph below is the reason:
// nobody would come for it. See keepsItsBubble in stream_store.go. The
// difference is only in what the user has already been told, and the run's real
// ending is said in the chat this round was speaking in either way, off the
// note rather than the binding row.
//
// It is the one publisher that books no retry when the ledger hands its words
// back deferred, and the reason is which delivery it can possibly be waiting on.
// A round still open is taken outright, so there is no wait; a round already
// taken is only reachable by batch through batchRunID, which nothing but a
// task-less round is ever filed under, and the sole other publisher of one of
// those is the settled flush. So the delivery ahead of the guard is a settle
// saying why the run never started — the round's real ending, and one that books
// its own retry if it fails. The promise this guard came to make is about a run
// that has already finished having anything to promise.
//
// One round it is refused outright, and the ledger refuses it rather than this
// function: a round put back because its own ending reached nobody
// (refuseTakeLocked). Its run is over, so streamCopyStillWorking promises work
// that has already finished — and the user may have been the one who stopped it. The
// bubble matters as much as the sentence: sealing it would consume the handle
// the restore is holding for the publisher that has the real words, so this
// guard would trade a truthful ending for a false promise. It says nothing and
// leaves the round where it is.
func (m *TypingIndicatorManager) fireGuard(ctx context.Context, sessionID pgtype.UUID, batch engine.RunBatchID) {
	m.streams.sayEnding(ctx, sessionID, byBatch(batch), roundContinues, nil,
		func(ctx context.Context, t roundTurn) (roundAddress, error) {
			if !t.HasBubble {
				return roundAddress{}, errNothingToSay
			}
			return t.Handle.address(), m.writeClosing(ctx, sessionID, t.Handle, streamCopyStillWorking, "window expiring")
		})
}

// writeClosing seals one bubble with text and reports whether the words reached
// a sender. Which bubble was decided by the ledger, which consumed the handle
// under its own lock — that is what makes two closers racing produce one
// closing frame.
//
// A closing frame that cannot go out falls back to a plain message, the same
// way the answer does in outbound.go. The words matter more here than there:
// streamCopyFailed is the only "that run did not go through" WeCom ever
// produces, so a frame lost to a reconnect window would otherwise leave the
// user with a spinner and no explanation that would ever arrive. The addressing
// comes off the handle, captured at ingest, because by now the binding row may
// point at a different chat.
//
// Both routes failing is what the returned error is for, and WHICH failure it
// carries decides what the next publisher gets. The ledger records nothing as
// SAID either way. When the error names a frame that never reached the wire, the
// round goes back on the open list with its bubble, so the attempt this manager
// books next writes a closing frame over the spinner the user is still watching.
// Anything weaker leaves the run owed its ending and the next publisher — a
// booked retry, a sweeper tick, WeCom's own redelivery — says the words as an
// ordinary message instead.
//
// That error carries both attempts' verdicts, not just the last one. A closing
// frame that went out and then lost its ack may be sealing the bubble this very
// moment — the verdict is what is missing, not the frame — so if the fallback
// behind it is then cleanly refused, the refusal alone would tell a retry that
// nothing reached the user and the retry would put the same words on the screen
// a second time. errWordsMayBeOnScreen is what stops it. A frame the socket
// itself would not take is the other case and not this one: nothing whole left,
// so the retry is exactly what that ending needs (deliveryCanBeRepeated).
func (m *TypingIndicatorManager) writeClosing(ctx context.Context, sessionID pgtype.UUID, h streamHandle, text, why string) error {
	err := m.senders.stream(ctx, h, text, true)
	if err == nil {
		return nil
	}
	m.log.WarnContext(ctx, "wecom typing: closing frame failed, saying it as a new message",
		"chat_session_id", util.UUIDToString(sessionID),
		"reason", why, "unusable", streamUnusable(err), "error", err)
	sendErr := m.senders.sendTextCtx(ctx, h.InstallationID, h.ChatID, h.ChatType, text)
	if sendErr == nil {
		return nil
	}
	m.log.WarnContext(ctx, "wecom typing: the fallback message was unsendable too",
		"chat_session_id", util.UUIDToString(sessionID), "reason", why, "error", sendErr)
	if !deliveryCanBeRepeated(err) {
		return errors.Join(errWordsMayBeOnScreen, err, sendErr)
	}
	return sendErr
}

// sessionIDFromEvent recovers the chat session from a task lifecycle event.
// EventChatDone puts it on the envelope; EventTaskFailed carries it only in the
// broadcast payload, and only for chat runs — so both places are checked.
func sessionIDFromEvent(e events.Event) (pgtype.UUID, bool) {
	if e.ChatSessionID != "" {
		if id, err := util.ParseUUID(e.ChatSessionID); err == nil && id.Valid {
			return id, true
		}
	}
	if p, ok := e.Payload.(map[string]any); ok {
		if s, _ := p["chat_session_id"].(string); s != "" {
			if id, err := util.ParseUUID(s); err == nil && id.Valid {
				return id, true
			}
		}
	}
	return pgtype.UUID{}, false
}
