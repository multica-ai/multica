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
// session (ClaimAgentTask), so the oldest round is the running one and
// everything behind it is waiting. Nothing consumes that order, though —
// QueuedBehind compares batch ids rather than list positions, and it is
// decided once when the round opens and never revised. The sorting is for
// whoever reads the list, not for a caller that depends on it.
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
	// roundToldAlready — the answer landed, another publisher's failure got
	// here first, or the words are going out on another goroutine right now.
	// Nothing to add.
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
	// roundFinished — this run's bubble is already closed. Only a badly
	// delayed OnIngested reaches this: the goroutine that paints the bubble
	// outlived the run it was painting for. Painting now would open a bubble
	// whose answer has already been delivered and which nothing would close.
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
// owes it. Four review rounds have found four different ways to get that wrong,
// and all four were the same mistake — the ledger recorded an outcome the user
// never got:
//
//	1. a promise spent by POSITION, so one round's words settled another
//	   round's promise and the round the user cared about was never told;
//	2. a delivery path that sent the words and settled NOTHING, leaving the
//	   promise on file for the next repeat of that run's failure to spend
//	   underneath the answer the user had just read;
//	3. "already told" read off the SESSION rather than the run, so a second
//	   asker was silenced by a note an unrelated run had left;
//	4. a promise recorded as kept BEFORE the send meant to keep it, so a send
//	   refused during a WebSocket reconnect lost the promise for good.
//
// Scenario tests were written for each and did not stop the next one, because
// each fix was a rule the next caller had to remember. So the ledger states its
// invariants and the API enforces them instead:
//
//	I1. NOTHING IS RECORDED AS SAID UNTIL IT HAS BEEN SAID. A run reaches told,
//	    and a promise leaves owed, only after a delivery reports that the words
//	    were accepted for sending.
//	I2. A RUN'S ENDING IS MATCHED BY THE RUN'S OWN ID — never by position,
//	    never by session. A promise another round is waiting on is not this
//	    run's to spend, and a note another round left is not this run's reason
//	    for silence.
//	I3. EVERY TERMINAL PATH ENDS IN WORDS OR LEAVES THE RUN OWED AN ENDING. No
//	    path may both stay silent and clear the record. A delivery that failed
//	    puts a claimed promise back where it was, and for a round this store
//	    WAS HOLDING it records that the run is still owed one — so the next
//	    publisher of the same news says it, rather than finding it filed as
//	    already said. A run this store held no round for leaves no trace: it
//	    was owed nothing here, and a debt filed against it would be
//	    indistinguishable from the inbound path's own record of the round —
//	    which is what knowsRound reads as proof of where a question was asked.
//
// sayEnding is the only way the ledger is written, and it takes the delivery as
// an argument rather than handing out the right to speak. A caller cannot hold
// a claim it forgets to resolve because it never holds one: it is given the
// round, it reports whether the words went out, and the store records that and
// nothing else. Adding a fifth way to end a round means writing another say
// function, which cannot be written in the wrong order.
//
// The one thing that is NOT conditional is the bubble. A handle is consumed by
// whichever closer reaches the round first — that is what makes two racing
// closers produce one closing frame — so it is taken under the same lock that
// finds the round, whether or not the frame it is taken for lands.
// ────────────────────────────────────────────────────────────────────────────

// errNothingToSay is how a delivery reports that it declined to speak: a cancel
// for a round this process has no record of, an empty completion nobody is owed
// an ending for, a session with no WeCom binding at all. Nothing reached the
// user, so under I1 nothing is recorded as SAID — and unlike a refused send it
// is not worth a warning, because no words were ever going out. It is not the
// same as nothing having happened: a round this store was holding has had its
// bubble consumed either way, so I3 still leaves that run owed an ending.
var errNothingToSay = errors.New("wecom: nothing to say for this round")

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

	// Promised says the guard closed this round's bubble with "还在处理，完成后
	// 我再单独回复你" and this turn holds that promise. It is what separates
	// "nothing to add" from "a promise to keep" when a run finishes silently,
	// and what lets a cancel speak without chasing an address it should not.
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
}

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
	// finished lists the batches whose bubble is over, so a badly delayed
	// OnIngested cannot paint a second one for a run that has already
	// answered. Bounded: a batch old enough to have fallen off cannot still
	// have a message in flight for it.
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
	// stays silent, and if the words never land the run comes off this list and
	// back onto owed, where the next publisher can still keep the promise.
	speaking []string
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

