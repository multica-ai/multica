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
// THIS STORE IS A CACHE, NOTHING MORE. It maps a chat session to the bubbles
// that can still be written to. An ending that finds a writable bubble writes
// into it; one that does not goes out through the plain aibot_send_msg path
// that outbound.go already has, addressed by the task's own delivery row. A
// bubble that cannot be written to — never painted, past its window, lost to a
// restart, or refused by the server when the closing frame goes out — is
// simply not used. Nothing here records what was said, what is owed, or who
// was told; nothing owes anyone anything once the bubble is gone.
//
// A session holds a LIST of open bubbles, not one. Messages the engine's
// debouncer collects into one agent run share a bubble; a message it gives a
// run of its own gets a bubble of its own, queued behind the run in flight —
// immediately, because a message that produces nothing on screen reads as a
// message that was lost.
//
// WHICH RUN A BUBBLE STANDS FOR IS DECIDED HERE, from two facts that arrive on
// their own and in either order:
//
//   - A message was ingested (open). That is a bubble, and nothing more — at
//     that moment nobody knows whether the debouncer will answer it with a run
//     of its own or fold it into the one already collecting.
//   - A chat run was queued for this session (bindNext, driven by the
//     task:queued subscription in typing_indicator.go). That is a run, and
//     nothing more — the event names a task and a chat session and says
//     nothing about which message produced it.
//
// The pairing rule is position, and position alone: a queued run takes the
// OLDEST round that has no run yet, and a message joins the round that is
// still collecting rather than opening a second one. That works because the
// engine serializes a session's chat tasks (ClaimAgentTask) and its debouncer
// produces at most one run per window, so per session the two sequences are in
// step. Nothing here re-measures the debounce gap from arrival times: whether
// a message opens a round or joins one is read off "is a round still waiting
// for its run", not off a clock.
//
// The rounds are kept sorted by the store's own sequence number (insertLocked),
// which for one session is the order its bubbles were painted in, and that
// order IS consumed: bindNext and takeOldestUnbound both take the oldest
// unbound round.
//
// The event can beat the bubble. The Router detaches the ingest goroutine, and
// a session's first message enqueues its task inside dispatch (router.go's
// startChat path) rather than on the debounced flush, so task:queued routinely
// arrives before anything is painted. A run with no round to take waits in the
// session's pending queue and is picked up by the next bubble.
//
// The task id, once bound, is what every later lifecycle event matches on —
// never a position. An auto-retry clone carries a fresh id and inherits its
// parent's chat_input_task_id, which is this round's own task id: see
// roundTaker, which resolves the clone through it, and retryUnbind, which puts
// the round back in line for the clone's own task:queued so the clone never
// takes a bubble some other question opened.
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
// ten-minute mark, and died at its own.
//
// So the clock is not something a bubble can be kept alive against: a run
// longer than the window loses its bubble and its answer goes out as an
// ordinary message. Carrying a round over onto a fresh stream is a separate
// layer on top of this one.
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

// streamCloseRetries is how many times a closing frame whose ack never came is
// written again, and streamCloseRetryDelay the gap between attempts. See seal.
const (
	streamCloseRetries    = 3
	streamCloseRetryDelay = 2 * time.Second
)

// openVerdict is the store's answer to "a message just arrived — does it get a
// bubble of its own".
type openVerdict int

