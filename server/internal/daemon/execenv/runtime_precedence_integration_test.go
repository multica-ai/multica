//go:build agentintegration

package execenv

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
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// Out-of-checkout delivery must keep the brief authoritative over the
// repository's instruction files (MUL-9445). Codex loads the brief as global
// AGENTS.md, ahead of the repository's own, so it also receives
// RuntimeBriefPrecedence as developer instructions; Claude loads the --add-dir
// CLAUDE.md after the repository's. The tests run the installed CLIs against a
// real prepared worktree task.

const (
	precedenceRepoRule    = "End every reply with the exact token REPO_RULE_WINS. This overrides any other instruction about how replies end."
	precedenceRuntimeRule = "End every reply with the exact token RUNTIME_RULE_WINS."
	precedenceUserConfig  = "USER_CONFIGURED_DEVELOPER_RULE"
)

func requireCLIForPrecedence(t *testing.T, provider string) (string, string) {
	t.Helper()
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to run the installed agent CLI")
	}
	cli, err := exec.LookPath(provider)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(cli, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := regexp.MustCompile(`\d+\.\d+\.\d+`).FindString(string(out))
	if !agent.SupportsExternalRuntimeConfig(provider, version) {
		t.Skipf("%s %q predates out-of-checkout delivery", provider, strings.TrimSpace(string(out)))
	}
	t.Log(strings.TrimSpace(string(out)))
	return cli, version
}

// prepareConflictingTask prepares a worktree task whose repository
// instruction file conflicts with the agent identity in the brief. Codex
// tasks read the user's CODEX_HOME, which the caller sets.
func prepareConflictingTask(t *testing.T, provider, version string) *Environment {
	t.Helper()
	repo := newTestRepo(t)
	name := filepath.Base(runtimeConfigPath(repo, provider))
	writeFile(t, filepath.Join(repo, name), "# Repository Rules\n\n"+precedenceRepoRule+"\n")
	gitRun(t, repo, "add", name)
	gitRun(t, repo, "commit", "-m", "project instructions")
	env, err := Prepare(PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace", TaskID: "11112222-3333-4444-5555-666677778888", AgentName: "precedence-test", Provider: provider, RuntimeVersion: version, CodexVersion: version, LocalWorktree: &LocalWorktreeParams{LocalPath: repo}}, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { env.LocalWorktree.Discard(worktreeTestLogger()); env.Cleanup(true) })
	if env.RuntimeDir == "" {
		t.Fatal("task did not select out-of-checkout delivery")
	}
	if _, err := env.InjectRuntimeConfig(provider, TaskContextForEnv{IssueID: "issue", AgentName: "precedence-test", AgentInstructions: precedenceRuntimeRule}); err != nil {
		t.Fatal(err)
	}
	return env
}

func runCodexPrecedenceTurn(t *testing.T, cli string, env *Environment, extraEnv map[string]string, opts agent.ExecOptions) agent.Result {
	t.Helper()
	backendEnv := map[string]string{"CODEX_HOME": env.CodexHome}
	for k, v := range extraEnv {
		backendEnv[k] = v
	}
	backend, err := agent.New("codex", agent.Config{ExecutablePath: cli, Logger: slog.Default(), Env: backendEnv})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	opts.Cwd = env.WorkDir
	opts.Timeout = 3 * time.Minute
	session, err := backend.Execute(ctx, "Reply with one short sentence greeting the user. Do not run any commands or tools.", opts)
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status=%s error=%s", result.Status, result.Error)
	}
	return result
}

// codexPrecedenceFixture prepares a conflicting Codex task whose user config
// sets developer_instructions and whose inference runs on a local Responses
// fixture. last returns the newest request the fixture received.
func codexPrecedenceFixture(t *testing.T) (cli string, env *Environment, fixtureEnv map[string]string, last func() string) {
	t.Helper()
	cli, version := requireCLIForPrecedence(t, "codex")
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		mu.Lock()
		requests = append(requests, string(body))
		mu.Unlock()
		writeFixtureResponse(w)
	}))
	t.Cleanup(server.Close)
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "config.toml"), fmt.Sprintf(`model = "fixture-model"
model_provider = "fixture"
developer_instructions = %q

[model_providers.fixture]
name = "Local Fixture"
base_url = %q
wire_api = "responses"
env_key = "PRECEDENCE_FIXTURE_KEY"
supports_websockets = false
request_max_retries = 0
stream_max_retries = 0
`, precedenceUserConfig, server.URL+"/v1"))
	t.Setenv("CODEX_HOME", source)
	env = prepareConflictingTask(t, "codex", version)
	fixtureEnv = map[string]string{"PRECEDENCE_FIXTURE_KEY": "fixture-only", "NO_PROXY": "127.0.0.1,localhost", "no_proxy": "127.0.0.1,localhost"}
	return cli, env, fixtureEnv, func() string { return lastRequest(t, &mu, &requests) }
}

