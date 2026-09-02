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
// The first two tests are the pair that matters: the same event, the same
// session, opposite verdicts, decided only by where the question came from.
// The rest pin the branches where the origin cannot be established. This is an
// authorization check on writing into somebody else's group chat, so those
// refuse — a lookup that did not answer is not evidence the question came from
// WeCom — and the case that made fail-open tempting is covered without them,
// by the round state this process already holds.

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
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
		// No guard: these tests drive the endings themselves.
		GuardAfter: -1,
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
// is stated here rather than left to the fake: this is the control the first
// test is read against, and a control that only holds because of a default is
// not one.
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

// TestAWebUIRunsFailureIsNotAnnouncedInTheRoom is the fix.
//
// Nobody in the room asked anything. The installer asked in a browser and that
// run failed, and the only thing tying it to WeCom is a binding row on the
// session they share.
func TestAWebUIRunsFailureIsNotAnnouncedInTheRoom(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheBrowser(t, "task-1")

	rig.failed(t, "task-1", false)

	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the room was told %q about a run nobody in it started — everyone in the chat "+
			"just learned that a question they never saw had gone wrong", got)
	}
	if frames := rig.conn.streamFrames(t); len(frames) != 0 {
		t.Fatalf("the room got %d stream frames for a browser run's failure, want none", len(frames))
	}
}

// The control, and the direction that costs more to get wrong. The round was
// asked in WeCom and this process holds no bubble for it — a restart mid-run
// — so the notice is addressed by the task's delivery row, the way the answer
// would be. It is the only "that run did not go through" WeCom ever produces.
func TestAWecomRunsFailureStillReachesTheAskerWithoutABubble(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")

	rig.failed(t, "task-1", false)

	got := pushedTexts(t, rig.conn)
	if len(got) != 1 || got[0] != streamCopyFailed {
		t.Fatalf("the asker read %q, want exactly [%q] — the failure of their own question never arrived", got, streamCopyFailed)
	}
}

// The same control with the bubble still open, so the notice goes into it
// rather than under it. A gate that refuses a WeCom round leaves
// this bubble spinning with no ending at all.
func TestAWecomRunsFailureStillClosesTheBubbleItOpened(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	rig.ran(t, "REQ-1", 1, "task-1")

	rig.failed(t, "task-1", false)

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 || frames[1]["finish"] != true || frames[1]["content"] != streamCopyFailed {
		t.Fatalf("the bubble was left as %v, want it sealed with %q — the asker is watching a "+
			"spinner for a run that is already dead", frames, streamCopyFailed)
	}
}

