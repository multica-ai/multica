package wecom

// stream_store.go — the handles that let each answer land in the bubble its
// question opened.
//
// WeCom's aibot API has no typing indicator, no reaction, no read receipt, and
// no way to edit a message after the fact. The one affordance it does have is
// the streaming message: an aibot_respond_msg frame with finish=false paints a
// bubble the client renders as "working", and a later frame carrying the SAME
// stream.id replaces that bubble's body in place — finish=true seals it and
// nothing can touch it again
// (https://developer.work.weixin.qq.com/document/path/101463).
//
// A session holds a LIST of open bubbles, not one. Messages the engine's
// debouncer collects into one agent run share a bubble; a message it gives a
// run of its own gets a bubble of its own, queued behind the run in flight —
// immediately, because a message that produces nothing on screen reads as a
// message that was lost.
//
// WHICH RUN A BUBBLE STANDS FOR IS NEVER INFERRED HERE. Both halves of that
// question are answered upstream and carried in:
//
//   - engine.RunBatchID says which messages are one run. The batcher decides
//     it under the lock that arms and retires the debounce window, so two
//     messages share an id if and only if one flush answers both. Re-deriving
//     it here from arrival times would be a second measurement of the same
//     gap, taken on a detached goroutine, and near the window boundary the two
//     disagree about how many runs exist — one bubble for two runs, or a
//     bubble no run will ever close.
//   - The task id arrives with the flush that created the run
//     (TypingNotifier.OnRunStarted), so every later lifecycle event matches a
//     round by id rather than by position. An auto-retry clone carries a fresh
//     id and inherits its parent's chat_input_task_id, which is this same
//     round's task id — see roundTaker, which resolves the clone through it
//     before any ending is said.
//
// What a round's ending is allowed to record, and when, is the ending ledger's
// contract further down. Read it before adding a caller.
//
// The rounds are kept sorted by batch id (insertLocked), which for one session
// reads as the order its runs execute in: the engine serializes chat tasks per
// session (ClaimAgentTask). Nothing consumes that order, though — QueuedBehind
// compares batch ids rather than list positions, and it is decided once when the
// round opens and never revised. The sorting is for whoever reads the list, not
// for a caller that depends on it.
//
// Being on the list is not the same as still running, and one round breaks the
// resemblance: a round whose ending was said and reached nobody is put back with
// the bubble it had (putRoundBackLocked), so the oldest round of a session can
// be one whose run finished long ago. Every reader that cares reads runEnded
// rather than the position — see the field.
//
// The catch is req_id. Every frame of one stream has to echo the req_id of the
// aibot_msg_callback that started the turn, and that value is only ever seen
// by the WebSocket read loop. The answer shows up minutes later on an event
// bus subscriber holding nothing but a chat_session_id. This store is the seam
// between the two: session in, {req_id, stream id, addressing} out.
//
// IN-MEMORY IS THE RIGHT STORAGE, and deliberately so. One bot is one long
// connection, and the Supervisor's WS lease already guarantees at most one
// replica holds it, so a handle is only ever useful in the process that
// created it. A restart loses the handles and the answers fall back to plain
// messages — degraded, not corrupted. Persisting them would be a trade rather
// than a fix: a stored handle still inside the window would be writable from
// the new process, at the cost of a row per bubble and a sweep to retire them,
// to save a fallback message on the restart that lands mid-run.
//
// A RECONNECT IS NOT A RESTART, and the difference is why this store is built
// once at boot, outside the connection loop (router.go). A handle outlives the
// socket it was made on, and WeCom scopes a callback's req_id to the turn
// rather than to that socket (measured 2026-08-09; sendersRegistry.stream
// carries the detail), so the bubble a question opened before a drop is closed
// by the answer over the next connection. A store rebuilt per connection, or
// emptied when one ends, would leave every reconnect's bubbles spinning with
// nothing left that could close them.
//
// Replay is not this file's problem. WeCom redelivers callbacks after a
// reconnect, but a redelivered frame loses the dedup claim in
// channel_inbound_message_dedup and never reaches OutcomeIngested, so it never
// reaches the typing indicator either. What this file does bound is the
// protocol's own window: a stream past streamMaxAge is refused by the server,
// and a handle past that age is worse than no handle at all — it would swallow
// the answer instead of delivering it.

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

// streamMaxAge is how long a handle is worth keeping: ten minutes, measured
// against the live tenant on 2026-08-09 rather than read off anyone's source.
// One stream was held open with our backend stopped and framed every thirty
// seconds until the server refused. It took the frame at 600.0s and refused
// the one at 630.0s with errcode 846608, errmsg "stream message update expired
// (>10 minutes), cannot update". So the true ceiling is somewhere in (600s,
// 630s] and this constant sits on its lower bound.
//
// The budget belongs to the STREAM, not to the req_id that carried it. The
// same probe sealed a first stream with finish=true at two minutes and opened
// a second on the same req_id with a fresh stream id: the second was still
// being accepted at eight minutes old, well past the first one's own
// ten-minute mark, and died at its own. That is what would make rotating onto
// a fresh stream a real way to outlive the window, and it is not visible on
// the wire — it is why it is written down here.
//
// The six minutes this used to say came from a different mechanism, not from a
// source that disagreed with the ten. Tencent's OpenClaw plugin carries six for
// the webhook callback flow, where the developer's server is polled for at most
// six minutes from the user's message; we hold a long connection, which the
// long-connection doc gives ten minutes from the opening frame. The plugin also
// describes its six as an idle timeout, which the measurement rules out
// separately: the clock ran while frames were landing every thirty seconds.
//
// The window applies to a queued round's bubble the same as a running one's:
// the clock starts at the opening frame, and waiting in line does not stop it.
const streamMaxAge = 10 * time.Minute

// streamGuardAfter is when we close a bubble ourselves rather than let it run
// into streamMaxAge. A minute of headroom covers a slow frame and leaves the
// user with a sentence instead of a spinner the server will no longer let us
// replace. It stays clear of the measured ceiling's lower bound, not just of
// streamMaxAge.
const streamGuardAfter = 9 * time.Minute

// roundMemory is how long a session keeps the note the last handle left
// behind. It has to outlast the bubble by a lot: the guard closes at nine
// minutes and the run it made a promise about carries on for as long as the
// agent needs, so the window between the promise and the failure it accounts
// for is the length of a long run, not the length of a stream. An hour covers
// those and still bounds the map to the sessions answered in the last hour.
const roundMemory = time.Hour

// roundEnding says whether the words a closer is writing are the last this
// round will need. Only the guard's are not: "still working, I'll reply
// separately" is a promise, and whatever the run does next is still owed.
type roundEnding bool

const (
	roundOver      roundEnding = true
	roundContinues roundEnding = false
)

// roundVerdict is the store's answer to "may I speak for this round, and where".
type roundVerdict int

const (
	// roundForgotten — nothing on file. A turn from before a restart, one that
	// never opened a bubble at all, or one whose only record is that its batch
	// is finished. The caller has to find the chat some other way.
	roundForgotten roundVerdict = iota
	// roundOwesAnEnding — the store found this round and the caller may say its
	// ending. Which words go where is on the roundTurn: into the bubble the
	// round still has, or against the promise the guard left where a bubble
	// used to be.
	roundOwesAnEnding
	// roundToldAlready — nothing for THIS caller to say, and no delivery runs
	// for it. Usually the words are already on the screen: the answer landed,
	// another publisher's failure got here first, or a delivery this caller
	// waited out reported this run's ending accepted. A round put back after its
	// ending had already been said is the same fact reached from a new
	// direction: the round is on the open list, and the words are on the screen
	// all the same, so sealing the bubble with them would be the second copy
	// (refuseTakeLocked).
	//
	// Twice it is weaker than that. The guard meeting a round whose run is over
	// gets this because its own sentence would be false, not because anyone has
	// been told anything. And a caller whose budget ran out while it waited
	// never learns what became of the delivery it was waiting on, and comes
	// back with this and errEndingDeferred together. There the verdict is not a
	// fact about the user's screen and it is not the end of the matter either:
	// the error is what says so, and it says come back. Nothing is recorded on
	// the strength of this verdict in any case: the ledger is written from what
	// a delivery reports and from nothing else (I1), and no delivery ever runs
	// to see it.
	roundToldAlready
)

// openVerdict is the store's answer to "a message just arrived — does it get a
// bubble of its own".
type openVerdict int

const (
	// roundOpened — the first message of a run. The caller paints the opening
	// frame and arms the guard; from here on the round owns the handle it
	// registered.
	roundOpened openVerdict = iota
	// roundJoined — another message of a run whose bubble is already on
	// screen. The bubble is this message's receipt too and nothing is painted.
	roundJoined
	// roundFinished — this run has had its closer already. Only a badly
	// delayed OnIngested reaches this: the goroutine that paints the bubble
	// outlived the run it was painting for. Painting now would open a bubble
	// whose answer has already been delivered and which nothing would close.
	//
	// Usually the first bubble is closed by now. It need not be — a round whose
	// ending reached nobody is back on the open list still holding it — and
	// that changes nothing for this caller: there is a bubble for this run on
	// the user's screen either way, and a second one is what must not happen.
	roundFinished
)

// streamHandle is everything needed to keep writing to one open bubble. The
// addressing is captured at ingest rather than looked up later: by the time
// the answer arrives the binding row may have been re-pointed, and the frame
// has to go back to the chat that asked.
type streamHandle struct {
	// ReqID is the aibot_msg_callback's req_id. WeCom refuses a stream frame
	// carrying any other value, including a req_id from an event callback
	// (errcode 846605). Each round's bubble runs on the req_id of the message
	// that opened it.
	ReqID string

	// StreamID is ours to choose. Reusing it updates the message; a new one
	// opens another — which is exactly how a session comes to hold several
	// bubbles at once.
	StreamID string

	// InstallationID finds the live socket. ChatID and ChatType address the
	// conversation for the fallback plain message a closing frame degrades to
	// when the stream cannot take it (typing_indicator.go).
	InstallationID pgtype.UUID
	ChatID         string
	ChatType       int

	// QueuedBehind records that this round was opened while another round was
	// still open — it spent its life waiting in line. An empty answer for such
	// a round means "handled together with the previous reply", which is worth
	// saying differently from a first round's plain silence. Set by the store
	// at open; callers registering a handle leave it false.
	QueuedBehind bool

	CreatedAt time.Time
}

// roundAddress is where a round's words go once its bubble is gone: the
// installation whose socket carries them and the chat that asked. The stream
// ids are deliberately not here — they name a bubble nobody can write to any
// more, and carrying them would invite another attempt.
type roundAddress struct {
	InstallationID pgtype.UUID
	ChatID         string
	ChatType       int
}

func (a roundAddress) known() bool { return a.InstallationID.Valid }

func (h streamHandle) address() roundAddress {
	return roundAddress{
		InstallationID: h.InstallationID,
		ChatID:         h.ChatID,
		ChatType:       h.ChatType,
	}
}

