package wecom

// failure_origin_test.go — a chat_session bound to a WeCom room is not
// exclusively WeCom's, and that is as true of a run that failed as of one
// that answered.
//
// The engine makes the INSTALLER the creator of a group's chat_session, so
// that session appears in their own Multica chat list. They can open it in a
// browser and ask the agent something. Both runs die the same way — one
// task:failed on the shared bus, carrying the same chat_session_id — and
// nothing in the event says which surface asked. Without the question,
// handleTaskFailed resolves the room off the delivery row and announces, in
// front of everyone in it, that something they never saw has gone wrong.
//
// A browser run with no bubble of its own has no route into the room at all:
// it has no delivery row, so there is no chat to address. The route that
// matters is a bubble it bound off task:queued, and that is what the tests
// below drive. The rest pin the branches where the origin cannot be
// established. This is an authorization check on writing into somebody else's
// group chat, so those refuse — a lookup that did not answer is not evidence
// the question came from WeCom.

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
)

// logRecorder keeps what the manager logged. A refusal writes no frame and no
// message, so without this a test asserting "nothing was sent" passes just as
// happily when the handler returned three lines earlier for an unrelated
// reason. The WARN line is the refusal's only outward sign, and it is also the
// only thing that tells an operator their database has stopped answering a
// question the room's notices depend on.
type logRecorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (r *logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *logRecorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec.Clone())
	return nil
}

func (r *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *logRecorder) WithGroup(string) slog.Handler      { return r }

// refusals returns the reason attribute of every WARN line the origin gate
// wrote, in order.
func (r *logRecorder) refusals() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, rec := range r.records {
		if rec.Level != slog.LevelWarn || !strings.Contains(rec.Message, "origin cannot be established") {
			continue
		}
		reason := ""
		rec.Attrs(func(a slog.Attr) bool {
			if a.Key == "reason" {
				reason = a.Value.String()
			}
			return true
		})
		out = append(out, reason)
	}
	return out
}

// newBoundRoomRig is a bubbleRig whose manager also reads the delivery row —
// the address a failure notice takes when no bubble is left, and the one the
// leak travels down. newBubbleRig leaves it nil because its own tests are
// about the rounds this process holds; here it is the whole point.
func newBoundRoomRig(t *testing.T) *bubbleRig {
	t.Helper()
	rig := newBubbleRig(t)
	rig.logs = &logRecorder{}
	rig.typing = NewTypingIndicator(TypingIndicatorConfig{
		Senders:    rig.senders,
		Streams:    rig.streams,
		Tasks:      rig.q,
		Deliveries: rig.q,
		Logger:     slog.New(rig.logs),
	})
	rig.bus = events.New()
	rig.typing.Register(rig.bus)
	return rig
}

// refusedOrigin asserts that the room was told nothing and that the refusal
// was logged once, with a reason naming what could not be established.
func (r *bubbleRig) refusedOrigin(t *testing.T, wantReason string) {
	t.Helper()
	if got := pushedTexts(t, r.conn); len(got) != 0 {
		t.Fatalf("the room was told %q about a run whose origin could not be established — "+
			"an unreadable lookup granted permission to write into an external group chat", got)
	}
	if frames := r.conn.streamFrames(t); len(frames) != 0 {
		t.Fatalf("the room got %d stream frames for a run of unknown origin, want none", len(frames))
	}
	reasons := r.logs.refusals()
	if len(reasons) != 1 {
		t.Fatalf("the gate logged %d refusals (%v), want exactly 1 — a failure notice this process "+
			"decided to swallow has to be visible to whoever runs it", len(reasons), reasons)
	}
	if !strings.Contains(reasons[0], wantReason) {
		t.Errorf("refusal reason = %q, want it to name %q", reasons[0], wantReason)
	}
}

