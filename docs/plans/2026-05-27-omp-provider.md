# oh-my-pi (omp) Provider Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Add "omp" (oh-my-pi) as a first-class agent provider alongside the existing "pi" provider in Multica's Go backend.

**Architecture:** Follow the exact pattern of the existing `piBackend` — a new `ompBackend` struct implementing the `Backend` interface by spawning `omp -p --mode json` and parsing the JSON event stream on stdout. oh-my-pi uses the same event types as pi (`agent_start`, `message_update`, `tool_execution_start`, `tool_execution_end`, `turn_end`, `error`, `auto_retry_end`) with only minor structural differences in the JSON payloads.

**Key differences from pi:**
- Binary: `omp` (not `pi`)
- Session management: omp auto-manages sessions at `~/.omp/agent/sessions/`; multica captures the session ID from stdout's initial header line and passes `--resume <id>` on subsequent turns (no explicit session file path)
- Model discovery: `omp --list-models` outputs `provider model context max-out ...` format (same first two columns)
- Auto-approve: pass `--auto-approve` (or `--yolo`) instead of relying on environment

**Tech Stack:** Go 1.23+, omp CLI (Bun/TypeScript), standard library only

---

### Task 1: Add omp model discovery

**Objective:** Register omp in `ListModels()` so the UI model dropdown works.

**Files:**
- Modify: `server/pkg/agent/models.go:88-98` (add case in switch)

**Step 1: Add the `"omp"` case to `ListModels()`**

In `models.go`, after the `"pi"` case (~line 91), add:

```go
case "omp":
    return cachedDiscovery(providerType, func() ([]Model, error) {
        return discoverOmpModels(ctx, executablePath)
    })
```

**Step 2: Add `discoverOmpModels()` function**

Append to `models.go`:

```go
// discoverOmpModels runs `omp --list-models` and parses its tabular output.
// The output format matches pi's: `provider  model  context  max-out  ...`
// columns. On any failure we fall back to an empty list.
func discoverOmpModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = "omp"
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return []Model{}, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executablePath, "--list-models")
	hideAgentWindow(cmd)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return []Model{}, nil
	}
	text := string(stdout)
	if strings.TrimSpace(text) == "" {
		text = stderr.String()
	}
	return parseOmpModels(text), nil
}
```

**Step 3: Add `parseOmpModels()` function**

The omp `--list-models` output groups into two sections: "Canonical models" and "Provider models". Both have the same column structure: first field is provider, second is model. Reuse the same parsing logic as `parsePiModels`.

```go
// parseOmpModels parses `omp --list-models` output. omp prints two
// sections (Canonical models, Provider models) each as a table with
// `provider  model  context  max-out  ...` columns. We extract
// provider/model from every data row and skip header rows.
func parseOmpModels(output string) []Model {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var models []Model
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		first := fields[0]
		// Skip section headers ("Canonical", "Provider") and column headers
		if strings.EqualFold(first, "provider") || strings.EqualFold(first, "canonical") {
			continue
		}
		id := first + "/" + fields[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		provider := first
		models = append(models, Model{ID: id, Label: id, Provider: provider})
	}
	return models
}
```

**Verification:** Run `go build ./server/...` to confirm compilation. The model dropdown won't work until the full backend exists, but compilation catches type errors.

---

### Task 2: Create the omp backend (`omp.go`)

**Objective:** Implement the full `Backend` interface for the omp CLI.

**Files:**
- Create: `server/pkg/agent/omp.go`

**Step 1: Write the backend struct and Execute method skeleton**

Create `server/pkg/agent/omp.go`. Follow `pi.go` exactly — same shape, different binary and args:

