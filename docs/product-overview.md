# Multica 产品全景文档

> **文档说明**
>
> 这份文档的目的是：**让任何没有写过代码的新同事，在 30 分钟内完全理解 Multica 这个产品到底有哪些功能、每个功能在整体中处于什么位置、一个功能和另一个功能如何协同**。
>
> 它的受众包括：
>
> - **新加入的工程师 / 产品 / 设计 / 运营**——用它做 onboarding 的第一份材料
> - **产品介绍工作**——需要对外讲解 Multica 时的事实基础
> - **文案工作者**——写交互文案、营销文案、帮助文档时，需要知道某个词（比如 "Skill"、"Runtime"、"Autopilot"）在产品体系里代表什么
> - **任何需要在修改某个局部前，先理解它与整体关系的人**
>
> 它**不是**：开发者文档、架构决策记录（ADR）、或者销售话术。它是**功能事实的汇总**——每一条描述都能在代码、schema 或 API 里找到对应。
>
> 文档基于对整个 monorepo（server、apps、packages、migrations、daemon、CLI）的系统性调研生成，数据截止日期 2026-04-21。

---

## 目录

1. [Multica 是什么](#1-multica-是什么)
2. [核心概念词典](#2-核心概念词典)
3. [功能全景（按模块）](#3-功能全景按模块)
   - 3.1 [Workspace 工作区](#31-workspace-工作区)
   - 3.2 [Issue 议题管理](#32-issue-议题管理)
   - 3.3 [Project 项目](#33-project-项目)
   - 3.4 [Agent 智能体](#34-agent-智能体)
   - 3.5 [Runtime 运行时 & Daemon 守护进程](#35-runtime-运行时--daemon-守护进程)
   - 3.6 [Skill 技能](#36-skill-技能)
   - 3.7 [Autopilot 自动驾驶](#37-autopilot-自动驾驶)
   - 3.8 [Chat 对话](#38-chat-对话)
   - 3.9 [Inbox 收件箱与通知](#39-inbox-收件箱与通知)
   - 3.10 [成员、邀请与权限](#310-成员邀请与权限)
   - 3.11 [搜索与命令面板](#311-搜索与命令面板)
   - 3.12 [认证、登录与 Onboarding](#312-认证登录与-onboarding)
   - 3.13 [设置与个人资料](#313-设置与个人资料)
   - 3.14 [CLI 命令行工具](#314-cli-命令行工具)
4. [系统架构全景](#4-系统架构全景)
5. [产品地图（全部路由）](#5-产品地图全部路由)
6. [跨平台差异：Web vs 桌面](#6-跨平台差异web-vs-桌面)
7. [附录：关键数据表速查](#7-附录关键数据表速查)

---

## 1. Multica 是什么

### 一句话定位

**Multica 把编码智能体变成真正的团队成员。**

像给同事分配任务一样，把一个 issue 指派给一个 agent，它会自己认领、写代码、汇报进度、更新状态——不需要你一直守着。

### 解决的问题

传统方式用 AI coding agent 的痛点：

- 每次都要复制粘贴 prompt
- 必须盯着终端，看它跑不跑得完
- 没有跨任务的记忆，每次都从零开始
- 多个 agent 同时工作时，没有一个"看板"能看到全局

Multica 做的事：

- Agent 和人**共用同一个任务看板**（issue board）
- Agent **有 profile**，会出现在 assignee 下拉里、会在评论区发言、会自己创建 issue
- 同一个 (agent, issue) 的多轮对话**自动恢复会话**——上一次的上下文、工作目录都保留
- **Skill 系统**让历史上解决过的问题沉淀成可复用的能力
- **Autopilot** 让 agent 按定时规则自动开工（比如每天早上 9 点做 bug triage）

### 定位一句话版本

> Multica 不是一个 AI 工具，而是一个**人 + AI 协作的任务管理平台**。agent 是一等公民，和人在同一个工作流里。

### 部署形态

- **云版本（Multica Cloud）**：官方托管服务，agent 通过你本地跑的 daemon 执行
- **自托管（Self-Host）**：完整后端可以部署在自己的服务器
- **客户端**：Next.js web 版 + Electron 桌面版（两端体验基本一致，桌面独有：多标签、原生托盘、自动更新）

### 支持的 Coding Agent

Multica **不自己训模型**，也不锁定某一家厂商。它是调度器，本地 daemon 会自动探测以下 CLI 工具并接入：

Claude Code · Codex · OpenClaw · OpenCode · Hermes · Gemini · Pi · Cursor Agent · Kimi · Kiro CLI

每个 agent 可以配置自己的模型、API Key、环境变量、MCP 服务器。

---

## 2. 核心概念词典

**理解这些名词是理解产品的前提。每个概念的定义都严格对应数据库表。**

| 概念 | 定义 | 映射的数据表 |
|------|------|-------------|
| **User 用户** | 一个人类账号，可以登录，属于多个 workspace | `user` |
| **Workspace 工作区** | 一切资源的容器。issue、agent、project、skill 全部隔离在 workspace 里。就是 Linear/Notion 里的 workspace/team 概念 | `workspace` |
| **Member 成员** | 用户在某个 workspace 里的身份。一个用户在不同 workspace 可以有不同角色（owner/admin/member） | `member` |
| **Agent 智能体** | 可被指派任务的 AI 工作者。有 profile（名字、头像、说明）、会指定 runtime 和 provider、可以配自定义 prompt 和技能 | `agent` |
| **Runtime 运行时** | Agent 实际跑在哪里的**执行环境**。可以是用户本地机器（通过 daemon）或云端实例。**一个 runtime = 一台可以跑 agent 的机器** | `agent_runtime` |
| **Daemon 守护进程** | 用户本地运行的后台程序，自动发现已安装的 coding CLI 并注册为 runtime，然后不停轮询 server 认领任务 | （进程，不是表） |
| **Issue 议题** | 一个工作单元——任务、bug、feature。最核心的产品对象。可以分配给人或 agent | `issue` |
| **Comment 评论** | Issue 下的讨论回复。人和 agent 都能发。在评论里 `@某个 agent` 会自动触发这个 agent 的新任务 | `comment` |
| **Task 任务** | Agent 执行一次 issue 所产生的一次运行。本质是"一次 agent 跑起来的会话"。队列化执行 | `agent_task_queue` |
| **Skill 技能** | 工作区级别的可复用说明文档。作用是给 agent 提供"怎么做某件事"的上下文。Agent 开跑时会把挂载的 skill 内容注入到工作目录让 CLI 能读到 | `skill`, `skill_file`, `agent_skill` |
| **Project 项目** | 议题的高层归属，类似"里程碑"或"版本"。issue 可以归属到 project | `project` |
| **Autopilot 自动驾驶** | 定时或被触发的自动化规则。按 cron 或 webhook 触发，自动创建 issue 并分配给 agent | `autopilot`, `autopilot_trigger`, `autopilot_run` |
| **Chat 对话** | 用户和 agent 的持久化多轮对话。不依附于 issue | `chat_session`, `chat_message` |
| **Inbox 收件箱** | 个人通知中心。被 @、被分配、订阅的 issue 有更新都会进这里 | `inbox_item` |
| **Subscriber 订阅者** | 谁关注某个 issue。被分配、被 @、评论过都会自动订阅。订阅者会收到 inbox 通知 | `issue_subscriber` |
| **Activity 活动 / Timeline 时间线** | 所有关键动作的审计记录。issue 详情页的"时间线"就是这个表的数据 | `activity_log` |
| **Pin 固定** | 个人侧边栏快捷方式，把常用的 issue/project 置顶 | `pinned_item` |
| **Reaction 反应** | Issue 或评论上的 emoji 反应，跟 GitHub/Slack 一样 | `issue_reaction`, `comment_reaction` |
| **Attachment 附件** | Issue 或评论的文件上传，支持 S3/CloudFront 或本地存储 | `attachment` |
| **Personal Access Token (PAT)** | 用户级 API token，CLI 和自动化用。`mul_` 前缀 | `personal_access_token` |
| **Daemon Token** | 单 workspace 单 daemon 的 token。`mdt_` 前缀，比 PAT 权限范围更小 | `daemon_token` |
| **Session Resumption 会话恢复** | 同一对 (agent, issue) 的下一次任务会自动复用上次 Claude Code 的 `session_id` 和工作目录——历史对话、文件状态都保留 | `agent_task_queue.session_id`, `.work_dir` |
| **MCP (Model Context Protocol)** | Anthropic 提出的协议，让 agent 通过标准接口调用外部工具。每个 agent 可配自己的 MCP server 列表 | `agent.mcp_config` (JSONB) |
| **Workspace Context 工作区上下文** | 工作区级别的 agent 系统提示词。所有该工作区的 agent 都会感知到它 | `workspace.context` |
| **Polymorphic Actor 多态行动者** | 设计范式：几乎所有"谁做了什么"的字段都是 `actor_type` (`member`/`agent`) + `actor_id`。这就是为什么 agent 能像人一样创建 issue、发评论、被订阅 | 贯穿所有表 |

---

## 3. 功能全景（按模块）

### 3.1 Workspace 工作区

> **角色**：一切的容器。Multica 的多租户边界。

#### 功能

- **多工作区**：一个用户可以属于多个 workspace，每个 workspace 完全隔离（issue、agent、skill、成员都独立）。
- **创建工作区**：只需要一个名字；自动生成 slug（URL 中使用的短 ID）。
- **切换工作区**：侧边栏下拉；桌面端每个工作区有独立的标签组。
- **离开工作区**：非 owner 成员可自行离开。
- **删除工作区**：只有 owner 可以，硬删除+级联。
- **Workspace 设置**：名称、slug、描述、**Workspace Context**（给该工作区所有 agent 的统一系统提示）、**仓库列表**（workspace 允许 agent 访问的 Git 仓库 URL 白名单）。
- **Workspace 头像 / issue 前缀**：每个工作区可以有自己的 issue 编号前缀（如 `ACME-42`）。

#### 产品里的位置

Workspace 不是一个功能，而是**所有功能的坐标系**。URL 的形态永远是 `/{workspace-slug}/...`，API 请求永远带 `X-Workspace-Slug` 头。一个 issue、一个 agent、一个 skill，脱离了 workspace 就没有意义。

#### 对应表

`workspace`, `member`, `workspace_invitation`

---

### 3.2 Issue 议题管理

> **角色**：Multica 的核心工作对象。

Issue 对应的概念在 Linear 叫 Issue、在 Jira 叫 Ticket、在 GitHub 叫 Issue——就是一个任务单元。Multica 的特色在于**issue 可以分配给 agent，和分配给人完全对等**。

#### 核心字段

- 标题、描述（Tiptap 富文本）、状态、优先级
- 编号（自动递增，带 workspace 前缀）
- **Assignee（可以是 member 或 agent）**
- **Creator（可以是 member 或 agent）**——agent 也能创建 issue
- Parent issue（用来做子任务）
- Project（归属的项目）
- Due date（截止日期）
- Labels（多对多标签）
- Dependencies（依赖/阻塞关系）
- Acceptance criteria（验收标准，JSONB）
- Origin（如果是 autopilot 创建的，会记录来源 autopilot run）

#### 视图

- **List 列表视图**：表格形式，可按 status/priority/assignee/creator/project 过滤、按名称/优先级/截止日/手动位置排序；支持开放和已完成分页。
- **Board 看板视图**：Kanban，按状态分列；支持拖拽（拖动会自动切到"手动排序"模式）。
- **My Issues 我的议题**：专属视图，三个 scope：分配给我 / 我创建的 / 我的 agent 负责的。

#### 交互

- **快速创建**：侧边栏单行快速创建、或弹窗富文本创建（支持草稿本地持久化）
- **批量操作**：多选后批量改 status/priority/assignee/删除
- **子 issue**：父 issue 显示子任务完成比例圆环
- **订阅（subscribe）**：默认 creator、assignee、被 @ 的人会自动订阅
- **Reaction**：issue 和评论都能加 emoji 反应
- **Pin 固定**：把 issue 置顶到侧边栏快捷栏
- **复制链接 / 快捷键跳转（Cmd+K）**
- **Timeline 时间线**：所有关键动作（状态变更、指派变更、评论）按时间顺序展示，混合 `activity_log` + `comment` 两类记录

#### 评论与讨论

- Tiptap 富文本编辑器，支持 `@` 提到 member 或 agent
- 嵌套回复（一层）
- emoji 反应
- **@agent 触发任务**：在评论里提到某个 agent，会自动生成一个新的 agent task，让它来回复/处理

#### 附件

- 拖拽上传或按钮上传
- 图片内联预览
- 存储后端：S3/CloudFront 或本地磁盘（自托管）

#### 产品里的位置

Issue 是**所有工作流的载体**：
- Agent 通过"被分配到 issue"获得任务
- Autopilot 通过"创建 issue"来触发 agent
- 评论通过"@agent" 追加任务
- Inbox 通知围绕 issue 生成

#### 对应表

`issue`, `comment`, `issue_label`, `issue_to_label`, `issue_dependency`, `issue_subscriber`, `issue_reaction`, `comment_reaction`, `attachment`, `activity_log`, `pinned_item`

---

### 3.3 Project 项目

> **角色**：多个 issue 的高层容器，类似 Linear 的 Project、Jira 的 Epic。

#### 功能

- 标题、描述、图标（emoji 或标识符）
- 状态：`planned` / `in_progress` / `paused` / `completed` / `cancelled`
- 优先级：urgent / high / medium / low / none
- **Lead 负责人**：可以是 member 或 agent（跟 issue 的 assignee 一样是多态）
- 详情页展示项目内的所有 issue
- 支持搜索项目

#### 产品里的位置

Project 相比 Issue 是更高层的组织单元。一个 issue 可以不属于任何 project，但如果属于，会在列表页的筛选、侧边栏导航、面包屑里集中展示。

#### 对应表

`project`

---

### 3.4 Agent 智能体

> **角色**：AI 工作者。Multica 最独特的对象。

一个 Agent 不是一个"AI 模型"，而是一个**带配置的工作者身份**。它有名字、头像、个人描述、说明书（系统提示词）、绑定的运行时、挂载的技能。在 UI 上它和人一样会出现在 assignee 下拉、评论作者、订阅者列表里。

#### 配置字段

- **基本信息**：名字、描述、头像（自动生成）
- **Provider**：选择底层是 Claude / Codex / OpenClaw / OpenCode / Hermes / Gemini / Pi / Cursor / Kimi / Kiro 中的哪一个
- **Runtime**：绑定到哪个运行时（即在哪台机器上跑）
- **Instructions 说明书**：agent 的系统提示词（"你是一个资深工程师..."）
- **Custom Env**：要注入到 CLI 进程的环境变量（如 `ANTHROPIC_API_KEY`、`ANTHROPIC_BASE_URL`、`CLAUDE_CODE_USE_BEDROCK`）
- **Custom Args**：附加给 CLI 的启动参数（如 `--model`, `--thinking`）
- **MCP Config**：Model Context Protocol 服务器列表（让 agent 有额外工具能力）
- **Max Concurrent Tasks**：同时最多跑几个任务
- **Skills**：关联多个 skill（见 3.6）
- **Visibility**：`workspace`（工作区可见）或 `private`（仅创建者可见）

#### 状态

- `idle` / `working` / `blocked` / `error` / `offline`——由 runtime heartbeat 决定
- 可以被 archive（软删除）

#### 交互

- 在 **Settings → Agents** 页面创建、编辑、归档
- 在 issue 的 assignee 下拉里选择
- 在评论里 `@agent` 触发
- 在 chat 面板里直接聊

#### 产品里的位置

Agent 是 Multica 的灵魂。几乎所有功能都围绕"如何让一个 agent 干活"展开：
- Issue 通过分配触发 agent
- Skill 通过挂载赋能 agent
- Runtime 提供 agent 的运行环境
- Autopilot 调度 agent 自动开工
- Chat 提供 agent 的对话界面

#### 对应表

`agent`, `agent_skill`

---

### 3.5 Runtime 运行时 & Daemon 守护进程

> **角色**：Agent 真正跑起来的物理/虚拟机器。

这是 Multica **分布式执行架构**的核心设计：**agent 不在 server 上运行，而在用户自己的机器上运行**。Server 只做任务调度、状态同步、数据存储。

#### Daemon 是什么

`multica` CLI 在用户的机器上启动一个后台进程（macOS launchd / Linux systemd / Windows 服务风格），它：

1. **自动探测** `$PATH` 上安装的 coding CLI（`claude`, `codex`, `opencode`, `openclaw`, `hermes`, `gemini`, `pi`, `cursor-agent`, `kimi`, `kiro-cli`）
2. 向 server **注册** 为一组 runtime（一个 CLI = 一个 runtime）
3. 每 3 秒 **轮询** 一次 server，有任务就认领
4. 每 15 秒 **心跳**（keepalive），报告自己还活着
5. 认领任务后，在本机的隔离工作目录里**启动 agent CLI**，把 agent 的输出流**实时推回 server**
6. 任务完成后上报结果、token 用量、session id 和工作目录（用于下次恢复）

#### Runtime 展示

在 **Settings → Runtimes** 页面可以看到：

- 每个 runtime 的名字、提供方（图标）、owner（谁的机器）、状态指示（在线/离线）、last seen 时间
- Ping 诊断：手动戳一下看响应
- Usage 用量：近期的 token 消耗统计
- Activity：任务活动情况
- CLI 安装指引（自托管模式下）
- 桌面端独有：**本地 daemon 卡片**，显示本机 daemon 状态、可一键重启

#### Runtime 的生命周期

- **注册**：daemon 启动时 POST `/api/daemon/register` 得到 runtime ID
- **在线**：15 秒一次心跳
- **离线**：如果 server 45 秒没收到心跳，把 runtime 标记为离线（server 后台 sweeper 每 30 秒巡检）
- **孤儿任务回收**：超过 5 分钟还在 dispatched 或超过 2.5 小时还在 running 的任务，sweeper 会把它标记为失败
- **长期离线 GC**：7 天没心跳且没活跃 agent 的 runtime 会被回收

#### CLI 与 Daemon 的关系

| 命令 | 说明 |
|------|------|
| `multica setup` | 一键配置：填 URL + 登录 + 启动 daemon |
| `multica login` | 浏览器打开 OAuth 登录，保存 90 天 PAT 到 `~/.multica/config.json` |
| `multica login --token <pat>` | 无头登录（SSH/CI） |
| `multica daemon start` | 后台启动 daemon（写 PID 到 `~/.multica/daemon.pid`，日志到 `~/.multica/daemon.log`） |
| `multica daemon stop` | 发 SIGTERM，优雅关闭（等待进行中的任务完成，超时 30s） |
| `multica daemon status` | 打印 daemon 状态、探测到的 agent、watch 中的 workspace |
| `multica daemon logs -f` | 实时跟随日志 |
| `multica daemon start --profile <name>` | 启动独立配置的 daemon（用于多环境，比如同时连 staging 和生产） |

#### 安全边界

- 每个任务一个**独立工作目录** `~/multica_workspaces/{ws}/{task_short_id}/workdir/`
- 环境变量**过滤**：阻止 agent 覆盖 daemon 的认证变量（`MULTICA_TOKEN` 等）
- 仓库访问**白名单**：agent 只能 checkout workspace 配置的仓库
- Codex 有**版本相关的 sandbox 策略**

#### 产品里的位置

Runtime 是让"给 agent 分配任务"这件事**能真正发生**的基础设施。没有 runtime，所有 agent 就是空壳。用户第一次 onboarding 时必须至少有一个 runtime 在线，否则 agent 没法干活。

#### 对应表

`agent_runtime`, `daemon_token`, `daemon_pairing_session`（弃用中）, `daemon_connection`（弃用中）, `runtime_usage`

---

### 3.6 Skill 技能

> **角色**：让 agent "学会"某种工作方式的可复用说明文档。

Skill 是一组 Markdown 文档 + 配套文件。它**不是代码**，**不是 prompt 模板**，而是**给 agent CLI 读的说明**。

#### 数据形态

```
skill
  ├─ name:         "react-patterns"
  ├─ description:  "Common React patterns and best practices"
  ├─ content:      "## Overview\n..."     # 主要说明文档
  └─ files:
      ├─ examples/hooks.md
      └─ examples/useState.jsx
```

#### 它怎么工作

1. **创建**：在 **Settings → Skills** 页面创建或从 URL 导入（如 clawhub.ai、skills.sh）
2. **挂载**：给某个 agent 勾选要用的 skill
3. **注入**：当 agent 认领任务时，daemon 把挂载的 skill 内容写到任务工作目录的 **provider 原生位置**：
   - Claude Code → `.claude/skills/{name}/SKILL.md`
   - Codex → `CODEX_HOME/skills/{name}/`
   - OpenCode → `.opencode/skills/{name}/SKILL.md`
   - Pi → `.pi/skills/{name}/SKILL.md`
   - Cursor → `.cursor/skills/{name}/SKILL.md`
   - GitHub Copilot → `.github/skills/{name}/SKILL.md`
   - 其他 → `.agent_context/skills/{name}/SKILL.md`
4. **使用**：agent CLI 自己按照 provider 约定发现并读取这些文件

> 💡 **Skill 是静态的**——不是 AI 生成的，也不会随执行变化。它是人写的经验文档。未来可能扩展成"AI 从历史任务中沉淀技能"，但当前版本不是。

#### CLI 对应命令

```bash
multica skill list
multica skill get <id>
multica skill create --title ...
multica skill import --url https://...
multica skill files upsert <skill-id> --path ...
```

#### 产品里的位置

Skill 是 Multica 区别于"每次都要写长 prompt"的关键机制。它让团队的专业知识**沉淀成可复用的组件**，绑在 agent 上就生效——就像给员工写的 SOP/playbook。

从架构角度：skill 不参与执行逻辑，只参与**上下文注入**。它在整个任务生命周期里只出现一次——在 daemon 启动 CLI 之前的环境准备阶段。

#### 对应表

`skill`, `skill_file`, `agent_skill`

---

### 3.7 Autopilot 自动驾驶

> **角色**：让 agent 在没人触发的时候也能自己开工的调度器。

Autopilot 解决的问题：很多工作是**周期性**的——每天早上的 bug triage、每周的依赖审计、每月的安全扫描。人手动触发太烦，Autopilot 是规则化自动触发。

#### 数据形态

```
autopilot
  ├─ title, description
  ├─ assignee:        <agent_id>          # 指定哪个 agent 跑
  ├─ execution_mode:  create_issue | run_only
  ├─ issue_title_template:  "Daily triage - {{date}}"
  ├─ concurrency_policy:    skip | queue | replace
  └─ triggers (多个):
       ├─ kind:  schedule | webhook | api
       ├─ cron_expression
       ├─ timezone
       └─ webhook_token
```

#### 两种执行模式

- **`create_issue`（默认）**：触发时先创建一个新 issue（标题用 `issue_title_template` 渲染），再把 issue 分配给 agent，走正常 agent 任务流程
- **`run_only`**：直接创建 task，不关联 issue（适合"只执行不留下 ticket"的场景，比如每小时检查某状态）

#### 三种触发方式

- **Schedule（cron）**：server 后台每 30 秒扫一次 `autopilot_trigger`，到点的触发出去
- **Webhook**：给出一个带 `webhook_token` 的 URL，外部 POST 即可触发
- **API / Manual**：UI 上点"立即运行"按钮，或用 CLI `multica autopilot trigger <id>`

#### 并发策略

- `skip`：同一个 autopilot 上一次还没跑完，跳过这次（去重）
- `queue`：排队等上一次跑完
- `replace`：中止上一次，换成这次

#### 运行记录

每次触发都在 `autopilot_run` 里留一条记录：`pending → issue_created → running → completed/failed/skipped`。在 UI 的 autopilot 详情页可以看全部历史。

#### 内置模板

产品提供一些现成的 autopilot 模板，一键创建：

- Daily news digest（每天 9:00）
- PR review reminder（工作日 10:00）
- Bug triage（工作日 9:00）
- Weekly progress report（每周 17:00）
- Dependency audit（每周 10:00）
- Security scan（每周 02:00）

#### 产品里的位置

Autopilot 让 Multica 从"你分配 → agent 做"升级到"agent 自己发起工作"。配合 `run_only` 模式，甚至可以在没有 issue 的前提下跑定时任务。Issue 上的 `origin_type=autopilot` + `origin_id` 字段留下了"这个 issue 是哪个 autopilot run 创建的"的追溯链。

#### 对应表

`autopilot`, `autopilot_trigger`, `autopilot_run`

---

### 3.8 Chat 对话

> **角色**：用户和 agent 的持久多轮对话界面，不依附于 issue。

有时候你不想为了和 agent 说一句话就开一个 issue。Chat 就是为这种"轻量对话"准备的——像 ChatGPT 的对话界面，但是你在和你工作区的某个 agent 对话。

#### 功能

- **创建会话**：选一个 agent 开始
- **消息列表**：支持 Markdown 渲染、代码块高亮
- **发送消息**：消息会被 queue 成一个 task，agent 执行后把响应作为消息写回
- **流式响应**：通过 WebSocket 实时推送
- **未读跟踪**：`unread_since` 字段记录第一条未读消息的时间戳
- **归档**：把旧会话移出活跃列表
- **Session 复用**：同一个 chat session 下的多轮消息会复用底层 CLI 的 `session_id`（Claude Code 能保留对话上下文）

#### 和 Issue 评论的区别

| | Chat | Issue 评论 |
|---|---|---|
| 上下文载体 | 独立 session（chat_session） | 某个 issue |
| 是否公开 | 个人和 agent 对话（私有） | 工作区所有成员可见 |
| 触发 agent | 每条 user 消息都触发 | 需要 `@agent` |
| 用途 | 探索、提问、一次性任务 | 和 issue 强绑定的工作推进 |

#### 产品里的位置

Chat 填补了"不够正式到需要开 issue、但又需要持久化"的对话空白。同时也是体验上更像常规聊天软件的入口。

#### 对应表

`chat_session`, `chat_message`；底层执行仍走 `agent_task_queue`（`chat_session_id` 字段区分）

---

### 3.9 Inbox 收件箱与通知

> **角色**：每个人的个人通知中心。

#### 数据形态

`inbox_item` 是推给特定"recipient"的条目：

- recipient_type = `member` 或 `agent`（agent 也能有 inbox！）
- type（e.g. `issue_assigned`, `comment_mention`, `task_completed`, `invitation_created`）
- severity（`action_required` / `attention` / `info`）
- 关联的 issue（如果有）
- read / archived 状态

#### 通知触发场景

- Issue 被分配给你
- 被 @ 提到
- 订阅的 issue 状态变化
- 订阅的 issue 有新评论
- 工作区邀请
- 你的 agent 任务完成/失败

#### 订阅机制（自动）

Server 的 subscriber listener 自动把以下人加入 `issue_subscriber`：

- issue creator
- 当前 assignee（变更会同步更新）
- 评论里被 @ 的人
- 手动订阅的人

#### UI

- **Inbox 页面**：两栏布局，左边列表 + 右边 issue 详情
- **批量操作**：全部标记已读 / 仅归档已读 / 归档已完成 issue 的通知
- **徽标**：侧边栏导航上显示未读数
- **WebSocket 推送**：新 inbox 条目实时到达（`inbox:new` 事件只发给目标用户）

#### 产品里的位置

Inbox 是"主动注意力系统"，让用户不必一直盯着看板也知道哪些事要自己处理。

#### 对应表

`inbox_item`, `issue_subscriber`

---

### 3.10 成员、邀请与权限

#### 角色体系

| 角色 | 权限 |
|------|------|
| **Owner** | 全部；唯一能删除工作区的角色 |
| **Admin** | 管理成员、管理设置；不能删工作区，不能移除其他 admin |
| **Member** | 创建 issue、评论、自我分配、使用 agent |

#### 邀请流程

- Admin 在 **Settings → Members** 输入邮箱邀请
- Server 生成 `workspace_invitation` 记录（7 天过期）
- 发送邮件（Resend 集成，未配置时打到 stderr）
- 被邀请人收到邀请：如果已有账号，会出现在个人 Inbox；如果没账号，邮件里有注册链接
- 接受 / 拒绝 / 过期

#### UI

- 成员列表：头像、邮箱、角色徽章、操作菜单（改角色、移除）
- 待处理邀请列表：可 resend、revoke
- Invite 接受页面（`/invite/[id]`）：展示工作区信息、接受/拒绝按钮

#### 邀请接受的桌面特殊处理

桌面端的 `multica://invite/{id}` 深链接**不是走路由**，而是触发 `WindowOverlay`——共享视图组件 `InvitePage` 装在原生窗口覆盖层里，保证拖拽移动窗口等原生体验。

#### 产品里的位置

成员管理是**一切协作的前提**。但在 Multica 里它有一个独特之处：成员系统也管 agent。之所以要有 `assignee_type` 区分 member 和 agent，就是为了让两者在同一套 API 里表达"谁可以被分配"。

#### 对应表

`member`, `workspace_invitation`

---

### 3.11 搜索与命令面板

#### 命令面板（Cmd+K）

全局搜索入口，覆盖：

- **Issues**（按标题、编号匹配）
- **Projects**（按名称匹配）
- **Workspaces**（按名称匹配，用于快速切换）
- **Navigation**（跳转到设置、runtimes、skills 等）
- **Actions**（新建 issue、新建 project、切换主题）
- **Recent Issues**（最近访问过的，自动记录）

#### 列表过滤

Issue 列表、project 列表、inbox 等都有本地 filter chips 和 search input。

#### 全文搜索

`GET /api/issues/search` 支持对 issue 的标题、描述、评论内容做全文搜索，返回命中片段。

> **当前没有基于向量的语义搜索**——产品宣传是 AI-native，但没有用 pgvector。Schema 里也没启用向量扩展。未来可能扩展。

#### 产品里的位置

Cmd+K 是 keyboard-first 用户（Linear-style）的主要导航方式，比点击侧边栏更快。

---

### 3.12 认证、登录与 Onboarding

#### 登录方式

- **邮箱验证码（Magic Link 风格）**：输入邮箱 → 收 6 位验证码 → 输入验证码登录
- **Google OAuth**：一键 Google 登录
- **PAT（CLI）**：用户在 Settings → API Tokens 里生成的 token，CLI/脚本场景

#### Onboarding 流程（正在重设计中）

位于 `packages/views/onboarding/` 和 `apps/web/app/(auth)/onboarding/`。

经典 5 步：

1. **Welcome** — 欢迎页
2. **Workspace** — 创建工作区（或跳过，如果已有）
3. **Runtime** — 展示可用的 runtime 和 CLI 安装指引
4. **Agent** — 创建第一个 agent（需要有 runtime）
5. **Complete** — 展示创建好的 workspace 和 agent，跳转到 dashboard

#### 邀请接受（Zero-workspace）

如果新用户是被邀请进来的（还没有自己的 workspace），接受邀请后直接进入该工作区，跳过 onboarding。

#### 认证后的跳转规则

- 已登录且有至少一个 workspace：跳到 `/{slug}/issues`
- 已登录但没有 workspace：进入 `/workspaces/new` 或 onboarding
- 未登录：跳到 `/login`

#### Signup 限流

Server 支持：
- `ALLOW_SIGNUP=false` 关闭注册
- `ALLOWED_EMAILS` / `ALLOWED_EMAIL_DOMAINS` 白名单

#### 产品里的位置

Onboarding 是新用户能不能成功把 agent 跑起来的关键漏斗。任何一步没完成（尤其是 runtime 没连上），后续功能都是空壳。

#### 对应表

`user`, `verification_code`, `personal_access_token`

---

### 3.13 设置与个人资料

#### My Account 标签

- **Profile**：名字、头像（不可上传，系统生成）、邮箱（只读）
- **Appearance**：主题（light / dark / system）
- **API Tokens**：创建/查看/撤销 PAT；创建时一次性展示完整 token
- **Daemon**（桌面独有）：本机 daemon 状态、重启、开机自启开关
- **Updates**（桌面独有）：当前版本、检查更新、自动更新开关

#### Workspace 标签

- **General**：名字、描述、**Workspace Context**（agent 系统级提示）
- **Members**：见 3.10
- **Repositories**：GitHub 集成，连接仓库列表，agent 白名单
- **Agents / Runtimes / Skills / Autopilots**：各自独立页面（实际上这些在侧边栏直接有入口，settings 里也有对应管理 tab）

#### 产品里的位置

Settings 是所有"配置即工作"动作的汇总：agent 的 prompt、workspace 的 context、仓库白名单、skill 的内容——都在这里。**对运营和文案来说最重要的一句话**：用户在 Multica 的 settings 页面做的配置，每一项都会影响 agent 实际执行时读到的上下文。

---

### 3.14 CLI 命令行工具

`multica` 不只是启动 daemon 的工具，也是完整的命令行操作层。很多用户喜欢在终端里推进工作而不是开 UI。

#### 工作区 / 议题

```bash
multica workspace list | get | watch | unwatch
multica issue list | get | create | update | assign | status
multica issue comment list | add | delete
multica issue runs <id>                 # 查看任务执行历史
multica issue run-messages <task-id>    # 查看某次执行的消息
```

#### Agent / Skill / Autopilot / Project / Repo

```bash
multica agent list | get | create | update | archive
multica skill list | get | create | update | delete | import | files upsert
multica autopilot list | get | create | update | trigger
multica autopilot trigger-add --cron "0 9 * * 1-5"
multica project list | get | create | update
multica repo list | add | update | delete
```

#### Runtime

```bash
multica runtime list | usage | activity | update
```

#### 配置 / 更新

```bash
multica config show | set server_url ...
multica auth status | logout
multica version | update
```

#### 产品里的位置

CLI 是 Multica 对开发者友好度的体现。对于 agent 自己来说，也同等重要——**agent 在执行任务时能调用 `multica` 命令读写 issue、评论、查文档**，这正是 CLI 在 "agent 作为一等公民"架构里的作用。

---

## 4. 系统架构全景

```
┌─────────────────────┐        ┌────────────────────┐        ┌──────────────────┐
│  Next.js Web App    │        │  Electron Desktop  │        │  multica CLI     │
│  apps/web           │        │  apps/desktop      │        │  server/cmd/     │
└──────────┬──────────┘        └──────────┬─────────┘        └────────┬─────────┘
           │  HTTP + WebSocket             │                           │  HTTP
           │                               │                           │
           └──────────────┬────────────────┴───────────────┬───────────┘
                          │                                │
                          ▼                                ▼
              ┌─────────────────────────────────────────────────┐
              │               Go Backend (server/)              │
              │  • Chi HTTP router  • gorilla/websocket hub      │
              │  • sqlc generated queries                        │
              │  • In-process event bus                          │
              │  • Background workers (sweeper / scheduler)      │
              └──────────────────┬──────────────────────────────┘
                                 │
                                 ▼
                      ┌──────────────────────┐
                      │  PostgreSQL 17       │
                      │  + pgcrypto          │
                      │  (28 tables)         │
                      └──────────────────────┘

                                 ▲
                                 │ HTTPS poll + heartbeat
                                 │
              ┌─────────────────────────────────────────────────┐
              │         Local Daemon (用户机器上运行)            │
              │  • 每 3s 认领任务  • 每 15s 心跳                 │
              │  • 探测并启动 agent CLI 子进程                   │
              │  • 为任务准备隔离工作目录                        │
              └───────────────┬─────────────────────────────────┘
                              │ spawns
              ┌───────────────┼─────────────────────────────────┐
              ▼               ▼              ▼              ▼
         Claude Code      Codex         OpenCode      …其他 CLI
         (子进程)         (子进程)      (子进程)
```

### 分层职责

| 层 | 负责什么 | 不负责什么 |
|---|---|---|
| **Web / Desktop 客户端** | UI、本地客户端状态（Zustand）、服务器状态缓存（TanStack Query）、WebSocket 订阅 | 业务规则、AI 调用 |
| **Server** | 持久化、权限、任务编排、事件广播、Autopilot 调度、Runtime 健康监测 | 不直接执行 agent、不调 LLM |
| **Daemon** | 探测并启动本地 CLI、管理任务工作目录、流式上报消息、session 恢复 | 不做业务决策、只认 server 给它的任务 |
| **Agent CLI（Claude Code 等）** | 实际调用 LLM、执行工具调用、写文件、跑测试 | 不感知 Multica 的数据模型（所有上下文通过 `multica` CLI 命令读回） |

### 实时层（WebSocket）

Server 启动一个 WebSocket hub：

- **鉴权**：URL 参数里的 JWT 或 PAT + workspace_slug
- **房间模型**：按 workspace 分房间，一个 workspace 的事件只广播给该房间的连接
- **个人定向推送**：`inbox:new`, `invitation:created` 等个人事件用 `SendToUser`
- **心跳**：server 每 54 秒 ping，客户端 60 秒内必须 pong

**全部事件类型（供文案参考，共约 60+ 个）**：
- `issue:created` / `issue:updated` / `issue:deleted`
- `comment:created` / `comment:updated` / `comment:deleted` / `reaction:added` / `issue_reaction:added`
- `agent:created` / `agent:status` / `agent:archived`
- `task:dispatch` / `task:progress` / `task:message` / `task:completed` / `task:failed` / `task:cancelled`
- `inbox:new` / `inbox:read` / `inbox:archived` / `inbox:batch-*`
- `workspace:updated` / `workspace:deleted` / `member:added` / `member:updated` / `member:removed`
- `invitation:created` / `invitation:accepted` / `invitation:declined` / `invitation:revoked`
- `chat:message` / `chat:done` / `chat:session_read`
- `skill:created` / `skill:updated` / `skill:deleted`
- `project:created` / `project:updated` / `project:deleted`
- `autopilot:created` / `autopilot:updated` / `autopilot:run_start` / `autopilot:run_done`
- `subscriber:added` / `activity:created`
- `daemon:heartbeat` / `daemon:register`

客户端收到事件后的模式：要么直接 patch 本地缓存（issue / comment / task 这类需要即时更新的），要么触发对应 query 的失效重拉（less-critical 数据）。

### AI / LLM 在哪里

**Multica 本身不直接调 LLM API**。所有 LLM 调用都在 agent CLI 子进程里发生（Claude Code 调 Anthropic API、Codex 调 OpenAI API 等）。

Server 和 daemon 做的事情是：

1. 准备 prompt（见 `server/internal/daemon/prompt.go`）
2. 准备环境变量（agent.custom_env 注入）
3. 准备工作目录（注入 CLAUDE.md / AGENTS.md / skills / issue context）
4. 启动 CLI 子进程
5. 流式读 CLI 的 stdout，把消息分类并转发

**所以看不到大段的 prompt 工程代码**——prompt 只有几个模板（task prompt、chat prompt、comment-triggered prompt），核心内容是 agent instructions + issue context + skill files，真正的 LLM 对话由 CLI 自己管理。

### 后台任务

Server 启动三个 goroutine：

1. **Runtime Sweeper**（每 30s）：标记离线 runtime、回收孤儿任务、GC 长期离线 runtime
2. **Autopilot Scheduler**（每 30s）：扫 cron 触发器，到点就 dispatch
3. **DB Stats Logger**：周期性打印 pgxpool 连接池状态

---

## 5. 产品地图（全部路由）

### 公共 / 认证

- `/` — 首页
- `/login` — 登录
- `/auth/callback` — OAuth 回调
- `/workspaces/new` — 创建工作区
- `/invite/[id]` — 接受邀请
- `/onboarding` — 首次引导

### 工作区内（`/{slug}/...`）

- `/issues` — Issue 列表（board / list 视图）
- `/issues/[id]` — Issue 详情
- `/my-issues` — 我的 issue（三 scope）
- `/projects` — 项目列表
- `/projects/[id]` — 项目详情
- `/autopilots` — Autopilot 列表
- `/autopilots/[id]` — Autopilot 详情
- `/agents` — Agent 列表
- `/runtimes` — Runtime 列表
- `/skills` — Skill 库
- `/inbox` — 收件箱
- `/settings` — 设置（包含多个 tab：profile / appearance / tokens / workspace / members / repos / daemon / updates）

### 桌面端特有（不是路由，是 WindowOverlay）

- **Create workspace overlay**
- **Invite accept overlay**（来自 `multica://invite/{id}` 深链接）
- **Onboarding overlay**（首次或零工作区时）

---

## 6. 跨平台差异：Web vs 桌面

### 共享（绝大部分功能）

所有业务页面（issues / projects / autopilots / agents / runtimes / skills / inbox / settings / chat / login / onboarding）的实际 UI 都在 `packages/views/` 里，web 和桌面共用同一套组件。

### Web 特有

- 地址栏 + 浏览器前进后退
- 服务端渲染（SSR）
- `/login` 的 OAuth 回调处理 localhost 端口（方便 CLI 登录）

### 桌面特有

- **多标签**：每个 workspace 独立标签组，可以拖拽重排
- **WindowOverlay**：邀请接受、创建工作区、onboarding 不走路由，而是原生窗口层
- **Daemon 集成**：设置里能直接重启本机 daemon、看状态
- **本地 daemon runtime 卡片**：在 Runtimes 页面自动显示本机 daemon
- **自动更新**：`Settings → Updates` 检查/下载/安装新版本
- **Immersive mode**：全屏模式，隐藏侧边栏
- **深链接**：`multica://auth/callback?token=...` 和 `multica://invite/{id}`
- **拖动区**：macOS 的红绿灯 + 顶部 48px 拖拽条（`h-12`）用来移动窗口
- **Workspace 单例守护**：`setCurrentWorkspace()` 管理当前活跃工作区的全局身份

### 为什么两端要做差异

Web 有 URL 栏——错误状态（比如"你没有访问这个 workspace 的权限"）作为一个可分享的 URL 页面是有意义的。桌面没有 URL 栏——同样的状态只会把用户困住，所以桌面选择**静默自愈**：把失效的 tab 从 store 里移除即可。这个差异直接影响多个细节：

- Web 有 `NoAccessPage`，桌面没有
- Web 有 `/workspaces/new` 页面，桌面把它做成 overlay
- Web 的 deep link 直接路由，桌面的深链接转 WindowOverlay

---

## 7. 附录：关键数据表速查

共 **28 张表**，覆盖 10 个产品域。以下按域列出最重要的字段，供文案/产品查询"某个功能背后到底存了什么"。

### 身份 / 认证

- `user` — 基础账号（id, email, name, avatar_url）
- `verification_code` — 邮箱验证码（code, expires_at, attempts）
- `personal_access_token` — 用户 API token（token_hash, token_prefix, revoked）

### 工作区 / 成员

- `workspace` — 容器（name, slug, description, context, settings, repos, issue_prefix, issue_counter）
- `member` — 成员身份（role: owner/admin/member）
- `workspace_invitation` — 邀请（invitee_email, status: pending/accepted/declined/expired）

### Agent / Runtime / Skill

- `agent` — Agent 主表（instructions, custom_env, custom_args, mcp_config, runtime_mode, visibility, status）
- `agent_runtime` — 运行时（daemon_id, provider, status: online/offline, last_seen_at）
- `agent_skill` — agent 挂载 skill 的 n-n 关联
- `skill` — 技能主文档（name, description, content）
- `skill_file` — 技能附带文件（path, content）
- `daemon_token` — 守护进程级 token
- `daemon_connection` / `daemon_pairing_session` — 早期设计（弃用中）

### Issue / 协作

- `issue` — 议题（status, priority, assignee_type+assignee_id, creator_type+creator_id, parent_issue_id, project_id, origin_type, origin_id, acceptance_criteria, due_date, position）
- `issue_label` / `issue_to_label` — 标签
- `issue_dependency` — 依赖关系（blocks / blocked_by / related）
- `issue_subscriber` — 订阅者（reason: creator/assignee/commenter/mentioned/manual）
- `issue_reaction` / `comment_reaction` — emoji 反应
- `comment` — 评论（type: comment/status_change/progress_update/system, parent_id for threading）
- `attachment` — 附件

### 任务执行

- `agent_task_queue` — 任务主表（status: queued/dispatched/running/completed/failed/cancelled, context, result, session_id, work_dir, trigger_comment_id, chat_session_id, autopilot_run_id）
- `task_message` — 每次执行的消息流水（seq, type, tool, input, output）
- `task_usage` — Token 用量（input/output/cache_read/cache_write tokens）

### 对话

- `chat_session` — 聊天会话（unread_since, session_id, work_dir）
- `chat_message` — 消息（role: user/assistant）

### 项目与组织

- `project` — 项目（status, priority, lead_type+lead_id, icon）
- `pinned_item` — 侧边栏置顶（item_type, item_id, position）

### 自动化

- `autopilot` — 规则（assignee_id, execution_mode: create_issue/run_only, issue_title_template, concurrency_policy）
- `autopilot_trigger` — 触发器（kind: schedule/webhook/api, cron_expression, timezone, next_run_at, webhook_token）
- `autopilot_run` — 执行记录（status: pending/issue_created/running/skipped/completed/failed）

### 通知与审计

- `inbox_item` — 收件箱条目（recipient_type, type, severity, read, archived）
- `activity_log` — 审计日志（actor_type: member/agent/system, action, details）
- `runtime_usage` — 运行时按日聚合 token 用量（给计费/容量规划用）

---

## 尾声

Multica 的设计可以归结为一句话：**把"人在一个看板上协作"这件事，扩展到了"人 + AI agent 在同一个看板上协作"**。

所有功能都是围绕这个核心展开：
- 为了让 agent 能像人一样被分配任务 → polymorphic actor（`assignee_type`）
- 为了让 agent 能自己开工 → Autopilot
- 为了让 agent 的工作方式能沉淀复用 → Skill
- 为了让 agent 执行在用户控制的环境里 → Runtime + Daemon
- 为了让人不被通知淹没 → Inbox + 自动订阅
- 为了让一次会话有连续性 → Session Resumption

当你读到某段文案、某个 UI 模块、某张表时，请把它放回这个"人 + AI 协作"的坐标系里去理解它的位置。
