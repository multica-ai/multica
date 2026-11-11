package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// progressHarness is an Outbound wired to a Bot API that records every call.
type progressHarness struct {
	t      *testing.T
	o      *Outbound
	taskID string
	// skew moves the Outbound's clock ahead of the wall clock, so a test can
	// let a minute of a run pass without waiting for it.
	skew atomic.Int64

	mu       sync.Mutex
	requests []progressRequest
	// hold, when set, keeps every sendMessage unanswered until it is closed.
	hold chan struct{}
}

type progressRequest struct {
	method string
	text   string
}

// newProgressHarness starts an Outbound for one chat. The clock under the
// steps stands still unless configure gives it a cadence, so a slow test run
// does not pick up edits of its own.
func newProgressHarness(t *testing.T, chatType channel.ChatType, configure ...func(*Outbound)) *progressHarness {
	t.Helper()
	h := &progressHarness{t: t, taskID: telegramTestEvent().TaskID}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		text, _ := body["text"].(string)
		h.mu.Lock()
		h.requests = append(h.requests, progressRequest{
			method: r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], text: text,
		})
		hold := h.hold
		h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			if hold != nil {
				<-hold
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"},"date":0,"text":"x"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	t.Cleanup(api.Close)

	q := newTelegramOutboundQueries(t)
	q.channelOrigin = true
	q.binding.ChatType = string(chatType)
	h.o = NewOutbound(q, nil, api.URL, api.Client(), nil)
	h.o.now = func() time.Time { return time.Now().Add(time.Duration(h.skew.Load())) }
	h.o.heartbeat = func(int64, time.Duration) time.Duration { return 0 }
	for _, fn := range configure {
		fn(h.o)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.o.Start(ctx)
	t.Cleanup(func() {
		cancel()
		if !h.o.WaitWithTimeout(time.Second) {
			t.Error("outbound workers did not stop")
		}
	})
	return h
}

func (h *progressHarness) sent() []progressRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]progressRequest(nil), h.requests...)
}

// last returns the newest request once there are n of them.
func (h *progressHarness) last(n int) progressRequest {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for len(h.sent()) < n && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := h.sent()
	if len(got) != n {
		h.t.Fatalf("requests = %+v, want %d", got, n)
	}
	return got[n-1]
}

// openEditSlot ends the chat's edit interval, so the next frame goes out at
// once instead of waiting for it.
func (h *progressHarness) openEditSlot() {
	h.o.mu.Lock()
	st := h.o.streams[h.taskID]
	h.o.mu.Unlock()
	if st == nil {
		return
	}
	st.schedule.mu.Lock()
	st.schedule.lastEdit = time.Now().Add(-time.Hour)
	st.schedule.mu.Unlock()
}

func (h *progressHarness) text(content string) {
	h.openEditSlot()
	h.o.handleTaskMessage(events.Event{
		TaskID: h.taskID, Type: protocol.EventTaskMessage,
		Payload: protocol.TaskMessagePayload{TaskID: h.taskID, Type: "text", Content: content},
	})
}

func (h *progressHarness) tool(tool string, input map[string]any) {
	h.openEditSlot()
	h.o.handleTaskMessage(toolFrame(h.taskID, tool, input))
}

func (h *progressHarness) launch() {
	h.o.handleTaskProgress(events.Event{
		TaskID: h.taskID, Type: protocol.EventTaskProgress,
		Payload: protocol.TaskProgressPayload{TaskID: h.taskID, Summary: "Launching codex", Step: 1, Total: 2},
	})
}

func toolFrame(taskID, tool string, input map[string]any) events.Event {
	return events.Event{
		TaskID: taskID, Type: protocol.EventTaskMessage,
		Payload: protocol.TaskMessagePayload{TaskID: taskID, Type: "tool_use", Tool: tool, Input: input},
	}
}

func toolCall(tool string, input map[string]any) protocol.TaskMessagePayload {
	return protocol.TaskMessagePayload{Type: "tool_use", Tool: tool, Input: input}
}

