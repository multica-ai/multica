package wecom

// Stream handles map each chat session's pending answers to writable WeCom bubbles.
// They are an in-memory cache: missing, expired, or rejected handles fall back to
// plain messages addressed by the task's delivery row.
//
// Messages join the collecting round; each queued run takes the oldest unbound
// round. Sequence order, not arrival-time estimates, pairs the two. A task event
// may precede its bubble, so unmatched runs wait in the session's pending queue.
// After binding, lifecycle events match task IDs. Retries resolve the original
// round through chat_input_task_id rather than taking another question's bubble.
//
// Handles include the callback req_id needed by later stream frames. Keep this
// store outside the connection loop: handles survive reconnects but are lost on
// process restart. The bot lease limits writes to one replica, and streamMaxAge
// bounds handle lifetime. Inbound dedup handles replay before it reaches this store.

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

// streamGuardAfter is when a live bubble is rotated onto a fresh stream rather
// than let run into streamMaxAge. A minute of headroom covers a slow frame:
// the old stream still has to take the closing frame that hands over to the
// new one. It stays clear of the measured ceiling's lower bound, not just of
// streamMaxAge.
// maxRotations bounds how many fresh bubbles one run may be handed. Twenty-
// four is four hours of a run that keeps stepping; past it the guard leaves
// the bubble to the server's window and the answer goes plain.
const maxRotations = 24

const streamGuardAfter = 9 * time.Minute

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

	// Locale is the language this round's closing words are written in,
	// resolved from the asker when the bubble was opened (typing_indicator.go).
	// It travels on the handle because every closer runs later, from an event
	// that names a task and nobody else — minutes after the goroutine that
	// knew who asked is gone.
	Locale Locale

	// Level is how much of the run this bubble may show while it is still
	// going (progress_render.go). It is settled when the bubble is opened,
	// while who asked and where they asked is still known, and read again on
	// every refresh — the events that drive those refreshes name a task and
	// nothing about a person.
	//
	// Settled PER ROUND, never carried over from the last one: the binding it
	// is decided from can be revoked, or re-pointed at a different person,
	// between two questions in the same chat, and the round after that must
	// not still be showing file paths on the strength of the round before it.
	Level progressLevel

	// Unusable says the server has disowned this stream (846605 / 846608): the
	// bubble it painted is on the user's screen and no frame will ever touch
	// it again, so what is left of the handle is the addressing. A caller that
	// gets one writes its words as a plain message instead of a frame. Set by
	// the store on the way out of take.
	Unusable bool

	// CreatedAt is when the CURRENT stream was opened, which is what the
	// protocol's window counts from. Rotating a round onto a fresh stream
	// resets it.
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

	// retryOf is the id of the attempt a released round is waiting to be
	// replaced by — the parent whose clone will answer this round's question.
	// Set by retryUnbind, cleared the moment a run is bound.
	//
	// It exists because NOTHING ELSE CAN TELL THE CLONE APART from a new
	// question's run at task:queued: both are a new task row with a new id on
	// the same session, and the adapter may not read the database on the bus.
	// The two orderings are symmetric — the clone may arrive before or after a
	// new question's run — so no arrival-order rule resolves them, and the
	// round has to carry the name of what it is waiting for until an ending
	// can resolve the lineage.
	retryOf string

	// guard rotates the bubble onto a fresh stream before the protocol's
	// window runs out on the current one.
	guard *time.Timer

	// steps counts the run's signs of life — every progress report for it that
	// reaches this replica, from its own bus or relayed from another, whether
	// or not a line was painted — and stepsAtOpen is the count when the
	// current stream opened. The guard
	// rotates a bubble only for a run that has stepped since: a run that ended
	// without an event (an agent archived on main publishes no task:cancelled)
	// would otherwise be handed a fresh bubble every nine minutes for as long
	// as the process lives. A quiet run keeps its stream until the server ends
	// it at the window, and its answer, if one still comes, goes out as a
	// plain message. Counts rather than clocks, so a step and an open in the
	// same instant still tell apart.
	steps       int
	stepsAtOpen int

	// rotations counts hand-overs, bounded by maxRotations as a backstop for a
	// run that keeps stepping and never ends.
	rotations int

	// feed is the bubble's scrolling list of steps, created on the first one
	// that reaches it (progress_render.go). It lives here rather than beside
	// the store's maps so it dies exactly when the round does — a list of a
	// finished run's tool calls has nothing left to be painted into. A
	// rotation starts a fresh one: the new stream is a new bubble.
	feed *progressFeed

	// folded is how far each run's transcript has been read into the feed,
	// by the run's own seq (task_message.seq), and it is the dedupe for every
	// progress report: a report at or below it is one already shown, however
	// many copies of it arrive and from whichever replica. Per run, because a
	// round can see two — an auto-retry clone starts its own seq at one.
	// It survives a rotation, unlike the feed: the new bubble starts empty,
	// not by replaying the transcript from the beginning.
	folded map[string]int32

	// started says the "starting the agent" line has been put into this
	// round's feed, so a second copy of the milestone is not a second line.
	started bool

	// unusable is the server's verdict that this stream takes no further
	// frame, kept rather than acted on by forgetting the round. The bubble is
	// over either way; the round is not, and the handle is the only address
	// its ending has.
	unusable bool

	// createdAt bounds the entry for the sweep when there is no handle to read
	// a time off.
	createdAt time.Time
}