// ── THE ENDING LEDGER'S CONTRACT ────────────────────────────────────────────
//
// owed / told / speaking below are one piece of bookkeeping answering one
// question: has this run's ending been said to the user, and if not, who still
// owes it. Seven review rounds have found seven different ways to get that
// wrong, and all seven were the same mistake — an outcome was treated as
// settled that the user never got:
//
//	1. a promise spent by POSITION, so one round's words settled another
//	   round's promise and the round the user cared about was never told;
//	2. a delivery path that sent the words and settled NOTHING, leaving the
//	   promise on file for the next repeat of that run's failure to spend
//	   underneath the answer the user had just read;
//	3. "already told" read off the SESSION rather than the run, so a second
//	   asker was silenced by a note an unrelated run had left;
//	4. a promise recorded as kept BEFORE the send meant to keep it, so a send
//	   refused during a WebSocket reconnect lost the promise for good;
//	5. a repeat answered "already said" while the first delivery was still on
//	   the wire, so when that delivery was refused the news had no publisher
//	   left — the one that was standing right there had been sent away before
//	   the outcome it came for existed;
//	6. a publisher waiting out another delivery read only WHETHER it landed and
//	   not WHAT it was, so the guard's "still working" — words that land and
//	   end nothing — silenced the failure notice and the answer behind it, and
//	   the promise it had just filed had no publisher left to keep it;
//	7. the fix for 5 and 6 reported a wait that lost to its caller's own
//	   deadline as "nothing to say", which callers are entitled to read as the
//	   end of the matter — so the publisher that still had the answer confirmed
//	   the chat:done handled and exited, and the guard's promise landing a
//	   moment later was a promise with nothing behind it.
//
// Scenario tests were written for each and did not stop the next one, because
// each fix was a rule the next caller had to remember. So the ledger states its
// invariants and the API enforces them instead:
//
//	I1. NOTHING IS RECORDED AS SAID UNTIL IT HAS BEEN SAID. A run reaches told,
//	    and a promise leaves owed, only after a delivery reports that the words
//	    were accepted for sending. Nor is anything ANSWERED as said before then:
//	    a publisher arriving while a delivery is on the wire is handed that
//	    delivery to wait on rather than a verdict of "already said" — until the
//	    holder reports, there is no such fact to give it. Two callers are
//	    answered roundToldAlready without that being a claim about the screen,
//	    and neither records or settles anything — see roundToldAlready's own doc,
//	    which says the verdict is about whether THIS caller should speak:
//	      - a waiter whose own budget runs out returns the verdict it came in
//	        holding, paired with errEndingDeferred. The error beside it is the
//	        store telling that caller to come back, because the news it walked in
//	        with is still undelivered and still nobody else's;
//	      - the guard, when it reaches a round whose run has already ended
//	        (refuseTakeLocked). It is refused rather than answered: its only
//	        sentence promises a reply still to come, and there is none. The round
//	        keeps its bubble for the publisher that has the real words.
//	    And what a waiter that IS released reads off the holder is the whole
//	    outcome, words and ending together: only a delivery that ENDED the round
//	    answers the run's other publishers, and one that landed a promise leaves
//	    them the round it just put back on owed.
//	I2. A RUN'S ENDING IS MATCHED BY THE RUN'S OWN ID — never by position,
//	    never by session. A promise another round is waiting on is not this
//	    run's to spend, and a note another round left is not this run's reason
//	    for silence.
//	I3. EVERY TERMINAL PATH ENDS IN WORDS OR LEAVES THE RUN SOMETHING TO BE
//	    SAID AGAINST. No path may both stay silent and clear the record. A
//	    delivery that failed puts a claimed promise back where it was, and for a
//	    round this store WAS HOLDING it leaves one of two things behind, decided
//	    by whether the words can be placed on the near side of the wire:
//	      - the ROUND, back on the open list with the bubble it had, when the
//	        delivery provably reached nobody. Nothing is filed as owed while it
//	        is there, because the round is the better record of the two — it
//	        carries a writable bubble, and the two readers that decide whether
//	        anyone speaks reach it: the next publisher, through indexLocked, and
//	        knowsRound, which scans the open list. owesEnding does NOT — it reads
//	        the note alone — so a caller asking that question while the round is
//	        back on the list is answered no. That is why the sweep hands the debt
//	        on rather than leaving the round to be the only record forever.
//	        It is the shorter-lived record,
//	        though: a round dies with the protocol's window at streamMaxAge and
//	        a note lives for roundMemory, so when the sweep retires such a round
//	        still unanswered it files the debt then (handOnTheDebtLocked). The
//	        record handed to a late publisher is the same one it would have
//	        found before, and it starts at the same moment — the note's clock
//	        has been running since the delivery failed;
//	      - the run OWED an ending, for every other failure. Its bubble went
//	        with the attempt, so the next publisher of the same news says the
//	        words somewhere else rather than finding them filed as already said.
//	    A run this store held no round for leaves no trace: it was owed nothing
//	    here, and a debt filed against it would be indistinguishable from the
//	    inbound path's own record of the round — which is what knowsRound reads
//	    as proof of where a question was asked.
//
// sayEnding is the only way the ledger is written, and it takes the delivery as
// an argument rather than handing out the right to speak. A caller cannot hold
// a claim it forgets to resolve because it never holds one: it is given the
// round, it reports whether the words went out, and the store records that and
// nothing else. Adding another way to end a round means writing another say
// function, which cannot be written in the wrong order.
//
// THE BUBBLE IS TAKEN UNDER THE SAME LOCK THAT FINDS THE ROUND, and that is
// what makes two racing closers produce one closing frame: from the moment a
// closer is handed the handle, nothing else holds this round.
//
// It is not taken for good, though. Whether the round comes back is decided by
// the one question the ledger asks about the delivery's error rather than about
// its success: did those words provably reach nobody. If they did, the spinner
// on the user's screen is untouched and the handle that writes over it is worth
// more to the next publisher than any debt, so the round goes back on the list
// holding it (putRoundBackLocked) — still exclusive, since the restore happens
// under the lock that publishes the outcome, so whoever was waiting resumes to
// find the round rather than the race for it.
//
// A round that comes back is a round whose RUN is already over, which is a shape
// nothing on this list had before. The three readers it would otherwise mislead
// are named on runEnded, and the take itself is gated for the two it would:
// refuseTakeLocked.
// ────────────────────────────────────────────────────────────────────────────

// errNothingToSay is how a delivery reports that it declined to speak: a cancel
// for a round this process has no record of, an empty completion nobody is owed
// an ending for, a session with no WeCom binding at all. Nothing reached the
// user, so under I1 nothing is recorded as SAID — and unlike a refused send it
// is not worth a warning, because no words were ever going out. It is not the
// same as nothing having happened: a round this store was holding has had its
// bubble consumed either way, so I3 still leaves that run owed an ending.
var errNothingToSay = errors.New("wecom: nothing to say for this round")

// errEndingDeferred is how a WAIT reports that it lost to its caller's own
// deadline. The delivery it was parked on is still running, so what became of
// those words is not yet a fact anyone holds; this caller reserved nothing, said
// nothing, and recorded nothing, and the news it arrived with never went out.
//
// It is deliberately not errNothingToSay and never wraps it, because the two
// mean opposite things to whoever receives them. "Nothing to say" is terminal —
// no bubble, nothing owed, a run this process has no business speaking for — and
// a caller may close the matter on it. This one means come back: the words are
// still in the caller's hands and it is still the publisher they have.
//
// A caller that reads it as terminal reports the news handled while holding it,
// and that is not merely a delayed delivery. The publisher ahead is often the
// nine-minute guard, whose words land and end nothing: the promise it files a
// moment later is then a promise with no publisher left to keep it, and the
// asker is looking at streamCopyStillWorking for a reply that has already
// happened. So every caller of sayEnding answers this one, and the answer is
// another attempt on a budget of its own — see the bounded retries in
// outbound.go and typing_indicator.go. The one caller that needs none is
// fireGuard, and its own comment says why.
var errEndingDeferred = errors.New("wecom: ran out of budget waiting for another delivery")

// roundTurn is what the ledger hands a delivery for the length of one ending:
// everything this store knows about where the round can still be reached.
//
// Both halves can be absent, and they are separate questions. A round whose
// opening frame the server refused has no bubble but may still be on file; a
// round the guard closed has no bubble and a promise instead; a run this
// process never saw has neither, and its delivery has to find the chat itself.
type roundTurn struct {
	// Handle is the round's open bubble, writable now. HasBubble says whether
	// there is one: a round with no painted frame, or one past the protocol's
	// window, reports false and its words go out as an ordinary message.
	Handle    streamHandle
	HasBubble bool

	// Addr is where this round was speaking, off the handle or off the note it
	// left. Unknown for a round this process holds nothing for.
	Addr roundAddress

	// Promised says this round was on owed and this turn claimed the debt:
	// words this session owes the asker that nobody has said. It is what
	// separates "nothing to add" from "something still to say" when a run
	// finishes silently, and what lets a cancel speak without chasing an
	// address it should not.
	//
	// The guard is the commonest source and the one to picture — it closed the
	// bubble with streamCopyStillWorking, and the separate reply that sentence
	// promised is still outstanding. The other two are the same debt filed
	// where no words reached the screen at all: a terminal ending whose
	// delivery failed with the bubble already spent (I3), and a round put back
	// with its bubble that reached the end of the protocol's window before any
	// publisher came for it (handOnTheDebtLocked). All three are written only
	// for a round this store was holding, which is what makes the debt proof
	// that a WeCom round is waiting on these words.
	Promised bool

	// Verdict is the store's answer to "may I speak for this round". A delivery
	// only ever runs for roundOwesAnEnding and roundForgotten; roundToldAlready
	// never reaches one.
	Verdict roundVerdict
}

// roundKey picks which round an ending speaks for. Both names are authoritative
// and neither is inferred: the task id is the one the debounced flush bound to
// the round, and the batch id is the engine's own name for the run — used by
// the two closers that fire before any answer exists, the guard and the flush
// that settled without creating a task.
type roundKey struct {
	taskID  string
	batch   engine.RunBatchID
	byBatch bool
}

func byTask(taskID string) roundKey { return roundKey{taskID: taskID} }

func byBatch(batch engine.RunBatchID) roundKey {
	return roundKey{batch: batch, byBatch: true}
}

// batchRunID is the name a round that never became a run is filed under.
//
// Every list on the note is keyed by a run id, and the settled flush's round
// has none: it is the path the Router takes when the enqueue produced no task
// at all, so no id was ever bound to it. Its batch is the entry's identity
// until one arrives, so that is what stands in — otherwise the one terminal
// path with no second publisher of its own is also the one the ledger cannot
// record anything about, and I3 holds everywhere except there.
//
// The prefix is what keeps the stand-in from ever being mistaken for a run. A
// task id is a UUID, so no lookup by task id can match one of these: a debt
// filed here is invisible to owesEnding, to wasTold, and — the one that
// matters — to knowsRound, which must never read a task-less round's debt as
// proof about some run that shares the session.
func batchRunID(batch engine.RunBatchID) string {
	if batch == 0 {
		return ""
	}
	return "batch:" + strconv.FormatUint(uint64(batch), 10)
}

