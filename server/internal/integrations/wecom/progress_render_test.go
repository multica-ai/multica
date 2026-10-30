package wecom

// progress_render_test.go — turning an agent's tool calls into a sentence a
// non-engineer can read, and the rolling window they land in.
//
// The property this file pins is that every tool the providers actually emit
// produces SOME line: a blank one is a step the user never sees happen, which
// is indistinguishable from a run that has stalled. What a line may say lives
// in progress_detail_test.go; who may read it, and which bubble it lands in,
// in progress_rounds_test.go.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// testClock drives the throttle without sleeping.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock { return &testClock{t: time.Now()} }

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func toolUse(tool string, input map[string]any) protocol.TaskMessagePayload {
	return protocol.TaskMessagePayload{Type: "tool_use", Tool: tool, Input: input}
}

// lineFor renders one task message the way the bubble would.
func lineFor(t *testing.T, msg protocol.TaskMessagePayload, loc Locale) string {
	t.Helper()
	step, ok := stepFromTaskMessage(msg)
	if !ok {
		t.Fatalf("%s/%s produced no step at all", msg.Type, msg.Tool)
	}
	return step.line(copyFor(loc), progressLevelDetail)
}

// TestEveryToolCallReadsAsAnAction — the mapping table, stated as what the
// user ends up reading.
func TestEveryToolCallReadsAsAnAction(t *testing.T) {
	cases := []struct {
		name string
		msg  protocol.TaskMessagePayload
		want string
	}{
		{"read names the file", toolUse("Read", map[string]any{"file_path": "/Users/dana/dev/server/config.go"}), "正在读取 /Users/dana/dev/server/config.go"},
		{"read without a path", toolUse("Read", nil), "正在读取文件"},
		{"codex read_file", toolUse("read_file", map[string]any{"path": "cmd/main.go"}), "正在读取 cmd/main.go"},
		{"edit names the file", toolUse("Edit", map[string]any{"file_path": "/srv/app/handler.go"}), "正在修改 /srv/app/handler.go"},
		{"write names the file", toolUse("Write", map[string]any{"file_path": "notes.md"}), "正在修改 notes.md"},
		{"codex patch_apply", toolUse("patch_apply", nil), "正在修改文件"},
		{"bash carries the command line", toolUse("Bash", map[string]any{"command": "git status --short"}), "正在执行 git status --short"},
		{"bash without a command", toolUse("Bash", nil), "正在执行命令"},
		{"grep", toolUse("Grep", map[string]any{"pattern": "token"}), "正在检索 token"},
		{"glob", toolUse("Glob", map[string]any{"pattern": "**/*.go"}), "正在检索 **/*.go"},
		{"search without a term", toolUse("Grep", nil), "正在检索代码"},
		{"webfetch", toolUse("WebFetch", map[string]any{"url": "https://example.com/x"}), "正在查 https://example.com/x"},
		{"websearch", toolUse("WebSearch", map[string]any{"query": "golang"}), "正在查 golang"},
		{"subagent", toolUse("Task", map[string]any{"description": "dig"}), "正在派子任务：dig"},
		{"todo", toolUse("TodoWrite", nil), "正在梳理计划"},
		{"unknown tool", toolUse("Frobnicate", nil), "正在使用 Frobnicate"},
		{"mcp tool", toolUse("mcp__calendar__list_events", nil), "正在调用 calendar · list_events"},
		{"error message", protocol.TaskMessagePayload{Type: "error", Content: "boom"}, "上一步出错了：boom，正在继续"},
		{"error with no message", protocol.TaskMessagePayload{Type: "error"}, "上一步出错了，正在继续"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lineFor(t, tc.msg, LocaleZhHans); got != tc.want {
				t.Errorf("line = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEnglishInstallationsReadEnglish — the pack is picked per installation,
// so a line built for one must not leak the other's language.
func TestEnglishInstallationsReadEnglish(t *testing.T) {
	got := lineFor(t, toolUse("Read", map[string]any{"file_path": "a/b/config.go"}), LocaleEn)
	if got != "Reading a/b/config.go" {
		t.Errorf("line = %q, want the English pack", got)
	}
	if strings.ContainsAny(got, "正在读取") {
		t.Errorf("line = %q still carries Chinese", got)
	}
}

// TestOnlyTheRunsOwnWorkRefreshesTheBubble — a tool_result is a content block
// and a chunk of text is the answer, which has its own closing frame.
// Rejecting both here is also what keeps the database read off the hot path:
// they are most of the volume.
func TestOnlyTheRunsOwnWorkRefreshesTheBubble(t *testing.T) {
	for _, msg := range []protocol.TaskMessagePayload{
		{Type: "tool_result", Tool: "Bash", Output: "ok"},
		{Type: "text", Content: "here is the answer"},
		{Type: "", Content: "x"},
	} {
		if _, ok := stepFromTaskMessage(msg); ok {
			t.Errorf("%q produced a progress step; it should not refresh the bubble", msg.Type)
		}
	}
	for _, msg := range []protocol.TaskMessagePayload{
		{Type: "tool_use", Tool: "Bash", Input: map[string]any{"command": "ls"}},
		{Type: "error", Content: "boom"},
		{Type: "thinking", Content: "hmm"},
	} {
		if _, ok := stepFromTaskMessage(msg); !ok {
			t.Errorf("%q produced no step; it is part of watching the run work", msg.Type)
		}
	}
}

// What a line may and may not carry lives in progress_detail_test.go — the
// argument is shown, the content block is not — and who may read it at all in
// progress_rounds_test.go.

// TestPayloadSurvivesASerializationRoundTrip — the same subscriber sees typed
// payloads in-process and maps once the event has been through JSON.
func TestPayloadSurvivesASerializationRoundTrip(t *testing.T) {
	raw := map[string]any{
		"type":  "tool_use",
		"tool":  "Read",
		"input": map[string]any{"file_path": "/x/y/config.go"},
	}
	step, ok := stepFromTaskMessage(raw)
	if !ok {
		t.Fatal("the map form produced no step")
	}
	if got := step.line(copyFor(LocaleZhHans), progressLevelDetail); got != "正在读取 /x/y/config.go" {
		t.Errorf("line = %q", got)
	}
}

// ---- the rolling feed ----

func newTestFeed(clock *testClock) *progressFeed {
	return &progressFeed{now: clock.now}
}

// TestTheBubbleAccumulatesSteps — the whole reason for this change: the user
// watches the list grow instead of staring at one frozen line.
func TestTheBubbleAccumulatesSteps(t *testing.T) {
	clock := newTestClock()
	feed := newTestFeed(clock)
	pack := copyFor(LocaleZhHans)
	opened := clock.now()

	first := feed.record(progressStep{kind: progressRead, arg: "a.go"}, pack, opened, progressLevelDetail)
	if !strings.Contains(first, "正在读取 a.go") {
		t.Fatalf("first frame = %q", first)
	}
	clock.advance(progressMinInterval)
	second := feed.record(progressStep{kind: progressCommand}, pack, opened, progressLevelDetail)
	if !strings.Contains(second, "正在读取 a.go") || !strings.Contains(second, "正在执行命令") {
		t.Errorf("second frame = %q, want both steps", second)
	}
	if !strings.HasPrefix(second, "<think>") || !strings.HasSuffix(second, "</think>") {
		t.Errorf("frame = %q, want the thinking wrapper so it does not read as the answer", second)
	}
}

// TestTheOldestStepScrollsOff — an hour-long run must not grow an unbounded
// bubble; a fixed window keeps the newest work visible.
func TestTheOldestStepScrollsOff(t *testing.T) {
	clock := newTestClock()
	feed := newTestFeed(clock)
	pack := copyFor(LocaleZhHans)
	opened := clock.now()

	// Deliberately not written in terms of progressMaxLines: a window stated
	// against itself passes at any size, including one that fills a phone
	// screen. Forty distinct steps, and what a chat bubble may show is a
	// number small enough to read at a glance.
	const pushes = 40
	const readableCeiling = 10

	var last string
	for i := 0; i < pushes; i++ {
		last = feed.record(progressStep{kind: progressTool, arg: fmt.Sprintf("step%03d", i)}, pack, opened, progressLevelDetail)
		clock.advance(progressMinInterval)
	}
	if strings.Contains(last, "step000") {
		t.Errorf("frame = %q, want the first of forty steps to have scrolled off", last)
	}
	if !strings.Contains(last, "step039") {
		t.Errorf("frame = %q, want the newest step", last)
	}
	if got := strings.Count(last, "\n· "); got > readableCeiling {
		t.Errorf("frame carries %d steps; a bubble nobody can read at a glance is not progress", got)
	}
	if got := strings.Count(last, "\n· "); got != progressMaxLines {
		t.Errorf("frame carries %d steps, want the full %d-line window", got, progressMaxLines)
	}
}

// TestRepeatedStepsCountInsteadOfRepeating — twenty greps in a row would
// otherwise push everything else out of the window with one sentence.
func TestRepeatedStepsCountInsteadOfRepeating(t *testing.T) {
	clock := newTestClock()
	feed := newTestFeed(clock)
	pack := copyFor(LocaleZhHans)
	opened := clock.now()

	var last string
	for i := 0; i < 3; i++ {
		last = feed.record(progressStep{kind: progressSearch}, pack, opened, progressLevelDetail)
		clock.advance(progressMinInterval)
	}
	if strings.Count(last, "正在检索代码") != 1 {
		t.Errorf("frame = %q, want one line for three identical steps", last)
	}
	if !strings.Contains(last, "×3") {
		t.Errorf("frame = %q, want the repeat count", last)
	}
}

// TestABurstOfStepsBecomesOneFrame — the throttle. Tool calls land several a
// second and every frame is a write on the bot's one socket.
func TestABurstOfStepsBecomesOneFrame(t *testing.T) {
	clock := newTestClock()
	feed := newTestFeed(clock)
	pack := copyFor(LocaleZhHans)
	opened := clock.now()

	if got := feed.record(progressStep{kind: progressRead, arg: "a.go"}, pack, opened, progressLevelDetail); got == "" {
		t.Fatal("the first step must reach the user immediately")
	}
	clock.advance(200 * time.Millisecond)
	if got := feed.record(progressStep{kind: progressRead, arg: "b.go"}, pack, opened, progressLevelDetail); got != "" {
		t.Errorf("a second frame %q went out inside the throttle window", got)
	}
	clock.advance(progressMinInterval)
	merged := feed.record(progressStep{kind: progressRead, arg: "c.go"}, pack, opened, progressLevelDetail)
	if merged == "" {
		t.Fatal("the throttle never released")
	}
	if !strings.Contains(merged, "b.go") {
		t.Errorf("frame = %q, want the throttled step folded into the next frame rather than lost", merged)
	}
}

// TestTheBubbleShowsHowLongItHasBeen — a spinner with no clock reads as stuck.
func TestTheBubbleShowsHowLongItHasBeen(t *testing.T) {
	clock := newTestClock()
	feed := newTestFeed(clock)
	opened := clock.now()
	clock.advance(23 * time.Second)

	frame := feed.record(progressStep{kind: progressSearch}, copyFor(LocaleZhHans), opened, progressLevelDetail)
	if !strings.Contains(frame, "已用时 23s") {
		t.Errorf("frame = %q, want the elapsed time", frame)
	}
}

func TestElapsedReadsTheSameAsTheWebPill(t *testing.T) {
	cases := map[time.Duration]string{
		0:                           "0s",
		7 * time.Second:             "7s",
		59 * time.Second:            "59s",
		60 * time.Second:            "1m",
		95 * time.Second:            "1m 35s",
		2*time.Hour + 3*time.Second: "120m 3s",
		-5 * time.Second:            "0s",
	}
	for d, want := range cases {
		if got := formatElapsed(d); got != want {
			t.Errorf("formatElapsed(%v) = %q, want %q", d, got, want)
		}
	}
}

// ---- the task → session cache ----

// cachedSessionID builds a distinct, valid UUID from one byte, for cache
// entries where the value only has to be comparable.
func cachedSessionID(n byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{n, n, n, n}, Valid: true}
}

// TestTheCacheAnswersTheSecondMessageForFree — a run posts dozens of tool
// messages and every one of them would otherwise be a database read.
func TestTheCacheAnswersTheSecondMessageForFree(t *testing.T) {
	clock := newTestClock()
	c := newTaskSessionCache()
	c.now = clock.now

	if _, hit := c.get("task-1"); hit {
		t.Fatal("empty cache reported a hit")
	}
	c.put("task-1", taskRound{session: cachedSessionID(9), round: "round-1"})
	got, hit := c.get("task-1")
	if !hit || got.session != cachedSessionID(9) {
		t.Fatalf("get = %v/%v, want the stored session", got, hit)
	}
	// The round travels with the session because both come off the one row —
	// without it a retry clone's steps would cost a second read each.
	if got.round != "round-1" {
		t.Errorf("get returned round %q, want %q", got.round, "round-1")
	}
}

// TestTheCacheRemembersTasksWithNoSession — an issue run publishes the same
// events and has no chat session at all. Without a negative entry every one of
// its messages re-asks the database the question it already answered.
func TestTheCacheRemembersTasksWithNoSession(t *testing.T) {
	c := newTaskSessionCache()
	c.put("issue-task", taskRound{})
	got, hit := c.get("issue-task")
	if !hit {
		t.Fatal("the negative entry was not remembered")
	}
	if got.session.Valid {
		t.Errorf("get = %v, want the recorded absence", got.session)
	}
}

func TestTheCacheForgetsStaleAndExcessEntries(t *testing.T) {
	clock := newTestClock()
	c := newTaskSessionCache()
	c.now = clock.now

	c.put("old", taskRound{session: cachedSessionID(1)})
	clock.advance(taskSessionTTL + time.Second)
	if _, hit := c.get("old"); hit {
		t.Error("an entry past its ttl was still served")
	}

	c2 := newTaskSessionCache()
	c2.max = 4
	for i := 0; i < 40; i++ {
		c2.put(string(rune('a'+i%26))+string(rune('0'+i/26)), taskRound{session: cachedSessionID(byte(i))})
	}
	if got := len(c2.byTask); got > 4 {
		t.Errorf("cache holds %d entries, over its %d cap", got, 4)
	}
}
