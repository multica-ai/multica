package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const qoderCloudTestPAT = "test-qoder-cloud-pat"

func TestQoderCloudExecuteCreatesPinnedSessionAndStreamsCurrentTurn(t *testing.T) {
	var (
		mu             sync.Mutex
		createPayload  map[string]any
		eventPayload   map[string]any
		streamCursor   string
		streamRawQuery string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+qoderCloudTestPAT {
			t.Errorf("Authorization = %q, want bearer test credential", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/agents/agent-1":
			_, _ = io.WriteString(w, `{"id":"agent-1","version":7}`)
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			if err := json.NewDecoder(r.Body).Decode(&createPayload); err != nil {
				t.Errorf("decode create session: %v", err)
			}
			_, _ = io.WriteString(w, `{"id":"session-1","status":"idle"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/sessions/session-1/events":
			if err := json.NewDecoder(r.Body).Decode(&eventPayload); err != nil {
				t.Errorf("decode send event: %v", err)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"event-user","turn_id":"turn-current"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/sessions/session-1/events/stream":
			mu.Lock()
			streamCursor = r.Header.Get("Last-Event-ID")
			streamRawQuery = r.URL.RawQuery
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				"id: old-id",
				"event: session.status_idle",
				`data: {"id":"old-id","type":"session.status_idle","turn_id":"turn-old"}`,
				"",
				"id: tool-use-1",
				"event: agent.tool_use",
				`data: {"id":"tool-use-1","type":"agent.tool_use","turn_id":"turn-current","name":"Bash","input":{"command":"pwd"}}`,
				"",
				"id: tool-result-1",
				"event: agent.tool_result",
				`data: {"id":"tool-result-1","type":"agent.tool_result","turn_id":"turn-current","tool_use_id":"tool-use-1","result":[{"type":"text","text":"ok"}]}`,
				"",
				"id: message-1",
				"event: agent.message",
				`data: {"id":"message-1","type":"agent.message","turn_id":"turn-current","content":[{"type":"text","text":"hello cloud"}]}`,
				"",
				// Buffered messages are authoritative, but reconnect/replay may
				// overlap. An identical event must not duplicate Result.Output.
				"id: message-1",
				"event: agent.message",
				`data: {"id":"message-1","type":"agent.message","turn_id":"turn-current","content":[{"type":"text","text":"hello cloud"}]}`,
				"",
				"id: idle-1",
				"event: session.status_idle",
				`data: {"id":"idle-1","type":"session.status_idle","turn_id":"turn-current","session_id":"session-1","usage":{"input_tokens":11,"output_tokens":3,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}}`,
				"",
			}, "\n"))
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{})
	result, messages := executeQoderCloudTest(t, backend, context.Background(), "say hello", ExecOptions{})

	if result.Status != "completed" || result.Output != "hello cloud" || result.SessionID != "session-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := result.Usage["qodercloud"]; got != (TokenUsage{InputTokens: 11, OutputTokens: 3, CacheReadTokens: 2, CacheWriteTokens: 1}) {
		t.Fatalf("unexpected token usage: %+v", got)
	}

	agent, ok := createPayload["agent"].(map[string]any)
	if !ok {
		t.Fatalf("create agent is not an object: %#v", createPayload["agent"])
	}
	if agent["id"] != "agent-1" || agent["type"] != "agent" || agent["version"] != float64(7) {
		t.Fatalf("session was not pinned to the resolved agent version: %#v", agent)
	}
	if createPayload["environment_id"] != "environment-1" {
		t.Fatalf("unexpected environment: %#v", createPayload["environment_id"])
	}

	events, ok := eventPayload["events"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("send payload events = %#v", eventPayload["events"])
	}
	userEvent := events[0].(map[string]any)
	content, ok := userEvent["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("user.message content is not a block array: %#v", userEvent["content"])
	}
	block := content[0].(map[string]any)
	if userEvent["type"] != "user.message" || block["type"] != "text" || block["text"] != "say hello" {
		t.Fatalf("unexpected user.message payload: %#v", userEvent)
	}

	mu.Lock()
	if streamCursor != "event-user" {
		t.Errorf("initial Last-Event-ID = %q, want event-user", streamCursor)
	}
	if streamRawQuery != "" {
		t.Errorf("stream query = %q; cursor must use Last-Event-ID and deltas must not be requested", streamRawQuery)
	}
	mu.Unlock()

	assertQoderCloudMessage(t, messages, func(message Message) bool {
		return message.Type == MessageStatus && message.SessionID == "session-1"
	}, "early session status")
	assertQoderCloudMessage(t, messages, func(message Message) bool {
		return message.Type == MessageToolUse && message.Tool == "Bash" && message.CallID == "tool-use-1" && message.Input["command"] == "pwd"
	}, "tool use")
	assertQoderCloudMessage(t, messages, func(message Message) bool {
		return message.Type == MessageToolResult && message.CallID == "tool-use-1" && message.Output == "ok"
	}, "tool result")
}

func TestQoderCloudExecuteAcceptsCurrentPublicEventsWithoutTurnID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sessions":
			_, _ = io.WriteString(w, `{"id":"session-no-turn"}`)
		case "/sessions/session-no-turn/events":
			_, _ = io.WriteString(w, `{"data":[{"id":"user-no-turn","type":"user.message"}]}`)
		case "/sessions/session-no-turn/events/stream":
			if got := r.Header.Get("Last-Event-ID"); got != "user-no-turn" {
				t.Errorf("Last-Event-ID = %q, want user-no-turn", got)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: agent.message\nid: response-no-turn\ndata: {\"content\":[{\"type\":\"text\",\"text\":\"no turn works\"}]}\n\nevent: session.status_idle\nid: idle-no-turn\ndata: {\"status\":\"idle\"}\n\n")
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
	result, _ := executeQoderCloudTest(t, backend, context.Background(), "hello", ExecOptions{})
	if result.Status != "completed" || result.Output != "no turn works" {
		t.Fatalf("unexpected no-turn result: %+v", result)
	}
}

func TestQoderCloudSystemPromptUsesTrailingSystemEventAndLastResponseCursor(t *testing.T) {
	var eventPayload struct {
		Events []qoderCloudOutgoingEvent `json:"events"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sessions":
			_, _ = io.WriteString(w, `{"id":"system-session"}`)
		case "/sessions/system-session/events":
			if err := json.NewDecoder(r.Body).Decode(&eventPayload); err != nil {
				t.Errorf("decode events: %v", err)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"accepted-user","turn_id":"system-turn"},{"id":"accepted-system"}]}`)
		case "/sessions/system-session/events/stream":
			if got := r.Header.Get("Last-Event-ID"); got != "accepted-system" {
				t.Errorf("Last-Event-ID = %q, want last processed system event", got)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: agent.message\nid: system-answer\ndata: {\"turn_id\":\"system-turn\",\"content\":[{\"type\":\"text\",\"text\":\"separate\"}]}\n\nevent: session.status_idle\nid: system-idle\ndata: {\"turn_id\":\"system-turn\"}\n\n")
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
	result, _ := executeQoderCloudTest(t, backend, context.Background(), "user text", ExecOptions{SystemPrompt: "system text"})
	if result.Status != "completed" || result.Output != "separate" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(eventPayload.Events) != 2 {
		t.Fatalf("events = %#v, want user + system", eventPayload.Events)
	}
	if eventPayload.Events[0].Type != "user.message" || eventPayload.Events[0].Content[0].Text != "user text" {
		t.Fatalf("first event must be the unmodified user message: %#v", eventPayload.Events[0])
	}
	if eventPayload.Events[1].Type != "system.message" || eventPayload.Events[1].Content[0].Text != "system text" {
		t.Fatalf("second event must be the separate system message: %#v", eventPayload.Events[1])
	}
}

func TestQoderCloudResumeAndResumeNotFound(t *testing.T) {
	t.Run("resume existing session", func(t *testing.T) {
		var unexpectedCreate atomic.Bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/sessions/resume-1":
				_, _ = io.WriteString(w, `{"id":"resume-1","status":"idle"}`)
			case r.Method == http.MethodPost && r.URL.Path == "/sessions/resume-1/events":
				_, _ = io.WriteString(w, `{"id":"resume-user","turn_id":"resume-turn"}`)
			case r.Method == http.MethodGet && r.URL.Path == "/sessions/resume-1/events/stream":
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "event: agent.message\nid: resume-answer\ndata: {\"turn_id\":\"resume-turn\",\"content\":[{\"type\":\"text\",\"text\":\"resumed\"}]}\n\nevent: session.status_idle\nid: resume-idle\ndata: {\"turn_id\":\"resume-turn\"}\n\n")
			case r.URL.Path == "/sessions" || strings.HasPrefix(r.URL.Path, "/agents/"):
				unexpectedCreate.Store(true)
				http.Error(w, "must not create", http.StatusInternalServerError)
			default:
				http.Error(w, "unexpected route", http.StatusNotFound)
			}
		}))
		defer server.Close()

		backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{})
		result, _ := executeQoderCloudTest(t, backend, context.Background(), "continue", ExecOptions{ResumeSessionID: "resume-1"})
		if result.Status != "completed" || result.Output != "resumed" || result.SessionID != "resume-1" {
			t.Fatalf("unexpected resume result: %+v", result)
		}
		if unexpectedCreate.Load() {
			t.Fatal("resume path attempted agent lookup or session creation")
		}
	})

	t.Run("missing session is positive resume rejection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "gone", http.StatusNotFound)
		}))
		defer server.Close()

		backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{})
		result, _ := executeQoderCloudTest(t, backend, context.Background(), "continue", ExecOptions{ResumeSessionID: "gone-session"})
		if result.Status != "failed" || !result.ResumeRejected || result.SessionID != "gone-session" {
			t.Fatalf("unexpected missing-resume result: %+v", result)
		}
	})
}

func TestQoderCloudResumeRejectsOnlyUnrecoverableSessionStates(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "terminated", body: `{"id":"poisoned","status":"terminated"}`},
		{name: "archived", body: `{"id":"poisoned","status":"idle","archived_at":"2026-08-09T12:00:00Z"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/sessions/poisoned" {
					_, _ = io.WriteString(w, tc.body)
					return
				}
				sent.Store(true)
				http.Error(w, "must not send to poisoned session", http.StatusInternalServerError)
			}))
			defer server.Close()

			backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{})
			result, _ := executeQoderCloudTest(t, backend, context.Background(), "continue", ExecOptions{ResumeSessionID: "poisoned"})
			if result.Status != "failed" || !result.ResumeRejected || !strings.Contains(result.Error, tc.name) {
				t.Fatalf("unrecoverable session did not request a fresh retry: %+v", result)
			}
			if sent.Load() {
				t.Fatal("backend posted an event to an unrecoverable session")
			}
		})
	}

	t.Run("running remains retryable with context intact", func(t *testing.T) {
		var sent atomic.Bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/sessions/busy" {
				_, _ = io.WriteString(w, `{"id":"busy","status":"running"}`)
				return
			}
			sent.Store(true)
			http.Error(w, "must wait for idle", http.StatusInternalServerError)
		}))
		defer server.Close()

		backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{})
		result, _ := executeQoderCloudTest(t, backend, context.Background(), "continue", ExecOptions{ResumeSessionID: "busy"})
		if result.Status != "failed" || result.ResumeRejected || !strings.Contains(result.Error, "running") {
			t.Fatalf("busy session was misclassified: %+v", result)
		}
		if sent.Load() {
			t.Fatal("backend posted an event before a running session became idle")
		}
	})
}

