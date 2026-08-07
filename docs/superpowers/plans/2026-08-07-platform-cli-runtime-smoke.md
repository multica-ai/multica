# Platform CLI Runtime Smoke Test Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a repeatable Multica integration test that launches the external Platform Agent CLI through the production Codex backend and verifies a completed mock turn.

**Architecture:** A build-tagged Go test reads an explicit executable path from the environment, validates the binary and version, constructs Multica's existing Codex backend, executes one turn, drains the real message stream, and asserts the Result contract. No production runtime code changes are required because Custom Runtime Profile compatibility is already implemented.

**Tech Stack:** Go 1.26.1, Multica `server/pkg/agent`, Codex App Server JSON-RPC over stdio, Go `agentintegration` build tag.

## Global Constraints

- Work on branch `codex/platform-cli-runtime-smoke`.
- Do not modify Codex backend or daemon production behavior.
- Do not add a private protocol family.
- The test must call `requireRealAgentSmoke(t)` before executable lookup.
- The test must require `MULTICA_PLATFORM_AGENT_CLI_PATH`.
- The external CLI must be invoked through `New("codex", Config{ExecutablePath: path})`.
- The test remains excluded from default Go test runs.
- Code comments are English.

---

### Task 1: Real Platform CLI Codex Compatibility Test

**Files:**

- Create: `server/pkg/agent/platform_cli_integration_test.go`
- Test: `server/pkg/agent/platform_cli_integration_test.go`

**Interfaces:**

- Consumes: `MULTICA_RUN_REAL_AGENT_SMOKE=1`.
- Consumes: absolute executable path from `MULTICA_PLATFORM_AGENT_CLI_PATH`.
- Produces: `TestPlatformAgentCLIRealCodexCompatibility(t *testing.T)`.
- Produces: source-level evidence that the external CLI satisfies Multica's production Codex backend contract.

- [ ] **Step 1: Add the build-tagged integration test**

```go
//go:build agentintegration

package agent

import (
    "context"
    "fmt"
    "io"
    "log/slog"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func TestPlatformAgentCLIRealCodexCompatibility(t *testing.T) {
    requireRealAgentSmoke(t)
    if testing.Short() {
        t.Skip("skipping external Platform Agent CLI smoke test in -short mode")
    }

    path := strings.TrimSpace(os.Getenv("MULTICA_PLATFORM_AGENT_CLI_PATH"))
    if path == "" {
        t.Skip("set MULTICA_PLATFORM_AGENT_CLI_PATH to the Platform Agent CLI executable")
    }
    if !filepath.IsAbs(path) {
        t.Fatalf("MULTICA_PLATFORM_AGENT_CLI_PATH must be absolute, got %q", path)
    }
    info, err := os.Stat(path)
    if err != nil {
        t.Fatalf("stat Platform Agent CLI: %v", err)
    }
    if !info.Mode().IsRegular() {
        t.Fatalf("Platform Agent CLI path is not a regular file: %s", path)
    }
    if info.Mode().Perm()&0o111 == 0 {
        t.Fatalf("Platform Agent CLI path is not executable: %s", path)
    }

    versionCtx, cancelVersion := context.WithTimeout(context.Background(), 5*time.Second)
    versionOutput, err := exec.CommandContext(versionCtx, path, "--version").CombinedOutput()
    cancelVersion()
    if err != nil {
        t.Fatalf("Platform Agent CLI --version failed: %v (%s)", err, strings.TrimSpace(string(versionOutput)))
    }
    version := strings.TrimSpace(string(versionOutput))
    if !strings.Contains(strings.ToLower(version), "platform-agent-cli") {
        t.Fatalf("unexpected Platform Agent CLI version output: %q", version)
    }

    logger := slog.New(slog.NewTextHandler(io.Discard, nil))
    backend, err := New("codex", Config{
        ExecutablePath: path,
        CLIVersion:     version,
        CodexVersion:   version,
        Logger:         logger,
    })
    if err != nil {
        t.Fatalf("new Codex backend: %v", err)
    }

    const prompt = "multica source integration smoke"
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    session, err := backend.Execute(ctx, prompt, ExecOptions{
        Cwd:              t.TempDir(),
        Timeout:          25 * time.Second,
        HandshakeTimeout: 5 * time.Second,
    })
    if err != nil {
        t.Fatalf("execute Platform Agent CLI: %v", err)
    }

    messagesCh := make(chan []Message, 1)
    go func() {
        var messages []Message
        for message := range session.Messages {
            messages = append(messages, message)
        }
        messagesCh <- messages
    }()

    var result Result
    select {
    case result = <-session.Result:
    case <-ctx.Done():
        t.Fatal("timeout waiting for Platform Agent CLI result")
    }
    messages := <-messagesCh

    if result.Status != "completed" {
        t.Fatalf("Platform Agent CLI run status=%q error=%q", result.Status, result.Error)
    }
    if result.Error != "" {
        t.Fatalf("Platform Agent CLI returned error %q", result.Error)
    }
    wantOutput := fmt.Sprintf("Mock Runtime 已收到任务：%s", prompt)
    if result.Output != wantOutput {
        t.Fatalf("output=%q, want %q", result.Output, wantOutput)
    }
    if result.SessionID == "" {
        t.Fatal("expected a non-empty Codex thread session ID")
    }

    textMessages := 0
    for _, message := range messages {
        switch message.Type {
        case MessageText:
            textMessages++
        case MessageToolUse, MessageToolResult:
            t.Fatalf("Phase 0 CLI emitted unexpected tool message: %+v", message)
        }
    }
    if textMessages == 0 {
        t.Fatal("expected at least one streamed text message")
    }
    t.Logf("Platform Agent CLI smoke OK: version=%q session=%s output=%q", version, result.SessionID, result.Output)
}
```

