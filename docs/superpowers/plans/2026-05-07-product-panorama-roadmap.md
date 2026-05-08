# 产品全景路线图实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补齐当前 Multica 实现与目标产品全景之间最关键的缺口，围绕任务管理、个人执行闭环、时间记录、项目协作、Workspace 边界，以及有选择地保留 AI 增强能力来推进。

**Architecture:** 这是一份路线图计划，不是一份单次实现计划。范围横跨多个相互独立的子系统，因此正确做法是按里程碑交付，每个里程碑都产出一个用户可感知的切片，然后再为该切片单独编写详细实现计划并执行。

**Tech Stack:** Vite + React workspace SPA、TanStack Router、Zustand、Go HTTP handlers、PostgreSQL、WebSocket events、CLI/daemon agent runtime。

---

## 范围检查

这个范围过大，不能用一份代码计划一次性覆盖。它至少跨越了 6 个相互独立的产品面：

1. 项目协作深度
2. Workspace 协作模型
3. 全局导航、搜索与命令入口
4. 个人执行闭环（复盘、计划、专注与提醒）
5. 超出个人视角的时间记录汇总
6. 旧版产品全景里存在、但当前产品壳层还没重新接回来的 AI 增强能力

下面每个里程碑，在开始编码之前都应该先有自己独立的实现计划。

---

## 当前状态缺口总结

| 缺口 | 当前证据 | 为什么重要 |
|---|---|---|
| 项目能力已存在，但仍然偏浅 | `apps/workspace/src/features/projects/components/project-board-page.tsx` 只是按 `project_id` 过滤 issue；`server/internal/handler/project.go` 暴露了标题、描述、图标、状态、负责人，但没有进度、时间线、汇总或时间聚合 | 项目已经是容器，但还不是一个强有力的协作驾驶舱 |
| Workspace 已存在，但邀请流程还不是完整产品面 | `apps/workspace/src/features/settings/components/members-tab.tsx` 直接按邮箱加成员；`server/internal/handler/workspace.go` 在 `CreateMember` 里按邮箱自动建 user；当前路由中没有独立的邀请接受面 | Workspace 成员关系在操作层面可用，但还不是一个完整、清晰的协作边界 |
| 搜索还是局部能力，不是产品级能力 | `server/internal/handler/issue.go` 里有 `parseIssueListSearch`，但 `apps/workspace/src/router.tsx` 里没有全局搜索路由或命令入口 | 产品缺少一个跨 issue、project、member 和命令动作的快速统一入口 |
| 个人执行闭环仍未形成 | `apps/workspace/src/router.tsx` 当前只有 `/my-time` 和 `/my-time/calendar`，但没有 nightly review、morning plan 或 `autopilot` 对应的路由与 handler | 这是当前项目围绕个人成长、复盘和降低每日决策成本的核心缺口 |
| 时间记录在个人视角较强，在团队和项目视角较弱 | `apps/workspace/src/router.tsx` 当前只有 `/my-time` 和 `/my-time/calendar`，但没有 workspace 或 project 级时间回顾面 | 时间已经是新产品全景中的核心支柱，但可见性还停留在个人层 |
| 提醒通道仍未完整 | `apps/workspace/src/features/settings/components/notifications-tab.tsx` 已支持 `ntfy` 配置，但 `server/internal/service/email.go` 当前仍主要覆盖验证码发送 | 自动复盘、次日计划和日常执行闭环都依赖更强的提醒能力 |
| 旧版 AI 全景的一些能力在当前产品壳层中仍缺失 | 当前前端功能和路由中没有 `chat` 或 `autopilot`；本轮搜索也没有找到 `chat_session`、`chat_message` 或 `autopilot` 对应的 migration 或 handler | 如果 AI 仍作为增强层存在，这些能力需要被有意识地重新接回，而不是继续悬空 |
| 知识管理仍未定义 | 当前产品总览文档明确推迟了知识管理 | 这是一个战略缺口，但在产品模型被定义清楚前，不适合直接作为构建目标 |

---

## 里程碑路线图

### 里程碑 1：让 Project 成为真正的执行驾驶舱

**Outcome:** 把项目从“元数据 + 项目范围内看板入口”提升为一个真正的协作中心，能直接展示进度和责任归属。

**Why first:** 新的产品叙事已经把项目协作提升为核心支柱，但当前实现还主要停留在分组层。

**Exit criteria:**

- 项目详情页不再只是静态元数据
- 项目页可以展示由 issue 推导出的进度信号
- 项目负责人在 UI 中清晰可见
- 项目页能快速回答“这个项目现在进展到哪里了”

