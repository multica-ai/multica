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
//     round's task id — see takeRound, which resolves the clone through it.
//
// The rounds stay ordered by batch id, which is the order the runs execute in:
// the engine serializes chat tasks per session (ClaimAgentTask), so the oldest
// round is the running one and everything behind it is waiting. That ordering
// is what QueuedBehind reads; nothing else depends on it any more.
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
// created it. A restart loses the handles, which is exactly right: the socket
// the req_ids belonged to is gone too, so the answers fall back to plain
// messages — degraded, not corrupted. Persisting them would buy nothing and
// promise a bubble no surviving socket could write to.
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
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

// streamMaxAge is how long a handle is worth keeping. The long-connection doc
// gives a stream 10 minutes before the server ends it; the one production
// implementation we can read — Tencent's own OpenClaw plugin — treats errcode
// 846608 as a 6-minute ceiling. The two do not agree, so we take the shorter
// number: being early costs a fallback message, being late costs the answer.
//
// The window applies to a queued round's bubble the same as a running one's:
// the clock starts at the opening frame, and waiting in line does not stop it.
const streamMaxAge = 6 * time.Minute

// streamGuardAfter is when we close a bubble ourselves rather than let it run
// into streamMaxAge. A minute of headroom covers a slow frame and leaves the
// user with a sentence instead of a spinner the server will no longer let us
// replace.
const streamGuardAfter = 5 * time.Minute

// roundMemory is how long a session keeps the note the last handle left
// behind. It has to outlast the bubble by a lot: the guard closes at five
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
	// roundOwesAnEnding — a bubble was closed early and its run went on, so
	// the real ending has never been said. Addressing follows.
	roundOwesAnEnding
	// roundToldAlready — the answer landed, or another publisher's failure got
	// here first. Nothing to add.
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

// endedRound is what a session keeps once a handle is gone: where the round
// was speaking, whether anything is still owed to it, and which runs are done.
//
// owed is the heart of it. A handle is taken by whichever ending gets there
// first, and the five-minute guard is allowed to be that one — it writes
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
}

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