func TestQoderCloudPendingActionsFailClosedAndCancel(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame string
	}{
		{
			name:  "mcp permission ask",
			frame: "event: agent.mcp_tool_use\nid: mcp-1\ndata: {\"id\":\"mcp-1\",\"type\":\"agent.mcp_tool_use\",\"name\":\"remote\",\"evaluated_permission\":\"ask\"}\n\n",
		},
		{
			name:  "idle requires action",
			frame: "event: session.status_idle\nid: action-idle\ndata: {\"id\":\"action-idle\",\"type\":\"session.status_idle\",\"stop_reason\":{\"type\":\"requires_action\",\"event_ids\":[\"custom-1\"]}}\n\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cancelCalled := make(chan struct{})
			var cancelOnce sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/sessions":
					_, _ = io.WriteString(w, `{"id":"pending-session"}`)
				case "/sessions/pending-session/events":
					_, _ = io.WriteString(w, `{"data":[{"id":"pending-user"}]}`)
				case "/sessions/pending-session/events/stream":
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, tc.frame)
				case "/sessions/pending-session/cancel":
					cancelOnce.Do(func() { close(cancelCalled) })
					w.WriteHeader(http.StatusAccepted)
					_, _ = io.WriteString(w, `{"status":"canceling"}`)
				default:
					http.Error(w, "unexpected route", http.StatusNotFound)
				}
			}))
			defer server.Close()

			backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
			result, _ := executeQoderCloudTest(t, backend, context.Background(), "act", ExecOptions{})
			if result.Status != "failed" || result.ResumeRejected || !strings.Contains(result.Error, "unsupported interactive action") {
				t.Fatalf("pending action was not failed closed: %+v", result)
			}
			select {
			case <-cancelCalled:
			case <-time.After(time.Second):
				t.Fatal("pending action did not cancel remote session")
			}
		})
	}
}

