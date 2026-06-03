# CSC Plugin 安装设计

**日期：** 2026-06-03
**分支：** `feat/csc-cloud-cli-dga-bridge`
**状态：** 草稿

## 概述

通过在 Multica Daemon 的任务执行流程中增加 Plugin 安装步骤，打通 CSC Cloud ↔ CSC CLI 的任务下发链路。当 CSC Agent 领取任务时，Daemon 在启动 Agent 进程之前，将所需的 CSC Plugin 安装到任务的隔离工作目录中。

本次为第一阶段：Plugin 名称和 Marketplace URL 硬编码。
后续阶段将改为可配置，最终由 Server 动态下发。

## 需求

1. 当 `provider == "csc"` 时，Daemon 必须在 Agent 进程启动前将 CSC Plugin 安装到任务的工作目录中。
2. 安装过程执行两条 CSC CLI 命令：
   - `csc plugin marketplace add <marketplaceURL>`
   - `csc plugin install <pluginName>@<source> --dir <workdir>`
3. 任一命令失败时，任务立即终止，错误通过现有 `FailTask` 机制上报到 Multica Server。
4. `Reuse()` 路径跳过 Plugin 安装（工作目录已由前一次任务填充）。
5. 改动限于 `execenv/` 包及 `daemon.go` 中的一处小对接。无需 Server 端或前端改动。

## 架构

### 执行流程（改动前）

```
execenv.Prepare()
  ├── 创建目录结构
  ├── writeContextFiles()        ← Skills、Issue 上下文
  ├── [codex]    setupCodexHome()
  └── [openclaw] setupOpenclawConfig()
```

### 执行流程（改动后）

```
execenv.Prepare()
  ├── 创建目录结构
  ├── writeContextFiles()        ← Skills、Issue 上下文
  ├── [codex]    setupCodexHome()
  ├── [openclaw] setupOpenclawConfig()
  └── [csc]      setupCSCPlugins()   ← 新增
                     ├── exec("csc plugin marketplace add <url>")
                     └── exec("csc plugin install <name>@costrict-plugins --dir <workdir>")
```

### 错误传播链路

```
setupCSCPlugins() 返回 error
  → execenv.Prepare() 返回 error
    → daemon.runTask() 返回 (TaskResult{}, error)
      → daemon.handleTask() 调用 FailTask(taskID, "csc plugin setup: ...", "", "", "agent_error")
        → Server 记录失败，UI 展示错误给用户
```

## 改动文件清单

| 文件 | 类型 | 说明 |
|---|---|---|
| `server/internal/daemon/execenv/execenv.go` | 修改 | 在 `Prepare()` 中增加 CSC 分支，`PrepareParams` 增加 `CSCBin` 字段 |
| `server/internal/daemon/execenv/csc_plugins.go` | **新增** | `setupCSCPlugins()` 实现 |
| `server/internal/daemon/execenv/csc_plugins_test.go` | **新增** | 单元测试 |
| `server/internal/daemon/daemon.go` | 修改 | 从配置取 `CSCBin` 传入 `PrepareParams` |

**不修改：** `agent/csc.go`、`agent/agent.go`、Server handler、前端。

## 详细设计

### `execenv/csc_plugins.go`

```go
package execenv

import (
    "context"
    "fmt"
    "log/slog"
    "os/exec"
    "time"
)

const (
    cscMarketplaceURL = "https://github.com/costrict-plugins-repo/marketplace.git"
    cscPluginSource   = "costrict-plugins"
)

// Phase 1: fixed plugin list.
var cscDefaultPlugins = []string{
    "cospower",
}

// setupCSCPlugins installs CSC plugins into the task's working directory.
// It runs two CSC CLI commands sequentially:
//   1. csc plugin marketplace add <marketplaceURL>
//   2. csc plugin install <pluginName>@<source> --dir <workdir>
//
// Both commands must succeed. On failure, returns an error describing which
// step failed and why. The caller (Prepare) propagates this to runTask →
// handleTask → FailTask so the server records the failure.
func setupCSCPlugins(ctx context.Context, cscBin string, workDir string, logger *slog.Logger) error {
    // Step 1: marketplace add
    addCtx, addCancel := context.WithTimeout(ctx, 60*time.Second)
    defer addCancel()

    addCmd := exec.CommandContext(addCtx, cscBin, "plugin", "marketplace", "add", cscMarketplaceURL)
    // ... hideAgentWindow, capture stderr
    if err := addCmd.Run(); err != nil {
        return fmt.Errorf("csc plugin marketplace add %s: %w", cscMarketplaceURL, err)
    }
    logger.Info("execenv: csc plugin marketplace add ok", "url", cscMarketplaceURL)

    // Step 2: plugin install
    for _, name := range cscDefaultPlugins {
        installCtx, installCancel := context.WithTimeout(ctx, 120*time.Second)
        spec := fmt.Sprintf("%s@%s", name, cscPluginSource)
        installCmd := exec.CommandContext(installCtx, cscBin, "plugin", "install", spec, "--dir", workDir)
        // ... hideAgentWindow, capture stderr
        err := installCmd.Run()
        installCancel()
        if err != nil {
            return fmt.Errorf("csc plugin install %s: %w", spec, err)
        }
        logger.Info("execenv: csc plugin install ok", "plugin", spec)
    }

    return nil
}
```

