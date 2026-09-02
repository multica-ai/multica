package wecom

// progress_rounds_test.go — the bubble actually fills in while the run is
// going, and the steps land in the right one.
//
// Every other progress test in this package exercises a piece: the renderer
// turns a tool call into a sentence, the redactor keeps a token out of it, the
// feed folds a burst into one frame. All of that can be perfect and the user
// still watch a spinner for four minutes, because the pieces are joined by two
// bus subscriptions, and a subscription that is never registered fails
// silently — the events keep flowing, nobody reads them, and nothing anywhere
// says so.
//
// So these drive a real events.Bus from the publisher's side, the way the
// daemon's own handler does, and assert a stream frame carrying the rendered
// step reaches the socket. The boot path's half of the same question — that
// anything subscribes at all when the server actually starts — is
// cmd/server/wecom_progress_wiring_test.go.
//
// The other half of the file is which round a step belongs to. A chat session
// outlives its turns, the daemon flushes a transcript in arrears, and the
// nine-minute guard rotates a bubble while its run carries on — so a step for a
// round that is over really does arrive after the next question has opened a
// bubble of its own.

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// fakeIdentityLookup answers "is the person in this chat the bot's principal",
// which is what decides whether their bubble may show the run's steps at all.
type fakeIdentityLookup struct {
	binding db.ChannelUserBinding
	err     error
	calls   int
}

func (f *fakeIdentityLookup) GetChannelUserBindingByUserID(context.Context, db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	f.calls++
	if f.err != nil {
		return db.ChannelUserBinding{}, f.err
	}
	return f.binding, nil
}

// progressRig is a bubbleRig whose asker is the bot's principal, so the steps
// are on the tier that shows them, with the manager registered on a real bus.
type progressRig struct {
	*bubbleRig
	identities *fakeIdentityLookup
	// chatType is where the questions come from. A one-to-one by default,
	// because that is the only place a step is ever shown.
	chatType channel.ChatType
}

func newProgressRig(t *testing.T) *progressRig {
	t.Helper()
	rig := newBubbleRig(t)

	principal := pgtype.UUID{Bytes: [16]byte{7, 7, 7}, Valid: true}
	rig.installer = principal
	identities := &fakeIdentityLookup{binding: db.ChannelUserBinding{MulticaUserID: principal}}

	rig.typing = NewTypingIndicator(TypingIndicatorConfig{
		Senders:    rig.typing.senders,
		Streams:    rig.streams,
		Tasks:      rig.q,
		Identities: identities,
		// No guard: these tests fire it themselves, at the moment they mean to.
		GuardAfter: -1,
	})
	rig.bus = events.New()
	rig.typing.Register(rig.bus)
	return &progressRig{bubbleRig: rig, identities: identities, chatType: channel.ChatTypeP2P}
}

// running is one whole round starting: the message arrives, the debounced
// flush creates its run, and the task row that names the run's chat session is
// on file — which is what a transcript event is resolved through, since
// task:message carries a task id and nothing else.
func (p *progressRig) running(t *testing.T, reqID string, batch engine.RunBatchID, taskName string) {
	t.Helper()
	p.askFrom(t, reqID, batch, p.chatType)
	p.runStarted(t, batch, taskName)
	p.fileTask(t, taskName, taskName)
}

// fileTask puts a task row on file: the chat session it belongs to, and the
// round it is part of. chat_input_task_id is the task's own id for a first
// attempt and its PARENT's for an auto-retry clone, which is how a clone's
// steps find the bubble its first attempt opened.
func (p *progressRig) fileTask(t *testing.T, taskName, roundName string) {
	t.Helper()
	id := mustParseTestUUID(t, taskName)
	p.q.tasks[util.UUIDToString(id)] = db.AgentTaskQueue{
		ID:              id,
		ChatSessionID:   bubbleSessionID(t),
		ChatInputTaskID: mustParseTestUUID(t, roundName),
	}
}