// pendingEnding is one ending in flight: reserved under the lock, not yet
// recorded, and holding everything needed to put the ledger back exactly as it
// was if the words never land. It never leaves this file — sayEnding is the
// only thing that creates one and the only thing that resolves one.
type pendingEnding struct {
	// live is false when there is nothing to record: an ending for a round the
	// flush had not yet named, or a turn no delivery ran for.
	live   bool
	key    string
	taskID string
	// alias is the auto-retry clone's own id, when the round was found under
	// the batch owner's name instead. A repeat carrying the clone's id has to
	// find the ending on file or it says the same thing a second time.
	alias  string
	ending roundEnding
	// owedAt is where in owed the promise sat when it was claimed, or -1 if
	// nothing was claimed. It is what I3 restores.
	owedAt int
	// held says this ending was reserved from a round this store actually had:
	// one takeAtLocked lifted off the open list, or one whose promise was still
	// on the note. Both are written only by this adapter's inbound path — a
	// bubble opened by a message it ingested, named by the flush that answered
	// it — so it is the run-level fact "this run was asked in the room".
	//
	// It is what separates the two failed deliveries. A round this store held
	// has lost its bubble to the attempt, so I3 leaves the run owed an ending
	// and the next publisher says it. A run this store held NOTHING for lost
	// nothing: forgottenLocked took no handle and consumed no promise, and it
	// reached here only because some subscriber tried to speak for a run on a
	// shared bus. Filing a debt for that one would put a run this adapter never
	// ingested on owed — where knowsRound reads it as proof the question was
	// asked in the room, and hands the failure gate a permission the database
	// was supposed to decide.
	held bool

	// flight is the reservation this delivery is holding: the entry on the
	// note's speaking list for a round on file, or the one in the inflight map
	// for a run this store has no note for. Resolved by endEnding, which is what
	// publishes the outcome to whoever is waiting behind it. Nil only where
	// nothing was reserved at all.
	flight *endingInFlight

	// entry is the round this delivery was handed a WRITABLE BUBBLE off, kept
	// so a failure that reached nobody can put it back on the open list with
	// that bubble — see putRoundBackLocked. Nil everywhere else, which is what
	// makes the restore unreachable by accident: a round with no bubble, a
	// handle past the protocol's window, and a run this store held nothing for
	// all leave it nil, and none of the three has a bubble worth keeping.
	entry *roundEntry
}

// keepsItsBubble reports whether a failed delivery puts this round back on the
// open list rather than leaving it owed an ending — the one exception to "a
// taken round stays taken", and deliberately three conditions rather than one.
//
// entry is the round, and the take sets it only where the bubble it handed out
// was actually writable.
//
// err has to name a failure this package can place on the near side of the
// wire, which is what bubbleSurvivedTheFailure answers. Anything weaker hands
// back a bubble the user may already be reading an ending in.
//
// roundOver is the ending, and the guard's roundContinues is excluded on
// purpose. The guard is the one publisher that books no retry — fireGuard's own
// doc says why — so a bubble handed back to it has nobody left to write into
// it, and it would give up the debt I3 files in exchange for nothing. It fires
// at nine minutes besides, so what it would be handing back is a bubble with a
// minute left to live.
//
// That last condition is why every round on the list with runEnded set arrived
// there through a TERMINAL ending, which is what lets the guard be refused one
// outright (refuseTakeLocked) rather than having to work out what it could
// truthfully say about it.
func (p pendingEnding) keepsItsBubble(err error) bool {
	return p.entry != nil && p.ending == roundOver && bubbleSurvivedTheFailure(err)
}

// inflightKey names one delivery: the session it is speaking in and the run it
// speaks for.
type inflightKey struct {
	key    string
	taskID string
}

// endingInFlight is one delivery that other publishers of the same news wait
// on. Every reserved ending has one, wherever the reservation is kept.
//
// task:failed has two publishers and the bus is synchronous, so a repeat can
// reach the store while the first delivery's words are still on the wire. What
// differs between a round on file and a run this store holds nothing for is only
// WHERE the reservation lives: on the note's speaking list for the first, and in
// a map of its own for the second — because anything filed on a note would be
// read by knowsRound as proof the question was asked in the room, and at that
// point there is no evidence the session is even WeCom's. Neither outlives the
// delivery: both are given up the moment it reports back, and both are retired
// by the same sweep at inFlightMaxAge when it never does.
//
// Waiting rather than answering "already said" is the point of the channel, and
// it is the point on both paths. If the first delivery fails, the publisher
// behind it is the retry that news still has, and answering it "already said"
// would swallow the words the way defect 4 swallowed a promise — which is
// exactly what the speaking list did before defect 5 was found. The wait is
// bounded by the caller's own context, and what it waits for is a send bounded
// by streamCloseTimeout.
//
// What it can cost the caller is that whole budget, which is defect 7: a wait
// the caller loses leaves it with the news and no time to send it in. That is
// not silence and it is not an ending, so it goes back as errEndingDeferred and
// the caller returns on a budget of its own.
//
// A wait the caller WINS costs it the same time and leaves the same nothing to
// speak in, which is why the delivery does not run on the remainder — see
// deliveryBudget.
//
// The outcome is published under s.mu, by the same call that files what the
// delivery lost. So a waiter released with delivered=false cannot get back into
// the store before the ledger is settled, and what it finds when it does is the
// promise restored, the round newly owed one, or the round itself back on the
// open list with its bubble — never a debt still in the air.
type endingInFlight struct {
	// done is closed once the delivery has reported. delivered is written
	// before the close and read only after it, so the close is the whole of the
	// synchronisation between the two.
	done      chan struct{}
	delivered bool
	// ending is what the holder is saying, and it is half of what a waiter
	// released with delivered=true has to know. Words that landed only account
	// for the run when they were its ENDING; the guard's are a promise, and a
	// waiter that read delivered alone went quiet on the strength of
	// streamCopyStillWorking — swallowing the failure notice and, worse, the
	// answer, while the promise those words created sat on owed with both of
	// its publishers spent. That is defect 6. Fixed at creation and never
	// written again, so it needs no synchronisation of its own.
	ending roundEnding
	// over says the outcome has been published. The sweep can retire a
	// reservation whose holder never reported, and that holder may still come
	// back afterwards; without this the second report would close done twice.
	over bool
	at   time.Time
}

// report publishes this delivery's outcome to whoever is waiting on it. Written
// under s.mu, and a second call is a no-op rather than a panic — see over.
func (f *endingInFlight) report(delivered bool) {
	if f.over {
		return
	}
	f.over, f.delivered = true, delivered
	close(f.done)
}

// inFlightMaxAge retires a reservation whose holder never reported — a delivery
// that panicked out of a bus listener, say. Nothing normal reaches it: a
// delivery is bounded by streamCloseTimeout, so a reservation this old has no
// holder left. Without it one lost report would strand every later publisher of
// that run: each parks on a channel nothing will ever close and pays its whole
// budget to find that out.
//
// It bounds both places a reservation is kept, which is the whole of what the
// two have in common: sweepLocked walks the inflight map and every note's
// speaking list against it.
const inFlightMaxAge = time.Minute

// endedRound is what a session keeps once a handle is gone: where the round
// was speaking, whether anything is still owed to it, and which runs are done.
//
// owed is the heart of it. A handle is taken by whichever ending gets there
// first, and the nine-minute guard is allowed to be that one — it writes
// "still working, I'll reply separately" while the run carries on. The failure
// that arrives afterwards finds no handle, and without this note it would
// return without a word: a promise, and then nothing.
//
// One note per session, but the promises inside it are counted per ROUND. A
// single flag could not hold two: with rounds A and B both guard-closed, A's
// own answer — the separate reply its guard promised — would clear the flag,
// and when B's run failed the store would say "already told" and B's asker
// would hear nothing at all. The address is still shared, which is fine
// because both rounds name the same chat; it is the promises that have to be
// individual.
type endedRound struct {
	addr roundAddress
	at   time.Time
	// owed lists the rounds promised a separate reply that have not had one,
	// oldest first, each holding its run id. A round's own ending settles its
	// own entry; another round's cannot.
	owed []string
	// finished lists the batches a closer has already been handed the bubble
	// for, so a badly delayed OnIngested cannot paint a second one for a run
	// that has already answered. An entry is permanent for as long as the note
	// is: what it records is that this run has had its closer, which stays true
	// even when the delivery failed and the round went back on the open list
	// still holding the bubble it was painted with. Bounded: a batch old enough
	// to have fallen off cannot still have a message in flight for it.
	finished []engine.RunBatchID
	// told lists the RUNS whose ending has already been said — a bubble
	// sealed, a promise kept, a plain message sent. It is what makes a second
	// publisher of one run's failure silent, and it has to be per run for the
	// same reason owed is: the existence of a note says only that SOMETHING in
	// this session has been spoken for. Keyed by session, a second run's
	// failure would read another run's note as its own and its asker would be
	// told nothing at all. Bounded the way finished is.
	told []string
	// speaking lists the runs whose ending is going out RIGHT NOW: claimed out
	// of owed, not yet on told, because under I1 nothing is told until it has
	// been said. It is what keeps the gap between those two safe. The bus is
	// synchronous and task:failed has two publishers, so a repeat can arrive
	// while the first delivery is still on the wire — it finds the run here and
	// waits on the delivery the entry carries: silent once that delivery reports
	// it put the run's ENDING on the screen, and the publisher this news has
	// left in every other case, because the run is then back on owed for it to
	// claim. Entries whose holder never came back are retired by sweepLocked.
	speaking []speakingRound
}

// speakingRound is one run's ending on the wire: the run it is for, and the
// delivery a publisher arriving behind it waits out.
//
// The id alone was what defect 5 had. A list of bare ids can only answer "being
// said", and the honest answer to that is not "already said" — it is "wait and
// see", which needs the delivery itself. Every entry carries one, so the answer
// is always available where the question is asked, and it carries the whole
// answer: the words' fate and which ending they were, which is what defect 6
// cost the reader of only the first half.
type speakingRound struct {
	taskID string
	flight *endingInFlight
}

// isTold reports whether this run's ending has already been said.
func (e endedRound) isTold(taskID string) bool {
	if taskID == "" {
		return false
	}
	for _, id := range e.told {
		if id == taskID {
			return true
		}
	}
	return false
}

// tell adds a run to the told list, keeping it bounded. Returns the new list.
func (e endedRound) tell(taskID string) []string {
	if taskID == "" || e.isTold(taskID) {
		return e.told
	}
	next := append(e.told, taskID)
	if len(next) > maxToldRounds {
		next = next[len(next)-maxToldRounds:]
	}
	return next
}

// owe adds a run to the owed list, keeping it bounded. Returns the new list.
func (e endedRound) owe(taskID string) []string {
	if taskID == "" || indexOfRun(e.owed, taskID) >= 0 {
		return e.owed
	}
	next := append(e.owed, taskID)
	if len(next) > maxOwedRounds {
		next = next[len(next)-maxOwedRounds:]
	}
	return next
}

// maxOwedRounds bounds the owed list, which I3 lets grow on its own: every
// ending that could not be delivered leaves its run owed one, so a session
// whose socket is down for the whole hour the note lives adds an entry per run.
// The oldest promise is the one worth dropping — the note itself expires at
// roundMemory, so an entry near the front is already close to worthless — and
// the same thirty-two is far more outstanding promises than a session
// serializing its runs can produce in that time.
const maxOwedRounds = 32

// maxToldRounds bounds the told list. What it has to outlast is a REPEAT of
// one run's ending — a sweeper tick republishing a failure, an auto-retry's
// first attempt arriving late — which lands within minutes of the original,
// while the note itself is discarded after roundMemory. Thirty-two rounds is
// far more than a session serializing its runs gets through in that gap, and
// it keeps the note a fixed size for a chat that never stops.
const maxToldRounds = 32

