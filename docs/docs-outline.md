# Multica Docs 执行大纲

> 这份是**执行文档 + 协作 tracker**。每篇文档都有独立条目，委派出去的人直接在对应条目里认领、更新状态。
>
> 战略思路（产品定位、读者画像、设计原则、视觉方向）保留在 [`docs-rewrite-plan.md`](./docs-rewrite-plan.md)。
>
> **语言**：只写中文版。英文版暂不做。
> **V1 目标**：25 篇覆盖所有核心功能。v2 留 24 篇深度/边缘内容 pending。

---

## 一、协作规则（接手任何一篇前必读）

### 1.1 写作守则（硬约束）

1. **源码优先**：每一条事实陈述必须能在源码里找到对应位置。不能从"产品宣传册"、"直觉"、"上一版本的文档"或"记忆"出发。
2. **代码里没有的功能一律不写**。即使 UI 疑似有、DB 有字段、handler 有接口但 service 层无真实读写逻辑，都视为"未实装"。遇到边界不确定的情况，标 ⚠️ 让 reviewer 再看一眼，不要硬写。
3. **下笔前先读源码验证本文件里的"写什么"清单**。这个清单是指引，不是真相。可能已过时、可能当时调研就不准。
4. **跨篇共通事实集中写**（例如 10 provider 矩阵写在 §4.3 Providers Matrix，其他篇 cross-link 过去），避免同一事实分散在多篇里。
5. **服务于产品定位**：Multica 的核心差异化是 **"BYO-agent 的 Linear—— agent 跑在你自己的机器，你掌控计算和 provider 选择"**。每篇的语气和深度都应该为这个叙事服务。
6. **为目标读者写**。不同读者期待不同深度。P0 新用户不关心 SQL 字段，P1 开发者愿意看架构图，P2 agent 读者需要命令自包含能复制。
7. **v1 认领优先**：先把 v1 的 25 篇 ship 出去，再开 v2。

### 1.2 目标读者分级

| 级别 | 读者 | 期待 |
|---|---|---|
| P0 | 新用户 / Evaluator | "这是啥？5 分钟跑起来" |
| P0 | 自托管运维 | "怎么部署？出问题怎么查？" |
| P1 | 团队管理员 / Workspace owner | "怎么配 agent？管权限？设 routines？" |
| P1 | 重度 CLI / 开发者用户 | "CLI 全集？架构细节？" |
| P2 | Agent 本身（被人类指向某页）| "每步命令要完整、可独立复制执行" |

### 1.3 状态码

- ⬜ Not started
- 🔍 Source research（正在读源码验证）
- ✍️ Drafting（正在写初稿）
- 👀 In review（待 review）
- ✅ Shipped

### 1.4 Flag（只在需要决策时填）

- 🤔 **Propose merge/drop** —— 认领后读源码发现这篇独立成页价值低，写一行理由，@ owner（Naiyuan）决策。

### 1.5 分工流程

1. **认领**：把 `Owner` 改成你的名字，`Status` 改成 🔍
2. **读源码**：用本条目的 "Source files" 作为起点，扩展看相关代码
3. **验证"写什么"清单**：发现过时/缺失/错误，直接改这个条目
4. **写初稿**：`Status` 改成 ✍️
5. **提 review**：`Status` 改成 👀，发消息 @ reviewer
6. **交付**：`Status` 改成 ✅

### 1.6 每篇目标字数

| 页面类型 | 字数范围 | 理由 |
|---|---|---|
| Concept 页（§2-§6 多数）| 800-1500 字 | 讲清一个概念 + 示例 + 关联 |
| Quickstart / Tutorial（§1.3-§1.4 / §2.1）| 500-1200 字 | 命令优先，解释从简 |
| Reference 页（§4.3 / §7-§8 多数）| 1000-2500 字 | 对照表 / env 清单 / 命令 cheatsheet，信息密度高 |
| Overview / Welcome（§1.1）| 300-600 字 | 定位 + 导航，不展开 |

**原则**：写到目标字数上限还没写完，说明该拆页或该压缩；写到下限还不够，说明内容薄，考虑合并。

### 1.7 Review Checklist（提 review 前自查）

- [ ] 每条事实陈述都能在 Source files 里找到对应代码位置
- [ ] 所有代码示例（shell / CLI）可以独立复制、独立运行（不依赖"把上面那个替换一下"）
- [ ] 术语和其他页一致：workspace / agent / runtime / daemon / task / skill / routine
- [ ] 所有 cross-link 指向的目标页存在（不是死链）
- [ ] 字数在目标范围内
- [ ] 本条目"不写"清单里的字段没被写进去
- [ ] 没有从记忆或旧文档复制过来的未验证事实

### 1.8 MDX 样例模板

> 这是 §2.3 Issues 的 mdx skeleton，用来统一标题层级 / callout / code block / cross-link 风格。所有 v1 文档按这个骨架写。

```mdx
---
title: Issues
description: Issue 是 Multica 的核心工作对象——人和 agent 都能被分配、评论、改状态。
---

# Issues

## 什么是 Issue

Issue 是 Multica 的核心工作对象……（1-2 段话说清楚"是什么"）

## 关键概念

### Polymorphic Assignee

Issue 的 assignee 可以是 member（人）或 agent。这是 Multica 和传统 task manager
最重要的区别——**agent 是 first-class assignee**。

<Callout type="info">
分配给 agent 会**立即**入队一个 task。详见 [Tasks](/docs/tasks)。
</Callout>

### 状态（Status）

| Status | 含义 | 默认 |
|---|---|---|
| backlog | 还没规划 | ✓ |
| todo | 准备开工 | |
| in_progress | 正在做 | |
| ... | ... | |

**注意**：状态转换无强制约束——任意状态可以直接互转。

## 操作

### 创建 Issue（CLI）

```bash
multica issue create \
  --title "Fix login bug" \
  --assignee @alice
```

### 分配给 Agent（触发自动执行）

```bash
multica issue assign <issue-id> --agent <agent-slug>
```

分配给 agent 时，Multica 会立刻入队一个 task 到对应 runtime。详见
[Assigning Issues to Agents](/docs/assign-agents)。

## 删除和级联

删除 issue 会级联删除：
- 所有评论 / reactions（硬删除）
- 该 issue 上 queued / dispatched 的 task（取消）
- 附件（异步清理 S3）

## Related

- [Comments](/docs/comments)
- [Projects](/docs/projects) —— Issue 的容器
- [Assigning Issues to Agents](/docs/assign-agents)
- [Tasks](/docs/tasks)
```

**关键约定**：