// toolCall publishes one tool call the way the daemon's transcript flush does.
func (p *progressRig) toolCall(t *testing.T, taskName, tool string, input map[string]any) {
	t.Helper()
	id := taskUUID(t, taskName)
	p.bus.Publish(events.Event{
		Type:   protocol.EventTaskMessage,
		TaskID: id,
		Payload: protocol.TaskMessagePayload{
			TaskID: id, Type: "tool_use", Tool: tool, Input: input,
		},
	})
}

// milestone publishes one of the daemon's own two progress lines.
func (p *progressRig) milestone(t *testing.T, taskName, summary string) {
	t.Helper()
	id := taskUUID(t, taskName)
	p.bus.Publish(events.Event{
		Type:    protocol.EventTaskProgress,
		TaskID:  id,
		Payload: protocol.TaskProgressPayload{TaskID: id, Summary: summary},
	})
}

// think publishes one increment of the agent's reasoning, the way the daemon's
// transcript flush does.
func (p *progressRig) think(t *testing.T, taskName, content string) {
	t.Helper()
	id := taskUUID(t, taskName)
	p.bus.Publish(events.Event{
		Type:    protocol.EventTaskMessage,
		TaskID:  id,
		Payload: protocol.TaskMessagePayload{TaskID: id, Type: "thinking", Content: content},
	})
}

// lastRefresh returns the body of the newest in-flight refresh frame.
func (p *progressRig) lastRefresh(t *testing.T) string {
	t.Helper()
	refreshes := p.refreshes(t)
	if len(refreshes) == 0 {
		t.Fatal("no in-flight refresh frame was written at all")
	}
	return refreshes[len(refreshes)-1]
}

// textBetween returns what sits between the first occurrence of left and the
// first occurrence of right after it — the seam itself.
func textBetween(body, left, right string) (string, bool) {
	i := strings.Index(body, left)
	if i < 0 {
		return "", false
	}
	rest := body[i+len(left):]
	j := strings.Index(rest, right)
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}

// refreshes returns the in-flight refresh frames — not the opening frame,
// which carries the placeholder, and not a closing one. They are the whole
// feature.
func (p *progressRig) refreshes(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, f := range p.conn.streamFrames(t) {
		content, _ := f["content"].(string)
		if f["finish"] == true || content == streamThinkingPlaceholder {
			continue
		}
		out = append(out, content)
	}
	return out
}

// pushText reads the words out of an aibot_send_msg body — the "as a new
// message" path, which ships as markdown (sendMsgTextBody).
func pushText(body map[string]any) string {
	md, _ := body["markdown"].(map[string]any)
	text, _ := md["content"].(string)
	return text
}

// tick moves the clock past the refresh interval, so the next step is worth a
// frame of its own rather than being folded into the last one.
func (p *progressRig) tick() {
	p.now = p.now.Add(progressMinInterval * 2)
}

// ---- the feature ----

// task:message is the transcript — one event per tool call — and the only
// signal fine-grained enough to say what the agent is doing right now. The
// whole path has to work for one line to appear: subscription, task row, tier
// check, feed, frame.
func TestATaskMessagePaintsTheToolCallIntoTheBubble(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", 1, "task-1")

	rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/srv/app/router.go"})

	refreshes := rig.refreshes(t)
	if len(refreshes) != 1 {
		t.Fatalf("a task:message tool call produced %d in-flight refresh frames, want 1 — "+
			"the bubble opens and closes with a spinner and nothing in between, which is what "+
			"a user sees for a whole long run", len(refreshes))
	}
	if !strings.Contains(refreshes[0], "/srv/app/router.go") {
		t.Errorf("the refresh does not name what the agent read: %q", refreshes[0])
	}
	if !strings.HasPrefix(refreshes[0], "<think>") {
		t.Errorf("the step list is not inside a think block (%q); it would read as the bot's answer", refreshes[0])
	}
	if !strings.Contains(refreshes[0], copyFor(DefaultLocale).StreamProgressPrefix) {
		t.Errorf("the refresh is missing the progress heading %q: %q",
			copyFor(DefaultLocale).StreamProgressPrefix, refreshes[0])
	}
	if rig.streams.depth() != 1 {
		t.Error("the refresh consumed the round's bubble; the answer would arrive as a loose message under a spinner")
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Errorf("progress went out as %d plain message(s); a refresh belongs in the bubble, not underneath it", len(pushes))
	}
}