const (
	// roundOpened — nothing was collecting, so this message starts a round.
	// The caller paints the opening frame; from here on the round owns the
	// handle it registered.
	roundOpened openVerdict = iota
	// roundJoined — a round is already on screen and still waiting for its
	// run, so the debounce window that will answer this message is the one
	// that bubble stands for. The bubble is this message's receipt too and
	// nothing is painted.
	roundJoined
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

	// Locale is the language this round's closing words are written in,
	// resolved from the asker when the bubble was opened (typing_indicator.go).
	// It travels on the handle because every closer runs later, from an event
	// that names a task and nobody else — minutes after the goroutine that
	// knew who asked is gone.
	Locale Locale

	// CreatedAt is when the stream was opened, which is what the protocol's
	// window counts from.
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

// errNothingToSay is how a delivery reports that it declined to speak: an
// empty completion with no bubble to close and no file to send, a session with
// no WeCom route at all. Nothing reached the user and nothing was owed, so it
// is not worth a warning — processEvent reads it as "skipped", which is not
// the same as "dropped".
var errNothingToSay = errors.New("wecom: nothing to say for this round")

// roundTurn is what take hands back: the round's bubble, if it still has one
// that can be written to.
type roundTurn struct {
	// Handle is the round's open bubble. HasBubble says whether there is one:
	// a round with no painted frame, or one past the protocol's window,
	// reports false and its words go out as an ordinary message — the handle
	// still names the chat that asked.
	Handle    streamHandle
	HasBubble bool
}

// roundKey picks which round an ending speaks for: the task id the session's
// task:queued bound to it. Authoritative and never inferred — a run whose id
// is not on file has no bubble here.
type roundKey struct {
	taskID string
}

func byTask(taskID string) roundKey { return roundKey{taskID: taskID} }

// roundSeq is the store's own name for a round, handed out in painting order.
// It is an internal handle, not a platform id: the only thing outside this
// file that ever holds one is the caller of open, which gives it back to drop
// when the server refuses the opening frame.
type roundSeq uint64

// roundEntry is one round's place in a session, from the moment anything is
// known about it until something takes it. Whoever takes or drops the round
// disposes of all of it in one lock.
//
// The bubble and the run arrive from different directions and in either order,
// which is why the entry exists independently of both. open brings the bubble
// (one goroutine per message, detached by the Router); the task:queued
// subscription brings the run. An entry with a task and no bubble is a run
// whose ingest goroutine has not got there yet, or one whose opening frame the
// server refused: its ending is still matched correctly, it just has nowhere
// on screen to land and falls back to a plain message.
type roundEntry struct {
	// seq is this round's place in its session's painting order, and the
	// entry's identity while nothing else names it.
	seq roundSeq

	// handle is the open bubble; painted reports whether there is one.
	handle  streamHandle
	painted bool

	// taskID is the run bound to this round, from the session's task:queued.
	// Empty means the round is waiting for one.
	taskID string

	// everBound separates the two ways a round can be waiting, which behave
	// differently and must not be confused:
	//
	//   - never bound — the debounce window that will answer it has not
	//     flushed yet. A message arriving now belongs to that same window, so
	//     it joins rather than opening a bubble of its own, and a settled
	//     flush (OnSettled) closes this one because it is a round that never
	//     became a run.
	//   - bound once, then released by retryUnbind — the platform is replacing
	//     this round's run with an auto-retry clone. The round is between runs,
	//     not collecting: a new message must NOT join it (that message is a new
	//     question with a run of its own coming), and OnSettled must not close
	//     it (its replacement is already on the way).
	everBound bool

	// createdAt bounds the entry for the sweep when there is no handle to read
	// a time off.
	createdAt time.Time
}

// maxFinishedRounds bounds the per-session memory of ended runs. Ten rounds
// back is far more than a task:queued can lag an ending by.
const maxFinishedRounds = 10

// pendingRun is a run queued for a session that had no round waiting for one:
// the ingest goroutine that will paint its bubble has not got there yet. It is
// held until a bubble appears, and swept with everything else once the
// protocol's window has passed — a run still pending that long has no bubble
// coming and must not take one that a much later question opened.
type pendingRun struct {
	taskID string
	at     time.Time
}

// streamStore maps chat_session_id to that session's rounds, oldest first.
type streamStore struct {
	mu       sync.Mutex
	sessions map[string][]*roundEntry

	// pending is each session's queue of runs that arrived before a bubble
	// existed to bind them to, oldest first. Drained one at a time, in order,
	// by the next bubble the session paints.
	pending map[string][]pendingRun

	// finished remembers, per session, the last few task ids whose round has
	// been taken, so a run that has already ended cannot bind a bubble some
	// later question opened. It is the one thing kept about a round after it is
	// gone, and it says nothing about what was said. Bounded by
	// maxFinishedRounds; a session whose rounds are all gone keeps its ring
	// until the sweep drops it along with everything else past the window.
	finished map[string]finishedRing

	// seq hands out round identities. Monotonic across the store, so it is
	// monotonic within every session, which is all anything here reads.
	seq roundSeq

	maxAge time.Duration
	now    func() time.Time

	// closeRetryDelay is the gap between two attempts at a closing frame
	// (seal). A field so a test can run the retries without the two seconds.
	closeRetryDelay time.Duration
}

// finishedRing is one session's recently ended runs, with when the last one was
// added so the sweep can retire the whole ring.
type finishedRing struct {
	tasks []string
	at    time.Time
}

func (r finishedRing) has(taskID string) bool {
	if taskID == "" {
		return false
	}
	for _, id := range r.tasks {
		if id == taskID {
			return true
		}
	}
	return false
}

func newStreamStore() *streamStore {
	return &streamStore{
		sessions:        make(map[string][]*roundEntry),
		pending:         make(map[string][]pendingRun),
		finished:        make(map[string]finishedRing),
		maxAge:          streamMaxAge,
		now:             time.Now,
		closeRetryDelay: streamCloseRetryDelay,
	}
}

// NewStreamStore is the constructor boot uses to mint the one store shared by
// the typing indicator (writer) and the chat-done subscriber (reader).
func NewStreamStore() *streamStore { return newStreamStore() }

// collectingLocked finds the round a newly ingested message belongs to: one
// that has never been bound to a run, so the debounce window that will answer
// it is still open. A round released by retryUnbind is deliberately not one —
// see roundEntry.everBound. Caller holds s.mu.
func (s *streamStore) collectingLocked(key string) *roundEntry {
	for _, r := range s.sessions[key] {
		if r.taskID == "" && !r.everBound {
			return r
		}
	}
	return nil
}

// unboundLocked finds the oldest round with no run bound to it — the one a
// newly queued run belongs to. Caller holds s.mu.
func (s *streamStore) unboundLocked(key string) *roundEntry {
	for _, r := range s.sessions[key] {
		if r.taskID == "" {
			return r
		}
	}
	return nil
}

// boundLocked finds the round a task id is bound to, or nil. Caller holds s.mu.
func (s *streamStore) boundLocked(key, taskID string) *roundEntry {
	if taskID == "" {
		return nil
	}
	for _, r := range s.sessions[key] {
		if r.taskID == taskID {
			return r
		}
	}
	return nil
}

// insertLocked files a new round at the end of its session's list. The
// sequence numbers are handed out under this same lock, so the list is always
// in painting order even when the Router's detached ingest goroutines race.
// Caller holds s.mu.
func (s *streamStore) insertLocked(key string, e *roundEntry) *roundEntry {
	s.sessions[key] = append(s.sessions[key], e)
	return e
}

// finishedLocked reports whether a run has already had its round taken. Caller
// holds s.mu.
func (s *streamStore) finishedLocked(key, taskID string) bool {
	return s.finished[key].has(taskID)
}

// retireLocked records that a run is over, keeping the ring bounded. Caller
// holds s.mu.
func (s *streamStore) retireLocked(key, taskID string) {
	if taskID == "" {
		return
	}
	ring := s.finished[key]
	if !ring.has(taskID) {
		ring.tasks = append(ring.tasks, taskID)
		if len(ring.tasks) > maxFinishedRounds {
			ring.tasks = ring.tasks[len(ring.tasks)-maxFinishedRounds:]
		}
	}
	ring.at = s.now()
	s.finished[key] = ring
}

// dropPendingLocked removes a run from the session's pending queue. Caller
// holds s.mu.
func (s *streamStore) dropPendingLocked(key, taskID string) {
	queue := s.pending[key]
	for i, p := range queue {
		if p.taskID != taskID {
			continue
		}
		queue = append(queue[:i], queue[i+1:]...)
		if len(queue) == 0 {
			delete(s.pending, key)
		} else {
			s.pending[key] = queue
		}
		return
	}
}

// takePendingLocked pops the oldest run waiting for a bubble, or "". Caller
// holds s.mu.
func (s *streamStore) takePendingLocked(key string) string {
	queue := s.pending[key]
	if len(queue) == 0 {
		return ""
	}
	taskID := queue[0].taskID
	if len(queue) == 1 {
		delete(s.pending, key)
	} else {
		s.pending[key] = queue[1:]
	}
	return taskID
}

// bindLocked attaches a run to a round. Caller holds s.mu.
func (e *roundEntry) bindLocked(taskID string) {
	e.taskID = taskID
	e.everBound = true
}

// open registers a message's bubble and says whether this message is the one
// that paints it. A message that arrives while a round is still collecting —
// painted, and with no run bound to it yet — joins that round, because the
// debounce window that will answer it is the one that bubble already stands
// for; a second bubble there is one nobody ever closes. Otherwise it opens a
// round of its own, immediately, because a wait with nothing on screen reads
// as a message that was lost.
//
// The returned sequence number is the caller's handle on the round for the one
// case where it has to take it back: an opening frame the server refuses
// outright (drop).
func (s *streamStore) open(sessionID pgtype.UUID, h streamHandle) (roundSeq, openVerdict) {
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()

	if h.CreatedAt.IsZero() {
		h.CreatedAt = s.now()
	}
	if e := s.collectingLocked(key); e != nil {
		return e.seq, roundJoined
	}

	s.seq++
	e := &roundEntry{seq: s.seq, handle: h, painted: true, createdAt: h.CreatedAt}
	e.handle.QueuedBehind = queuedBehind(s.sessions[key])
	s.insertLocked(key, e)
	// A run queued before anything was on screen has been waiting for exactly
	// this. Pairing them here rather than leaving the run for the NEXT bubble
	// is what keeps the two sequences in step.
	if taskID := s.takePendingLocked(key); taskID != "" {
		e.bindLocked(taskID)
	}
	return e.seq, roundOpened
}

// queuedBehind reports whether a round opening now would be waiting on one
// already on file. Its own empty answer then means "the reply ahead of it
// covered this", which is worth saying differently from plain silence. Decided
// once, when the round opens, and never revised.
func queuedBehind(rounds []*roundEntry) bool { return len(rounds) > 0 }

// bindNext records that a run was queued for this session and hands it the
// round it belongs to: the oldest one still waiting for a run. From here on
// every task lifecycle event finds its bubble by task id.
//
// A run with no round waiting goes on the session's pending queue rather than
// being dropped, because the Router detaches the ingest goroutine and a
// session's first message enqueues its task inside dispatch — so the event
// routinely beats the bubble it belongs to.
func (s *streamStore) bindNext(sessionID pgtype.UUID, taskID string) {
	if taskID == "" {
		return
	}
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()

	// A run whose ending has already been handled must never take a bubble: it
	// would be a bubble with no ending left to close it.
	if s.finishedLocked(key, taskID) {
		return
	}
	if s.boundLocked(key, taskID) != nil {
		return // already on file; a republished queued event changes nothing
	}
	if e := s.unboundLocked(key); e != nil {
		e.bindLocked(taskID)
		return
	}
	for _, p := range s.pending[key] {
		if p.taskID == taskID {
			return
		}
	}
	s.pending[key] = append(s.pending[key], pendingRun{taskID: taskID, at: s.now()})
}

// retryUnbind puts a round back in line for the run that will replace it, and
// reports whether it found one.
//
// The platform answers a retryable failure by creating an auto-retry clone: a
// NEW task row, with a new id, which publishes a task:queued of its own. The
// clone's id is the only name its ending will ever carry, and the round it
// belongs to is this one — so the round gives up the dead attempt's id and
// goes back to waiting. Being the oldest round with no run, it is what the
// clone's task:queued then takes.
//
// This works in either publish order, which matters because FailTask emits the
// clone's task:queued BEFORE the parent's task:failed (service/task.go, the
// retried block above broadcastTaskFailedEvent) while a backoff child is
// queued minutes later by the deferred sweeper. Early, the clone is already
// waiting in the pending queue and is taken here; late, it finds this round
// still unbound and takes it then.
//
// The bubble is deliberately untouched: the user is watching a spinner for a
// question whose answer is still coming.
func (s *streamStore) retryUnbind(sessionID pgtype.UUID, taskID string) bool {
	if taskID == "" {
		return false
	}
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.boundLocked(key, taskID)
	if e == nil {
		return false
	}
	e.taskID = ""
	if clone := s.takePendingLocked(key); clone != "" {
		e.bindLocked(clone)
	}
	return true
}

// takeOldestUnbound hands back the oldest round that never became a run — the
// bubble a settled flush opened and nothing will ever answer. A round released
// by retryUnbind is not one of those: its replacement is on the way.
func (s *streamStore) takeOldestUnbound(sessionID pgtype.UUID) (roundTurn, bool) {
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	e := s.collectingLocked(key)
	if e == nil {
		return roundTurn{}, false
	}
	for i, r := range s.sessions[key] {
		if r == e {
			return s.takeAtLocked(key, i), true
		}
	}
	return roundTurn{}, false
}

// forget records that a run has ended without taking a round for it: it drops
// the run from the pending queue and retires its id.
//
// It is what stops an ending that arrived before the bubble — a cancel, or a
// failure this process is not the one to announce — from leaving a run on the
// pending queue that the NEXT question's bubble would then bind itself to,
// spinning with nothing left that could close it.
func (s *streamStore) forget(sessionID pgtype.UUID, taskID string) {
	if taskID == "" {
		return
	}
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions[key]) == 0 && len(s.pending[key]) == 0 {
		// Nothing of this session's is on file, so there is nothing to forget
		// and no reason to start remembering. task:failed fires for every run
		// in the deployment; a ring per stranger's session would be a leak.
		return
	}
	s.dropPendingLocked(key, taskID)
	s.retireLocked(key, taskID)
}

