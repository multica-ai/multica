package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func runFakeCursorFirstOutput(mode string) {
	_, _ = io.Copy(io.Discard, os.Stdin)
	switch mode {
	case "first-output-silent":
		time.Sleep(30 * time.Second)
		return
	case "first-output-invalid":
		fmt.Println("not JSON")
		fmt.Println(`{}`)
		time.Sleep(30 * time.Second)
		return
	case "first-output-tool":
		fmt.Println(`{"type":"tool_call","subtype":"started","call_id":"slow","tool_call":{"shellToolCall":{"args":{"command":"build"}}}}`)
	case "first-output-started":
		fmt.Println(`{"type":"system","subtype":"init","session_id":"started"}`)
	}
	time.Sleep(800 * time.Millisecond)
	if mode == "first-output-tool" {
		fmt.Println(`{"type":"tool_call","subtype":"completed","call_id":"slow","tool_call":{"shellToolCall":{"result":{"success":{"output":"done"}}}}}`)
	}
	fmt.Println(`{"type":"result","subtype":"success","result":"done"}`)
}

func TestCursorFirstOutputTimeout(t *testing.T) {
	for _, tc := range []struct {
		name, mode, resume, want string
		timeout                  time.Duration
		cancel                   bool
	}{
		{name: "silent launch", mode: "silent", timeout: 300 * time.Millisecond, want: "timeout"},
		{name: "silent resume", mode: "silent", resume: "prior-session", timeout: 300 * time.Millisecond, want: "timeout"},
		{name: "invalid output", mode: "invalid", timeout: 300 * time.Millisecond, want: "timeout"},
		{name: "startup disarms timer", mode: "started", timeout: 300 * time.Millisecond, want: "completed"},
		{name: "long tool disarms timer", mode: "tool", timeout: 300 * time.Millisecond, want: "completed"},
		{name: "disabled", mode: "delayed", want: "completed"},
		{name: "caller cancellation", mode: "silent", timeout: time.Second, cancel: true, want: "aborted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			backend, err := New("cursor", Config{ExecutablePath: os.Args[0], Logger: slog.Default(), Env: map[string]string{cursorFakeModeEnv: "first-output-" + tc.mode}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "test", ExecOptions{CursorFirstOutputTimeout: tc.timeout, ResumeSessionID: tc.resume})
			if err != nil {
				t.Fatal(err)
			}
			if tc.cancel {
				cancel()
			}
			select {
			case result := <-session.Result:
				if result.Status != tc.want {
					t.Fatalf("result=%+v, want %s", result, tc.want)
				}
				if tc.want == "timeout" && !strings.Contains(result.Error, "no protocol event within 300ms") {
					t.Fatalf("missing startup diagnostic: %+v", result)
				}
				if tc.want == "completed" && result.Output != "done" {
					t.Fatalf("lost result: %+v", result)
				}
			case <-time.After(6 * time.Second):
				t.Fatal("Cursor process did not exit within test bound")
			}
		})
	}
}