// task:progress fires exactly twice per run and both lines are the daemon's
// own, so on its own it leaves the whole middle of a run blank — but those two
// are the only thing said before the first tool call, which on a slow start is
// the longest silence there is.
//
// A milestone REWRITES the round's bubble: it goes into the open one and
// leaves it open. The two ways to get that wrong both leave the user worse off
// than the blank spinner did — sealing the bubble takes the answer's only
// destination away, and pushing the line as a plain message puts a status
// update in the conversation, one per milestone, underneath a spinner that is
// still turning.
func TestATaskProgressEventRewritesTheOpenBubble(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", 1, "task-1")

	rig.milestone(t, "task-1", "Launching claude")

	refreshes := rig.refreshes(t)
	if len(refreshes) != 1 {
		t.Fatalf("a task:progress event produced %d refresh frames, want 1", len(refreshes))
	}
	if !strings.Contains(refreshes[0], "Launching claude") {
		t.Errorf("the refresh does not carry the milestone: %q", refreshes[0])
	}
	if !strings.HasPrefix(refreshes[0], "<think>") {
		t.Errorf("the milestone is not inside a think block (%q); it would read as the bot's answer", refreshes[0])
	}
	if !strings.Contains(refreshes[0], copyFor(DefaultLocale).StreamProgressPrefix) {
		t.Errorf("the refresh is missing the progress heading %q: %q",
			copyFor(DefaultLocale).StreamProgressPrefix, refreshes[0])
	}
	if rig.streams.depth() != 1 {
		t.Error("the milestone consumed the round's bubble; the answer would arrive as a loose message under a spinner that nothing can now clear")
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Errorf("a milestone went out as %d plain message(s); it belongs in the bubble, not underneath it", len(pushes))
	}
}

// A run's dozens of transcript messages must not each put a database read on
// the daemon's own HTTP request. Neither the session nor the round a task
// belongs to ever changes, so they are asked for once, together, off one row.
func TestTheRoundBehindARunIsReadOnce(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", 1, "task-1")

	before := rig.q.taskGets
	for i := 0; i < 5; i++ {
		rig.toolCall(t, "task-1", "Bash", map[string]any{"command": "go test ./..."})
		rig.tick()
	}
	if got := rig.q.taskGets - before; got != 1 {
		t.Errorf("the task row was read %d times for one run's transcript, want 1 — "+
			"a read per tool call lands on the daemon's own request", got)
	}
}

// An issue or autopilot run publishes exactly the same events and has no chat
// session at all. On most deployments it is the commonest case, so it must
// cost one lookup and then nothing.
func TestARunWithNoChatSessionWritesNothingAndIsRememberedAsSuch(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", 1, "task-1")
	// The issue run's row: no chat session.
	id := mustParseTestUUID(t, "issue-run")
	rig.q.tasks[util.UUIDToString(id)] = db.AgentTaskQueue{ID: id, ChatInputTaskID: id}

	before := rig.q.taskGets
	for i := 0; i < 3; i++ {
		rig.toolCall(t, "issue-run", "Read", map[string]any{"file_path": "/etc/secrets.env"})
		rig.tick()
	}
	if got := len(rig.refreshes(t)); got != 0 {
		t.Errorf("an issue run wrote %d frame(s) into somebody's chat bubble", got)
	}
	if got := rig.q.taskGets - before; got != 1 {
		t.Errorf("the task row was read %d times for a run with no chat session, want 1", got)
	}
}

// A read that fails is not an answer, but re-asking is worse: the rest of the
// batch would each put another round trip on a database that has just shown it
// cannot serve them.
func TestADatabaseThatCannotAnswerIsNotAskedAgainImmediately(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", 1, "task-1")
	rig.q.taskErr = pgx.ErrTxClosed

	before := rig.q.taskGets
	for i := 0; i < 4; i++ {
		rig.toolCall(t, "task-2", "Read", map[string]any{"file_path": "/a.go"})
		rig.tick()
	}
	if got := rig.q.taskGets - before; got != 1 {
		t.Errorf("a failing task lookup was retried %d times inside one batch, want 1", got)
	}
}