var clockLine = regexp.MustCompile(`\n⏱ [^<\n]*`)

// clockless drops the clock from a body. It appears once a run is a second
// old, which a loaded machine reaches in the middle of a test.
func clockless(text string) string { return clockLine.ReplaceAllString(text, "") }

// An agent that says one sentence and then works in tools for minutes left the
// chat showing that sentence and nothing else. The steps go under it, in the
// same message, and give way when the agent speaks again.
func TestOutboundShowsToolStepsUnderTheStreamedText(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)

	h.text("I will check the repositories.")
	if got := h.last(1); got.method != "sendMessage" || got.text != "I will check the repositories." {
		t.Fatalf("opening frame = %+v", got)
	}

	// The wrapper shell the command arrives in is not part of the step.
	h.tool("exec_command", map[string]any{"command": `/bin/bash -lc "git status --short"`})
	want := "I will check the repositories.\n\n<blockquote>💻 Running git status --short</blockquote>"
	if got := h.last(2); got.method != "editMessageText" || clockless(got.text) != want {
		t.Fatalf("first step = %+v\nwant text %q", got, want)
	}

	// The same call again is the same line, counted.
	h.tool("exec_command", map[string]any{"command": `/bin/bash -lc "git status --short"`})
	h.tool("Read", map[string]any{"file_path": "server/main.go"})
	want = "I will check the repositories.\n\n<blockquote>💻 Running git status --short ×2\n📖 Reading server/main.go</blockquote>"
	if got := h.last(4); clockless(got.text) != want {
		t.Fatalf("later steps = %q\nwant %q", got.text, want)
	}

	// A tool's output is not a step and costs no edit.
	h.openEditSlot()
	h.o.handleTaskMessage(events.Event{
		TaskID: h.taskID, Type: protocol.EventTaskMessage,
		Payload: protocol.TaskMessagePayload{TaskID: h.taskID, Type: "tool_result", Tool: "Read", Output: "package main"},
	})

	// The agent is writing again: what it was doing is over, and what it says
	// now is a new paragraph.
	h.text("Everything is clean.")
	if got := h.last(5); got.text != "I will check the repositories.\n\nEverything is clean." {
		t.Fatalf("text after the steps = %q", got.text)
	}
}

// Many agents start with a tool call and write nothing until the end. The
// first step opens the reply, so the chat is not empty for the whole run.
func TestOutboundOpensTheReplyOnTheFirstToolStep(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)

	h.tool("Grep", map[string]any{"pattern": "TODO <fix>"})
	got := h.last(1)
	// The fragment's markup characters were already replaced on the way in.
	if got.method != "sendMessage" || clockless(got.text) != "<blockquote>🔎 Searching for TODO fix</blockquote>" {
		t.Fatalf("opening step = %+v", got)
	}
}

// The agent's CLI takes seconds to start and the model more to answer. The
// reply opens when the run is launched, and the first real step takes the
// stand-in's place.
func TestOutboundOpensTheReplyWhenTheAgentIsLaunched(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)

	h.launch()
	if got := h.last(1); got.method != "sendMessage" || clockless(got.text) != "<blockquote>⏳ Starting the agent</blockquote>" {
		t.Fatalf("launch = %+v", got)
	}

	// The daemon's other milestone is the run finishing: nothing to show.
	h.o.handleTaskProgress(events.Event{
		TaskID: h.taskID, Type: protocol.EventTaskProgress,
		Payload: protocol.TaskProgressPayload{TaskID: h.taskID, Summary: "Finishing task", Step: 2, Total: 2},
	})

	h.tool("Read", map[string]any{"file_path": "go.mod"})
	if got := h.last(2); clockless(got.text) != "<blockquote>📖 Reading go.mod</blockquote>" {
		t.Fatalf("first step after launch = %q", got.text)
	}

	h.text("Go 1.26.")
	if got := h.last(3); got.text != "Go 1.26." {
		t.Fatalf("text after launch = %q", got.text)
	}
}

