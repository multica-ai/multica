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
// A session holds a LIST of open bubbles, not one. Messages inside one
// debounce window share a run and share a bubble; a message past the window
// starts a round of its own, queued behind the run in flight — and it gets its
// own bubble immediately, because a message that produces nothing on screen
// reads as a message that was lost. The rounds are FIFO: the engine serializes
// chat tasks per session (ClaimAgentTask), so the oldest open round is the one
// running and everything behind it is waiting. That ordering is what lets an
// ending find its bubble without carrying a task id everywhere: the answer
// seals the head, a flush that produced no task seals the tail, and a task id,
// when there is one, overrides both.
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

// sameRoundWindow is how close two messages have to be to be one agent run.
//
// It is the engine's debounce window rather than a number of its own, because
// it is the same question asked twice. The engine's batcher re-arms a
// per-session timer on every inbound message and fires one run when the session
// has been quiet for that long (engine/batcher.go), so messages less than a
// window apart are collected into a single run and anything later is a run of
// its own. A local constant here would be a second copy of that rule, free to
// drift from the one that decides.
//
// The gap is measured from the PREVIOUS message on the NEWEST round, not from
// any bubble's opening: the timer re-arms, so a message every two seconds is
// one run no matter how long the burst goes on, and a bubble's age says
// nothing about which run the next message belongs to.
//
// Known divergence: this re-measures with its own clock what the batcher
// already decided with its. OnIngested runs on a detached goroutine, so the
// two measurements of one gap can differ by scheduling jitter, and a gap
// within jitter of the window can be classified differently on the two sides
// — a bubble for a round the batcher merged, or one bubble for two runs.
// Removing it means threading the batcher's own verdict through the engine's
// TypingNotifier seam (an upstream API change); until then the window is 3s
// and the jitter is milliseconds.
const sameRoundWindow = engine.DefaultChatRunBatchWindow

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
	// roundForgotten — nothing on file. A turn from before a restart, or one
	// that never opened a bubble at all. The caller has to find the chat some
	// other way.
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
	// roundOpened — a new round. The caller paints the opening frame and arms
	// the guard; from here on the round owns the handle it registered.
	roundOpened openVerdict = iota
	// roundJoined — inside the debounce window of the newest open round. The
	// engine's batcher folds this message into that round's run, so the bubble
	// already on screen is this message's receipt too and nothing is painted.
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

func (h streamHandle) address() roundAddress {
	return roundAddress{
		InstallationID: h.InstallationID,
		ChatID:         h.ChatID,
		ChatType:       h.ChatType,
	}
}

// endedRound is what a session keeps once a handle is gone: where the round
// was speaking, whether anything is still owed to it, and which run it was.
//
// owed is the heart of it. A handle is taken by whichever ending gets there
// first, and the five-minute guard is allowed to be that one — it writes
// "still working, I'll reply separately" while the run carries on. The failure
// that arrives afterwards finds no handle, and without this note it would
// return without a word: a promise, and then nothing.
//
// taskID is the adoption fence. Once the guard has consumed a round's handle,
// the round's run is still going, and the next open bubble in line — a QUEUED
// message's — must not be sealed by it.
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
	// oldest first, each holding its run id — empty when the guard fired
	// before the run had a name. A round's own ending settles its own entry;
	// another round's cannot.
	owed []string
	// fenced lists every run whose own bubble is already over, so none of them
	// may seize a bubble opened later. A single slot could not do it: it would
	// hold whichever run ended LAST, and a slow first question's ending then
	// walks straight past it into the third question's bubble — sealing it
	// with the wrong answer, and leaving that question's own run locked out of
	// its own bubble.
	fenced []string
}

// isFenced reports whether this run's own bubble is already over.
func (e endedRound) isFenced(taskID string) bool {
	if taskID == "" {
		return false
	}
	for _, id := range e.fenced {
		if id == taskID {
			return true
		}
	}
	return false
}

// fence adds a run to the list, keeping it bounded: a session that runs for
// days should not accumulate ids forever, and a run old enough to have fallen
// off cannot still have a bubble to protect.
func (e endedRound) fence(taskID string) []string {
	if taskID == "" || e.isFenced(taskID) {
		return e.fenced
	}
	next := append(e.fenced, taskID)
	if len(next) > maxFencedRuns {
		next = next[len(next)-maxFencedRuns:]
	}
	return next
}

// maxFencedRuns bounds the fence list. Ten rounds back is far more than a run
// can outlive — the platform stops accepting frames for a bubble after six
// minutes, and rounds serialize per session.
const maxFencedRuns = 10

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
	if taskID == "" {
		return owed
	}
	// A named ending with no matching promise settles nothing: the promise on
	// file belongs to a different round, and taking it would leave that
	// round's asker with silence.
	return owed
}