func TestQoderCloudCustomToolResultContinuesTurn(t *testing.T) {
	var (
		handlerCalls  atomic.Int32
		resultPosts   atomic.Int32
		resultPayload qoderCloudCustomToolResultEvent
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			_, _ = io.WriteString(w, `{"id":"custom-session"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/sessions/custom-session/events":
			var payload struct {
				Events []json.RawMessage `json:"events"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode event payload: %v", err)
			}
			var envelope struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(payload.Events[0], &envelope)
			if envelope.Type == "user.message" {
				_, _ = io.WriteString(w, `{"data":[{"id":"custom-user","turn_id":"custom-turn"}]}`)
				return
			}
			resultPosts.Add(1)
			if err := json.Unmarshal(payload.Events[0], &resultPayload); err != nil {
				t.Errorf("decode custom result: %v", err)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"custom-result-accepted","turn_id":"custom-turn"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/sessions/custom-session/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				"event: agent.custom_tool_use",
				"id: custom-call-1",
				`data: {"id":"custom-call-1","type":"agent.custom_tool_use","turn_id":"custom-turn","name":"multica_get_issue","input":{"issue_id":"MUL-7"}}`,
				"",
				// Stream overlap after the accepted result must neither execute nor
				// submit the mutating request a second time.
				"event: agent.custom_tool_use",
				"id: custom-call-1",
				`data: {"id":"custom-call-1","type":"agent.custom_tool_use","turn_id":"custom-turn","name":"multica_get_issue","input":{"issue_id":"MUL-7"}}`,
				"",
				"event: session.status_idle",
				"id: custom-action-idle",
				`data: {"id":"custom-action-idle","type":"session.status_idle","turn_id":"custom-turn","stop_reason":{"type":"requires_action","event_ids":["custom-call-1"]}}`,
				"",
				// Replaying the resolved action status must not submit or emit the
				// cached result a second time.
				"event: session.status_idle",
				"id: custom-action-idle",
				`data: {"id":"custom-action-idle","type":"session.status_idle","turn_id":"custom-turn","stop_reason":{"type":"requires_action","event_ids":["custom-call-1"]}}`,
				"",
				"event: agent.message",
				"id: custom-answer",
				`data: {"id":"custom-answer","type":"agent.message","turn_id":"custom-turn","content":[{"type":"text","text":"tool complete"}]}`,
				"",
				"event: session.status_idle",
				"id: custom-idle",
				`data: {"id":"custom-idle","type":"session.status_idle","turn_id":"custom-turn"}`,
				"",
			}, "\n"))
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
	result, messages := executeQoderCloudTest(t, backend, context.Background(), "inspect", ExecOptions{
		CustomToolHandler: func(_ context.Context, call CustomToolCall) (CustomToolResult, error) {
			handlerCalls.Add(1)
			if call.CallID != "custom-call-1" || call.Name != "multica_get_issue" || call.Input["issue_id"] != "MUL-7" {
				t.Errorf("unexpected custom call: %#v", call)
			}
			return CustomToolResult{Content: `{"id":"issue-7"}`}, nil
		},
	})
	if result.Status != "completed" || result.Output != "tool complete" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if handlerCalls.Load() != 1 || resultPosts.Load() != 1 {
		t.Fatalf("custom calls: handler=%d result_posts=%d, want 1/1", handlerCalls.Load(), resultPosts.Load())
	}
	if resultPayload.Type != "user.custom_tool_result" || resultPayload.CustomToolUseID != "custom-call-1" || resultPayload.IsError || resultPayload.Content[0].Text != `{"id":"issue-7"}` {
		t.Fatalf("unexpected custom result payload: %#v", resultPayload)
	}
	assertQoderCloudMessage(t, messages, func(message Message) bool {
		return message.Type == MessageToolUse && message.Tool == "multica_get_issue" && message.CallID == "custom-call-1"
	}, "custom tool use")
	assertQoderCloudMessage(t, messages, func(message Message) bool {
		return message.Type == MessageToolResult && message.Tool == "multica_get_issue" && message.CallID == "custom-call-1" && message.Output == `{"id":"issue-7"}`
	}, "custom tool result")
	var toolUses, toolResults int
	for _, message := range messages {
		if message.Type == MessageToolUse && message.CallID == "custom-call-1" {
			toolUses++
		}
		if message.Type == MessageToolResult && message.CallID == "custom-call-1" {
			toolResults++
		}
	}
	if toolUses != 1 || toolResults != 1 {
		t.Fatalf("custom messages: uses=%d results=%d, want 1/1", toolUses, toolResults)
	}
}

