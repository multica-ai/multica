//go:build agentintegration

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/codexcontext"
	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

func TestOperationalCodexRealBinaryContextBoundary(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to allow real agent CLI execution")
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex not on PATH")
	}
	version, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("codex --version: %v (%s)", err, strings.TrimSpace(string(version)))
	}
	t.Logf("Codex characterization binary: %s", strings.TrimSpace(string(version)))

	const (
		taskSentinel          = "OPERATIONAL-TASK-SENTINEL"
		assignedSentinel      = "ASSIGNED-SKILL-SENTINEL"
		supportSentinel       = "ASSIGNED-SUPPORT-SENTINEL"
		rootAgentSentinel     = "ROOT-AGENTS-AMBIENT-SENTINEL"
		nestedAgentSentinel   = "NESTED-AGENTS-AMBIENT-SENTINEL"
		ambientSkillSentinel  = "AMBIENT-SKILL-SENTINEL"
		staleBriefSentinel    = "STALE-RUNTIME-BRIEF-SENTINEL"
		previousTurnSentinel  = "PREVIOUS-SESSION-SENTINEL"
		repositoryFileContent = "repository-readable"
	)

	var (
		requestsMu sync.Mutex
		requests   []map[string]any
	)
	recorder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			http.Error(w, readErr.Error(), http.StatusBadRequest)
			return
		}
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requestsMu.Lock()
		requests = append(requests, request)
		requestNumber := len(requests)
		requestsMu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		if requestNumber == 1 {
			_ = writeCodexSSE(w,
				map[string]any{"type": "response.created", "response": map[string]any{"id": "resp-1"}},
				map[string]any{"type": "response.output_item.done", "item": map[string]any{
					"type": "function_call", "call_id": "call-shell", "name": "shell_command",
					"arguments": `{"command":"printf shell-ok && test -f repo.txt && grep -q repository-readable repo.txt"}`,
				}},
				completedCodexResponse("resp-1"),
			)
			return
		}
		_ = writeCodexSSE(w,
			map[string]any{"type": "response.created", "response": map[string]any{"id": "resp-2"}},
			map[string]any{"type": "response.output_item.done", "item": map[string]any{
				"type": "message", "role": "assistant", "id": "msg-1",
				"content": []map[string]any{{"type": "output_text", "text": "recording-complete"}},
			}},
			completedCodexResponse("resp-2"),
		)
	}))
	defer recorder.Close()

	root := t.TempDir()
	sharedHome := filepath.Join(root, "shared-codex-home")
	workDir := filepath.Join(root, "repository")
	if err := os.MkdirAll(filepath.Join(workDir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeIntegrationFile(t, filepath.Join(sharedHome, "auth.json"), `{}`)
	writeIntegrationFile(t, filepath.Join(sharedHome, "skills", "ambient", "SKILL.md"), ambientSkillSentinel)
	writeIntegrationFile(t, filepath.Join(workDir, "AGENTS.md"), rootAgentSentinel)
	writeIntegrationFile(t, filepath.Join(workDir, "nested", "AGENTS.md"), nestedAgentSentinel)
	writeIntegrationFile(t, filepath.Join(workDir, "repo.txt"), repositoryFileContent)
	writeIntegrationFile(t, filepath.Join(workDir, ".agent_context", "AGENTS.md"), staleBriefSentinel)
	t.Setenv("CODEX_HOME", sharedHome)

	operational := codexcontext.OperationalContext{
		BaseInstructions:      "You are the Multica operational test agent.",
		DeveloperInstructions: "Use only the explicit task and assigned skills.",
		Prompt:                taskSentinel,
		Skills: []skillbundle.Skill{{
			Name:        "assigned-observer",
			Description: assignedSentinel,
			Content:     "Inspect only the requested operational state.",
			Files:       []skillbundle.File{{Path: "references/check.txt", Content: supportSentinel}},
		}},
	}
	env, err := execenv.Prepare(execenv.PrepareParams{
		WorkspacesRoot: filepath.Join(root, "workspaces"),
		WorkspaceID:    "workspace-1",
		TaskID:         "task-operational-real-binary",
		Provider:       "codex",
		CodexVersion:   strings.TrimSpace(string(version)),
		LocalWorkDir:   workDir,
		CodexContext:   &operational,
		Task:           execenv.TaskContextForEnv{IssueID: "MUL-test"},
	}, slog.Default())
	if err != nil {
		t.Fatalf("prepare operational environment: %v", err)
	}
	// The characterization endpoint is intentionally account-free. Production
	// operational homes keep the shared auth link; this test removes it only so
	// the exact binary cannot consult a developer's logged-in account.
	if err := os.Remove(filepath.Join(env.CodexHome, "auth.json")); err != nil {
		t.Fatalf("remove account auth from recording fixture: %v", err)
	}

	configPath := filepath.Join(env.CodexHome, "config.toml")
	existingConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read operational config: %v", err)
	}
	providerConfig := fmt.Sprintf(`model = "gpt-5.4"
model_provider = "multica-recording"

%s
[model_providers.multica-recording]
name = "Multica recording endpoint"
base_url = %q
env_key = "MULTICA_RECORDING_API_KEY"
wire_api = "responses"
requires_openai_auth = false
	`, existingConfig, recorder.URL+"/v1")
	if err := os.WriteFile(configPath, []byte(providerConfig), 0o600); err != nil {
		t.Fatalf("write recording provider config: %v", err)
	}

	backend, err := agent.New("codex", agent.Config{
		ExecutablePath: path,
		CodexVersion:   strings.TrimSpace(string(version)),
		BuiltinRuntime: true,
		Logger:         slog.Default(),
		Env: map[string]string{
			"CODEX_HOME":                    env.CodexHome,
			"OPENAI_API_KEY":                "recording-only",
			"MULTICA_RECORDING_API_KEY":     "recording-only",
			"MULTICA_PREVIOUS_SESSION_MARK": previousTurnSentinel,
		},
	})
	if err != nil {
		t.Fatalf("create codex backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, operational.Prompt, agent.ExecOptions{
		Cwd:                   env.WorkDir,
		Model:                 "gpt-5.4",
		Timeout:               50 * time.Second,
		HandshakeTimeout:      15 * time.Second,
		ResumeSessionID:       "previous-session-must-not-resume",
		CodexContextMode:      codexcontext.ModeOperational,
		BaseInstructions:      operational.BaseInstructions,
		DeveloperInstructions: operational.DeveloperInstructions,
		McpConfig:             json.RawMessage(`{"mcpServers":{"ambient":{"command":"ambient-mcp-sentinel"}}}`),
	})
	if err != nil {
		t.Fatalf("execute operational codex: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" || !strings.Contains(result.Output, "recording-complete") {
		t.Fatalf("operational result: status=%q error=%q output=%q", result.Status, result.Error, result.Output)
	}

	requestsMu.Lock()
	captured := append([]map[string]any(nil), requests...)
	requestsMu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("recorded %d model requests, want 2", len(captured))
	}
	firstJSON, _ := json.Marshal(captured[0])
	first := string(firstJSON)
	for _, want := range []string{taskSentinel, assignedSentinel} {
		if !strings.Contains(first, want) {
			t.Errorf("first request missing %q", want)
		}
	}
	for _, excluded := range []string{rootAgentSentinel, nestedAgentSentinel, ambientSkillSentinel, staleBriefSentinel, previousTurnSentinel, "ambient-mcp-sentinel"} {
		if strings.Contains(first, excluded) {
			t.Errorf("first request contains excluded context %q", excluded)
		}
	}
	secondJSON, _ := json.Marshal(captured[1])
	if !strings.Contains(string(secondJSON), "shell-ok") {
		t.Errorf("second request does not contain successful shell output: %s", secondJSON)
	}
	supportPath := filepath.Join(env.CodexHome, "skills", "assigned-observer", "references", "check.txt")
	support, err := os.ReadFile(supportPath)
	if err != nil || string(support) != supportSentinel {
		t.Fatalf("assigned support file = %q, %v", support, err)
	}
}

func writeCodexSSE(w io.Writer, events ...map[string]any) error {
	for _, event := range events {
		payload, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal SSE event: %w", err)
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return fmt.Errorf("write SSE event: %w", err)
		}
	}
	return nil
}

func completedCodexResponse(id string) map[string]any {
	return map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": id,
			"usage": map[string]any{
				"input_tokens": 0, "input_tokens_details": nil,
				"output_tokens": 0, "output_tokens_details": nil, "total_tokens": 0,
			},
		},
	}
}

func writeIntegrationFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
