package agent

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskpolicy"
)

func brainAcceptancePolicy(t *testing.T) *taskpolicy.ExecutionPolicy {
	t.Helper()
	policy, err := taskpolicy.NewBrainMCPAcceptance(taskpolicy.Request{
		Mode:       taskpolicy.BrainMCPAcceptanceMode,
		MCPServer:  "brain",
		ProjectIDs: []string{taskpolicy.BrainProjectID, taskpolicy.MulticaProjectID},
	})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestClaudeVersionAtLeast(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"2.1.294", false},
		{"2.1.295", true},
		{"Claude Code 2.1.300", true},
		{"2.1.295-beta", false},
		{"2.1.300.4", false},
		{"unknown", false},
		{"", false},
	} {
		if got := claudeVersionAtLeast(tc.value, minRestrictedClaudeVersion); got != tc.want {
			t.Errorf("claudeVersionAtLeast(%q) = %t, want %t", tc.value, got, tc.want)
		}
	}
}

func TestBuildRestrictedClaudeArgsExposeOnlyExactMCPTools(t *testing.T) {
	policy := brainAcceptancePolicy(t)
	args, err := buildRestrictedClaudeArgs(ExecOptions{
		ExecutionPolicy:    policy,
		Model:              "claude-sonnet",
		CustomArgs:         []string{"--tools", "Bash", "--agent", "other"},
		ExtraArgs:          []string{"--dangerous"},
		ClaudeSettingsPath: "/tmp/untrusted-settings.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\x00")
	for _, required := range []string{"--restricted", "--no-session-persistence", "--tools", "", "--agents", "--agent", restrictedClaudeAgentName, "--strict-mcp-config"} {
		if !strings.Contains(joined, required) {
			t.Errorf("restricted args omit %q: %#v", required, args)
		}
	}
	toolsIndex := slicesIndex(args, "--tools")
	if toolsIndex < 0 || toolsIndex+1 >= len(args) || args[toolsIndex+1] != "" {
		t.Fatalf("--tools must be followed by an empty argument to disable built-ins: %#v", args)
	}
	for _, forbidden := range []string{"Bash", "Read", "Write", "Edit", "Patch", "--dangerous", "/tmp/untrusted-settings.json", "bypassPermissions"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("restricted args contain forbidden value %q: %#v", forbidden, args)
		}
	}
	index := slicesIndex(args, "--agents")
	if index < 0 || index+1 >= len(args) {
		t.Fatalf("missing --agents JSON: %#v", args)
	}
	var agents map[string]restrictedClaudeAgent
	if err := json.Unmarshal([]byte(args[index+1]), &agents); err != nil {
		t.Fatalf("decode dynamic agent: %v", err)
	}
	agent, ok := agents[restrictedClaudeAgentName]
	if !ok {
		t.Fatalf("dynamic main agent missing: %#v", agents)
	}
	if !reflect.DeepEqual(agent.Tools, policy.Tools) {
		t.Fatalf("agent tools = %#v, want exact policy list %#v", agent.Tools, policy.Tools)
	}
}

func TestValidateRestrictedClaudeConfigFailsClosed(t *testing.T) {
	policy := brainAcceptancePolicy(t)
	config := json.RawMessage(`{"mcpServers":{"brain":{"type":"http","url":"https://brain.invalid/mcp"},"other":{"type":"http","url":"https://other.invalid/mcp"}}}`)
	for _, tc := range []struct {
		name string
		cfg  Config
		opts ExecOptions
	}{
		{name: "unsupported version", cfg: Config{BuiltinRuntime: true, CLIVersion: "2.1.294"}, opts: ExecOptions{ExecutionPolicy: policy, McpConfig: config}},
		{name: "unknown version", cfg: Config{BuiltinRuntime: true}, opts: ExecOptions{ExecutionPolicy: policy, McpConfig: config}},
		{name: "custom runtime", cfg: Config{CLIVersion: "2.1.300"}, opts: ExecOptions{ExecutionPolicy: policy, McpConfig: config}},
		{name: "missing server", cfg: Config{BuiltinRuntime: true, CLIVersion: "2.1.300"}, opts: ExecOptions{ExecutionPolicy: policy, McpConfig: json.RawMessage(`{"mcpServers":{"other":{"type":"http","url":"https://other.invalid/mcp"}}}`)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateRestrictedClaudeConfig(tc.cfg, tc.opts); err == nil {
				t.Fatal("restricted config unexpectedly accepted")
			}
		})
	}
}

