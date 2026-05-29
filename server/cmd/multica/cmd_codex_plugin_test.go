package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func TestCodexPluginSchemaJSONIncludesRequiredTools(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "json", "")
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatalf("Flags().Set(output) error = %v", err)
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runCodexPluginSchema(cmd, nil); err != nil {
		t.Fatalf("runCodexPluginSchema() error = %v", err)
	}

	var got codexPluginSchema
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output=%q", err, buf.String())
	}

	if got.DefaultSource != "codex_app_plugin" {
		t.Fatalf("DefaultSource = %q, want codex_app_plugin", got.DefaultSource)
	}

	seen := map[string]bool{}
	for _, tool := range got.Tools {
		seen[tool.Name] = true
	}
	for _, name := range []string{
		"issue_search",
		"issue_get",
		"session_bind",
		"runtime_event_append",
		"conversation_sync",
		"comment_add",
		"usage_update",
		"attachment_upload",
	} {
		if !seen[name] {
			t.Fatalf("schema missing tool %q", name)
		}
	}
}

func TestCodexPluginSchemaRejectsUnknownOutput(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "yaml", "")

	err := runCodexPluginSchema(cmd, nil)
	if err == nil {
		t.Fatal("expected error for unsupported output")
	}
}

func TestRunCodexPluginBindPostsBinding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", "")
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/OPE-1493":
			json.NewEncoder(w).Encode(map[string]any{"id": "11111111-1111-4111-8111-111111111111", "identifier": "OPE-1493"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/11111111-1111-4111-8111-111111111111/local-runs":
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode posted body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "run-1", "issue_id": "11111111-1111-4111-8111-111111111111", "status": "running", "top_comment_id": "comment-1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cmd := testCodexPluginBindCommand(srv.URL)
	if err := runCodexPluginBind(cmd, []string{"OPE-1493"}); err != nil {
		t.Fatalf("runCodexPluginBind() error = %v", err)
	}
	if posted["source_key"] != "thread-1:bind" || posted["cli_name"] != "codex_app" || posted["comments_mode"] != "thread" {
		t.Fatalf("posted body = %+v", posted)
	}
}

func TestRunCodexPluginEventPostsEvent(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "")
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/OPE-1493":
			json.NewEncoder(w).Encode(map[string]any{"id": "11111111-1111-4111-8111-111111111111", "identifier": "OPE-1493"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/local-runs/binding-1/messages":
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode posted body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{"task_id": "binding-1", "seq": 1, "type": "final", "content": "done"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cmd := testCodexPluginEventCommand(srv.URL)
	if err := runCodexPluginEvent(cmd, []string{"OPE-1493"}); err != nil {
		t.Fatalf("runCodexPluginEvent() error = %v", err)
	}
	if posted["type"] != "final" || posted["content"] != "done" {
		t.Fatalf("posted body = %+v", posted)
	}
}

func TestRunCodexPluginUsagePostsUsage(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "")
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/OPE-1493":
			json.NewEncoder(w).Encode(map[string]any{"id": "11111111-1111-4111-8111-111111111111", "identifier": "OPE-1493"})
		case r.Method == http.MethodPut && r.URL.Path == "/api/local-runs/binding-1/usage":
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode posted body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cmd := testCodexPluginUsageCommand(srv.URL)
	if err := runCodexPluginUsage(cmd, []string{"OPE-1493"}); err != nil {
		t.Fatalf("runCodexPluginUsage() error = %v", err)
	}
	usage := posted["usage"].([]any)[0].(map[string]any)
	if usage["model"] != "gpt-5.1-codex" || usage["input_tokens"].(float64) != 100 || usage["output_tokens"].(float64) != 25 {
		t.Fatalf("posted body = %+v", posted)
	}
}

func TestRunCodexPluginMCPServerListsTools(t *testing.T) {
	input := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n")
	var output bytes.Buffer

	cmd := testCodexPluginMCPCommand("")
	if err := runCodexPluginMCPServer(cmd, input, &output); err != nil {
		t.Fatalf("runCodexPluginMCPServer() error = %v", err)
	}

	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output=%q", err, output.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected MCP error: %+v", resp.Error)
	}
	seen := map[string]bool{}
	for _, tool := range resp.Result.Tools {
		seen[tool.Name] = true
	}
	for _, name := range []string{"issue_search", "issue_get", "session_bind", "runtime_event_append", "conversation_sync", "comment_add", "usage_update"} {
		if !seen[name] {
			t.Fatalf("MCP tools/list missing %q in %+v", name, resp.Result.Tools)
		}
	}
	if seen["attachment_upload"] {
		t.Fatalf("attachment_upload should stay out of the first stdio MCP implementation")
	}
}