// Regression: ISSUE-001 — batched custom tools were posted before Qoder declared them pending.
// Found by /qa on 2026-08-10.
// Report: docs/qoder-cloud-agents.md#browser-e2e-verification-2026-08-10
func TestQoderCloudCustomToolsWaitForBatchedRequiredActions(t *testing.T) {
	var (
		handlerCalls atomic.Int32
		resultPosts  atomic.Int32
		statusSent   atomic.Bool
		postMu       sync.Mutex
		postedIDs    []string
	)
	releaseStatus := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseStatus) }) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			_, _ = io.WriteString(w, `{"id":"batched-session"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/sessions/batched-session/events":
			var payload struct {
				Events []json.RawMessage `json:"events"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode event payload: %v", err)
				return
			}
			var envelope struct {
				Type            string `json:"type"`
				CustomToolUseID string `json:"custom_tool_use_id"`
			}
			if err := json.Unmarshal(payload.Events[0], &envelope); err != nil {
				t.Errorf("decode event envelope: %v", err)
				return
			}
			if envelope.Type == "user.message" {
				_, _ = io.WriteString(w, `{"data":[{"id":"batched-user","turn_id":"batched-turn"}]}`)
				return
			}
			if !statusSent.Load() {
				t.Error("custom result was posted before requires_action")
			}
			resultPosts.Add(1)
			postMu.Lock()
			postedIDs = append(postedIDs, envelope.CustomToolUseID)
			postMu.Unlock()
			_, _ = fmt.Fprintf(w, `{"data":[{"id":"accepted-%s","turn_id":"batched-turn"}]}`, envelope.CustomToolUseID)
		case r.Method == http.MethodGet && r.URL.Path == "/sessions/batched-session/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				"event: agent.custom_tool_use",
				"id: batched-call-1",
				`data: {"id":"batched-call-1","type":"agent.custom_tool_use","turn_id":"batched-turn","name":"multica_get_issue","input":{"issue_id":"MUL-3"}}`,
				"",
				"event: agent.custom_tool_use",
				"id: batched-call-2",
				`data: {"id":"batched-call-2","type":"agent.custom_tool_use","turn_id":"batched-turn","name":"multica_list_issue_comments","input":{"issue_id":"MUL-3"}}`,
				"",
				"",
			}, "\n"))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-releaseStatus
			statusSent.Store(true)
			_, _ = io.WriteString(w, strings.Join([]string{
				"event: session.status_idle",
				"id: batched-action-idle",
				`data: {"id":"batched-action-idle","type":"session.status_idle","turn_id":"batched-turn","stop_reason":{"type":"requires_action","event_ids":["batched-call-1","batched-call-2"]}}`,
				"",
				"event: agent.message",
				"id: batched-answer",
				`data: {"id":"batched-answer","type":"agent.message","turn_id":"batched-turn","content":[{"type":"text","text":"both complete"}]}`,
				"",
				"event: session.status_idle",
				"id: batched-idle",
				`data: {"id":"batched-idle","type":"session.status_idle","turn_id":"batched-turn"}`,
				"",
			}, "\n"))
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
	session, err := backend.Execute(context.Background(), "inspect twice", ExecOptions{
		CustomToolHandler: func(_ context.Context, call CustomToolCall) (CustomToolResult, error) {
			handlerCalls.Add(1)
			return CustomToolResult{Content: `{"call_id":"` + call.CallID + `"}`}, nil
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var messages []Message
	for len(messages) < 3 {
		select {
		case message, ok := <-session.Messages:
			if !ok {
				t.Fatalf("message stream closed before both custom tools were buffered: %+v", messages)
			}
			messages = append(messages, message)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for buffered custom tool messages")
		}
	}
	var bufferedUses int
	for _, message := range messages {
		if message.Type == MessageToolUse {
			bufferedUses++
		}
	}
	if bufferedUses != 2 {
		t.Fatalf("buffered tool uses=%d, want 2; messages=%+v", bufferedUses, messages)
	}
	if handlerCalls.Load() != 0 || resultPosts.Load() != 0 {
		t.Fatalf("custom tools ran before requires_action: handler=%d posts=%d", handlerCalls.Load(), resultPosts.Load())
	}
	releaseOnce.Do(func() { close(releaseStatus) })

	messagesDone := make(chan struct{})
	go func() {
		defer close(messagesDone)
		for message := range session.Messages {
			messages = append(messages, message)
		}
	}()
	var result Result
	select {
	case result = <-session.Result:
		<-messagesDone
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for batched custom tool result")
	}
	if result.Status != "completed" || result.Output != "both complete" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if handlerCalls.Load() != 2 || resultPosts.Load() != 2 {
		t.Fatalf("custom calls: handler=%d result_posts=%d, want 2/2", handlerCalls.Load(), resultPosts.Load())
	}
	postMu.Lock()
	defer postMu.Unlock()
	if strings.Join(postedIDs, ",") != "batched-call-1,batched-call-2" {
		t.Fatalf("posted custom result IDs=%v, want required-action order", postedIDs)
	}
}

func TestQoderCloudCustomToolBatchFailsClosedBeforeExecution(t *testing.T) {
	tests := []struct {
		name       string
		finalFrame string
	}{
		{
			name:       "unknown required action",
			finalFrame: "event: session.status_idle\nid: invalid-action-idle\ndata: {\"stop_reason\":{\"type\":\"requires_action\",\"event_ids\":[\"known-call\",\"missing-call\"]}}\n\n",
		},
		{
			name:       "terminal idle with unsent request",
			finalFrame: "event: session.status_idle\nid: premature-idle\ndata: {}\n\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var handlerCalls, resultPosts atomic.Int32
			cancelCalled := make(chan struct{})
			var cancelOnce sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/sessions":
					_, _ = io.WriteString(w, `{"id":"fail-closed-session"}`)
				case r.Method == http.MethodPost && r.URL.Path == "/sessions/fail-closed-session/events":
					body, _ := io.ReadAll(r.Body)
					if bytes.Contains(body, []byte("user.custom_tool_result")) {
						resultPosts.Add(1)
					}
					_, _ = io.WriteString(w, `{"data":[{"id":"fail-closed-user"}]}`)
				case r.Method == http.MethodGet && r.URL.Path == "/sessions/fail-closed-session/events/stream":
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "event: agent.custom_tool_use\nid: known-call\ndata: {\"id\":\"known-call\",\"type\":\"agent.custom_tool_use\",\"name\":\"multica_create_issue\",\"input\":{\"title\":\"must not run\"}}\n\n"+tc.finalFrame)
				case r.Method == http.MethodPost && r.URL.Path == "/sessions/fail-closed-session/cancel":
					cancelOnce.Do(func() { close(cancelCalled) })
					w.WriteHeader(http.StatusAccepted)
					_, _ = io.WriteString(w, `{"status":"canceling"}`)
				default:
					http.Error(w, "unexpected route", http.StatusNotFound)
				}
			}))
			defer server.Close()

			backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
			result, _ := executeQoderCloudTest(t, backend, context.Background(), "do not mutate", ExecOptions{
				CustomToolHandler: func(context.Context, CustomToolCall) (CustomToolResult, error) {
					handlerCalls.Add(1)
					return CustomToolResult{Content: `{"created":true}`}, nil
				},
			})
			if result.Status != "failed" || !strings.Contains(result.Error, "unsupported interactive action") {
				t.Fatalf("unsafe pending batch did not fail closed: %+v", result)
			}
			if handlerCalls.Load() != 0 || resultPosts.Load() != 0 {
				t.Fatalf("unsafe pending batch caused side effects: handler=%d posts=%d", handlerCalls.Load(), resultPosts.Load())
			}
			select {
			case <-cancelCalled:
			case <-time.After(time.Second):
				t.Fatal("unsafe pending batch did not cancel remote session")
			}
		})
	}
}