// askedInTheBrowser files a task row for a run the installer started in the
// web UI: it owns its own input batch, like every direct task since MUL-4351,
// and the messages in that batch carry no channel_ingested stamp.
func (r *bubbleRig) askedInTheBrowser(t *testing.T, taskName string) {
	t.Helper()
	r.q.fileTask(t, taskUUID(t, taskName))
	r.q.channelIngested = askedInTheWebUI()
}

// askedInTheRoom files the same row for a question typed in WeCom. The stamp
// is stated here rather than left to the fake: a control that only holds
// because of a default is not one.
func (r *bubbleRig) askedInTheRoom(t *testing.T, taskName string) {
	t.Helper()
	r.q.fileTask(t, taskUUID(t, taskName))
	r.q.channelIngested = askedOverWecom()
}

// pushedTexts is what the room actually read: the markdown of every
// aibot_send_msg the connection was asked to write.
func pushedTexts(t *testing.T, c *bubbleConn) []string {
	t.Helper()
	var out []string
	for _, body := range c.pushes(t) {
		md, _ := body["markdown"].(map[string]any)
		if md == nil {
			out = append(out, "")
			continue
		}
		s, _ := md["content"].(string)
		out = append(out, s)
	}
	return out
}

// The gate must not seal a WeCom round's bubble with a web run's ending. It
// runs before the take for exactly this: the room has a live question of its
// own, and the browser run's failure has no business ending it.
func TestAWebUIRunsFailureLeavesTheRoomsOwnBubbleAlone(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	rig.ran(t, "REQ-1", "task-1") // the room's own question, still running
	rig.askedInTheBrowser(t, "task-2")

	rig.failed(t, "task-2", false)

	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the room was told %q about a browser run", got)
	}
	if frames := rig.conn.streamFrames(t); len(frames) != 1 {
		t.Fatalf("the room's bubble went from 1 frame to %d — a browser run's failure wrote "+
			"into the bubble the room's own question is still waiting on", len(frames))
	}
	if rig.streams.depth() != 1 {
		t.Fatalf("the room's round is gone (depth %d) — its own answer now has nowhere to land",
			rig.streams.depth())
	}
}

// ---- where the origin cannot be established ----
//
// Every one of these refuses and says so. Writing into a WeCom group is a
// permission, and none of these branches is evidence that the run came from
// WeCom: a `connection refused` from the database says nothing at all about
// which surface asked. "One line of copy naming no question and no answer"
// still tells a room that activity nobody there can see has gone wrong, and
// the existence of the activity is the disclosure.
//
// The case that made fail-open tempting is covered further down, out of the
// round state this process already holds.

// The database did not answer. A lookup that failed is not a verdict, and a
// gate that treated it as one would let an outage hand out the permission the
// gate exists to withhold.
func TestAnUnreadableOriginRefusesTheFailure(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	// The gate's population: a run with no delivery row (no row, so the stamp is the only thing that could say).
	rig.q.deliveryFiled = notFiled()
	rig.askedInTheRoom(t, "task-1")
	rig.q.originErr = errors.New("connection refused")

	rig.failed(t, "task-1", false)

	rig.refusedOrigin(t, "connection refused")
}

