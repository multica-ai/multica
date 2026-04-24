# Multica Docs 站重写规划（v2）

> **本规划是什么**：Multica 对外 doc 站（`apps/docs/`，Fumadocs + Next.js）的从零重写方案。它替换 v1 规划——v1 之前在代码调研之前写的，很多对概念的切分现在看是错的。
>
> **v2 的数据基础**：4 份并行 subagent 的代码级调研，覆盖 Workspace/Members/Issues/Projects、Agent/Runtime/Daemon/Tasks、Skill/MCP/Autopilot/Chat、Inbox/Realtime/Auth 四大领域。每一处涉及产品行为的陈述都能在代码里找到对应位置。
>
> **本文档语言**：中文（团队内部规划，你要逐篇 review）。
> **doc 站本身语言**：英文先行，中文作为 Phase 10 的 i18n。
>
> **风格目标**：排版/布局对标 Anthropic docs（奶油底、serif heading、宽松行距、窄行宽、深色代码块的灵魂），但**色板继续用 Multica 自己的 tokens**（冷蓝 brand）——visual 上是"Multica 色 + Anthropic 排版语法"。

---

## 一、产品定位（文档的落脚点）

Multica = **人 + AI agent 在同一个看板上协作的任务管理平台**。

这个定位决定了它的文档**和普通 SaaS 文档有三个不一样的地方**，贯穿全规划：

1. **术语负担重**。Workspace/Agent/Runtime/Daemon/Skill/Autopilot/MCP/Trigger/Session Resumption——对新用户**没有一个是 obvious** 的。**Concepts 是文档第一支柱**。
2. **分布式执行架构要讲透**。Server 不跑 agent，agent 跑在用户本地的 daemon 上——这是所有"我的 agent 怎么不工作"问题的根源。Architecture Overview 要早出现。
3. **文档也被 agent 读**。现有 `cloud-quickstart.mdx` 已经有"把这段指令给你的 agent，让它自己按文档安装"的模式——意味着文档**要能被 agent 跟着做**：每一步命令要完整、可执行、独立（不能"把上面那个替换一下"）。这直接影响 code block 写法。

---

## 二、读者画像（按优先级排）

| 优先级 | 读者 | 关心什么 |
|---|---|---|
| P0 | **新用户 / Evaluator** | "这是啥？5 分钟跑起来" |
| P0 | **自托管运维 (DevOps)** | "怎么部署？资源要多少？出问题怎么查？" |
| P1 | **团队管理员 / Workspace owner** | "怎么配 agent？管权限？设 autopilot？" |
| P1 | **重度 CLI / 开发者用户** | "CLI 全集？直接调 API？" |
| P2 | **Agent 本身** | "这个命令怎么用？这个概念是什么？" |
| ✗ | OSS 贡献者 | 暂不做 —— 用 `CONTRIBUTING.md` 顶着 |

> **关键**：P2 的 agent 不会逛导航，只会被人类用 `Fetch https://docs.multica.ai/...` 指向某一页。所以每一页都要**自包含**。

---

## 三、设计原则

1. **Concepts First, Tasks Second**。先建立词汇表，再讲操作。
2. **每个概念独立讲透，不合并糊弄**。宁可多一页也不要把 MCP 塞进 Agents 糊弄过去。
3. **「入口」概念独立于「对象」概念**。Trigger 不是 Task 的属性——它是跨入口的共通机制，值得自己一页。
4. **每篇 < 8 分钟读完**。Concept 页可以稍长，Guide/Reference 页聚焦单一主题。
5. **命令块可独立复制运行**——不写"把上面那个改成 XXX"，这对 agent 读者不友好。
6. **版本敏感的事实用代码注释标记来源**——比如"支持的 agent provider"列表，来自哪个文件，后期可以做成自动扫描。

---

## 四、信息架构（v2）

**六大板块，共 56 篇。**

### 板块 1. Introduction（2 篇）

让读者用 2 分钟理解这是什么产品。

| 篇目 | 核心内容 |
|---|---|
| **Welcome** | 定位 + 核心价值 + 一张架构图 + 3 种部署形态（Cloud / Self-Host / Desktop）导航 |
| **How Multica works** | 一张大图把 User / Issue / Agent / Runtime / Daemon / Skill / Task / Trigger 之间的关系串起来——目标是建立**正确心智模型**，而不是记名词 |

### 板块 2. Getting Started（3 篇）

| 篇目 | 核心内容 |
|---|---|
| **Cloud Quickstart** | 5 分钟：signup → install CLI → `multica setup` → 第一个 agent → 第一个 issue |
| **Self-Host Quickstart** | 10 分钟：`install.sh --with-server` → `multica setup self-host` |
| **Your first task** | 从 issue 创建 → assign 给 agent → 看 agent 流式工作 → review 结果（截图 + GIF） |