// indexLocked finds the round an ending speaks for, or -1. Matching is by the
// name the caller carried in and there is no positional fallback: a run whose
// id is not on file has no bubble here, and taking somebody else's would seal
// the wrong question with this answer. Caller holds s.mu.
func (s *streamStore) indexLocked(key string, k roundKey) int {
	if k.taskID == "" {
		return -1
	}
	for i, r := range s.sessions[key] {
		if r.taskID == k.taskID {
			return i
		}
	}
	return -1
}

// takeAtLocked removes rounds[i] and hands back what is left of it: the
// bubble, if it can still be written to.
//
// The entry goes unconditionally. Removing it under the lock that found it is
// the mutual exclusion between two closers racing for one run — whichever
// gets here first is the only one that ever sees the handle, so one run
// produces one closing frame. A round with no bubble reports absent, and so
// does a handle past maxAge: the server would refuse the frame and a caller
// that believed it had a bubble would leave the user with nothing.
//
// The run goes on the session's finished ring so a republished task:queued for
// a run that has already ended cannot bind a bubble some later question
// opened. Caller holds s.mu.
func (s *streamStore) takeAtLocked(key string, i int) roundTurn {
	rounds := s.sessions[key]
	entry := rounds[i]
	rounds = append(rounds[:i], rounds[i+1:]...)
	if len(rounds) == 0 {
		delete(s.sessions, key)
	} else {
		s.sessions[key] = rounds
	}
	s.retireLocked(key, entry.taskID)

	turn := roundTurn{}
	if entry.painted && !s.expiredLocked(entry.handle.CreatedAt) {
		turn.Handle, turn.HasBubble = entry.handle, true
	}
	return turn
}