// ---- and what an open round is, and is not, evidence of ----
//
// A round still open used to skip the gate entirely: it was written by the
// inbound path, so the bubble is the room's, and under the batch identity the
// engine handed down the run bound to it was the room's too.
//
// Binding off task:queued separates those two facts. The BUBBLE is still
// proof — OnIngested only ever runs for a WeCom turn. The RUN is not: a
// question typed in Multica on the same session publishes an event with the
// same chat_session_id and the same NULL issue_id, and can bind it. So the
// ending has to be attributed from the database, and when the database cannot
// answer, the honest outcome is silence.
//
// That is a real cost and it is the cheaper of the two. A withheld notice
// leaves the asker without news of a run that failed. Announcing on a guess
// puts a browser run's error text in front of everyone in a room that never
// saw the question, in a chat with no unsend.
//
// REVERSE VERIFICATION: let the failure through when the origin read fails and
// this reports the room sealed by a run it cannot attribute.
func TestAnOutageWithholdsTheNoticeRatherThanGuessingTheOrigin(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	// The gate's population: a run with no delivery row (no row, so the stamp is the only thing that could say).
	rig.q.deliveryFiled = notFiled()
	rig.ran(t, "REQ-1", "task-1")
	rig.q.taskErr = errors.New("connection refused")
	rig.q.originErr = errors.New("connection refused")

	rig.failed(t, "task-1", false)

	frames := rig.conn.streamFrames(t)
	if len(frames) != 1 {
		t.Fatalf("the bubble was sealed on an origin nobody could establish: %v", frames)
	}
	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the room was told %q about a run whose origin could not be read", got)
	}
	reasons := rig.logs.refusals()
	if len(reasons) != 1 || !strings.Contains(reasons[0], "connection refused") {
		t.Fatalf("the gate logged %v, want one refusal naming the read that failed — a notice this process swallowed has to be visible to whoever runs it", reasons)
	}
	// The round is left alone too: an unreachable database is not evidence the
	// run belongs elsewhere, and releasing on it would hand the room's own
	// bubble away before the answer still coming for it arrives.
	if !rig.streams.has(bubbleSessionID(t), taskUUID(t, "task-1")) {
		t.Fatal("the outage released the round: the answer still coming for it will find nothing to seal")
	}
}

// TestAnotherChannelsFailureNeverReachesTheTaskRow is the cost side of the
// same gate, and it is a correctness question dressed as a performance one.
//
// task:failed is published for every run in the deployment, and the bus is
// synchronous — this subscriber runs on the publisher's goroutine, so whatever
// it does before concluding "not mine" is charged to a failure that has
// nothing to do with WeCom, and every listener registered behind it waits.
//
// engine.TaskInputIsChannelIngested cannot be what turns those away: it
// reports whether the input came from A channel, not from THIS one, so a
// failed Slack run passes it and used to be rejected two queries later by a
// binding lookup that found no WeCom row. The binding row is what actually
// answers "is this session ours", so it is asked first, and a run this adapter
// has nothing to do with must not reach the task row at all.
//
// The query counts are the assertion rather than a stopwatch: they are what
// "returns promptly" means here, and they do not flake on a loaded machine.
func TestAnotherChannelsFailureNeverReachesTheTaskRow(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	// A failed Slack run. The row is real and its input IS channel-ingested,
	// so the origin gate would answer "deliver" if it were ever asked.
	rig.askedInTheRoom(t, "task-1")
	// What makes it Slack's: the delivery row is real and names Slack. Modelling
	// it as a MISSING row was the fixture's own error — that is the first-party
	// shape, and it is the one population that must reach the gate.
	rig.q.sessionChannelType = "slack"

	rig.failed(t, "task-1", false)

	if rig.q.taskGets != 0 {
		t.Fatalf("the task row was read %d time(s) for a failed run on a session with no WeCom "+
			"binding — another channel's failure has to cost this adapter one lookup, not three, "+
			"and it is paying for it on the publisher's goroutine", rig.q.taskGets)
	}
	if asked := rig.q.originAsked(); len(asked) != 0 {
		t.Fatalf("the channel_ingested stamp was read for %v — a run on a session this adapter "+
			"is not bound to never had an origin worth establishing", asked)
	}
	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the room was told %q about another channel's failed run", got)
	}
	if frames := rig.conn.streamFrames(t); len(frames) != 0 {
		t.Fatalf("another channel's failed run wrote %d stream frames here, want none", len(frames))
	}
}

// ---- and where the local evidence must not be manufactured ----
//
// The gate answers "yes, this run is ours" from memory before it asks the
// database, and the memory it reads is the open list. That shortcut is only
// sound while the list holds rounds this adapter actually ingested: a delivery
// attempt for a run of somebody else's must leave nothing behind that the gate
// would later read as proof.