func TestQoderCloudCustomToolBatchRetryKeepsRequiredActionCursor(t *testing.T) {
	originalDelay := qoderCloudReconnectDelay
	qoderCloudReconnectDelay = time.Millisecond
	t.Cleanup(func() { qoderCloudReconnectDelay = originalDelay })

	var (
		streamCalls atomic.Int32
		mu          sync.Mutex
		cursors     []string
		handlers    = map[string]int{}
		posts       = map[string]int{}
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			_, _ = io.WriteString(w, `{"id":"batch-retry-session"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/sessions/batch-retry-session/events":
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte("user.custom_tool_result")) {
				_, _ = io.WriteString(w, `{"data":[{"id":"batch-retry-user"}]}`)
				return
			}
			var payload struct {
				Events []qoderCloudCustomToolResultEvent `json:"events"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("decode custom result: %v", err)
				return
			}
			callID := payload.Events[0].CustomToolUseID
			mu.Lock()
			posts[callID]++
			attempt := posts[callID]
			mu.Unlock()
			if callID == "retry-call-b" && attempt == 1 {
				http.Error(w, "temporary second result failure", http.StatusServiceUnavailable)
				return
			}
			_, _ = fmt.Fprintf(w, `{"data":[{"id":"accepted-%s"}]}`, callID)
		case r.Method == http.MethodGet && r.URL.Path == "/sessions/batch-retry-session/events/stream":
			mu.Lock()
			cursors = append(cursors, r.Header.Get("Last-Event-ID"))
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			switch streamCalls.Add(1) {
			case 1:
				_, _ = io.WriteString(w, "event: agent.custom_tool_use\nid: retry-call-a\ndata: {\"id\":\"retry-call-a\",\"type\":\"agent.custom_tool_use\",\"name\":\"tool_a\",\"input\":{}}\n\nevent: agent.custom_tool_use\nid: retry-call-b\ndata: {\"id\":\"retry-call-b\",\"type\":\"agent.custom_tool_use\",\"name\":\"tool_b\",\"input\":{}}\n\nevent: session.status_idle\nid: retry-batch-action\ndata: {\"id\":\"retry-batch-action\",\"type\":\"session.status_idle\",\"stop_reason\":{\"type\":\"requires_action\",\"event_ids\":[\"retry-call-a\",\"retry-call-b\"]}}\n\n")
			case 2:
				_, _ = io.WriteString(w, "event: session.status_idle\nid: retry-batch-action\ndata: {\"id\":\"retry-batch-action\",\"type\":\"session.status_idle\",\"stop_reason\":{\"type\":\"requires_action\",\"event_ids\":[\"retry-call-a\",\"retry-call-b\"]}}\n\n")
			default:
				_, _ = io.WriteString(w, "event: agent.message\nid: retry-batch-answer\ndata: {\"content\":[{\"type\":\"text\",\"text\":\"batch recovered\"}]}\n\nevent: session.status_idle\nid: retry-batch-idle\ndata: {}\n\n")
			}
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
	result, messages := executeQoderCloudTest(t, backend, context.Background(), "retry batch", ExecOptions{
		CustomToolHandler: func(_ context.Context, call CustomToolCall) (CustomToolResult, error) {
			mu.Lock()
			handlers[call.CallID]++
			mu.Unlock()
			return CustomToolResult{Content: `{"ok":true}`}, nil
		},
	})
	if result.Status != "completed" || result.Output != "batch recovered" {
		t.Fatalf("unexpected result: %+v", result)
	}

	mu.Lock()
	defer mu.Unlock()
	if handlers["retry-call-a"] != 1 || handlers["retry-call-b"] != 1 {
		t.Fatalf("handler calls=%v, want exactly once per tool", handlers)
	}
	if posts["retry-call-a"] != 1 || posts["retry-call-b"] != 2 {
		t.Fatalf("result posts=%v, want A once and B retried once", posts)
	}
	wantCursors := []string{"batch-retry-user", "retry-call-b", "accepted-retry-call-b"}
	if strings.Join(cursors, ",") != strings.Join(wantCursors, ",") {
		t.Fatalf("stream cursors=%v, want %v", cursors, wantCursors)
	}
	resultMessages := map[string]int{}
	for _, message := range messages {
		if message.Type == MessageToolResult {
			resultMessages[message.CallID]++
		}
	}
	if resultMessages["retry-call-a"] != 1 || resultMessages["retry-call-b"] != 1 {
		t.Fatalf("tool result messages=%v, want exactly once per tool", resultMessages)
	}
}

func TestQoderCloudCustomToolReplayResendsCachedResultWithoutReexecution(t *testing.T) {
	originalDelay := qoderCloudReconnectDelay
	qoderCloudReconnectDelay = time.Millisecond
	t.Cleanup(func() { qoderCloudReconnectDelay = originalDelay })

	var handlerCalls, resultPosts, streamCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			_, _ = io.WriteString(w, `{"id":"replay-tool-session"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/sessions/replay-tool-session/events":
			body, _ := io.ReadAll(r.Body)
			if bytes.Contains(body, []byte("user.custom_tool_result")) {
				if resultPosts.Add(1) == 1 {
					http.Error(w, "ambiguous temporary failure", http.StatusServiceUnavailable)
					return
				}
				_, _ = io.WriteString(w, `{"data":[{"id":"replay-result-accepted"}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"replay-user"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/sessions/replay-tool-session/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			streamCalls.Add(1)
			_, _ = io.WriteString(w, "event: agent.custom_tool_use\nid: replay-call\ndata: {\"id\":\"replay-call\",\"type\":\"agent.custom_tool_use\",\"name\":\"multica_create_issue\",\"input\":{\"title\":\"once\"}}\n\nevent: session.status_idle\nid: replay-action-idle\ndata: {\"id\":\"replay-action-idle\",\"type\":\"session.status_idle\",\"stop_reason\":{\"type\":\"requires_action\",\"event_ids\":[\"replay-call\"]}}\n\n")
			if resultPosts.Load() > 1 {
				_, _ = io.WriteString(w, "event: agent.message\nid: replay-answer\ndata: {\"content\":[{\"type\":\"text\",\"text\":\"created once\"}]}\n\nevent: session.status_idle\nid: replay-idle\ndata: {}\n\n")
			}
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
	result, _ := executeQoderCloudTest(t, backend, context.Background(), "create", ExecOptions{
		CustomToolHandler: func(context.Context, CustomToolCall) (CustomToolResult, error) {
			handlerCalls.Add(1)
			return CustomToolResult{Content: `{"id":"created"}`}, nil
		},
	})
	if result.Status != "completed" || result.Output != "created once" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if handlerCalls.Load() != 1 || resultPosts.Load() != 2 || streamCalls.Load() < 2 {
		t.Fatalf("replay counts handler=%d result_posts=%d streams=%d", handlerCalls.Load(), resultPosts.Load(), streamCalls.Load())
	}
}

func TestQoderCloudCustomToolFailuresReturnErrorResults(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		frame   string
		handler CustomToolHandler
		want    string
	}{
		{
			name:  "missing handler",
			id:    "missing-handler",
			frame: `{"id":"missing-handler","type":"agent.custom_tool_use","name":"multica_list_issues","input":{}}`,
			want:  "no client-side custom tool handler",
		},
		{
			name:  "handler error",
			id:    "handler-error",
			frame: `{"id":"handler-error","type":"agent.custom_tool_use","name":"unknown","input":{}}`,
			handler: func(context.Context, CustomToolCall) (CustomToolResult, error) {
				return CustomToolResult{}, errors.New("tool is not allowlisted")
			},
			want: "tool is not allowlisted",
		},
		{
			name:  "malformed input",
			id:    "bad-input",
			frame: `{"id":"bad-input","type":"agent.custom_tool_use","name":"multica_get_issue","input":["not","object"]}`,
			handler: func(context.Context, CustomToolCall) (CustomToolResult, error) {
				t.Fatal("malformed input reached handler")
				return CustomToolResult{}, nil
			},
			want: "must be a JSON object",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotResult qoderCloudCustomToolResultEvent
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/sessions":
					_, _ = io.WriteString(w, `{"id":"error-session"}`)
				case r.Method == http.MethodPost && r.URL.Path == "/sessions/error-session/events":
					body, _ := io.ReadAll(r.Body)
					if bytes.Contains(body, []byte("user.custom_tool_result")) {
						var payload struct {
							Events []qoderCloudCustomToolResultEvent `json:"events"`
						}
						_ = json.Unmarshal(body, &payload)
						gotResult = payload.Events[0]
						_, _ = io.WriteString(w, `{"data":[{"id":"error-result"}]}`)
						return
					}
					_, _ = io.WriteString(w, `{"data":[{"id":"error-user"}]}`)
				case r.Method == http.MethodGet && r.URL.Path == "/sessions/error-session/events/stream":
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "event: agent.custom_tool_use\nid: call-frame\ndata: "+tc.frame+"\n\nevent: session.status_idle\nid: error-action-idle\ndata: {\"stop_reason\":{\"type\":\"requires_action\",\"event_ids\":[\""+tc.id+"\"]}}\n\nevent: agent.message\nid: recovered\ndata: {\"content\":[{\"type\":\"text\",\"text\":\"recovered\"}]}\n\nevent: session.status_idle\nid: error-idle\ndata: {}\n\n")
				default:
					http.Error(w, "unexpected route", http.StatusNotFound)
				}
			}))
			defer server.Close()

			backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
			result, _ := executeQoderCloudTest(t, backend, context.Background(), "try", ExecOptions{CustomToolHandler: tc.handler})
			if result.Status != "completed" || result.Output != "recovered" {
				t.Fatalf("turn did not recover from tool error: %+v", result)
			}
			if !gotResult.IsError || !strings.Contains(gotResult.Content[0].Text, tc.want) {
				t.Fatalf("error result = %#v, want substring %q", gotResult, tc.want)
			}
		})
	}
}

