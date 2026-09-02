package wecom

// progress_thinking_test.go — the agent's own reasoning, in the bubble.
//
// Thinking is unlike every other thing that reaches this bubble. It arrives as
// increments rather than events, it has no natural end, it is the only text in
// the frame the agent wrote as prose, and it is long. So it gets a rolling
// tail of its own instead of a line in the step list, and it is the one place
// an injected </think> could close the wrapper the whole body is built from.

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func thinking(content string) protocol.TaskMessagePayload {
	return protocol.TaskMessagePayload{Type: "thinking", Content: content}
}

// thinkingFrame plays a series of thinking increments into a fresh feed and
// returns the last frame it produced.
func thinkingFrame(t *testing.T, chunks ...string) string {
	t.Helper()
	clock := newTestClock()
	feed := newTestFeed(clock)
	pack := copyFor(LocaleZhHans)
	opened := clock.now()

	var last string
	for _, c := range chunks {
		step, ok := stepFromTaskMessage(thinking(c))
		if !ok {
			t.Fatalf("thinking %q produced no step", c)
		}
		if frame := feed.record(step, pack, opened, progressLevelDetail); frame != "" {
			last = frame
		}
		clock.advance(progressMinInterval)
	}
	return last
}

// TestTheBubbleShowsWhatTheAgentIsThinking — the point of the change. The
// steps say what it touched; this says why.
func TestTheBubbleShowsWhatTheAgentIsThinking(t *testing.T) {
	frame := thinkingFrame(t, "先看一下 handler.go 里的分支，", "再决定要不要改 router。")
	if !strings.Contains(frame, "先看一下 handler.go 里的分支") {
		t.Errorf("frame = %q, want the thinking in it", frame)
	}
	if !strings.Contains(frame, "再决定要不要改 router") {
		t.Errorf("frame = %q, want the later increment too — thinking arrives in pieces", frame)
	}
}

// TestThinkingRollsToItsTail — a run thinks for minutes and the bubble is one
// message. Keeping the newest stretch is what makes it readable; keeping all
// of it is what makes the server refuse the frame.
func TestThinkingRollsToItsTail(t *testing.T) {
	frame := thinkingFrame(t,
		"OLDEST-MARKER "+strings.Repeat("甲", progressThinkingRunes),
		strings.Repeat("乙", progressThinkingRunes)+" NEWEST-MARKER",
	)
	if strings.Contains(frame, "OLDEST-MARKER") {
		t.Error("frame still carries the oldest thinking; the tail must roll")
	}
	if !strings.Contains(frame, "NEWEST-MARKER") {
		t.Errorf("frame = %.200q…, want the newest thinking", frame)
	}
	if n := len([]rune(frame)); n > progressThinkingRunes+200 {
		t.Errorf("frame is %d runes; the tail is not bounded", n)
	}
	if !utf8.ValidString(frame) {
		t.Error("the tail was cut mid-character")
	}
	if !strings.Contains(frame, "…") {
		t.Error("frame does not say the thinking was cut short")
	}
}

// TestThinkingCannotCloseTheThinkWrapper is the injection case. The body IS a
// <think> block, and this is the one text in it the agent wrote freely — a
// reply reasoning about this very feature contains the literal.
func TestThinkingCannotCloseTheThinkWrapper(t *testing.T) {
	frame := thinkingFrame(t, "气泡是 <think>…</think> 画出来的，所以 </think> 要处理掉")

	if n := strings.Count(frame, "</think>"); n != 1 {
		t.Errorf("frame carries %d closing tags, want exactly the wrapper's own:\n%s", n, frame)
	}
	if n := strings.Count(frame, "<think>"); n != 1 {
		t.Errorf("frame carries %d opening tags, want exactly the wrapper's own:\n%s", n, frame)
	}
	if !strings.HasPrefix(frame, "<think>") || !strings.HasSuffix(frame, "</think>") {
		t.Errorf("frame = %q, want the wrapper intact around everything", frame)
	}
	if !strings.Contains(frame, "画出来的") {
		t.Errorf("frame = %q lost the words it was defusing the tag out of", frame)
	}
}