// The gate must not seal a WeCom round's bubble with a web run's ending. It
// runs before the take for exactly this: the room has a live question of its
// own, and the browser run's failure has no business ending it.
func TestAWebUIRunsFailureLeavesTheRoomsOwnBubbleAlone(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	rig.ran(t, "REQ-1", 1, "task-1") // the room's own question, still running
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

// A task:failed with no task id on it cannot be attributed at all. Both
// publishers go through service.taskEvent, which sets TaskID on the envelope
// and task_id in the payload from the row it is publishing about, so this is a
// shape nothing in production produces — and a test-only shape is not worth a
// standing permission to write into a customer's group chat.
func TestAFailureWithNoTaskIDIsRefused(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.q.channelIngested = askedInTheWebUI() // would refuse, if it were ever asked

	rig.bus.Publish(events.Event{
		Type:          protocol.EventTaskFailed,
		ChatSessionID: bubbleSession,
		Payload:       map[string]any{"failure_reason": "provider_network"},
	})

	rig.refusedOrigin(t, "no task id")
}

// The task row is gone — cancelled and reaped while its failure was in flight.
// Nothing left to read means nothing that says the question was asked here.
func TestAVanishedTaskRowRefusesTheFailure(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.q.channelIngested = askedInTheWebUI() // no row to ask about, so this never applies

	rig.failed(t, "task-1", false) // rig.q.tasks holds no row for it

	rig.refusedOrigin(t, "cannot read the task row")
}

// The database did not answer. A lookup that failed is not a verdict, and a
// gate that treated it as one would let an outage hand out the permission the
// gate exists to withhold.
func TestAnUnreadableOriginRefusesTheFailure(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	rig.q.originErr = errors.New("connection refused")

	rig.failed(t, "task-1", false)

	rig.refusedOrigin(t, "connection refused")
}

// ---- and where it can be established without a database ----
//
// The case the refusals above look like they cost is the case with local
// evidence, which is why the trade-off is not one. A round still open was
// written only by the inbound path — a message this adapter ingested, named by
// the flush that answered it — so it is proof of origin that no outage can
// take away.

// A round still open is evidence that costs no read at all: the bubble on
// screen was opened by a message from the room.
func TestAnOpenRoundIsProofEnoughOfOrigin(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.ran(t, "REQ-1", 1, "task-1")
	rig.q.taskErr = errors.New("connection refused")
	rig.q.originErr = errors.New("connection refused")

	rig.failed(t, "task-1", false)

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 || frames[1]["finish"] != true || frames[1]["content"] != streamCopyFailed {
		t.Fatalf("the bubble was left as %v, want it sealed with %q — a database outage left the room's "+
			"own question spinning on a run that is already dead", frames, streamCopyFailed)
	}
	if rig.q.taskGets != 0 {
		t.Errorf("the gate read %d task row(s) for a run whose own bubble is open in this process", rig.q.taskGets)
	}
}

// The verdict is read off the batch OWNER, not off the task that failed. An
// auto-retry clone's own id owns no messages, so asking about it would answer
// "not from the channel" and silence the failure of every WeCom question long
// enough to be retried.
func TestTheOriginOfARetryCloneIsItsParentsBatch(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	// FailTask's retry child: fresh id, inheriting the parent's input batch.
	rig.q.fileRetryClone(t, taskUUID(t, "retry"), taskUUID(t, "task-1"))

	rig.failed(t, "retry", false)

	asked := rig.q.originAsked()
	if len(asked) != 1 || asked[0] != taskUUID(t, "task-1") {
		t.Fatalf("the origin was asked about %v, want [%s] — a retry's failure is judged on the "+
			"question that started it, not on the clone's own empty batch",
			asked, taskUUID(t, "task-1"))
	}
	if got := pushedTexts(t, rig.conn); len(got) != 1 || got[0] != streamCopyFailed {
		t.Fatalf("the asker read %q, want [%q]", got, streamCopyFailed)
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
	// What makes it Slack's: no WeCom binding for this session.
	rig.q.sessionErr = pgx.ErrNoRows

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

// TestAWebRunsUndeliveredAnswerDoesNotBuyItTheRoomsVoice is the fix.
//
// The installer asks in their browser, against a session the room also uses.
// The origin gate turns that answer away before anything WeCom-side is
// touched: nothing taken, nothing filed, nothing said. Its later task:failed
// then has to be decided by the row, and the row says no.
//
// The answer is still driven through processEvent rather than skipped, and the
// socket is still taken down under it, because that is what makes this test
// fail if the origin gate is ever moved back behind the take: the answer would
// reach deliverAnswer and the dead socket would fail it, and the store would
// have been asked to take a round for a run this adapter never ingested.
func TestAWebRunsUndeliveredAnswerDoesNotBuyItTheRoomsVoice(t *testing.T) {
	t.Parallel()
	rig := newBoundRoomRig(t)

	// The room asked something earlier and read its answer.
	rig.askedInTheRoom(t, "task-1")
	rig.ran(t, "REQ-1", 1, "task-1")
	rig.answer(t, "42", "task-1")

	// Now the installer asks the same session something in a browser. No
	// bubble — and the socket is down when the answer goes out.
	rig.askedInTheBrowser(t, "task-2")
	rig.senders.clear(rig.instID, rig.conn.sender)
	if err := rig.out.processEvent(context.Background(), events.Event{
		ChatSessionID: bubbleSession,
		TaskID:        taskUUID(t, "task-2"),
		Payload:       protocol.ChatDonePayload{Content: "the salary band for that role is 42k"},
	}); err != nil {
		t.Fatalf("a browser question's answer is refused at the gate, so nothing is attempted "+
			"and there is nothing to report: %v", err)
	}
	rig.senders.set(rig.instID, rig.conn.sender) // the socket comes back

	// Everything read up to here belongs to the answer path. What the failure
	// path asks on its own is the tail after this mark.
	beforeFailure := len(rig.q.originAsked())

	rig.failed(t, "task-2", false)

	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the room was told %q about a run nobody in it started — everyone in the chat "+
			"just learned that a question they never saw had gone wrong", got)
	}
	if asked := rig.q.originAsked()[beforeFailure:]; len(asked) != 1 || asked[0] != taskUUID(t, "task-2") {
		t.Fatalf("the failure path read the channel_ingested stamp for %v, want exactly [%s] — "+
			"a run with no round of this adapter's has to be decided by the row, "+
			"and this one skipped the check on evidence it manufactured for itself",
			asked, taskUUID(t, "task-2"))
	}
	// Two frames: the room's own bubble, opened and sealed by its own answer.
	if frames := rig.conn.streamFrames(t); len(frames) != 2 {
		t.Fatalf("the room's stream frames are %v, want the 2 its own round wrote", frames)
	}
}