### 板块 3. Concepts（17 篇 —— 灵魂）

**每页统一模板**：What · Why it exists · How it connects · Related。

| # | 篇目 | 它回答的问题 | 代码事实高光 |
|---|---|---|---|
| 1 | **Workspaces** | 多租户边界；slug / issue prefix / issue_counter 管什么 | slug 正则 `^[a-z0-9]+(?:-[a-z0-9]+)*$`；issue number **per-workspace 自增**；硬删除级联 |
| 2 | **Members & Access** | owner/admin/member 3 级权限；邀请流程；角色约束 | **邀请不存在的邮箱会自动创建 user**（用 email 当名字）；每个 workspace 至少保留 1 个 owner |
| 3 | **Issues** | 最核心工作对象；polymorphic assignee（member 或 agent） | **分配给 agent 会自动入队 task**；private agent 只能被 owner/admin 分配；`acceptance_criteria`/`position`/`first_executed_at` 等字段在代码里**未实装**，不写进文档 |
| 4 | **Projects** | issue 容器；lead 可以是 agent | 非常薄（9 个字段）；删除 project 只是把 issue.project_id 设 NULL |
| 5 | **Agents** | AI 工作者身份；provider/instructions/custom_env/custom_args/model 分别影响什么 | **`custom_env` 在 DB 里明文存储，无加密**——醒目警告；archive 用 `archived_at` 软删除；API 响应对非 owner 做 redact |
| 6 | **Runtimes** | 一台机器 × 一个 provider 一行；注册/在线/离线生命周期 | **唯一约束 (workspace_id, daemon_id, provider)**——同一台机器同一 provider 不会有重复 runtime；daemon 重启复用旧 runtime 行 |
| 7 | **The Daemon** | 分布式执行的灵魂；poll + heartbeat + concurrent execution | 每 30s heartbeat；75s 无心跳 → 离线；启动时调 `recover-orphans` 回收孤儿任务；max_concurrent_tasks 有双层（daemon + agent） |
| 8 | **Tasks** | 任务是什么；生命周期 queued→dispatched→running→completed/failed | **session_id mid-flight pinning**（agent 首条 system message 一到就持久化，不等完成）；失败自动重试只对 issue-sourced 任务（max_attempts=3），chat 和 autopilot 不自动重试 |
| 9 | **Triggers & Entry Points** ← **独立页** | 5 种让 task 产生的入口：Assignment / Comment @mention / Chat / Autopilot / Rerun；每种的行为对比 | 每种的 FK 字段不同（trigger_comment_id / chat_session_id / autopilot_run_id）；**对比表**：哪种有 session resume / 自动重试 / priority 来源 / dedup |
| 10 | **Skills** | 工作区 skill + 本地 skill；按 provider 的注入路径 | 8 种 provider 有不同 skill 根路径（Claude=`.claude/skills/`、Codex=`$CODEX_HOME/skills/`、Pi=`.pi/agent/skills/`、etc）；skill 不参与执行，只参与上下文注入 |
| 11 | **MCP** | 独立协议；怎么给 agent 配 MCP server；和 skill 的区别 | **目前只 Claude Code 真用**——其他 provider 收到 McpConfig 但 CLI 没对应 flag；JSONB 明文存储，非 owner redact |
| 12 | **Autopilots** | 让 agent 自动开工的调度器；两种执行模式；三种触发；并发策略 | **Webhook trigger 字段有但没接路由**——第一版不文档化；concurrency policy 只对 `run_only` 模式生效；`create_issue` 模式由 issue FSM 自然 gate |
| 13 | **Chat** | 和 issue comment 的区别；session 复用 | **完全沙盒**——chat 里的 agent 不能发 comment 到 issue；session_id 用 COALESCE 持久化，agent crash 不会抹掉 |
| 14 | **Inbox** | 个人通知中心；10 种通知类型 | **Agents 可以被加入 subscriber 表但永远收不到 inbox 通知**——`notifyIssueSubscribers` 显式过滤；mention dedup 只在单 event 内生效（一 comment 里 @alice 5 次 = 1 inbox） |
| 15 | **Subscriptions** | 谁会自动订阅；如何手动订阅 | **取消分配后旧 assignee 不会被取消订阅**；parent issue 冒泡只对 `status_changed` 生效 |
| 16 | **Authentication & Tokens** | 3 种凭证 + signup flow + OAuth | JWT cookie（30 天）/ PAT（`mul_` 前缀）/ Daemon Token（`mdt_` 前缀）；Daemon Token **不能命中 user-scoped 路由**；PAT 几乎什么都能命中；signup 白名单优先级：`ALLOWED_EMAILS` > `ALLOWED_EMAIL_DOMAINS` > `ALLOW_SIGNUP` |
| 17 | **Realtime & Events** | WebSocket hub + room model + 事件目录 | **40+ event types**（按命名空间分：issue:* / task:* / inbox:* / chat:* 等）；WS 是 **push-only**（client→server 走 HTTP）；room 按 workspace；inbox:* 用 SendToUser 定向推送 |

