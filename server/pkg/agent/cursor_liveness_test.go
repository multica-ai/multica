package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

const cursorFakeModeEnv = "CURSOR_FAKE_MODE"

type cursorCleanupRetryProcess struct {
	checks, stops, closes int
	stopped               bool
	stopFailures          int
}

func (p *cursorCleanupRetryProcess) alive() (bool, error) {
	p.checks++
	if p.checks == 1 {
		return false, errors.New("temporary ownership lookup failure")
	}
	return !p.stopped, nil
}
func (p *cursorCleanupRetryProcess) terminate() error {
	p.stops++
	if p.stops <= p.stopFailures {
		return errors.New("temporary termination failure")
	}
	p.stopped = true
	return nil
}

func TestCursorBackgroundCloseRetriesTermination(t *testing.T) {
	for _, failures := range []int{1, 2} {
		t.Run(strconv.Itoa(failures), func(t *testing.T) {
			messages := make(chan Message, 4)
			tracker := newCursorBackgroundTools(context.Background(), nil, messages, slog.Default())
			defer tracker.Close()
			p := &cursorCleanupRetryProcess{checks: 1, stopFailures: failures}
			tracker.Send(Message{Type: MessageToolUse, Tool: "shell", CallID: "retry"})
			tracker.mu.Lock()
			tracker.tools = append(tracker.tools, cursorBackgroundTool{
				call:    cursorToolCall{Name: "shell", CallID: "retry", Result: "raw launch"},
				process: &cursorBackgroundProcess{platform: p},
			})
			tracker.mu.Unlock()
			tracker.Close()
			wantCount := int32(0)
			if failures == 2 {
				wantCount = 1
			}
			if count, _ := tracker.Activity(); count != wantCount || p.stops != 2 || p.closes != 1 {
				t.Fatalf("termination retry: count=%d stops=%d closes=%d", count, p.stops, p.closes)
			}
			<-messages
			if msg := <-messages; msg.Output != "raw launch" || len(messages) != 0 || tracker.Interrupt() {
				t.Fatalf("incorrect final result or repeated recovery: %+v", msg)
			}
		})
	}
}
func (p *cursorCleanupRetryProcess) close() { p.closes++ }

func TestCursorBackgroundCloseRetriesUnconfirmedCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	messages := make(chan Message, 4)
	tracker := newCursorBackgroundTools(ctx, nil, messages, slog.Default())
	p := &cursorCleanupRetryProcess{}
	tracker.Send(Message{Type: MessageToolUse, Tool: "shell", CallID: "retry"})
	tracker.mu.Lock()
	tracker.tools = append(tracker.tools, cursorBackgroundTool{
		call:    cursorToolCall{Name: "shell", CallID: "retry", Result: "raw launch"},
		process: &cursorBackgroundProcess{platform: p},
	})
	tracker.mu.Unlock()
	if tracker.Interrupt() {
		t.Fatal("failed ownership lookup granted recovery")
	}
	if count, _ := tracker.Activity(); count != 1 || len(messages) != 1 {
		t.Fatal("failed cleanup released its tool result")
	}
	tracker.Close()
	tracker.Close()
	if count, _ := tracker.Activity(); count != 0 || p.stops != 1 || p.closes != 1 || tracker.Interrupt() {
		t.Fatalf("cleanup lifecycle: count=%d stops=%d closes=%d", count, p.stops, p.closes)
	}
	<-messages
	if msg := <-messages; msg.CallID != "retry" || msg.Output != "raw launch" || len(messages) != 0 {
		t.Fatalf("missing or repeated matching result: %+v", msg)
	}
}