```go
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ompBackend implements Backend by spawning the oh-my-pi CLI in
// non-interactive JSON mode (`omp -p --mode json`) and parsing its
// event stream on stdout.
type ompBackend struct {
	cfg Config
}

func (b *ompBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "omp"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("omp executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}

	// omp manages sessions internally at ~/.omp/agent/sessions/.
	// On first run we omit --resume so omp creates a new session.
	// On resume we pass --resume with the session ID captured from
	// the initial stdout header line.
	resumeID := opts.ResumeSessionID

	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildOmpArgs(prompt, resumeID, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("omp stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[omp:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start omp: %w", err)
	}

	b.cfg.Logger.Info("omp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Close stdout when the context is cancelled so scanner.Scan() unblocks.
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		var output strings.Builder
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)
		var sessionID string

		scanner := bufio.NewScanner(stdout)
		// omp message_update events can embed full message partials,
		// so give the scanner generous headroom.
		scanner.Buffer(make([]byte, 0, 1024*1024), 32*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			// First JSON line in omp --mode json is the session header:
			// {"type":"session","version":3,"id":"...","timestamp":"...","cwd":"..."}
			// Capture the session ID for resume.
			if sessionID == "" {
				var hdr ompSessionHeader
				if err := json.Unmarshal([]byte(line), &hdr); err == nil && hdr.Type == "session" && hdr.ID != "" {
					sessionID = hdr.ID
					continue
				}
			}

			var evt ompStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "agent_start":
				trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

			case "message_update":
				if evt.AssistantMessageEvent == nil {
					continue
				}
				switch evt.AssistantMessageEvent.Type {
				case "text_delta":
					if d := evt.AssistantMessageEvent.Delta; d != "" {
						output.WriteString(d)
						trySend(msgCh, Message{Type: MessageText, Content: d})
					}
				case "thinking_delta":
					if d := evt.AssistantMessageEvent.Delta; d != "" {
						trySend(msgCh, Message{Type: MessageThinking, Content: d})
					}
				}

			case "tool_execution_start":
				trySend(msgCh, Message{
					Type:   MessageToolUse,
					Tool:   evt.ToolName,
					CallID: evt.ToolCallID,
					Input:  decodeOmpArgs(evt.Args),
				})

			case "tool_execution_end":
				trySend(msgCh, Message{
					Type:   MessageToolResult,
					CallID: evt.ToolCallID,
					Output: decodeOmpResult(evt.Result),
				})

			case "turn_end":
				if usage := decodeOmpUsage(evt.Message); usage != nil {
					model := usage.Model
					if model == "" {
						model = opts.Model
					}
					if model == "" {
						model = "unknown"
					}
					u := usageMap[model]
					u.InputTokens += usage.Input
					u.OutputTokens += usage.Output
					u.CacheReadTokens += usage.CacheRead
					u.CacheWriteTokens += usage.CacheWrite
					usageMap[model] = u
				}

			case "error":
				errText := decodeOmpString(evt.Message)
				trySend(msgCh, Message{Type: MessageError, Content: errText})
				if finalStatus == "completed" {
					finalStatus = "failed"
					finalError = errText
				}

			case "auto_retry_end":
				if !evt.Success && finalStatus == "completed" {
					finalStatus = "failed"
					if evt.FinalError != "" {
						finalError = evt.FinalError
					} else {
						finalError = "omp exhausted automatic retries"
					}
				}
			}
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("omp timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("omp exited with error: %v", waitErr)
		}

		b.cfg.Logger.Info("omp finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}
```

**Step 2: Add event types**

Below the `Execute` method, add the omp-specific JSON types:

```go
// ── omp event types ──

// ompSessionHeader is the first JSON line emitted by `omp --mode json`
// in print mode. We capture the session ID for later resume.
type ompSessionHeader struct {
	Type    string `json:"type"`
	Version int    `json:"version,omitempty"`
	ID      string `json:"id"`
	Cwd     string `json:"cwd,omitempty"`
}

// ompStreamEvent is the union of fields consumed from omp's JSON event
// stream. The event shape matches pi's with minor differences: result
// can be any JSON value (not just string), and tool_execution_end uses
// isError (boolean) vs is_error.
type ompStreamEvent struct {
	Type string `json:"type"`

	// message_update
	AssistantMessageEvent *ompAssistantMessageEvent `json:"assistantMessageEvent,omitempty"`

	// tool_execution_start / tool_execution_end
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	IsError    bool            `json:"isError,omitempty"`

	// error / turn_end: Message can be string or object
	Message json.RawMessage `json:"message,omitempty"`

	// auto_retry_end
	Success    bool   `json:"success,omitempty"`
	FinalError string `json:"finalError,omitempty"`
}

type ompAssistantMessageEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta,omitempty"`
}