func TestRestrictedClaudeHooksBlockOutOfScopeCallsBeforeDispatch(t *testing.T) {
	policy := brainAcceptancePolicy(t)
	hooks, hookURL, err := startRestrictedClaudeHooks(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer hooks.Close()

	settings, err := hooks.settingsJSON(hookURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"PreToolUse"`, `"PostToolUse"`, `"PostToolUseFailure"`, `"onFailure":"block"`} {
		if !strings.Contains(string(settings), value) {
			t.Fatalf("hook settings omit %s: %s", value, settings)
		}
	}

	revision := strings.Repeat("a", 64)
	contextPayload := fmt.Sprintf(`{"schemaVersion":"silvermediums.brain.v2","servedAt":"2000-01-01T00:00:00Z","dataPolicy":"reference-only","connection":{"state":"online","checkedAt":"2000-01-01T00:00:00Z"},"project":{"id":"brain"},"documents":[{"id":"doc-1","revision":%q}]}`, revision)
	contextResponse := map[string]any{"content": []any{map[string]any{"type": "text", "text": contextPayload}}}
	preContext := restrictedClaudeHookEvent{
		EventName: "PreToolUse", ToolName: "mcp__brain__brain_context",
		ToolInput: map[string]any{"project": "brain", "q": "How do I retrieve evidence beyond the short search excerpt?"}, ToolUseID: "ctx-call",
	}
	if decision := postRestrictedHook(t, hookURL, preContext); decision != "allow" {
		t.Fatalf("exact context request decision = %q, want allow", decision)
	}
	postContext := restrictedClaudeHookEvent{
		EventName: "PostToolUse", ToolName: "mcp__brain__brain_context",
		ToolResponse: mustMarshal(t, contextResponse), ToolUseID: "ctx-call",
	}
	if decision := postRestrictedHook(t, hookURL, postContext); decision != "" {
		t.Fatalf("context completion returned a permission decision: %q", decision)
	}
	if len(hooks.authorizedDocs) != 1 || hooks.authorizedDocs["doc-1\x00"+revision] != "brain" {
		t.Fatalf("hook retained unexpected context state: %#v", hooks.authorizedDocs)
	}

	readInput := map[string]any{"id": "doc-1", "revision": revision}
	if decision := postRestrictedHook(t, hookURL, restrictedClaudeHookEvent{
		EventName: "PreToolUse", ToolName: "mcp__brain__brain_read",
		ToolInput: readInput, ToolUseID: "read-call",
	}); decision != "allow" {
		t.Fatalf("cited immutable read decision = %q, want allow", decision)
	}
	for _, denied := range []struct {
		name  string
		tool  string
		input map[string]any
	}{
		{"uncited document", "mcp__brain__brain_read", map[string]any{"id": "other", "revision": revision}},
		{"wrong revision", "mcp__brain__brain_read", map[string]any{"id": "doc-1", "revision": strings.Repeat("b", 64)}},
		{"query broadening", "mcp__brain__brain_context", map[string]any{"project": "brain", "q": "everything"}},
		{"broader event range", "mcp__brain__company_events", map[string]any{"afterId": float64(1), "throughId": float64(199901), "limit": float64(1)}},
	} {
		t.Run(denied.name, func(t *testing.T) {
			if hooks.allowsToolInput(strings.TrimPrefix(denied.tool, "mcp__brain__"), denied.input) {
				t.Fatalf("out-of-scope request was authorized: %s", denied.name)
			}
			decision := postRestrictedHook(t, hookURL, restrictedClaudeHookEvent{
				EventName: "PreToolUse", ToolName: denied.tool,
				ToolInput: denied.input, ToolUseID: "denied-" + strings.ReplaceAll(denied.name, " ", "-"),
			})
			if decision != "deny" {
				t.Fatalf("out-of-scope request decision = %q, want deny", decision)
			}
		})
	}
	if hooks.Failure() == "" {
		t.Fatal("an out-of-scope tool attempt did not fail the restricted task")
	}
}

func TestRestrictedClaudeHookEnforcesTaskCallLimit(t *testing.T) {
	policy := brainAcceptancePolicy(t)
	hooks, hookURL, err := startRestrictedClaudeHooks(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer hooks.Close()
	for i := 0; i < restrictedClaudeMaxToolCalls; i++ {
		decision := postRestrictedHook(t, hookURL, restrictedClaudeHookEvent{
			EventName: "PreToolUse", ToolName: "mcp__brain__brain_projects",
			ToolInput: map[string]any{}, ToolUseID: fmt.Sprintf("call-%d", i),
		})
		if decision != "allow" {
			t.Fatalf("tool call %d was denied before the limit: %q", i+1, decision)
		}
	}
	decision := postRestrictedHook(t, hookURL, restrictedClaudeHookEvent{
		EventName: "PreToolUse", ToolName: "mcp__brain__brain_projects",
		ToolInput: map[string]any{}, ToolUseID: "call-over-limit",
	})
	if decision != "deny" || hooks.Failure() == "" {
		t.Fatalf("over-limit tool call decision=%q failure=%q", decision, hooks.Failure())
	}
}

func TestRestrictedClaudeHookAcceptsBoundedFullReadBeyond64KiB(t *testing.T) {
	policy := brainAcceptancePolicy(t)
	hooks, hookURL, err := startRestrictedClaudeHooks(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer hooks.Close()
	body := strings.Repeat("synthetic full document\n", 4096)
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	hooks.authorizedDocs["synthetic-doc\x00"+revision] = "brain"
	if decision := postRestrictedHook(t, hookURL, restrictedClaudeHookEvent{
		EventName: "PreToolUse", ToolName: "mcp__brain__brain_read", ToolUseID: "large-read",
		ToolInput: map[string]any{"id": "synthetic-doc", "revision": revision},
	}); decision != "allow" {
		t.Fatal("authorized full read was denied")
	}
	payload := fmt.Sprintf(`{"schemaVersion":"silvermediums.brain.v2","servedAt":"2000-01-01T00:00:00Z","dataPolicy":"reference-only","connection":{"state":"online","checkedAt":"2000-01-01T00:00:00Z"},"document":{"id":"synthetic-doc","project":"brain","revision":%q,"body":%q,"bytes":%d,"truncated":false,"currentRevision":%q,"available":true}}`, revision, body, len(body), revision)
	postRestrictedHook(t, hookURL, restrictedClaudeHookEvent{
		EventName: "PostToolUse", ToolName: "mcp__brain__brain_read", ToolUseID: "large-read",
		ToolResponse: mustMarshal(t, map[string]any{"content": []any{map[string]any{"type": "text", "text": payload}}}),
	})
	if _, ok := hooks.WaitDuration(t.Context(), "large-read", "brain_read"); !ok || hooks.Failure() != "" {
		t.Fatal("complete full read above 64KiB failed hook verification")
	}
}

func TestRestrictedClaudeHookRejectsBodyAboveAgentEventBudget(t *testing.T) {
	hooks := &restrictedClaudeHooks{path: "/synthetic"}
	req := httptest.NewRequest(http.MethodPost, "/synthetic", strings.NewReader(strings.Repeat(" ", agentStreamMaxLineBytes+1)))
	hooks.serveHTTP(httptest.NewRecorder(), req)
	if hooks.Failure() == "" {
		t.Fatal("oversized hook body was accepted")
	}
}

func TestRestrictedClaudeReceiptBudgetFailsOnAggregateCountAndBytes(t *testing.T) {
	policy := brainAcceptancePolicy(t)
	content := json.RawMessage(`[{"type":"text","text":"brain request rejected (HTTP 404)"}]`)
	input := map[string]any{"identity": "not-an-enrolled-project"}
	for _, tc := range []struct {
		name   string
		budget restrictedClaudeReceiptBudget
		want   string
	}{
		{"count", restrictedClaudeReceiptBudget{count: restrictedClaudeMaxReceipts}, "receipt count limit"},
		{"aggregate bytes", restrictedClaudeReceiptBudget{evidenceBytes: restrictedClaudeMaxEvidenceBytes - 1}, "receipt byte limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := map[string]restrictedClaudeCall{
				"resolve-call": {tool: "brain_resolve", input: input},
			}
			hooks := completedRestrictedHooks(policy, "brain_resolve", "resolve-call", time.Millisecond)
			user := claudeSDKMessage{Message: mustMarshal(t, claudeMessageContent{
				Content: []claudeContentBlock{{Type: "tool_result", ToolUseID: "resolve-call", Content: content, IsError: true}},
			})}
			failure, receipts, unverifiable := (&claudeBackend{}).handleRestrictedUser(
				t.Context(), user, make(chan Message, 1), policy, calls, hooks, &tc.budget,
			)
			if failure == "" || !strings.Contains(failure, tc.want) || len(receipts) != 0 || !unverifiable {
				t.Fatalf("budget failure=%q unverifiable=%t receipts=%s", failure, unverifiable, receipts)
			}
		})
	}
}

func postRestrictedHook(t *testing.T, url string, event restrictedClaudeHookEvent) string {
	t.Helper()
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result struct {
		HookSpecificOutput struct {
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hook status = %d", resp.StatusCode)
	}
	return result.HookSpecificOutput.PermissionDecision
}

func TestRestrictedClaudeExecuteRejectsUnsupportedVersionBeforeStartingCLI(t *testing.T) {
	b := &claudeBackend{cfg: Config{
		ExecutablePath: "/nonexistent/claude",
		BuiltinRuntime: true,
		CLIVersion:     "2.1.294",
		Logger:         slog.Default(),
	}}
	_, err := b.Execute(t.Context(), "private task prompt", ExecOptions{
		ExecutionPolicy: brainAcceptancePolicy(t),
		McpConfig:       json.RawMessage(`{"mcpServers":{"brain":{"type":"http","url":"https://brain.invalid/mcp"}}}`),
	})
	if err == nil || !strings.Contains(err.Error(), minRestrictedClaudeVersion) {
		t.Fatalf("unsupported CLI was not rejected before launch: %v", err)
	}
}

func TestValidateRestrictedClaudeConfigFiltersServersWithoutMutatingInput(t *testing.T) {
	policy := brainAcceptancePolicy(t)
	config := json.RawMessage(`{"mcpServers":{"brain":{"type":"http","url":"https://brain.invalid/mcp"},"other":{"type":"http","url":"https://other.invalid/mcp"}}}`)
	before := append(json.RawMessage(nil), config...)
	filtered, err := validateRestrictedClaudeConfig(Config{BuiltinRuntime: true, CLIVersion: "2.1.300"}, ExecOptions{
		ExecutionPolicy: policy,
		McpConfig:       config,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(config, before) {
		t.Fatalf("shared MCP config changed: %s", config)
	}
	var got struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(filtered, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.MCPServers) != 1 || len(got.MCPServers["brain"]) == 0 {
		t.Fatalf("filtered config = %s", filtered)
	}
}

func TestRestrictedClaudeAssistantDeniesLocalAndUnknownTools(t *testing.T) {
	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	policy := brainAcceptancePolicy(t)
	for _, tool := range []string{"Bash", "Read", "Write", "Edit", "mcp__other__brain_read", "mcp__brain__unknown"} {
		ch := make(chan Message, 2)
		msg := claudeSDKMessage{Message: mustMarshal(t, claudeMessageContent{
			Content: []claudeContentBlock{{Type: "tool_use", ID: "call-denied", Name: tool}},
		})}
		_, failure := b.handleRestrictedAssistant(msg, ch, map[string]TokenUsage{}, policy, map[string]restrictedClaudeCall{})
		if failure == "" {
			t.Errorf("tool %q was not rejected", tool)
		}
		if len(ch) != 0 {
			t.Errorf("denied tool %q emitted a persisted message", tool)
		}
	}
}

func TestRestrictedClaudeRejectsUnsafeToolCallIdentifiers(t *testing.T) {
	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	policy := brainAcceptancePolicy(t)
	for _, callID := range []string{
		"private prompt fragment\nsecond line",
		strings.Repeat("x", restrictedClaudeMaxCallIDBytes+1),
	} {
		ch := make(chan Message, 1)
		msg := claudeSDKMessage{Message: mustMarshal(t, claudeMessageContent{
			Content: []claudeContentBlock{{
				Type:  "tool_use",
				ID:    callID,
				Name:  "mcp__brain__brain_projects",
				Input: mustMarshal(t, map[string]any{}),
			}},
		})}
		_, failure := b.handleRestrictedAssistant(msg, ch, map[string]TokenUsage{}, policy, map[string]restrictedClaudeCall{})
		if failure == "" || len(ch) != 0 {
			t.Fatalf("unsafe call identifier was retained: failure=%q messages=%d", failure, len(ch))
		}
	}
}

func TestRestrictedClaudeHookRejectsUnsafeToolCallIdentifier(t *testing.T) {
	policy := brainAcceptancePolicy(t)
	hooks, hookURL, err := startRestrictedClaudeHooks(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer hooks.Close()
	decision := postRestrictedHook(t, hookURL, restrictedClaudeHookEvent{
		EventName: "PreToolUse",
		ToolName:  "mcp__brain__brain_projects",
		ToolInput: map[string]any{},
		ToolUseID: strings.Repeat("x", restrictedClaudeMaxCallIDBytes+1),
	})
	if decision != "deny" || hooks.Failure() == "" {
		t.Fatalf("unsafe hook call identifier decision=%q failure=%q", decision, hooks.Failure())
	}
}

func TestRestrictedClaudeFailureIsMonotonic(t *testing.T) {
	const first = "restricted Claude attempted an unauthorized or uncorrelated tool call"
	if got := retainRestrictedClaudeFailure(first, ""); got != first {
		t.Fatalf("later successful event cleared restricted failure: got %q, want %q", got, first)
	}
	if got := retainRestrictedClaudeFailure(first, "later failure"); got != first {
		t.Fatalf("later event replaced first restricted failure: got %q, want %q", got, first)
	}
	if got := retainRestrictedClaudeFailure("", first); got != first {
		t.Fatalf("first restricted failure was not recorded: got %q, want %q", got, first)
	}
}

func TestRestrictedClaudeEndConversationIsHarmlessAndNeverAReceipt(t *testing.T) {
	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	policy := brainAcceptancePolicy(t)
	calls := map[string]restrictedClaudeCall{}
	assistant := claudeSDKMessage{Message: mustMarshal(t, claudeMessageContent{
		Content: []claudeContentBlock{{Type: "tool_use", ID: "end-1", Name: "EndConversation"}},
	})}
	turn, failure := b.handleRestrictedAssistant(assistant, make(chan Message, 1), map[string]TokenUsage{}, policy, calls)
	if failure != "" || turn.toolUses != 1 || hasUnfinishedRestrictedMCPCall(calls) {
		t.Fatalf("EndConversation was not treated as harmless: turn=%+v failure=%q calls=%+v", turn, failure, calls)
	}

	user := claudeSDKMessage{Message: mustMarshal(t, claudeMessageContent{
		Content: []claudeContentBlock{{Type: "tool_result", ToolUseID: "end-1", Content: json.RawMessage(`"private control result"`)}},
	})}
	output := make(chan Message, 1)
	failure, receipts, unverifiable := b.handleRestrictedUser(t.Context(), user, output, policy, calls, nil, &restrictedClaudeReceiptBudget{})
	if failure != "" || unverifiable || len(receipts) != 0 || len(output) != 0 || len(calls) != 0 {
		t.Fatalf("EndConversation result affected task evidence: failure=%q unverifiable=%t receipts=%s messages=%d calls=%+v", failure, unverifiable, receipts, len(output), calls)
	}
}

func TestRestrictedClaudeAssistantDoesNotExposeToolInput(t *testing.T) {
	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	policy := brainAcceptancePolicy(t)
	ch := make(chan Message, 1)
	calls := map[string]restrictedClaudeCall{}
	msg := claudeSDKMessage{Message: mustMarshal(t, claudeMessageContent{
		Content: []claudeContentBlock{{
			Type: "tool_use", ID: "call-1", Name: "mcp__brain__brain_read",
			Input: mustMarshal(t, map[string]any{"id": "private-doc-id", "revision": strings.Repeat("a", 64)}),
		}},
	})}
	turn, failure := b.handleRestrictedAssistant(msg, ch, map[string]TokenUsage{}, policy, calls)
	if failure != "" || turn.toolUses != 1 {
		t.Fatalf("restricted assistant result: turn=%+v failure=%q", turn, failure)
	}
	got := <-ch
	if got.Tool != "mcp__brain__brain_read" || got.Input != nil {
		t.Fatalf("unsafe tool-use message: %+v", got)
	}
	if strings.Contains(fmt.Sprint(got), "private") {
		t.Fatalf("private tool input escaped: %+v", got)
	}
}

func TestRestrictedClaudeEventToolUsesExactFrozenRange(t *testing.T) {
	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	policy := brainAcceptancePolicy(t)
	for _, tc := range []struct {
		name  string
		input map[string]any
		want  bool
	}{
		{name: "exact selector", input: map[string]any{"afterId": 199900, "throughId": 199901, "limit": 1}, want: true},
		{name: "wider range", input: map[string]any{"afterId": 199000, "throughId": 199901, "limit": 1}},
		{name: "larger limit", input: map[string]any{"afterId": 199900, "throughId": 199901, "limit": 10}},
		{name: "extra query", input: map[string]any{"afterId": 199900, "throughId": 199901, "limit": 1, "query": "private"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan Message, 1)
			calls := map[string]restrictedClaudeCall{}
			msg := claudeSDKMessage{Message: mustMarshal(t, claudeMessageContent{
				Content: []claudeContentBlock{{
					Type: "tool_use", ID: "event-call", Name: "mcp__brain__company_events",
					Input: mustMarshal(t, tc.input),
				}},
			})}
			_, failure := b.handleRestrictedAssistant(msg, ch, map[string]TokenUsage{}, policy, calls)
			if (failure == "") != tc.want {
				t.Fatalf("failure=%q, want allowed=%t", failure, tc.want)
			}
			if got := len(ch); (got == 1) != tc.want {
				t.Fatalf("emitted %d tool-use events, want allowed=%t", got, tc.want)
			}
		})
	}
}

func TestRestrictedClaudeReceiptOmitsDocumentBodyAndAcceptsHistoricalRevision(t *testing.T) {
	body := "private document body"
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	currentRevision := strings.Repeat("e", 64)
	payload := fmt.Sprintf(`{"schemaVersion":"silvermediums.brain.v2","servedAt":"2026-04-01T12:00:00Z","dataPolicy":"authorized","connection":{"state":"online","checkedAt":"2026-04-01T12:00:00Z"},"document":{"id":"doc-1","project":"brain","revision":"%s","body":%q,"bytes":%d,"truncated":false,"currentRevision":"%s","available":true}}`, revision, body, len(body), currentRevision)
	content := json.RawMessage(fmt.Sprintf(`[{"type":"text","text":%q}]`, payload))
	policy := brainAcceptancePolicy(t)
	input := map[string]any{"id": "doc-1", "revision": revision}
	got, verified := restrictedClaudeReceiptWithInput("brain_read", "call-1", content, false, 1500*time.Millisecond, input, policy)
	if !verified {
		t.Fatalf("Brain read receipt not verified: %s", got)
	}
	for _, want := range []string{`"documentId":"doc-1"`, `"projectId":"brain"`, `"revision":"` + revision + `"`, `"currentRevision":"` + currentRevision + `"`, `"bodySha256Verified":true`, `"byteLengthVerified":true`, `"durationMs":1500`, `"durationSource":"pre_post_tool_hook_monotonic"`} {
		if !strings.Contains(got, want) {
			t.Errorf("receipt omits %s: %s", want, got)
		}
	}
	if strings.Contains(got, body) || strings.Contains(got, "bodyBytes") {
		t.Fatalf("receipt retained source body: %s", got)
	}
}

func TestRestrictedClaudeReadReceiptCorrelatesRequestedDocumentAndRevision(t *testing.T) {
	body := "synthetic body"
	documentID := "docs/synthetic report.md"
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	payload := fmt.Sprintf(`{"schemaVersion":"silvermediums.brain.v2","servedAt":"2026-04-01T12:00:00Z","dataPolicy":"authorized","connection":{"state":"online","checkedAt":"2026-04-01T12:00:00Z"},"document":{"id":%q,"project":"brain","revision":"%s","body":%q,"bytes":%d,"truncated":false,"currentRevision":"%s","available":true}}`, documentID, revision, body, len(body), strings.Repeat("e", 64))
	content := json.RawMessage(fmt.Sprintf(`[{"type":"text","text":%q}]`, payload))
	policy := brainAcceptancePolicy(t)
	for _, input := range []map[string]any{
		{"id": "other-doc", "revision": revision},
		{"id": documentID, "revision": strings.Repeat("f", 64)},
	} {
		got, verified := restrictedClaudeReceiptWithInput("brain_read", "call-1", content, false, time.Second, input, policy)
		if verified || !strings.Contains(got, `"receiptVerified":false`) {
			t.Fatalf("mismatched requested read was accepted: input=%v receipt=%s", input, got)
		}
	}
	got, verified := restrictedClaudeReceiptWithInput("brain_read", "call-1", content, false, time.Second,
		map[string]any{"id": documentID, "revision": revision}, policy)
	if !verified || !strings.Contains(got, `"documentId":"docs/synthetic report.md"`) {
		t.Fatalf("valid path-based document ID was rejected: verified=%t receipt=%s", verified, got)
	}
}

func TestRestrictedClaudeExpectedUnknownProject404IsVerifiedWithoutErrorText(t *testing.T) {
	policy := brainAcceptancePolicy(t)
	content := json.RawMessage(`[{"type":"text","text":"brain request rejected (HTTP 404)"}]`)
	input := map[string]any{"identity": "not-an-enrolled-project"}
	got, verified := restrictedClaudeReceiptWithInput("brain_resolve", "resolve-call", content, true, time.Second, input, policy)
	if !verified {
		t.Fatalf("expected exact 404 refusal was rejected: %s", got)
	}
	for _, want := range []string{`"expectedRefusal":true`, `"httpStatus":404`, `"requestedIdentity":"not-an-enrolled-project"`, `"receiptVerified":true`} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected refusal receipt omitted %s: %s", want, got)
		}
	}
	if strings.Contains(got, "brain request rejected") {
		t.Fatalf("raw refusal prose escaped: %s", got)
	}
	success := json.RawMessage(`[{"type":"text","text":"{\"schemaVersion\":\"silvermediums.brain.v2\",\"servedAt\":\"2000-01-01T00:00:00Z\",\"dataPolicy\":\"reference-only\",\"connection\":{\"state\":\"online\",\"checkedAt\":\"2000-01-01T00:00:00Z\"},\"project\":{\"id\":\"brain\"}}"}]`)
	if got, verified := restrictedClaudeReceiptWithInput("brain_resolve", "resolve-call", success, false, time.Second, input, policy); verified {
		t.Fatalf("successful resolution incorrectly satisfied the fixed unknown-project refusal: %s", got)
	}
	for _, tc := range []struct {
		name  string
		input map[string]any
		text  string
	}{
		{"wrong identity", map[string]any{"identity": "another-project"}, "brain request rejected (HTTP 404)"},
		{"auth failure", input, "brain request rejected (HTTP 401)"},
		{"server failure", input, "brain request rejected (HTTP 500)"},
		{"unstructured failure", input, "not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(fmt.Sprintf(`[{"type":"text","text":%q}]`, tc.text))
			got, verified := restrictedClaudeReceiptWithInput("brain_resolve", "resolve-call", raw, true, time.Second, tc.input, policy)
			if verified || strings.Contains(got, tc.text) {
				t.Fatalf("unexpected refusal accepted or raw error retained: verified=%t receipt=%s", verified, got)
			}
		})
	}
}

func TestRestrictedClaudeContextReceiptCorrelatesProjectAndOnlineState(t *testing.T) {
	policy := brainAcceptancePolicy(t)
	revision := strings.Repeat("a", 64)
	makeContent := func(project string, state string) json.RawMessage {
		payload := fmt.Sprintf(`{"schemaVersion":"silvermediums.brain.v2","servedAt":"2000-01-01T00:00:00Z","dataPolicy":"reference-only","connection":{"state":%q,"checkedAt":"2000-01-01T00:00:00Z"},"project":{"id":%q},"documents":[{"id":"doc-1","revision":%q}]}`, state, project, revision)
		return json.RawMessage(fmt.Sprintf(`[{"type":"text","text":%q}]`, payload))
	}
	input := map[string]any{"project": "brain", "q": "How do I retrieve evidence beyond the short search excerpt?"}
	got, verified := restrictedClaudeReceiptWithInput("brain_context", "context-call", makeContent("brain", "online"), false, time.Second, input, policy)
	if !verified || !strings.Contains(got, `"projectId":"brain"`) {
		t.Fatalf("exact online context was rejected: verified=%t receipt=%s", verified, got)
	}
	for _, content := range []json.RawMessage{makeContent("multica", "online"), makeContent("brain", "offline")} {
		if got, verified := restrictedClaudeReceiptWithInput("brain_context", "context-call", content, false, time.Second, input, policy); verified {
			t.Fatalf("mismatched context was accepted: %s", got)
		}
	}
}

func TestRestrictedClaudeReadHookCorrelatesCitationProject(t *testing.T) {
	body := "synthetic cited document"
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	payload := fmt.Sprintf(`{"schemaVersion":"silvermediums.brain.v2","servedAt":"2000-01-01T00:00:00Z","dataPolicy":"reference-only","connection":{"state":"online","checkedAt":"2000-01-01T00:00:00Z"},"document":{"id":"doc-1","project":"brain","revision":%q,"body":%q,"bytes":%d,"truncated":false,"currentRevision":%q,"available":true}}`, revision, body, len([]byte(body)), revision)
	response := json.RawMessage(fmt.Sprintf(`{"content":[{"type":"text","text":%q}],"isError":false}`, payload))
	hooks := &restrictedClaudeHooks{
		policy:         brainAcceptancePolicy(t),
		authorizedDocs: map[string]string{"doc-1\x00" + revision: "brain"},
	}
	input := map[string]any{"id": "doc-1", "revision": revision}
	if !hooks.matchesAuthorizedReadLocked(response, input) {
		t.Fatal("cited document from its authorized project was rejected")
	}
	wrongProjectPayload := strings.Replace(payload, `"project":"brain"`, `"project":"multica"`, 1)
	wrongProjectResponse := json.RawMessage(fmt.Sprintf(`{"content":[{"type":"text","text":%q}],"isError":false}`, wrongProjectPayload))
	if hooks.matchesAuthorizedReadLocked(wrongProjectResponse, input) {
		t.Fatal("valid self-hashed read from a different project was accepted")
	}
}

func TestRestrictedClaudeHookWaitRequiresCompletedVerification(t *testing.T) {
	done := make(chan struct{})
	close(done)
	hooks := &restrictedClaudeHooks{calls: map[string]*restrictedClaudeHookCall{
		"call": {tool: "brain_context", duration: time.Millisecond, done: done},
	}}
	if _, measured := hooks.WaitDuration(t.Context(), "call", "brain_context"); measured {
		t.Fatal("a duration assigned before verification completed was accepted")
	}
}

func TestRestrictedClaudeConcurrentHookResultWaitsForVerification(t *testing.T) {
	for i := 0; i < 50; i++ {
		hooks := &restrictedClaudeHooks{
			policy: brainAcceptancePolicy(t),
			calls: map[string]*restrictedClaudeHookCall{
				"call": {
					tool: "brain_context", input: map[string]any{"project": "brain"},
					startedAt: time.Now().Add(-time.Millisecond), done: make(chan struct{}),
				},
			},
			authorizedDocs: make(map[string]string),
		}
		waited := make(chan bool, 1)
		go func() {
			_, measured := hooks.WaitDuration(t.Context(), "call", "brain_context")
			waited <- measured
		}()
		hooks.handlePostToolUse(httptest.NewRecorder(), restrictedClaudeHookEvent{
			EventName: "PostToolUse", ToolName: "mcp__brain__brain_context", ToolUseID: "call",
			ToolResponse: json.RawMessage(`{"content":[{"type":"text","text":"invalid context"}],"isError":false}`),
		})
		if <-waited || hooks.Failure() == "" {
			t.Fatal("concurrent stream result escaped failed hook verification")
		}
	}
}

func TestRestrictedClaudeReceiptFailsClosedForUnknownCompanyEventSchema(t *testing.T) {
	content := json.RawMessage(`[{"type":"text","text":"{\"event_id\":\"199901\",\"citation\":\"citation\"}"}]`)
	got, verified := restrictedClaudeReceiptWithInput("company_events", "call-1", content, false, time.Second,
		map[string]any{"afterId": float64(199900), "throughId": float64(199901), "limit": float64(1)}, brainAcceptancePolicy(t))
	if verified || strings.Contains(got, "199901") || strings.Contains(got, "citation") {
		t.Fatalf("unknown company event response was treated as verified: verified=%t receipt=%s", verified, got)
	}
}

func TestRestrictedClaudeCompanyEventsReceiptKeepsKnownEnvelopeUnverified(t *testing.T) {
	content := json.RawMessage(`[{"type":"text","text":"{\"schemaVersion\":\"silvermediums.brain.v2\",\"servedAt\":\"2026-10-18T01:00:00Z\",\"dataPolicy\":\"authorized\",\"items\":[{}],\"next_after_id\":199901,\"snapshot_through_id\":199901}"}]`)
	input := map[string]any{"afterId": float64(199900), "throughId": float64(199901), "limit": float64(1)}
	got, verified := restrictedClaudeReceiptWithInput("company_events", "call-1", content, false, 1500*time.Millisecond, input, brainAcceptancePolicy(t))
	for _, want := range []string{`"receiptVerified":false`} {
		if !strings.Contains(got, want) {
			t.Fatalf("unverified event receipt omitted %s: %s", want, got)
		}
	}
	if verified || strings.Contains(got, `"citation"`) || strings.Contains(got, `"eventId"`) {
		t.Fatalf("unknown event row contract was treated as verified: verified=%t receipt=%s", verified, got)
	}
}

func TestRestrictedClaudeCompanyEventsResultPersistsOnlyOperationalSummary(t *testing.T) {
	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	policy := brainAcceptancePolicy(t)
	input := map[string]any{"afterId": float64(199900), "throughId": float64(199901), "limit": float64(1)}
	calls := map[string]restrictedClaudeCall{}
	assistant := claudeSDKMessage{Message: mustMarshal(t, claudeMessageContent{
		Content: []claudeContentBlock{{
			Type: "tool_use", ID: "event-call", Name: "mcp__brain__company_events",
			Input: mustMarshal(t, input),
		}},
	})}
	if _, failure := b.handleRestrictedAssistant(assistant, make(chan Message, 1), map[string]TokenUsage{}, policy, calls); failure != "" {
		t.Fatalf("exact event selector was rejected: %s", failure)
	}
	payload := `{"schemaVersion":"silvermediums.brain.v2","servedAt":"2026-10-18T01:00:00Z","dataPolicy":"authorized","connection":{"state":"online","checkedAt":"2026-10-18T01:00:00Z"},"items":[{"id":199901,"evidence_ref":"source_event:199901","producer":"synthetic_test","event_type":"watcher.status","payload_schema_version":1,"observed_at":"2026-10-18T00:00:00Z","received_at":"2026-10-18T00:00:01Z","complete":true,"scope":{"record_id":199901},"payload_sha256":"39f618e2cc21a328899a316380c29da2298b6ab9d94b71af2c3402595dd9ea92","payload":{"body":"SENSITIVE_EVENT_BODY"}}],"has_more":false,"next_after_id":199901,"snapshot_through_id":199901}`
	content := json.RawMessage(fmt.Sprintf(`[{"type":"text","text":%q}]`, payload))
	user := claudeSDKMessage{Message: mustMarshal(t, claudeMessageContent{
		Content: []claudeContentBlock{{Type: "tool_result", ToolUseID: "event-call", Content: content}},
	})}
	output := make(chan Message, 1)
	hooks := completedRestrictedHooks(policy, "company_events", "event-call", 1500*time.Millisecond)
	failure, receipts, unverifiable := b.handleRestrictedUser(t.Context(), user, output, policy, calls, hooks, &restrictedClaudeReceiptBudget{})
	if failure != "" || unverifiable || len(receipts) != 1 {
		t.Fatalf("company event result state: failure=%q unverifiable=%t receipts=%s", failure, unverifiable, receipts)
	}
	for _, secret := range []string{"source_event:199901", "SENSITIVE_EVENT_BODY", `"eventRows"`, `"evidenceRef"`} {
		if strings.Contains(string(receipts[0]), secret) {
			t.Fatalf("unverified event summary retained %q: %s", secret, receipts[0])
		}
	}
	for _, safeField := range []string{`"tool":"company_events"`, `"status":"verified"`, `"durationSource":"pre_post_tool_hook_monotonic"`} {
		if !strings.Contains(string(receipts[0]), safeField) {
			t.Fatalf("operational event summary omitted %s: %s", safeField, receipts[0])
		}
	}
	resultMessage := <-output
	if !resultMessage.DurationMeasured || strings.Contains(resultMessage.Output, "SENSITIVE") {
		t.Fatalf("event tool result was not safely summarized/timed: %+v", resultMessage)
	}
}

func completedRestrictedHooks(policy *taskpolicy.ExecutionPolicy, tool, callID string, duration time.Duration) *restrictedClaudeHooks {
	done := make(chan struct{})
	close(done)
	return &restrictedClaudeHooks{
		policy: policy,
		calls: map[string]*restrictedClaudeHookCall{
			callID: {tool: tool, duration: duration, done: done, completed: true},
		},
	}
}

func TestRestrictedBrainReceiptRequiresCompleteEvidence(t *testing.T) {
	validRead := map[string]any{
		"dataPolicyPresent":  true,
		"documentId":         "doc-1",
		"projectId":          "brain",
		"revision":           strings.Repeat("a", 64),
		"currentRevision":    strings.Repeat("a", 64),
		"bytes":              float64(10),
		"truncated":          false,
		"available":          true,
		"bodySha256Verified": true,
		"byteLengthVerified": true,
	}
	if !restrictedBrainReceiptIsVerifiable("brain_read", validRead) {
		t.Fatal("complete immutable read receipt was rejected")
	}
	for name, mutate := range map[string]func(map[string]any){
		"truncated":        func(receipt map[string]any) { receipt["truncated"] = true },
		"unavailable":      func(receipt map[string]any) { receipt["available"] = false },
		"body hash failed": func(receipt map[string]any) { receipt["bodySha256Verified"] = false },
		"body byte count failed": func(receipt map[string]any) {
			receipt["byteLengthVerified"] = false
		},
		"invalid current pointer": func(receipt map[string]any) { receipt["currentRevision"] = "not-a-hash" },
		"missing project":         func(receipt map[string]any) { receipt["projectId"] = "" },
	} {
		t.Run(name, func(t *testing.T) {
			receipt := make(map[string]any, len(validRead))
			for key, value := range validRead {
				receipt[key] = value
			}
			mutate(receipt)
			if restrictedBrainReceiptIsVerifiable("brain_read", receipt) {
				t.Fatal("incomplete or invalid evidence was accepted")
			}
		})
	}
}

func TestRestrictedBrainCompanySelectorRequiresClassifiedCompleteProjects(t *testing.T) {
	valid := map[string]any{
		"dataPolicyPresent": true,
		"projects": []any{map[string]any{
			"projectId":      "multica",
			"portfolioClass": "business_application",
			"classificationSource": map[string]any{
				"programId": "SYNTHETIC", "observedAt": "2000-01-01T00:00:00Z",
				"evidenceStage": "synthetic-test-only", "sourceRepository": "example/synthetic",
				"sourceRevision": strings.Repeat("b", 40), "sourceReference": "portfolio.json",
				"sourceSha256": strings.Repeat("c", 64),
			},
		}},
		"projectsTotal":             float64(1),
		"projectReceiptsIncomplete": false,
	}
	if !restrictedBrainReceiptIsVerifiable("brain_company_projects", valid) {
		t.Fatal("complete company selector receipt was rejected")
	}
	for name, mutate := range map[string]func(map[string]any){
		"wrong portfolio class": func(receipt map[string]any) {
			receipt["projects"].([]any)[0].(map[string]any)["portfolioClass"] = "personal"
		},
		"missing source pin": func(receipt map[string]any) {
			delete(receipt["projects"].([]any)[0].(map[string]any), "classificationSource")
		},
		"incomplete list": func(receipt map[string]any) {
			receipt["projectReceiptsIncomplete"] = true
		},
		"count mismatch": func(receipt map[string]any) {
			receipt["projectsTotal"] = float64(2)
		},
	} {
		t.Run(name, func(t *testing.T) {
			receipt := map[string]any{
				"dataPolicyPresent": true,
				"projects": []any{map[string]any{
					"projectId":      "multica",
					"portfolioClass": "business_application",
					"classificationSource": map[string]any{
						"programId": "SYNTHETIC", "observedAt": "2000-01-01T00:00:00Z",
						"evidenceStage": "synthetic-test-only", "sourceRepository": "example/synthetic",
						"sourceRevision": strings.Repeat("b", 40), "sourceReference": "portfolio.json",
						"sourceSha256": strings.Repeat("c", 64),
					},
				}},
				"projectsTotal":             float64(1),
				"projectReceiptsIncomplete": false,
			}
			mutate(receipt)
			if restrictedBrainReceiptIsVerifiable("brain_company_projects", receipt) {
				t.Fatal("incomplete or non-company selector evidence was accepted")
			}
		})
	}
}

func TestRestrictedClaudeControlRequestDeniesUnauthorizedTools(t *testing.T) {
	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	policy := brainAcceptancePolicy(t)
	hooks, _, err := startRestrictedClaudeHooks(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer hooks.Close()
	var output bytes.Buffer
	msg := claudeSDKMessage{
		RequestID: "req-1",
		Request: mustMarshal(t, claudeControlRequestPayload{
			Subtype: "tool_use", ToolName: "Bash",
			Input: mustMarshal(t, map[string]any{"command": "cat /etc/passwd"}),
		}),
	}
	if failure := b.handleRestrictedControlRequest(msg, &output, hooks); failure == "" {
		t.Fatal("unauthorized control request was accepted")
	}
	if strings.Contains(output.String(), "cat /etc/passwd") {
		t.Fatalf("control response exposed tool input: %s", output.String())
	}
	var response struct {
		Response struct {
			Response struct {
				Behavior string `json:"behavior"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatal(err)
	}
	if response.Response.Response.Behavior != "deny" {
		t.Fatalf("behavior = %q, want deny", response.Response.Response.Behavior)
	}
}

func TestRestrictedClaudeControlRequestUsesExactEventSelector(t *testing.T) {
	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	policy := brainAcceptancePolicy(t)
	hooks, _, err := startRestrictedClaudeHooks(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer hooks.Close()
	for _, tc := range []struct {
		name  string
		input map[string]any
		want  bool
	}{
		{name: "exact selector", input: map[string]any{"afterId": 199900, "throughId": 199901, "limit": 1}, want: true},
		{name: "wider selector", input: map[string]any{"afterId": 1, "throughId": 199901, "limit": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			msg := claudeSDKMessage{
				RequestID: "req-event",
				Request: mustMarshal(t, claudeControlRequestPayload{
					Subtype: "tool_use", ToolName: "mcp__brain__company_events",
					Input: mustMarshal(t, tc.input),
				}),
			}
			failure := b.handleRestrictedControlRequest(msg, &output, hooks)
			if (failure == "") != tc.want {
				t.Fatalf("failure=%q, want allowed=%t", failure, tc.want)
			}
			if tc.want {
				if !strings.Contains(output.String(), `"behavior":"allow"`) {
					t.Fatalf("control response = %s, want allow", output.String())
				}
			} else if strings.Contains(output.String(), `"behavior":"allow"`) {
				t.Fatalf("out-of-scope event selector was allowed: %s", output.String())
			}
		})
	}

	var output bytes.Buffer
	msg := claudeSDKMessage{
		RequestID: "req-uncited-read",
		Request: mustMarshal(t, claudeControlRequestPayload{
			Subtype: "tool_use", ToolName: "mcp__brain__brain_read",
			Input: mustMarshal(t, map[string]any{"id": "doc-1", "revision": strings.Repeat("a", 64)}),
		}),
	}
	if failure := b.handleRestrictedControlRequest(msg, &output, hooks); failure == "" ||
		strings.Contains(output.String(), `"behavior":"allow"`) {
		t.Fatalf("uncited read control request was not denied: failure=%q response=%s", failure, output.String())
	}
}

func TestClaudeRestrictedExecutePersistsOnlySafeReceipt(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\nexec " + shellQuote(os.Args[0]) + " -test.run=^TestRestrictedClaudeFakeCLIHelper$ -- \"$@\"\n"
	writeTestExecutable(t, fakePath, []byte(script))
	var logs bytes.Buffer
	backend, err := New("claude", Config{
		ExecutablePath: fakePath,
		CLIVersion:     "2.1.300",
		BuiltinRuntime: true,
		Env:            map[string]string{"IS_SANDBOX": "1", "MULTICA_RESTRICTED_CLAUDE_HELPER": "1"},
		Logger:         slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(t.Context(), "SENSITIVE_PROMPT", ExecOptions{
		ExecutionPolicy: brainAcceptancePolicy(t),
		McpConfig:       json.RawMessage(`{"mcpServers":{"brain":{"type":"http","url":"https://brain.invalid/mcp"},"other":{"type":"http","url":"https://other.invalid/mcp"}}}`),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	var messages []Message
	for message := range session.Messages {
		messages = append(messages, message)
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("restricted task status=%q error=%q", result.Status, result.Error)
	}
	if result.SessionID != "" {
		t.Fatalf("restricted task retained session id %q", result.SessionID)
	}
	for _, secret := range []string{"SENSITIVE_DOCUMENT_BODY", "SENSITIVE_ASSISTANT_TEXT", "SENSITIVE_THINKING", "SENSITIVE_INPUT", "SENSITIVE_FINAL_OUTPUT", "SENSITIVE_SESSION", "SENSITIVE_STDERR", "SENSITIVE_PROMPT", "doc-1", revisionHashFixture()} {
		if strings.Contains(result.Output, secret) || strings.Contains(result.Error, secret) || strings.Contains(logs.String(), secret) {
			t.Fatalf("restricted task persisted %s", secret)
		}
	}
	if !strings.Contains(result.Output, `"status":"verified"`) || !strings.Contains(result.Output, `"durationSource":"pre_post_tool_hook_monotonic"`) {
		t.Fatalf("safe result omitted operational verification or hook timing: %s", result.Output)
	}
	for _, message := range messages {
		if message.SessionID != "" {
			t.Fatalf("restricted message retained session id: %+v", message)
		}
		switch message.Type {
		case MessageText, MessageThinking, MessageLog:
			t.Fatalf("restricted task exposed private provider content: %+v", message)
		case MessageToolUse:
			if message.Input != nil || !strings.HasPrefix(message.Tool, "mcp__brain__") {
				t.Fatalf("unsafe restricted tool-use event: %+v", message)
			}
		case MessageToolResult:
			if !message.DurationMeasured || message.DurationMS < 0 {
				t.Fatalf("restricted tool result omitted hook timing: %+v", message)
			}
			for _, private := range []string{"doc-1", revisionHashFixture(), "SENSITIVE_DOCUMENT_BODY"} {
				if strings.Contains(message.Output, private) {
					t.Fatalf("restricted tool result persisted evidence field %q: %+v", private, message)
				}
			}
		}
	}
}

func TestRestrictedClaudeFakeCLIHelper(t *testing.T) {
	if os.Getenv("MULTICA_RESTRICTED_CLAUDE_HELPER") != "1" {
		return
	}
	args := os.Args
	separator := slicesIndex(args, "--")
	if separator < 0 {
		t.Fatal("restricted Claude fake helper did not receive a CLI argument boundary")
	}
	args = args[separator+1:]
	settingsPath := ""
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "--settings" {
			settingsPath = args[index+1]
			break
		}
	}
	if settingsPath == "" {
		t.Fatal("restricted Claude fake helper did not receive hook settings")
	}
	settings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Hooks map[string][]struct {
			Hooks []struct {
				URL string `json:"url"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(settings, &parsed); err != nil {
		t.Fatal(err)
	}
	hookURL := parsed.Hooks["PreToolUse"][0].Hooks[0].URL
	send := func(event restrictedClaudeHookEvent) {
		body, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, hookURL, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("hook status = %d", resp.StatusCode)
		}
		var decision struct {
			HookSpecificOutput struct {
				PermissionDecision string `json:"permissionDecision"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&decision); err != nil {
			t.Fatal(err)
		}
		if event.EventName == "PreToolUse" && decision.HookSpecificOutput.PermissionDecision != "allow" {
			t.Fatalf("fake provider tool was denied: %s", event.ToolName)
		}
	}
	emit := func(event any) {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fmt.Fprintln(os.Stdout, string(encoded))
	}
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')

	revision := revisionHashFixture()
	contextPayload := fmt.Sprintf(`{"schemaVersion":"silvermediums.brain.v2","servedAt":"2000-01-01T00:00:00Z","dataPolicy":"reference-only","connection":{"state":"online","checkedAt":"2000-01-01T00:00:00Z"},"project":{"id":"brain"},"documents":[{"id":"doc-1","revision":%q}]}`, revision)
	body := "SENSITIVE_DOCUMENT_BODY"
	documentRevision := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	readPayload := fmt.Sprintf(`{"schemaVersion":"silvermediums.brain.v2","servedAt":"2000-01-01T00:00:00Z","dataPolicy":"reference-only","connection":{"state":"online","checkedAt":"2000-01-01T00:00:00Z"},"document":{"id":"doc-1","project":"brain","revision":%q,"body":%q,"bytes":%d,"truncated":false,"currentRevision":%q,"available":true}}`, documentRevision, body, len([]byte(body)), strings.Repeat("e", 64))
	toolCall := func(name, id string, input any, text string, failed ...bool) {
		isError := len(failed) > 0 && failed[0]
		send(restrictedClaudeHookEvent{EventName: "PreToolUse", ToolName: "mcp__brain__" + name, ToolInput: input.(map[string]any), ToolUseID: id})
		response := map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": isError}
		send(restrictedClaudeHookEvent{EventName: "PostToolUse", ToolName: "mcp__brain__" + name, ToolResponse: mustMarshal(t, response), ToolUseID: id})
		emit(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_use", "id": id, "name": "mcp__brain__" + name, "input": input},
		}}})
		emit(map[string]any{"type": "user", "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": isError, "content": []any{map[string]any{"type": "text", "text": text}}},
		}}})
	}
	emit(map[string]any{"type": "system", "subtype": "init", "session_id": "SENSITIVE_SESSION"})
	evaluating := os.Getenv("MULTICA_RESTRICTED_CLAUDE_EVALUATION") == "1"
	if evaluating {
		for index, item := range []struct{ project, question string }{
			{"brain", "How do I retrieve evidence beyond the short search excerpt?"},
			{"brain", taskpolicy.BrainAuthorityQuestion},
			{"multica", "A worker completed its task. Has the owner accepted the issue?"},
		} {
			doc := item.project + "-doc"
			scopedContext := strings.ReplaceAll(strings.ReplaceAll(contextPayload, `"id":"brain"`, `"id":"`+item.project+`"`), "doc-1", doc)
			scopedRead := strings.ReplaceAll(strings.ReplaceAll(readPayload, `"project":"brain"`, `"project":"`+item.project+`"`), "doc-1", doc)
			scopedRead = strings.Replace(scopedRead, `"available":true`, `"available":true,"tier":"working","status":"superseded_revision","freshness":"unverified","assured":false,"recordedAt":"2000-01-01T00:00:00Z"`, 1)
			toolCall("brain_context", fmt.Sprintf("context-%d", index), map[string]any{"project": item.project, "q": item.question}, scopedContext)
			toolCall("brain_read", fmt.Sprintf("read-%d", index), map[string]any{"id": doc, "revision": documentRevision}, scopedRead)
		}
		toolCall("brain_resolve", "refusal", map[string]any{"identity": "not-an-enrolled-project"}, "brain request rejected (HTTP 404)", true)
		event := `{"schemaVersion":"silvermediums.brain.v2","servedAt":"2000-01-01T00:00:00Z","dataPolicy":"reference-only","connection":{"state":"online","checkedAt":"2000-01-01T00:00:00Z"},"items":[{"id":199901,"evidence_ref":"source_event:199901","producer":"synthetic","event_type":"synthetic","payload_schema_version":1,"observed_at":"2000-01-01T00:00:00Z","received_at":"2000-01-01T00:00:00Z","complete":true,"scope":{},"payload_sha256":"` + strings.Repeat("a", 64) + `","payload":{}}],"has_more":false,"next_after_id":199901,"snapshot_through_id":199901}`
		toolCall("company_events", "event", map[string]any{"afterId": float64(199900), "throughId": float64(199901), "limit": float64(1)}, event)
	} else {
		toolCall("brain_context", "context-call", map[string]any{"project": "brain", "q": "How do I retrieve evidence beyond the short search excerpt?"}, contextPayload)
		toolCall("brain_read", "read-call", map[string]any{"id": "doc-1", "revision": documentRevision}, readPayload)
	}
	emit(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{
		map[string]any{"type": "text", "text": "SENSITIVE_ASSISTANT_TEXT"},
		map[string]any{"type": "thinking", "thinking": "SENSITIVE_THINKING"},
	}}})
	final := "SENSITIVE_FINAL_OUTPUT"
	if evaluating {
		final = syntheticBrainAcceptanceAnswers()
	}
	emit(map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": "SENSITIVE_SESSION", "result": final})
}

func revisionHashFixture() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte("SENSITIVE_DOCUMENT_BODY")))
}

func slicesIndex(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}