// ---------------------------------------------------------------------------
// the gate has to survive a bubble that is already bound
// ---------------------------------------------------------------------------

// A question typed in Multica on a WeCom-bound session produces a task:queued
// that is byte-identical to the room's own: CreateChatTask inserts issue_id as
// NULL for EVERY chat task — WeCom, web, desktop, mobile — and taskEvent
// stamps the same chat_session_id. So the browser's run can bind the room's
// bubble, and from that moment the round being on this session's open list
// says nothing about where the question was asked.
//
// The failure path used to treat exactly that as proof: a round on the list
// skipped the origin gate. With the batch identity the engine handed down that
// was sound — the binding was WeCom's own. Off the bus it is not, and the
// consequence is the worst one this adapter has: everybody in the room reads
// the error text of a question none of them asked.
//
// REVERSE VERIFICATION: restore the `if !m.streams.has(sessionID, taskID)`
// wrapper around the gate and this fails with the room told about a browser
// run's failure.
func TestABrowserRunThatBoundTheRoomsBubbleIsStillNotAnnouncedInIt(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.ask(t, "REQ-ROOM")            // the room's own question paints a bubble
	rig.askedInTheBrowser(t, "web-1") // …and the browser's run is what binds it
	rig.queueTask(t, taskUUID(t, "web-1"), "")

	rig.failed(t, "web-1", false)

	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the room was told %q about a run nobody in it started", got)
	}
	closing := 0
	for _, f := range rig.conn.streamFrames(t) {
		if f["finish"] == true {
			closing++
		}
	}
	if closing != 0 {
		t.Fatalf("the room's bubble was sealed with a browser run's failure (%d closing frame(s))", closing)
	}
}

// And the round has to come back. A gate that refuses without releasing what
// it refused leaves the bubble bound to a run that will never close it: the
// asker watches it turn until the platform ends it, and their own answer —
// which arrives with no round left to take — degrades to a plain message.
//
// REVERSE VERIFICATION: return from the refusal without releasing and this
// fails with the answer arriving as a push instead of in the bubble.
func TestARefusedRunGivesTheBubbleBackToTheOneThatEarnedIt(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.out = NewOutbound(rig.q, rig.senders, rig.streams, nil)
	rig.ask(t, "REQ-ROOM")
	rig.askedInTheBrowser(t, "web-1")
	rig.queueTask(t, taskUUID(t, "web-1"), "")
	rig.failed(t, "web-1", false) // refused by the gate, and released

	// Now the room's own run, which was queued behind it.
	rig.askedInTheRoom(t, "task-1")
	rig.queueTask(t, taskUUID(t, "task-1"), "")
	rig.answer(t, "the agent reply", "task-1")

	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("the room's answer arrived as %d plain message(s) — its bubble was still held by the browser run: %v", len(pushes), pushes)
	}
	sealed := false
	for _, f := range rig.conn.streamFrames(t) {
		if f["finish"] == true && f["content"] == "the agent reply" {
			sealed = true
		}
	}
	if !sealed {
		t.Fatal("the room's answer never sealed the bubble its own question opened")
	}
}

// Cancellation had no origin gate at all. It mattered less when only WeCom's
// own runs could hold a WeCom bubble; once a browser run can bind one, a
// "这次处理已取消" seals the room's bubble for a cancellation nobody in the
// room performed — and the question that bubble belonged to is still running.
//
// REVERSE VERIFICATION: drop the gate from handleTaskCancelled and this fails
// with the room's bubble sealed by a browser run's cancellation.
func TestABrowserRunsCancellationDoesNotSealTheRoomsBubble(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.ask(t, "REQ-ROOM")
	rig.askedInTheBrowser(t, "web-1")
	rig.queueTask(t, taskUUID(t, "web-1"), "")

	rig.cancelled(t, "web-1")

	for _, f := range rig.conn.streamFrames(t) {
		if f["finish"] == true {
			t.Fatalf("the room's bubble was sealed by a browser run's cancellation: %v", f)
		}
	}
	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the room was told %q about a cancellation nobody in it performed", got)
	}
}