// The fake shell has its own group and an already-running child before Cursor
// reports success.pid, matching the process relationships observed in #8050.
func runFakeCursorStream(mode string) {
	if mode == "leaf" {
		duration, _ := time.ParseDuration(os.Getenv("CURSOR_FAKE_DURATION"))
		time.Sleep(duration)
		return
	}
	if mode == "shell" {
		if gate := os.Getenv("CURSOR_FAKE_FORK_GATE"); gate != "" {
			for {
				if _, err := os.Stat(gate); err == nil {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), cursorFakeModeEnv+"=leaf")
		hideAgentWindow(child)
		if err := child.Start(); err != nil {
			panic(err)
		}
		data, _ := json.Marshal([]int{os.Getpid(), child.Process.Pid})
		if err := os.WriteFile(os.Getenv("CURSOR_FAKE_PIDS"), data, 0600); err != nil {
			panic(err)
		}
		_ = child.Wait()
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	if mode == "burst" {
		for i := 0; i < 600; i++ {
			fmt.Printf("{\"type\":\"tool_use\",\"tool_id\":\"%d\",\"tool_name\":\"read\"}\n", i)
			fmt.Printf("{\"type\":\"tool_result\",\"tool_id\":\"%d\",\"output\":\"ok\"}\n", i)
		}
		fmt.Println(`{"type":"result","subtype":"success","result":"done"}`)
		return
	}
	count := 1
	if mode == "multiple" {
		count = 2
	}
	var children []*exec.Cmd
	for i := 0; i < count; i++ {
		child := exec.Command(os.Args[0])
		pidFile := filepath.Join(os.Getenv("CURSOR_FAKE_DIR"), strconv.Itoa(i)+".json")
		child.Env = append(os.Environ(), cursorFakeModeEnv+"=shell", "CURSOR_FAKE_PIDS="+pidFile)
		hideAgentWindow(child)
		configureCursorTestBackgroundProcess(child)
		if err := child.Start(); err != nil {
			panic(err)
		}
		children = append(children, child)
		if mode == "late" {
			data, _ := json.Marshal([]int{child.Process.Pid})
			if err := os.WriteFile(pidFile, data, 0600); err != nil {
				panic(err)
			}
		}
		for mode != "late" {
			var pids []int
			data, _ := os.ReadFile(pidFile)
			if json.Unmarshal(data, &pids) == nil && len(pids) == 2 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		fmt.Printf("{\"type\":\"tool_call\",\"subtype\":\"started\",\"call_id\":\"bg-%d\",\"tool_call\":{\"shellToolCall\":{\"args\":{}}}}\n", i)
		fmt.Printf("{\"type\":\"tool_call\",\"subtype\":\"completed\",\"call_id\":\"bg-%d\",\"tool_call\":{\"shellToolCall\":{\"result\":{\"isBackground\":true,\"success\":{\"pid\":%d,\"shellId\":\"shell-%d\"}}}}}\n", i, child.Process.Pid, i)
	}
	fmt.Println(`{"type":"thinking","subtype":"delta","text":"ready"}`)
	fmt.Println(`{"type":"thinking","subtype":"completed"}`)
	if mode != "finish" {
		for _, child := range children {
			_ = child.Wait()
		}
	}
	fmt.Println(`{"type":"system","subtype":"task_notification"}`)
	fmt.Println(`{"type":"result","subtype":"success","session_id":"background-session","result":"background work finished"}`)
}

func TestCursorResultWithoutMessageConsumer(t *testing.T) {
	backend, err := New("cursor", Config{ExecutablePath: os.Args[0], Logger: slog.Default(), Env: map[string]string{cursorFakeModeEnv: "burst"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "overflow the optional transcript", ExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result:
		if result.Status != "completed" || result.Output != "done" {
			t.Fatalf("result-only caller blocked: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal("Result depended on draining Messages")
	}
	count, _ := session.ToolActivity()
	if count == 0 {
		return
	}
	t.Fatalf("dropped transcript corrupted tool accounting: %d", count)
}

func TestCursorBackgroundLifecycle(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("background process ownership is unavailable on this platform")
	}
	for _, mode := range []string{"natural", "budget", "multiple", "finish", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			duration := "30s"
			if mode == "natural" {
				duration = "800ms"
			}
			backend, err := New("cursor", Config{ExecutablePath: self, Logger: slog.Default(), Env: map[string]string{
				cursorFakeModeEnv: mode, "CURSOR_FAKE_DIR": dir, "CURSOR_FAKE_DURATION": duration, "CURSOR_FAKE_SETSID": "1",
			}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "test background lifecycle", ExecOptions{})
			if err != nil {
				t.Fatal(err)
			}
			ready := make(chan struct{})
			done := make(chan struct{})
			var messages []Message
			go func() {
				defer close(done)
				for msg := range session.Messages {
					messages = append(messages, msg)
					if msg.Type == MessageThinking && msg.Content == "ready" {
						close(ready)
					}
				}
			}()
			select {
			case <-ready:
			case <-ctx.Done():
				t.Fatal("fake never reached background observation")
			}
			switch mode {
			case "budget", "multiple":
				if !session.InterruptBackgroundTools() {
					t.Fatal("tool budget did not clean up owned background processes")
				}
				if session.InterruptBackgroundTools() {
					t.Fatal("completed tools granted a second recovery window")
				}
			case "cancel":
				cancel()
			case "natural":
				// A silent job must retain its tool boundary until natural exit.
				select {
				case result := <-session.Result:
					t.Fatalf("background job ended early: %+v", result)
				case <-time.After(150 * time.Millisecond):
				}
			}
			var result Result
			select {
			case result = <-session.Result:
			case <-time.After(10 * time.Second):
				t.Fatal("Cursor did not finalize")
			}
			<-done
			wantStatus := "completed"
			if mode == "cancel" {
				wantStatus = "aborted"
			}
			if result.Status != wantStatus {
				t.Fatalf("status=%q error=%q, want %s", result.Status, result.Error, wantStatus)
			}
			if mode != "cancel" && (result.Output != "background work finished" || result.SessionID != "background-session") {
				t.Fatalf("authoritative result lost: %+v", result)
			}
			started, completed := 0, 0
			for _, msg := range messages {
				switch msg.Type {
				case MessageToolUse:
					started++
				case MessageToolResult:
					completed++
					var raw struct {
						IsBackground bool `json:"isBackground"`
					}
					if json.Unmarshal([]byte(msg.Output), &raw) != nil || !raw.IsBackground {
						t.Fatalf("raw tool result lost: %q", msg.Output)
					}
				}
			}
			if mode != "cancel" && (started == 0 || started != completed) {
				t.Fatalf("unbalanced lifecycle: started=%d completed=%d", started, completed)
			}
			files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
			for _, file := range files {
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				var pids []int
				if err := json.Unmarshal(data, &pids); err != nil {
					t.Fatal(err)
				}
				for _, pid := range pids {
					assertCursorTestProcessGone(t, pid)
				}
			}
		})
	}
}

func TestCursorBackgroundUnverifiedPIDReturnsToIdleBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	messages := make(chan Message, 1)
	tracker := newCursorBackgroundTools(ctx, nil, messages, slog.Default())
	defer tracker.Close()
	tracker.Send(Message{Type: MessageToolUse, Tool: "shell", CallID: "unverified"})
	<-messages
	call := cursorToolCall{Name: "shell", CallID: "unverified", Background: true, PID: -1, Result: `{"isBackground":true}`}
	tracker.Add(call)
	tracker.Reap()
	if tracker.Interrupt() {
		t.Fatal("unverified PID granted a cleanup recovery window")
	}
	if count, _ := tracker.Activity(); count != 0 || len(messages) != 1 {
		t.Fatalf("uncaptured launch did not release its result: count=%d messages=%d", count, len(messages))
	}
	if msg := <-messages; msg.Output != call.Result {
		t.Fatalf("raw launch result lost: %+v", msg)
	}
	tracker.Close()
	if tracker.Interrupt() || len(messages) != 0 {
		t.Fatal("closed tracker granted recovery or duplicated the launch result")
	}
}

func TestCursorBackgroundPIDParsing(t *testing.T) {
	for _, tc := range []struct {
		result     string
		background bool
		pid        int
	}{
		{`{"isBackground":true,"success":{"pid":42,"shellId":"s"}}`, true, 42},
		{`{"isBackground":true,"success":{"pid":"42"}}`, true, 0},
		{`{"isBackground":true}`, true, 0},
		{`{"isBackground":false,"success":{"pid":42}}`, false, 0},
		{`{"stdout":"isBackground: true pid:42"}`, false, 0},
	} {
		evt := cursorStreamEvent{CallID: "c", ToolCall: json.RawMessage(`{"shellToolCall":{"result":` + tc.result + `}}`)}
		call := parseCursorToolCall(&evt)
		if call.PID != tc.pid || call.Background != tc.background || call.Result != tc.result {
			t.Fatalf("parse %s: %+v", tc.result, call)
		}
	}
}
func TestCursorBackgroundOutputTextCannotFakeLifecycleIsStructural(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		event          string
		wantName       string
		wantBackground bool
		wantResult     string
	}{
		{
			name: "root boolean true is the observation",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c1",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"serve"},"result":{"success":{"exitCode":0},"isBackground":true},"toolCallId":"c1"}}}`,
			wantName:       "shell",
			wantBackground: true,
			wantResult:     `{"success":{"exitCode":0},"isBackground":true}`,
		},
		{
			name: "isBackground on another tool is not a launched shell",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c9",` +
				`"tool_call":{"readToolCall":{"args":{"path":"server.log"},` +
				`"result":{"content":"ok","isBackground":true},"toolCallId":"c9"}}}`,
			wantName:   "read",
			wantResult: `{"content":"ok","isBackground":true}`,
		},
		{
			name: "explicit false is foreground",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c2",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"ls"},"result":{"success":{"exitCode":0},"isBackground":false},"toolCallId":"c2"}}}`,
			wantName:   "shell",
			wantResult: `{"success":{"exitCode":0},"isBackground":false}`,
		},
		{
			name: "isBackground only inside captured stdout is not an observation",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c3",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"grep -r isBackground ."},` +
				`"result":{"stdout":"{\"isBackground\":true}","exitCode":0},"toolCallId":"c3"}}}`,
			wantName:   "shell",
			wantResult: `{"stdout":"{\"isBackground\":true}","exitCode":0}`,
		},
		{
			name: "a nested isBackground is not a root field",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c4",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"serve"},` +
				`"result":{"meta":{"isBackground":true},"exitCode":0},"toolCallId":"c4"}}}`,
			wantName:   "shell",
			wantResult: `{"meta":{"isBackground":true},"exitCode":0}`,
		},
		{
			name: "a string where the boolean belongs is not an observation",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c5",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"serve"},` +
				`"result":{"isBackground":"true"},"toolCallId":"c5"}}}`,
			wantName:   "shell",
			wantResult: `{"isBackground":"true"}`,
		},
		{
			name: "non-object result is not an observation",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c6",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"serve"},"result":[{"isBackground":true}],"toolCallId":"c6"}}}`,
			wantName:   "shell",
			wantResult: `[{"isBackground":true}]`,
		},
		{
			name: "missing result stays foreground",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c7",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"serve"},"toolCallId":"c7"}}}`,
			wantName: "shell",
		},
		{
			name: "null result stays foreground",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c8",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"serve"},"result":null,"toolCallId":"c8"}}}`,
			wantName:   "shell",
			wantResult: `null`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var evt cursorStreamEvent
			if err := json.Unmarshal([]byte(tt.event), &evt); err != nil {
				t.Fatalf("unmarshal event: %v", err)
			}
			call := parseCursorToolCall(&evt)
			if call.Background != tt.wantBackground {
				t.Errorf("Background = %v, want %v", call.Background, tt.wantBackground)
			}
			if call.Result != tt.wantResult {
				t.Errorf("Result = %q, want %q (raw result must be preserved verbatim)", call.Result, tt.wantResult)
			}
			if call.Name != tt.wantName || call.CallID == "" {
				t.Errorf("tool identity lost: name=%q want %q, callID=%q", call.Name, tt.wantName, call.CallID)
			}
		})
	}
}