**Likely surfaces:**

- Modify: `apps/workspace/src/features/projects/components/projects-page.tsx`
- Modify: `apps/workspace/src/features/projects/components/project-board-page.tsx`
- Modify: `apps/workspace/src/shared/types/project.ts`
- Modify: `server/internal/handler/project.go`
- Modify: `server/pkg/db/generated/project.sql.go`
- 新增或修改与项目查询、页面行为相关的测试

**Dedicated follow-up plan should cover:**

- 进度模型
- issue 汇总方式
- 项目最近活动
- 负责人和责任归属展示
- 后续与项目时间汇总的兼容性

---

### 里程碑 2：让 Workspace 协作模型真正完整

**Outcome:** Workspace 不再只是一个容器，而会成为具备完整成员生命周期的协作边界。

**Why second:** 新产品全景已经把 Workspace 放回基础层，产品实现也应该匹配这个重要性。

**Exit criteria:**

- 成员生命周期不再隐式发生
- 邀请和访问状态对管理员与成员都清晰可见
- 角色边界明确、行为可预测
- Workspace 设置能够清楚地区分团队级配置与个人级配置

**Likely surfaces:**

- Modify: `apps/workspace/src/features/settings/components/members-tab.tsx`
- Modify: `apps/workspace/src/features/settings/components/settings-page.tsx`
- Modify: `apps/workspace/src/features/workspace/store.ts`
- Modify: `server/internal/handler/workspace.go`
- Possibly add: `server/internal/handler/workspace_invitation.go`
- 如果邀请被建模为显式记录，则可能新增 migration 和 query 文件

**Dedicated follow-up plan should cover:**

- 显式邀请模型
- 待接受成员状态
- 角色切换规则
- owner 转移与防护规则
- workspace 级审计能力

---

### 里程碑 3：补上全局搜索与命令入口层

**Outcome:** 用户获得一个全局、快速的入口，可以跳转到任务、项目、设置和关键动作。

**Why third:** 当核心对象稳定之后，下一个明显瓶颈就是导航效率。当前产品壳层已经有很多路由，但没有统一入口。

**Exit criteria:**

- 用户可以搜索的不止 issue
- 用户可以直接跳转到对象和动作
- 搜索成为默认工作流的一部分，而不是某个页面内的局部过滤器

**Likely surfaces:**

- Modify: `apps/workspace/src/router.tsx`
- Modify: `apps/workspace/src/features/layout/components/app-sidebar.tsx`
- Add: `apps/workspace/src/features/search/`
- 可能新增后端接口，或先复用现有 issue/project 查询，再在前端做聚合层
- 增加搜索结果跳转与命令执行相关测试

**Dedicated follow-up plan should cover:**

- 对象排序策略
- command palette 交互
- 键盘入口
- issue、project、member、settings 的覆盖范围
- 后续扩展到 skills、agents、runtimes 的方式

---

### 里程碑 4：先把时间记录长成个人执行闭环

**Outcome:** 时间记录不再只回答“我做了什么”，还会直接支撑夜间复盘、次日计划和专注支持，成为降低每日决策成本的输入层。

**Why fourth:** 个人时间记录基础已经存在。相比直接跳到团队报表，先把它变成真实可用的个人执行闭环，更符合当前项目围绕个人成长、复盘和效率提升的目标。

**Exit criteria:**

- 夜间复盘报告可以由 AI 自动定时生成草稿
- 次日计划可以由 AI 自动定时生成草稿，并给出 Top 3 / 三只青蛙 / 建议顺序
- 至少保留两个显式确认点：确认复盘草稿、确认次日计划草稿
- 自动运行默认只生成个人草稿，不静默改写共享对象
- 番茄/专注支持开始接入时间记录或 worklog 体系

**Likely surfaces:**

- Modify: `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`
- Modify: `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx`
- Modify: `apps/workspace/src/features/issues/components/workbench-issues-page.tsx`
- Add: `apps/workspace/src/features/autopilot/`
- Modify: `apps/workspace/src/router.tsx`
- Modify: `server/internal/handler/time_entry.go`
- Modify: `server/internal/handler/worklog.go`
- Add: `server/internal/handler/autopilot.go`
- 视提醒落地方式而定，修改通知配置与投递相关接口

**Dedicated follow-up plan should cover:**

- 夜间复盘和次日计划的数据来源
- 个人草稿与共享对象的边界
- 双确认状态机和可回滚规则
- scheduler-first 的触发模型与时区处理
- 番茄/专注支持与 time entry / worklog 的关系
- 邮件、ntfy、或多通道组合的提醒策略