type ompTurnEndMessage struct {
	Role  string    `json:"role,omitempty"`
	Model string    `json:"model,omitempty"`
	Usage *ompUsage `json:"usage,omitempty"`
}

type ompUsage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	CacheRead   int64 `json:"cacheRead"`
	CacheWrite  int64 `json:"cacheWrite"`
	TotalTokens int64 `json:"totalTokens"`
}
```

**Step 3: Add decode helpers**

```go
func decodeOmpArgs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func decodeOmpResult(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func decodeOmpString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

func decodeOmpUsage(raw json.RawMessage) *ompTurnEndMessage {
	if len(raw) == 0 {
		return nil
	}
	var m ompTurnEndMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	if m.Usage == nil {
		return nil
	}
	return &m
}
```

**Step 4: Add argument builder**

```go
// ── Arg builder ──

// ompBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var ompBlockedArgs = map[string]blockedArgMode{
	"-p":        blockedStandalone, // non-interactive mode
	"--print":   blockedStandalone, // alias for -p
	"--mode":    blockedWithValue,  // "json" event stream protocol
	"--resume":  blockedWithValue,  // daemon manages session resume
	"--session": blockedWithValue,  // alias for --resume
}

// buildOmpArgs assembles the argv for a one-shot omp invocation.
//
// Flags:
//   -p                          non-interactive mode
//   --mode json                 emit one JSON event per line on stdout
//   --resume <id>               resume a previous session (only on resume)
//   --provider <name>           provider override
//   --model <id>                model identifier
//   --append-system-prompt <s>  extra system instructions
//   --auto-approve              skip tool-approval prompts in daemon mode
//
// Custom args appended before the positional prompt.
func buildOmpArgs(prompt, resumeID string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--mode", "json",
		"--auto-approve",
	}
	if resumeID != "" {
		args = append(args, "--resume", resumeID)
	}
	if opts.Model != "" {
		provider, model := splitOmpModel(opts.Model)
		if provider != "" {
			args = append(args, "--provider", provider)
		}
		if model != "" {
			args = append(args, "--model", model)
		}
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, ompBlockedArgs, logger)...)
	args = append(args, prompt)
	return args
}

// splitOmpModel parses a "provider/model" string into parts.
func splitOmpModel(s string) (provider, model string) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "/"); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return "", s
}
```

**Verification:** Run `go build ./server/...` to confirm compilation.

---

### Task 3: Register omp in the agent factory

**Objective:** Wire the new backend into the agent dispatch so `New("omp", cfg)` returns an `ompBackend`.

**Files:**
- Modify: `server/pkg/agent/agent.go:107-132` (add case in switch)

**Step 1: Add `"omp"` case to `New()`**

After the `"pi"` case (~line 123), add:

```go
case "omp":
    return &ompBackend{cfg: cfg}, nil
```

**Step 2: Add launch header**

In `launchHeaders` map, after `"pi"` (~line 155), add:

```go
"omp": "omp (json mode)",
```

**Step 3: Update the switch's default error message and godoc comment**

In the godoc comment on line 101, add `"omp"` to the supported types list. In the default error on line 131, add `"omp"` to the supported list.

**Verification:** Run `go build ./server/...` — should compile cleanly.

---

### Task 4: Add omp to agent tests

**Objective:** Ensure omp is recognized in the agent registry test.

**Files:**
- Modify: `server/pkg/agent/agent_test.go`

**Step 1: Add "omp" to the expected types list**

In `agent_test.go`, find the test that enumerates agent types (around line 75) and add `"omp"` to the list:

```go
"hermes", "kimi", "kiro", "omp", "openclaw", "opencode", "pi",
```

**Verification:** Run `go test ./server/pkg/agent/ -run TestAgentTypes -v`

---

### Task 5: Create omp backend tests

**Objective:** Test argument building and event parsing for the omp backend.

**Files:**
- Create: `server/pkg/agent/omp_test.go`

**Step 1: Write argument-building tests**

```go
package agent

