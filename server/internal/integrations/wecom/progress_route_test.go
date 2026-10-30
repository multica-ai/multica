package wecom

// progress_route_test.go — the doubles and helpers the progress tests share
// (the transcript a bubble reads back, a way to wait for the lane), and the
// single-replica tests of progress_route.go: the lane, the write guard on every
// frame a step writes, the watermark, and the relay's progress branch. The
// two-replica tests are progress_two_replica_db_test.go.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// fakeTranscript is task_message as ListTaskProgressSince sees it. Rows are
// filed the way the daemon's request files them — every kind, each with the
// next seq for its task — and read back BY TASK, past a seq, filtered to the
// kinds the query names, in seq order. A task with no rows reads as an empty
// slice, which is what a :many query returns.
type fakeTranscript struct {
	mu    sync.Mutex
	rows  map[string][]fakeTranscriptRow
	next  map[string]int32
	reads int
	// begun counts reads that have started, gated or not.
	begun int
	err   error
	// gate holds every read open until it is closed: a slow database.
	gate chan struct{}
}

type fakeTranscriptRow struct {
	seq     int32
	msgType string
	tool    string
	content string
	input   []byte
}

func newFakeTranscript() *fakeTranscript {
	return &fakeTranscript{rows: map[string][]fakeTranscriptRow{}, next: map[string]int32{}}
}

// file appends one message to a task's transcript and returns its seq.
func (f *fakeTranscript) file(t testing.TB, taskID, msgType, tool, content string, input map[string]any) int32 {
	t.Helper()
	var raw []byte
	if input != nil {
		var err error
		if raw, err = json.Marshal(input); err != nil {
			t.Fatalf("marshal transcript input: %v", err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next[taskID]++
	seq := f.next[taskID]
	f.rows[taskID] = append(f.rows[taskID], fakeTranscriptRow{seq: seq, msgType: msgType, tool: tool, content: content, input: raw})
	return seq
}

func (f *fakeTranscript) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

func (f *fakeTranscript) ListTaskProgressSince(_ context.Context, arg db.ListTaskProgressSinceParams) ([]db.ListTaskProgressSinceRow, error) {
	f.mu.Lock()
	gate := f.gate
	f.begun++
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.err != nil {
		return nil, f.err
	}
	out := []db.ListTaskProgressSinceRow{}
	for _, r := range f.rows[util.UUIDToString(arg.TaskID)] {
		if r.seq <= arg.AfterSeq {
			continue
		}
		switch r.msgType {
		case "tool_use", "thinking", "error": // the query's IN list
		default:
			continue
		}
		out = append(out, db.ListTaskProgressSinceRow{
			Seq:     r.seq,
			Type:    r.msgType,
			Tool:    pgtype.Text{String: r.tool, Valid: r.tool != ""},
			Content: pgtype.Text{String: r.content, Valid: r.content != ""},
			Input:   r.input,
		})
	}
	return out, nil
}

func (f *fakeOutboundQueries) ListTaskProgressSince(ctx context.Context, arg db.ListTaskProgressSinceParams) ([]db.ListTaskProgressSinceRow, error) {
	if f.transcript == nil {
		return []db.ListTaskProgressSinceRow{}, nil
	}
	return f.transcript.ListTaskProgressSince(ctx, arg)
}

// waitProgress blocks until the manager's lane has nothing queued or running,
// which is when everything a report was going to write has been written.
func waitProgress(t testing.TB, m *TypingIndicatorManager) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !m.progress.idle() {
		if time.Now().After(deadline) {
			t.Fatal("the progress lane never went idle")
		}
		time.Sleep(time.Millisecond)
	}
}

// ---- the lane: nothing runs on the request that reported ----

// The daemon posts a run's transcript one batch at a time, from one goroutine,
// and waits for each answer. Anything the subscriber does inline — a database
// read, a frame and its ack wait — is time added to every batch of every run.
// So the subscriber only classifies and queues, and the read and the write
// happen on the lane.
//
// REVERSE VERIFICATION: call m.processProgress inline in handleTaskMessage
// instead of m.progress.offer, and Publish waits on the held read.
func TestAReportReturnsBeforeItsBubbleIsWritten(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", "task-1")

	gate := make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate) }) })
	rig.q.transcript.mu.Lock()
	rig.q.transcript.gate = gate
	rig.q.transcript.mu.Unlock()

	id := taskUUID(t, "task-1")
	seq := rig.q.transcript.file(t, id, "tool_use", "Read", "", map[string]any{"file_path": "/srv/app/slow.go"})
	published := make(chan struct{})
	go func() {
		rig.bus.Publish(events.Event{Type: protocol.EventTaskMessage, TaskID: id,
			Payload: protocol.TaskMessagePayload{TaskID: id, Type: "tool_use", Tool: "Read", Seq: int(seq)}})
		close(published)
	}()
	select {
	case <-published:
	case <-time.After(2 * time.Second):
		release.Do(func() { close(gate) })
		<-published
		t.Fatal("publishing a task:message waited on the bubble's transcript read; " +
			"the daemon's request that reported it was held for as long as the database took")
	}

	release.Do(func() { close(gate) })
	waitProgress(t, rig.typing)
	if got := rig.refreshes(t); len(got) != 1 || !strings.Contains(got[0], "slow.go") {
		t.Errorf("once the read came back the step was not shown: %q", got)
	}
}

