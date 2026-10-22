//go:build agentintegration

package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// This fixture uses the installed CLI but no model, account, or Brain service.
// Run the entire test process inside a qualified process-local network fence.
func TestClaudeRestrictedInstalledSyntheticDispatch(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" ||
		os.Getenv("MULTICA_CLAUDE_SYNTHETIC_FIXTURE") != "1" {
		t.Skip("requires explicit installed-CLI synthetic fixture opt-in and external network confinement")
	}
	executable := os.Getenv("MULTICA_CLAUDE_FIXTURE_EXECUTABLE")
	if !filepath.IsAbs(executable) {
		t.Fatal("an explicit absolute fixture CLI path is required")
	}
	requireSyntheticNetworkFence(t)
	for _, mode := range []string{"allow", "deny", "refused", "timeout", "malformed", "native_bash", "native_read", "native_write", "native_edit"} {
		t.Run(mode, func(t *testing.T) {
			runClaudeSyntheticDispatch(t, executable, mode)
		})
	}
}

func requireSyntheticNetworkFence(t *testing.T) {
	t.Helper()
	for _, address := range []string{"198.51.100.1:443", "[2001:db8::1]:443"} {
		dialer := net.Dialer{Timeout: time.Second}
		conn, err := dialer.DialContext(t.Context(), "tcp", address)
		if conn != nil {
			_ = conn.Close()
		}
		if !(errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) ||
			errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH)) {
			t.Fatal("fixture network fence must deny non-loopback sockets immediately, not by timeout")
		}
	}
}