// The refresh interval folds a burst of tool calls into one frame — a bot that
// wrote one frame per tool call would spend its socket on frames nobody can
// read that fast — and the steps that were folded in are still in the list
// when the next frame goes out.
func TestABurstOfToolCallsBecomesOneFrame(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", 1, "task-1")

	for _, tool := range []string{"Read", "Grep", "Edit"} {
		rig.toolCall(t, "task-1", tool, map[string]any{"file_path": "/srv/" + tool + ".go", "pattern": tool})
	}
	if got := len(rig.refreshes(t)); got != 1 {
		t.Fatalf("three tool calls inside the refresh interval produced %d frames, want 1", got)
	}

	rig.tick()
	rig.toolCall(t, "task-1", "Bash", map[string]any{"command": "go build ./..."})
	refreshes := rig.refreshes(t)
	if len(refreshes) != 2 {
		t.Fatalf("a step past the refresh interval produced %d frames in total, want 2", len(refreshes))
	}
	if !strings.Contains(refreshes[1], "go build") {
		t.Errorf("the second frame is missing the step that triggered it: %q", refreshes[1])
	}
	if !strings.Contains(refreshes[1], "Edit.go") {
		t.Errorf("the second frame dropped the steps folded into the first: %q", refreshes[1])
	}
}

// ---- the seam between two reasoning increments ----
//
// The agent's reasoning reaches the bubble as 500ms increments of one
// continuous stream, cut wherever the batching fell rather than at a sentence
// or even at a word. The buffer is joined by plain concatenation, so whatever
// whitespace sits at a seam is all that keeps the two sides apart. These two
// guard the joined text as the person in WeCom reads it, across a flush
// boundary — which is why they go through the bus and the round rather than
// through the feed directly: the trimming that welds them is on the way in,
// and a feed-level test that hands the increments over already joined cannot
// see it.

// A flush that falls between two words must not weld them together:
// "再看router.go" where the agent wrote "再看 router.go". It is unreadable in
// the small, and worse in mixed script, where the fused pair reads as one
// unfamiliar token rather than as two words.
func TestReasoningCutMidSentenceDoesNotWeldTwoWordsTogether(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", 1, "task-1")

	const head = "先看 handler.go 里的分支，再看 "
	const tail = "router.go 的注册顺序。"

	rig.think(t, "task-1", head)
	rig.tick()
	rig.think(t, "task-1", tail)

	body := rig.lastRefresh(t)
	if !strings.Contains(body, "里的分支") || !strings.Contains(body, "的注册顺序") {
		t.Fatalf("frame = %q lost the reasoning on one side of the seam", body)
	}
	gap, ok := textBetween(body, "再看", "router.go")
	if !ok {
		t.Fatalf("frame = %q does not carry both halves of the sentence", body)
	}
	if strings.TrimSpace(gap) != "" {
		t.Fatalf("the two markers matched the wrong place; %q sits between them:\n%s", gap, body)
	}
	if gap == "" {
		t.Errorf("the seam between two thinking increments lost its space: the bubble reads %q where the agent wrote %q.\n"+
			"Every 500ms boundary that falls between two words welds them into one.\nwhole frame:\n%s",
			"再看router.go", "再看 router.go", body)
	}
}

// A reasoning delta announces a new block by opening with a blank line,
// because concatenation is how the buffer is built. That blank line is leading
// whitespace of its increment, and it is the only thing marking the break.
func TestTwoReasoningBlocksDoNotRunTogetherAcrossAFlush(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", 1, "task-1")

	const first = "先确认 handler.go 里的分支。"
	const second = "\n\n再决定要不要改 router.go 的注册顺序。"

	rig.think(t, "task-1", first)
	rig.tick()
	rig.think(t, "task-1", second)

	body := rig.lastRefresh(t)
	if !strings.Contains(body, "里的分支") || !strings.Contains(body, "的注册顺序") {
		t.Fatalf("frame = %q lost one of the two reasoning blocks", body)
	}
	gap, ok := textBetween(body, "里的分支。", "再决定")
	if !ok {
		t.Fatalf("frame = %q does not carry both reasoning blocks", body)
	}
	if strings.TrimSpace(gap) != "" {
		t.Fatalf("the two markers matched the wrong place; %q sits between them:\n%s", gap, body)
	}
	if !strings.Contains(gap, "\n") {
		t.Errorf("the break between two reasoning blocks was dropped at the flush boundary: they are separated by %q, "+
			"so the bubble runs them together as one paragraph.\nThe agent sent a blank line between them.\nwhole frame:\n%s", gap, body)
	}
}