// roundEntry pairs one bubble's handle with everything scoped to its life: the
// guard timer that closes it if nothing else does, and the run it belongs to.
// Whoever takes or drops the round disposes of all of it in one lock.
type roundEntry struct {
	handle streamHandle
	guard  *time.Timer

	// taskID is the run this bubble belongs to, adopted by the first ending
	// that reaches it. A chat session outlives its turns, so a previous turn's
	// ending can still be arriving after the user has asked the next question
	// — it resolves to the same session and would otherwise seal the new
	// bubble with the old answer.
	taskID string

	// lastIngest is when this round last took a message — the bubble's own
	// opening for the first one, and every joined follow-up after that. On the
	// newest round it is what separates a burst from a queue (sameRoundWindow).
	lastIngest time.Time
}

// streamStore maps chat_session_id to that session's open bubbles, oldest
// first, and — for a while after the last bubble is gone — to what the round
// it belonged to still owes.
type streamStore struct {
	mu       sync.Mutex
	sessions map[string][]*roundEntry
	ended    map[string]endedRound

	// unadoptedDebt counts, per session, the rounds the guard closed before
	// any run was matched to them. Their runs are still coming and have no id
	// on file to be fenced by; see settleUnadoptedDebtLocked.
	unadoptedDebt map[string]int

	maxAge time.Duration
	now    func() time.Time
}

func newStreamStore() *streamStore {
	return &streamStore{
		sessions:      make(map[string][]*roundEntry),
		ended:         make(map[string]endedRound),
		unadoptedDebt: make(map[string]int),
		maxAge:        streamMaxAge,
		now:           time.Now,
	}
}

// NewStreamStore is the constructor boot uses to mint the one store shared by
// the typing indicator (writer) and the chat-done subscriber (reader).
func NewStreamStore() *streamStore { return newStreamStore() }

// clock reads the store's own time source, so callers that need to stamp a
// moment (a message's arrival, before any of the work that delays it) use the
// same clock the store compares against — including the fake one tests drive.
func (s *streamStore) clock() time.Time { return s.now() }

// open registers a message against a session and says whether it starts a
// bubble of its own. Inside the debounce window of the newest open round it
// joins that round — the engine's batcher is about to fold the two messages
// into one run, and two bubbles for one answer is one bubble nobody ever
// closes. Past the window it is a round of its own, opened immediately: it
// will wait for as long as the run ahead of it needs, and that wait must not
// look like silence.
func (s *streamStore) open(sessionID pgtype.UUID, h streamHandle) openVerdict {
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()

	// Both sides of this comparison are ARRIVAL times, not "when the code got
	// here" times: the caller stamps CreatedAt before the lookups that delay
	// it, so a slow database moves neither. Measuring with now() here is what
	// would let this disagree with the engine's debouncer, which has always
	// judged the same gap from arrival.
	if h.CreatedAt.IsZero() {
		h.CreatedAt = s.now()
	}
	rounds := s.sessions[key]
	if n := len(rounds); n > 0 {
		newest := rounds[n-1]
		if h.CreatedAt.Sub(newest.lastIngest) < sameRoundWindow {
			newest.lastIngest = h.CreatedAt
			return roundJoined
		}
		h.QueuedBehind = true
	}
	s.sessions[key] = append(rounds, &roundEntry{handle: h, lastIngest: h.CreatedAt})
	return roundOpened
}

// arm attaches the expiry guard to the round holding streamID. A round that
// ended between the open and this call has already left the list, so there is
// nothing to guard and the timer is stopped instead of leaked.
func (s *streamStore) arm(sessionID pgtype.UUID, streamID string, t *time.Timer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.sessions[util.UUIDToString(sessionID)] {
		if r.handle.StreamID == streamID {
			r.guard = t
			return
		}
	}
	t.Stop()
}

// takeTail removes and returns the newest open round — the one whose flush
// just settled without producing a task (OnSettled). The rounds ahead of it
// belong to runs that are still real.
func (s *streamStore) takeTail(sessionID pgtype.UUID, ending roundEnding) (streamHandle, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := util.UUIDToString(sessionID)
	rounds := s.sessions[key]
	if len(rounds) == 0 {
		return streamHandle{}, false
	}
	return s.takeAtLocked(key, len(rounds)-1, ending)
}

// takeTask removes and returns the round belonging to taskID.
//
// An exact match anywhere in the list wins. Failing that, the head is taken if
// no run has been matched to it yet — the tasks serialize per session, so an
// unmatched head IS the running round. A head already matched to a DIFFERENT
// run is refused: taking it would seal the wrong bubble, and the caller has the
// ended-notes path for a run whose bubble is already gone. An empty taskID is a
// caller whose event named no run at all, and gets the head unconditionally.
func (s *streamStore) takeTask(sessionID pgtype.UUID, taskID string, ending roundEnding) (streamHandle, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := util.UUIDToString(sessionID)
	rounds := s.sessions[key]
	if len(rounds) == 0 {
		// Settle before leaving: a debt from a guard close that emptied the
		// session is this run's, and left standing it is paid by the next
		// question — which then loses its own answer's bubble.
		s.settleUnadoptedDebtLocked(key, taskID)
		return streamHandle{}, false
	}
	if taskID == "" {
		return s.takeAtLocked(key, 0, ending)
	}
	for i, r := range rounds {
		if r.taskID == taskID {
			return s.takeAtLocked(key, i, ending)
		}
	}
	if rounds[0].taskID == "" {
		// Two fences, because taking IS adopting: a run whose own bubble the
		// guard already closed may not seize the queued round's bubble as its
		// ending. The note names the run when the guard fired after the run
		// had a name; the debt covers the one that died wordless —
		// per-session serialization makes the first unseen run to end the dead
		// round's. Either way the caller's plain-message path delivers the
		// words to the right chat, and the queued round keeps its bubble for
		// its own run.
		if note, has := s.ended[key]; has && note.isFenced(taskID) {
			return streamHandle{}, false
		}
		if s.settleUnadoptedDebtLocked(key, taskID) {
			return streamHandle{}, false
		}
		// Record whose ending this was, so the note the take files carries the
		// run's real id rather than an empty one.
		rounds[0].taskID = taskID
		return s.takeAtLocked(key, 0, ending)
	}
	return streamHandle{}, false
}