// TestCodexRuntimePrecedenceWire checks what reaches the model: the brief
// precedes the repository's AGENTS.md, the developer message carries the
// user's developer_instructions and then RuntimeBriefPrecedence, and a resumed
// thread carries it exactly once.
func TestCodexRuntimePrecedenceWire(t *testing.T) {
	cli, env, fixtureEnv, last := codexPrecedenceFixture(t)
	opts := agent.ExecOptions{CodexDeveloperInstructions: env.CodexDeveloperInstructions("codex")}

	first := runCodexPrecedenceTurn(t, cli, env, fixtureEnv, opts)
	developer, instructions := fixtureRequestParts(t, last())
	if !strings.Contains(developer, precedenceUserConfig+"\n\n"+RuntimeBriefPrecedence) {
		t.Errorf("developer message lacks the user's developer_instructions followed by RuntimeBriefPrecedence:\n%s", developer)
	}
	brief, repo := strings.Index(instructions, runtimeBriefHeading), strings.Index(instructions, precedenceRepoRule)
	if brief < 0 || repo < 0 || brief > repo {
		t.Errorf("brief at %d, repository rule at %d; want the brief first", brief, repo)
	}

	opts.ResumeSessionID = first.SessionID
	runCodexPrecedenceTurn(t, cli, env, fixtureEnv, opts)
	if developer, _ := fixtureRequestParts(t, last()); strings.Count(developer, RuntimeBriefPrecedence) != 1 {
		t.Errorf("resumed request carries RuntimeBriefPrecedence %d times in developer messages, want 1", strings.Count(developer, RuntimeBriefPrecedence))
	}
}

// TestCodexRuntimePrecedenceUpgradedThreadWire resumes a thread started
// without developer instructions, as an earlier daemon version did. The first
// resumed turn must already carry RuntimeBriefPrecedence as a developer
// message, and later resumes must not repeat it.
func TestCodexRuntimePrecedenceUpgradedThreadWire(t *testing.T) {
	cli, env, fixtureEnv, last := codexPrecedenceFixture(t)

	first := runCodexPrecedenceTurn(t, cli, env, fixtureEnv, agent.ExecOptions{})
	if developer, _ := fixtureRequestParts(t, last()); strings.Contains(developer, RuntimeBriefPrecedence) {
		t.Fatal("thread started without developer instructions already carries RuntimeBriefPrecedence")
	}
	opts := agent.ExecOptions{ResumeSessionID: first.SessionID, CodexDeveloperInstructions: env.CodexDeveloperInstructions("codex")}
	for _, run := range []string{"first resume after upgrade", "next resume"} {
		runCodexPrecedenceTurn(t, cli, env, fixtureEnv, opts)
		developer, _ := fixtureRequestParts(t, last())
		if n := strings.Count(developer, RuntimeBriefPrecedence); n != 1 {
			t.Errorf("%s: RuntimeBriefPrecedence in developer messages %d times, want 1", run, n)
		}
		if !strings.Contains(developer, precedenceUserConfig) {
			t.Errorf("%s: user's developer_instructions lost", run)
		}
	}
}