// isFinished reports whether this run's bubble is already over.
func (e endedRound) isFinished(batch engine.RunBatchID) bool {
	if batch == 0 {
		return false
	}
	for _, id := range e.finished {
		if id == batch {
			return true
		}
	}
	return false
}

// finish adds a batch to the list, keeping it bounded: a session that runs for
// days should not accumulate ids forever.
func (e endedRound) finish(batch engine.RunBatchID) []engine.RunBatchID {
	if batch == 0 || e.isFinished(batch) {
		return e.finished
	}
	next := append(e.finished, batch)
	if len(next) > maxFinishedRounds {
		next = next[len(next)-maxFinishedRounds:]
	}
	return next
}

// maxFinishedRounds bounds the finished list. Ten rounds back is far more than
// an ingest goroutine can lag by — it holds the Router's reply budget, a
// couple of seconds, against rounds that take minutes.
const maxFinishedRounds = 10

// isOwed reports whether anything is still promised.
func (e endedRound) isOwed() bool { return len(e.owed) > 0 }

// speakingAt finds the delivery saying this run's ending, or -1. This is the
// lookup a PUBLISHER does: it knows a run and wants whoever is speaking for it.
func (e endedRound) speakingAt(taskID string) int {
	if taskID == "" {
		return -1
	}
	for i, s := range e.speaking {
		if s.taskID == taskID {
			return i
		}
	}
	return -1
}

// speakingByFlight finds the entry one delivery reserved for ITSELF, or -1.
//
// The other lookup cannot answer this. A run's id names whoever is speaking for
// it, and that need not be this delivery: the sweep retires a reservation whose
// holder never reported and the next publisher takes one of its own for the same
// run, so a holder that turns up after both and gave up its entry by id would
// withdraw a right to speak that is not its own — leaving a live delivery with
// nothing excluding its repeats and the same words going out twice. A
// reservation is given up by whoever took it and by nobody else.
func (e endedRound) speakingByFlight(f *endingInFlight) int {
	if f == nil {
		return -1
	}
	for i, s := range e.speaking {
		if s.flight == f {
			return i
		}
	}
	return -1
}

// indexOfRun finds a run in one of the note's id lists, or -1. Every match
// against those goes through it, and speakingAt is the same lookup for the one
// list whose entries carry a delivery as well: I2 says a run is matched by its
// own id and by nothing else, and two lookups between them are two places for
// that to stay true. speakingByFlight is not a third — it matches a reservation
// against its holder, which is a question about identity rather than about
// which run an ending speaks for.
func indexOfRun(runs []string, taskID string) int {
	if taskID == "" {
		return -1
	}
	for i, id := range runs {
		if id == taskID {
			return i
		}
	}
	return -1
}

// withoutRun returns runs with the entry at i removed, leaving the input
// untouched — the note is copied in and out of the map, so a shared backing
// array would let one round's edit rewrite another's list.
func withoutRun(runs []string, i int) []string {
	return append(runs[:i:i], runs[i+1:]...)
}

// withoutSpeaker is withoutRun for the speaking list, and copies for the same
// reason.
func withoutSpeaker(speaking []speakingRound, i int) []speakingRound {
	return append(speaking[:i:i], speaking[i+1:]...)
}

// restoreRun puts a claimed promise back where it was, which is what I3 owes a
// delivery that failed. Position carries no meaning any more — every match is
// by id — but a list that comes back in the order it went out is one less thing
// for a future reader to wonder about.
//
// A run already on the list stays where it is, the way owe leaves one alone.
// One promise is one promise however many publishers file it, and a second copy
// would be a debt no claim ever clears: the sweep files the debt of a delivery
// that never reported, and that delivery's holder may still turn up afterwards
// with the claim it took.
func restoreRun(runs []string, at int, taskID string) []string {
	if indexOfRun(runs, taskID) >= 0 {
		return runs
	}
	if at < 0 || at > len(runs) {
		at = len(runs)
	}
	out := make([]string, 0, len(runs)+1)
	out = append(out, runs[:at]...)
	out = append(out, taskID)
	return append(out, runs[at:]...)
}

// roundEntry is one run's place in a session, from the moment anything is
// known about it until its ending is said. Whoever takes or drops the round
// disposes of all of it in one lock.
//
// The two facts arrive from different directions and in either order, which is
// why the entry exists independently of both. OnIngested brings the bubble
// (one goroutine per message, detached by the Router); the debounced flush
// brings the task id ~3s later. An entry with a task and no bubble is a run
// whose ingest goroutine has not got there yet, or one whose opening frame the
// server refused: its ending is still matched correctly, it just has nowhere
// on screen to land and falls back to a plain message.
type roundEntry struct {
	// batch is the engine's own name for this run and the entry's identity.
	batch engine.RunBatchID

	// handle is the open bubble; painted reports whether there is one.
	handle  streamHandle
	painted bool

	// taskID is the run the flush created for this batch, as reported by
	// OnRunStarted. Empty until the debounce window expires.
	taskID string

	// guard closes the bubble if nothing else does before the protocol's
	// stream window runs out. Stopped by the take and never re-armed: the only
	// round that comes back is one whose run has already ended (runEnded), and
	// the guard has nothing true to say about one of those.
	guard *time.Timer

	// runEnded says this round is on the list for the second time: its run
	// produced an ending, a delivery said it, and those words provably reached
	// nobody, so putRoundBackLocked handed the round back with the bubble the
	// user is still watching. The run itself is over.
	//
	// Three readers need that apart from a first-time round, and all three would
	// otherwise state something false about a run that has already finished:
	// queuedBehind, which would make the NEXT round open claiming it was queued
	// behind this one; the guard, whose only sentence promises more work; and
	// the sweep, which is what hands the debt on when this round finally goes.
	runEnded bool

	// createdAt bounds the entry for the sweep when there is no handle to read
	// a time off.
	createdAt time.Time
}

// endingName is the id this round's ending is filed under: the run the flush
// bound to it, or — for the round that never became a run — the stand-in built
// from its batch. It is the one name told, owed and speaking ever carry for this
// round, so every lookup against those goes through it.
func (e *roundEntry) endingName() string {
	if e.taskID != "" {
		return e.taskID
	}
	return batchRunID(e.batch)
}

// streamStore maps chat_session_id to that session's rounds, oldest first, and
// — for a while after the last one is gone — to what the session still owes.
type streamStore struct {
	mu       sync.Mutex
	sessions map[string][]*roundEntry
	ended    map[string]endedRound
	// inflight is the exclusion for the deliveries the note cannot hold: one
	// entry per run this store has no round for whose words are on the wire
	// right now. Kept apart from ended on purpose — see endingInFlight.
	inflight map[inflightKey]*endingInFlight

	maxAge time.Duration
	now    func() time.Time
}

func newStreamStore() *streamStore {
	return &streamStore{
		sessions: make(map[string][]*roundEntry),
		ended:    make(map[string]endedRound),
		inflight: make(map[inflightKey]*endingInFlight),
		maxAge:   streamMaxAge,
		now:      time.Now,
	}
}

// NewStreamStore is the constructor boot uses to mint the one store shared by
// the typing indicator (writer) and the chat-done subscriber (reader).
func NewStreamStore() *streamStore { return newStreamStore() }

// entryLocked finds the round for a batch, or nil. Caller holds s.mu.
func (s *streamStore) entryLocked(key string, batch engine.RunBatchID) *roundEntry {
	for _, r := range s.sessions[key] {
		if r.batch == batch {
			return r
		}
	}
	return nil
}

// insertLocked files a new round in batch order. The ids are monotonic, so
// this keeps the list in the order the runs will execute in even when the
// Router's detached ingest goroutines deliver two messages out of order.
// Caller holds s.mu.
func (s *streamStore) insertLocked(key string, e *roundEntry) *roundEntry {
	rounds := s.sessions[key]
	i := len(rounds)
	for i > 0 && rounds[i-1].batch > e.batch {
		i--
	}
	rounds = append(rounds, nil)
	copy(rounds[i+1:], rounds[i:])
	rounds[i] = e
	s.sessions[key] = rounds
	return e
}

// open registers a message's bubble against the run the engine collected it
// into, and says whether this message is the one that paints it. Every message
// of a run calls this; the first gets roundOpened and the rest roundJoined,
// because one run produces one answer and a second bubble for it is a bubble
// nobody ever closes.
//
// Which run this is comes from batch — the debouncer's own verdict — so the
// count of bubbles and the count of runs cannot drift apart.
func (s *streamStore) open(sessionID pgtype.UUID, batch engine.RunBatchID, h streamHandle) openVerdict {
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()

	if note, ok := s.ended[key]; ok && note.isFinished(batch) {
		return roundFinished
	}
	if h.CreatedAt.IsZero() {
		h.CreatedAt = s.now()
	}
	if e := s.entryLocked(key, batch); e != nil {
		if e.painted {
			return roundJoined
		}
		// The flush got here before this message's ingest goroutine did. The
		// round already has its run; it was only ever missing the bubble.
		h.QueuedBehind = e.queuedBehind(s.sessions[key])
		e.handle, e.painted = h, true
		return roundOpened
	}
	e := &roundEntry{batch: batch, handle: h, painted: true, createdAt: h.CreatedAt}
	s.insertLocked(key, e)
	e.handle.QueuedBehind = e.queuedBehind(s.sessions[key])
	return roundOpened
}

// queuedBehind reports whether any OLDER round of this session is still WAITING
// TO BE ANSWERED — this round will wait for it, and an empty answer of its own
// then means "the reply ahead of it covered this", not plain silence.
//
// A round whose run has already ended does not count, however long its bubble
// stays on the list. It is there because its ending reached nobody, not because
// anything is still running for it, and the engine's queue moved on the moment
// it finished. Counting it would open the next round with QueuedBehind set, and
// an empty completion for THAT round would then close with streamCopyMerged —
// telling the user this message was folded into a reply that was never
// delivered.
func (e *roundEntry) queuedBehind(rounds []*roundEntry) bool {
	for _, r := range rounds {
		if r.batch < e.batch && !r.runEnded {
			return true
		}
	}
	return false
}

// bind records the task the debounced flush created for a batch. This is the
// authoritative round-to-run link: from here on every task lifecycle event
// finds its bubble by id.
//
// It files a round even when no bubble has been painted yet, because the
// Router runs OnIngested on a detached goroutine and the flush that names the
// task can win the race. The bubble attaches to the same entry when it lands.
//
// The round on the list is consulted BEFORE the finished list, and the order is
// the whole of what lets a round that came back learn its run's name. The take
// files the batch as finished on its way out and never unfiles it, because that
// list is the one thing standing between a badly delayed OnIngested and a second
// bubble for a run that has already answered. A round in front of it is not that
// case: it is the same round, still on the list, still holding the bubble it was
// painted with, and a flush that has yet to report is exactly what it is waiting
// for. Without the name, every ending after this one arrives carrying a task id
// this store cannot match to the bubble it is holding.
func (s *streamStore) bind(sessionID pgtype.UUID, batch engine.RunBatchID, taskID string) {
	if taskID == "" || batch == 0 {
		return
	}
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entryLocked(key, batch); e != nil {
		e.taskID = taskID
		return
	}
	if note, ok := s.ended[key]; ok && note.isFinished(batch) {
		return
	}
	s.insertLocked(key, &roundEntry{batch: batch, taskID: taskID, createdAt: s.now()})
}