// A group is read by people the run does not belong to. They see what kind of
// work is going on, and none of the paths, commands or search terms.
func TestOutboundKeepsStepArgumentsOutOfGroups(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeGroup)

	h.tool("exec_command", map[string]any{"command": "cat /srv/acme/.env"})
	h.tool("exec_command", map[string]any{"command": "ls /srv/acme"})
	got := h.last(2)
	if clockless(got.text) != "<blockquote>💻 Running a command ×2</blockquote>" {
		t.Fatalf("group steps = %q", got.text)
	}
}

// Steps that arrive while an edit is already waiting for its slot ride along
// with it: one edit carries all of them.
func TestOutboundCarriesABurstOfStepsInOneEdit(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P)

	h.text("Looking.")
	h.last(1)

	// Leave 150ms of the edit interval and send the burst inside it.
	st := func() *streamState {
		h.o.mu.Lock()
		defer h.o.mu.Unlock()
		return h.o.streams[h.taskID]
	}()
	st.schedule.mu.Lock()
	st.schedule.lastEdit = time.Now().Add(150*time.Millisecond - privateEditInterval)
	st.schedule.mu.Unlock()
	for _, file := range []string{"a.go", "b.go", "c.go"} {
		h.o.handleTaskMessage(toolFrame(h.taskID, "Read", map[string]any{"file_path": file}))
	}
	if got := h.sent(); len(got) != 1 {
		t.Fatalf("steps inside the edit interval sent %d requests, want them held back", len(got)-1)
	}

	want := "Looking.\n\n<blockquote>📖 Reading a.go\n📖 Reading b.go\n📖 Reading c.go</blockquote>"
	if got := h.last(2); clockless(got.text) != want {
		t.Fatalf("burst = %q\nwant %q", got.text, want)
	}
	time.Sleep(300 * time.Millisecond)
	if got := h.sent(); len(got) != 2 {
		t.Fatalf("requests after the burst = %d, want 2", len(got))
	}
}

// A build can run for minutes without a frame. The clock under the steps keeps
// moving meanwhile, or the message looks exactly like a run that hung.
func TestOutboundKeepsTheClockMovingDuringALongStep(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P, func(o *Outbound) {
		o.heartbeat = func(int64, time.Duration) time.Duration { return 40 * time.Millisecond }
	})

	h.tool("exec_command", map[string]any{"command": "make test"})
	if got := h.last(1); clockless(got.text) != "<blockquote>💻 Running make test</blockquote>" {
		t.Fatalf("opening step = %+v", got)
	}

	// 75 seconds pass with nothing from the agent.
	h.skew.Add(int64(75 * time.Second))
	want := "<blockquote>💻 Running make test\n⏱ 1m 15s · 1 step</blockquote>"
	if got := h.last(2); got.method != "editMessageText" || got.text != want {
		t.Fatalf("clock = %+v\nwant text %q", got, want)
	}

	// The clock has not moved since: nothing new to show, so nothing is sent.
	time.Sleep(200 * time.Millisecond)
	if got := h.sent(); len(got) != 2 {
		t.Fatalf("requests with the clock standing still = %+v, want 2", got)
	}
}

// With the switch off the reply is the agent's text and nothing else.
func TestOutboundShowsNoStepsWhenProgressIsOff(t *testing.T) {
	h := newProgressHarness(t, channel.ChatTypeP2P, func(o *Outbound) { o.SetProgressMode(ProgressOff) })

	h.tool("exec_command", map[string]any{"command": "make test"})
	h.text("Done.")
	if got := h.last(1); got.method != "sendMessage" || got.text != "Done." {
		t.Fatalf("reply = %+v", got)
	}
}