// An auto-retry clone gets a fresh task id and inherits its parent's
// chat_input_task_id, which is the id the flush bound the round under. Without
// resolving that, the steps simply stop the moment a run is retried — the
// bubble freezes on whatever the first attempt had reached and stays there for
// the rest of the run.
func TestARetryClonesStepsLandInTheRoundItsParentOpened(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", 1, "task-1")
	// FailTask's clone: its own id, its parent's round.
	rig.fileTask(t, "retry", "task-1")

	rig.toolCall(t, "retry", "Read", map[string]any{"file_path": "/srv/app/retry.go"})

	refreshes := rig.refreshes(t)
	if len(refreshes) != 1 {
		t.Fatalf("a retry clone's tool call produced %d refresh frames, want 1 — "+
			"the bubble stops updating the moment a run is retried", len(refreshes))
	}
	if !strings.Contains(refreshes[0], "retry.go") {
		t.Errorf("the refresh does not carry the clone's step: %q", refreshes[0])
	}
}

// ---- who may read a step at all ----

// A step can carry a file path, a search term, a command line. Outside the
// principal's own one-to-one chat it produces nothing: the WeCom session key
// for a room is the room, so every member reads the same bubble, and a
// colleague's own chat is somebody else's private conversation. WeCom has no
// unsend and the tenant archives the message, so being wrong here is not
// recoverable.
func TestAChatThatIsNotThePrincipalsShowsNoSteps(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		who   string
		setUp func(rig *progressRig)
	}{
		{
			name:  "a group, where the whole room reads the bubble",
			who:   "a room",
			setUp: func(rig *progressRig) { rig.chatType = channel.ChatTypeGroup },
		},
		{
			name: "a colleague's own chat with the principal's bot",
			who:  "a colleague",
			setUp: func(rig *progressRig) {
				rig.identities.binding = db.ChannelUserBinding{
					MulticaUserID: pgtype.UUID{Bytes: [16]byte{9, 9, 9}, Valid: true},
				}
			},
		},
		{
			name:  "a sender nobody can identify",
			who:   "an unknown sender",
			setUp: func(rig *progressRig) { rig.identities.err = pgx.ErrNoRows },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rig := newProgressRig(t)
			tc.setUp(rig)
			rig.running(t, "REQ-A", 1, "task-1")

			rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/home/dana/salary-review.md"})

			if got := rig.refreshes(t); len(got) != 0 {
				t.Errorf("%s was shown %d step frame(s): %q\n"+
					"A file path in a bubble that is not the principal's own cannot be unsent, "+
					"and the tenant's archive keeps it.", tc.who, len(got), got[0])
			}
		})
	}
}

// ---- decision 3: the audience is decided again for every round ----

