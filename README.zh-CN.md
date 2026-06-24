<p align="center">
  <img src="docs/assets/banner.jpg" alt="Multica — 人类与 AI，并肩前行" width="100%">
</p>

<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="docs/assets/logo-light.svg">
  <img alt="Multica" src="docs/assets/logo-light.svg" width="50">
</picture>

# Multica

**你的下一批员工，不是人类。**

开源的 Managed Agents 平台。<br/>
将编码 Agent 变成真正的队友——分配任务、跟踪进度、积累技能。

[![CI](https://github.com/multica-ai/multica/actions/workflows/ci.yml/badge.svg)](https://github.com/multica-ai/multica/actions/workflows/ci.yml)
[![GitHub stars](https://img.shields.io/github/stars/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/stargazers)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.gg/W8gYBn226t)

[官网](https://multica.ai) · [云服务](https://multica.ai) · [Discord](https://discord.gg/W8gYBn226t) · [X](https://x.com/MulticaAI) · [自部署指南](SELF_HOSTING.md) · [参与贡献](CONTRIBUTING.md)

**[English](README.md) | 简体中文**

</div>

## Multica 是什么？

Multica 将编码 Agent 变成真正的队友。像分配给同事一样分配给 Agent——它们会自主接手工作、编写代码、报告阻塞问题、更新状态。

不再需要复制粘贴 prompt，不再需要盯着运行过程。你的 Agent 出现在看板上、参与对话、随着时间积累可复用的技能。可以理解为开源的 Managed Agents 基础设施——厂商中立、可自部署、专为人类 + AI 团队设计。支持 **Claude Code**、**Codex**、**GitHub Copilot CLI**、**OpenClaw**、**OpenCode**、**Hermes**、**Gemini**、**Pi**、**Cursor Agent**、**Kimi**、**Kiro CLI** 与 **Qoder CLI**。

面向更大的团队，Squads（小队）提供稳定的路由层：把任务分给由 Agent 带队的小队，由队长判断谁最适合接手。

<p align="center">
  <img src="docs/assets/hero-screenshot.png" alt="Multica 看板视图" width="800">
</p>

## 为什么叫 "Multica"？

Multica——**Mul**tiplexed **I**nformation and **C**omputing **A**gent。

这个名字是在向 20 世纪 60 年代具有开创意义的操作系统 Multics 致意。Multics 首创了分时系统，让多个用户能够共享同一台机器，同时又像各自独占它一样使用。Unix 则是在有意简化 Multics 的基础上诞生的，强调一个用户、一个任务、一种优雅的哲学。

我们认为，类似的转折点正在再次出现。几十年来，软件团队一直处于一种单线程的工作模式，一个工程师处理一个任务，一次只专注于一个上下文。AI agents 改变了这个等式。Multica 将"分时"重新带回这个时代，只不过今天在系统中进行多路复用的"用户"，既包括人类，也包括自主代理。

在 Multica 中，agents 是一级团队成员。它们会被分配 issue，汇报进展，提出阻塞，并交付代码，就像人类同事一样。任务分配、活动时间线、任务生命周期，以及运行时基础设施，Multica 从第一天起就是围绕这一理念构建的。

和当年的 Multics 一样，这一判断建立在"多路复用"之上。一个小团队不该因为人数少就显得能力有限。有了合适的系统，两名工程师加上一组 agents，就能发挥出二十人团队的推进速度。

## 功能特性

Multica 管理完整的 Agent 生命周期：从任务分配到执行监控再到技能复用。

- **Agent 即队友** — 像分配给同事一样分配给 Agent。它们有个人档案、出现在看板上、发表评论、创建 Issue、主动报告阻塞问题。
- **Squads（小队）** — 把多个 Agent（以及人类成员）组合成由 leader agent 带队的小队，直接把任务分配给小队本身。Leader 会判断谁最适合接手，团队扩容时路由方式保持不变。用 `@前端组` 代替 `@小张或小李或小王`。
- **自主执行** — 设置后无需管理。完整的任务生命周期管理（排队、认领、执行、完成/失败），通过 WebSocket 实时推送进度。
- **自动化（Autopilots）** — 为 Agent 安排周期性工作。定时（Cron）、Webhook 或手动触发，自动化会自动创建 Issue 并分配给 Agent——日报、周报、定期巡检都能让它自己跑起来。
- **可复用技能** — 每个解决方案都成为全团队可复用的技能。部署、数据库迁移、代码审查——技能让团队能力随时间持续增长。
- **确定性工具（Deterministic Tools）** — 当"测试到底有没有通过？"必须被*验证*而不是被猜测时，编写一个带类型的 Go 步骤，让 Agent 通过 MCP 调用它。直接在工作区里编写和测试，在沙箱中运行，返回可审计的结果。详见 [确定性工具](#确定性工具)。
- **统一运行时** — 一个控制台管理所有算力。本地 daemon 和云端运行时，自动检测可用 CLI，实时监控。
- **多工作区** — 按团队组织工作，工作区级别隔离。每个工作区有独立的 Agent、Issue 和设置。

---

## 快速安装

### macOS / Linux（推荐 Homebrew）

```bash
brew install multica-ai/tap/multica
```

后续可用 `brew upgrade multica-ai/tap/multica` 更新 CLI。

### macOS / Linux（安装脚本）

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash
```

如果没有 Homebrew，可以使用安装脚本。脚本会安装 Multica CLI：检测到 `brew` 时通过 Homebrew 安装，否则直接下载二进制。

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex
```

安装完成后，一条命令完成配置、认证和启动：

```bash
multica setup          # 连接 Multica Cloud，登录，启动 daemon
```

> **自部署？** 加上 `--with-server` 在本地部署完整的 Multica 服务：
>
> ```bash
> curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --with-server
> multica setup self-host
> ```
>
> 需要 Docker。详见 [自部署指南](SELF_HOSTING.md)。

---

## 快速上手

安装好 CLI（或注册 [Multica 云服务](https://multica.ai)）后，按以下步骤将第一个任务分配给 Agent：

### 1. 配置并启动 daemon

```bash
multica setup           # 配置、认证、启动 daemon（一条命令搞定）
```

daemon 在后台运行，保持你的机器与 Multica 的连接。它会自动检测 PATH 中可用的 Agent CLI（`claude`、`codex`、`copilot`、`openclaw`、`opencode`、`hermes`、`gemini`、`pi`、`cursor-agent`、`kimi`、`kiro-cli`、`qodercli`）。

### 2. 确认运行时已连接

在 Multica Web 端打开你的工作区，进入 **设置 → 运行时（Runtimes）**，你应该能看到你的机器已作为一个活跃的 **Runtime** 出现在列表中。

> **什么是 Runtime（运行时）？** Runtime 是可以执行 Agent 任务的计算环境。它可以是你的本地机器（通过 daemon 连接），也可以是云端实例。每个 Runtime 会上报可用的 Agent CLI，Multica 据此决定将任务路由到哪里执行。

### 3. 创建 Agent

进入 **设置 → Agents**，点击 **新建 Agent**。选择你刚连接的 Runtime，选择 Provider（Claude Code、Codex、GitHub Copilot CLI、OpenClaw、OpenCode、Hermes、Gemini、Pi、Cursor Agent、Kimi、Kiro CLI 或 Qoder CLI），并为 Agent 起个名字——它将以这个名字出现在看板、评论和任务分配中。

### 4. 分配你的第一个任务

在看板上创建一个 Issue（或通过 `multica issue create` 命令创建），然后将其分配给你的新 Agent。Agent 会自动接手任务、在你的 Runtime 上执行、并实时汇报进度——就像一个真正的队友一样。

大功告成！你的 Agent 现在是团队的一员了。 🎉

---

## 确定性工具

技能是*建议性*的——它是 Agent 读取的 Markdown，可以遵循、转述，也可以忽略。对于需要判断力的场景（"如何组织一个 PR"、命名规范），这种形态是对的；但对任何与正确性相关的事情，它就是错的：一条写着"确保测试通过"的技能，只是一个模型可以凭空绕过的建议。

**确定性工具**填补了这个缺口。工具是会*真正执行*的带类型 Go 代码——它检查仓库、强制执行策略、或运行某个门禁，并返回 Agent 可据以分支判断的、可验证的结果。Agent 通过 [MCP](https://modelcontextprotocol.io) 调用工具；一套内置目录（`repo_facts`、`policy_check`、`build_probe`、`test_gate`、`diff_summarize`、`artifact_emit`）已编译进 daemon 二进制文件，你也可以在工作区里编写自己的工具。

| | 技能（建议性） | 确定性工具 |
|---|---|---|
| 是什么 | Agent 上下文中的 Markdown | 会执行的带类型 Go 代码 |
| 答错时 | 模型据以行动的一个建议 | 被测试捕获的一个 Bug |
| 适用于 | 框架、规范、判断 | 仓库事实、门禁、"通过了吗？" |

### 编写工具

打开工作区，在侧边栏进入 **Tools（工具）**。编写一个确定性 Go *步骤*，给出示例输入，点击 **Test（测试）** 即可在沙箱中立即运行——无需部署，无需重新构建。

一个步骤是一个名为 `step` 的 Go 包，暴露一个函数：

```go
package step

import "strings"

// Run 接收解码后的 JSON 输入，返回一个 Result 信封。
func Run(input map[string]any) map[string]any {
	name, _ := input["name"].(string)
	if name == "" {
		return map[string]any{
			"status":     "error",
			"error_code": "INVALID_INPUT",
			"summary":    "input.name is required",
		}
	}
	return map[string]any{
		"status":  "ok",
		"summary": "Greeted " + name,
		"machine_data": map[string]any{
			"greeting": "Hello, " + strings.ToUpper(name),
			"length":   len(name),
		},
	}
}
```

用输入 `{ "name": "world" }` 测试，会返回标准的 **Result 信封**——这与内置工具和 Agent 使用的是同一份契约：

```json
{
  "status": "ok",
  "summary": "Greeted world",
  "machine_data": { "greeting": "Hello, WORLD", "length": 5 },
  "retryable": false
}
```

`status` 取值为 `"ok"` 或 `"error"`；失败时设置一个稳定的 `error_code`（`INVALID_INPUT`、`MISSING_DEPENDENCY`、`POLICY_FAILURE`、`TIMEOUT`、`INTERNAL_ERROR`）。一个只返回数据、不带 `status` 的步骤会被视为成功。

### 是门禁，不是猜测

确定性工具的意义在于*强制执行*，而非建议。一个策略门禁会返回 Agent 无法绕过的硬性失败：

```go
package step

import "strings"

// 如果改动落在了非 feature 分支上，就让任务失败。
func Run(input map[string]any) map[string]any {
	branch, _ := input["branch"].(string)
	if !strings.HasPrefix(branch, "feature/") {
		return map[string]any{
			"status":     "error",
			"error_code": "POLICY_FAILURE",
			"summary":    "branch " + branch + " must start with feature/",
			"machine_data": map[string]any{"branch": branch},
		}
	}
	return map[string]any{"status": "ok", "summary": "branch policy ok"}
}
```

### 沙箱

步骤运行在内嵌的 Go 解释器中，而非编译后的二进制文件，因此可以在运行时编写和修改，无需重新部署。解释器采用**白名单制**：步骤只能导入纯粹、确定性的标准库包（`fmt`、`strings`、`strconv`、`regexp`、`encoding/json`、`time`、`slices`、`math` 等），其余一概不可。`os`、`os/exec`、`io`、`net/*`、`syscall` 均不可导入——步骤可以对输入做计算，但无法触及主机、文件系统或网络。

每次运行还会在**独立的隔离进程**中进行（二进制文件以一次性沙箱模式重新执行自身），而非在进程内运行：子进程只拥有不含服务端任何密钥的最小环境，并带有内核 CPU 时间上限；步骤一旦超时即被硬性终止（`SIGKILL`），panic 则以 `INTERNAL_ERROR` 形式返回——绝不会导致崩溃，也不会在长期运行的进程中泄漏 goroutine。

### 启用面向 Agent 的工具平面

确定性工具平面默认关闭。在 daemon 上启用它，Agent 即可通过 MCP 收到这些工具：

```bash
export MULTICA_DETTOOLS_ENABLED=true                                   # 总开关
export MULTICA_DETTOOLS_ALLOWED=repo_facts,policy_check,build_probe,test_gate  # 白名单（默认为完整的只读目录）
export MULTICA_DETTOOLS_TIMEOUT=90s                                    # 单个工具的超时
```

启用后，工作区**已保存**的工具会随每个任务一起下发给 Agent：认领任务时，daemon 把启用的工具写入任务工作目录，由每个任务的 MCP 服务在沙箱中运行，Agent 即可像调用其他工具一样按名调用它们。还可通过 Agent 的 `runtime_config`（`deterministic_tools.allowed_tools` / `denied_tools`）做按 Agent 收窄——Agent 只能收窄 daemon 的白名单，永远无法扩大它。

---

## 架构

```
┌──────────────┐     ┌──────────────┐     ┌──────────────────┐
│   Next.js    │────>│  Go 后端     │────>│   PostgreSQL     │
│   前端       │<────│  (Chi + WS)  │<────│   (pgvector)     │
└──────────────┘     └──────┬───────┘     └──────────────────┘
                            │
                     ┌──────┴───────┐
                     │ Agent Daemon │  运行在你的机器上
                     └──────────────┘  （Claude Code、Codex、GitHub Copilot CLI、
                                        OpenCode、OpenClaw、Hermes、Gemini、
                                        Pi、Cursor Agent、Kimi、Kiro CLI、Qoder CLI）
```

| 层级 | 技术栈 |
|------|--------|
| 前端 | Next.js 16 (App Router) |
| 后端 | Go (Chi router, sqlc, gorilla/websocket) |
| 数据库 | PostgreSQL 17 with pgvector |
| Agent 运行时 | 本地 daemon 执行 Claude Code、Codex、GitHub Copilot CLI、OpenClaw、OpenCode、Hermes、Gemini、Pi、Cursor Agent、Kimi、Kiro CLI 或 Qoder CLI |

## 开发

参与 Multica 代码贡献，请参阅 [贡献指南](CONTRIBUTING.md)。

**环境要求：** [Node.js](https://nodejs.org/) v20+, [pnpm](https://pnpm.io/) v10.28+, [Go](https://go.dev/) v1.26+, [Docker](https://www.docker.com/)

```bash
pnpm install
cp .env.example .env
make setup
make start
```

完整的开发流程、worktree 支持、测试和问题排查请参阅 [CONTRIBUTING.md](CONTRIBUTING.md)。

iOS 移动端代码位于 [`apps/mobile/`](apps/mobile/)，自己编译装到手机的方法见 [README](apps/mobile/README.md)。

## 开源协议

[Modified Apache 2.0 (with commercial restrictions)](LICENSE)