import (
	"log/slog"
	"strings"
	"testing"
)

func TestBuildOmpArgsBasicFlags(t *testing.T) {
	args := buildOmpArgs("hello world", "", ExecOptions{
		Model:        "anthropic/claude-sonnet-4-20250514",
		SystemPrompt: "be helpful",
	}, slog.Default())

	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-p",
		"--mode json",
		"--auto-approve",
		"--provider anthropic",
		"--model claude-sonnet-4-20250514",
		"--append-system-prompt",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in args, got: %v", want, args)
		}
	}

	// Prompt must be the last positional argument.
	if args[len(args)-1] != "hello world" {
		t.Errorf("prompt should be last arg, got %q", args[len(args)-1])
	}
}

func TestBuildOmpArgsResume(t *testing.T) {
	args := buildOmpArgs("continue", "abc123", ExecOptions{}, slog.Default())

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--resume abc123") {
		t.Errorf("expected --resume abc123 in args, got: %v", args)
	}
}

func TestBuildOmpArgsNoResume(t *testing.T) {
	// When no resume ID, --resume should NOT appear
	args := buildOmpArgs("fresh start", "", ExecOptions{}, slog.Default())

	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--resume") {
		t.Errorf("--resume should not appear for fresh sessions, got: %v", args)
	}
}

func TestBuildOmpArgsNoToolRestriction(t *testing.T) {
	// Like pi, omp should not pass --tools to avoid restricting the tool registry.
	args := buildOmpArgs("test", "", ExecOptions{}, slog.Default())
	for i, arg := range args {
		if arg == "--tools" {
			t.Errorf("buildOmpArgs emits --tools %q; should not restrict tool registry", args[i+1])
		}
	}
}

func TestBuildOmpArgsCustomArgsAppended(t *testing.T) {
	args := buildOmpArgs("prompt", "", ExecOptions{
		CustomArgs: []string{"--no-lsp"},
	}, slog.Default())

	found := false
	for _, arg := range args {
		if arg == "--no-lsp" {
			found = true
		}
	}
	if !found {
		t.Errorf("custom --no-lsp should pass through via custom_args, got: %v", args)
	}
}

func TestBuildOmpArgsBlockedArgsFiltered(t *testing.T) {
	// User-supplied -p or --mode must be filtered out
	args := buildOmpArgs("p", "", ExecOptions{
		CustomArgs: []string{"-p", "--mode", "text"},
	}, slog.Default())

	// Count occurrences of -p in args (there should be exactly 1, from hardcoded)
	count := 0
	for _, arg := range args {
		if arg == "-p" {
			count++
		}
		if arg == "--mode" {
			t.Errorf("--mode from custom_args should be filtered, got: %v", args)
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one -p (the hardcoded one), got %d in: %v", count, args)
	}
}
```

**Verification:** Run `go test ./server/pkg/agent/ -run TestBuildOmp -v` — all should pass.

---

### Task 6: Write model parsing tests

**Objective:** Verify that `parseOmpModels` correctly extracts provider/model from omp's `--list-models` output.

**Files:**
- Modify: `server/pkg/agent/omp_test.go` (add to the file from Task 5)

**Step 1: Add model parsing tests**

```go
func TestParseOmpModelsProviderSection(t *testing.T) {
	output := `Provider models
provider  model         context  max-out  thinking  images
anthropic claude-sonnet  200000   32000   low,high  yes
openai    gpt-5.2        128000   16000   off,low   yes
`

	models := parseOmpModels(output)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %v", len(models), models)
	}
	if models[0].ID != "anthropic/claude-sonnet" {
		t.Errorf("expected anthropic/claude-sonnet, got %s", models[0].ID)
	}
	if models[0].Provider != "anthropic" {
		t.Errorf("expected provider=anthropic, got %s", models[0].Provider)
	}
	if models[1].ID != "openai/gpt-5.2" {
		t.Errorf("expected openai/gpt-5.2, got %s", models[1].ID)
	}
}