// arm attaches the expiry guard to a round. A round that ended between the
// open and this call has already left the list, so there is nothing to guard
// and the timer is stopped instead of leaked.
//
// Armed once per round and never re-armed. The take stops it, and a round the
// take hands back is one whose run is over — see putRoundBackLocked for why the
// guard is not the closer such a round wants.
func (s *streamStore) arm(sessionID pgtype.UUID, batch engine.RunBatchID, t *time.Timer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entryLocked(util.UUIDToString(sessionID), batch); e != nil {
		e.guard = t
		return
	}
	t.Stop()
}

// indexLocked finds the round an ending speaks for, or -1. Matching is by the
// name the caller carried in and there is no positional fallback: a run whose
// id is not on file has no bubble here, and taking somebody else's would seal
// the wrong question with this answer. Caller holds s.mu.
func (s *streamStore) indexLocked(key string, k roundKey) int {
	if !k.byBatch && k.taskID == "" {
		return -1
	}
	for i, r := range s.sessions[key] {
		if k.byBatch {
			if r.batch == k.batch {
				return i
			}
			continue
		}
		if r.taskID == k.taskID {
			return i
		}
	}
	return -1
}

// noteLocked returns this session's note, fresh if there is none yet. Caller
// holds s.mu.
func (s *streamStore) noteLocked(key string) endedRound {
	if note, ok := s.ended[key]; ok {
		return note
	}
	return endedRound{}
}

// speakLocked joins a run to the note's speaking list and hands the delivery to
// the pending that will resolve it. One call does both, so a reservation cannot
// be taken without something to report what became of it — the same reason
// sayEnding hands out no claims. Caller holds s.mu.
func (s *streamStore) speakLocked(note endedRound, p *pendingEnding) endedRound {
	f := &endingInFlight{done: make(chan struct{}), ending: p.ending, at: s.now()}
	note.speaking = append(note.speaking, speakingRound{taskID: p.taskID, flight: f})
	p.flight = f
	return note
}

// takeAtLocked removes rounds[i] and reserves its ending — the ending of a
// round, whichever ending it is.
//
// The handle goes unconditionally, because it is the mutual exclusion that
// makes two racing closers write one closing frame. A round with no bubble
// reports absent, and so does a handle past maxAge: the server would refuse the
// frame and a caller that believed it had a bubble would leave the user with
// nothing.
//
// Unconditionally is not irreversibly. Everything this undoes on the way out is
// recorded on the pendingEnding, because a delivery that provably reached
// nobody leaves the bubble exactly as the user last saw it and the round goes
// back on the list it came off — putRoundBackLocked, called from endEnding and
// nowhere else. The take stays the exclusion either way: nothing else can hold
// this round until that decision has been made, under this same lock.
//
// What the round IS gets filed here too, because none of it depends on words
// reaching anyone: this run has had its closer, so a badly delayed OnIngested
// cannot paint a second bubble for a run that has already answered, and this is
// the chat the round was speaking in. Neither is undone by the restore. The finished
// list in particular is permanent — a round handed back is handed back with the
// bubble it already had, so nothing about it wants a second one painted, and
// unfiling would reopen that hole for good the moment the sweep took the round
// away. bind consults the round in front of the list for the one thing that did
// need the unfile. What the round has been TOLD is the only part that waits for
// the delivery (I1), and the pendingEnding is what carries it there. Caller
// holds s.mu.
func (s *streamStore) takeAtLocked(key string, i int, ending roundEnding) (roundTurn, pendingEnding) {
	rounds := s.sessions[key]
	entry := rounds[i]
	rounds = append(rounds[:i], rounds[i+1:]...)
	if len(rounds) == 0 {
		delete(s.sessions, key)
	} else {
		s.sessions[key] = rounds
	}
	if entry.guard != nil {
		// A round off the list has nothing to guard. Whether the timer was still
		// waiting does not matter: it is never re-armed, and a guard whose
		// goroutine is already on its way here finds a round it is refused
		// (beginEndingLocked) or no round at all.
		entry.guard.Stop()
	}

	note := s.noteLocked(key)
	note.finished = note.finish(entry.batch)
	addr := roundAddress{}
	if entry.painted {
		addr = entry.handle.address()
	}
	if addr.known() {
		// A round that never had a bubble must not overwrite a known address
		// with its blank one.
		note.addr = addr
	} else {
		addr = note.addr
	}

	// endingName is the run the flush bound, or — for the settled flush's round,
	// where the enqueue produced no task and nothing later ever will — the
	// stand-in built from the batch. It is still a round of this adapter's with a
	// bubble on somebody's screen, so its ending is filed under the only name it
	// has.
	pending := pendingEnding{
		key: key, taskID: entry.endingName(), ending: ending, owedAt: -1, held: true,
	}
	promised := false
	if pending.taskID != "" {
		pending.live = true
		// A round on the open list with its own promise outstanding is a shape
		// only the sweep can produce: it files the debt of a delivery that
		// never reported, and that holder may still turn up afterwards and put
		// the round back (putRoundBackLocked). Rare — the holder is a delivery
		// bounded by streamCloseTimeout and the sweep waits a minute — and
		// spending that promise twice would not be, so it is claimed here
		// exactly the way a claim claims it.
		if at := indexOfRun(note.owed, pending.taskID); at >= 0 {
			note.owed = withoutRun(note.owed, at)
			pending.owedAt, promised = at, true
		}
		note = s.speakLocked(note, &pending)
	}
	note.at = s.now()
	s.ended[key] = note

	turn := roundTurn{Addr: addr, Promised: promised, Verdict: roundOwesAnEnding}
	if entry.painted && !s.expiredLocked(entry.handle.CreatedAt) {
		turn.Handle, turn.HasBubble = entry.handle, true
		// The one round worth putting back: there is a spinner on somebody's
		// screen and this handle is what writes over it. A round with no bubble,
		// or one whose window has run out, has nothing to keep — its ending goes
		// out as an ordinary message and I3's debt is the whole of what a failure
		// leaves behind.
		pending.entry = entry
	}
	return turn, pending
}

// putRoundBackLocked returns a round to the open list it was lifted off, and it
// is the whole of what the ledger does differently for a delivery that provably
// reached nobody.
//
// What goes back is the entry, in batch order, and the ONE thing that changes
// about it is that it now says its run is over (runEnded). Nothing else the take
// filed is undone: the note's address was true before the take and is true
// after, and the finished list is what stops a second bubble being painted for
// this run — see takeAtLocked.
//
// Only from endEnding, under the lock that publishes the delivery's outcome. A
// publisher released by that outcome finds the round already back, which is what
// makes "wait, then come round" land on a bubble rather than on a debt.
//
// The guard does not come back with it, and the stopped timer is dropped for
// good. The guard's only sentence is streamCopyStillWorking, a reply still to
// come, which for a run that has already ended is false; worse, saying it would
// CONSUME the bubble, so the attempt this restore exists for would find nothing
// to seal and would speak beside the spinner after all. What closes the bubble
// instead is the publisher that comes back for the ending, and all four book
// one: a failure, a cancellation, a settle, and the answer — which has to,
// being the only publisher holding the words this round is being kept for
// (sayTheAnswer). If none comes, the sweep retires the round and hands the debt
// on. Caller holds s.mu.
func (s *streamStore) putRoundBackLocked(p pendingEnding) {
	p.entry.runEnded = true
	p.entry.guard = nil
	s.insertLocked(p.key, p.entry)
}

// sayEnding is the only way this store's ending ledger is written, and the
// whole of the contract at the top of this file lives in its shape.
//
// It finds the round k names, hands what it knows to say, and records what say
// reports — in that order, always. A caller never holds the right to speak as a
// value it could drop, so "claimed but never settled" is not a state that can
// be written: there is nothing to forget to resolve.
//
//	say returns nil            — the words were accepted for sending. The
//	                             promise is settled, the run goes on told, and
//	                             a guard's ending files the promise it just made.
//	say returns an error       — nothing reached the user, so nothing is told
//	                             (I1) and the next publisher of the same news
//	                             can still say it. A claimed promise returns to
//	                             where it sat. A round this store was holding is
//	                             left owed an ending even if it was owed none
//	                             before (I3), because its bubble went with the
//	                             attempt — unless the error says the words
//	                             provably reached nobody, in which case the
//	                             bubble did not go anywhere either and the round
//	                             goes back on the open list holding it. A run
//	                             this store held no round for is left exactly as
//	                             it was found: untouched. errNothingToSay is the
//	                             deliberate case and is recorded the same way.
//
// The address say returns is where it actually spoke, which is how a delivery
// that found its own chat in the binding row teaches the note an address it did
// not have. A zero address leaves the note's own alone.
//
// resolve is the auto-retry lookup, called only when the id on the event matches
// nothing this store holds — a clone carries a fresh id and inherits the round's
// own on chat_input_task_id. Once per attempt at speaking, so a caller that
// waited out another delivery and came back round asks again, the round having
// possibly appeared under either name in the meantime. It runs BEFORE anything
// is said, so whichever name finds the round, the words go out exactly once.
//
// ctx is the caller's own budget, and the only thing this function spends it on
// is waiting out a delivery already under way for the same run — see
// endingInFlight. A caller whose budget runs out while it waits says nothing
// this time round and gets errEndingDeferred back, which is this store telling
// it to return with a budget of its own: the delivery ahead is still running, so
// there is no outcome yet that could account for the news this caller came with.
//
// say is handed a context rather than closing over the caller's, because a wait
// that is WON costs exactly as much of the caller's budget as one that is lost —
// see deliveryBudget for what the remainder would otherwise buy.
func (s *streamStore) sayEnding(
	ctx context.Context,
	sessionID pgtype.UUID,
	k roundKey,
	ending roundEnding,
	resolve func(string) string,
	say func(context.Context, roundTurn) (roundAddress, error),
) (roundVerdict, error) {
	key := util.UUIDToString(sessionID)

	// The loop runs a second time only for a caller that waited out another
	// delivery and came away still owing the user words — because that delivery
	// was refused, or because what it landed was the guard's promise rather than
	// this run's ending. It is then the publisher the news has left, so it goes
	// round and reserves the ending itself. Each turn of the loop consumes one
	// COMPLETED delivery, whose reservation was given up under the lock that
	// released the waiter, so nothing spins: what the next turn finds is the
	// round on owed, or the round itself back on the open list because the
	// delivery ahead reached nobody at all — never the same wait again.
	for {
		s.mu.Lock()
		s.sweepLocked()
		turn, pending, begun := s.beginEndingLocked(key, k, ending)
		s.mu.Unlock()

		if turn.Verdict == roundForgotten && begun.worthResolving && resolve != nil && !k.byBatch && k.taskID != "" {
			if root := resolve(k.taskID); root != "" && root != k.taskID {
				s.mu.Lock()
				rootTurn, rootPending, rootBegun := s.beginEndingLocked(key, byTask(root), ending)
				s.mu.Unlock()
				if rootTurn.Verdict != roundForgotten {
					// The round was found under the batch owner's name, so the
					// ending is said in the clone's name too — a repeat carrying
					// the clone's own id has to find it on file or it goes looking
					// for a chat to repeat it in.
					turn, pending, begun = rootTurn, rootPending, rootBegun
					pending.alias = k.taskID
				}
			}
		}

		if turn.Verdict == roundForgotten && pending.live {
			// The one delivery with no note to keep its reservation on — a round
			// on file had its taken by beginEndingLocked, and this is the path
			// that reaches here with nothing reserved either way. Taken here
			// rather than in forgottenLocked because the clone lookup above can
			// replace this pending with a round's, and a reservation taken for a
			// name that then goes unused would be one nobody ever resolves.
			own, wait := s.reserveInFlight(pending.key, pending.taskID, pending.ending)
			begun.wait, pending.flight = wait, own
		}
		if begun.wait != nil {
			// Somebody else is speaking for this run right now. Nothing was
			// reserved for this caller, on either path, so there is nothing to
			// resolve whichever way the wait goes.
			select {
			case <-begun.wait.done:
				if begun.wait.delivered && begun.wait.ending == roundOver {
					// The words this caller came to say are on the user's
					// screen, put there by the publisher that got here first.
					return roundToldAlready, nil
				}
				// Everything else leaves this caller the publisher the run has
				// left, and what it comes back to is already filed: the outcome
				// was published under the lock that filed it. A refusal put the
				// promise back on owed, or left the round newly owed one. A
				// delivered PROMISE — the guard's, the one ending that ends
				// nothing — put the run on owed for exactly this caller to
				// spend: what is on the screen says a reply is still coming,
				// and the failure or the answer this caller came with is still
				// the news it was sent to deliver.
				continue
			case <-ctx.Done():
				// No budget left to speak with, so nothing is said and — this
				// caller holding no reservation — nothing is recorded. What it
				// still has is the news, and there is no fact yet that could
				// excuse it: the delivery ahead has not reported, and it may be
				// the guard's, which lands words and ends nothing. So this goes
				// back as a deferral rather than as silence, and the caller
				// comes again on a budget of its own.
				return turn.Verdict, errEndingDeferred
			}
		}
		if turn.Verdict == roundToldAlready {
			// Said already. Nothing was reserved, so there is nothing to resolve.
			return turn.Verdict, nil
		}
		// done() is deferred and endEnding is not, and the difference is what
		// each of them costs when the delivery panics out of a bus listener: an
		// undone budget leaves a timer armed for a whole streamCloseTimeout, and
		// the sweep has no way to find it, while the reservation endEnding would
		// have released is swept at inFlightMaxAge — see retireSpeakersLocked for
		// why that one is deliberately left to the sweep.
		sayCtx, done := deliveryBudget(ctx)
		defer done()
		addr, err := say(sayCtx, turn)
		s.endEnding(pending, addr, err)
		return turn.Verdict, err
	}
}