// levelFor's inputs can change between two questions in the same chat: a
// binding is revoked, or re-pointed at a different person, and re-installing
// the bot moves the principal. An answer cached for the session would keep
// showing one person's file paths and search terms to whoever inherited the
// chat, for as long as the session lasted.
func TestTheAudienceIsDecidedAgainForEveryRound(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)

	rig.running(t, "REQ-A", 1, "task-1")
	rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/home/dana/salary-review.md"})
	if got := len(rig.refreshes(t)); got != 1 {
		t.Fatalf("the principal's own round showed %d steps, want 1 — "+
			"the rest of this test cannot mean anything if nothing is shown to begin with", got)
	}
	rig.answer(t, "done", "task-1")

	// Between the two questions the binding is revoked. Everything else about
	// the session is unchanged: same chat, same sender id, same installation.
	rig.identities.err = pgx.ErrNoRows

	rig.tick()
	before := len(rig.refreshes(t))
	asked := rig.identities.calls
	rig.running(t, "REQ-B", 2, "task-2")
	rig.toolCall(t, "task-2", "Read", map[string]any{"file_path": "/home/dana/salary-review.md"})

	if rig.identities.calls == asked {
		t.Error("the second round never asked who was on the other end of the chat; " +
			"its audience was inherited from the round before it")
	}
	after := rig.refreshes(t)
	if len(after) != before {
		t.Errorf("the round AFTER the binding was revoked still showed %d step frame(s):\n  %q\n"+
			"The bubble's audience was decided once for the session and reused, so whoever holds "+
			"this chat now keeps reading the principal's file paths — in a chat WeCom cannot unsend.",
			len(after)-before, after[len(after)-1])
	}
}

// ---- decisions 1 and 2: a stream that is over gets no more frames ----

// The nine-minute guard seals the bubble that is about to expire and opens a
// fresh one for the run to carry on in. The steps that follow belong to the
// new bubble and only there: a refresh on the sealed stream would be refused
// by the server, or worse, repaint a status over the hand-over line.
//
// REVERSE VERIFICATION: make streamStore.rotate leave e.handle as it was (drop
// the `e.handle = next` line) and this fails with every refresh addressed to
// the sealed stream.
func TestASealedBubbleIsNotRefreshed(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", 1, "task-1")
	// A second question, with a run of its own, so the store is not empty and
	// the cheap "nothing open" rejection cannot be what makes this pass.
	rig.running(t, "REQ-B", 2, "task-2")

	old, next := rig.rotated(t, 1)
	before := len(rig.conn.streamFrames(t))
	beforePushes := len(rig.conn.pushes(t))

	for i := 0; i < 5; i++ {
		rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/srv/app/still-running.go"})
		rig.tick()
	}

	after := rig.conn.streamFrames(t)[before:]
	if len(after) == 0 {
		t.Fatal("a rotated round's run wrote no refresh frame at all; the new bubble spins with nothing in it")
	}
	for _, f := range after {
		if f["id"] == old {
			t.Errorf("a refresh was written to the sealed stream %s: %v", old, f)
		}
		if f["id"] != next {
			t.Errorf("a refresh was written to stream %v, want the rotated bubble %s", f["id"], next)
		}
	}
	if got := len(rig.conn.pushes(t)) - beforePushes; got != 0 {
		t.Errorf("a rotated round's steps went out as %d plain message(s). A step is not an "+
			"ending: chasing an address for one turns a status line into a chat message, once per "+
			"1.5s, for the rest of the run.", got)
	}
}

// The trailing steps of a round that is over must not be painted into the
// bubble the NEXT question opened. The daemon flushes a transcript in arrears
// and the debounced flush names a round's run about three seconds after the
// message arrives, so there is a real window in which the new bubble has no
// run of its own yet and the old run is still talking.
//
// Every way a round's first bubble can be over is checked here, because the
// answer is the same one and for the same reason — a step is a write into a
// bubble, and the next question's bubble is not this run's. A finished run
// gets no frame at all; a rotated run's frame goes to its own new bubble.
func TestAFinishedRunsStepsNeverLandInTheNextQuestionsBubble(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		end  func(t *testing.T, rig *progressRig)
		// stillOpen says the round carries on in a bubble of its own after
		// end, so a refresh is expected — just not in the next question's.
		stillOpen bool
	}{
		{"answered", func(t *testing.T, rig *progressRig) { rig.answer(t, "all done", "task-1") }, false},
		{"rotated", func(t *testing.T, rig *progressRig) { rig.rotated(t, 1) }, true},
		{"cancelled", func(t *testing.T, rig *progressRig) { rig.cancelled(t, "task-1") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rig := newProgressRig(t)
			rig.running(t, "REQ-A", 1, "task-1")
			tc.end(t, rig)

			// The next question. Its bubble is painted immediately; the flush
			// that names its run has not happened yet.
			rig.ask(t, "REQ-B", 2)
			nextQuestion := rig.streamIDOf(t, 2)
			before := len(rig.conn.streamFrames(t))

			rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/srv/app/last-round.go"})

			after := rig.conn.streamFrames(t)[before:]
			for _, f := range after {
				if f["id"] == nextQuestion {
					t.Errorf("the previous round's tool call was painted into the new question's bubble: %q\n"+
						"The user asked something else and is watching the previous run's work reported as "+
						"the answer to it — and the new round's own run is then locked out of its own bubble.",
						f["content"])
				}
			}
			if !tc.stillOpen && len(after) != 0 {
				t.Errorf("a finished round's tool call wrote %d frame(s): %v", len(after), after)
			}
			if tc.stillOpen && len(after) == 0 {
				t.Errorf("a rotated round's tool call wrote nothing; its own new bubble spins empty")
			}
		})
	}
}

