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
// bubble that cannot be written to — never painted, past its window, disowned
// by the server, lost to a restart — is simply not used. Nothing here records
// what was said, what is owed, or who was told; nothing owes anyone anything
// once the bubble is gone.
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
//     round's task id — see roundTaker, which resolves the clone through it.
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
// ten-minute mark, and died at its own. That is what makes rotating onto a
// fresh stream a real way to outlive the window — see rotate, and fireGuard in
// typing_indicator.go, which is where it is done.
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
	// that opened it — including every stream the round is rotated onto.
	ReqID string

	// StreamID is ours to choose. Reusing it updates the message; a new one
	// opens another — which is exactly how a session comes to hold several
	// bubbles at once, and how a round outlives one stream's window.
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
	// that names a task and nobody else — and one of them runs on a timer,
	// minutes after the goroutine that knew who asked is gone.
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
	// reports false and its words go out as an ordinary message. A handle the
	// server has disowned is still handed back, with Unusable set, because
	// its addressing is still the chat that asked.
	Handle    streamHandle
	HasBubble bool
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

// roundEntry is one run's place in a session, from the moment anything is
// known about it until something takes it. Whoever takes or drops the round
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

	// guard rotates the bubble onto a fresh stream before the protocol's
	// window runs out on the current one.
	guard *time.Timer

	// steps counts the run's signs of life — every task:message or
	// task:progress event for it, whether or not a line was painted — and
	// stepsAtOpen is the count when the current stream opened. The guard
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

	// unusable is the server's verdict that this stream takes no further
	// frame, kept rather than acted on by forgetting the round. The bubble is
	// over either way; the round is not, and the handle is the only address
	// its ending has.
	unusable bool

	// createdAt bounds the entry for the sweep when there is no handle to read
	// a time off.
	createdAt time.Time
}

// maxFinishedRounds bounds the per-session memory of closed batches. Ten
// rounds back is far more than an ingest goroutine can lag by — it holds the
// Router's reply budget, a couple of seconds, against rounds that take
// minutes.
const maxFinishedRounds = 10

// streamStore maps chat_session_id to that session's rounds, oldest first.
type streamStore struct {
	mu       sync.Mutex
	sessions map[string][]*roundEntry

	// finished remembers, per session, the last few batches whose round has
	// been taken, so a badly delayed OnIngested cannot paint a second bubble
	// for a run that has already answered. It is the one thing kept about a
	// round after it is gone, and it says nothing about what was said — only
	// that nothing more should be painted. Bounded by maxFinishedRounds; a
	// session whose rounds are all gone keeps its ring until the sweep drops
	// it along with everything else past the window.
	finished map[string]finishedRing

	maxAge time.Duration
	now    func() time.Time

	// closeRetryDelay is the gap between two attempts at a closing frame
	// (seal). A field so a test can run the retries without the two seconds.
	closeRetryDelay time.Duration
}

// finishedRing is one session's recently closed batches, with when the last
// one was added so the sweep can retire the whole ring.
type finishedRing struct {
	batches []engine.RunBatchID
	at      time.Time
}

func (r finishedRing) has(batch engine.RunBatchID) bool {
	if batch == 0 {
		return false
	}
	for _, id := range r.batches {
		if id == batch {
			return true
		}
	}
	return false
}