func runClaudeSyntheticDispatch(t *testing.T, executable, mode string) {
	t.Helper()
	scratch := t.TempDir()
	sentinel, marker := filepath.Join(scratch, "synthetic-sentinel"), filepath.Join(scratch, "forbidden-output")
	const readCanary = "SYNTHETIC_FORBIDDEN_READ_CANARY"
	if err := os.WriteFile(sentinel, []byte(readCanary), 0600); err != nil {
		t.Fatal(err)
	}
	var dispatches, modelRequests, preHooks, postHooks atomic.Int32
	var mu sync.Mutex
	var protocolFailure string
	fail := func(message string) {
		mu.Lock()
		protocolFailure = message
		mu.Unlock()
	}
	toolName := "mcp__brain__brain_projects"
	injectedTool := toolName
	input := map[string]any{}
	if mode == "deny" {
		input["project"] = "out-of-scope-synthetic-project"
	}
	switch mode {
	case "native_bash":
		injectedTool, input = "Bash", map[string]any{"command": "printf synthetic > " + marker}
	case "native_read":
		injectedTool, input = "Read", map[string]any{"file_path": sentinel}
	case "native_write":
		injectedTool, input = "Write", map[string]any{"file_path": marker, "content": "synthetic"}
	case "native_edit":
		injectedTool, input = "Edit", map[string]any{"file_path": sentinel, "old_string": readCanary, "new_string": "modified"}
	}
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&request) != nil {
			fail("unreadable synthetic MCP request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(request.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "synthetic-brain", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name": "brain_projects", "description": "Synthetic empty project list",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
			}}}
		case "tools/call":
			dispatches.Add(1)
			if request.Params.Name != "brain_projects" {
				fail("unexpected synthetic MCP tool was dispatched")
			}
			result = map[string]any{"isError": false, "content": []any{map[string]string{"type": "text", "text": `{"schemaVersion":"silvermediums.brain.v2","servedAt":"2000-01-01T00:00:00Z","dataPolicy":"synthetic-only","connection":{"state":"online","checkedAt":"2000-01-01T00:00:00Z"},"projects":[],"syntheticCanary":"SYNTHETIC_SOURCE_BODY_CANARY"}`}}}
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID,
				"error": map[string]any{"code": -32601, "message": "Method not found"}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer mcp.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/hello":
			w.WriteHeader(http.StatusOK)
			return
		case "/v1/messages/count_tokens":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"input_tokens":1}`)
			return
		case "/v1/messages":
		default:
			fail("unexpected synthetic API route")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var request struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&request) != nil {
			fail("unreadable synthetic API request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if modelRequests.Add(1) > 6 {
			fail("synthetic API request budget exceeded")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		found := false
		for _, tool := range request.Tools {
			if tool.Name == toolName {
				found = true
			} else if tool.Name != "EndConversation" {
				fail("native or unexpected tool exposed to synthetic API")
			}
		}
		if !found {
			fail("authorized MCP tool was not exposed to synthetic API")
		}
		hasResult := false
		for _, message := range request.Messages {
			if bytes.Contains(message.Content, []byte(readCanary)) && mode != "native_edit" {
				fail("forbidden local read disclosed the synthetic sentinel")
			}
			var blocks []struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(message.Content, &blocks) == nil {
				for _, block := range blocks {
					hasResult = hasResult || block.Type == "tool_result"
				}
			}
		}
		writeSyntheticClaudeSSE(w, injectedTool, input, hasResult)
	}))
	defer api.Close()

	hooks, realHookURL, err := startRestrictedClaudeHooks(brainAcceptancePolicy(t))
	if err != nil {
		t.Fatal(err)
	}
	defer hooks.Close()
	hookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, restrictedClaudeHookBodyLimit+1))
		if err != nil {
			fail("unreadable hook event")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var event restrictedClaudeHookEvent
		if json.Unmarshal(raw, &event) != nil {
			fail("invalid hook event")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if event.EventName == "PreToolUse" {
			preHooks.Add(1)
			switch mode {
			case "timeout":
				select {
				case <-r.Context().Done():
				case <-time.After(4 * time.Second):
				}
				return
			case "malformed":
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, "{invalid")
				return
			}
		} else {
			postHooks.Add(1)
		}
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, realHookURL, bytes.NewReader(raw))
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			fail("synthetic hook forwarding failed")
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	defer hookServer.Close()
	hookURL := hookServer.URL
	if mode == "refused" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		hookURL = "http://" + listener.Addr().String()
		_ = listener.Close()
	}
	settings, err := hooks.settingsJSON(hookURL)
	if err != nil {
		t.Fatal(err)
	}
	settingsPath, mcpPath := filepath.Join(scratch, "settings.json"), filepath.Join(scratch, "mcp.json")
	config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"brain": map[string]string{"type": "http", "url": mcp.URL}}})
	for path, data := range map[string][]byte{settingsPath: settings, mcpPath: config} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	args, err := buildRestrictedClaudeArgs(ExecOptions{ExecutionPolicy: brainAcceptancePolicy(t), Model: "claude-sonnet-4-6", MaxTurns: 3})
	if err != nil {
		t.Fatal(err)
	}
	args = append(args, "--settings", settingsPath, "--setting-sources", "", "--mcp-config", mcpPath)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	hideAgentWindow(cmd)
	cmd.Cancel = func() error {
		signalProcessGroup(cmd, syscall.SIGKILL)
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	cmd.Dir = scratch
	cmd.Env = []string{
		"HOME=" + scratch, "CLAUDE_CONFIG_DIR=" + filepath.Join(scratch, "config"), "TMPDIR=" + scratch,
		"PATH=/usr/bin:/bin", "ANTHROPIC_BASE_URL=" + api.URL, "ANTHROPIC_AUTH_TOKEN=synthetic-not-an-account",
		"DISABLE_AUTOUPDATER=1", "DISABLE_UPDATES=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"IS_SANDBOX=1",
	}
	versionCmd := exec.CommandContext(ctx, executable, "--version")
	versionCmd.Env, versionCmd.Dir = cmd.Env, scratch
	versionOutput, err := versionCmd.Output()
	version := strings.TrimSpace(string(versionOutput))
	if err != nil || !claudeVersionAtLeast(version, minRestrictedClaudeVersion) {
		t.Fatal("explicit fixture CLI does not meet the restricted version gate")
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := startOwnedProcessTree(cmd, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	defer signalProcessGroup(cmd, syscall.SIGKILL)
	if err := writeClaudeInput(stdin, "SYNTHETIC_PROMPT_CANARY. Execute the single synthetic project-list tool once, then finish."); err != nil {
		cancel()
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	sawResult := false
	for scanner.Scan() {
		var event claudeSDKMessage
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if event.Type == "control_request" {
			// Isolate the HTTP hook boundary: do not mask a broken hook with
			// the backend's independent input gate in negative cases.
			response := map[string]any{"type": "control_response", "response": map[string]any{
				"subtype": "success", "request_id": event.RequestID,
				"response": map[string]string{"behavior": "allow"},
			}}
			_ = json.NewEncoder(stdin).Encode(response)
		}
		if event.Type == "result" {
			sawResult = true
			_ = stdin.Close()
		}
	}
	_ = stdin.Close()
	waitErr := cmd.Wait()
	if ctx.Err() != nil || scanner.Err() != nil {
		t.Fatal("installed CLI did not finish the bounded synthetic fixture")
	}
	mu.Lock()
	failure := protocolFailure
	mu.Unlock()
	if failure != "" {
		t.Fatal(failure)
	}
	if !sawResult || modelRequests.Load() == 0 {
		t.Fatalf("fixture never reached scripted API result; exit=%v stderr=%s", waitErr, stderr.String())
	}
	wantDispatches := int32(0)
	if mode == "allow" {
		wantDispatches = 1
		if preHooks.Load() != 1 || postHooks.Load() != 1 || hooks.Failure() != "" {
			t.Fatalf("allowed dispatch was not correlated to pre/post hooks: pre=%d post=%d", preHooks.Load(), postHooks.Load())
		}
	} else if mode != "refused" && !strings.HasPrefix(mode, "native_") && (preHooks.Load() != 1 || postHooks.Load() != 0) {
		t.Fatalf("negative dispatch did not reach its HTTP hook exactly once: pre=%d post=%d", preHooks.Load(), postHooks.Load())
	}
	if strings.HasPrefix(mode, "native_") {
		backendEnv := make(map[string]string)
		for _, entry := range cmd.Env {
			key, value, _ := strings.Cut(entry, "=")
			backendEnv[key] = value
		}
		backendEnv["HOME"], backendEnv["CLAUDE_CONFIG_DIR"] = filepath.Join(scratch, "backend-home"), filepath.Join(scratch, "backend-config")
		for _, path := range []string{backendEnv["HOME"], backendEnv["CLAUDE_CONFIG_DIR"]} {
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		backend, err := New("claude", Config{
			ExecutablePath: executable, CLIVersion: version, BuiltinRuntime: true,
			Env: backendEnv, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if err != nil {
			t.Fatal(err)
		}
		session, err := backend.Execute(ctx, "SYNTHETIC_PROMPT_CANARY", ExecOptions{
			Cwd: scratch, ExecutionPolicy: brainAcceptancePolicy(t), McpConfig: config,
			Timeout: 10 * time.Second, EvaluateBrainAcceptance: EvaluateBrainAcceptance,
		})
		if err != nil {
			t.Fatal("native replay failed before task execution")
		}
		for message := range session.Messages {
			if message.Type == MessageText || message.Type == MessageThinking || message.Input != nil ||
				strings.Contains(message.Content, readCanary) {
				t.Fatal("native replay exposed private content")
			}
		}
		result := <-session.Result
		rejectedTool := result.Error == "restricted Claude attempted an unauthorized or uncorrelated tool call" ||
			result.Error == "restricted Claude attempted a tool call outside the authorized read scope" ||
			result.Error == "restricted Brain MCP execution returned an unverifiable tool result"
		if result.Status != "failed" || !rejectedTool {
			t.Fatalf("actual backend did not fail injected native action: status=%s error=%s", result.Status, result.Error)
		}
		if strings.Contains(result.Output+result.Error, readCanary) {
			t.Fatal("native replay disclosed the read canary in the result")
		}
		content, err := os.ReadFile(sentinel)
		if err != nil || string(content) != readCanary {
			t.Fatal("injected native action modified the synthetic sentinel")
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("injected native action executed or wrote a file")
		}
	}
	if dispatches.Load() != wantDispatches {
		t.Fatalf("hook mode=%s dispatched %d MCP calls; want %d", mode, dispatches.Load(), wantDispatches)
	}
	var transcripts []string
	var persistedBytes int64
	walkErr := filepath.WalkDir(scratch, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
			return errors.New("unexpected scratch file")
		}
		persistedBytes += info.Size()
		if persistedBytes > 32<<20 {
			return errors.New("scratch byte limit exceeded")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, canary := range []string{"SYNTHETIC_SOURCE_BODY_CANARY", "SYNTHETIC_FINAL_ANSWER_CANARY", "SYNTHETIC_PROMPT_CANARY"} {
			if bytes.Contains(content, []byte(canary)) {
				return errors.New("synthetic source, answer, or prompt persisted in CLI scratch")
			}
		}
		relative, err := filepath.Rel(scratch, path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".jsonl") && !strings.Contains(relative, "mcp-logs-brain"+string(filepath.Separator)) {
			transcripts = append(transcripts, relative)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("synthetic CLI scratch privacy check failed: %v", walkErr)
	}
	if len(transcripts) != 0 {
		t.Fatalf("installed CLI created JSONL despite no-session-persistence: %v", transcripts)
	}
	t.Logf("synthetic-only mode=%s dispatches=%d pre_hooks=%d post_hooks=%d api_requests=%d", mode, dispatches.Load(), preHooks.Load(), postHooks.Load(), modelRequests.Load())
}

func writeSyntheticClaudeSSE(w http.ResponseWriter, toolName string, input map[string]any, final bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	emit := func(event string, value any) {
		data, _ := json.Marshal(value)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	}
	emit("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_synthetic", "type": "message", "role": "assistant", "model": "claude-sonnet-4-6",
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]int{"input_tokens": 1, "output_tokens": 0},
	}})
	block := map[string]any{"type": "tool_use", "id": "synthetic-call", "name": toolName, "input": map[string]any{}}
	delta := map[string]any{"type": "input_json_delta"}
	encoded, _ := json.Marshal(input)
	delta["partial_json"] = string(encoded)
	stop := "tool_use"
	if final {
		block = map[string]any{"type": "text", "text": ""}
		delta = map[string]any{"type": "text_delta", "text": "SYNTHETIC_FINAL_ANSWER_CANARY"}
		stop = "end_turn"
	}
	emit("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": block})
	emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": delta})
	emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	emit("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 1}})
	emit("message_stop", map[string]string{"type": "message_stop"})
}