func TestQoderCloudStreamRetriesTransientFailures(t *testing.T) {
	originalDelay := qoderCloudReconnectDelay
	qoderCloudReconnectDelay = time.Millisecond
	t.Cleanup(func() { qoderCloudReconnectDelay = originalDelay })

	t.Run("http 5xx and rescheduled", func(t *testing.T) {
		var streamCalls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/sessions":
				_, _ = io.WriteString(w, `{"id":"retry-session"}`)
			case "/sessions/retry-session/events":
				_, _ = io.WriteString(w, `{"data":[{"id":"retry-user"}]}`)
			case "/sessions/retry-session/events/stream":
				call := streamCalls.Add(1)
				if call <= 3 {
					http.Error(w, "temporary", http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if call == 4 {
					_, _ = io.WriteString(w, "event: session.status_rescheduled\nid: rescheduled-1\ndata: {\"type\":\"session.status_rescheduled\"}\n\n")
					return
				}
				_, _ = io.WriteString(w, "event: agent.message\nid: retry-answer\ndata: {\"content\":[{\"type\":\"text\",\"text\":\"recovered\"}]}\n\nevent: session.status_idle\nid: retry-idle\ndata: {}\n\n")
			default:
				http.Error(w, "unexpected route", http.StatusNotFound)
			}
		}))
		defer server.Close()

		backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
		result, _ := executeQoderCloudTest(t, backend, context.Background(), "retry", ExecOptions{})
		if result.Status != "completed" || result.Output != "recovered" || streamCalls.Load() != 5 {
			t.Fatalf("transient HTTP stream did not recover: calls=%d result=%+v", streamCalls.Load(), result)
		}
	})

	t.Run("transport", func(t *testing.T) {
		var transportFailures atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/sessions":
				_, _ = io.WriteString(w, `{"id":"transport-session"}`)
			case "/sessions/transport-session/events":
				_, _ = io.WriteString(w, `{"data":[{"id":"transport-user"}]}`)
			case "/sessions/transport-session/events/stream":
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "event: agent.message\nid: transport-answer\ndata: {\"content\":[{\"type\":\"text\",\"text\":\"network recovered\"}]}\n\nevent: session.status_idle\nid: transport-idle\ndata: {}\n\n")
			default:
				http.Error(w, "unexpected route", http.StatusNotFound)
			}
		}))
		defer server.Close()

		transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			if strings.HasSuffix(request.URL.Path, "/events/stream") && transportFailures.Add(1) <= 3 {
				return nil, fmt.Errorf("temporary transport failure")
			}
			return http.DefaultTransport.RoundTrip(request)
		})
		backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{
			AgentVersion: 2,
			HTTPClient:   &http.Client{Transport: transport},
		})
		result, _ := executeQoderCloudTest(t, backend, context.Background(), "retry", ExecOptions{})
		if result.Status != "completed" || result.Output != "network recovered" || transportFailures.Load() != 4 {
			t.Fatalf("transport stream did not recover: calls=%d result=%+v", transportFailures.Load(), result)
		}
	})
}

