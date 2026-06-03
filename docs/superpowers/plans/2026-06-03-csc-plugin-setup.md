# CSC Plugin 安装 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Multica Daemon 的任务执行流程中为 CSC Agent 增加 Plugin 安装步骤，打通 CSC Cloud ↔ CSC CLI 的任务下发链路。

**Architecture:** 在 `execenv.Prepare()` 中为 `provider == "csc"` 增加 `setupCSCPlugins()` 调用，与现有的 `setupCodexHome()` / `setupOpenclawConfig()` 同级。通过 `exec.Command` 执行 CSC CLI 的 `plugin marketplace add` 和 `plugin install` 命令。失败时返回 error，沿 `Prepare → runTask → handleTask → FailTask` 链路上报 Server。

**Tech Stack:** Go 1.26, `os/exec`, `context.WithTimeout`

---

### Task 1: 新增 `setupCSCPlugins()` 函数及单元测试

**Files:**
- Create: `server/internal/daemon/execenv/csc_plugins.go`
- Create: `server/internal/daemon/execenv/csc_plugins_test.go`

- [ ] **Step 1: 编写失败测试**

创建 `server/internal/daemon/execenv/csc_plugins_test.go`，测试 `setupCSCPlugins` 的基本行为。使用临时目录下的 shell 脚本模拟 CSC CLI，遵循 `execenv` 包中 `execenv_test.go` 的测试模式。

```go
package execenv

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeFakeCSC writes a shell script that simulates the csc CLI.
// commands is a map of command substring -> exit code (0=success, 1=fail).
// The script logs every invocation to {dir}/invocations.log for verification.
func writeFakeCSC(t *testing.T, dir string, commands map[string]int) string {
	t.Helper()
	var script strings.Builder
	script.WriteString("#!/bin/sh\n")
	script.WriteString("echo \"$@\" >> " + filepath.Join(dir, "invocations.log") + "\n")
	for substr, exitCode := range commands {
		script.WriteString("echo \"$@\" | grep -q '" + substr + "' && exit " + itoa(exitCode) + "\n")
	}
	script.WriteString("exit 0\n")

	path := filepath.Join(dir, "csc")
	if err := os.WriteFile(path, []byte(script.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoa(n int) string {
	return strings.TrimLeft(strings.Replace(
		strings.Replace(
			strings.Replace(
				strings.Replace(
					strings.Replace(
						strings.Replace(
							strings.Replace(
								strings.Replace(
									strings.Replace(
										strings.Replace(string(n), "1", "1", -1),
										"2", "2", -1),
									"3", "3", -1),
								"4", "4", -1),
							"5", "5", -1),
						"6", "6", -1),
					"7", "7", -1),
				"8", "8", -1),
			"9", "9", -1),
		"0", "0", -1), " ")
}

func readInvocations(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "invocations.log"))
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestSetupCSCPlugins_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, nil) // all commands succeed
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), fakeBin, workDir, slog.Default())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	invocations := readInvocations(t, dir)
	if len(invocations) < 2 {
		t.Fatalf("expected at least 2 invocations, got %d: %v", len(invocations), invocations)
	}
	if !strings.Contains(invocations[0], "plugin marketplace add") {
		t.Errorf("first invocation should be marketplace add, got: %s", invocations[0])
	}
	if !strings.Contains(invocations[1], "plugin install") {
		t.Errorf("second invocation should be plugin install, got: %s", invocations[1])
	}
	if !strings.Contains(invocations[1], "cospower@costrict-plugins") {
		t.Errorf("install should use cospower@costrict-plugins, got: %s", invocations[1])
	}
	if !strings.Contains(invocations[1], workDir) {
		t.Errorf("install should pass --dir workdir, got: %s", invocations[1])
	}
}

func TestSetupCSCPlugins_MarketplaceAddFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, map[string]int{
		"marketplace add": 1,
	})
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), fakeBin, workDir, slog.Default())
	if err == nil {
		t.Fatal("expected error when marketplace add fails")
	}
	if !strings.Contains(err.Error(), "marketplace add") {
		t.Errorf("error should mention marketplace add, got: %v", err)
	}
}

func TestSetupCSCPlugins_InstallFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, map[string]int{
		"plugin install": 1,
	})
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), fakeBin, workDir, slog.Default())
	if err == nil {
		t.Fatal("expected error when plugin install fails")
	}
	if !strings.Contains(err.Error(), "plugin install") {
		t.Errorf("error should mention plugin install, got: %v", err)
	}
}

func TestSetupCSCPlugins_CSCBinEmpty(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), "", workDir, slog.Default())
	if err != nil {
		t.Fatalf("empty cscBin should skip silently, got: %v", err)
	}
	// No invocations.log should exist
	if _, err := os.Stat(filepath.Join(dir, "invocations.log")); err == nil {
		t.Error("expected no invocations when cscBin is empty")
	}
}

func TestSetupCSCPlugins_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	// Write a fake that sleeps forever
	script := "#!/bin/sh\nsleep 300\n"
	fakeBin := filepath.Join(dir, "csc")
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := setupCSCPlugins(ctx, fakeBin, workDir, slog.Default())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	// Should contain context deadline or signal info
	if !strings.Contains(err.Error(), "marketplace add") && !strings.Contains(err.Error(), "context") {
		t.Errorf("error should mention marketplace add or context, got: %v", err)
	}
}

func TestSetupCSCPlugins_ErrorMessageContainsURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, map[string]int{
		"marketplace add": 1,
	})
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), fakeBin, workDir, slog.Default())
	if err == nil {
		t.Fatal("expected error")
	}
	// Error should contain the marketplace URL for debugging
	if !strings.Contains(err.Error(), cscMarketplaceURL) {
		t.Errorf("error should contain marketplace URL %q, got: %v", cscMarketplaceURL, err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server && go test ./internal/daemon/execenv/ -run TestSetupCSCPlugins -v`