// The holder throttles before it reads. A burst of tool calls inside one
// refresh interval is one transcript read, not one per call: every report
// would otherwise be a database round trip on the holder for a frame the
// throttle then declines to write.
//
// REVERSE VERIFICATION: drop `feed.due()` from the read's condition in
// showProgress, and the burst reads three times.
func TestABurstOfReportsIsOneTranscriptRead(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", "task-1")

	for _, tool := range []string{"Read", "Grep", "Edit"} {
		rig.toolCall(t, "task-1", tool, map[string]any{"file_path": "/srv/" + tool + ".go", "pattern": tool})
	}
	if got := rig.q.transcript.readCount(); got != 1 {
		t.Errorf("three reports inside one refresh interval read the transcript %d times, want 1", got)
	}
	rig.tick()
	rig.toolCall(t, "task-1", "Bash", map[string]any{"command": "go build ./..."})
	if got := rig.q.transcript.readCount(); got != 2 {
		t.Errorf("a report past the interval did not read again (%d reads in all, want 2)", got)
	}
	if last := rig.lastRefresh(t); !strings.Contains(last, "Edit.go") || !strings.Contains(last, "go build") {
		t.Errorf("the frame after the interval is missing a step the throttle held back: %q", last)
	}
}

// A copy of a report already shown — a relay replay, a frame that overtook
// it, the same event twice — is settled on the watermark alone, before the
// throttle and before the database.
//
// REVERSE VERIFICATION: drop the `n.seq <= slot.folded` early return in
// showProgress, and the copy past the interval reads the transcript again.
func TestACopyOfAShownReportReadsNothing(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", "task-1")
	rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/srv/app/once.go"})
	reads, frames := rig.q.transcript.readCount(), len(rig.refreshes(t))

	rig.tick() // past the interval, so only the watermark can stop the read
	id := taskUUID(t, "task-1")
	rig.bus.Publish(events.Event{Type: protocol.EventTaskMessage, TaskID: id,
		Payload: protocol.TaskMessagePayload{TaskID: id, Type: "tool_use", Tool: "Read", Seq: 1}})
	waitProgress(t, rig.typing)

	if got := rig.q.transcript.readCount() - reads; got != 0 {
		t.Errorf("a copy of a report already shown read the transcript %d time(s)", got)
	}
	if got := len(rig.refreshes(t)) - frames; got != 0 {
		t.Errorf("a copy of a report already shown wrote %d frame(s)", got)
	}
}

// ---- every frame a step writes asks whether the bot may still write ----

// A revocation mid-run stops the steps at the next frame. The handle was
// captured when the bubble opened; nothing since has asked.
//
// REVERSE VERIFICATION: pass ctx instead of guardReply(ctx, …) to
// senders.stream in writeProgress, and the step is written.
func TestARevokedInstallationsBubbleShowsNoMoreSteps(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", "task-1")
	rig.q.installation.Status = "revoked"

	rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/srv/app/after-revocation.go"})

	if got := rig.refreshes(t); len(got) != 0 {
		t.Errorf("a revoked bot wrote %d step frame(s) into its bubble: %q", len(got), got)
	}
}

// revokedOnceRefused is the bot being revoked in the moment between the
// server refusing a refresh and the notice that follows it: active until the
// connection has taken a refresh, revoked from then on.
type revokedOnceRefused struct {
	*fakeOutboundQueries
	conn *bubbleConn
	t    *testing.T
}