// TestThinkingKeepsItsShape — a step is one line and gets its whitespace
// folded. A paragraph of reasoning is not a step: folding it to one line makes
// it unreadable, which is a way of not showing it.
func TestThinkingKeepsItsShape(t *testing.T) {
	frame := thinkingFrame(t, "第一步：读代码。\n第二步：改。\n\n\n\n第三步：跑测试。")
	if !strings.Contains(frame, "第一步：读代码。\n第二步：改。") {
		t.Errorf("frame = %q, want the line breaks kept", frame)
	}
	if strings.Contains(frame, "\n\n\n") {
		t.Errorf("frame = %q, want runs of blank lines collapsed", frame)
	}
}

// TestThinkingDropsControlCharacters — same floor as every other fragment.
func TestThinkingDropsControlCharacters(t *testing.T) {
	frame := thinkingFrame(t, "before\x00\x1b[31mafter\rmore")
	for _, r := range frame {
		if r < 0x20 && r != '\n' {
			t.Errorf("frame carries control character %q", r)
		}
	}
	if !strings.Contains(frame, "before") || !strings.Contains(frame, "after") {
		t.Errorf("frame = %q lost the words around the control characters", frame)
	}
}

// TestStepsAndThinkingShareOneBlock — one bubble, one <think>, both halves in
// a fixed order: what it did, what it is thinking, how long it has been.
func TestStepsAndThinkingShareOneBlock(t *testing.T) {
	clock := newTestClock()
	feed := newTestFeed(clock)
	pack := copyFor(LocaleZhHans)
	opened := clock.now()

	feed.record(stepFromToolUse("Read", map[string]any{"file_path": "/srv/app/handler.go"}), pack, opened, progressLevelDetail)
	clock.advance(progressMinInterval)
	step, _ := stepFromTaskMessage(thinking("这里的分支不太对"))
	frame := feed.record(step, pack, opened, progressLevelDetail)

	stepAt := strings.Index(frame, "正在读取 /srv/app/handler.go")
	thinkAt := strings.Index(frame, "这里的分支不太对")
	elapsedAt := strings.Index(frame, "已用时")
	if stepAt < 0 || thinkAt < 0 || elapsedAt < 0 {
		t.Fatalf("frame = %q, want the step, the thinking and the clock", frame)
	}
	if !(stepAt < thinkAt && thinkAt < elapsedAt) {
		t.Errorf("frame = %q, want steps then thinking then the clock", frame)
	}
}

// TestThinkingIsForThePrincipalOnly — reasoning restates the premises of the
// question, which in a chat that is not the principal's own is exactly what
// the closed tier exists for. The tier gate is checked once, above the
// renderer, so this pins the renderer's own half of it.
func TestThinkingIsForThePrincipalOnly(t *testing.T) {
	clock := newTestClock()
	feed := newTestFeed(clock)
	step, ok := stepFromTaskMessage(thinking("先把去年的报价单翻出来"))
	if !ok {
		t.Fatal("a thinking increment produced no step")
	}
	if frame := feed.record(step, copyFor(LocaleZhHans), clock.now(), progressLevelNone); frame != "" {
		t.Errorf("the closed tier rendered reasoning into the bubble: %q", frame)
	}
}

// TestAFullBubbleFitsTheProtocol — the window and the tail have to add up to
// less than what the server will take, or the frame is rejected and the user
// watches a spinner that never moves.
func TestAFullBubbleFitsTheProtocol(t *testing.T) {
	clock := newTestClock()
	feed := newTestFeed(clock)
	pack := copyFor(LocaleZhHans)
	opened := clock.now()

	var last string
	for i := 0; i < progressMaxLines*2; i++ {
		feed.record(stepFromToolUse("Bash", map[string]any{
			"command": strings.Repeat("字", 400) + string(rune('a'+i)),
		}), pack, opened, progressLevelDetail)
		clock.advance(progressMinInterval)
		step, _ := stepFromTaskMessage(thinking(strings.Repeat("思", 400)))
		last = feed.record(step, pack, opened, progressLevelDetail)
		clock.advance(progressMinInterval)
	}
	if len(last) > streamContentLimit {
		t.Errorf("a full bubble is %d bytes, over the %d the server takes", len(last), streamContentLimit)
	}
}