// maxFinishedRounds bounds the per-session memory of ended runs. Ten rounds
// back is far more than a task:queued can lag an ending by.
const maxFinishedRounds = 10

// pendingRun is a run queued for a session that had no round waiting for one:
// the ingest goroutine that will paint its bubble has not got there yet. It is
// held until a bubble appears, or until pendingMaxAge says none is coming.
type pendingRun struct {
	taskID string
	at     time.Time
}

// pendingMaxAge is how long a queued run may wait for the bubble it was meant
// to bind to.
//
// The clock is the ingest goroutine's, not the protocol's. That goroutine
// resolves a sender, writes one opening frame and returns — bounded by the
// router's own reply timeout and one ack wait, a few seconds — so a run still
// pending after that is one whose bubble is never coming: the paint was
// refused, the envelope was unreadable, the round was dropped. What it can
// still do is pair with the NEXT question's bubble, which belongs to somebody
// else and whose answer would then find no round of its own.
//
// streamMaxAge was the wrong bound for it: ten minutes is how long the SERVER
// keeps a stream writable, which says nothing about how long an ingest takes.
const pendingMaxAge = 30 * time.Second

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

// unboundLocked finds the oldest round with no run bound to it, INCLUDING one
// released by retryUnbind. It is bindNext's second choice, taken only when no
// never-bound round is waiting — see the note there for why the order is what
// keeps two turns from being cross-wired.
// Caller holds s.mu.
func (s *streamStore) unboundLocked(key string) *roundEntry {
	for _, r := range s.sessions[key] {
		if r.taskID == "" && r.retryOf == "" {
			return r
		}
	}
	return nil
}