func (r revokedOnceRefused) GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	inst, err := r.fakeOutboundQueries.GetChannelInstallation(ctx, arg)
	if err == nil && len(r.conn.streamFrames(r.t)) > 1 {
		inst.Status = "revoked"
	}
	return inst, err
}

// The "your bubble is stuck" notice is a message of its own, written after
// the refresh was refused, and it asks again too.
//
// REVERSE VERIFICATION: pass ctx instead of guardReply(ctx, …) to
// sendTextCtx in writeProgress, and the notice goes out on the revoked bot.
func TestARevokedInstallationIsNotToldItsBubbleIsStuck(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", "task-1")
	rig.conn.disownAfterFrames = 1 // the opening frame lands, the first refresh is refused
	rig.typing.deliveries = revokedOnceRefused{fakeOutboundQueries: rig.q, conn: rig.conn, t: t}

	rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/srv/app/router.go"})

	if got := len(rig.refreshes(t)); got != 1 {
		t.Fatalf("the refresh the server refuses was not written (%d refreshes); "+
			"this test cannot reach the notice without it", got)
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Errorf("the revoked bot sent %d message(s) about its stuck bubble: %v", len(pushes), pushes)
	}
}

// The nine-minute hand-over is two frames, the old bubble's closing line and
// the new bubble's opening one, and both are frames of a reply.
//
// REVERSE VERIFICATION: drop the guardReply line in fireGuard, and both are
// written on the revoked bot.
func TestARevokedInstallationGetsNoHandOver(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", "task-1")
	rig.stepped(t, "task-1")
	before := len(rig.conn.streamFrames(t))
	rig.q.installation.Status = "revoked"

	rig.typing.fireGuard(context.Background(), bubbleSessionID(t), rig.seqOf(t, "task-1"))

	if got := rig.conn.streamFrames(t)[before:]; len(got) != 0 {
		t.Errorf("the hand-over wrote %d frame(s) on a revoked bot: %v", len(got), got)
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Errorf("the hand-over sent %d message(s) on a revoked bot: %v", len(pushes), pushes)
	}
}

// ---- which runs have steps to show ----

// A question typed in Multica can hold a WeCom round for a moment —
// task:queued binds it, and the origin gate gives it back at the end — but it
// has no delivery row, and its steps are not the room's question's steps.
//
// REVERSE VERIFICATION: delete the return in progressTarget's ErrNoRows case
// for the delivery row, and the browser run's tool call is painted.
func TestARunTypedInMulticaShowsNoStepsInTheRoomsBubble(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.askFrom(t, "REQ-A", rig.chatType)
	id := taskUUID(t, "web-1")
	rig.q.perTaskDelivery = map[string]bool{id: false}
	rig.q.perTaskIngested = map[string]bool{id: false}
	rig.q.fileTask(t, id)
	rig.queueTask(t, id, "")
	rig.fileTask(t, "web-1", "web-1")
	// Anti-vacuity: the browser run really is holding the room's round.
	if slot, here := rig.streams.progressFor(bubbleSessionID(t), id, id, 0); !here || !slot.writable {
		t.Fatal("the browser run never took the round; nothing below can fail for the reason it names")
	}

	rig.toolCall(t, "web-1", "Read", map[string]any{"file_path": "/home/dana/salary-review.md"})

	if got := rig.refreshes(t); len(got) != 0 {
		t.Errorf("a run typed in Multica painted %d step(s) into the room's bubble: %q", len(got), got)
	}
}

// ---- the watermark: what has been shown is not shown again ----

// A rotation gives the run a new bubble and an empty list. What the old
// bubble showed is not replayed into the new one: the watermark lives on the
// round, not on the feed the rotation throws away.
//
// REVERSE VERIFICATION: reset e.folded alongside e.feed in streamStore.rotate,
// and the new bubble starts with the old bubble's steps.
func TestARotatedBubbleStartsWithOnlyTheStepsSinceTheRotation(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", "task-1")
	rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/srv/app/before-rotation.go"})
	_, next := rig.rotated(t, "task-1")
	rig.tick()
	rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/srv/app/after-rotation.go"})

	var last string
	for _, f := range rig.conn.streamFrames(t) {
		content, _ := f["content"].(string)
		if f["id"] == next && f["finish"] != true && content != streamThinkingPlaceholder {
			last = content
		}
	}
	if !strings.Contains(last, "after-rotation.go") {
		t.Fatalf("the new bubble never showed the step after the rotation: %q", last)
	}
	if strings.Contains(last, "before-rotation.go") {
		t.Errorf("the new bubble replayed a step the old one already showed: %q", last)
	}
}

