package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const chatSessionTestID = "018f3caa-0000-7000-8000-000000000001"

func chatSessionTestEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Chdir(t.TempDir())
	setCLITestServerEnv(t, serverURL)
	t.Setenv("MULTICA_TOKEN", "mat_chat_test")
	t.Setenv("MULTICA_AGENT_ID", "agent-chat-test")
	t.Setenv("MULTICA_TASK_ID", "task-chat-test")
	t.Setenv("LC_ALL", "C")
}

func executeChatSessionTest(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs(args)
	return captureStdout(t, cmd.Execute)
}

func TestChatSessionList(t *testing.T) {
	for _, output := range []string{"json", "table"} {
		t.Run(output, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/chat/sessions/" || r.URL.RawQuery != "" {
					t.Errorf("request = %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer mat_chat_test" || r.Header.Get("X-Workspace-ID") != "ws-1" {
					t.Error("task credential or workspace missing")
				}
				if r.Header.Get("X-Agent-ID") != "agent-chat-test" || r.Header.Get("X-Task-ID") != "task-chat-test" {
					t.Error("task execution headers missing")
				}
				fmt.Fprint(w, `[{"id":"`+chatSessionTestID+`","title":"Queue test","agent_id":"agent-1","status":"active"},{"id":"other","title":"Other conversation","agent_id":"agent-2","status":"active"}]`)
			}))
			defer srv.Close()
			chatSessionTestEnv(t, srv.URL)
			out, err := executeChatSessionTest(t, newChatListCmd(), "--title", "Queue test", "--output", output)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, chatSessionTestID) || strings.Contains(out, "Other conversation") {
				t.Fatalf("unexpected filtered output: %s", out)
			}
			if output == "json" {
				var rows []map[string]any
				if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 {
					t.Fatalf("invalid JSON list: %s (%v)", out, err)
				}
			}
		})
	}
}

func TestChatSessionListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[]`) }))
	defer srv.Close()
	chatSessionTestEnv(t, srv.URL)
	out, err := executeChatSessionTest(t, newChatListCmd())
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("output=%q err=%v", out, err)
	}
}

func TestChatSessionSend(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprint(queued), func(t *testing.T) {
			var calls atomic.Int32
			body := "  请检查排队\n保留正文格式。  "
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/api/chat/sessions/"+chatSessionTestID+"/messages" {
					t.Errorf("request = %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer mat_chat_test" || r.Header.Get("X-Workspace-ID") != "ws-1" {
					t.Error("task credential or workspace missing")
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload["content"] != body || len(payload) != 1 {
					t.Errorf("payload = %#v", payload)
				}
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintf(w, `{"message_id":"message-1","task_id":"run-1","queued":%t,"supports_queue":true,"created_at":"2026-10-04T02:00:00Z"}`, queued)
			}))
			defer srv.Close()
			chatSessionTestEnv(t, srv.URL)
			out, err := executeChatSessionTest(t, newChatSendCmd(), chatSessionTestID, "--body", body)
			if err != nil {
				t.Fatal(err)
			}
			var resp map[string]any
			if err := json.Unmarshal([]byte(out), &resp); err != nil {
				t.Fatal(err)
			}
			if resp["queued"] != queued || resp["task_id"] != "run-1" || calls.Load() != 1 {
				t.Fatalf("response=%#v calls=%d", resp, calls.Load())
			}
		})
	}
}

func TestChatSessionSendInvalidInputDoesNotRequest(t *testing.T) {
	for _, args := range [][]string{
		{}, {chatSessionTestID}, {chatSessionTestID, "--body", " \n\t"},
		{"../other/messages", "--body", "hello"}, {"018f3caa", "--body", "hello"},
		{chatSessionTestID, "extra", "--body", "hello"},
		{chatSessionTestID, "--body", "hello", "--output", "invalid"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer srv.Close()
			chatSessionTestEnv(t, srv.URL)
			_, err := executeChatSessionTest(t, newChatSendCmd(), args...)
			if err == nil || calls.Load() != 0 {
				t.Fatalf("error=%v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestChatSessionSendFailureDoesNotRetry(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 429, 500, 502, 503, 504} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":"send rejected"}`)
			}))
			defer srv.Close()
			chatSessionTestEnv(t, srv.URL)
			out, err := executeChatSessionTest(t, newChatSendCmd(), chatSessionTestID, "--body", "hello")
			if err == nil || calls.Load() != 1 || out != "" {
				t.Fatalf("error=%v calls=%d output=%q", err, calls.Load(), out)
			}
			var httpErr *cli.HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != status {
				t.Fatalf("HTTP classification lost: %v", err)
			}
			message := cli.FormatError(err, false)
			if status >= 500 {
				if !strings.Contains(message, "Check the destination before sending again") || strings.Contains(message, "try again later") {
					t.Fatalf("unsafe delivery guidance: %q", message)
				}
				if !strings.Contains(cli.FormatError(err, true), "send rejected") {
					t.Fatal("debug output lost the underlying server response")
				}
			} else {
				var userErr *cli.UserMessageError
				if errors.As(err, &userErr) {
					t.Fatalf("explicit rejection should keep its original guidance: %v", err)
				}
			}
		})
	}
}

func TestChatSessionSendDroppedResponseDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	defer srv.Close()
	chatSessionTestEnv(t, srv.URL)
	_, err := executeChatSessionTest(t, newChatSendCmd(), chatSessionTestID, "--body", "hello")
	if err == nil || calls.Load() != 1 {
		t.Fatalf("error=%v calls=%d", err, calls.Load())
	}
	if !strings.Contains(cli.FormatError(err, false), "Check the destination before sending again") || cli.ExitCodeFor(err) != cli.ExitNetwork {
		t.Fatalf("lost delivery guidance or network classification: %v", err)
	}
}

func TestChatSessionSendMalformedAcknowledgementDoesNotRetry(t *testing.T) {
	for _, response := range []string{`null`, `{}`, `{"message_id":"message-1"}`, `{"task_id":"run-1"}`, `not-json`} {
		t.Run(response, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, response)
			}))
			defer srv.Close()
			chatSessionTestEnv(t, srv.URL)
			out, err := executeChatSessionTest(t, newChatSendCmd(), chatSessionTestID, "--body", "hello")
			if err == nil || calls.Load() != 1 || out != "" {
				t.Fatalf("error=%v calls=%d output=%q", err, calls.Load(), out)
			}
			if !strings.Contains(strings.ToLower(cli.FormatError(err, false)), "check the destination before sending again") {
				t.Fatalf("missing ambiguous-delivery guidance: %v", err)
			}
		})
	}
}

func TestChatSessionSendTableDoesNotAssumeMissingQueueFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"message_id":"message-1","task_id":"run-1"}`)
	}))
	defer srv.Close()
	chatSessionTestEnv(t, srv.URL)
	out, err := executeChatSessionTest(t, newChatSendCmd(), chatSessionTestID, "--body", "hello", "--output", "table")
	if err != nil || !strings.Contains(out, "run-1") || !strings.Contains(out, "unknown") {
		t.Fatalf("output=%q error=%v", out, err)
	}
}

func TestChatSessionCommandRegistration(t *testing.T) {
	for _, name := range []string{"list", "send", "history", "thread"} {
		cmd, _, err := chatCmd.Find([]string{name})
		if err != nil || cmd == chatCmd || cmd.Name() != name {
			t.Fatalf("chat %s is not registered: %v", name, err)
		}
	}
}

func TestChatSessionCommandsRejectMissingOrMemberTaskCredential(t *testing.T) {
	for _, token := range []string{"", "mul_member_test"} {
		for _, name := range []string{"list", "send"} {
			t.Run(name+"/"+token, func(t *testing.T) {
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
				defer srv.Close()
				chatSessionTestEnv(t, srv.URL)
				t.Setenv("MULTICA_TOKEN", token)
				cmd := newChatListCmd()
				var args []string
				if name == "send" {
					cmd = newChatSendCmd()
					args = []string{chatSessionTestID, "--body", "hello"}
				}
				_, err := executeChatSessionTest(t, cmd, args...)
				if err == nil || !strings.Contains(err.Error(), "mat_") || calls.Load() != 0 {
					t.Fatalf("error=%v calls=%d", err, calls.Load())
				}
			})
		}
	}
}