// awaitingRetryLocked answers two separate questions, and the returns are
// deliberately not interchangeable:
//
//	at   — the index of the round waiting for THIS root, or -1. Only >= 0 is a
//	       match; callers index on this and nothing else.
//	held — whether the session holds any round waiting for a retry at all.
//	       This is the cheap in-memory check that decides whether an ending
//	       pays for the lineage read.
//
// Caller holds s.mu.
func (s *streamStore) awaitingRetryLocked(key, root string) (at int, held bool) {
	at = -1
	for i, r := range s.sessions[key] {
		if r.retryOf == "" {
			continue
		}
		held = true
		if root != "" && r.retryOf == root {
			at = i
		}
	}
	return at, held
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

// seqLocked finds the round with this sequence number, or nil. Caller holds
// s.mu.
func (s *streamStore) seqLocked(key string, seq roundSeq) *roundEntry {
	for _, r := range s.sessions[key] {
		if r.seq == seq {
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
	// Drop what has waited past its own clock before taking: an abandoned run
	// at the head would otherwise hand itself to a bubble opened much later.
	for len(queue) > 0 && s.now().Sub(queue[0].at) > pendingMaxAge {
		queue = queue[1:]
	}
	if len(queue) == 0 {
		delete(s.pending, key)
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
	s.insertLocked(key, e)
	// A run queued before anything was on screen has been waiting for exactly
	// this. Pairing them here rather than leaving the run for the NEXT bubble
	// is what keeps the two sequences in step.
	if taskID := s.takePendingLocked(key); taskID != "" {
		e.bindLocked(taskID)
	}
	return e.seq, roundOpened
}

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
	// A NEVER-BOUND ROUND FIRST, A RELEASED ONE ONLY IF NONE IS WAITING. The
	// two lookups have to agree about a released round or two turns end up
	// cross-wired: collectingLocked already refuses one (a new message must
	// not join a round whose replacement run is on the way), so a new
	// question opens a round of its own — and if this bind then handed that
	// question's run the RELEASED round, each turn would seal the other's
	// bubble. Driven in the real backoff order, that is two people's answers
	// swapped, silently.
	//
	// Preferring the never-bound round makes the two agree. What it costs is
	// the narrow case where the clone's task:queued arrives while a newer
	// question's round is painted but its own run has not been queued yet:
	// the clone takes the newer round and the released one is left without a
	// run. That round then spins until streamMaxAge, its answer arrives as a
	// plain message, and recordOpened makes it countable — a degraded turn,
	// not a wrong one. Between a stranded bubble and an answer delivered to
	// the wrong question, only one of the two is recoverable by asking again.
	e := s.collectingLocked(key)
	if e == nil {
		e = s.unboundLocked(key)
	}
	if e != nil {
		e.retryOf = ""
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
	// The clone is usually already queued — service publishes its task:queued
	// before the parent's task:failed — so the ordinary case hands the round
	// straight over and nothing is ever left waiting.
	if clone := s.takePendingLocked(key); clone != "" {
		e.taskID = ""
		e.bindLocked(clone)
		return true
	}
	// NO CLONE YET. The round goes back to waiting, but NOT into the pool the
	// next task:queued draws from: a clone's queued event is byte-identical to
	// a new question's, so the pool would hand this round to whichever arrived
	// first, and driven in the real order that is two turns cross-wired, each
	// sealing the other's bubble.
	//
	// It records the name of what it is waiting for instead. A clone inherits
	// its parent's chat_input_task_id, and EnqueueChatTask stamps
	// chat_input_task_id = id on the turn it creates, so this id IS the root
	// the clone resolves to — which makes the clone's ENDING able to name this
	// round with an authoritative id where its queued event could not.
	e.taskID = ""
	e.retryOf = taskID
	return true
}

// releaseRound hands a bubble back when the run bound to it turns out not to be
// one this adapter will ever close — a question typed in Multica that took the
// room's round off task:queued.
//
// Unlike retryUnbind there is no replacement coming, so the round genuinely
// goes back to waiting: the room's own run, which found the round taken and
// went to the pending queue, is bound here if it is waiting, and otherwise the
// next task:queued for this session takes it.
//
// everBound stays set. The round is between runs rather than collecting, so a
// new message must not join it — see roundEntry.everBound.
func (s *streamStore) releaseRound(sessionID pgtype.UUID, taskID string) bool {
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
	if next := s.takePendingLocked(key); next != "" {
		e.bindLocked(next)
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

// arm attaches the expiry guard to a round, replacing any earlier one. A
// round that ended between the open and this call has already left the list,
// so there is nothing to guard and the timer is stopped instead of leaked.
func (s *streamStore) arm(sessionID pgtype.UUID, seq roundSeq, t *time.Timer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.seqLocked(util.UUIDToString(sessionID), seq); e != nil {
		if e.guard != nil && e.guard != t {
			e.guard.Stop()
		}
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
	if entry.guard != nil {
		entry.guard.Stop()
	}
	s.retireLocked(key, entry.taskID)

	turn := roundTurn{}
	if entry.painted && !s.expiredLocked(entry.handle.CreatedAt) {
		turn.Handle, turn.HasBubble = entry.handle, true
		// The server's verdict travels with the handle: a stream it has
		// disowned still names the chat, and that addressing is all a caller
		// can use. Writing another frame to it would be one more refusal
		// charged against the whole bot's rate limit.
		turn.Handle.Unusable = entry.unusable
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
	_, awaitingRetry := s.awaitingRetryLocked(key, "")
	s.mu.Unlock()

	// A ROUND WAITING FOR A NAMED ATTEMPT IS RESOLVED BY LINEAGE, NOT BY
	// WHATEVER task:queued GUESSED. The clone's queued event cannot name the
	// round it belongs to, so bindNext may have bound this run to a newer
	// question's round; its chat_input_task_id can name it, and that is
	// authoritative. Read first rather than on a miss, because on this path the
	// first lookup can succeed with the WRONG round.
	//
	// The read is paid only while a retry is outstanding — awaitingRetry is an
	// in-memory check over one session's rounds — so an ordinary ending still
	// costs what it always did.
	if awaitingRetry && k.taskID != "" && resolve != nil {
		if root := resolve(ctx, k.taskID); root != "" {
			s.mu.Lock()
			if i, _ := s.awaitingRetryLocked(key, root); i >= 0 {
				turn := s.takeAtLocked(key, i)
				s.mu.Unlock()
				return turn, true
			}
			s.mu.Unlock()
		}
	}

	s.mu.Lock()
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

// progressSlot is what a progress report needs from the round it belongs to.
type progressSlot struct {
	handle streamHandle
	// feed is nil unless writable.
	feed *progressFeed
	// folded is how far the reporting run's transcript is already in the
	// feed; started, whether the starting line is.
	folded  int32
	started bool
	// writable: painted, not disowned, inside the window. A round that is
	// here but not writable still counts the report (see steps) and still
	// makes this replica the one the report was for.
	writable bool
}

// progressFor finds the round a progress report belongs to and counts the
// report as a sign of life on it. roundKey is the run the session's
// task:queued bound to the round (an auto-retry clone's parent); taskID is
// the run that reported, whose transcript seq the report carries. seq is zero
// for the milestone, which has no seq.
//
// The second result says whether the round is on this replica at all, and
// that is a different question from whether it can take a frame: a round
// that is here is this replica's to show or not show, and the report must not
// be passed on to anyone else.
//
// A report at or below what is already folded is a copy of one already
// counted — a relayed duplicate, a replay — and does not count again.
//
// The feed is created lazily, on the first report that reaches a writable
// round, and lives on the entry so it dies exactly when the round does — a
// list of a finished run's tool calls has nothing left to be painted into.
func (s *streamStore) progressFor(sessionID pgtype.UUID, roundKey, taskID string, seq int32) (progressSlot, bool) {
	if roundKey == "" {
		return progressSlot{}, false
	}
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.sessions[key] {
		if r.taskID != roundKey {
			continue
		}
		slot := progressSlot{folded: r.folded[taskID], started: r.started}
		if seq == 0 || seq > slot.folded {
			r.steps++
		}
		// Three ways a round on the list still has nowhere to put a step. No
		// bubble: the run was bound before the ingest goroutine painted one,
		// or the opening frame was refused. Past the window: the server
		// refuses the frame and the ending will fall back to a plain message.
		// Disowned: another connection owns this conversation now, and every
		// frame from here is a refusal counted against the whole bot's rate
		// limit.
		if !r.painted || r.unusable || s.expiredLocked(r.handle.CreatedAt) {
			return slot, true
		}
		if r.feed == nil {
			r.feed = newProgressFeed(s.now)
		}
		slot.handle, slot.feed, slot.writable = r.handle, r.feed, true
		return slot, true
	}
	return progressSlot{}, false
}

// markFolded records that taskID's transcript is in the round's feed up to
// seq, and, with started, that the starting line is too. Never backwards: a
// slower read that finishes second cannot unfold what a faster one recorded.
// A round that has ended since is left alone; there is nothing to record on.
func (s *streamStore) markFolded(sessionID pgtype.UUID, roundKey, taskID string, seq int32, started bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.sessions[util.UUIDToString(sessionID)] {
		if r.taskID != roundKey {
			continue
		}
		if seq > r.folded[taskID] {
			if r.folded == nil {
				r.folded = map[string]int32{}
			}
			r.folded[taskID] = seq
		}
		if started {
			r.started = true
		}
		return
	}
}

// markUnusable records the server's verdict that a round's stream will take no
// further frame. It reports whether this call is the one that learned it, so
// the reader is told once rather than on every refusal that follows.
//
// The round is kept rather than forgotten: the bubble is over, the round is
// not, and the handle is the only address its ending has.
func (s *streamStore) markUnusable(sessionID pgtype.UUID, streamID string) bool {
	if streamID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.sessions[util.UUIDToString(sessionID)] {
		if r.painted && r.handle.StreamID == streamID {
			if r.unusable {
				return false
			}
			r.unusable = true
			return true
		}
	}
	return false
}

// rotate moves a live round onto a fresh stream id, and hands back the handle
// it had (old) and the one it has now (next). false when there is nothing to
// rotate: the round is gone, was never painted, has been disowned, or is
// already past the window — in which case the caller has nothing to seal and
// nothing to reopen.
//
// THE ROUND IS NAMED BY ITS SEQUENCE NUMBER, not by the run bound to it, and
// that is what makes a rotation safe under an identity that arrives on the
// bus. The task id on an entry moves — it is empty until task:queued binds one
// and empty again while an auto-retry clone is replacing it (retryUnbind) — so
// a guard that named its round by task would rotate the wrong bubble, or none,
// depending on when it happened to fire. The sequence number is handed out
// once at open and nothing revises it.
//
// Nothing else about the entry moves either: the round keeps its task, its
// everBound, and its place in the list. So a rotated round is still bound,
// which is what keeps bindNext (which takes the oldest UNBOUND round) from
// handing a later question's run to a bubble that is mid-answer, and its own
// answer still finds it by task id and is written to the stream it holds now.
//
// The swap happens BEFORE either frame is written, under the lock, so an
// answer that takes the round while the hand-over is on the wire gets the new
// stream id. Its closing frame then queues behind the seal (one frame in
// flight per req_id, ws_sender.go) and lands as the new bubble's only frame,
// which WeCom accepts as creating the message — measured 2026-08-09, see
// senders_registry.go. Had the answer been handed the OLD id it would have
// written a second closing frame onto a stream the hand-over had just sealed.
//
// The window restarts: CreatedAt is stamped now, so expiry and the next guard
// count from the new stream's opening frame, which is what the server counts
// from. The new stream is a new bubble and carries none of the old one's
// step lines, but it does carry the reasoning buffer (progressFeed.continued):
// redaction runs over that joined buffer, and a label at the end of the old
// bubble's reasoning (`password=`) is what lets the value at the start of the
// new one be recognised. The old bubble is sealed with the hand-over line, so
// the reasoning it showed is not on screen twice.
func (s *streamStore) rotate(sessionID pgtype.UUID, seq roundSeq, streamID string) (old, next streamHandle, ok bool) {
	if streamID == "" {
		return streamHandle{}, streamHandle{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.seqLocked(util.UUIDToString(sessionID), seq)
	if e == nil || !e.painted || e.unusable || s.expiredLocked(e.handle.CreatedAt) {
		return streamHandle{}, streamHandle{}, false
	}
	// Only a run that has stepped since this stream opened is alive enough
	// to deserve the next one; see steps. The cap is the backstop.
	if e.steps <= e.stepsAtOpen || e.rotations >= maxRotations {
		return streamHandle{}, streamHandle{}, false
	}
	old = e.handle
	next = old
	next.StreamID = streamID
	next.CreatedAt = s.now()
	e.handle = next
	e.createdAt = next.CreatedAt
	e.feed = e.feed.continued()
	e.rotations++
	e.stepsAtOpen = e.steps
	return old, next, true
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
	// A bubble the server has disowned mid-run is not written to again: the
	// typing indicator was told this stream takes no frame and has already
	// said so to the reader, and every further attempt is a refusal charged
	// against the whole bot's rate limit. Every closer comes through here, so
	// each of them takes its own fallback — the answer and a failure notice go
	// out as the new message the reader was promised.
	if h.Unusable {
		senders.recordEnding(errStreamDisowned)
		return errStreamDisowned
	}
	var err error
	for attempt := 0; ; attempt++ {
		// The first attempt is an ordinary frame and waits its turn like one.
		// Every attempt after it is the SAME frame written again, which the
		// gate lets through: blocking it would leave the first one unanswered
		// and send the answer a second time by the plain route.
		if attempt == 0 {
			err = senders.stream(ctx, h, text, true)
		} else {
			retryErr := senders.streamRewrite(ctx, h, text, true)
			if provablyNotSent(retryErr) {
				// A retry that wrote nothing says nothing about the frame before
				// it, which reached the socket and was never answered. Keep that
				// reading: taking the retry's would call the bubble provably
				// empty and send its words again as a message.
				break
			}
			err = retryErr
		}
		if !errors.Is(err, errStreamAckTimeout) || attempt >= streamCloseRetries {
			break
		}
		if s.expiredLocked(h.CreatedAt) {
			break
		}
		// A RETRY THAT CANNOT FINISH IS WORSE THAN NO RETRY. It spends the
		// caller's remaining budget waiting, then hands back ctx.Err() instead
		// of whatever the server actually said — and the fallback that needed
		// that budget has none left. streamCloseRetries is therefore an upper
		// bound and the deadline is the authority: one more attempt costs the
		// pause plus a full ack wait, and it is only started if that fits.
		if d, ok := ctx.Deadline(); ok && time.Until(d) < s.closeRetryDelay+ackTimeout {
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