// Two reads for one run can finish out of order. The slower one finishing
// second must not move the watermark back, or the next read shows its rows
// again.
//
// REVERSE VERIFICATION: drop the `seq > r.folded[taskID]` condition in
// markFolded, and the watermark goes back to 3.
func TestTheWatermarkNeverMovesBackwards(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", "task-1")
	session, id := bubbleSessionID(t), taskUUID(t, "task-1")

	rig.streams.markFolded(session, id, id, 5, false)
	rig.streams.markFolded(session, id, id, 3, false)

	slot, here := rig.streams.progressFor(session, id, id, 0)
	if !here {
		t.Fatal("the round is gone")
	}
	if slot.folded != 5 {
		t.Errorf("watermark = %d after folding through 5 and then 3, want 5", slot.folded)
	}
}

// The starting line is said once, and only before the first tool call: a
// replayed milestone is not a second line, and one that arrives after the
// transcript has started is not news.
//
// REVERSE VERIFICATION: drop `!slot.started` from showProgress's started
// condition and the replayed milestone adds a frame; drop `slot.folded == 0`
// and the late one does.
func TestTheStartingLineIsSaidOnceAndOnlyFirst(t *testing.T) {
	t.Parallel()
	t.Run("a replayed milestone", func(t *testing.T) {
		t.Parallel()
		rig := newProgressRig(t)
		rig.running(t, "REQ-A", "task-1")
		rig.milestone(t, "task-1", "Launching claude", 1)
		rig.tick()
		rig.milestone(t, "task-1", "Launching claude", 1)
		if got := rig.refreshes(t); len(got) != 1 {
			t.Errorf("a milestone delivered twice wrote %d frames, want 1: %q", len(got), got)
		}
	})
	t.Run("a milestone after the first tool call", func(t *testing.T) {
		t.Parallel()
		rig := newProgressRig(t)
		rig.running(t, "REQ-A", "task-1")
		rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/srv/app/first.go"})
		rig.tick()
		rig.milestone(t, "task-1", "Launching claude", 1)
		refreshes := rig.refreshes(t)
		if len(refreshes) != 1 {
			t.Errorf("a milestone after the first tool call wrote %d frames, want 1: %q", len(refreshes), refreshes)
		}
	})
}

// ---- a step in flight when the answer seals the bubble ----

// The lane reads the bubble's handle, then the transcript, then writes. The
// answer can seal the bubble in between. The step must then write nothing:
// a refresh after the closing frame repaints a spinner over the answer.
//
// REVERSE VERIFICATION: drop the isSealed check in
// wsSender.beginStreamFrameLocked, and the step lands after the answer.
func TestAStepInFlightWhenTheAnswerSealsTheBubbleWritesNothing(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", "task-1")
	stream := rig.streamIDOf(t, "task-1")

	gate := make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate) }) })
	rig.q.transcript.mu.Lock()
	rig.q.transcript.gate = gate
	rig.q.transcript.mu.Unlock()

	id := taskUUID(t, "task-1")
	seq := rig.q.transcript.file(t, id, "tool_use", "Read", "", map[string]any{"file_path": "/srv/app/late.go"})
	published := make(chan struct{})
	go func() {
		rig.bus.Publish(events.Event{Type: protocol.EventTaskMessage, TaskID: id,
			Payload: protocol.TaskMessagePayload{TaskID: id, Type: "tool_use", Tool: "Read", Seq: int(seq)}})
		close(published)
	}()
	// Wait until the lane holds the bubble's handle and is inside the read.
	deadline := time.Now().Add(5 * time.Second)
	for {
		rig.q.transcript.mu.Lock()
		begun := rig.q.transcript.begun
		rig.q.transcript.mu.Unlock()
		if begun > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the lane never started reading the transcript")
		}
		time.Sleep(time.Millisecond)
	}

	rig.answer(t, "here is the answer", "task-1")
	release.Do(func() { close(gate) })
	<-published
	waitProgress(t, rig.typing)

	closed := false
	for _, f := range rig.conn.streamFrames(t) {
		if f["id"] != stream {
			continue
		}
		if closed {
			t.Errorf("a frame was written to the bubble after its closing frame: %v", f)
		}
		if f["finish"] == true {
			closed = true
		}
	}
	if !closed {
		t.Fatal("the answer never sealed the bubble; nothing above can fail for the reason it names")
	}
}