### 板块 4. Guides（12 篇，任务导向）

| 篇目 | 核心内容 |
|---|---|
| Assign an issue to an agent | UI + CLI 两种方式 |
| Create and configure an agent | provider、instructions、custom_env、mcp、skills |
| Connect a runtime (local daemon) | daemon install → login → start → 出现在 Runtimes 页 |
| Write and share a skill | 新建 / 编辑 / 挂载到 agent |
| Import a skill from GitHub / ClawHub | import URL 的流程 |
| Import a local skill from your machine | 通过 daemon 扫描本机 skill 目录并上传 |
| Set up an autopilot | 模板起步、schedule / API trigger、run_only vs create_issue |
| Trigger an agent from comments | `@agent` 的规则、防自触发 guard |
| Use the chat interface | 何时用 chat 何时用 issue、session 复用表现 |
| Manage team members and roles | invite、角色升降、remove |
| Configure MCP servers for an agent | JSON 配置示例、常见 MCP server |
| Work from the terminal (CLI-first) | 纯 CLI 完成 create→assign→follow |

### 板块 5. Self-Hosting（8 篇）

| 篇目 | 必讲的 critical warning |
|---|---|
| Overview | 决策树（哪种部署模式适合你） |
| Docker Compose deployment | `make selfhost` vs `make selfhost-build` |
| Environment variables reference | 完整 env 表 |
| Authentication setup | **🚨 `APP_ENV != "production"` 会让 verification code 固定为 `888888`** —— 生产必须设置 `APP_ENV=production`；Google OAuth 配置；signup 白名单 |
| Storage | S3 / CloudFront / 本地磁盘 |
| Email | Resend 配置；**没配会落到 stderr** |
| Upgrading | 版本升级 + migration 策略 |
| Troubleshooting | 常见问题（日志在哪、端口冲突、daemon 连不上、等） |

### 板块 6. CLI Reference（14 篇）

按 command category 组织。每个命令页统一 schema：**Synopsis · Options · Examples · Exit codes · Related**。

Installation / Authentication / Setup / Daemon / Workspace / Issue / Comment / Agent / Skill / Autopilot / Project / Repo / Runtime / Config & Version

---

## 五、代码调研发现的 12 条必写事实

这些都是 product-overview.md **没明确写清楚**、但代码里真实存在、文档里**必须呈现**的事实。每条都标了归属页面。

| # | 事实 | 归属页面 |
|---|---|---|
| 1 | `custom_env` 在 DB 里明文存储，无加密；非 owner redact 仅在 API 响应层做 | Agents |
| 2 | Agent 可被加入 subscriber 表，但永远收不到 inbox 通知 | Subscriptions / Inbox |
| 3 | Session Resumption 只有 Claude Code 真用；Codex 的 session_id 存了不读；其他不支持 | Tasks / Agents |
| 4 | MCP 目前只有 Claude Code 真用——其他 provider 忽略 mcp_config | MCP |
| 5 | Webhook autopilot trigger 字段建了但没接路由——第一版不文档化 | Autopilots |
| 6 | custom_env merge 是覆盖而非合并——不能用 custom_env"取消设置"系统 env | Agents |
| 7 | 旧 assignee 取消分配后不会被取消订阅 | Subscriptions |
| 8 | `APP_ENV != "production"` 时 verification code 恒为 `888888` | Self-Hosting → Auth |
| 9 | Signup 白名单优先级：ALLOWED_EMAILS > ALLOWED_EMAIL_DOMAINS > ALLOW_SIGNUP | Self-Hosting → Auth |
| 10 | One daemon ↔ many runtimes；one runtime ↔ ONE provider；同 daemon_id 重启复用旧 runtime 行 | Runtimes / Daemon |
| 11 | Inbox 10 种类型，mention dedup 只在单 event 内生效 | Inbox |
| 12 | WebSocket 是 push-only；client 写操作走 HTTP；room 按 workspace，inbox:* 用 SendToUser | Realtime & Events |

---

## 六、富内容策略（不单调）