// take removes the round k names and hands back its bubble. The second result
// says whether a round was on file at all: false is a run this process holds
// nothing for — a turn from before a restart, one that never opened a bubble,
// or one already taken by another closer.
//
// resolve is the auto-retry lookup, consulted at most once and only when the
// id on the event matches nothing in a session that still has rounds open — a
// clone carries a fresh id and inherits the round's own on chat_input_task_id,
// so it is filed under its parent's name. It runs OUTSIDE the lock, because it
// costs a database row.
func (s *streamStore) take(ctx context.Context, sessionID pgtype.UUID, k roundKey, resolve func(context.Context, string) string) (roundTurn, bool) {
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	s.sweepLocked()
	// Whatever else happens, this run is over: it must not be left waiting for
	// a bubble it would only strand.
	s.dropPendingLocked(key, k.taskID)
	if i := s.indexLocked(key, k); i >= 0 {
		turn := s.takeAtLocked(key, i)
		s.mu.Unlock()
		return turn, true
	}
	if len(s.sessions[key]) > 0 || len(s.pending[key]) > 0 {
		s.retireLocked(key, k.taskID)
	}
	worthResolving := k.taskID != "" && len(s.sessions[key]) > 0
	s.mu.Unlock()

	if !worthResolving || resolve == nil {
		return roundTurn{}, false
	}
	root := resolve(ctx, k.taskID)
	if root == "" || root == k.taskID {
		return roundTurn{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.indexLocked(key, byTask(root)); i >= 0 {
		return s.takeAtLocked(key, i), true
	}
	return roundTurn{}, false
}

// has reports whether a session holds a round bound to this run. A round is
// opened by a message this adapter ingested and bound to this run by the
// session's own task:queued, so an entry here is local proof the question was
// asked in the room — the one case the failure notice's origin gate can decide without a
// database (failureBelongsOnWecom).
func (s *streamStore) has(sessionID pgtype.UUID, taskID string) bool {
	if taskID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.sessions[util.UUIDToString(sessionID)] {
		if r.taskID == taskID {
			return true
		}
	}
	return false
}

// holding reports whether this store has anything on file anywhere — a round,
// painted or not, or a run still waiting for its bubble. It is the "nothing
// here to close" test at the head of the two ending subscribers.
//
// Unpainted rounds and pending runs both count, and that is the point. depth()
// screens on painted because it answers "how many bubbles are on screen"; a run
// whose bubble is still in flight is exactly the one whose ending must not be
// dropped — forgetting it is what keeps the bubble that lands a moment later
// from binding a run that has already ended, and skipping it leaves a spinner
// nothing will ever close.
func (s *streamStore) holding() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rounds := range s.sessions {
		if len(rounds) > 0 {
			return true
		}
	}
	return len(s.pending) > 0
}

// drop forgets a round without sending anything — used when the opening frame
// was refused and the bubble the handle describes never existed. seq is what
// open handed back.
func (s *streamStore) drop(sessionID pgtype.UUID, seq roundSeq) {
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	rounds := s.sessions[key]
	for i, r := range rounds {
		if r.seq != seq {
			continue
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

// depth reports how many bubbles are open across all sessions. Diagnostics
// and tests.
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

// sweepLocked evicts rounds the server would no longer accept, runs that have
// been waiting for a bubble longer than one could still be coming, and the
// finished rings of sessions that have been quiet for a whole window. An
// ending normally takes a round long before this fires; the sweep is what keeps
// a round whose run produced no ending at all — and a run whose ingest
// goroutine never arrived — from accumulating forever. Caller holds s.mu.
func (s *streamStore) sweepLocked() {
	for key, rounds := range s.sessions {
		live := rounds[:0]
		for _, r := range rounds {
			if s.expiredLocked(r.createdAt) {
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
	for key, queue := range s.pending {
		live := queue[:0]
		for _, p := range queue {
			if s.expiredLocked(p.at) {
				continue
			}
			live = append(live, p)
		}
		if len(live) == 0 {
			delete(s.pending, key)
		} else {
			s.pending[key] = live
		}
	}
	for key, ring := range s.finished {
		if s.expiredLocked(ring.at) {
			delete(s.finished, key)
		}
	}
}

// seal writes a bubble's closing frame and is the one place the retry policy
// for closing frames lives. Every closer goes through it: the answer, the
// failure and cancellation notices, and the settled flush.
//
// The policy, measured against the live bot (STRATEGY §6.5):
//
//   - An ack that never comes (errStreamAckTimeout) says nothing about whether
//     the frame landed, and a frame written right before a disconnect does
//     NOT land. Re-sending the identical closing frame is accepted with errcode
//     0 whether or not the first one did, so the frame is written again, up to
//     streamCloseRetries more times, streamCloseRetryDelay apart. The registry
//     resolves the sender per frame, so a reconnect between two attempts is
//     covered. THE CHOSEN SIDE: a retry after an ack that was merely lost may
//     put in front of the user a closing frame they have already seen — the
//     same content on the same stream, which the client renders in place. That
//     is preferred over the alternative, an answer written before a drop that
//     never arrived and was never sent again.
//   - A verdict from the server (streamUnusable: 846605 / 846608) ends it: this
//     stream will never take a frame, and the caller falls back to a plain
//     message.
//   - errStreamBusy and errStreamSuperseded are not retried. Busy cannot
//     happen to a closing frame (they queue, ws_sender.go); superseded means
//     another closer sealed this stream first, and the answer is theirs.
//   - The retries stop when ctx ends or the stream's own window is gone: a
//     frame the server is about to refuse on age is not worth the wait.
//
// The ending is counted once, whatever the number of attempts: a bubble that
// took the frame on the second try ended in words all the same.
func (s *streamStore) seal(ctx context.Context, senders *sendersRegistry, h streamHandle, text string) error {
	var err error
	for attempt := 0; ; attempt++ {
		err = senders.stream(ctx, h, text, true)
		if !errors.Is(err, errStreamAckTimeout) || attempt >= streamCloseRetries {
			break
		}
		if s.expiredLocked(h.CreatedAt) {
			break
		}
		if s.closeRetryDelay > 0 {
			timer := time.NewTimer(s.closeRetryDelay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				err = ctx.Err()
			}
			if ctx.Err() != nil {
				break
			}
		}
	}
	senders.recordEnding(err)
	return err
}

// roundTaker matches a task lifecycle event to the round it belongs to. Both
// halves of the store's identity live behind it: the binding task:queued
// filed, and the one column that resolves an auto-retry clone back to it.
type roundTaker struct {
	streams *streamStore
	tasks   taskLookup
	log     *slog.Logger
}

// take is roundTaker's one job: take on the store, with the auto-retry lookup
// supplied.
//
// The id on the event is tried first, because that is the id bindNext filed.
// retryUnbind normally hands the round straight to the clone's own task:queued,
// so the clone's ending matches on that first try; this column is the belt to
// that braces. It reads chat_input_task_id, which the clone inherits from its
// parent and which is the round's own task id (EnqueueChatTask stamps
// chat_input_task_id = id on the turn it creates) — so a clone whose
// task:queued this process never saw, one queued before a restart, still finds
// its round rather than falling back to whichever is at the head.
//
// The lookup costs one read, and the store only asks for it on a miss in a
// session that still holds a round. Without a task lookup configured the miss
// is simply a miss.
//
// With no store at all the in-place reply is disabled: nothing is ever found.
func (r roundTaker) take(ctx context.Context, sessionID pgtype.UUID, k roundKey) (roundTurn, bool) {
	if r.streams == nil {
		return roundTurn{}, false
	}
	return r.streams.take(ctx, sessionID, k, r.rootTaskID)
}

// rootTaskID reads the input batch a task belongs to — its own id for a first
// attempt, the parent's for an auto-retry clone. Empty when there is nothing
// to gain from asking.
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