// ---- decision 4: a disowned bubble stops refreshing ----

// When another replica or a reconnect takes over a conversation, WeCom refuses
// every write to the old stream (846605 / 846608). Without refreshes that costs
// one refusal at the end of the run. WITH refreshes it costs one every 1.5s —
// roughly 400 on a ten-minute run, every one of them refused. What those 400
// calls spend is not something we can state: WeCom publishes one message
// frequency limit, per application per member (rate_limit.go), and names no
// separate figure for a bot's stream frames. That is a reason not to spend it,
// not a reason to read it as free.
//
// So the first refusal is the last one: the round is marked, the user is told
// once where the rest of the round will appear, and nothing writes to that
// stream again.
func TestADisownedBubbleStopsRefreshingAfterTheFirstRefusal(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", 1, "task-1")
	rig.conn.disownAfterFrames = 1 // the opening frame lands; the takeover happens right after

	const steps = 40
	for i := 0; i < steps; i++ {
		rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/srv/app/router.go"})
		rig.tick()
	}

	if got := len(rig.refreshes(t)); got != 1 {
		t.Fatalf("%d of %d steps were written to a stream the server had already disowned, want 1. "+
			"Every one is a refusal, and WeCom's rate limit is per BOT — a single lost bubble in one "+
			"chat throttles messages for every other user of this bot.", got, steps)
	}
	pushes := rig.conn.pushes(t)
	if len(pushes) != 1 {
		t.Fatalf("a disowned bubble produced %d plain message(s), want exactly 1: the user is "+
			"watching a spinner nothing can ever clear and has to be told once where the rest of "+
			"this round is going to appear", len(pushes))
	}
	if text := pushText(pushes[0]); !strings.Contains(text, copyFor(DefaultLocale).StreamStuck) {
		t.Errorf("the message about the stuck bubble says %q, want %q",
			text, copyFor(DefaultLocale).StreamStuck)
	}
}

// A bubble the server has disowned still belongs to a round, and that round
// still has to end in words. They go out as a new message: the frame would only
// buy one more refusal, and the addressing captured at ingest is what puts the
// answer in the chat that asked.
func TestADisownedBubblesAnswerArrivesAsANewMessage(t *testing.T) {
	t.Parallel()
	rig := newProgressRig(t)
	rig.running(t, "REQ-A", 1, "task-1")
	rig.conn.disownAfterFrames = 1 // the opening frame lands; the takeover happens right after
	rig.toolCall(t, "task-1", "Read", map[string]any{"file_path": "/srv/app/router.go"})

	framesBefore := len(rig.conn.streamFrames(t))
	rig.answer(t, "here is the answer", "task-1")

	if got := len(rig.conn.streamFrames(t)) - framesBefore; got != 0 {
		t.Errorf("the answer was written to a disowned stream as %d frame(s); it would be refused "+
			"and the user would never see it", got)
	}
	var delivered bool
	for _, p := range rig.conn.pushes(t) {
		if strings.Contains(pushText(p), "here is the answer") {
			delivered = true
		}
	}
	if !delivered {
		t.Error("the answer never reached the chat at all: its bubble could not be sealed and " +
			"nothing sent it as a message instead")
	}
}