### `execenv/execenv.go` — Prepare() 改动

`PrepareParams` 新增 `CSCBin` 字段：

```go
type PrepareParams struct {
    // ... 现有字段 ...
    OpenclawBin string  // resolved openclaw CLI path
    CSCBin      string  // resolved csc CLI path (empty = skip plugin setup)
    // ...
}
```

在 `Prepare()` 尾部增加 CSC 分支，与现有 provider 分支平级：

```go
if provider == "csc" && params.CSCBin != "" {
    if err := setupCSCPlugins(ctx, params.CSCBin, env.WorkDir, logger); err != nil {
        return nil, fmt.Errorf("csc plugin setup: %w", err)
    }
}
```

### `daemon/daemon.go` — runTask() 改动

提取 CSC 二进制路径（与 `openclawBin` 同模式）：

```go
cscBin := ""
if provider == "csc" {
    cscBin = entry.Path
}
```

传入 `PrepareParams`：

```go
env, err = execenv.Prepare(execenv.PrepareParams{
    // ... 现有字段 ...
    OpenclawBin:  openclawBin,
    CSCBin:       cscBin,        // 新增
    // ...
}, d.logger)
```

`Reuse()` 路径无需传递 `CSCBin`——复用时跳过 Plugin 安装，目录已经填充完毕：

```go
env = execenv.Reuse(execenv.ReuseParams{
    // ... 现有字段 ...
    // 无需 CSCBin — 复用路径跳过 Plugin 安装
}, d.logger)
```

## 错误处理

| 场景 | 处理方式 | 原因 |
|---|---|---|
| `cscBin == ""` | 跳过整个 setup | CSC 二进制不可用，无需尝试 |
| marketplace add 失败 | 返回 error → FailTask → Server | 无 marketplace，install 无法执行 |
| plugin install 失败 | 返回 error → FailTask → Server | 无 plugin，任务执行无意义 |
| 命令超时（60s/120s） | context cancel → 返回 error → FailTask | 无响应的 CLI 不应无限阻塞 |

上报到 Server 的错误消息包含失败步骤和 stderr 输出，例如：
- `csc plugin marketplace add https://github.com/.../marketplace.git: exit status 1 (stderr: ...)`
- `csc plugin install cospower@costrict-plugins: timeout after 120s`

## 测试

### 单元测试（`csc_plugins_test.go`）

| 测试用例 | 验证内容 |
|---|---|
| `TestSetupCSCPlugins_Success` | 两条命令均执行且参数正确，返回 nil |
| `TestSetupCSCPlugins_MarketplaceAddFails` | 返回包含 "marketplace add" 的 error |
| `TestSetupCSCPlugins_InstallFails` | 返回包含 "plugin install" 的 error |
| `TestSetupCSCPlugins_CSCBinEmpty` | 直接返回，不执行任何命令 |
| `TestSetupCSCPlugins_Timeout` | context 取消后返回 error |

测试使用伪造可执行文件（按需成功/失败的脚本），通过 `ExecutablePath` 传入，遵循 `daemon_test.go` 中的现有模式。

### 集成测试（手动）

1. 确保 CSC CLI 在 PATH 上且 `csc plugin` 子命令可用。
2. 启动 Daemon 并配置 CSC Agent。
3. 从 Multica UI 触发任务分配。
4. 验证：
   - Plugin 已安装到任务工作目录。
   - 任务通过 CSC CLI 执行且 Plugin 可用。
   - 强制失败时（如错误的 marketplace URL），UI 展示失败状态。

## 后续演进

```
第一阶段（本次 PR）    第二阶段                第三阶段
──────────────────     ──────────────────     ──────────────────
硬编码常量             配置文件驱动            Server 动态下发
┌────────────────┐    ┌────────────────┐     ┌────────────────┐
│ marketplaceURL │    │ marketplaceURL │     │ Task.PluginReqs
│ pluginName    │ ──→│ plugins []     │ ──→ │   {name, source
│ (硬编码)       │    │ (配置文件)     │     │    version}    │
└────────────────┘    └────────────────┘     │ (Server 下发)  │
                                             └────────────────┘
涉及文件:              涉及文件:               涉及文件:
csc_plugins.go        config.go              handler/daemon.go
无外部改动            + PrepareParams         + Task 类型
                                              + execenv 读取
```

函数签名自然演进——硬编码常量变为参数，无需结构重构：

```go
// 第一阶段
setupCSCPlugins(ctx, cscBin, workDir, logger)

// 第二阶段（从配置文件读取参数）
setupCSCPlugins(ctx, cscBin, workDir, marketplaceURL, plugins, logger)

// 第三阶段（参数来自 Task，通过 PrepareParams 传入）
setupCSCPlugins(ctx, cscBin, workDir, params.PluginReqs, logger)
```