Expected: 编译失败，`setupCSCPlugins` 和 `cscMarketplaceURL` 未定义

- [ ] **Step 3: 编写 `setupCSCPlugins()` 实现**

创建 `server/internal/daemon/execenv/csc_plugins.go`：

```go
package execenv

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const (
	// cscMarketplaceURL is the hardcoded CSC plugin marketplace repository URL.
	// Phase 1: hardcoded. Phase 2: configurable. Phase 3: server-driven.
	cscMarketplaceURL = "https://github.com/costrict-plugins-repo/marketplace.git"
	// cscPluginSource is the CSC plugin source identifier used in install specs.
	cscPluginSource = "costrict-plugins"
)

// cscDefaultPlugins is the Phase 1 hardcoded list of plugins to install.
var cscDefaultPlugins = []string{
	"cospower",
}

// setupCSCPlugins installs CSC plugins into the task's working directory.
// It runs CSC CLI commands sequentially:
//
//	1. csc plugin marketplace add <marketplaceURL>
//	2. csc plugin install <pluginName>@<source> --dir <workdir>
//
// Both commands must succeed. On failure, returns an error describing which
// step failed and why. The caller (Prepare) propagates this to runTask ->
// handleTask -> FailTask so the server records the failure.
//
// When cscBin is empty, the function returns nil immediately without
// executing any commands (the CSC binary is not available on this host).
func setupCSCPlugins(ctx context.Context, cscBin string, workDir string, logger *slog.Logger) error {
	if cscBin == "" {
		return nil
	}

	// Step 1: marketplace add
	addCtx, addCancel := context.WithTimeout(ctx, 60*time.Second)
	defer addCancel()

	addCmd := exec.CommandContext(addCtx, cscBin, "plugin", "marketplace", "add", cscMarketplaceURL)
	var addStderr strings.Builder
	addCmd.Stderr = &addStderr
	if err := addCmd.Run(); err != nil {
		stderrMsg := strings.TrimSpace(addStderr.String())
		if stderrMsg != "" {
			return fmt.Errorf("csc plugin marketplace add %s: %w (stderr: %s)", cscMarketplaceURL, err, stderrMsg)
		}
		return fmt.Errorf("csc plugin marketplace add %s: %w", cscMarketplaceURL, err)
	}
	logger.Info("execenv: csc plugin marketplace add ok", "url", cscMarketplaceURL)

	// Step 2: plugin install
	for _, name := range cscDefaultPlugins {
		installCtx, installCancel := context.WithTimeout(ctx, 120*time.Second)
		spec := fmt.Sprintf("%s@%s", name, cscPluginSource)
		installCmd := exec.CommandContext(installCtx, cscBin, "plugin", "install", spec, "--dir", workDir)
		var installStderr strings.Builder
		installCmd.Stderr = &installStderr
		err := installCmd.Run()
		installCancel()
		if err != nil {
			stderrMsg := strings.TrimSpace(installStderr.String())
			if stderrMsg != "" {
				return fmt.Errorf("csc plugin install %s: %w (stderr: %s)", spec, err, stderrMsg)
			}
			return fmt.Errorf("csc plugin install %s: %w", spec, err)
		}
		logger.Info("execenv: csc plugin install ok", "plugin", spec, "dir", workDir)
	}

	return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd server && go test ./internal/daemon/execenv/ -run TestSetupCSCPlugins -v`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add server/internal/daemon/execenv/csc_plugins.go server/internal/daemon/execenv/csc_plugins_test.go