func TestQoderCloudReconnectUsesLastEventIDAndDeduplicatesBufferedMessage(t *testing.T) {
	var (
		streamCalls atomic.Int32
		mu          sync.Mutex
		cursors     []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sessions":
			_, _ = io.WriteString(w, `{"id":"reconnect-session"}`)
		case "/sessions/reconnect-session/events":
			_, _ = io.WriteString(w, `{"data":[{"id":"reconnect-user","turn_id":"reconnect-turn"}]}`)
		case "/sessions/reconnect-session/events/stream":
			mu.Lock()
			cursors = append(cursors, r.Header.Get("Last-Event-ID"))
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			call := streamCalls.Add(1)
			_, _ = io.WriteString(w, "event: agent.message\nid: reconnect-message\ndata: {\"turn_id\":\"reconnect-turn\",\"content\":[{\"type\":\"text\",\"text\":\"once\"}]}\n\n")
			if call > 1 {
				_, _ = io.WriteString(w, "event: session.status_idle\nid: reconnect-idle\ndata: {\"turn_id\":\"reconnect-turn\"}\n\n")
			}
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
	result, _ := executeQoderCloudTest(t, backend, context.Background(), "hello", ExecOptions{})
	if result.Status != "completed" || result.Output != "once" {
		t.Fatalf("unexpected reconnect result: %+v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(cursors) < 2 || cursors[0] != "reconnect-user" || cursors[1] != "reconnect-message" {
		t.Fatalf("unexpected reconnect cursors: %#v", cursors)
	}
}

func TestQoderCloudTruncatedFrameDoesNotAdvanceReconnectCursor(t *testing.T) {
	var (
		streamCalls atomic.Int32
		mu          sync.Mutex
		cursors     []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sessions":
			_, _ = io.WriteString(w, `{"id":"truncated-session"}`)
		case "/sessions/truncated-session/events":
			_, _ = io.WriteString(w, `{"data":[{"id":"accepted-user","turn_id":"truncated-turn"},{"id":"accepted-system"}]}`)
		case "/sessions/truncated-session/events/stream":
			mu.Lock()
			cursors = append(cursors, r.Header.Get("Last-Event-ID"))
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			if streamCalls.Add(1) == 1 {
				// event_start and buffered agent.message deliberately share an id.
				// Neither that lifecycle frame nor the truncated JSON may advance
				// the cursor past the last accepted outbound event.
				_, _ = io.WriteString(w, "event: event_start\nid: shared-message-id\ndata: {\"id\":\"shared-message-id\",\"type\":\"event_start\"}\n\n")
				_, _ = io.WriteString(w, "event: agent.message\nid: shared-message-id\ndata: {\"id\":\"shared-message-id\",\"type\":\"agent.message\",\"turn_id\":\"truncated-turn\",\"content\":[{\"type\":\"text\",\"text\":\"cut off")
				return
			}
			_, _ = io.WriteString(w, "event: agent.message\nid: shared-message-id\ndata: {\"id\":\"shared-message-id\",\"type\":\"agent.message\",\"turn_id\":\"truncated-turn\",\"content\":[{\"type\":\"text\",\"text\":\"complete once\"}]}\n\nevent: session.status_idle\nid: truncated-idle\ndata: {\"turn_id\":\"truncated-turn\"}\n\n")
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
	result, _ := executeQoderCloudTest(t, backend, context.Background(), "hello", ExecOptions{SystemPrompt: "stay exact"})
	if result.Status != "completed" || result.Output != "complete once" {
		t.Fatalf("truncated frame recovery lost or duplicated output: %+v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(cursors) != 2 || cursors[0] != "accepted-system" || cursors[1] != "accepted-system" {
		t.Fatalf("truncated frame advanced reconnect cursor: %#v", cursors)
	}
}

func TestQoderCloudCancellationPostsCancelWithIndependentContext(t *testing.T) {
	streamStarted := make(chan struct{})
	cancelCalled := make(chan struct{})
	var streamOnce, cancelOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sessions":
			_, _ = io.WriteString(w, `{"id":"cancel-session"}`)
		case "/sessions/cancel-session/events":
			_, _ = io.WriteString(w, `{"data":[{"id":"cancel-user"}]}`)
		case "/sessions/cancel-session/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			streamOnce.Do(func() { close(streamStarted) })
			<-r.Context().Done()
		case "/sessions/cancel-session/cancel":
			if r.Method != http.MethodPost {
				t.Errorf("cancel method = %s, want POST", r.Method)
			}
			body, _ := io.ReadAll(r.Body)
			if len(body) != 0 {
				t.Errorf("cancel body = %q, want empty", body)
			}
			cancelOnce.Do(func() { close(cancelCalled) })
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"status":"canceling"}`)
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend := newQoderCloudTestBackend(t, server.URL, QoderCloudConfig{AgentVersion: 2})
	ctx, cancel := context.WithCancel(context.Background())
	session, err := backend.Execute(ctx, "wait", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case <-streamStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not start")
	}
	cancel()
	select {
	case result := <-session.Result:
		if result.Status != "cancelled" || result.SessionID != "cancel-session" {
			t.Fatalf("unexpected cancellation result: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for cancellation result")
	}
	select {
	case <-cancelCalled:
	case <-time.After(time.Second):
		t.Fatal("remote cancel endpoint was not called")
	}
}

func TestQoderCloudErrorsRedactPAT(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			secret := fmt.Sprintf("fake-secret-%d-that-must-not-leak", status)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "request failed for Bearer "+secret, status)
			}))
			defer server.Close()

			backend, err := New("qodercloud", Config{QoderCloud: QoderCloudConfig{
				BaseURL:       server.URL,
				PAT:           secret,
				AgentID:       "agent-1",
				EnvironmentID: "environment-1",
			}})
			if err != nil {
				t.Fatalf("new backend: %v", err)
			}
			result, _ := executeQoderCloudTest(t, backend, context.Background(), "hello", ExecOptions{})
			if strings.Contains(result.Error, secret) {
				t.Fatalf("result leaked PAT: %q", result.Error)
			}
			if !strings.Contains(result.Error, "[REDACTED]") || result.Status != "failed" || result.ResumeRejected {
				t.Fatalf("unexpected redacted error: %+v", result)
			}
		})
	}
}

func TestParseQoderCloudSSEAcceptsLargeBufferedFrame(t *testing.T) {
	large := strings.Repeat("x", 256*1024)
	payload, err := json.Marshal(qoderCloudEvent{
		ID:      "large-event",
		Type:    "agent.message",
		Content: json.RawMessage(fmt.Sprintf(`[{"type":"text","text":%q}]`, large)),
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	wire := "event: agent.message\nid: large-event\ndata: " + string(payload) + "\n\n"
	var got qoderCloudSSEEvent
	done, err := parseQoderCloudSSE(strings.NewReader(wire), func(event qoderCloudSSEEvent) (bool, error) {
		got = event
		return true, nil
	})
	if err != nil || !done {
		t.Fatalf("parse large frame: done=%v err=%v", done, err)
	}
	if got.ID != "large-event" || len(got.Data) <= 64*1024 {
		t.Fatalf("large frame was truncated: id=%q bytes=%d", got.ID, len(got.Data))
	}
}

func newQoderCloudTestBackend(t *testing.T, baseURL string, override QoderCloudConfig) Backend {
	t.Helper()
	cfg := QoderCloudConfig{
		BaseURL:       baseURL,
		PAT:           qoderCloudTestPAT,
		AgentID:       "agent-1",
		EnvironmentID: "environment-1",
		AgentVersion:  override.AgentVersion,
		HTTPClient:    override.HTTPClient,
	}
	backend, err := New("qodercloud", Config{Logger: slog.Default(), QoderCloud: cfg})
	if err != nil {
		t.Fatalf("new qoder cloud backend: %v", err)
	}
	return backend
}

func executeQoderCloudTest(t *testing.T, backend Backend, ctx context.Context, prompt string, opts ExecOptions) (Result, []Message) {
	t.Helper()
	session, err := backend.Execute(ctx, prompt, opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var messages []Message
	messagesDone := make(chan struct{})
	go func() {
		defer close(messagesDone)
		for message := range session.Messages {
			messages = append(messages, message)
		}
	}()
	select {
	case result := <-session.Result:
		<-messagesDone
		return result, messages
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Qoder Cloud result")
		return Result{}, nil
	}
}

func assertQoderCloudMessage(t *testing.T, messages []Message, match func(Message) bool, description string) {
	t.Helper()
	for _, message := range messages {
		if match(message) {
			return
		}
	}
	t.Errorf("missing %s in messages: %+v", description, messages)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