// deliveryBudget is what a delivery speaks on, which is deliberately not
// whatever its caller has left.
//
// One thing eats a caller's budget before a word goes out: the wait. A
// publisher parked on another delivery for the same run pays up to its whole
// deadline there, and losing that wait is not the costly half — that one comes
// back as errEndingDeferred and books a retry. WINNING it costs the same time
// and then hands the remainder to the send, the binding row and the
// installation row. When the remainder is nothing, those fail with a context
// error, the round has already been consumed, and there is no deferral to book
// a retry from because the wait did not lose.
//
// For a NOTICE that is a silent drop with the news still in hand, and it is
// what this budget exists to prevent. For an ANSWER it no longer is: a context
// error now takes answerRetryCause's default and books a refusal attempt
// (outbound.go). The budget stays because the notices are still on the old
// terms, and because a delivery that dies mid-send is worth not having whoever
// is carrying it — answer or notice — pay for it.
//
// So every delivery gets a fresh one, of the same length as the budget every
// closing frame written from a timer runs on. Detached from the caller
// altogether, and that is the part a condition cannot do: a caller runs out of
// time BEFORE the delivery — the wait — and it also gives up DURING one, at the
// binding row, at the installation row, at the ack. Both leave the same screen,
// because by then the round has been consumed and the wait did not lose, so
// neither has a deferral to come back on. Handing out the caller's remainder
// whenever it looks generous enough only covers the first, and only while a
// caller's budget and this constant are the same ten seconds
// (chatRunFlushTimeout, engine/router.go) — at eleven, a delivery whose caller
// gave up a moment later would die mid-send with the news still in hand.
//
// The price is the ceiling on a synchronous bus subscriber: an ending that
// waits out another delivery and then speaks can hold the publishing goroutine
// for the caller's budget and this one, back to back. handleEvent in
// outbound.go states that ceiling where it is paid. It is also the ceiling on a
// shutdown: a delivery under way when the process is stopping runs to its own
// deadline rather than the caller's, which is bounded and is the same trade the
// short-budget case has always made.
func deliveryBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), streamCloseTimeout)
}

// beginning is what beginEndingLocked found besides the turn itself.
type beginning struct {
	// wait is the delivery already speaking for this run. A caller handed one
	// has reserved nothing: it waits for that delivery's outcome, and goes quiet
	// or comes back round as the publisher the run has left depending on what
	// the outcome is — words that ENDED the round, or anything else. Losing that
	// wait to its own deadline is a third outcome and neither of the first two:
	// it leaves with errEndingDeferred, to return on a budget of its own.
	wait *endingInFlight
	// worthResolving says whether a second id is worth a database row: true only
	// when this session holds something a clone could still be reached through —
	// an open bubble, a promise, or a delivery on the wire.
	worthResolving bool
}

// refuseTakeLocked is the gate in front of takeAtLocked: two questions the note
// and the round answer between them, both of which only became askable once a
// round could come back from a failed ending (putRoundBackLocked). Before that a
// round on this list had never been spoken for and its run was still going, so
// finding it was the whole of the right to take it. Caller holds s.mu.
//
// The first is whether this round's ending has already been SAID. A round that
// came back can outlive its own ending: the sweep retires a holder that never
// reported, the next publisher claims the debt that leaves and delivers, and the
// holder then turns up naming a write that never reached the wire — so the round
// is handed back with the run on told. Taking it there puts a second copy of the
// same ending on the user's screen, which is the one thing the told list exists
// to prevent.
//
// The second is the guard meeting a round whose run is over. Its sentence
// promises a reply that is still coming, and there is none: what the user did
// was cancel the run, or the answer already happened and only its delivery
// failed. It would also consume the bubble the restore is keeping for the
// publisher that has the real words — see putRoundBackLocked.
//
// Refusing is roundToldAlready in both cases, which is that verdict's own
// meaning: nothing for THIS caller to say, and no delivery runs for it. The
// round stays on the list, because the publisher it IS waiting for is still
// entitled to seal the bubble.
func (s *streamStore) refuseTakeLocked(key string, entry *roundEntry, ending roundEnding) (roundTurn, bool) {
	note := s.noteLocked(key)
	silent := roundTurn{Addr: note.addr, Verdict: roundToldAlready}
	if note.isTold(entry.endingName()) {
		return silent, true
	}
	if entry.runEnded && ending == roundContinues {
		return silent, true
	}
	return roundTurn{}, false
}

// reserveInFlight takes the right to speak for a run this store holds no round
// for, or hands back the reservation somebody else is already holding. ending is
// what THIS caller would be saying, and it is recorded on the reservation for
// whoever waits behind it — see endingInFlight.ending.
func (s *streamStore) reserveInFlight(key, taskID string, ending roundEnding) (own, wait *endingInFlight) {
	if taskID == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := inflightKey{key: key, taskID: taskID}
	if held, ok := s.inflight[k]; ok {
		return nil, held
	}
	f := &endingInFlight{done: make(chan struct{}), ending: ending, at: s.now()}
	s.inflight[k] = f
	return f, nil
}

// releaseInFlightLocked publishes a delivery's outcome to whoever is waiting on
// it and gives the reservation up. The map entry goes only if it is still this
// delivery's: a reservation the sweep retired may already have been taken over
// by a later publisher, whose right to speak is not this one's to withdraw. Its
// own waiters were released by the sweep, and report says so once. Caller holds
// s.mu.
func (s *streamStore) releaseInFlightLocked(k inflightKey, f *endingInFlight, delivered bool) {
	if s.inflight[k] == f {
		delete(s.inflight, k)
	}
	f.report(delivered)
}

// beginEndingLocked finds the round and reserves its ending, or hands back the
// delivery already saying it. It is half of sayEnding and has no other caller:
// on its own it produces exactly the unresolved claim this file exists to make
// unrepresentable.
//
// The third return is everything about the attempt that is not the turn — see
// beginning. Caller holds s.mu.
func (s *streamStore) beginEndingLocked(key string, k roundKey, ending roundEnding) (roundTurn, pendingEnding, beginning) {
	if i := s.indexLocked(key, k); i >= 0 {
		if why, refused := s.refuseTakeLocked(key, s.sessions[key][i], ending); refused {
			return why, pendingEnding{}, beginning{}
		}
		turn, pending := s.takeAtLocked(key, i, ending)
		return turn, pending, beginning{}
	}
	// No bubble. Whether a clone could still find one is what the open list
	// answers; whether it could find a promise is what the note answers.
	worthResolving := len(s.sessions[key]) > 0

	note, ok := s.ended[key]
	if !ok {
		return s.forgottenLocked(key, k.taskID, ending, worthResolving)
	}
	if s.now().Sub(note.at) > roundMemory {
		delete(s.ended, key)
		return s.forgottenLocked(key, k.taskID, ending, worthResolving)
	}
	// An ending already told is deliberately NOT a reason to go looking for a
	// second id. told is a dedup of ONE run's ending, matched by that run's own
	// id (I2), and a retry clone is a different run: reading the owner's note
	// as the clone's silence is defect 3 in another coat, and it would swallow
	// the retry's answer whenever the first attempt's own failure had already
	// been reported. The one honest link between the two names is a delivery
	// that actually went out under both, which endEnding records as the alias.
	worthResolving = worthResolving || note.isOwed() || len(note.speaking) > 0

	if !note.addr.known() {
		// Nowhere to speak. Not a refusal — the caller finds its own chat.
		return s.forgottenLocked(key, k.taskID, ending, worthResolving)
	}
	taskID := k.taskID
	if k.byBatch {
		// A batch that matched no open round normally has nothing left to end.
		// The exception is the round that never became a run: its ending is
		// filed under the batch's own name (batchRunID), so a debt left by a
		// delivery nothing accepted is here and this is the only lookup that can
		// find it. Not forgotten in either case — a caller told to go and find a
		// chat would announce an ending it cannot attribute to any round in it.
		bid := batchRunID(k.batch)
		if at := note.speakingAt(bid); at >= 0 {
			// The same wait the by-task lookup below does, for the same reason:
			// a delivery on the wire is not a fact about the screen. Two
			// publishers name a task-less round by its batch — the guard at nine
			// minutes and the settled flush — and the guard's is the one that
			// lands a promise and ends nothing, so a settle behind it has to
			// come back round and say why the run never started rather than
			// leave the asker on streamCopyStillWorking for a run that will
			// never report anything.
			return roundTurn{Addr: note.addr, Verdict: roundToldAlready}, pendingEnding{},
				beginning{wait: note.speaking[at].flight}
		}
		at := indexOfRun(note.owed, bid)
		if at < 0 {
			return roundTurn{Addr: note.addr, Verdict: roundToldAlready}, pendingEnding{}, beginning{}
		}
		note.owed = withoutRun(note.owed, at)
		pending := pendingEnding{live: true, key: key, taskID: bid, ending: ending, owedAt: at, held: true}
		note = s.speakLocked(note, &pending)
		note.at = s.now()
		s.ended[key] = note
		return roundTurn{Addr: note.addr, Promised: true, Verdict: roundOwesAnEnding}, pending, beginning{}
	}
	if taskID == "" {
		// An unnamed ending claims nothing for the same reason no unnamed
		// promise is ever filed: there is no round it could be speaking for.
		return roundTurn{Addr: note.addr, Verdict: roundToldAlready}, pendingEnding{}, beginning{}
	}
	if note.isTold(taskID) {
		return roundTurn{Addr: note.addr, Verdict: roundToldAlready}, pendingEnding{}, beginning{}
	}
	if at := note.speakingAt(taskID); at >= 0 {
		// Being said right now, on another goroutine — which is not the same
		// fact as having been said, and answering as though it were is defect 5.
		// The caller waits on that delivery instead: silent only if what landed
		// was this run's own ending, and otherwise the publisher the news has
		// left. The verdict alongside is what it falls back to with no budget
		// left to wait, and it says nothing about the screen there — what the
		// caller acts on in that case is errEndingDeferred, which tells it to
		// come back, because this delivery may yet turn out to have said
		// nothing that accounts for the news the caller is carrying.
		return roundTurn{Addr: note.addr, Verdict: roundToldAlready}, pendingEnding{},
			beginning{wait: note.speaking[at].flight}
	}
	at := indexOfRun(note.owed, taskID)
	if at < 0 {
		// The promises on file belong to other rounds and this run has never
		// been spoken for. Not this caller's to spend (I2), and not a reason
		// for silence: the caller finds its own address, and whatever it says
		// there is still recorded — see forgottenLocked.
		return s.forgottenLocked(key, taskID, ending, worthResolving)
	}
	note.owed = withoutRun(note.owed, at)
	pending := pendingEnding{live: true, key: key, taskID: taskID, ending: ending, owedAt: at, held: true}
	note = s.speakLocked(note, &pending)
	note.at = s.now()
	s.ended[key] = note
	return roundTurn{Addr: note.addr, Promised: true, Verdict: roundOwesAnEnding}, pending, beginning{}
}