git commit -m "feat(execenv): add setupCSCPlugins for CSC plugin installation"
```

---

### Task 2: 在 `PrepareParams` 中增加 `CSCBin` 字段并在 `Prepare()` 中调用 `setupCSCPlugins()`

**Files:**
- Modify: `server/internal/daemon/execenv/execenv.go` (PrepareParams struct + Prepare function)

- [ ] **Step 1: 编写失败测试**

在 `server/internal/daemon/execenv/execenv_test.go` 中增加测试，验证 `Prepare()` 在 `Provider == "csc"` 且 `CSCBin` 非空时调用 plugin setup。

```go
func TestPrepare_CSCPluginSetup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}

	dir := t.TempDir()
	// Create a fake csc binary that succeeds
	fakeBin := filepath.Join(dir, "fake-csc")
	script := "#!/bin/sh\necho $@ >> " + filepath.Join(dir, "invocations.log") + "\nexit 0\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	workspacesRoot := filepath.Join(dir, "wsroot")
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-1234",
		TaskID:         "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		AgentName:      "test-agent",
		Provider:       "csc",
		CSCBin:         fakeBin,
		Task:           TaskContextForEnv{},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	if env == nil {
		t.Fatal("expected non-nil Environment")
	}

	// Verify plugin commands were invoked
	data, err := os.ReadFile(filepath.Join(dir, "invocations.log"))
	if err != nil {
		t.Fatal("expected invocations.log to exist — setupCSCPlugins was not called")
	}
	log := string(data)
	if !strings.Contains(log, "plugin marketplace add") {
		t.Errorf("expected marketplace add invocation, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin install") {
		t.Errorf("expected plugin install invocation, got:\n%s", log)
	}
}

func TestPrepare_CSCPluginSetupFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}

	dir := t.TempDir()
	// Create a fake csc binary that fails
	fakeBin := filepath.Join(dir, "fake-csc")
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	workspacesRoot := filepath.Join(dir, "wsroot")
	_, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-1234",
		TaskID:         "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		AgentName:      "test-agent",
		Provider:       "csc",
		CSCBin:         fakeBin,
		Task:           TaskContextForEnv{},
	}, testLogger())
	if err == nil {
		t.Fatal("expected Prepare to fail when CSC plugin setup fails")
	}
	if !strings.Contains(err.Error(), "csc plugin setup") {
		t.Errorf("error should mention csc plugin setup, got: %v", err)
	}
}

func TestPrepare_CSCSkippedWhenBinEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}

	dir := t.TempDir()
	workspacesRoot := filepath.Join(dir, "wsroot")
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-1234",
		TaskID:         "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		AgentName:      "test-agent",
		Provider:       "csc",
		CSCBin:         "", // empty — should skip plugin setup
		Task:           TaskContextForEnv{},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare should succeed with empty CSCBin, got: %v", err)
	}
	if env == nil {
		t.Fatal("expected non-nil Environment")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server && go test ./internal/daemon/execenv/ -run "TestPrepare_CSC" -v`
Expected: 编译失败 — `PrepareParams` 没有 `CSCBin` 字段

- [ ] **Step 3: 修改 `PrepareParams` 增加 `CSCBin` 字段**

在 `server/internal/daemon/execenv/execenv.go` 的 `PrepareParams` struct 中增加字段。

在 `OpenclawBin` 字段后面（约第 40 行之后）添加：

```go
CSCBin        string            // resolved csc CLI path (only used when Provider == "csc"); empty = skip plugin setup
```

- [ ] **Step 4: 在 `Prepare()` 函数尾部增加 CSC 分支**

在 `execenv.go` 的 `Prepare()` 函数中，在 openclaw 分支之后（约第 191 行 `}` 之后、`logger.Info` 之前），添加：

```go
// For CSC, install plugins into the task working directory. The plugins
// provide the runtime with the tools it needs to execute the dispatched
// task. Fail closed: without plugins, the task cannot run meaningfully.
if params.Provider == "csc" && params.CSCBin != "" {
	if err := setupCSCPlugins(context.Background(), params.CSCBin, env.WorkDir, logger); err != nil {
		return nil, fmt.Errorf("csc plugin setup: %w", err)
	}
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd server && go test ./internal/daemon/execenv/ -run "TestPrepare_CSC" -v`
Expected: 全部 PASS

- [ ] **Step 6: 运行全部 execenv 测试确认无回归**

Run: `cd server && go test ./internal/daemon/execenv/ -v`
Expected: 全部 PASS（包括已有的测试）

- [ ] **Step 7: 提交**

```bash
git add server/internal/daemon/execenv/execenv.go server/internal/daemon/execenv/execenv_test.go
git commit -m "feat(execenv): wire setupCSCPlugins into Prepare for CSC provider"
```

---

### Task 3: 在 `daemon.runTask()` 中传入 `CSCBin`

**Files:**
- Modify: `server/internal/daemon/daemon.go` (runTask function, ~line 2283-2307)

- [ ] **Step 1: 修改 `runTask()` — 提取 `cscBin` 并传入 `PrepareParams`**

在 `server/internal/daemon/daemon.go` 中找到 `openclawBin` 的提取逻辑（约第 2283-2286 行）：

```go
openclawBin := ""
if provider == "openclaw" {
    openclawBin = entry.Path
}
```

在其后添加 `cscBin` 的提取：

```go
cscBin := ""
if provider == "csc" {
    cscBin = entry.Path
}
```

然后在 `execenv.Prepare()` 调用中（约第 2298-2307 行），在 `OpenclawBin` 字段之后添加 `CSCBin`：

```go
CSCBin:       cscBin,
```

最终 `execenv.Prepare()` 调用变为：

```go
env, err = execenv.Prepare(execenv.PrepareParams{
    WorkspacesRoot: d.cfg.WorkspacesRoot,
    WorkspaceID:    task.WorkspaceID,
    TaskID:         task.ID,
    AgentName:      agentName,
    Provider:       provider,
    CodexVersion:   codexVersion,
    OpenclawBin:    openclawBin,
    CSCBin:         cscBin,
    Task:           taskCtx,
}, d.logger)
```

注意：`ReuseParams` 中不需要传 `CSCBin`，因为复用路径跳过 Plugin 安装。

- [ ] **Step 2: 编译确认**

Run: `cd server && go build ./...`
Expected: 编译成功，无错误

- [ ] **Step 3: 运行 daemon 包测试确认无回归**

Run: `cd server && go test ./internal/daemon/ -v -count=1`
Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add server/internal/daemon/daemon.go
git commit -m "feat(daemon): pass CSCBin from agent config to execenv.Prepare"
```

---

### Task 4: 全量验证

- [ ] **Step 1: 运行 server 全量测试**

Run: `cd server && go test ./... -count=1`
Expected: 全部 PASS

- [ ] **Step 2: 确认最终 commit 历史**

Run: `git log --oneline -5`
Expected: 3 个新 commit，分别是：
1. `feat(execenv): add setupCSCPlugins for CSC plugin installation`
2. `feat(execenv): wire setupCSCPlugins into Prepare for CSC provider`
3. `feat(daemon): pass CSCBin from agent config to execenv.Prepare`

- [ ] **Step 3: 最终 diff 检查**

Run: `git diff HEAD~3 --stat`
Expected: 修改/新增的文件列表与设计文档一致