// isSpeaking reports whether this run's ending is on the wire right now.
func (e endedRound) isSpeaking(taskID string) bool { return indexOfRun(e.speaking, taskID) >= 0 }

// indexOfRun finds a run in one of the note's lists, or -1. Every match in this
// file goes through it: I2 says a run is matched by its own id and by nothing
// else, and one lookup is one place for that to stay true.
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

// restoreRun puts a claimed promise back where it was, which is what I3 owes a
// delivery that failed. Position carries no meaning any more — every match is
// by id — but a list that comes back in the order it went out is one less thing
// for a future reader to wonder about.
func restoreRun(runs []string, at int, taskID string) []string {
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
	// stream window runs out.
	guard *time.Timer

	// createdAt bounds the entry for the sweep when there is no handle to read
	// a time off.
	createdAt time.Time
}

// streamStore maps chat_session_id to that session's rounds, oldest first, and
// — for a while after the last one is gone — to what the session still owes.
type streamStore struct {
	mu       sync.Mutex
	sessions map[string][]*roundEntry
	ended    map[string]endedRound

	maxAge time.Duration
	now    func() time.Time
}

func newStreamStore() *streamStore {
	return &streamStore{
		sessions: make(map[string][]*roundEntry),
		ended:    make(map[string]endedRound),
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

// queuedBehind reports whether any OLDER round of this session is still on
// file — this round will wait for it, and an empty answer of its own then
// means "the reply ahead of it covered this", not plain silence.
func (e *roundEntry) queuedBehind(rounds []*roundEntry) bool {
	for _, r := range rounds {
		if r.batch < e.batch {
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
func (s *streamStore) bind(sessionID pgtype.UUID, batch engine.RunBatchID, taskID string) {
	if taskID == "" || batch == 0 {
		return
	}
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if note, ok := s.ended[key]; ok && note.isFinished(batch) {
		return
	}
	if e := s.entryLocked(key, batch); e != nil {
		e.taskID = taskID
		return
	}
	s.insertLocked(key, &roundEntry{batch: batch, taskID: taskID, createdAt: s.now()})
}

// arm attaches the expiry guard to a round. A round that ended between the
// open and this call has already left the list, so there is nothing to guard
// and the timer is stopped instead of leaked.
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

// takeAtLocked removes rounds[i] and reserves its ending — the ending of a
// round, whichever ending it is.
//
// The handle goes unconditionally, because it is the mutual exclusion that
// makes two racing closers write one closing frame. A round with no bubble
// reports absent, and so does a handle past maxAge: the server would refuse the
// frame and a caller that believed it had a bubble would leave the user with
// nothing.
//
// What the round IS gets filed here too, because none of it depends on words
// reaching anyone: the batch is over, so a badly delayed OnIngested cannot
// paint a second bubble for a run that has already answered, and this is the
// chat the round was speaking in. What the round has been TOLD is the only part
// that waits for the delivery (I1), and the pendingEnding is what carries it
// there. Caller holds s.mu.
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

	pending := pendingEnding{key: key, taskID: entry.taskID, ending: ending, owedAt: -1, held: true}
	promised := false
	if entry.taskID != "" {
		pending.live = true
		// A round back on the open list while its own promise is outstanding is
		// not a shape this store produces, but spending that promise twice
		// would be — so it is claimed here exactly the way a claim claims it.
		if at := indexOfRun(note.owed, entry.taskID); at >= 0 {
			note.owed = withoutRun(note.owed, at)
			pending.owedAt, promised = at, true
		}
		note.speaking = append(note.speaking, entry.taskID)
	}
	note.at = s.now()
	s.ended[key] = note

	turn := roundTurn{Addr: addr, Promised: promised, Verdict: roundOwesAnEnding}
	if entry.painted && !s.expiredLocked(entry.handle.CreatedAt) {
		turn.Handle, turn.HasBubble = entry.handle, true
	}
	return turn, pending
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
//	                             where it sat, and a round this store was
//	                             holding is left owed an ending even if it was
//	                             owed none before (I3) — its bubble went with
//	                             the attempt. A run this store held no round for
//	                             is left exactly as it was found: untouched.
//	                             errNothingToSay is the deliberate case and is
//	                             recorded the same way.
//
// The address say returns is where it actually spoke, which is how a delivery
// that found its own chat in the binding row teaches the note an address it did
// not have. A zero address leaves the note's own alone.
//
// resolve is the auto-retry lookup, called at most once and only when the id on
// the event matches nothing this store holds — a clone carries a fresh id and
// inherits the round's own on chat_input_task_id. It runs BEFORE anything is
// said, so whichever name finds the round, the words go out exactly once.
func (s *streamStore) sayEnding(
	sessionID pgtype.UUID,
	k roundKey,
	ending roundEnding,
	resolve func(string) string,
	say func(roundTurn) (roundAddress, error),
) (roundVerdict, error) {
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	s.sweepLocked()
	turn, pending, worthResolving := s.beginEndingLocked(key, k, ending)
	s.mu.Unlock()

	if turn.Verdict == roundForgotten && worthResolving && resolve != nil && !k.byBatch && k.taskID != "" {
		if root := resolve(k.taskID); root != "" && root != k.taskID {
			s.mu.Lock()
			rootTurn, rootPending, _ := s.beginEndingLocked(key, byTask(root), ending)
			s.mu.Unlock()
			if rootTurn.Verdict != roundForgotten {
				// The round was found under the batch owner's name, so the
				// ending is said in the clone's name too — a repeat carrying
				// the clone's own id has to find it on file or it goes looking
				// for a chat to repeat it in.
				turn, pending = rootTurn, rootPending
				pending.alias = k.taskID
			}
		}
	}

	if turn.Verdict == roundToldAlready {
		// Said already, or being said on another goroutine right now. Nothing
		// was reserved, so there is nothing to resolve.
		return turn.Verdict, nil
	}
	addr, err := say(turn)
	s.endEnding(pending, addr, err == nil)
	return turn.Verdict, err
}

// beginEndingLocked finds the round and reserves its ending. It is half of
// sayEnding and has no other caller: on its own it produces exactly the
// unresolved claim this file exists to make unrepresentable.
//
// The third return says whether a second id is worth a database row: true only
// when this session holds something a clone could still be reached through — an
// open bubble, or a promise. Caller holds s.mu.
func (s *streamStore) beginEndingLocked(key string, k roundKey, ending roundEnding) (roundTurn, pendingEnding, bool) {
	if i := s.indexLocked(key, k); i >= 0 {
		turn, pending := s.takeAtLocked(key, i, ending)
		return turn, pending, false
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
	if k.byBatch || taskID == "" {
		// A batch that matched no open round has nothing left to end, and an
		// unnamed ending claims nothing for the same reason no unnamed promise
		// is ever filed: there is no round it could be speaking for. Not
		// forgotten either — a caller told to go and find a chat would announce
		// an ending it cannot attribute to any round in it.
		return roundTurn{Addr: note.addr, Verdict: roundToldAlready}, pendingEnding{}, false
	}
	if note.isTold(taskID) || note.isSpeaking(taskID) {
		return roundTurn{Addr: note.addr, Verdict: roundToldAlready}, pendingEnding{}, false
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
	note.speaking = append(note.speaking, taskID)
	note.at = s.now()
	s.ended[key] = note
	return roundTurn{Addr: note.addr, Promised: true, Verdict: roundOwesAnEnding},
		pendingEnding{live: true, key: key, taskID: taskID, ending: ending, owedAt: at, held: true},
		false
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
func (s *streamStore) forgottenLocked(key, taskID string, ending roundEnding, worthResolving bool) (roundTurn, pendingEnding, bool) {
	turn := roundTurn{Verdict: roundForgotten}
	if taskID == "" {
		return turn, pendingEnding{}, worthResolving
	}
	return turn, pendingEnding{live: true, key: key, taskID: taskID, ending: ending, owedAt: -1}, worthResolving
}

// endEnding is the other half of sayEnding: it records the delivery's account
// of itself, and it is where all three invariants are actually paid.
//
// delivered is the whole of the decision. False reaches told for nothing (I1)
// and returns a claimed promise to exactly where it sat, which is what makes a
// send refused during a reconnect window a retry rather than a loss. For a
// round this store was holding it goes one step further and leaves the run owed
// an ending it was not owed before (I3): the bubble was consumed whether or not
// the words landed, so only a later ending can put anything under it.
//
// False for a run this store held no round for writes nothing whatsoever — see
// held on pendingEnding for why that asymmetry is the point rather than an
// omission.
func (s *streamStore) endEnding(p pendingEnding, addr roundAddress, delivered bool) {
	if !p.live {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	note, ok := s.ended[p.key]
	if !ok {
		if !delivered {
			// Nothing was reserved in a note and nothing was said. There is no
			// reason to start remembering this session now.
			return
		}
		// Words reached a WeCom chat for a round this store had no note for —
		// a run that outlived a restart, or one whose bubble was never
		// painted. This is that note.
		note = endedRound{}
	}
	if at := indexOfRun(note.speaking, p.taskID); at >= 0 {
		note.speaking = withoutRun(note.speaking, at)
	}
	note.at = s.now()
	if !delivered {
		switch {
		case p.owedAt >= 0:
			note.owed = restoreRun(note.owed, p.owedAt, p.taskID)
		case p.held && note.addr.known():
			// A round this store was holding, nothing claimed, nothing landed.
			// Its bubble has just been consumed, so what the user is looking at
			// is a spinner nothing will ever seal — and this store knows the
			// chat it is in. Recording the run as owed is what makes the next
			// publisher of its ending say the words there instead of finding
			// the round gone and going quiet.
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
// That second half only holds because owed is written for a round this store
// was HOLDING and for nothing else (endEnding, the held flag). A ledger that
// filed a debt for any run whose delivery failed would put runs asked in the
// installer's browser on this list, and this function would then answer yes for
// them — an authorization question settled by a bus event the caller does not
// control. Read owed as "a round of ours is owed words", never as "the store
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

// remembered reports how many SESSIONS hold a note — one per session, however
// many of its rounds are on it. Diagnostics, and the "this process knows
// nothing at all" check at the head of the two ending subscribers; neither
// wants a round count, so none is kept.
func (s *streamStore) remembered() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ended)
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

// sweepLocked evicts rounds the server would no longer accept, and the notes
// left by rounds too old to still be running. The guard timer normally retires
// a round long before this fires; the sweep is what keeps a process whose
// timers were beaten by a clock jump from accumulating entries forever, and it
// is the only thing that bounds the notes, which no timer touches. Caller
// holds s.mu.
func (s *streamStore) sweepLocked() {
	for key, rounds := range s.sessions {
		live := rounds[:0]
		for _, r := range rounds {
			if s.expiredLocked(r.createdAt) {
				if r.guard != nil {
					r.guard.Stop()
				}
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
		}
	}
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
	say func(roundTurn) (roundAddress, error),
) (roundVerdict, error) {
	if r.streams == nil {
		_, err := say(roundTurn{Verdict: roundForgotten})
		return roundForgotten, err
	}
	return r.streams.sayEnding(sessionID, k, ending,
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