- [ ] **Step 2: Verify the test fails with an incompatible executable**

Run:

```bash
cd server
MULTICA_RUN_REAL_AGENT_SMOKE=1 \
MULTICA_PLATFORM_AGENT_CLI_PATH=/usr/bin/false \
go test -tags=agentintegration ./pkg/agent \
  -run TestPlatformAgentCLIRealCodexCompatibility -count=1 -v
```

Expected: FAIL at `Platform Agent CLI --version failed`. This demonstrates that the test rejects an incompatible executable before protocol execution.

- [ ] **Step 3: Verify the test passes with the real Platform Agent CLI**

Run:

```bash
cd server
MULTICA_RUN_REAL_AGENT_SMOKE=1 \
MULTICA_PLATFORM_AGENT_CLI_PATH=/Users/zxx/Documents/技术学习/platform-agent-cli/bin/platform-agent-cli \
go test -tags=agentintegration ./pkg/agent \
  -run TestPlatformAgentCLIRealCodexCompatibility -count=1 -v
```

Expected: PASS with status `completed`, session `thread-1`, and output `Mock Runtime 已收到任务：multica source integration smoke`.

- [ ] **Step 4: Format the test**

Run:

```bash
gofmt -w server/pkg/agent/platform_cli_integration_test.go
```

- [ ] **Step 5: Commit the integration test**

```bash
git add server/pkg/agent/platform_cli_integration_test.go
git commit -m "test(agent): cover external platform CLI runtime"
```

### Task 2: Regression and Change-Record Verification

**Files:**

- Verify: `server/pkg/agent/platform_cli_integration_test.go`
- Verify: `docs/superpowers/specs/2026-08-07-platform-cli-runtime-smoke-design.md`
- Verify: `docs/superpowers/plans/2026-08-07-platform-cli-runtime-smoke.md`

**Interfaces:**

- Consumes: the committed design and integration test.
- Produces: a clean default test result and reviewable Git history.

- [ ] **Step 1: Run the default package regression**

```bash
cd server
go test ./pkg/agent -count=1
```

Expected: PASS. The external CLI smoke is excluded without `-tags=agentintegration`.

- [ ] **Step 2: Run the focused real-binary smoke again**

```bash
cd server
MULTICA_RUN_REAL_AGENT_SMOKE=1 \
MULTICA_PLATFORM_AGENT_CLI_PATH=/Users/zxx/Documents/技术学习/platform-agent-cli/bin/platform-agent-cli \
go test -tags=agentintegration ./pkg/agent \
  -run TestPlatformAgentCLIRealCodexCompatibility -count=1 -v
```

Expected: PASS.

- [ ] **Step 3: Run repository checks for the changed Go package**

```bash
cd server
go vet ./pkg/agent
```

Expected: exit 0.

- [ ] **Step 4: Verify Git history and diff**

```bash
git status --short --branch
git log --oneline -3
git diff origin/main...HEAD --check
git diff --stat origin/main...HEAD
```

Expected: the branch contains separate design, implementation-plan, and integration-test commits; the diff check is clean; and the working tree has no uncommitted changes.