// A cancelled run has no answer to take the steps' place. The text stays as
// the agent left it and the steps go; they would say "running" for good.
func TestOutboundTakesTheStepsOffACancelledReply(t *testing.T) {
	cancelled := func(h *progressHarness) {
		h.o.handleTaskCancelled(events.Event{
			TaskID: h.taskID, ChatSessionID: telegramTestEvent().ChatSessionID, Type: protocol.EventTaskCancelled,
		})
	}

	t.Run("text stays", func(t *testing.T) {
		h := newProgressHarness(t, channel.ChatTypeP2P)
		h.text("Checking the build.")
		h.tool("exec_command", map[string]any{"command": "make test"})
		h.last(2)
		cancelled(h)
		if got := h.last(3); got.method != "editMessageText" || got.text != "Checking the build." {
			t.Fatalf("after cancel = %+v", got)
		}
	})

	t.Run("a message of steps alone is removed", func(t *testing.T) {
		h := newProgressHarness(t, channel.ChatTypeP2P)
		h.tool("exec_command", map[string]any{"command": "make test"})
		h.last(1)
		cancelled(h)
		if got := h.last(2); got.method != "deleteMessage" {
			t.Fatalf("after cancel = %+v", got)
		}
	})

	// Text that is waiting for the chat's next edit has already taken the
	// steps' place in memory, but the chat still shows them.
	t.Run("steps go when text was waiting to replace them", func(t *testing.T) {
		h := newProgressHarness(t, channel.ChatTypeP2P)
		h.text("Checking the build.")
		h.tool("exec_command", map[string]any{"command": "make test"})
		h.last(2)
		// Inside the edit interval: kept, not sent.
		h.o.handleTaskMessage(events.Event{
			TaskID: h.taskID, Type: protocol.EventTaskMessage,
			Payload: protocol.TaskMessagePayload{TaskID: h.taskID, Type: "text", Content: "It fails."},
		})
		cancelled(h)
		if got := h.last(3); got.method != "editMessageText" || got.text != "Checking the build.\n\nIt fails." {
			t.Fatalf("after cancel = %+v", got)
		}
	})

	// The cancel can arrive while the message that opens the reply is still
	// on its way to Telegram. Nothing would ever edit it again.
	t.Run("a message still on its way is removed when it lands", func(t *testing.T) {
		h := newProgressHarness(t, channel.ChatTypeP2P)
		hold := make(chan struct{})
		h.mu.Lock()
		h.hold = hold
		h.mu.Unlock()
		sent := make(chan struct{})
		go func() {
			defer close(sent)
			h.launch()
		}()
		h.last(1)
		cancelled(h)
		close(hold)
		<-sent
		if got := h.last(2); got.method != "deleteMessage" {
			t.Fatalf("after cancel = %+v", got)
		}
	})
}

// Who sees a step's argument is the mode's decision and the chat's.
func TestStepForShowsArgumentsByModeAndChat(t *testing.T) {
	call := toolCall("Bash", map[string]any{"command": "cat notes.txt"})
	cases := []struct {
		mode    ProgressMode
		private bool
		want    string
	}{
		{ProgressAuto, true, "Running cat notes.txt"},
		{ProgressAuto, false, "Running a command"},
		{ProgressSummary, true, "Running a command"},
		{ProgressDetail, false, "Running cat notes.txt"},
	}
	for _, tc := range cases {
		o := &Outbound{progress: tc.mode}
		step, ok := o.stepFor(&replyTarget{private: tc.private}, call)
		if !ok || step.icon != "💻" || step.text != tc.want {
			t.Errorf("mode %s private %v: step = %+v, %v; want %q", tc.mode, tc.private, step, ok, tc.want)
		}
	}
}