- **Callout**：`<Callout type="info|warning|tip">...</Callout>`。warning 用于陷阱（如 888888），info 用于补充说明，tip 用于最佳实践
- **代码块**：shell 命令用 \`\`\`bash；配置用 \`\`\`yaml / \`\`\`env；JSON 用 \`\`\`json
- **Cross-link**：用 markdown 链接 `[显示文字](/docs/page-slug)`，不要写成 "详见 Tasks 章节"
- **表格**：有 3 行以上对照才用表格，不要 1-2 行也用
- **标题层级**：H1 只能一个（等于页面 title），H2 是主要分段，H3 是小节

---

## 二、版本规划

### V1（25 篇，第一次 ship）

覆盖所有核心功能，让新用户能"5 分钟懂产品 + 10 分钟跑起来 + 30 分钟用上 agent"。

### V2（24 篇 Pending）

深度 reference / 开发者向 / 高级部署。等 v1 ship + 用户反馈后再补。

### V1 篇目清单

| 板块 | V1 篇数 | V2 推迟篇数 |
|---|---|---|
| 1. Welcome & Quickstart | 4 | 0 |
| 2. Workspace & Team | 4 | 0 |
| 3. Agents | 3 | 1（MCP）|
| 4. How Agents Run | 3 | 0 |
| 5. Working with Agents | 4 | 0 |
| 6. Staying Informed | 1 | 2（Subscriptions 合并 / Realtime）|
| 7. Administration | 3 | 5（Self-Host Overview / Docker Compose / Storage / Email / Upgrading / Signup Controls）|
| 8. Reference | 3 | 13（CLI 各子命令详细页）|
| **合计** | **25** | **24** |

---

## 三、大纲概览（v1 导航）

| # | 板块 | 定位 | 篇数 |
|---|---|---|---|
| 1 | [Welcome & Quickstart](#板块-1welcome--quickstart) | 这是什么 + 5 分钟跑起来 | 4 |
| 2 | [Workspace & Team](#板块-2workspace--team) | 人能理解的部分（Linear-like） | 4 |
| 3 | [Agents](#板块-3agents) | 引入 agent 这个新物种 | 3 |
| 4 | [How Agents Run](#板块-4how-agents-run) | 执行架构（daemon / runtime / task / providers） | 3 |
| 5 | [Working with Agents](#板块-5working-with-agents) | **4 种触发方式——产品核心特色** | 4 |
| 6 | [Staying Informed](#板块-6staying-informed) | Inbox + Subscriptions | 1 |
| 7 | [Administration](#板块-7administration) | Env / Auth Setup / Troubleshooting | 3 |
| 8 | [Reference](#板块-8reference) | CLI / Tokens / Desktop | 3 |

---

## 板块 1：Welcome & Quickstart

### 1.1 Welcome — 👀 In review [v1]

- **Source files**: `README.md`, `docs/docs-rewrite-plan.md`（定位段）, `apps/docs/content/docs/index.mdx`（现状）
- **目标读者**: P0 新用户 / evaluator（第一次听说 Multica）
- **叙事位置**: 第一页。定义整个产品。读完应该能回答"这是啥"。
- **Punch line（推荐）**: **"Your agents, your machine, your backlog."**
  > The task manager where AI teammates run on your own laptop.
- **写什么**（300-600 字）:
  - Punch line + 副标题
  - 三段展开：
    1. Agent 是 first-class（能被分配 / 评论 / 改状态 / 作为 project lead）
    2. Agent 跑在你自己的 daemon 上——你掌控计算和 API key
    3. Provider-agnostic：支持 Claude Code / Codex / Cursor CLI / Copilot 等 10 种
  - 一句借势："Speaks MCP natively. Compatible with Anthropic Agent Skills."
  - 3 种部署形态导航（Cloud / Self-Host / Desktop）
- **不写**:
  - 不用 "AI-native"（已贬值）
  - 不用 "autonomous"（撞 Autopilot 大军）
  - 不暗示对标 Devin（分类不同）
  - 架构细节（下一页）
- **写前要验证**: 产品定位文案和团队当前 positioning 是否一致
- **⚠️ 动笔前必读**:
  - 不要写"human + AI agent first-class"——Linear 2026 CEO 已宣布 "issue tracking is dead"，这叙事不再独特
  - 真正独特点：**本地 daemon + BYO provider**（SaaS 结构性做不到）
  - 不超过 600 字。这是 landing page，不是说明书
- **Owner**: Claude
- **Flag**: –
- **交付位置**: `apps/docs/content/docs/index.mdx`（v1 暂平铺，未迁 `zh/`）

### 1.2 How Multica Works — ⬜ Not started [v1]

- **Source files**: `server/cmd/multica-daemon/`, `packages/core/`, 战略 plan 的"产品定位"段
- **目标读者**: P0 新用户（想先建立心智模型再动手）
- **叙事位置**: 第二页。一张大图把 User / Issue / Agent / Runtime / Daemon / Task / Trigger 串起来。
- **写什么**（800-1200 字 + 一张架构图）:
  - 一张架构图（Mermaid）：server ↔ 你的 daemon ↔ 你的 provider CLI
  - 三段话解释：
    1. Server 维护 workspace / issue / agent 元数据
    2. Daemon 跑在你的机器上，poll server 领任务、调本地 provider CLI、汇报结果
    3. 4 种触发方式让 agent 开工（导到 §5 各篇）
- **不写**: API 细节、具体 provider 差异
- **写前要验证**: 架构图反映最新代码（有没有新组件）
- **⚠️ 动笔前必读**:
  - 这页灵魂是**架构图**。图画不好等于没写
  - 图里每个 box 点进去能到后续哪一篇（cross-link 要全）
- **Owner**: –

### 1.3 Quickstart (Cloud) — ⬜ Not started [v1]

- **Source files**: `apps/docs/content/docs/cloud-quickstart.mdx`（现有）, `server/cmd/multica/cmd_setup.go`, `cmd_login.go`, `cmd_agent.go`, `cmd_issue.go`
- **目标读者**: P0 新用户（想 5 分钟跑起来）
- **叙事位置**: 第三页。
- **写什么**（600-1000 字）:
  - Signup → install CLI → `multica login` → `multica setup cloud` → 创建第一个 agent → 创建第一个 issue → 分配给 agent → 看它工作
  - 每步命令可独立复制运行
  - 末尾一句："如果你想用 desktop app，参见 [Desktop App](/docs/desktop-app)"
- **不写**: self-host（下一篇）、daemon 深入配置
- **写前要验证**: `cmd_setup.go` 真实 flow；各命令最新 flag
- **⚠️ 动笔前必读**:
  - 时间承诺：5 分钟。如果步骤超过 5 分钟，减步骤或老实写更长
  - 每条命令要可复制运行
- **Owner**: –

### 1.4 Quickstart (Self-Host) — ⬜ Not started [v1]

- **Source files**: `Makefile`（selfhost target）, `docker-compose.selfhost.yml`, `.env.example`, `server/cmd/multica/cmd_setup.go`
- **目标读者**: P0 self-host 评估者
- **叙事位置**: Cloud Quickstart 的姐妹篇。
- **写什么**（800-1200 字）:
  - `make selfhost` vs `make selfhost-build` 差异
  - 自动生成 JWT_SECRET
  - Migration 启动自动执行（**zero-touch upgrade** 是卖点）
  - 第一次启动后 `multica setup self-host`
  - 最小可行配置（必填 env）
  - **⚠️ 提醒 `APP_ENV=production` 的陷阱**（详细讲在 §7.2）
- **不写**: 完整 env 表（§7.1）、Storage/Email 进阶配置（v2）
- **写前要验证**: `selfhost` vs `selfhost-build` 实际差异
- **⚠️ 动笔前必读**:
  - 目标 10 分钟跑起来。超时就砍步骤
  - 不和 §7 Administration 写重复：这里是 happy path，§7 是 reference / 排错
- **Owner**: –

---

## 板块 2：Workspace & Team

> **板块叙事**：先讲"人的世界"——和 Linear 基本同构的部分。让读者在熟悉的土壤上建立心智，为板块 3 引入 agent 做铺垫。

### 2.1 Workspaces — ⬜ Not started [v1]

- **Source files**: `server/internal/handler/workspace.go`, `server/pkg/db/queries/workspace.sql`, `server/migrations/001/006/020`, `server/internal/validation/workspace.go`（slug + reserved 列表）
- **目标读者**: P0 新用户、P1 团队管理员
- **叙事位置**: 板块 2 第一篇。定义"你在哪工作"。
- **写什么**（800-1200 字）:
  - Workspace = 多租户边界（所有查询按 workspace_id 过滤）
  - Slug 约束（正则 `^[a-z0-9]+(?:-[a-z0-9]+)*$` + reserved 列表）
  - Issue prefix（2-5 大写字符）+ issue counter per-workspace 自增
  - Workspace context 字段（给 agent 读的 workspace-level 上下文）
  - 硬删除级联
- **不写**: 创建 workspace 的 desktop/web UI 细节
- **写前要验证**:
  - Slug 正则现值
  - Reserved slug 完整列表
  - Context 字段是不是真的被 agent 读（确认用途）
- **⚠️ 动笔前必读**:
  - Reserved slug 列表给代码引用链接，不手写（代码会演进）
  - Context 字段如果用途不明就标 ⚠️ 问清楚再写
- **Owner**: –

### 2.2 Members & Roles — ⬜ Not started [v1]

- **Source files**: `server/internal/handler/invitation.go`, `workspace.go`（member 部分）, `server/pkg/db/queries/invitation.sql`, `server/migrations/041`
- **目标读者**: P1 团队管理员
- **叙事位置**: Workspace 之后。
- **写什么**（1000-1500 字）:
  - 三级权限矩阵（owner / admin / member）—— 用表格
  - **邀请双路径**：`CreateInvitation` vs `CreateMember`
  - 邮箱自动创建（邀请不存在邮箱时）
  - **至少保留 1 owner 约束**
  - 角色提升约束（非 owner 不能邀请为 owner）
  - 7 天邀请有效期
- **不写**: 邮件模板内容、OAuth（§7.2）
- **写前要验证**: 权限矩阵每行对应 handler；邀请有效期；邮件失败行为
- **⚠️ 动笔前必读**: 权限矩阵表 + 邀请流程图是必需的
- **Owner**: –

### 2.3 Issues & Projects — ⬜ Not started [v1]

> **合并说明**：Project 在代码里非常薄（9 字段），合并进 Issues 一页讲。用"Project 作为容器"一节处理。

- **Source files**:
  - Issues: `server/internal/handler/issue.go`, `server/pkg/db/queries/issue.sql`, migrations 001/015/017/018/020/050
  - Projects: `server/internal/handler/project.go`, `server/pkg/db/queries/project.sql`
- **目标读者**: P0 新用户、P1 团队管理员
- **叙事位置**: 核心工作对象。
- **写什么**（1500-2000 字）:
  - **Issues 部分**:
    - Polymorphic assignee（member/agent/null）——第一次正式提"可以分配给 agent"
    - Status 枚举（backlog/todo/in_progress/in_review/done/blocked/cancelled），**无强制 FSM**
    - Priority / Label / Subscription / Reaction / Dependency / Bulk 操作
    - Issue number per-workspace 自增
    - Comment reply 树、@mention
    - 删除级联
  - **Projects 部分**（末尾一节）:
    - 9 字段、polymorphic lead（member/agent）
    - Issue 关联（project_id 可 NULL）
    - 删除 project 不删 issue（只把 project_id → NULL）
- **不写**（源码未实装）:
  - `acceptance_criteria` JSONB（无读写）
  - `context_refs` JSONB（无读写）
- **写前要验证**:
  - Status / Priority 枚举真实值
  - Label color 格式
  - `position` 和 `first_executed_at` 确实在用（⚠️ 旧 plan 误说未实装）
- **⚠️ 动笔前必读**:
  - 字数在 1500-2000 之间；超出就拆 Projects 回独立页
  - 第一次提"agent 可以是 assignee"，**不要展开 agent 机制**（板块 3 讲）
- **Owner**: –

### 2.4 Comments — ⬜ Not started [v1]

- **Source files**: `server/internal/handler/comment.go`, `server/pkg/db/queries/comment.sql`, migrations 017/018, `server/cmd/server/notification_listeners.go`（mention 解析）
- **目标读者**: P0 新用户、P1 重度用户
- **叙事位置**: 板块 2 最后一篇。用 @agent 为板块 5 铺垫。
- **写什么**（800-1200 字）:
  - Comment 创建 / 编辑 / 删除（作者 = member 或 agent）
  - Reply 树（parent_id，CASCADE）
  - @mention member 或 agent
  - **⚠️ @all 展开到全 workspace member**（误用会炸 inbox）
  - Emoji reaction
  - Mention dedup：**单 comment 内生效**
- **不写**: @agent 触发 task 的机制（§5.2 讲）
- **写前要验证**: mention dedup 作用域
- **⚠️ 动笔前必读**:
  - 板块 2 到板块 5 的桥梁，末尾预告"评论里 @agent 能触发任务，详见 [Mentioning Agents](/docs/mentioning-agents)"
  - @all 警告必须醒目
- **Owner**: –

---

## 板块 3：Agents

> **板块叙事**：新物种登场。读者已经理解 workspace/issue/project/comment 之后，正式引入 agent。重点：agent 是 first-class 团队成员。

### 3.1 What is an Agent — ⬜ Not started [v1]

- **Source files**: `server/internal/handler/agent.go`, `server/pkg/db/queries/agent.sql`, migrations 001, `server/cmd/server/notification_listeners.go`（"agent 不收 inbox" 过滤）
- **目标读者**: P0 新用户（第一次接触 agent 概念）
- **叙事位置**: 板块 3 首篇。用"agent 像人又不像人"建立心智。
- **写什么**（1000-1500 字）:
  - Agent 作为 first-class 成员（可分配 issue / 评论 / 改状态 / project lead）
  - 和 human 相似点 vs 差异点（对照表）:
    - 相似：出现在 assignee / commenter / subscriber
    - 差异：绑 provider、需要 runtime 在线、**永远不收 inbox**、有 visibility（workspace / private）、可 archive
- **不写**: provider 细节（§3.2）、skill/MCP（§3.3）、daemon 机制（§4.1）
- **写前要验证**: "agent 不收 inbox" 的过滤实现位置；visibility 默认值
- **⚠️ 动笔前必读**:
  - 这页的灵魂是"和人对比"——用两列对照表最清晰
  - 末尾预告：绑 provider 在 §3.2、挂 skill 在 §3.3、真的跑起来在板块 4
- **Owner**: –

### 3.2 Creating & Configuring Agents — ⬜ Not started [v1]

- **Source files**: `server/internal/handler/agent.go`, `server/pkg/db/queries/agent.sql`, `packages/views/agents/`, `server/cmd/multica/cmd_agent.go`
- **目标读者**: P1 团队管理员
- **叙事位置**: 怎么创建一个 agent。
- **写什么**（1200-1800 字）:
  - Provider 选择——列主流 5 个（Claude Code / Codex / Cursor / Copilot / Gemini）+ 提示"完整对比见 [Providers Matrix](/docs/providers)"
  - Model（静态 vs 动态发现）
  - Instructions（系统提示词）
  - **`custom_env`**：⚠️ DB 明文存储，非 owner redact；**覆盖而非合并**
  - `custom_args`：pass-through
  - `visibility`（workspace / private）
  - `max_concurrent_tasks`（默认 1）
  - Archive / restore
- **不写**: skill（§3.3）、MCP（v2 推，在 §3.3 末尾提一句）、provider 完整 matrix（§4.3）
- **写前要验证**: custom_env 明文存储；合并策略；max_concurrent_tasks 默认
- **⚠️ 动笔前必读**:
  - ⚠️ `custom_env` 明文存储必须用 warning block，否则用户会把生产 token 扔进去
  - Provider matrix 只列 5 个主流，完整表 cross-link 到 §4.3
- **Owner**: –

### 3.3 Skills — ⬜ Not started [v1]

- **Source files**: `server/internal/handler/skill.go`, `server/pkg/db/queries/skill.sql`, `server/internal/daemon/execenv/context.go`（各 provider skill 注入路径）, `server/cmd/multica/cmd_skill.go`
- **目标读者**: P1 重度用户、P2 agent
- **叙事位置**: 强化 agent 能力。
- **写什么**（1200-1800 字）:
  - **开篇借 Anthropic 比喻**：
    > "Skill 是 agent 的'员工专业知识'——程序性知识模块；MCP 是 agent 的'工具通道'——外部系统连通性。" （引自 Anthropic 官方博客）
  - **兼容性宣示**：
    > "Multica Skill 采用 [Anthropic Agent Skills 开放标准](https://agentskills.io) 的 `SKILL.md` 格式。所有符合该规范的 skill（包括 Anthropic 官方仓库、ClawHub、skills.sh 上发布的包）都可以直接导入使用。"
  - Skill 文件结构（SKILL.md + config + 任意支持文件）
  - 来源：workspace skill（云端）vs local skill（daemon 扫描本机）
  - 导入：新建 / GitHub / ClawHub / 本机目录
  - 挂载到 agent（junction table `agent_skill`）
  - **10 provider 注入路径矩阵**（或 cross-link §4.3）
  - Skill 在 task dispatch 时同步
  - **⚠️ ClawHavoc 警示**：2026-2 曝过 "ClawHavoc" 恶意包事件。ClawHub 已集成 VirusTotal 扫描，但安装第三方 skill 前务必检查 SKILL.md 和附带脚本。
  - **末尾一段"Skills vs MCP"**（v2 再开 MCP 独立页）:
    > MCP（Model Context Protocol）是另一层概念——让 agent 连外部工具（数据库、文件系统、第三方 API）。Multica 支持 `mcp_config` 字段，但目前**仅 Claude Code 真实消费**，其他 provider 接收但未传递。详见 v2 的 MCP 专页（开发中）。
- **不写**: skill 内部 DSL（不存在）、MCP 深入（v2）
- **写前要验证**:
  - 10 provider 路径是否还都对（execenv/context.go 最新值）
  - Skill 大小限制（1 MB/file？）
  - path traversal 检查
- **⚠️ 动笔前必读**:
  - 开篇必须用 Anthropic 比喻 + agentskills.io 兼容声明（借势生态最大化）
  - ClawHavoc 警示是必写——不警示用户可能装到恶意包
- **Owner**: –

---

## 板块 4：How Agents Run

> **板块叙事**：agent 怎么真的动起来——分布式执行、用户自己跑 daemon。这是 Multica 结构上区别于 Linear/Jira 的关键部分。

### 4.1 Daemon & Runtimes — ⬜ Not started [v1]

> **合并说明**：Daemon 和 Runtime 概念耦合紧密（runtime = daemon × provider），放一页讲更连贯。

- **Source files**:
  - Daemon: `server/internal/daemon/daemon.go`, `server/cmd/multica-daemon/main.go`, `server/cmd/multica/cmd_daemon.go`, `server/pkg/db/queries/daemon.sql`
  - Runtime: `server/pkg/db/queries/runtime.sql`, `server/internal/handler/runtime.go`, `server/migrations/004`, `server/cmd/server/runtime_sweeper.go`
- **目标读者**: P0 运维、P1 开发者
- **叙事位置**: 板块 4 第一篇。"为什么我的 agent 不工作" 的答疑总枢。
- **写什么**（1500-2000 字）:
  - **Daemon 部分**:
    - Daemon = 本地 worker，poll + 执行 + 汇报
    - **Heartbeat 15s** / **45s offline**（⚠️ 旧 plan 写错过，必须代码核实）
    - Poll 频率 3s
    - max_concurrent_tasks（daemon 20 + agent 1，双层 gate）
    - Recover-orphans（启动时把 dispatched/running 转 failed）
    - Legacy daemon_id migration（hostname → UUID 自动迁移）
    - 配置优先级（CLI flag > config file > env）
    - CLI：`multica daemon install/login/start/stop/status/logs`
  - **Runtime 部分**:
    - Runtime = daemon × provider
    - 唯一约束 `(workspace_id, daemon_id, provider)`
    - 自动注册 + 重启复用
    - Sweeper 每 30s 扫描，7 天 offline 自动删除
- **不写**: provider 执行细节（§4.3）、task 状态机（§4.2）
- **写前要验证**:
  - ⚠️ Heartbeat 是 15s 不是 30s
  - ⚠️ Offline 阈值是 45s 不是 75s
  - Sweeper 间隔 + 自动删除阈值
- **⚠️ 动笔前必读**:
  - 旧 plan 的 heartbeat / offline 数字是错的，认领者必须代码级核实
  - 这页是 support 减压神器，写好能避免大量"agent 不工作"咨询
  - Runtime 概念反直觉，建议用图：一个 daemon 可以有多个 runtime（每 provider 一个）
- **Owner**: –

### 4.2 Tasks — ⬜ Not started [v1]

> **合并说明**：原 §5.5 Rerun 合并进来作为最后一节。

- **Source files**: `server/internal/service/task.go`, `server/pkg/db/queries/task.sql`, `server/internal/handler/task.go`, `task_lifecycle.go`, `runtime_sweeper.go`（timeout）
- **目标读者**: P0 新用户、P1 开发者
- **叙事位置**: runtime 之后。一个 agent 的"一次工作" = 一个 task。
- **写什么**（1500-2200 字）:
  - 状态机（queued → dispatched → running → completed/failed/cancelled）
  - `session_id` mid-flight pinning
  - `attempt` / `max_attempts`（默认 2）
  - **Retryable reasons**：runtime_offline / runtime_recovery / timeout
  - **Non-retryable**：agent_error / 手动失败
  - **自动重试仅对 issue-sourced 和 chat-sourced**——**Routines 任务不自动重试**
  - **Dispatch timeout 5min / Running timeout 2.5h**
  - Priority 来源（issue priority / chat 硬编码 2 / routine priority）
  - Per-issue serialization
  - **Rerun**（最后一节）:
    - 入口：UI / CLI
    - 行为：cancel 当前 active 任务 → 新 task，继承 session_id，attempt 重置为 1
    - vs auto-retry（系统自动）的区别
- **不写**: 触发入口详情（§5 每篇讲一种）、provider session resume（§4.3）
- **写前要验证**: max_attempts 默认；timeout 数值；retryable reasons 清单
- **⚠️ 动笔前必读**:
  - 用状态机图（Mermaid）
  - ⚠️ Routines 任务不自动重试、chat priority 硬编码 = 2，这两条容易漏
  - Retryable vs non-retryable 用表格
- **Owner**: –

### 4.3 Providers Matrix — ⬜ Not started [v1]

- **Source files**: `server/pkg/agent/*.go`（10 个 provider 文件）, `server/internal/daemon/execenv/context.go`（skill 路径）
- **目标读者**: P1 重度用户（选 provider）
- **叙事位置**: 板块 4 最后一篇。10 provider 能力大表。
- **写什么**（1500-2500 字）:
  - **分组列出**（不按字母序）:
    - **新手首选**：Claude Code（feature-complete）/ Codex（主流替代）
    - **商业主流**：Cursor / Copilot / Gemini
    - **ACP 生态**：Hermes（Nous Research）/ Kimi（Moonshot AI）
    - **开源替代**：OpenCode（SST）/ Pi（minimalist）/ OpenClaw
  - **大对照表**:
    | Provider | 厂商 | Session Resume | MCP | Skill 注入路径 | custom_args | 备注 |
  - 每个 provider 一小段（80-150 字）：核心定位 + 用户画像 + 官网链接 + Multica 兼容性
  - **Session resume 精确现状**:
    - ✅ 真用：Claude / Hermes / Kimi / OpenCode / Copilot
    - ⚠️ Codex：代码有 thread/resume 但 unreachable（future feature）
    - ❌ 不支持：Pi / Gemini / OpenClaw
    - ❓ 未审：Cursor
- **不写**: provider 官方使用文档（外链）、MCP 协议本身
- **写前要验证**:
  - 认领者**必须逐个打开 `server/pkg/agent/*.go`** 确认
  - Session resume 实现细节（flag vs thread API）
  - 新 provider 加入 / 旧 provider 删除
- **⚠️ 动笔前必读**:
  - ⚠️ 这是最容易过时的一页，provider 代码频繁变动
  - 精确到 "代码里这个 flag 传给这个 CLI" 级别，不模糊说"支持"
  - Codex "unreachable" 状态必须明确（不是承诺）
- **Owner**: –

---

## 板块 5：Working with Agents

> **板块叙事**：这是产品最有特色的部分。4 种触发方式对应不同协作场景。
>
> **板块开头必须加 intro**：4 种方式的对比表，让读者一页看懂再点进去细节。
>
> | 方式 | 何时用 | 是否自动重试 | Session 复用 | Priority 来源 |
> |---|---|---|---|---|
> | [Assignment](/docs/assign-agents) | 最常见；分配 issue | ✓ | ✓ | issue priority |
> | [Mention](/docs/mentioning-agents) | "帮我看下这条" | ✓ | ✓ | issue priority |
> | [Chat](/docs/chat) | 独立对话，不绑 issue | ✗ | ✓ | hardcoded=2 |
> | [Routines](/docs/routines) | 定时 / 自动触发 | ✗ | ✓ | routine priority |

### 5.1 Assigning Issues to Agents — ⬜ Not started [v1]

- **Source files**: `server/internal/handler/issue.go`（UpdateIssue assign）, `server/internal/service/task.go`（`EnqueueTaskForIssue`）
- **目标读者**: P0 新用户
- **叙事位置**: 最常见触发方式。
- **写什么**（600-1000 字）:
  - UI：issue 详情页选 agent as assignee
  - CLI：`multica issue assign <id> --agent <agent-slug>`
  - 分配后立刻入队 task
  - Private agent 仅 owner/admin 可分配
  - 取消分配：**不自动取消订阅**
  - Per-issue serialization
- **不写**: task 内部机制（§4.2）、subscription（§6.1）
- **写前要验证**: `EnqueueTaskForIssue` 最新逻辑
- **⚠️ 动笔前必读**: 最常见触发——尽可能简洁；旧 assignee 不自动取消订阅要说明
- **Owner**: –

### 5.2 Mentioning Agents in Comments — ⬜ Not started [v1]

- **Source files**: `server/cmd/server/notification_listeners.go`（mention 解析）, `server/internal/service/task.go`（`EnqueueTaskForMention`）, `subscriber_listeners.go`（防自触发）
- **目标读者**: P0 新用户、P1 重度用户
- **叙事位置**: 第二种触发。"这个 agent 帮我看一下"。
- **写什么**（800-1200 字）:
  - 在 comment 里 `@agent-slug`
  - 触发：`EnqueueTaskForMention`，带 `trigger_comment_id`
  - **Dedup 按 agent**（不同 agent 可以被并行 @）
  - **防自触发 guard**（`HasAgentCommentedSince`）
  - 和 assignment 的区别：不改 assignee、不改 status、task 带 trigger_comment_id
- **不写**: inbox mention 通知（§6.1）、@all（§2.4）
- **写前要验证**: 防自触发 guard 条件；dedup 作用域
- **⚠️ 动笔前必读**: 用真实场景开头（"你想让 X agent 分析一下这条 issue"）
- **Owner**: –

### 5.3 Chat — ⬜ Not started [v1]

- **Source files**: `server/internal/handler/chat.go`, `server/pkg/db/queries/chat.sql`, `server/internal/service/task.go`（`EnqueueChatTask`）, `packages/views/chat/`
- **目标读者**: P1 重度用户
- **叙事位置**: 第三种触发。"直接和 agent 对话，不绑 issue"。
- **写什么**（1000-1500 字）:
  - Chat session = agent × user × workspace 独立对话
  - 发消息 → `EnqueueChatTask`（无 issue_id）
  - Session 复用（session_id + work_dir COALESCE 持久化）
  - **⚠️ 完全沙盒**：chat 里的 agent **不能发 comment 到 issue**
  - Priority 硬编码 = 2，**不自动重试**
  - Session 软删除（`status='archived'`）
- **不写**: provider 层 session 机制（§4.3）
- **写前要验证**: chat vs issue comment 隔离性；unread_since 用途
- **⚠️ 动笔前必读**:
  - "沙盒"是 chat 最重要的产品语义，不说清用户会误以为 chat agent 能动 issue
  - 和 §5.2 mention 的区别要对比清楚
- **Owner**: –

### 5.4 Routines — ⬜ Not started [v1]

> **改名说明**：原 Autopilot 改名 Routines。理由：GitHub Copilot 2026-04 已推 "Autopilot mode"（语义是"自主度"），两者正面撞且语义不同。Routines 更贴切（= standing orders / 定期指令）。

- **Source files**: `server/internal/handler/autopilot.go`, `server/pkg/db/queries/autopilot.sql`, `server/internal/service/autopilot.go`, `service/task.go`（`CreateAutopilotTask`）
- **目标读者**: P1 管理员
- **叙事位置**: 第四种触发。"让 agent 自己定期开工"。
- **写什么**（1200-1800 字）:
  - **开篇用类比**:
    > "Routine 就是给 agent 的 **standing order**——像给 human teammate 设一个'每周一早上做 standup summary'的长期指令。"
  - Routine = agent × schedule/trigger × 执行模式
  - **两种模式**（用用户心智词，不说"run_only"）:
    - **Quietly run in the background**（对应代码 `run_only`）：fire-and-forget，不留 issue 痕迹。适合静默维护、数据抓取
    - **Create a tracked issue first**（对应代码 `create_issue`）：先建 issue 再跑，留可追溯 audit trail。适合周期 audit、每周工作报告
  - **Trigger**:
    - `schedule`（cron + timezone）
    - `api`（手动 POST 触发）
    - ~~webhook~~（字段存在但**未接入路由**，不写）
  - Concurrency policy 只对 "Quietly run" 模式生效
  - **⚠️ Routine 任务不自动重试**
  - Run 历史查看
- **不写**: webhook trigger（未接入）；Label 字段（用途不明，先验证）
- **写前要验证**:
  - Webhook 是否还未接入
  - Label 字段用途
  - Cron 格式兼容性
- **⚠️ 动笔前必读**:
  - ⚠️ 全文 **用 Routines，不用 Autopilot**（避免和 GitHub Copilot Autopilot 混淆）
  - 数据库/代码里还叫 `autopilot_*` —— 文档里对读者说 Routines，但引用代码位置可以括号说明"代码表名 autopilot"
  - 两种模式用用户心智词，不要直接暴露 `run_only` / `create_issue`
- **Owner**: –

---

## 板块 6：Staying Informed

### 6.1 Inbox & Subscriptions — ⬜ Not started [v1]

> **合并说明**：Subscription 是 Inbox 通知的前置规则，放一页讲逻辑更顺。

- **Source files**:
  - Inbox: `server/cmd/server/notification_listeners.go`, `server/pkg/db/queries/inbox.sql`, `packages/core/inbox/`, `packages/views/inbox/`
  - Subscriptions: `server/cmd/server/subscriber_listeners.go`, `server/pkg/db/queries/issue_subscriber.sql`, migrations 015
- **目标读者**: P0 所有用户、P1 重度用户
- **叙事位置**: 板块 6 唯一一篇。"agent 在背后干活，怎么知道发生了什么"。
- **写什么**（1500-2000 字）:
  - **Inbox 部分**:
    - **10 种实际触发的通知**:
      1-3. issue_assigned / unassigned / assignee_changed
      4. status_changed（**唯一冒泡到 parent issue**）
      5-6. priority_changed / due_date_changed
      7-8. new_comment / mentioned
      9. reaction_added（issue + comment）
      10. task_failed
    - **⚠️ @all 展开到全 workspace member**
    - Mention dedup 单 comment 内
    - **Agent 永远收不到 inbox**（即使在 subscriber 表）
    - 操作：查看 / 已读 / 批量已读 / 归档 / 过滤
  - **Subscriptions 部分**:
    - 自动订阅规则（creator / assignee / commenter / @mentioned）
    - 手动订阅 / 取消
    - **Parent 冒泡只对 status_changed**
    - **取消分配不自动取消订阅**
- **不写**:
  - 4 种已定义但无触发逻辑的通知（review_requested / task_completed / agent_blocked / agent_completed）
  - WebSocket event 清单（v2 Realtime 页）
- **写前要验证**:
  - 通知类型清单最新值
  - mention dedup 作用域
  - @all 展开时机
- **⚠️ 动笔前必读**:
  - 旧 plan 说 "10 种通知"，代码有 14 个定义——**只讲 10 个实际触发的**，4 个 planned 可以脚注
  - "agent 不收 inbox" 和 §3.1 呼应
- **Owner**: –

---

## 板块 7：Administration

> **板块叙事**：给 self-host 运维 + 开发者。语气 reference 向，不讲故事。
>
> **V1 砍掉**：Self-Host Overview（合并进 §1.4）/ Docker Compose 深入（简化到 §1.4）/ Storage / Email / Upgrading / Signup Controls（合并进 §7.2）/ Authentication & Tokens（拆到 §8.2）

### 7.1 Environment Variables — ⬜ Not started [v1]

- **Source files**: `.env.example`, `server/internal/config/`
- **目标读者**: self-host 运维
- **叙事位置**: self-host 部署的 reference 页。
- **写什么**（1500-2500 字）:
  - 按类别分组：
    - **必填**：DATABASE_URL / PORT / JWT_SECRET / APP_ENV / FRONTEND_ORIGIN
    - **Email**：RESEND_API_KEY（未配置→code 落 stderr）
    - **OAuth**：GOOGLE_CLIENT_ID / _SECRET / _REDIRECT_URI
    - **Storage**：S3_BUCKET / CloudFront 等（默认本地 `./data/uploads`）
    - **Signup 控制**：ALLOW_SIGNUP / ALLOWED_EMAIL_DOMAINS / ALLOWED_EMAILS（**三级优先级**）
  - 每个变量：默认值 / 来源 / 何时必填
- **不写**: Storage / Email 深入配置（v2）
- **写前要验证**: `.env.example` 里的变量穷尽吗（可能有 code-level 但没进 example 的）
- **⚠️ 动笔前必读**:
  - reference 页，完整性第一
  - Signup 三级优先级 EMAILS > DOMAINS > ALLOW_SIGNUP 必须说清
- **Owner**: –

### 7.2 Authentication Setup — ⬜ Not started [v1]

> **合并说明**：原 7.3 Auth Setup + 7.10 Signup Controls 合并。

- **Source files**: `server/internal/handler/auth.go`（APP_ENV 判断 + checkSignupAllowed）, `.env.example`（auth 相关注释）
- **目标读者**: self-host 运维
- **叙事位置**: self-host 的 auth 配置。
- **写什么**（1500-2000 字）:
  - **🚨 超醒目 warning block**：`APP_ENV=production` 必须设置，否则 verification code 恒为 `888888`（任何人登录任何账号）
  - Email + verification code 登录流程（依赖 Resend）
  - Google OAuth 配置步骤（创建 OAuth client → redirect URI → 填 env）
  - **Signup 白名单三层优先级决策树**:
    1. ALLOWED_EMAILS 命中 → allow
    2. ALLOWED_EMAIL_DOMAINS 命中 → allow
    3. ALLOW_SIGNUP=true → allow；false → deny
  - 典型场景：开放给公司域 / 限定几个邮箱 / 完全关闭 signup
  - 和邀请的关系（signup 关了也能通过邀请加人）
- **不写**: JWT 实现、token 类型（§8.2 讲）
- **写前要验证**: APP_ENV 判断条件；OAuth 流程最新；Signup 优先级
- **⚠️ 动笔前必读**:
  - ⚠️⚠️ **888888 陷阱必须最醒目**（红色 warning block），这是 self-host 最大坑
  - OAuth 给外部步骤截图，别假设读者懂 GCP Console
  - 决策树建议用 Mermaid 图
- **Owner**: –

### 7.3 Troubleshooting — ⬜ Not started [v1]

- **Source files**: 各 handler error / daemon log / server log
- **目标读者**: self-host 运维 + 所有遇到问题的用户
- **叙事位置**: 板块 7 最后一篇。
- **写什么**（1200-2000 字，v1 先覆盖 Top 6-8 问题）:
  - Daemon 连不上 server（token 过期 / network / server 挂）
  - 任务一直 queued（runtime offline / max_concurrent 满 / agent 配错）
  - WebSocket 连不上（cookie / CORS / proxy）
  - Email 没收到（Resend 未配置 → 看 stderr）
  - 验证码收到是 888888 但不工作（APP_ENV 检查）
  - Port 冲突
  - 日志位置：daemon / server / browser console
- **不写**: 深度 bug report（去 GitHub issue）
- **写前要验证**: Top 问题列表反映真实 support 记录
- **⚠️ 动笔前必读**:
  - 每个问题：症状 → 可能原因 → 怎么查 → 怎么修（四段式）
  - 这页应不断 append，v1 先写最高频的 6-8 个
- **Owner**: –

---

## 板块 8：Reference

### 8.1 CLI Cheatsheet — ⬜ Not started [v1]

> **合并说明**：v1 不做 14 页 CLI 详细 reference，用一页 cheatsheet 覆盖核心命令 + 认证入口。V2 再按命令组拆详细页。

- **Source files**: `server/cmd/multica/main.go`（命令树）, 各 `cmd_*.go`
- **目标读者**: P1 开发者、P2 agent
- **叙事位置**: Reference 板块首篇。
- **写什么**（2000-2500 字）:
  - **认证入口**（开头一段）:
    - `multica login` → 拿 PAT（`mul_` 前缀）
    - PAT 存 `~/.multica/config.json`
    - 详细 token 机制见 [Authentication & Tokens](/docs/auth-tokens)
  - **命令总览**（按功能分组，每条一行）:
    - **Auth**：`login / auth status / auth logout`
    - **Setup**：`setup cloud / setup self-host`
    - **Workspace**：`workspace list / get / members`
    - **Issue**：`issue list / get / create / update / assign / status / search / runs / rerun` + 嵌套 `issue comment` / `issue subscriber`
    - **Project**：`project list / get / create / update / delete / status`
    - **Agent**：`agent list / get / create / update / archive / restore / tasks` + 嵌套 `agent skills`
    - **Skill**：`skill list / get / create / update / delete / import` + 嵌套 `skill files`
    - **Autopilot**（命令名保留，文档里叫 Routines）：`autopilot list / get / create / update / delete / runs` + 嵌套 `autopilot trigger`
    - **Repo**：`repo checkout`
    - **Daemon**：`daemon install / login / start / stop / status / logs`
    - **Runtime**：`runtime list / usage / activity / ping / update`
    - **Misc**：`config / version / update / attachment download`
  - 每条命令：1 行描述 + 最常用 flag
  - 末尾指引："完整 flag / exit code / examples 见 v2 详细 CLI reference（开发中）"
- **不写**: 每条命令的深入 reference（v2）、shell completion
- **写前要验证**: 命令总数；每条最常用 flag
- **⚠️ 动笔前必读**:
  - 不做 14 页详细 reference，cheatsheet 级足够
  - CLI 叫 `autopilot` 但用户文档里说 Routines，加一行说明"CLI 子命令名为 `autopilot`（将在后续版本统一）"
- **Owner**: –

### 8.2 Authentication & Tokens — ⬜ Not started [v1]

- **Source files**: `server/internal/handler/auth.go`, `server/internal/middleware/auth.go`, `server/internal/middleware/daemon_auth.go`, `server/cmd/multica/cmd_auth.go`
- **目标读者**: P1 管理员、P1 开发者（用 API / CLI / daemon）
- **叙事位置**: Reference 板块。讲"三种身份证"。
- **写什么**（1200-1800 字）:
  - **3 种 token**:
    - **JWT Cookie**（`multica_auth`，HttpOnly，30 天）—— 浏览器
    - **PAT**（`mul_` 前缀）—— CLI / 脚本
    - **Daemon Token**（`mdt_` 前缀）—— daemon 专用
  - **Token 适用矩阵**:
    | 路由 | JWT | PAT | Daemon Token |
    |---|---|---|---|
    | `/api/user/*` | ✓ | ✓ | ✗ |
    | `/api/workspaces/:id/*` | ✓ | ✓ | ✗ |
    | `/api/daemon/*` | ✗ | ✓ | ✓ |
    | `WS /ws` | ✓（cookie）| ✓（首条消息）| - |
  - 登录 flow（email + code / OAuth）
  - PAT 创建 / 撤销 / 管理（UI 在 Settings，CLI 通过 `multica login`）
  - Daemon token 生成时机（`multica daemon login`）
  - Logout（删本地 token，不撤销 server session）
- **不写**: self-host 时的 auth setup（§7.2）、CLI 具体命令（§8.1）
- **写前要验证**: Daemon Token 在 WS 的行为；JWT 过期后重连
- **⚠️ 动笔前必读**:
  - Token 矩阵是灵魂——一张表解决
  - Daemon Token 不能命中 user-scoped 路由必须明确
- **Owner**: –

### 8.3 Desktop App — ⬜ Not started [v1]

- **Source files**: `apps/desktop/src/main/`, `apps/desktop/src/renderer/src/stores/tab-store.ts`, `stores/window-overlay-store.ts`, `apps/desktop/src/main/updater.ts`, `scripts/package.mjs`
- **目标读者**: 使用 desktop 版的用户
- **叙事位置**: Reference 最后一篇。桌面版独有能力。
- **写什么**（1000-1500 字）:
  - **Desktop vs Web 对比表**（开篇）
  - **多 tab 系统**（per-workspace 隔离，localStorage 持久化，跨 workspace 切换时恢复上次活跃 tab）
  - **自动更新**（electron-updater + GitHub Release；Windows arm64 特殊处理 `latest-arm64.yml`；app quit 时安装）
  - **Daemon 不内置**：desktop 只是窗口，daemon 要单独 `multica daemon start`，desktop package 里 bundle 了 CLI
  - 安装：macOS .dmg / Windows .exe / Linux .AppImage
- **不写**:
  - Window Overlay（实现细节，用户无感知）
  - Electron 框架本身
- **写前要验证**:
  - Tab system 持久化机制
  - 自动更新平台矩阵
  - CLI 确实 bundle 进 desktop 包
- **⚠️ 动笔前必读**:
  - 重点是"为什么选 desktop 而不是 web"
  - Desktop vs Web 对比表是核心
- **Owner**: –

---

## 四、V2 Pending 清单（24 篇，v1 ship 后再动）

> 这些篇不在 v1 scope，但位置已规划。等 v1 ship + 有用户反馈后再开写。

- **板块 3**: 3.4 MCP（独立页，深入 MCP 协议 + 各 provider 支持矩阵）
- **板块 6**: 6.2 Realtime（WebSocket event 完整清单、push-only 模型、41 event types、reconnection）
- **板块 7**（self-host 深度）:
  - 7.4 Self-Hosting Overview（决策树：Cloud / Self-Host / Hybrid）
  - 7.5 Docker Compose 深入部署（镜像 / 端口 / 数据卷 / 生产参数）
  - 7.6 Storage（S3 / CloudFront / 本地 disk 三模式对比）
  - 7.7 Email（Resend 配置 / 邮件场景 / 未配置 fallback）
  - 7.8 Upgrading（版本 tag / migration 自动执行 / 回滚策略）
- **板块 8**（CLI 详细 reference，从 8.1 cheatsheet 拆开）:
  - 8.4 multica auth / login 详细
  - 8.5 multica setup 详细
  - 8.6 multica workspace 详细
  - 8.7 multica issue（+comment / subscriber）详细
  - 8.8 multica project 详细
  - 8.9 multica agent（+skills）详细
  - 8.10 multica skill 详细
  - 8.11 multica autopilot 详细
  - 8.12 multica repo 详细
  - 8.13 multica daemon 详细
  - 8.14 multica runtime 详细
  - 8.15 multica config / version / update / attachment 详细

---

## 五、进度汇总（v1 25 篇）

| 板块 | 总数 | ✅ | 👀 | ✍️ | 🔍 | ⬜ |
|---|---|---|---|---|---|---|
| 1. Welcome & Quickstart | 4 | 0 | 1 | 0 | 0 | 3 |
| 2. Workspace & Team | 4 | 0 | 0 | 0 | 0 | 4 |
| 3. Agents | 3 | 0 | 0 | 0 | 0 | 3 |
| 4. How Agents Run | 3 | 0 | 0 | 0 | 0 | 3 |
| 5. Working with Agents | 4 | 0 | 0 | 0 | 0 | 4 |
| 6. Staying Informed | 1 | 0 | 0 | 0 | 0 | 1 |
| 7. Administration | 3 | 0 | 0 | 0 | 0 | 3 |
| 8. Reference | 3 | 0 | 0 | 0 | 0 | 3 |
| **V1 合计** | **25** | **0** | **1** | **0** | **0** | **24** |

---

## 六、决策点记录（append-only）

- **2026-04-23** 初始大纲：49 篇，8 板块（v2 版）
- **2026-04-23** 生态调研后修正：
  - Autopilot → **Routines**（避免撞 GitHub Copilot Autopilot mode）
  - Welcome positioning 换成 **"Your agents, your machine, your backlog."**（不打 "human + agent first-class"，Linear 自己已是）
  - Skill 开篇加 Anthropic 比喻 + agentskills.io 兼容声明 + ClawHavoc 警示
  - MCP 推 v2（目前仅 Claude Code 真用，v1 在 §3.3 末尾提一句）
- **2026-04-23** v1 scope 收敛到 25 篇：
  - 合并：Projects → Issues / Runtime → Daemon / Rerun → Tasks / Subscriptions → Inbox / Signup → Auth Setup / CLI 14 页 → Cheatsheet
  - 推 v2：MCP / Realtime / Storage / Email / Upgrading / Self-Host Overview / Docker Compose 深入 / CLI 详细 reference
  - 保留独立成页：所有用户高频感知的功能（Chat / Routines / Inbox / Skills / Providers Matrix / Desktop App / Self-Host Quickstart）
- （后续更新 append 到此处）