// settle removes one promise: this round's own, matched by run id. Returns the
// remaining list.
func settle(owed []string, taskID string) []string {
	for i, id := range owed {
		if id == taskID {
			return append(owed[:i:i], owed[i+1:]...)
		}
	}
	// A named ending with no matching promise settles nothing: the promise on
	// file belongs to a different round, and taking it would leave that
	// round's asker with silence.
	return owed
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

// takeBatch removes and returns the round for a batch — the closer that knows
// exactly which run it speaks for: the guard whose timer fired, and the flush
// that settled without producing a task.
func (s *streamStore) takeBatch(sessionID pgtype.UUID, batch engine.RunBatchID, ending roundEnding) (streamHandle, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := util.UUIDToString(sessionID)
	for i, r := range s.sessions[key] {
		if r.batch == batch {
			return s.takeAtLocked(key, i, ending)
		}
	}
	return streamHandle{}, false
}

// takeTask removes and returns the round belonging to taskID, matched on the
// binding the flush filed. There is no positional fallback: a run whose id is
// not on file has no bubble here, and taking somebody else's would seal the
// wrong question with this answer.
func (s *streamStore) takeTask(sessionID pgtype.UUID, taskID string, ending roundEnding) (streamHandle, bool) {
	if taskID == "" {
		return streamHandle{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := util.UUIDToString(sessionID)
	for i, r := range s.sessions[key] {
		if r.taskID == taskID {
			return s.takeAtLocked(key, i, ending)
		}
	}
	return streamHandle{}, false
}

// takeAtLocked removes rounds[i] and hands its handle over — the ending of a
// round, whichever ending it is. A round with no bubble reports absent: the
// caller's plain-message path is what delivers its words. So does a handle
// past maxAge, because the server would refuse the frame and a caller that
// believed it had a bubble would leave the user with nothing.
//
// ending is the caller's account of what it is about to write. Everything but
// the guard ends the round; the guard's copy promises a separate reply, and the
// note left behind is what makes that promise keepable — the failure that
// arrives ten minutes later has no handle and needs both the address and the
// knowledge that nobody has spoken yet. Caller holds s.mu.
func (s *streamStore) takeAtLocked(key string, i int, ending roundEnding) (streamHandle, bool) {
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
	usable := entry.painted && !s.expiredLocked(entry.handle.CreatedAt)
	addr := roundAddress{}
	if entry.painted {
		addr = entry.handle.address()
	}
	s.rememberLocked(key, addr, ending, entry.taskID, entry.batch)
	if !usable {
		return streamHandle{}, false
	}
	return entry.handle, true
}

// remember files where a round was speaking without taking a handle for it —
// the answer that went out as a plain message because the bubble was already
// gone, and the failure notice that had to find its chat in the binding row.
// Both are endings, and both have to be on file or the next publisher of the
// same news repeats it.
func (s *streamStore) remember(sessionID pgtype.UUID, addr roundAddress, ending roundEnding, taskID string) {
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.rememberLocked(key, addr, ending, taskID, 0)
}

// rememberLocked files a round's ending, with two refusals. A finished ending
// for one round must not erase a still-OWED note left by ANOTHER round's
// guard: the owed note is a promise ("I'll reply separately") whose keeping
// depends on this record existing when the failure arrives, and dedup of the
// finished round's own ending is what gets sacrificed — at most a repeated
// notice, where losing the note costs a broken promise. And a round that never
// had a bubble must not overwrite a known address with its blank one.
func (s *streamStore) rememberLocked(key string, addr roundAddress, ending roundEnding, taskID string, batch engine.RunBatchID) {
	next := endedRound{addr: addr, at: s.now()}
	if prev, ok := s.ended[key]; ok {
		next.owed = prev.owed
		next.finished = prev.finish(batch)
		if !addr.known() {
			next.addr = prev.addr
		}
	} else if batch != 0 {
		next.finished = []engine.RunBatchID{batch}
	}
	switch ending {
	case roundContinues:
		// Only a NAMED promise is claimable. A guard that fired before the
		// flush had named the run leaves nothing for a later failure to match,
		// and an unnamed entry would be handed to the first failure in the
		// session — which is a different round's.
		if taskID != "" {
			next.owed = append(next.owed, taskID)
		}
	case roundOver:
		next.owed = settle(next.owed, taskID)
	}
	s.ended[key] = next
}

// claimEnding asks whether a round with no bubble left is still owed an ending,
// and claims the right to say it. The claim is the point: task:failed has two
// publishers and a sweeper tick can repeat one, so whoever gets here first
// speaks and everyone after reads roundToldAlready.
//
// roundForgotten is not a refusal. It means this process has no address for
// the round — a restart mid-run, a turn whose opening frame the server refused
// — so the caller has to find the chat itself.
func (s *streamStore) claimEnding(sessionID pgtype.UUID) (roundAddress, roundVerdict) {
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	round, ok := s.ended[key]
	if !ok {
		return roundAddress{}, roundForgotten
	}
	if s.now().Sub(round.at) > roundMemory {
		delete(s.ended, key)
		return roundAddress{}, roundForgotten
	}
	if !round.addr.known() {
		return roundAddress{}, roundForgotten
	}
	if !round.isOwed() {
		return round.addr, roundToldAlready
	}
	round.owed = round.owed[1:]
	s.ended[key] = round
	return round.addr, roundOwesAnEnding
}

// remembered reports how many rounds are on file past their bubble.
// Diagnostics, and the cheap rejection at the head of the failure subscriber.
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

// hasRounds reports whether a session has any round on file. It gates the one
// database read the retry-clone lookup costs, so a session with nothing open
// never pays for it.
func (s *streamStore) hasRounds(sessionID pgtype.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions[util.UUIDToString(sessionID)]) > 0
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

// take claims the bubble a task's ending belongs to.
//
// The id on the event is tried first, because that is the id the flush bound.
// An auto-retry clone is the one case it does not match: FailTask creates the
// clone with a fresh id and it inherits the parent's chat_input_task_id, which
// is the round's own task id (EnqueueChatTask stamps chat_input_task_id = id
// on the turn it creates). So the clone's answer is routed by reading that
// column, not by falling back to whichever bubble is at the head — the round a
// clone belongs to is on file, it is just filed under the batch's owner.
//
// The lookup costs one read, and only on a miss for a session that still has a
// round open. Without a task lookup configured the miss is simply a miss: the
// answer goes out as a plain message.
func (r roundTaker) take(ctx context.Context, sessionID pgtype.UUID, taskID string, ending roundEnding) (streamHandle, bool) {
	if r.streams == nil || taskID == "" {
		return streamHandle{}, false
	}
	if h, ok := r.streams.takeTask(sessionID, taskID, ending); ok {
		return h, true
	}
	root := r.rootTaskID(ctx, sessionID, taskID)
	if root == "" || root == taskID {
		return streamHandle{}, false
	}
	return r.streams.takeTask(sessionID, root, ending)
}

// rootTaskID reads the input batch a task belongs to — its own id for a first
// attempt, the parent's for an auto-retry clone. Empty when there is nothing
// to gain from asking.
func (r roundTaker) rootTaskID(ctx context.Context, sessionID pgtype.UUID, taskID string) string {
	if r.tasks == nil || !r.streams.hasRounds(sessionID) {
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
