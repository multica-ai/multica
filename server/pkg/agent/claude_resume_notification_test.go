package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const notificationQueueRecord = `{"type":"queue-operation","operation":"enqueue","sessionId":"notification-session","content":"<task-notification><task-id>background-task</task-id></task-notification>"}` + "\n"

// Re-exec the test binary with an isolated synthetic transcript. Deliberately
// exit before acknowledging supplement initialization, so the prompt writer
// observes a closed pipe after the provider has already exited with code 1.
func runFakeClaudeNotificationResume() {
	if !bufio.NewScanner(os.Stdin).Scan() {
		os.Exit(2)
	}
	mode := os.Getenv("CLAUDE_NOTIFICATION_CASE")
	if mode != "no-notification" && mode != "old-notification" {
		f, err := os.OpenFile(os.Getenv("CLAUDE_NOTIFICATION_TRANSCRIPT"), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			os.Exit(2)
		}
		_, _ = f.WriteString(notificationQueueRecord)
		if mode == "persisted-assistant" {
			_, _ = f.WriteString("{\"type\":\"assistant\"}\n")
		}
		_ = f.Close()
	}
	fmt.Println(`{"type":"system","subtype":"init","session_id":"notification-session"}`)
	switch mode {
	case "assistant":
		fmt.Println(`{"type":"assistant","message":{"content":[{"type":"text","text":"work started"}]}}`)
	case "unreadable-assistant":
		fmt.Println(`{"type":"assistant","message":42}`)
	case "result":
		fmt.Println(`{"type":"result","is_error":true,"result":"rate limited","session_id":"notification-session"}`)
	case "stderr":
		fmt.Fprintln(os.Stderr, "authentication failed")
	case "malformed-stream":
		fmt.Println("not a JSON event")
	case "healthy":
		fmt.Println(`{"type":"control_response","response":{"subtype":"success","request_id":"multica-supplement-initialize"}}`)
		if !bufio.NewScanner(os.Stdin).Scan() {
			os.Exit(2)
		}
		fmt.Println(`{"type":"result","result":"done","session_id":"notification-session"}`)
		os.Exit(0)
	case "timeout":
		time.Sleep(time.Minute)
	case "exit-two":
		os.Exit(2)
	}
	os.Exit(1)
}

func TestClaudeNotificationResume(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		fresh        bool
		wantRejected bool
	}{
		{name: "poisoned", wantRejected: true},
		{name: "no-notification"},
		{name: "old-notification"},
		{name: "assistant"},
		{name: "unreadable-assistant"},
		{name: "result"},
		{name: "stderr"},
		{name: "persisted-assistant"},
		{name: "malformed-stream"},
		{name: "healthy"},
		{name: "timeout"},
		{name: "exit-two"},
		{name: "fresh", fresh: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configDir := t.TempDir()
			projectDir := filepath.Join(configDir, "projects", "fixture")
			if err := os.MkdirAll(projectDir, 0o700); err != nil {
				t.Fatal(err)
			}
			transcript := filepath.Join(projectDir, "notification-session.jsonl")
			original := "{\"type\":\"user\",\"message\":\"original history\"}\n"
			if tc.name == "old-notification" {
				original += notificationQueueRecord
			}
			if err := os.WriteFile(transcript, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			backend := &claudeBackend{cfg: Config{ExecutablePath: self, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Env: map[string]string{
				"IS_SANDBOX": "1", "CLAUDE_CONFIG_DIR": configDir,
				"CLAUDE_FAKE_MODE": "notification_resume", "CLAUDE_NOTIFICATION_CASE": tc.name,
				"CLAUDE_NOTIFICATION_TRANSCRIPT": transcript,
			}}}
			opts := ExecOptions{ResumeSessionID: "notification-session", EnableTaskSupplement: true}
			if tc.name == "timeout" {
				opts.Timeout = 200 * time.Millisecond
			}
			if tc.fresh {
				opts.ResumeSessionID = ""
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "continue", opts)
			if err != nil {
				t.Fatal(err)
			}
			for range session.Messages {
			}
			result := <-session.Result
			if result.ResumeRejected != tc.wantRejected {
				t.Fatalf("ResumeRejected = %v, want %v: %+v", result.ResumeRejected, tc.wantRejected, result)
			}
			if tc.name == "healthy" && (result.Status != "completed" || result.Output != "done" || result.SessionID != opts.ResumeSessionID) {
				t.Fatalf("healthy resume changed: %+v", result)
			}
			if tc.name == "timeout" && result.Status != "timeout" {
				t.Fatalf("timeout changed: %+v", result)
			}
			if tc.wantRejected {
				if result.Status != "failed" || !strings.HasPrefix(result.Error, "claude exited with error: exit status 1") {
					t.Fatalf("provider exit must be primary: %+v", result)
				}
				if result.SessionID != "" {
					t.Fatalf("rejected session retained: %+v", result)
				}
			}
			if tc.name == "no-notification" && !strings.HasPrefix(result.Error, "claude exited with error: exit status 1") {
				t.Fatalf("closed stdin masked provider exit: %+v", result)
			}
			got, err := os.ReadFile(transcript)
			if err != nil {
				t.Fatal(err)
			}
			want := original
			if tc.name != "no-notification" && tc.name != "old-notification" {
				want += notificationQueueRecord
			}
			if tc.name == "persisted-assistant" {
				want += "{\"type\":\"assistant\"}\n"
			}
			if string(got) != want {
				t.Fatal("backend changed the provider transcript")
			}
		})
	}
}

func TestClaudeNotificationEvidenceRequiresIntactTranscript(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		append string
	}{
		{"other-session", strings.ReplaceAll(notificationQueueRecord, "notification-session", "other-session")},
		{"quoted-notification", strings.ReplaceAll(notificationQueueRecord, "<task-notification>", "quoted <task-notification>")},
		{"truncated-record", strings.TrimSuffix(notificationQueueRecord, "\n")},
		{"malformed-record", notificationQueueRecord + "not json\n"},
		{"oversized", notificationQueueRecord + strings.Repeat(" ", claudeNotificationReadLimit)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := &claudeUsageSnapshot{path: path, fileInfo: info, offset: info.Size()}
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(tc.append); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			found, err := snapshot.appendedTaskNotification("notification-session")
			if err != nil || found {
				t.Fatalf("ambiguous transcript accepted: found=%v, err=%v", found, err)
			}
		})
	}
}