// forgottenLocked is the verdict for a run this store holds nothing for, and it
// still reserves an ending.
//
// That is the point. "Nothing on file" is not "nothing happened": the caller
// goes and finds the chat in the binding row and says the run's ending there,
// and a delivery the ledger never hears about is exactly defect 2 — the words
// go out, nothing is recorded, and the next publisher of the same news repeats
// them. So the pending is live, and a successful delivery files the note that
// keeps the repeat quiet, addressed wherever the caller actually spoke.
//
// No note is created or touched HERE, and nothing joins speaking, because at
// this point there is no evidence the session is even WeCom's — this subscriber
// sees every failed run on a shared bus. Only words that actually reached a
// WeCom chat produce a note; a delivery that FAILED writes nothing at all, which
// is what the pending's held flag carries down to endEnding. Caller holds s.mu.
func (s *streamStore) forgottenLocked(key, taskID string, ending roundEnding, worthResolving bool) (roundTurn, pendingEnding, beginning) {
	turn := roundTurn{Verdict: roundForgotten}
	begun := beginning{worthResolving: worthResolving}
	if taskID == "" {
		return turn, pendingEnding{}, begun
	}
	return turn, pendingEnding{live: true, key: key, taskID: taskID, ending: ending, owedAt: -1}, begun
}

// endEnding is the other half of sayEnding: it records the delivery's account
// of itself, and it is where all three invariants are actually paid.
//
// It takes the delivery's error rather than a bare "did it land", because two
// failures that read the same from here are not the same fact about the user's
// screen. Everything the ledger records still turns on nil / not nil; what the
// error decides on top of that is whether the round's BUBBLE survived the
// attempt — see keepsItsBubble, and bubbleSurvivedTheFailure for the one group
// of failures this package can place on the near side of the wire.
//
// A failure reaches told for nothing (I1) and returns a claimed promise to
// exactly where it sat, which is what makes a send refused during a reconnect
// window a retry rather than a loss. What it leaves for the next publisher is
// one of two things, and the error is what picks:
//
//   - the round itself, back on the open list with the handle it had, when the
//     words provably reached nobody. The spinner is still on the screen and
//     still writable, so the next publisher SEALS it instead of speaking beside
//     it. Nothing goes on owed here: the round is on file, which knowsRound
//     reads the same way, and I3 is satisfied by the better of the two records
//     for as long as that record lasts. The sweep files the debt if the round
//     reaches the end of the protocol's window with nobody having come for it.
//   - the run owed an ending it was not owed before (I3), for every other
//     failure of a round this store was holding. The bubble was consumed and may
//     be carrying words already, so only a later ending said somewhere else can
//     account for it.
//
// A failure for a run this store held no round for writes nothing whatsoever —
// see held on pendingEnding for why that asymmetry is the point rather than an
// omission. What every path does report is the outcome itself, to the publisher
// waiting behind it: releasing the reservation is the one thing that happens
// before the live check, because a delivery that reserved the right to speak has
// to hand on what became of it whether or not there was anything to record.
//
// It happens under the lock that files the rest, which is what a waiter that
// comes back round depends on: by the time it can re-enter the store, the
// promise it is coming back for is already on owed, or the round it is coming
// back for is already on the open list.
//
// What it gives up is its OWN reservation, found by identity rather than by run
// — see speakingByFlight. A delivery the sweep already retired can find a later
// publisher's entry under the same run, and taking that one would leave live
// words on the wire with nothing excluding their repeats.
func (s *streamStore) endEnding(p pendingEnding, addr roundAddress, err error) {
	delivered := err == nil

	s.mu.Lock()
	defer s.mu.Unlock()
	if p.flight != nil {
		s.releaseInFlightLocked(inflightKey{key: p.key, taskID: p.taskID}, p.flight, delivered)
	}
	if !p.live {
		return
	}
	note, ok := s.ended[p.key]
	if !ok {
		if !delivered {
			// Nothing was reserved in a note and nothing was said. There is no
			// reason to start remembering this session now.
			//
			// Nor is a round put back here: the take that lifts one writes this
			// note in the same breath, so a note that has gone missing under a
			// live delivery is a session something else has already forgotten.
			return
		}
		// Words reached a WeCom chat for a round this store had no note for —
		// a run that outlived a restart, or one whose bubble was never
		// painted. This is that note.
		note = endedRound{}
	}
	if at := note.speakingByFlight(p.flight); at >= 0 {
		note.speaking = withoutSpeaker(note.speaking, at)
	}
	note.at = s.now()
	if !delivered {
		if p.owedAt >= 0 {
			// A claimed promise goes back where it sat whatever became of the
			// bubble: the two are separate facts, and this one is only ever
			// claimed from the note.
			note.owed = restoreRun(note.owed, p.owedAt, p.taskID)
		}
		switch {
		case p.keepsItsBubble(err):
			// The words provably reached nobody, so the round is exactly as it
			// was a moment ago: a spinner on somebody's screen, on a stream the
			// server has heard nothing about since it was opened. Put it back
			// rather than filing a debt against it — the publisher that comes
			// next then writes a closing frame over that spinner instead of
			// speaking beside it, which is the whole of what the user sees. The
			// debt is not lost, only deferred: sweepLocked files it if the round
			// reaches the end of the protocol's window still unanswered.
			s.putRoundBackLocked(p)
		case p.owedAt < 0 && p.held && note.addr.known():
			// A round this store was holding, nothing claimed, nothing landed,
			// and nothing that can be put back. Its bubble has been consumed, so
			// what the user is looking at is a spinner nothing will ever seal —
			// and this store knows the chat it is in. Recording the run as owed
			// is what makes the next publisher of its ending say the words there
			// instead of finding the round gone and going quiet.
			//
			// held is what keeps this off a run that was never this adapter's.
			// The address is a SESSION-level fact — one earlier WeCom round
			// leaves it on the note and it outlives every round after — so
			// without held, an answer the installer asked for in their browser,
			// whose delivery this replica could not make, would file itself as
			// owed on a session it merely shares. knowsRound would then read
			// that debt as proof the question came from the room and wave the
			// run's failure notice into the chat with no database check at all.
			note.owed = note.owe(p.taskID)
		}
		s.ended[p.key] = note
		return
	}
	if addr.known() {
		note.addr = addr
	}
	switch p.ending {
	case roundContinues:
		// "还在处理，完成后我再单独回复你" is on the user's screen, so the
		// promise now exists. It is the one ending that files a promise instead
		// of settling one, and the one that tells nothing: the round goes on.
		note.owed = note.owe(p.taskID)
	case roundOver:
		// The promise was claimed at begin and the words have landed, so it
		// stays settled. This run has now been spoken for; a repeat of its own
		// ending stays silent, another run's ending is not covered by it.
		note.told = note.tell(p.taskID)
		if p.alias != "" {
			note.told = note.tell(p.alias)
		}
	}
	s.ended[p.key] = note
}

// owesEnding reports whether a run's promise is still outstanding — nothing has
// been said for it and nothing is being said right now. Read-only; for the
// wiring guards and the tests that assert what a path left behind.
func (s *streamStore) owesEnding(sessionID pgtype.UUID, taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	note, ok := s.ended[util.UUIDToString(sessionID)]
	if !ok || s.now().Sub(note.at) > roundMemory {
		return false
	}
	return indexOfRun(note.owed, taskID) >= 0
}

// wasTold reports whether a run's ending has been recorded as said. Read-only;
// the other half of what owesEnding inspects.
func (s *streamStore) wasTold(sessionID pgtype.UUID, taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	note, ok := s.ended[util.UUIDToString(sessionID)]
	if !ok {
		return false
	}
	return note.isTold(taskID)
}

// knowsRound reports whether this process holds local evidence that a run
// belongs to a WeCom round of this session: a round bound to it, or an ending
// owed to it. Both trace back to the inbound path — a round is opened by a
// message this adapter ingested and named by the flush that answered it, and
// every entry on owed comes from a round that was on that list — so either one
// is positive proof of where the question was asked, needing no database.
//
// A round can be on that list because an ending was ATTEMPTED for it and
// reached nobody (putRoundBackLocked). That changes nothing here: it is the
// same round, opened by the same ingested message, and the failed attempt is
// exactly why it is still worth answering yes for.
//
// That second half only holds because owed is written for a round this store
// was HOLDING and for nothing else. Three places write it and all three are
// that: the held flag gates endEnding's; the sweep's retired speakers come off
// the note's speaking list, which only a round taken off the open list or
// claimed against its own promise ever joins; and the debt the sweep hands on
// when it retires a round that was put back comes off that round itself
// (handOnTheDebtLocked). The delivery with no round of its own keeps its
// reservation in the inflight map, where no origin question reads it. A ledger
// that filed a debt for any run whose delivery failed would put runs asked in
// the installer's browser on this list, and this function would then answer yes
// for them — an authorization question settled by a bus event the caller does
// not control. Read owed as "a round of ours is owed words", never as "the store
// once tried to speak for this id".
//
// It is deliberately not the whole of the origin question: a run with nothing
// on file here may still have come from WeCom, and that case is the row's to
// answer (failureBelongsOnWecom).
func (s *streamStore) knowsRound(sessionID pgtype.UUID, taskID string) bool {
	if taskID == "" {
		return false
	}
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.sessions[key] {
		if r.taskID == taskID {
			return true
		}
	}
	note, ok := s.ended[key]
	if !ok || s.now().Sub(note.at) > roundMemory {
		return false
	}
	for _, id := range note.owed {
		if id == taskID {
			return true
		}
	}
	return false
}