| 组件 | 用途 |
|---|---|
| Mermaid diagram | 架构图 / task 生命周期 / trigger 流向 / autopilot 调度链 |
| Tabs | Cloud / Self-Host / Desktop 并列；CLI / UI 并列 |
| Callouts（内置）| Tip / Warning / Note — **警告类密集用在 Agents 的 custom_env 和 Self-Host 的 888888** |
| Code Tabs | API 调用多语言（Shell / Node / Go） |
| Video / GIF | "Create your first agent"、"Follow an agent working" |
| DeploymentPicker（定制）| 交互式决策树：回答 3 个问题 → 推荐部署路径 |
| ConceptHero（定制）| 每个 Concept 页顶部的大图 + tagline + "also see" |
| CLIBlock（定制）| 终端样式 + copy + 期望输出 |
| APIRoute（定制）| API endpoint 统一渲染 |
| LifecycleDiagram（定制）| 任务状态机 / runtime 在线离线状态机 |
| TriggerComparison（定制）| 5 种 trigger 的对比矩阵——Triggers 页的核心组件 |

---

## 七、技术基础设施

### 7.1 视觉基础（Phase 1）

- `apps/docs/app/global.css` 里 `@import "@multica/ui/styles/tokens.css"`，覆盖 Fumadocs 的 `neutral.css`
- 字体：Heading serif（**Fraunces** 或 **Source Serif 4**，`next/font` 加载）+ Body `--font-sans` + Code `--font-mono`
- 排版：主列 ~720px，段间距 1.2×，h1/h2 serif，代码块深色高对比，链接保留下划线

### 7.2 Dark/Light（已就位）

Fumadocs RootProvider 自动切换；tokens.css 已有 `.dark`，直接可用。

### 7.3 i18n（Phase 10）

Fumadocs 原生支持：`content/docs/[lang]/...`。初期只英文，中文后补。

### 7.4 CI（Phase 0）

当前 `.github/workflows/ci.yml:33` 用 `--filter='!@multica/docs'` 排除了 docs build。**在 Phase 0 做一个独立小 PR 把它加回来**——否则 MDX 语法错 CI 不拦，只有 Vercel 部署时才发现。

### 7.5 dev:docs 快捷命令（已做）

`pnpm dev:docs` 已加到 root `package.json`。

---

## 八、依次开发的 Phase 分期

**约束**：Phase 3 及之后每篇 mdx 是**独立 commit**，你按 commit 一篇一篇 review。

| Phase | 产出 | review 粒度 | 预估 |
|---|---|---|---|
| **Phase 0** | CI 加 docs build；`pnpm dev:docs`（已做） | 1 个 PR | 0.5h |
| **Phase 1** | 视觉基础：tokens、serif 字体、排版规则、light/dark 验证 | 1 个 PR，看整体调性 | 1 天 |
| **Phase 2** | IA 骨架：清空 `content/docs/`，按 v2 IA 建 56 个空 mdx + `meta.json` | 1 个 PR，看导航树 | 0.5 天 |
| **Phase 3** | Introduction 2 篇 | 每篇 1 commit | 1 天 |
| **Phase 4** | Getting Started 3 篇 | 每篇 1 commit | 2 天 |
| **Phase 5** | Concepts 17 篇 | 每篇 1 commit，分 3-4 批推 | 5-7 天 |
| **Phase 6** | Guides 12 篇 | 每篇 1 commit | 3-4 天 |
| **Phase 7** | Self-Hosting 8 篇 | 每篇 1 commit | 2-3 天 |
| **Phase 8** | CLI Reference 14 篇 | 每篇 1 commit | 3-4 天 |
| **Phase 9** | 富内容组件（Mermaid / 定制组件） | 按组件分 commit | 2 天（可穿插） |
| **Phase 10** | i18n 中文 | 每篇 1 commit | 3-5 天（可延后） |

**总计约 55 篇 mdx + 基础设施**，按上述节奏单人 3-4 周可完成英文版。

---

## 九、本规划**不做**的

- OSS 贡献者文档（用 `CONTRIBUTING.md` 顶着）
- API Reference 独立板块（CLI 覆盖 95% 场景，第一版不做）
- 版本化文档（`/v0.2/`、`/v0.3/`）
- Blog / Changelog UI（Changelog 先外链 `CHANGELOG.md`）
- 自动从代码生成 API reference
- 语义搜索 / 向量搜索（产品本身还没用 pgvector）
- Webhook autopilot trigger（代码未接路由）

---

## 十、立即的下一步

本规划你确认后：

1. 开分支 `docs/rewrite-v1`
2. 执行 **Phase 0**（CI + 已做的 dev:docs，做成独立小 PR）
3. 执行 **Phase 1**（视觉基础）——独立 PR，你启动 dev server 看调性
4. 执行 **Phase 2**（IA 骨架）——独立 PR，你看导航
5. Phase 3 开始 **每篇一个 commit 依次推**

你按顺序 review，中间可随时 course correct。