// ---- the relay: a progress frame never takes a reply's place ----

type recordingProgressSink struct {
	mu     sync.Mutex
	frames []relayFrame
}

func (s *recordingProgressSink) acceptRelayedProgress(f relayFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames = append(s.frames, f)
}

func (s *recordingProgressSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames)
}

// A long run reports far faster than a bot's queue drains. If progress frames
// went through the dispatcher's queues they would fill them, and the answer
// behind them would be shed — the one frame the queues exist for.
//
// REVERSE VERIFICATION: drop the relayKindProgress branch in
// DeliverWecomOutbound, and the first progress frame takes the queue's one
// place and the answer is shed.
func TestProgressFramesNeverTakeAnAnswersPlaceInTheQueue(t *testing.T) {
	t.Parallel()
	relay := NewRelayOutbound(nil, nil, RelayConfig{Shards: 1, QueueDepth: 1}, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))
	metrics := newCountingMetrics()
	relay.SetMetrics(metrics)
	sink := &recordingProgressSink{}
	relay.attachProgress(sink)
	const inst = "11111111-1111-1111-1111-111111111111"

	for seq := int32(1); seq <= 50; seq++ {
		body, _ := json.Marshal(relayFrame{Kind: relayKindProgress, InstallationID: inst,
			SessionID: "22222222-2222-2222-2222-222222222222", TaskID: "33333333-3333-3333-3333-333333333333", Seq: seq})
		relay.DeliverWecomOutbound(inst, body, progressEventID(progressNudge{taskID: "t", seq: seq}))
	}
	body, _ := json.Marshal(relayFrame{Kind: relayKindReply, InstallationID: inst, ChatID: "chat", Content: "the answer",
		TaskID: "33333333-3333-3333-3333-333333333333"})
	relay.DeliverWecomOutbound(inst, body, "reply-1")

	if got := metrics.get("relay_shed"); got != 0 {
		t.Errorf("%d frame(s) were shed; the answer must never queue behind progress", got)
	}
	if got := len(relay.queues[0]); got != 1 {
		t.Fatalf("the queue holds %d frame(s), want exactly the answer", got)
	}
	if item := <-relay.queues[0]; item.frame.Kind != relayKindReply {
		t.Errorf("the queue's one place went to a %q frame, not the answer", item.frame.Kind)
	}
	if got := sink.count(); got != 50 {
		t.Errorf("the progress sink saw %d of 50 frames", got)
	}
}

// A replica whose boot block never attached the typing indicator still reads
// every progress frame, and drops them. That is a run on another replica
// showing no steps, with no symptom anywhere, so it is said — once, not once
// per frame.
//
// REVERSE VERIFICATION: drop the warning in deliverProgress and the first
// case fails; attach nothing in the second and it fails.
func TestARelayWithNoProgressSinkSaysSoOnce(t *testing.T) {
	t.Parallel()
	frame, _ := json.Marshal(relayFrame{Kind: relayKindProgress, InstallationID: "i", SessionID: "s", TaskID: "t", Seq: 1})
	const warning = "nothing here shows them"

	log, buf := capturingLogger()
	unattached := NewRelayOutbound(nil, nil, RelayConfig{}, log)
	for i := 0; i < 3; i++ {
		unattached.DeliverWecomOutbound("i", frame, "e")
	}
	if got := strings.Count(buf.String(), warning); got != 1 {
		t.Errorf("an unattached relay warned %d times over three progress frames, want 1:\n%s", got, buf.String())
	}

	log, buf = capturingLogger()
	attached := NewRelayOutbound(nil, nil, RelayConfig{}, log)
	attached.attachProgress(&recordingProgressSink{})
	attached.DeliverWecomOutbound("i", frame, "e")
	if strings.Contains(buf.String(), warning) {
		t.Errorf("a relay with a sink attached warned that nothing shows progress:\n%s", buf.String())
	}
}
