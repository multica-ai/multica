# Multica

**Multiplexed Information and Computing Agent**

一个原生桌面客户端，通过可视化界面将编程智能体的能力带给每一个人。

[English](./README.md) | 简体中文 | [繁體中文](./README.zh-TW.md) | [日本語](./README.ja.md) | [한국어](./README.ko.md)

## 为什么叫 "Multica"？

这个名字的灵感来自于 [Multics](https://en.wikipedia.org/wiki/Multics)（Multiplexed Information and Computing Service，多路复用信息与计算服务），这是一个创建于 1964 年的开创性操作系统。尽管 Multics 最终没有广泛普及，但它奠定了现代操作系统的基础，包括层级文件系统等概念。Unix 本身就是从 Multics 衍生而来的（Uniplexed Information and Computing Service -> Unics -> Unix）。

**隐喻：** 正如 Multics 当年是为了解决多用户分时共享计算资源的问题，Multica 旨在解决多模型/多智能体协作的问题，服务于知识工作者。

## 解决的问题

编程智能体（如 Claude Code、Codex、Gemini CLI）在 2025 年变得极其强大，其能力已经远远超出了单纯的代码编写。然而，95% 的知识工作者因为三个核心障碍而无法使用这些能力：

**1. 交互形态的错配**
- 基于命令行的工具需要理解终端概念、文件路径和环境变量
- 现有工具聚焦于代码输出（差异对比、提交、代码检查），而非业务成果
- 知识工作者关心的是结果（图表、报告、分析），而不是生成这些结果的脚本

**2. 本地环境的挑战**
- 基于网页的智能体无法访问本地文件、文件夹或原生应用
- 设置 Python、Node.js 或其他依赖是一个巨大的障碍
- 缺少一个"开箱即用"、处理好所有依赖的沙盒环境

**3. 隐私与信任**
- 敏感的业务数据（财务分析、法律文件、医疗记录）不能上传到第三方服务器
- 需要一种数据留在本地、智能来自云端的模式

Multica 通过提供可视化的原生桌面界面来弥合这一鸿沟，在保持数据本地化的同时，充分利用编程智能体的能力。

## 特性

- 原生 macOS 应用，界面简洁直观
- 通过 [Agent Client Protocol (ACP)](https://github.com/anthropics/agent-client-protocol) 支持多种 AI 智能体
- 本地优先：数据永远不会离开你的设备
- 会话管理，支持历史记录和恢复功能
- 内置 CLI，适合高级用户和测试使用

## 支持的智能体

| 智能体 | 命令 | 安装方式 |
|-------|---------|---------|
| [OpenCode](https://github.com/opencode-ai/opencode) | `opencode acp` | `go install github.com/opencode-ai/opencode@latest` |
| [Codex CLI (ACP)](https://github.com/zed-industries/codex-acp) | `codex-acp` | `npm install -g codex-acp` |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | `gemini acp` | `npm install -g @google/gemini-cli` |

## 快速开始

```bash
# 安装依赖
pnpm install

# 检查已安装的智能体
pnpm cli doctor

# 启动桌面应用
pnpm dev
```

## 命令行工具

Multica 包含一个完整的 CLI，用于测试和与智能体交互：

```bash
pnpm cli                          # 交互模式
pnpm cli prompt "消息"             # 单次提问
pnpm cli sessions                 # 列出会话
pnpm cli resume <id>              # 恢复会话
pnpm cli agents                   # 列出可用智能体
pnpm cli doctor                   # 检查智能体安装状态
```

### 交互模式

启动交互式 REPL 会话：

```bash
pnpm cli
```

可用命令：

| 命令 | 描述 |
|---------|-------------|
| `/help` | 显示帮助 |
| `/new [cwd]` | 创建新会话（默认：当前目录） |
| `/sessions` | 列出所有会话 |
| `/resume <id>` | 通过 ID 前缀恢复会话 |
| `/delete <id>` | 删除会话 |
| `/history` | 显示当前会话的消息历史 |
| `/agent <name>` | 切换到其他智能体 |
| `/agents` | 列出可用智能体 |
| `/doctor` | 检查智能体安装状态 |
| `/status` | 显示当前状态 |
| `/cancel` | 取消当前请求 |
| `/quit` | 退出 CLI |

### 单次提问

发送单个提示并退出：

```bash
pnpm cli prompt "2+2等于多少？"
pnpm cli prompt "列出文件" --cwd=/tmp
```

### 选项

| 选项 | 描述 |
|--------|-------------|
| `--cwd=PATH` | 智能体的工作目录 |
| `--log` | 将会话日志保存到 `logs/` 目录 |
| `--log=PATH` | 将会话日志保存到指定文件 |

## 开发

```bash
# 以开发模式启动 Electron 应用
pnpm dev

# 类型检查
pnpm typecheck

# 运行测试
pnpm test
```

## 构建

```bash
pnpm build:mac      # macOS
pnpm build:win      # Windows
pnpm build:linux    # Linux
```

## 架构

```
Multica (Electron)
+-- 渲染进程 (React)
|   +-- UI 组件（聊天、设置等）
|
+-- 主进程
|   +-- Conductor（协调智能体通信）
|   |   +-- SessionStore（会话持久化）
|   |   +-- ClientSideConnection（ACP SDK）
|   |         +-- AgentProcess（子进程管理）
|   |               +-- opencode/codex-acp/gemini (stdio)
|   |
|   +-- IPC 处理器（会话、智能体、配置）
|
+-- Preload (contextBridge)
    +-- electronAPI（暴露给渲染进程）
```

### 会话管理

Multica 在 ACP 之上维护自己的会话层：

```
~/.multica/sessions/
+-- index.json              # 会话列表（快速加载）
+-- data/
    +-- {session-id}.json   # 完整会话数据 + 更新
```

**关键设计决策：**
- **客户端存储**：Multica 存储原始的 `session/update` 数据用于 UI 展示
- **智能体无关**：每个智能体独立管理自己的内部状态
- **恢复行为**：创建新的 ACP 会话，在 UI 中显示存储的历史记录

## 许可证

Apache-2.0