// holding reports whether this store has anything on file anywhere: a round on
// some session's open list, painted or not, or a session's ended note. It is
// the "nothing here to close" test at the head of the two ending subscribers.
//
// Unpainted rounds count, and that is the point. depth() screens on painted
// because it answers "how many bubbles are on screen"; a round bound to a run
// whose opening frame is still in flight has no bubble yet and is exactly the
// one whose ending must not be dropped — retiring it is what makes the late
// paint a no-op (open returns roundFinished), and skipping it leaves a spinner
// nothing will ever close.
func (s *streamStore) holding() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ended) > 0 {
		return true
	}
	for _, rounds := range s.sessions {
		if len(rounds) > 0 {
			return true
		}
	}
	return false
}

// knowsSession reports whether this process holds anything at all for a
// session: a round on the open list, or a note left by one that has ended.
// Both are written only by this adapter's inbound path, so either settles that
// the session is WeCom's without asking the database.
//
// Weaker than knowsRound, and for a different job. knowsRound answers "did
// THIS run come from the room", which is an authorization question and has to
// name the run. This one answers "is this session ours at all", which is what
// keeps another channel's failed run off the database entirely.
func (s *streamStore) knowsSession(sessionID pgtype.UUID) bool {
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions[key]) > 0 {
		return true
	}
	note, ok := s.ended[key]
	return ok && s.now().Sub(note.at) <= roundMemory
}

// drop forgets a round without sending anything — used when the opening frame
// was refused and the bubble the handle describes never existed.
func (s *streamStore) drop(sessionID pgtype.UUID, batch engine.RunBatchID) {
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	rounds := s.sessions[key]
	for i, r := range rounds {
		if r.batch != batch {
			continue
		}
		if r.guard != nil {
			r.guard.Stop()
		}
		rounds = append(rounds[:i], rounds[i+1:]...)
		if len(rounds) == 0 {
			delete(s.sessions, key)
		} else {
			s.sessions[key] = rounds
		}
		return
	}
}

// depth reports how many bubbles are open across all sessions. Diagnostics,
// tests, and the cheap rejection at the head of the failure subscriber.
func (s *streamStore) depth() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, rounds := range s.sessions {
		for _, r := range rounds {
			if r.painted {
				n++
			}
		}
	}
	return n
}

func (s *streamStore) expiredLocked(createdAt time.Time) bool {
	return s.now().Sub(createdAt) > s.maxAge
}

// sweepLocked evicts rounds the server would no longer accept, the notes left by
// rounds too old to still be running, and the reservations of deliveries that
// never reported — both places one of those is kept. The guard timer normally
// retires a round long before this fires; the sweep is what keeps a process
// whose timers were beaten by a clock jump from accumulating entries forever,
// and it is the only thing that bounds the notes and the reservations, which no
// timer touches. It runs at the head of every ending and every open, so a stale
// reservation is gone before the publisher that would have parked on it looks.
//
// One round it evicts is not stale bookkeeping but an unpaid debt, and that one
// it hands on rather than drops: a round put back because its ending reached
// nobody (runEnded). While it was on the list it WAS the record — knowsRound,
// the next publisher and I3 all read it there — and the bubble is what made it
// the better record of the two. Past the protocol's window the handle is worth
// nothing and the round stops being read at all, so what I3 asks for goes back
// to being the debt. Without the handover the record's life would have been
// quietly cut from roundMemory to streamMaxAge by a change that only meant to
// keep a bubble. Caller holds s.mu.
func (s *streamStore) sweepLocked() {
	for key, rounds := range s.sessions {
		live := rounds[:0]
		for _, r := range rounds {
			if s.expiredLocked(r.createdAt) {
				if r.guard != nil {
					r.guard.Stop()
				}
				s.handOnTheDebtLocked(key, r)
				continue
			}
			live = append(live, r)
		}
		if len(live) == 0 {
			delete(s.sessions, key)
		} else {
			s.sessions[key] = live
		}
	}
	now := s.now()
	for key, round := range s.ended {
		if now.Sub(round.at) > roundMemory {
			delete(s.ended, key)
			continue
		}
		if next, retired := s.retireSpeakersLocked(round, now); retired {
			s.ended[key] = next
		}
	}
	for k, f := range s.inflight {
		if now.Sub(f.at) > inFlightMaxAge {
			// Nobody is coming back for it. Release whoever is waiting as "not
			// delivered", which is the only honest answer: no delivery ever
			// reported that these words reached anyone.
			s.releaseInFlightLocked(k, f, false)
		}
	}
}

// handOnTheDebtLocked leaves a run owed its ending as the sweep takes away the
// round that was standing in for that debt. Caller holds s.mu.
//
// Only a round put back by a failed ending has one to hand on. Every other round
// the sweep evicts has never been spoken for: its run may still be going, and a
// debt filed for it would be a promise nobody made about words nobody has. That
// silent drop is the one an untaken round has always had, and it is unchanged.
//
// Not for a run already on told, which the sweep can reach: a round put back by a
// holder the sweep had retired comes back to a run somebody else has since
// spoken for (refuseTakeLocked), and filing a debt would hand the next publisher
// the right to say the same ending a second time.
//
// The address gate and the shape of the filing are retireSpeakersLocked's, for
// its reasons: a note with no address is one beginEndingLocked answers
// roundForgotten for, so a debt on it could never be handed to anybody, and owe
// dedups by id so a second filing is a no-op. The note's own clock is
// deliberately not touched. It has been running since the delivery that failed
// wrote this note, which is exactly where the debt would have started had the
// round never been put back.
func (s *streamStore) handOnTheDebtLocked(key string, r *roundEntry) {
	if !r.runEnded {
		return
	}
	note, ok := s.ended[key]
	if !ok || !note.addr.known() {
		return
	}
	name := r.endingName()
	if name == "" || note.isTold(name) {
		return
	}
	note.owed = note.owe(name)
	s.ended[key] = note
}

// retireSpeakersLocked drops the note's reservations whose holder never came
// back, and leaves each of those runs owed its ending. Caller holds s.mu.
//
// The note needs this for the same reason the inflight map does, and the
// reservation is the same reservation — see endingInFlight. A delivery that
// panicked out of a bus listener never reaches endEnding, because sayEnding
// calls it on the line after say rather than from a defer, and events.Bus
// recovers the panic and carries on with the next listener. Without a sweep the
// entry it left behind outlives it for the note's whole hour, and what that
// costs got worse when the exclusion became a wait: every later publisher of
// that run now parks on a delivery that will never report until its own budget
// runs out — streamCloseTimeout — and Publish runs its listeners one after
// another, so every other task:failed listener registered behind it waits too.
//
// The debt is filed the way a refusal this package cannot account for files it
// (I3): a holder that never reported said nothing about where its words got to,
// and the round's bubble went with the take, so what the asker has is a spinner
// nothing can seal and the next publisher has to be the one that says so. The
// round is not put back here for exactly that reason — putRoundBackLocked is for
// a delivery that came back and named its failure, and this one never came back
// at all. That the debt may be filed twice — swept here, then filed again by a
// holder that turns up after all — is what this was declined for once, and it is
// not a real cost: owe already dedups by id, and restoreRun does too, so the
// second filing is a no-op. The address gate
// is endEnding's, for the reason endEnding has it: a note with no address is one
// beginEndingLocked answers roundForgotten for, so a debt filed on it is a debt
// no publisher could ever be handed.
func (s *streamStore) retireSpeakersLocked(note endedRound, now time.Time) (endedRound, bool) {
	retired := false
	for i := 0; i < len(note.speaking); {
		sp := note.speaking[i]
		if now.Sub(sp.flight.at) <= inFlightMaxAge {
			i++
			continue
		}
		note.speaking = withoutSpeaker(note.speaking, i)
		if note.addr.known() {
			note.owed = note.owe(sp.taskID)
		}
		sp.flight.report(false)
		retired = true
	}
	return note, retired
}

// roundTaker matches a task lifecycle event to the round it belongs to. Both
// halves of the store's identity live behind it: the binding the flush filed,
// and the one column that resolves an auto-retry clone back to it.
type roundTaker struct {
	streams *streamStore
	tasks   taskLookup
	log     *slog.Logger
}

// sayEnding is roundTaker's one job: sayEnding on the store, with the auto-retry
// lookup supplied. Everything the contract at the top of this file promises
// holds here unchanged — the delivery goes in, the record comes out of what it
// reports, and no caller ever holds an unresolved claim.
//
// The id on the event is tried first, because that is the id the flush bound.
// An auto-retry clone is the one case it does not match: FailTask creates the
// clone with a fresh id and it inherits the parent's chat_input_task_id, which
// is the round's own task id (EnqueueChatTask stamps chat_input_task_id = id on
// the turn it creates). So a clone's ending is routed by reading that column,
// not by falling back to whichever round is at the head — the round a clone
// belongs to is on file, it is just filed under the batch's owner.
//
// The lookup costs one read, and the store only asks for it on a miss in a
// session that holds something a second id could match. Without a task lookup
// configured the miss is simply a miss: the delivery falls back to whatever it
// can find on its own.
//
// With no store at all the in-place reply is disabled, so the delivery still
// runs — an answer has to reach the user either way — and nothing is recorded.
func (r roundTaker) sayEnding(
	ctx context.Context,
	sessionID pgtype.UUID,
	k roundKey,
	ending roundEnding,
	say func(context.Context, roundTurn) (roundAddress, error),
) (roundVerdict, error) {
	if r.streams == nil {
		sayCtx, done := deliveryBudget(ctx)
		defer done()
		_, err := say(sayCtx, roundTurn{Verdict: roundForgotten})
		return roundForgotten, err
	}
	return r.streams.sayEnding(ctx, sessionID, k, ending,
		func(taskID string) string { return r.rootTaskID(ctx, taskID) }, say)
}

// rootTaskID reads the input batch a task belongs to — its own id for a first
// attempt, the parent's for an auto-retry clone. Empty when there is nothing
// to gain from asking. Whether asking is worth a row is the caller's call:
// take asks only while a bubble is open, claim and settle only while a promise
// is outstanding.
func (r roundTaker) rootTaskID(ctx context.Context, taskID string) string {
	if r.tasks == nil {
		return ""
	}
	id, err := util.ParseUUID(taskID)
	if err != nil || !id.Valid {
		return ""
	}
	task, err := r.tasks.GetAgentTask(ctx, id)
	if err != nil {
		if r.log != nil {
			r.log.DebugContext(ctx, "wecom stream: cannot read the run behind an ending",
				"task_id", taskID, "error", err)
		}
		return ""
	}
	if !task.ChatInputTaskID.Valid {
		return ""
	}
	return util.UUIDToString(task.ChatInputTaskID)
}