// A long command keeps both ends: what it is, and what it is aimed at.
func TestStepForClipsALongCommandInTheMiddle(t *testing.T) {
	command := "go test -race -count=1 " + strings.Repeat("-tags=integration ", 5) + "./internal/integrations/telegram/..."
	o := &Outbound{progress: ProgressDetail}
	step, _ := o.stepFor(&replyTarget{}, toolCall("Bash", map[string]any{"command": command}))
	if n := len([]rune(step.text)); n > maxStepRunes {
		t.Fatalf("step is %d runes, want at most %d: %q", n, maxStepRunes, step.text)
	}
	if !strings.HasPrefix(step.text, "Running go test -race") || !strings.HasSuffix(step.text, "telegram/...") || !strings.Contains(step.text, "…") {
		t.Fatalf("step = %q", step.text)
	}
}

func TestUnwrapShellCommand(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"double quotes", map[string]any{"command": `/bin/bash -lc "rg \"TODO\" server"`}, `rg "TODO" server`},
		{"single quotes", map[string]any{"command": `zsh -c 'echo '\''hi'\'''`}, `echo 'hi'`},
		{"bare", map[string]any{"command": "sh -c ls"}, "ls"},
		{"no wrapper", map[string]any{"command": "git status"}, "git status"},
		{"argv", map[string]any{"cmd": []any{"bash", "-lc", "go build ./..."}}, "go build ./..."},
		{"argv without a shell", map[string]any{"command": []any{"git", "status"}}, "git status"},
		// An agent often sends a whole script as one command. Its first line
		// says where it starts; the rest would be a patch body in the chat.
		{"script in a wrapper", map[string]any{"command": "/bin/zsh -lc \"mkdir run-b\napply_patch <<'PATCH'\n*** Begin Patch\n+First line.\nPATCH\""}, "mkdir run-b …"},
		{"script without a wrapper", map[string]any{"command": "\nset -e\nmake test\n"}, "set -e …"},
		{"argv script", map[string]any{"cmd": []any{"bash", "-lc", "cd server\ngo build ./..."}}, "cd server …"},
		{"one line with a trailing newline", map[string]any{"command": "bash -lc 'git status\n'"}, "git status"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := tc.input["command"]
			out := unwrapShellCommand(tc.input)
			got, _ := out["command"].(string)
			if got == "" {
				got, _ = out["cmd"].(string)
			}
			if got != tc.want {
				t.Fatalf("command = %q, want %q", got, tc.want)
			}
			// Other subscribers read the same payload.
			if s, ok := original.(string); ok && tc.input["command"] != s {
				t.Fatalf("the event's own input was changed to %v", tc.input["command"])
			}
		})
	}
}

// The list is where the run is now: the newest steps, with a count of all of
// them.
func TestStepFeedKeepsTheNewestSteps(t *testing.T) {
	var feed stepFeed
	feed.add(stepLine{icon: "⏳", text: "Starting the agent", transient: true})
	for _, file := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		feed.add(stepLine{icon: "📖", text: "Reading " + file})
	}
	feed.add(stepLine{icon: "📖", text: "Reading h"})
	want := "<blockquote>📖 Reading c\n📖 Reading d\n📖 Reading e\n📖 Reading f\n📖 Reading g\n📖 Reading h ×2\n⏱ 2m · 9 steps</blockquote>"
	if got := feed.render(2 * time.Minute); got != want {
		t.Fatalf("feed = %q\nwant %q", got, want)
	}
	if got := (&stepFeed{}).render(time.Minute); got != "" {
		t.Fatalf("empty feed = %q", got)
	}
}

func TestParseProgressMode(t *testing.T) {
	for raw, want := range map[string]ProgressMode{
		"": ProgressAuto, "auto": ProgressAuto, " Off ": ProgressOff, "summary": ProgressSummary, "DETAIL": ProgressDetail,
	} {
		if got, ok := ParseProgressMode(raw); !ok || got != want {
			t.Errorf("ParseProgressMode(%q) = %s, %v; want %s", raw, got, ok, want)
		}
	}
	if got, ok := ParseProgressMode("verbose"); ok || got != ProgressAuto {
		t.Errorf("unknown mode = %s, %v; want auto reported as unknown", got, ok)
	}
}