---

### 里程碑 5：再把时间记录扩展到团队与项目回顾

**Outcome:** 时间记录不再只服务个人闭环，还可以回答“团队的时间花在了哪里”以及“项目最近投入是否合理”。

**Why fifth:** 先把个人闭环跑通，再把时间数据往项目和 workspace 视角聚合，能避免报表先行而使用价值不足。

**Exit criteria:**

- 项目页可以展示时间总量或近期投入
- 出现 workspace 级的团队时间回顾面
- 时间数据不只是记录事实，还能解释执行情况

**Likely surfaces:**

- Modify: `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`
- Modify: `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx`
- Modify: `apps/workspace/src/features/projects/components/projects-page.tsx`
- Modify: `server/internal/handler/time_entry.go`
- Modify: `server/internal/handler/worklog.go`
- 新增用于汇总接口的 shared types 与 query hooks

**Dedicated follow-up plan should cover:**

- 项目级时间汇总
- workspace 级时间回顾
- 按成员、日期、项目过滤
- 明确区分操作日志与分析视图

---

### 里程碑 6：把 AI 增强层扩展为可维护的 Automation / Assistant 层

**Outcome:** AI 仍然作为增强层保留，但不再只是一组零散能力，而是演进成可维护的 autopilot 模板、受限规则和助手流程。

**Why sixth:** 在 Daily Autopilot 证明价值之后，再把它抽象成可维护的规则加流水线层，比一开始做泛化 chat 或全功能 workflow builder 更稳妥。

**Exit criteria:**

- autopilot 流程可以作为受限模板维护，而不是每个流程单独硬编码
- scheduler-first 自动化可以在产品中被查看、启停和审计
- limited rule-first 扩展可以覆盖 planning signals、提醒、recurrence 等少量高价值场景
- AI 助手可以支持资料查找、调研总结、任务分发建议等流程，但不绕开现有 issue 模型
- 产品能清楚说明 chat 是否存在，以及它与 autopilot 的边界

**Likely surfaces:**

- Expand: `apps/workspace/src/features/autopilot/`
- Possibly add: `apps/workspace/src/features/chat/`
- Modify: `apps/workspace/src/router.tsx`
- Add: `server/internal/handler/autopilot.go`
- Possibly add: `server/internal/handler/chat.go`
- 视模型而定，新增 flow template / run / confirmation / schedule 相关 schema 与 query 支撑

**Dedicated follow-up plan should cover:**

- 受限流水线模板与全功能 workflow builder 的边界
- Trigger / Schedule / Context / Draft / Confirmation / Apply 的最小流转模型
- autopilot 与现有 agent trigger 的关系
- research / summary / task distribution 这类助手场景的落点
- 可见性、审计、失败重试和人工接管规则
- rule-first 的扩展边界，避免过早演变成通用自动化平台

---

### 里程碑 7：先定义知识管理，再决定是否实现

**Outcome:** 在任何实现开始之前，先为知识管理建立产品模型。

**Why last:** 它在今天是明确未定义的。如果现在直接做，只会制造反复返工。

**Exit criteria:**

- 团队就“知识管理”在本产品里的具体含义达成一致
- 该能力被锚定到明确的对象和工作流
- 在实现计划开始前，先有一份独立 spec

**Likely surfaces:**

- 先做产品 spec，不先做代码
- 后续候选对象可能包括 `skills`、`workspace context`、项目文档，以及 issue 关联笔记

**Dedicated follow-up plan should cover:**

- 对象模型
- 所有权模型
- 检索入口
- 它与 AI 和项目执行之间的关系

---

## 推荐顺序

推荐顺序如下：

1. **项目驾驶舱**
2. **Workspace 协作完善**
3. **全局搜索与命令入口**
4. **个人执行闭环**
5. **团队与项目时间回顾**
6. **Automation / Assistant 扩展**
7. **知识管理定义**

这个顺序能让产品持续围绕新的产品全景推进：

- 先补强共享任务与协作基础
- 再把已有的个人时间记录升级成个人执行闭环
- 然后把时间数据上卷到项目与 workspace 视角
- 再把 AI 扩展为可维护的自动化与助手层
- 最后再定义知识管理

---

## 里程碑交付模型

每个里程碑都应该产出：

1. 一份简短产品 spec
2. 一份独立实现计划
3. 一个可发布、用户可感知的切片
4. 如果行为变化影响产品叙事，则同步更新产品总览文档

不要把这份路线图当成一个大批次来执行。每个里程碑都应该被单独规划、单独实现、单独评审。

