# Agent Plugin 数据结构设计

**日期：** 2026-06-04
**分支：** `feat/csc-cloud-cli-dga-bridge`
**状态：** 已批准

## 概述

将 daemon 内部的 plugin 数据结构从扁平的 `PluginSource` 替换为嵌套的 `AgentPlugin`，与 Agent 模型上的新 plugin 字段对齐。一个 agent 绑定一个 plugin（单数），不再使用硬编码默认值。

**范围：** 仅 daemon 侧。不涉及 Server handler、DB migration、前端 TypeScript 类型。

## 新数据结构

```go
// execenv/plugins.go

// AgentPlugin describes the plugin bound to an agent.
type AgentPlugin struct {
    ID      string         `json:"id"`
    Name    string         `json:"name"`
    Install *PluginInstall `json:"install,omitempty"`
}

// PluginInstall describes how to install a plugin from a marketplace.
type PluginInstall struct {
    Method              string `json:"method"`               // e.g. "plugin_marketplace"
    Marketplace         string `json:"marketplace"`          // e.g. "anthropics/claude-plugins-official"
    PluginName          string `json:"plugin_name"`          // e.g. "superpowers"
    MarketplaceName     string `json:"marketplace_name"`     // e.g. "claude-plugins-official"
    MarketplaceRepo     string `json:"marketplace_repo"`     // e.g. "anthropics/claude-plugins-official"
    MarketplaceVerified bool   `json:"marketplace_verified"` // e.g. true
}
```

关键决策：
- 单数 `Plugin *AgentPlugin`，不是 `Plugins []`——一个 agent 只绑定一个 plugin
- `Install` 是 pointer，`nil` 表示没有安装方法
- `method` 字段为将来扩展预留（目前只有 `"plugin_marketplace"`）

## 删除的结构

```go
// 删除以下旧结构
type PluginSource struct {
    MarketplaceURL  string `json:"marketplace_url"`
    MarketplaceName string `json:"marketplace_name"`
    Plugin          string `json:"plugin"`
}
```

## 数据流

```
AgentData.Plugin *execenv.AgentPlugin  (daemon/types.go — 新增)
    ↓ daemon.go 赋值
Task.Plugin *execenv.AgentPlugin  (daemon/types.go — 替换旧 Plugins []PluginSource)
    ↓ daemon.go 赋值
TaskContextForEnv.Plugin *AgentPlugin  (execenv/execenv.go — 替换旧 Plugins []PluginSource)
    ↓ Prepare() 传递
setupPlugins(provider, bin, workDir, plugin, logger)  (签名改为 *AgentPlugin)
    ↓ 路由
setupCSCPlugins(ctx, bin, workDir, plugin, logger)  (签名改为 *AgentPlugin)
```

## setupCSCPlugins 新解析逻辑

从 `plugin.Install` 提取安装参数：
- `MarketplaceRepo` → marketplace URL（`csc plugin marketplace add`）
- `MarketplaceName` → marketplace 名称（`csc plugin marketplace update`）
- `PluginName` → 插件名称（`csc plugin install/update`）

当 `plugin == nil` 或 `plugin.Install == nil` 时，跳过整个安装。
移除现有硬编码默认值（`cospowers-requirements` / `costrict-plugins`）。

### 命令流程（不变）

对每个 plugin 执行：
1. `csc plugin marketplace add <MarketplaceRepo>`（non-fatal）
2. `csc plugin marketplace update <MarketplaceName>`
3. `csc plugin install <PluginName>@<MarketplaceName> -s local`
4. `csc plugin update <PluginName>@<MarketplaceName> -s local`

## 变更文件

| 文件 | 变更 |
|---|---|
| `execenv/plugins.go` | 删除 `PluginSource`，新增 `AgentPlugin` + `PluginInstall`。`setupPlugins` / `setupCSCPlugins` 签名改为接收 `*AgentPlugin`。移除硬编码默认值。 |
| `execenv/execenv.go` | `TaskContextForEnv.Plugins []PluginSource` → `Plugin *AgentPlugin` |
| `daemon/types.go` | `AgentData` 新增 `Plugin *execenv.AgentPlugin`。`Task.Plugins []execenv.PluginSource` → `Plugin *execenv.AgentPlugin` |
| `daemon/daemon.go` | `taskCtx.Plugins = task.Plugins` → `taskCtx.Plugin = task.Plugin` |
| `execenv/plugins_test.go` | 所有 `PluginSource{...}` 改为 `&AgentPlugin{Install: &PluginInstall{...}}`。移除 "empty plugins uses defaults" 测试。 |
| `execenv/execenv_test.go` | `TaskContextForEnv` 构造处更新字段名。 |

## 不变的部分

- Server handler / DB / API — 不做改动
- `ReuseParams` / `Reuse` 路径 — 复用已有 workdir，不重复安装 plugin
- 其他 provider（Claude、Codex、OpenClaw）— 不变
- 前端 TypeScript 类型 — 不变

## 验证

```bash
cd server && go build ./cmd/multica
cd server && go test ./internal/daemon/execenv/ -run "TestSetupCSCPlugins|TestSetupPlugins|TestPrepare_CSCPlugin" -v
cd server && go test ./internal/daemon/ -v -count=1
```