// ---------------------------------------------------------------------------
// the notice has to survive a replica that holds no socket
// ---------------------------------------------------------------------------

// On main a failure notice produced on a replica with no socket for this
// installation was handed to the one holding it, as an ordinary relayKindReply.
// The bubble path took the notice off the answer's route and onto its own,
// which speaks to the sender registry directly — so on any multi-replica
// deployment the notice is simply not delivered, and the WS lease guarantees
// that is the NORMAL case rather than an edge: exactly one replica holds the
// socket, and the run can end on any of them.
//
// What the asker gets instead is nothing, under a bubble that turns until the
// platform ends it.
//
// REVERSE VERIFICATION: take the relay back out of sayAsPlainMessage and this
// fails with nothing routed and the notice lost.
func TestAFailureOnAReplicaWithNoSocketIsRoutedToTheOneThatHasIt(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	relay := &recordingRelay{}
	rig.typing.WithRelay(relay)
	rig.senders.clear(rig.instID, rig.conn.sender) // this replica holds no socket for it
	rig.askedInTheRoom(t, "task-1")

	rig.failed(t, "task-1", false)

	if len(relay.frames) != 1 {
		t.Fatalf("%d frames routed, want 1 — the asker was told nothing and no other replica was asked to tell them", len(relay.frames))
	}
	f := relay.frames[0]
	if f.Kind != relayKindReply || f.Content != streamCopyFailed {
		t.Fatalf("routed %+v, want the failure notice as an ordinary reply", f)
	}
	if f.TaskID != taskUUID(t, "task-1") {
		t.Fatalf("routed frame names task %q, want %q — the holder cannot seal the right bubble without it", f.TaskID, taskUUID(t, "task-1"))
	}
}

// recordingRelay is the router seam with nothing behind it: it records what
// would have been handed to the replica holding the socket.
type recordingRelay struct{ frames []relayFrame }

func (r *recordingRelay) publish(f relayFrame, _ string) bool {
	r.frames = append(r.frames, f)
	return true
}

// ---- the answer path's gate has to hand the bubble back too ----

// The failure path releases a round a first-party run took; the answer path
// returns at its own gate (TaskInputIsChannelIngested) and used to leave the
// round bound to a run that will never close it. Same rule, second call site:
// whoever refuses to speak for a run has to give back what that run is holding.
//
// Without it the asker watches their bubble turn until the platform ends it,
// and their own answer finds no round and degrades to a plain message.
func TestAWebRunsCompletionAlsoGivesTheRoomsBubbleBack(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.out = NewOutbound(rig.q, rig.senders, rig.streams, nil)
	rig.ask(t, "REQ-ROOM-2")
	rig.askedInTheBrowser(t, "web-2")
	rig.queueTask(t, taskUUID(t, "web-2"), "")

	// The browser run COMPLETES — it does not fail — so the answer path's gate
	// is the one that has to release.
	rig.answer(t, "the browser's answer", "web-2")

	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the room was told %q about a run nobody there started", got)
	}

	// The room's own run, which was queued behind the browser's.
	rig.askedInTheRoom(t, "task-r")
	rig.queueTask(t, taskUUID(t, "task-r"), "")
	rig.answer(t, "the agent reply", "task-r")

	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("the room's answer arrived as %d plain message(s) — its bubble was still held by "+
			"the browser run: %v", len(pushes), pushes)
	}
	sealed := false
	for _, f := range rig.conn.streamFrames(t) {
		if f["finish"] == true && f["content"] == "the agent reply" {
			sealed = true
		}
	}
	if !sealed {
		t.Fatal("the room's answer never sealed the bubble its own question opened")
	}
}