func newStreamStore() *streamStore {
	return &streamStore{
		sessions:        make(map[string][]*roundEntry),
		finished:        make(map[string]finishedRing),
		maxAge:          streamMaxAge,
		now:             time.Now,
		closeRetryDelay: streamCloseRetryDelay,
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

// finishedLocked reports whether a batch's round has already been taken.
// Caller holds s.mu.
func (s *streamStore) finishedLocked(key string, batch engine.RunBatchID) bool {
	return s.finished[key].has(batch)
}

// retireLocked records that a batch's round is over, keeping the ring
// bounded. Caller holds s.mu.
func (s *streamStore) retireLocked(key string, batch engine.RunBatchID) {
	if batch == 0 {
		return
	}
	ring := s.finished[key]
	if !ring.has(batch) {
		ring.batches = append(ring.batches, batch)
		if len(ring.batches) > maxFinishedRounds {
			ring.batches = ring.batches[len(ring.batches)-maxFinishedRounds:]
		}
	}
	ring.at = s.now()
	s.finished[key] = ring
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

	if s.finishedLocked(key, batch) {
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
	if s.finishedLocked(key, batch) {
		return
	}
	if e := s.entryLocked(key, batch); e != nil {
		e.taskID = taskID
		return
	}
	s.insertLocked(key, &roundEntry{batch: batch, taskID: taskID, createdAt: s.now()})
}

// arm attaches the expiry guard to a round, replacing any earlier one. A
// round that ended between the open and this call has already left the list,
// so there is nothing to guard and the timer is stopped instead of leaked.
func (s *streamStore) arm(sessionID pgtype.UUID, batch engine.RunBatchID, t *time.Timer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entryLocked(util.UUIDToString(sessionID), batch); e != nil {
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
// The batch goes on the session's finished ring so a badly delayed OnIngested
// cannot paint a second bubble for a run that has already answered. Caller
// holds s.mu.
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
	s.retireLocked(key, entry.batch)

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
	if i := s.indexLocked(key, k); i >= 0 {
		turn := s.takeAtLocked(key, i)
		s.mu.Unlock()
		return turn, true
	}
	worthResolving := !k.byBatch && k.taskID != "" && len(s.sessions[key]) > 0
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

// feedFor hands back the bubble a step belongs in, and the scrolling list that
// bubble is painted from. The step is addressed by the run the debounced flush
// bound to the round, so a session with several rounds open never has to guess.
//
// The feed is created lazily, on the first step that reaches a round, and lives
// on the entry so it dies exactly when the round does — a list of a finished
// run's tool calls has nothing left to be painted into.
func (s *streamStore) feedFor(sessionID pgtype.UUID, taskID string) (streamHandle, *progressFeed, bool) {
	if taskID == "" {
		return streamHandle{}, nil, false
	}
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.sessions[key] {
		if r.taskID != taskID {
			continue
		}
		// Three ways a round on the list still has nowhere to put a step. No
		// bubble: the flush filed the round before the ingest goroutine
		// painted one, or the opening frame was refused. Past the window: the
		// server refuses the frame and the ending will fall back to a plain
		// message. Disowned: another connection owns this conversation now,
		// and every frame from here is a refusal counted against the whole
		// bot's rate limit.
		r.steps++
		if !r.painted || r.unusable || s.expiredLocked(r.handle.CreatedAt) {
			return streamHandle{}, nil, false
		}
		if r.feed == nil {
			r.feed = newProgressFeed(s.now)
		}
		return r.handle, r.feed, true
	}
	return streamHandle{}, nil, false
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
// from. The feed starts over too — the new stream is a new bubble and carries
// none of the old one's lines.
func (s *streamStore) rotate(sessionID pgtype.UUID, batch engine.RunBatchID, streamID string) (old, next streamHandle, ok bool) {
	if streamID == "" {
		return streamHandle{}, streamHandle{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entryLocked(util.UUIDToString(sessionID), batch)
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
	e.feed = nil
	e.rotations++
	e.stepsAtOpen = e.steps
	return old, next, true
}

// has reports whether a session holds a round bound to this run. A round is
// opened by a message this adapter ingested and named by the flush that
// answered it, so an entry here is local proof the question was asked in the
// room — the one case the failure notice's origin gate can decide without a
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

// holding reports whether this store has any round on file anywhere, painted
// or not. It is the "nothing here to close" test at the head of the two
// ending subscribers.
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
	for _, rounds := range s.sessions {
		if len(rounds) > 0 {
			return true
		}
	}
	return false
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
// tests, and the cheap rejection at the head of the progress subscribers.
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

// sweepLocked evicts rounds the server would no longer accept, and the
// finished rings of sessions that have been quiet for a whole window. The
// guard timer normally rotates a round long before this fires; the sweep is
// what keeps a process whose timers were beaten by a clock jump from
// accumulating entries forever. Caller holds s.mu.
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
	for key, ring := range s.finished {
		if s.expiredLocked(ring.at) {
			delete(s.finished, key)
		}
	}
}

// seal writes a bubble's closing frame and is the one place the retry policy
// for closing frames lives. Every closer goes through it: the answer, the
// failure and cancellation notices, the settled flush, and the hand-over a
// rotation writes on the stream it is leaving.
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
// halves of the store's identity live behind it: the binding the flush filed,
// and the one column that resolves an auto-retry clone back to it.
type roundTaker struct {
	streams *streamStore
	tasks   taskLookup
	log     *slog.Logger
}

// take is roundTaker's one job: take on the store, with the auto-retry lookup
// supplied.
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
