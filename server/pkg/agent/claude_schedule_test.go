package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func scheduleEvent(typ, content, receipt string) claudeSDKMessage {
	return claudeSDKMessage{Type: typ, Message: json.RawMessage(`{"content":[` + content + `]}`), ToolUseResult: json.RawMessage(receipt)}
}

func TestClaudeSchedulesRequireNativeReceipt(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name, receipt       string
		failed, child, want bool
	}{
		{name: "confirmed", receipt: fmt.Sprintf(`{"scheduledFor":%d}`, now.Add(time.Hour).UnixMilli()), want: true},
		{name: "text only", receipt: ""},
		{name: "disabled", receipt: `{"scheduledFor":0}`},
		{name: "failed", receipt: fmt.Sprintf(`{"scheduledFor":%d}`, now.Add(time.Hour).UnixMilli()), failed: true},
		{name: "subagent", receipt: fmt.Sprintf(`{"scheduledFor":%d}`, now.Add(time.Hour).UnixMilli()), child: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s claudeSchedules
			call := scheduleEvent("assistant", `{"type":"tool_use","id":"arm","name":"ScheduleWakeup","input":{}}`, "")
			result := scheduleEvent("user", fmt.Sprintf(`{"type":"tool_result","tool_use_id":"arm","is_error":%t,"content":"scheduled"}`, tc.failed), tc.receipt)
			if tc.child {
				call.ParentToolUseID = "child"
				result.ParentToolUseID = "child"
			}
			s.observe(call, now)
			s.observe(result, now)
			if _, got := s.waitingUntil(); got != tc.want {
				t.Fatalf("waiting=%v want %v", got, tc.want)
			}
		})
	}
}

func TestClaudeSchedulesConsumeAndCancel(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	var s claudeSchedules
	s.observe(scheduleEvent("assistant", `{"type":"tool_use","id":"arm","name":"ScheduleWakeup","input":{}}`, ""), now)
	s.observe(scheduleEvent("user", `{"type":"tool_result","tool_use_id":"arm"}`, fmt.Sprintf(`{"scheduledFor":%d}`, now.Add(time.Minute).UnixMilli())), now)
	s.beginTurn(now.Add(time.Second))
	if _, ok := s.waitingUntil(); !ok {
		t.Fatal("early notification consumed future wakeup")
	}
	s.beginTurn(now.Add(time.Minute))
	if _, ok := s.waitingUntil(); ok {
		t.Fatal("fired one-shot wakeup retained")
	}
	s.observe(scheduleEvent("assistant", `{"type":"tool_use","id":"cron","name":"CronCreate","input":{"cron":"* * * * *"}}`, ""), now)
	s.observe(scheduleEvent("user", `{"type":"tool_result","tool_use_id":"cron"}`, `{"id":"job","recurring":true}`), now)
	s.beginTurn(now.Add(time.Minute))
	if due, ok := s.waitingUntil(); !ok || !due.After(now.Add(time.Minute)) {
		t.Fatalf("recurring cron lost after first fire: %v %v", due, ok)
	}
	s.observe(scheduleEvent("assistant", `{"type":"tool_use","id":"stop","name":"CronDelete","input":{"id":"job"}}`, ""), now)
	s.observe(scheduleEvent("user", `{"type":"tool_result","tool_use_id":"stop","is_error":true}`, `"failed"`), now)
	if _, ok := s.waitingUntil(); !ok {
		t.Fatal("failed deletion removed cron")
	}
	s.observe(scheduleEvent("assistant", `{"type":"tool_use","id":"stop2","name":"CronDelete","input":{"id":"job"}}`, ""), now)
	s.observe(scheduleEvent("user", `{"type":"tool_result","tool_use_id":"stop2"}`, `{"id":"job"}`), now)
	if _, ok := s.waitingUntil(); ok {
		t.Fatal("successful deletion retained cron")
	}
}