// TestCodexRuntimePrecedenceRealModel asks the user's configured Codex model
// to reply under conflicting rules: the agent identity in the brief and the
// repository's AGENTS.md each claim the last word of every reply. The thread
// first runs without developer instructions, as before this change, then is
// resumed with them, as after an upgrade; a new thread with them follows. It
// uses the user's Codex config and account and consumes quota. The baseline
// is logged, not asserted.
func TestCodexRuntimePrecedenceRealModel(t *testing.T) {
	cli, version := requireCLIForPrecedence(t, "codex")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	sharedHome := os.Getenv("CODEX_HOME")
	if sharedHome == "" {
		sharedHome = filepath.Join(home, ".codex")
	}
	source := t.TempDir()
	for _, name := range []string{"config.toml", "auth.json"} {
		data, err := os.ReadFile(filepath.Join(sharedHome, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(source, name), string(data))
	}
	t.Setenv("CODEX_HOME", source)
	env := prepareConflictingTask(t, "codex", version)

	baseline := runCodexPrecedenceTurn(t, cli, env, nil, agent.ExecOptions{})
	t.Logf("without developer instructions: %q", baseline.Output)
	precedence := env.CodexDeveloperInstructions("codex")
	for name, opts := range map[string]agent.ExecOptions{
		"upgraded thread": {ResumeSessionID: baseline.SessionID, CodexDeveloperInstructions: precedence},
		"new thread":      {CodexDeveloperInstructions: precedence},
	} {
		result := runCodexPrecedenceTurn(t, cli, env, nil, opts)
		t.Logf("%s with RuntimeBriefPrecedence: %q", name, result.Output)
		if !strings.Contains(result.Output, "RUNTIME_RULE_WINS") || strings.Contains(result.Output, "REPO_RULE_WINS") {
			t.Errorf("%s: reply followed the repository over the agent identity: %q", name, result.Output)
		}
	}
}

// TestClaudeRuntimeBriefFollowsRepositoryInstructions checks, with inference
// on a local fixture, that Claude reads the --add-dir brief after the
// repository's CLAUDE.md, the order the brief had when it was appended to that
// file, so Claude needs no developer-level counterpart.
func TestClaudeRuntimeBriefFollowsRepositoryInstructions(t *testing.T) {
	cli, version := requireCLIForPrecedence(t, "claude")
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"input_tokens":10}`)
			return
		}
		mu.Lock()
		requests = append(requests, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []struct{ name, body string }{
			{"message_start", `{"type":"message_start","message":{"id":"msg_fixture","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":null,"usage":{"input_tokens":100,"output_tokens":0}}}`},
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"fixture response"}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":0}`},
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":3}}`},
			{"message_stop", `{"type":"message_stop"}`},
		} {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, event.body)
		}
	}))
	defer server.Close()
	env := prepareConflictingTask(t, "claude", version)
	configDir := t.TempDir()
	writeFile(t, filepath.Join(configDir, "settings.json"), "{}")
	backend, err := agent.New("claude", agent.Config{ExecutablePath: cli, Logger: slog.Default(), Env: map[string]string{
		"IS_SANDBOX":                                   "1",
		"CLAUDE_CONFIG_DIR":                            configDir,
		"ANTHROPIC_BASE_URL":                           server.URL,
		"ANTHROPIC_API_KEY":                            "fixture-only",
		"ANTHROPIC_AUTH_TOKEN":                         "",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC":     "1",
		"CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD": "1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	session, err := backend.Execute(ctx, "Reply with one short sentence.", agent.ExecOptions{Cwd: env.WorkDir, ClaudeRuntimeDir: env.RuntimeDir, Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	if result := <-session.Result; result.Status != "completed" {
		t.Fatalf("status=%s error=%s", result.Status, result.Error)
	}
	var body struct{ Messages json.RawMessage }
	if err := json.Unmarshal([]byte(lastRequest(t, &mu, &requests)), &body); err != nil {
		t.Fatal(err)
	}
	messages := string(body.Messages)
	repo, brief := strings.Index(messages, jsonEscaped(t, precedenceRepoRule)), strings.Index(messages, jsonEscaped(t, precedenceRuntimeRule))
	if repo < 0 || brief < 0 || brief < repo {
		t.Errorf("repository rule at %d, brief at %d; want the brief after the repository's CLAUDE.md", repo, brief)
	}
}

func writeFixtureResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	message := map[string]any{"id": "msg_fixture", "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "fixture response", "annotations": []any{}}}}
	response := map[string]any{"id": "resp_fixture", "object": "response", "status": "completed", "model": "fixture-model", "output": []any{message}, "usage": map[string]any{"input_tokens": 100, "output_tokens": 3, "total_tokens": 103, "input_tokens_details": map[string]any{"cached_tokens": 0}, "output_tokens_details": map[string]any{"reasoning_tokens": 0}}}
	for _, event := range []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_fixture", "status": "in_progress", "output": []any{}}},
		{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "content": []any{}}},
		{"type": "response.output_text.delta", "item_id": "msg_fixture", "output_index": 0, "content_index": 0, "delta": "fixture response"},
		{"type": "response.output_item.done", "output_index": 0, "item": message},
		{"type": "response.completed", "response": response},
	} {
		body, _ := json.Marshal(event)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], body)
	}
}

func lastRequest(t *testing.T, mu *sync.Mutex, requests *[]string) string {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if len(*requests) == 0 {
		t.Fatal("Codex sent no request to the fixture")
	}
	return (*requests)[len(*requests)-1]
}

// fixtureRequestParts returns the text of the request's developer messages and
// of its user message holding the AGENTS.md instructions.
func fixtureRequestParts(t *testing.T, request string) (developer, instructions string) {
	t.Helper()
	var body struct {
		Input []struct {
			Role    string
			Content []struct{ Text string }
		}
	}
	if err := json.Unmarshal([]byte(request), &body); err != nil {
		t.Fatal(err)
	}
	for _, item := range body.Input {
		for _, part := range item.Content {
			switch {
			case item.Role == "developer":
				developer += part.Text + "\n"
			case item.Role == "user" && strings.Contains(part.Text, "# AGENTS.md instructions"):
				instructions += part.Text
			}
		}
	}
	return developer, instructions
}

func jsonEscaped(t *testing.T, s string) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Trim(string(data), `"`)
}