func TestRunCodexPluginMCPServerCallsSessionBind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", "")
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/OPE-1493":
			json.NewEncoder(w).Encode(map[string]any{"id": "11111111-1111-4111-8111-111111111111", "identifier": "OPE-1493"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/11111111-1111-4111-8111-111111111111/local-runs":
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode posted body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "binding-1", "issue_id": "11111111-1111-4111-8111-111111111111", "status": "running", "top_comment_id": "comment-1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	input := bytes.NewBufferString(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"session_bind","arguments":{"issue_id":"OPE-1493","codex_thread_id":"thread-1","project_folder":"/tmp/project","branch":"feat/OPE-1493-codex-plugin","source_key":"thread-1:bind"}}}` + "\n")
	var output bytes.Buffer
	cmd := testCodexPluginMCPCommand(srv.URL)
	if err := runCodexPluginMCPServer(cmd, input, &output); err != nil {
		t.Fatalf("runCodexPluginMCPServer() error = %v", err)
	}

	if posted["source_key"] != "thread-1:bind" || posted["cli_name"] != "codex_app" || posted["comments_mode"] != "thread" {
		t.Fatalf("posted body = %+v", posted)
	}

	var resp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output=%q", err, output.String())
	}
	if resp.Error != nil || resp.Result.IsError {
		t.Fatalf("unexpected MCP error response: %s", output.String())
	}
	if len(resp.Result.Content) != 1 || resp.Result.Content[0].Type != "text" {
		t.Fatalf("unexpected MCP content: %+v", resp.Result.Content)
	}
	var envelope struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &envelope); err != nil {
		t.Fatalf("decode MCP tool envelope: %v; text=%q", err, resp.Result.Content[0].Text)
	}
	if !envelope.OK || envelope.Data["binding_id"] != "binding-1" || envelope.Data["top_comment_id"] != "comment-1" {
		t.Fatalf("unexpected MCP tool envelope: %+v", envelope)
	}
}