// The fake owns its timer, just like native Claude. EOF before the second turn
// cancels it, reproducing the adapter's former close-stdin-on-first-result bug.
func runFakeClaudeNativeLoop(mode string) {
	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		os.Exit(61)
	}
	fmt.Println(`{"type":"system","session_id":"native-session"}`)
	tool, input, receipt := "ScheduleWakeup", `{"delaySeconds":60}`, fmt.Sprintf(`{"scheduledFor":%d}`, time.Now().Add(80*time.Millisecond).UnixMilli())
	if mode == "native_cron" {
		tool = "CronCreate"
		input = `{"cron":"* * * * *","recurring":true}`
		receipt = `{"id":"job","recurring":true}`
	}
	fmt.Printf("{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"tool_use\",\"id\":\"arm\",\"name\":%q,\"input\":%s}]}}\n", tool, input)
	fmt.Printf("{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"arm\",\"content\":\"scheduled\"}]},\"tool_use_result\":%s}\n", receipt)
	fmt.Println(`{"type":"result","session_id":"native-session","result":"first turn","modelUsage":{"k3-256k":{"inputTokens":10,"outputTokens":2}}}`)
	if mode == "native_loop_exit" {
		return
	}
	eof := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, reader); close(eof) }()
	if mode == "native_loop_cancel" {
		<-eof
		return
	}
	select {
	case <-eof:
		return
	case <-time.After(100 * time.Millisecond):
	}
	fmt.Println(`{"type":"system","subtype":"init","session_id":"native-session"}`)
	if mode == "native_loop_init_incomplete" {
		return
	}
	fmt.Println(`{"type":"assistant","message":{"content":[{"type":"text","text":"second turn"}],"model":"k3-256k","usage":{"input_tokens":5,"output_tokens":1}}}`)
	if mode == "native_loop_incomplete" {
		return
	}
	tool, input, receipt = "ScheduleWakeup", `{"stop":true}`, `{"scheduledFor":0,"stopped":true}`
	if mode == "native_cron" {
		tool = "CronDelete"
		input = `{"id":"job"}`
		receipt = `{"id":"job"}`
	}
	fmt.Printf("{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"tool_use\",\"id\":\"stop\",\"name\":%q,\"input\":%s}]}}\n", tool, input)
	fmt.Printf("{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"stop\"}]},\"tool_use_result\":%s}\n", receipt)
	fmt.Println(`{"type":"result","session_id":"native-session","result":"second turn","modelUsage":{"k3-256k":{"inputTokens":20,"outputTokens":4}}}`)
	<-eof
}

func TestClaudeExecuteNativeLoop(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"native_loop", "native_cron", "native_loop_exit", "native_loop_incomplete", "native_loop_init_incomplete", "native_loop_cancel"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			backend, err := New("claude", Config{ExecutablePath: self, Env: map[string]string{"CLAUDE_FAKE_MODE": mode, "IS_SANDBOX": "1"}, Logger: slog.Default()})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "/loop check", ExecOptions{Model: "k3-256k", Timeout: 3 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			waited := false
			for msg := range session.Messages {
				if msg.Type == MessageStatus && !msg.WaitingUntil.IsZero() {
					waited = true
					if mode == "native_loop_cancel" {
						cancel()
					}
				}
			}
			result := <-session.Result
			if !waited {
				t.Fatal("no confirmed waiting status")
			}
			if result.SessionID != "native-session" {
				t.Fatalf("session lost: %+v", result)
			}
			switch mode {
			case "native_loop_cancel":
				if result.Status != "aborted" {
					t.Fatalf("cancelled wait: %+v", result)
				}
			case "native_loop_incomplete", "native_loop_init_incomplete":
				if result.Status != "failed" || !strings.Contains(result.Error, "without terminal result") {
					t.Fatalf("incomplete new turn masked by previous result: %+v", result)
				}
			case "native_loop_exit":
				if result.Status != "completed" || result.Output != "first turn" {
					t.Fatalf("completed checkpoint lost at EOF: %+v", result)
				}
			default:
				if result.Status != "completed" || result.Output != "second turn" || result.Usage["k3-256k"].InputTokens != 20 {
					t.Fatalf("second turn/stop/cumulative usage: %+v", result)
				}
			}
		})
	}
}
