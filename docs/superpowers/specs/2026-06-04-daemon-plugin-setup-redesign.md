# Daemon Plugin Setup 重构设计

## 背景

原始的 CSC plugin 安装逻辑（`csc_plugins.go`）存在以下问题：

1. **配置硬编码** — marketplace URL 和 plugin 名称是常量
2. **CSC CLI 用法错误** — 使用了不存在的 `--dir` 参数，应该用 `cmd.Dir` + `-s project`
3. **缺少 update 步骤** — CSC 要求在 `plugin install` 之前先执行 `plugin update`
4. **PluginSource 重复定义** — 在 `daemon/types.go` 和 `execenv/csc_plugins.go` 各定义了一份，中间用 convert 函数转换（循环依赖的变通方案）
5. **命名绑定 CSC** — 文件名和函数名都是 CSC 专用的，不利于复用

本次重构将 plugin 机制通用化（CSC 作为第一个实现），并修正 CSC CLI 的命令流程。

## 设计

### PluginSource — 扁平化一对一结构

每个 plugin 条目自包含，携带自己的 marketplace URL：

```go
// execenv/plugins.go
type PluginSource struct {
    MarketplaceURL string `json:"marketplace_url"`
    Plugin         string `json:"plugin"`
}
```

Task 字段：`Plugins []execenv.PluginSource`

Server 下发的 JSON 示例：
```json
{
  "plugins": [
    {"marketplace_url": "https://github.com/foo/marketplace.git", "plugin": "cospower"}
  ]
}
```

### 单一定义位置

`PluginSource` 只在 `execenv` 中定义。`daemon/types.go` 通过 `[]execenv.PluginSource` 引用。

这样不需要 convert 函数就能消除循环依赖——`daemon` 已经在 import `execenv` 了。

### 通用 plugin 分发器

按 provider 路由到对应的实现：

```go
// execenv/plugins.go
func setupPlugins(ctx context.Context, provider, bin, workDir string, plugins []PluginSource, logger *slog.Logger) error {
    if len(plugins) == 0 || bin == "" {
        return nil
    }
    switch provider {
    case "csc":
        return setupCSCPlugins(ctx, bin, workDir, plugins, logger)
    default:
        return nil
    }
}
```

### CSC 实现流程

对每个 `PluginSource` 执行：

1. `csc plugin marketplace add <marketplaceURL>`（cmd.Dir = workDir）
2. `csc plugin update <plugin>`（cmd.Dir = workDir）
3. `csc plugin install <plugin> -s project`（cmd.Dir = workDir）

不使用 `--dir` 参数。所有命令通过 `cmd.Dir = workDir` 设置工作目录。install 使用 `-s project` scope。

### 调用链

```
daemon.runTask
  → taskCtx.Plugins = task.Plugins  （直接赋值，类型一致）
  → execenv.Prepare(params)
    → setupPlugins(provider, cscBin, workDir, params.Task.Plugins, logger)
      → setupCSCPlugins(...)
```

### 变更文件

| 文件 | 变更 |
|---|---|
| `execenv/csc_plugins.go` → 改名为 `plugins.go` | 定义 `PluginSource`，新增 `setupPlugins` 分发器，重构 `setupCSCPlugins` 使用新的 `PluginSource` |
| `daemon/types.go` | 删除本地的 `PluginSource` 结构体，`Task.Plugins` 改为 `[]execenv.PluginSource` |
| `execenv/execenv.go` | 调用 `setupPlugins` 替代 `setupCSCPlugins`，从 `params.Task.Plugins` 读取数据 |
| `daemon/daemon.go` | 删除 `convertPluginsForEnv`，直接将 `task.Plugins` 赋值给 `taskCtx.Plugins` |
| `execenv/csc_plugins_test.go` → 改名为 `plugins_test.go` | 更新所有测试适配新结构 |
| `execenv/execenv_test.go` | 更新 CSC 集成测试 |

### 不变的部分

- Server handler / DB / API — Phase 2 再做
- `ReuseParams` / `Reuse` 路径 — 复用已有 workdir，不重复安装 plugin
- 其他 provider（Claude、Codex、OpenClaw）— 不变

## 验证

```bash
cd server && go build ./cmd/multica
cd server && go test ./internal/daemon/execenv/ -run "TestSetupCSCPlugins|TestSetupPlugins|TestPrepare_CSCPlugin" -v
```