// settleUnadoptedDebtLocked answers "does this run belong to a round whose
// bubble the guard closed before the run was ever matched to it". Such a round
// had no task id to leave a fence under, so the store counts them instead:
// tasks serialize per session, so the first never-seen run to end after such a
// close is that round's. Settling the debt stamps the session's note with the
// run's id, which restores the exact-id fence for the rest of that run's
// events. Caller holds s.mu.
func (s *streamStore) settleUnadoptedDebtLocked(key, taskID string) bool {
	if s.unadoptedDebt[key] <= 0 {
		return false
	}
	s.unadoptedDebt[key]--
	if s.unadoptedDebt[key] == 0 {
		delete(s.unadoptedDebt, key)
	}
	if note, has := s.ended[key]; has {
		note.fenced = note.fence(taskID)
		s.ended[key] = note
	}
	return true
}

// takeStream removes and returns the round holding streamID — the guard's own
// closer, which must end exactly the bubble whose timer fired and no other.
func (s *streamStore) takeStream(sessionID pgtype.UUID, streamID string, ending roundEnding) (streamHandle, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := util.UUIDToString(sessionID)
	for i, r := range s.sessions[key] {
		if r.handle.StreamID == streamID {
			return s.takeAtLocked(key, i, ending)
		}
	}
	return streamHandle{}, false
}

// takeAtLocked removes rounds[i] and hands its handle over — the ending of a
// round, whichever ending it is. A handle past maxAge is dropped and reported
// as absent: the server would refuse the frame, and a caller that believed it
// had a bubble would leave the user with nothing.
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
	if s.expiredLocked(entry.handle) {
		return streamHandle{}, false
	}
	if ending == roundContinues && entry.taskID == "" {
		// The guard closed a round no run had been matched to yet. Its run is
		// still coming, with an id nobody knows — count it, so the first
		// unseen run to end is recognised as this round's rather than allowed
		// to take the next bubble in line.
		s.unadoptedDebt[key]++
	}
	s.rememberLocked(key, entry.handle.address(), ending, entry.taskID)
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
	s.rememberLocked(key, addr, ending, taskID)
}

// rememberLocked files a round's ending, with one refusal: a finished ending
// for one round must not erase a still-OWED note left by ANOTHER round's
// guard. The owed note is a promise ("I'll reply separately") whose keeping
// depends on this record existing when the failure arrives; dedup of the
// finished round's own ending is what gets sacrificed, and that costs at most
// a repeated notice where losing the note costs a broken promise.
func (s *streamStore) rememberLocked(key string, addr roundAddress, ending roundEnding, taskID string) {
	next := endedRound{addr: addr, at: s.now()}
	if prev, ok := s.ended[key]; ok {
		next.owed = prev.owed
		next.fenced = prev.fence(taskID)
	} else if taskID != "" {
		next.fenced = []string{taskID}
	}
	switch ending {
	case roundContinues:
		next.owed = append(next.owed, taskID)
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
// roundForgotten is not a refusal. It means this process never saw the round —
// a restart mid-run, a turn whose opening frame the server refused — so there
// is no address on file and the caller has to find the chat itself.
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

// drop forgets the round holding streamID without sending anything — used when
// the opening frame was refused and the bubble the handle describes never
// existed.
func (s *streamStore) drop(sessionID pgtype.UUID, streamID string) {
	key := util.UUIDToString(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	rounds := s.sessions[key]
	for i, r := range rounds {
		if r.handle.StreamID != streamID {
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
		n += len(rounds)
	}
	return n
}

func (s *streamStore) expiredLocked(h streamHandle) bool {
	return s.now().Sub(h.CreatedAt) > s.maxAge
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
			if s.expiredLocked(r.handle) {
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
	// A debt is only meaningful while the session is still live enough to
	// have a note or a bubble; past both, the run it waited for is long gone.
	for key := range s.unadoptedDebt {
		if _, hasRounds := s.sessions[key]; hasRounds {
			continue
		}
		if _, hasNote := s.ended[key]; hasNote {
			continue
		}
		delete(s.unadoptedDebt, key)
	}
}