func TestParseOmpModelsCanonicalSection(t *testing.T) {
	output := `Canonical models
canonical    selected               variants  context  max-out
claude-sonnet anthropic/claude-sonnet  2      200000   32000
`

	models := parseOmpModels(output)
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %v", len(models), models)
	}
	if models[0].ID != "anthropic/claude-sonnet" {
		t.Errorf("expected anthropic/claude-sonnet, got %s", models[0].ID)
	}
}

func TestParseOmpModelsDeduplication(t *testing.T) {
	// Same model in both sections — only one entry
	output := `Canonical models
canonical    selected               variants  context  max-out
claude-sonnet anthropic/claude-sonnet  2      200000   32000

Provider models
provider  model         context  max-out  thinking  images
anthropic claude-sonnet  200000   32000   low,high  yes
`

	models := parseOmpModels(output)
	if len(models) != 1 {
		t.Fatalf("expected 1 deduplicated model, got %d: %v", len(models), models)
	}
}

func TestParseOmpModelsEmpty(t *testing.T) {
	models := parseOmpModels("")
	if len(models) != 0 {
		t.Errorf("expected 0 models for empty input, got %d", len(models))
	}
}

func TestParseOmpModelsNoModelsMessage(t *testing.T) {
	models := parseOmpModels("No models available. Set API keys in environment variables.")
	if len(models) != 0 {
		t.Errorf("expected 0 models for no-models message, got %d", len(models))
	}
}
```

**Verification:** Run `go test ./server/pkg/agent/ -run TestParseOmp -v` — all should pass.

---

### Task 7: Full suite verification

**Objective:** Run the full agent test suite to ensure no regressions.

**Step 1: Run all agent tests**

```bash
go test ./server/pkg/agent/ -v
```

Expected: all existing tests pass, and the new omp tests pass.

**Step 2: Build the full server**

```bash
go build ./server/...
```

Expected: clean build, no errors.

**Step 3: Run the full Go test suite**

```bash
cd server && go test ./... 2>&1 | tail -20
```

Expected: no test failures. Any pre-existing failures are unrelated.

---

### Task 8: Commit

```bash
cd /home/ethanturk/multica
git add server/pkg/agent/omp.go server/pkg/agent/omp_test.go server/pkg/agent/agent.go server/pkg/agent/models.go server/pkg/agent/agent_test.go
git commit -m "feat(agent): add omp (oh-my-pi) provider backend

Implements the Backend interface for the omp CLI, following the same
pattern as the existing pi provider. Key differences:

- Binary: omp (vs pi)
- Session management: captures session ID from stdout header, resumes
  via --resume instead of explicit session file paths
- Auto-approve: passes --auto-approve flag instead of relying on env
- Model discovery: parses omp --list-models output (same columnar format)
"
```

---

## Summary

| File | Action | Purpose |
|------|--------|---------|
| `server/pkg/agent/omp.go` | Create | Backend implementation (~300 lines) |
| `server/pkg/agent/omp_test.go` | Create | Argument + model parsing tests |
| `server/pkg/agent/agent.go` | Modify | Add "omp" to New() dispatch + launchHeaders |
| `server/pkg/agent/models.go` | Modify | Add "omp" to ListModels() + discovery/parsing |
| `server/pkg/agent/agent_test.go` | Modify | Add "omp" to expected types list |

**Estimated effort:** ~30-45 minutes for a Go developer following the tasks sequentially.