func TestRunCodexPluginMCPServerEndToEndSyncDedupe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", "")
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	const issueID = "11111111-1111-4111-8111-111111111111"

	var eventPosts int
	var conversationPosts int
	var commentPosts int
	var usagePosts int
	var conversationBodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/search":
			json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{
					"id":         issueID,
					"identifier": "OPE-1493",
					"title":      "Codex plugin sync test",
				}},
				"total": 1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/OPE-1493":
			json.NewEncoder(w).Encode(map[string]any{
				"id":         issueID,
				"identifier": "OPE-1493",
				"title":      "Codex plugin sync test",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+issueID+"/local-runs":
			json.NewEncoder(w).Encode(map[string]any{
				"id":       "binding-1",
				"issue_id": issueID,
				"status":   "running",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/local-runs/binding-1/messages":
			var posted map[string]any
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode posted message: %v", err)
			}
			sourceKey, _ := posted["source_key"].(string)
			if strings.HasPrefix(sourceKey, "thread-1:conversation:1:") {
				conversationPosts++
				conversationBodies = append(conversationBodies, posted)
			} else {
				eventPosts++
			}
			json.NewEncoder(w).Encode(map[string]any{
				"task_id": "binding-1",
				"seq":     1,
				"type":    "final",
				"content": posted["content"],
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+issueID+"/comments":
			commentPosts++
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "comment-2",
				"content": "Direct plugin comment.",
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/local-runs/binding-1/usage":
			usagePosts++
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	input := strings.Join([]string{
		mcpToolCallLine(1, "issue_search", map[string]any{"query": "Codex", "limit": 20}),
		mcpToolCallLine(2, "session_bind", map[string]any{
			"issue_id":        "OPE-1493",
			"codex_thread_id": "thread-1",
			"project_folder":  "/tmp/project",
			"branch":          "feat/OPE-1493-codex-plugin",
			"source_key":      "thread-1:bind",
		}),
		mcpToolCallLine(3, "runtime_event_append", map[string]any{
			"issue_id":   "OPE-1493",
			"binding_id": "binding-1",
			"event_type": "final",
			"content":    "Finished from Codex App plugin.",
			"visibility": "issue_comment",
			"source_key": "thread-1:event:final",
		}),
		mcpToolCallLine(4, "runtime_event_append", map[string]any{
			"issue_id":   "OPE-1493",
			"binding_id": "binding-1",
			"event_type": "final",
			"content":    "Finished from Codex App plugin.",
			"visibility": "issue_comment",
			"source_key": "thread-1:event:final",
		}),
		mcpToolCallLine(5, "usage_update", map[string]any{
			"issue_id":      "OPE-1493",
			"binding_id":    "binding-1",
			"model":         "gpt-5.1-codex",
			"usage_mode":    "cumulative",
			"input_tokens":  100,
			"output_tokens": 25,
			"total_tokens":  125,
		}),
		mcpToolCallLine(6, "conversation_sync", map[string]any{
			"issue_id":     "OPE-1493",
			"binding_id":   "binding-1",
			"user_message": "hello",
			"bot_message":  "hello",
			"source_key":   "thread-1:conversation:1",
		}),
		mcpToolCallLine(7, "usage_update", map[string]any{
			"issue_id":      "OPE-1493",
			"binding_id":    "binding-1",
			"model":         "gpt-5.1-codex",
			"usage_mode":    "cumulative",
			"input_tokens":  100,
			"output_tokens": 25,
			"total_tokens":  125,
		}),
		mcpToolCallLine(8, "comment_add", map[string]any{
			"issue_id":   "OPE-1493",
			"content":    "Direct plugin comment.",
			"source_key": "thread-1:comment:1",
		}),
		mcpToolCallLine(9, "comment_add", map[string]any{
			"issue_id":   "OPE-1493",
			"content":    "Direct plugin comment.",
			"source_key": "thread-1:comment:1",
		}),
	}, "\n") + "\n"

	var output bytes.Buffer
	cmd := testCodexPluginMCPCommand(srv.URL)
	if err := runCodexPluginMCPServer(cmd, bytes.NewBufferString(input), &output); err != nil {
		t.Fatalf("runCodexPluginMCPServer() error = %v", err)
	}

	envelopes := decodeMCPToolEnvelopes(t, output.String())
	if len(envelopes) != 9 {
		t.Fatalf("got %d MCP envelopes, want 9; output=%s", len(envelopes), output.String())
	}
	if items, _ := envelopes[0].Data["items"].([]any); len(items) != 1 {
		t.Fatalf("issue_search items = %+v, want one result", envelopes[0].Data["items"])
	}
	if envelopes[1].Data["binding_id"] != "binding-1" {
		t.Fatalf("session_bind envelope = %+v", envelopes[1])
	}
	if envelopes[2].Data["event_id"] != "binding-1:1" {
		t.Fatalf("first final event envelope = %+v", envelopes[2])
	}
	if envelopes[3].Data["event_id"] != "binding-1:1" {
		t.Fatalf("duplicate final event envelope = %+v", envelopes[3])
	}
	if envelopes[4].Data["deduped"] != false {
		t.Fatalf("first usage envelope = %+v", envelopes[4])
	}
	if envelopes[5].Data["user_content"] != "用户：hello" || envelopes[5].Data["bot_content"] != "bot：hello" {
		t.Fatalf("conversation envelope = %+v", envelopes[5])
	}
	sessionTotal, _ := envelopes[6].Data["session_total"].(map[string]any)
	if envelopes[6].Data["deduped"] != false || sessionTotal["total_tokens"].(float64) != 125 {
		t.Fatalf("duplicate usage envelope = %+v", envelopes[6])
	}
	if envelopes[7].Data["deduped"] != false || envelopes[7].Data["comment_id"] != "comment-2" {
		t.Fatalf("first direct comment envelope = %+v", envelopes[7])
	}
	if envelopes[8].Data["deduped"] != false || envelopes[8].Data["comment_id"] != "comment-2" {
		t.Fatalf("duplicate direct comment envelope = %+v", envelopes[8])
	}
	if eventPosts != 2 || conversationPosts != 2 || usagePosts != 2 || commentPosts != 2 {
		t.Fatalf("eventPosts=%d conversationPosts=%d usagePosts=%d commentPosts=%d, want event=2 conversation=2 usage=2 comments=2", eventPosts, conversationPosts, usagePosts, commentPosts)
	}
	if len(conversationBodies) != 2 ||
		conversationBodies[0]["type"] != "user_input" ||
		conversationBodies[0]["content"] != "用户：hello" ||
		conversationBodies[0]["source_key"] != "thread-1:conversation:1:user" ||
		conversationBodies[1]["type"] != "final" ||
		conversationBodies[1]["content"] != "bot：hello" ||
		conversationBodies[1]["source_key"] != "thread-1:conversation:1:bot" {
		t.Fatalf("conversationBodies = %+v", conversationBodies)
	}
}

func TestCodexPluginHooksSyncPromptAndStopToBoundThread(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", "")
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	const issueID = "11111111-1111-4111-8111-111111111111"

	var posted []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/OPE-1493":
			json.NewEncoder(w).Encode(map[string]any{"id": issueID, "identifier": "OPE-1493"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+issueID:
			json.NewEncoder(w).Encode(map[string]any{"id": issueID, "identifier": "OPE-1493"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+issueID+"/local-runs":
			json.NewEncoder(w).Encode(map[string]any{
				"id":             "binding-1",
				"issue_id":       issueID,
				"status":         "running",
				"top_comment_id": "comment-1",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/local-runs/binding-1/messages":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode posted message: %v", err)
			}
			posted = append(posted, body)
			json.NewEncoder(w).Encode(map[string]any{
				"task_id": "binding-1",
				"seq":     len(posted),
				"type":    body["type"],
				"content": body["content"],
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	bindCmd := testCodexPluginBindCommand(srv.URL)
	if err := runCodexPluginBind(bindCmd, []string{"OPE-1493"}); err != nil {
		t.Fatalf("runCodexPluginBind() error = %v", err)
	}

	promptCmd := testCodexPluginHookCommand(srv.URL)
	promptCmd.SetIn(strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"session-1","turn_id":"turn-1","cwd":"/tmp/project","prompt":"HI","model":"gpt-5.1-codex"}`))
	if err := runCodexPluginHook(promptCmd, nil); err != nil {
		t.Fatalf("UserPromptSubmit hook error = %v", err)
	}

	stopCmd := testCodexPluginHookCommand(srv.URL)
	var out bytes.Buffer
	stopCmd.SetOut(&out)
	stopCmd.SetIn(strings.NewReader(`{"hook_event_name":"Stop","session_id":"session-1","turn_id":"turn-1","cwd":"/tmp/project","last_assistant_message":"hi，有什么要继续处理的？","model":"gpt-5.1-codex"}`))
	if err := runCodexPluginHook(stopCmd, nil); err != nil {
		t.Fatalf("Stop hook error = %v", err)
	}

	if len(posted) != 2 ||
		posted[0]["type"] != "user_input" ||
		posted[0]["content"] != "用户：HI" ||
		posted[1]["type"] != "final" ||
		posted[1]["content"] != "bot：hi，有什么要继续处理的？" {
		t.Fatalf("posted hook sync body = %+v", posted)
	}
	if posted[0]["source_key"] != "session-1:turn:turn-1:conversation:user" ||
		posted[1]["source_key"] != "session-1:turn:turn-1:conversation:bot" {
		t.Fatalf("source keys = %v / %v", posted[0]["source_key"], posted[1]["source_key"])
	}
	var hookResp map[string]any
	if err := json.Unmarshal(out.Bytes(), &hookResp); err != nil {
		t.Fatalf("decode stop hook output: %v; output=%q", err, out.String())
	}
	if hookResp["continue"] != true {
		t.Fatalf("stop hook output = %+v", hookResp)
	}
}

func TestCodexPluginStopHookSkipsExplicitConversationSync(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", "")
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	const issueID = "11111111-1111-4111-8111-111111111111"

	var posted []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/OPE-1493":
			json.NewEncoder(w).Encode(map[string]any{"id": issueID, "identifier": "OPE-1493"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+issueID:
			json.NewEncoder(w).Encode(map[string]any{"id": issueID, "identifier": "OPE-1493"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+issueID+"/local-runs":
			json.NewEncoder(w).Encode(map[string]any{
				"id":             "binding-1",
				"issue_id":       issueID,
				"status":         "running",
				"top_comment_id": "comment-1",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/local-runs/binding-1/messages":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode posted message: %v", err)
			}
			posted = append(posted, body)
			json.NewEncoder(w).Encode(map[string]any{
				"task_id": "binding-1",
				"seq":     len(posted),
				"type":    body["type"],
				"content": body["content"],
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	bindCmd := testCodexPluginBindCommand(srv.URL)
	if err := runCodexPluginBind(bindCmd, []string{"OPE-1493"}); err != nil {
		t.Fatalf("runCodexPluginBind() error = %v", err)
	}

	promptCmd := testCodexPluginHookCommand(srv.URL)
	promptCmd.SetIn(strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"session-1","turn_id":"turn-1","cwd":"/tmp/project","prompt":"你好","model":"gpt-5.1-codex"}`))
	if err := runCodexPluginHook(promptCmd, nil); err != nil {
		t.Fatalf("UserPromptSubmit hook error = %v", err)
	}

	_, err := codexPluginMCPToolConversationSync(testCodexPluginMCPCommand(srv.URL), map[string]any{
		"issue_id":     "OPE-1493",
		"binding_id":   "binding-1",
		"user_message": "你好",
		"bot_message":  "你好，我在。当前会话仍绑定在 Multica issue OPE-1782，这条对话也已同步过去。",
		"source_key":   "session-1:turn:turn-1:conversation",
	})
	if err != nil {
		t.Fatalf("conversation_sync error = %v", err)
	}

	stopCmd := testCodexPluginHookCommand(srv.URL)
	var out bytes.Buffer
	stopCmd.SetOut(&out)
	stopCmd.SetIn(strings.NewReader(`{"hook_event_name":"Stop","session_id":"session-1","turn_id":"turn-1","cwd":"/tmp/project","last_assistant_message":"你好，我在。当前会话仍绑定在 Multica issue OPE-1782，这条对话也已同步过去。","model":"gpt-5.1-codex"}`))
	if err := runCodexPluginHook(stopCmd, nil); err != nil {
		t.Fatalf("Stop hook error = %v", err)
	}

	if len(posted) != 2 ||
		posted[0]["type"] != "user_input" ||
		posted[0]["content"] != "用户：你好" ||
		posted[1]["type"] != "final" ||
		posted[1]["content"] != "bot：你好，我在。当前会话仍绑定在 Multica issue OPE-1782，这条对话也已同步过去。" {
		t.Fatalf("posted = %+v, want only explicit conversation_sync messages", posted)
	}
	var hookResp map[string]any
	if err := json.Unmarshal(out.Bytes(), &hookResp); err != nil {
		t.Fatalf("decode stop hook output: %v; output=%q", err, out.String())
	}
	if hookResp["continue"] != true {
		t.Fatalf("stop hook output = %+v", hookResp)
	}
}

func mcpToolCallLine(id int, name string, args map[string]any) string {
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	})
	if err != nil {
		panic(fmt.Sprintf("marshal MCP tool call: %v", err))
	}
	return string(raw)
}

type mcpToolEnvelope struct {
	OK   bool           `json:"ok"`
	Data map[string]any `json:"data"`
}

func decodeMCPToolEnvelopes(t *testing.T, output string) []mcpToolEnvelope {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	envelopes := make([]mcpToolEnvelope, 0, len(lines))
	for _, line := range lines {
		var resp struct {
			Result struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				IsError bool `json:"isError"`
			} `json:"result"`
			Error any `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("decode MCP response line: %v; line=%q", err, line)
		}
		if resp.Error != nil || resp.Result.IsError {
			t.Fatalf("unexpected MCP error response: %s", line)
		}
		if len(resp.Result.Content) != 1 || resp.Result.Content[0].Type != "text" {
			t.Fatalf("unexpected MCP content: %+v", resp.Result.Content)
		}
		var envelope mcpToolEnvelope
		if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &envelope); err != nil {
			t.Fatalf("decode MCP tool envelope: %v; text=%q", err, resp.Result.Content[0].Text)
		}
		if !envelope.OK {
			t.Fatalf("MCP tool envelope not ok: %+v", envelope)
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

func testCodexPluginBindCommand(serverURL string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	_ = cmd.Flags().Set("server-url", serverURL)
	_ = cmd.Flags().Set("workspace-id", "ws-1")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("codex-thread-id", "thread-1", "")
	cmd.Flags().String("codex-session-id", "", "")
	cmd.Flags().String("project-folder", "/tmp/project", "")
	cmd.Flags().String("branch", "feat/OPE-1493-codex-plugin", "")
	cmd.Flags().String("source", "codex_app_plugin", "")
	cmd.Flags().String("source-key", "thread-1:bind", "")
	return cmd
}

func testCodexPluginEventCommand(serverURL string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	_ = cmd.Flags().Set("server-url", serverURL)
	_ = cmd.Flags().Set("workspace-id", "ws-1")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("binding-id", "binding-1", "")
	cmd.Flags().String("event-type", "final", "")
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("content", "done", "")
	cmd.Flags().Bool("content-stdin", false, "")
	cmd.Flags().String("content-file", "", "")
	cmd.Flags().String("occurred-at", "", "")
	cmd.Flags().String("visibility", "issue_comment", "")
	cmd.Flags().String("source", "codex_app_plugin", "")
	cmd.Flags().String("source-key", "thread-1:event:final", "")
	return cmd
}

func testCodexPluginUsageCommand(serverURL string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	_ = cmd.Flags().Set("server-url", serverURL)
	_ = cmd.Flags().Set("workspace-id", "ws-1")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("binding-id", "binding-1", "")
	cmd.Flags().String("provider", "openai", "")
	cmd.Flags().String("model", "gpt-5.1-codex", "")
	cmd.Flags().String("usage-mode", "cumulative", "")
	cmd.Flags().Int64("input-tokens", 100, "")
	cmd.Flags().Int64("output-tokens", 25, "")
	cmd.Flags().Int64("total-tokens", 125, "")
	cmd.Flags().Int64("cached-input-tokens", 0, "")
	cmd.Flags().Int64("reasoning-tokens", 0, "")
	cmd.Flags().String("source", "codex_app_plugin", "")
	cmd.Flags().String("source-key", "thread-1:usage:1", "")
	return cmd
}

func testCodexPluginMCPCommand(serverURL string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	if serverURL != "" {
		_ = cmd.Flags().Set("server-url", serverURL)
		_ = cmd.Flags().Set("workspace-id", "ws-1")
	}
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("config", "", "")
	return cmd
}

func testCodexPluginHookCommand(serverURL string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	_ = cmd.Flags().Set("server-url", serverURL)
	_ = cmd.Flags().Set("workspace-id", "ws-1")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("config", "", "")
	return cmd
}

func init() {
	cli.ClientVersion = "test"
}
