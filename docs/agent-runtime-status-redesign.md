# Agent / Runtime 状态系统重设计

> **文档定位**：这是一份完整的设计 + 实施方案。任何一个新加入的工程师 / 设计师 / 产品，看完这份文档应该能：
> - 理解我们要解决的问题、为什么这么解决
> - 知道每个阶段做什么、按什么顺序做、产出什么
> - 在不读代码的前提下能独立讨论方案
>
> 本文档是 [agent-status-design-brief.md](./agent-status-design-brief.md) 和 [agent-status-redesign-plan.md](./agent-status-redesign-plan.md) 的合并升级版，达成共识后会取代它们。

---

## 目录

1. [背景与目标](#一背景与目标)
2. [核心思想](#二核心思想)
3. [状态系统规范](#三状态系统规范)
4. [数据架构](#四数据架构)
5. [跨平台策略](#五跨平台策略)
6. [设计语言](#六设计语言)
7. [实施分阶段](#七实施分阶段)
8. [验收标准](#八验收标准)
9. [边界与不做的事](#九边界与不做的事)
10. [风险与注意事项](#十风险与注意事项)
11. [参考](#十一参考)

---

## 一、背景与目标

### 1.1 Multica 的产品定位提醒

Multica 是 AI-native 的任务管理平台——agent 是和人对等的"同事"。一个工作区里同时有人和 agent 在协作，相互分配任务、评论、订阅。

理解这个定位很重要，因为它直接决定了状态系统的需求：**用户对 agent 的预期跟"对同事的预期"是相同的**——我希望随时知道这个同事现在能不能接活、在不在线、是不是出问题了。

### 1.2 当前的核心问题

**所有界面都在直接展示后端的原始字段，缺少"用户视角"的状态翻译层。**

具体表现（按用户感知严重度排序）：

1. **Agent 列表的 "Idle" 绿点会骗人**——daemon 已经死了，agent 仍然显示 Idle。用户分配任务后没有任何反馈，长时间困惑"为什么 agent 不动"。
2. **Runtime 列表只有一个圆点**——"刚断线 5 分钟"和"3 个月前断线"视觉一模一样，用户判断不出严重程度。
3. **Issue 详情页多个 agent 同时工作时只有 1 个可见**——其他 agent 的卡片埋在下方滚动区，没有总览。
4. **任务失败时只有红色 X**——`agent_error`（agent 自己挂了）和 `runtime_offline`（daemon 离线）处理方式完全不同，但视觉上不区分。
5. **Chat 发消息后只有一个 spinner 转数分钟**——无法区分"排队"、"思考"、"调用工具"、"生成回答"四个阶段。

### 1.3 根本原因

后端字段是**任务调度的内部状态**（`agent.status` = idle/working/blocked/error），不是给用户看的。当前前端把后端字段直接渲染给用户，就把"调度内部视角"暴露成了"用户视角"。

但用户不关心 agent 的内部调度状态，用户关心的是：

- **能不能用？**（在线 / 离线）
- **如果在用，在干什么？**（工作中 / 排队中 / 失败了）
- **如果不能用，为什么？**（daemon 离线 / CLI 没装 / 任务超时）

这些问题的答案，**没有任何一个能从单一字段直接得到**——它们都需要把多个数据源聚合后才能算出来。

### 1.4 目标

**做一套"用户视角"的状态系统**，覆盖三类对象：

- **Agent**：5 态（available / working / pending / failed / offline），跨界面一致
- **Runtime**：4 态（online / recently_lost / offline / about_to_gc）
- **Task**：阶段化（queued / dispatched / thinking / using_tool / generating / completed / failed）

完成这套系统后，下列现有界面会自然变好：

- Agents 列表 / 详情：看到真实可用性
- Runtimes 列表 / 详情：看到机器健康度
- Issue Detail：多 agent 全景 + 失败原因显示
- 跨界面 hover card（issue assignee / autopilot / chat / @mention）：状态一致
- Chat：分阶段进度

---

## 二、核心思想

### 2.1 一句话

**把"用户视角的状态"做成前端的派生量**——后端只暴露真相（任务存在、心跳到达），前端按 UI 需要把这些真相聚合成"用户能理解的状态"。

### 2.2 三个设计原则

#### 原则 1：派生函数住在前端，不污染后端

`agent.status` / `runtime.status` 这些后端字段是 **物理事实**：
- "task X 现在 running"
- "daemon Y 45 秒前发了心跳"

而 "Available / Working / Pending / Failed / Offline" 这些是 **UI 翻译**：
- "FAILED 状态保持 2 分钟" 是设计决策
- "蓝色表示 working" 是视觉决策
- 不同界面可能要不同视角（issue 里看"任务阶段"、列表里看"是否可分配"）

**UI 翻译应该住在前端，跟着设计需求一起迭代。** 把它放进后端，每改一次都要 migration + WS payload 兼容 + 老客户端处理，迭代周期从分钟变周。

#### 原则 2：服务器状态住在 TanStack Query cache，不复制进 Zustand

我们的全局状态有两套：
- **Zustand** 管 client state（UI 选中、筛选器、modal 开关）
- **TanStack Query cache** 管 server state（来自 API 的所有数据）

TQ cache 不是组件级缓存，**它本身就是 server state 的全局状态管理**。跨组件共享、按 key 索引、自动去重。

派生状态是 server data 的纯函数。结果不存——每次组件渲染时按需用 `useMemo` 算一遍。算的成本是几个 filter + some 调用，可忽略。

#### 原则 3：聚合在前端做，但要避免 N+1

派生状态需要 3 份原始数据：
- agents 列表
- runtimes 列表
- 当前活跃任务列表（active tasks）

朴素做法：每个组件 `useAgentTasks(agentId)`——一个 issues 列表 30 个 agent 头像 = 30 次请求。这就是 N+1。

正解：**进工作区时一次性拉"全工作区的活跃任务"**（数据量天然不大，活跃任务永远是少数），存进 TQ cache。所有组件共享这一份缓存，按 agentId 在内存里 filter——零额外请求。

这是把"per-agent 的数据需求"转换成"全工作区的集合数据需求"。集合数据天然只需要 1 次请求。

---

## 三、状态系统规范

### 3.1 Agent 五态

| 状态 | 颜色 | 用户语义 | 出现条件 |
|---|---|---|---|
| **Available** | 🟢 绿 | 在线空闲，可以接活 | runtime 在线 + 没有活跃任务 |
| **Working** | 🔵 蓝 | 正在干活 | runtime 在线 + 至少一个任务在执行 |
| **Pending** | 🟡 黄 | 任务排着但没在跑 | runtime 在线 + 0 个执行中 + ≥1 个排队 |
| **Failed** | 🔴 红 | 最近一次失败 | 最近 2 分钟内有任务失败 |
| **Offline** | ⚫ 灰 | Daemon 离线，不可用 | runtime 离线（包含 CLI 未安装） |

**复合维度**：当 agent 是 Working 但同时有任务排队，主状态保持 Working，旁边带 `+N` 角标（"还有 N 个排队"）。

**派生规则**（按优先级匹配，命中即返回）：

```ts
type AgentPresence = "available" | "working" | "pending" | "failed" | "offline"

function deriveAgentPresence(input: {
  agent: Agent
  runtime: AgentRuntime
  recentTasks: AgentTask[]   // 该 agent 最近 N 个任务
  now: number                // 当前时间戳
}): AgentPresence {
  // 1. Runtime 离线（含 CLI 未安装）→ offline
  if (input.runtime.status === "offline") return "offline"

  // 2. 最近窗口内有 failed task → failed
  const recentFailed = input.recentTasks.find(
    t => t.status === "failed" &&
         (input.now - new Date(t.completed_at).getTime()) < FAILED_WINDOW_MS
  )
  if (recentFailed) return "failed"

  // 3. 有 running task → working
  if (input.recentTasks.some(t => t.status === "running")) return "working"

  // 4. 有 queued/dispatched task → pending
  if (input.recentTasks.some(t => t.status === "queued" || t.status === "dispatched")) {
    return "pending"
  }

  // 5. Otherwise → available
  return "available"
}
```

**复合维度的派生**：

```ts
function deriveAgentPresenceDetail(...): {
  presence: AgentPresence
  runningCount: number
  queuedCount: number
  failureReason?: TaskFailureReason  // 仅 presence === "failed" 时有值
}
```

**待确认常量**：

- `FAILED_WINDOW_MS = 2 * 60 * 1000`（2 分钟）。
  - 短到避免污染（任务失败的红点不会一直黏着）
  - 长到让用户能看见（不会还没看就消失）
  - **未来可能扩展**：2 分钟内强提示（红色动效），之后降级为 tooltip 的"最近失败"摘要——这样既不刺眼，又不丢信息。本期先用固定 2 分钟。

### 3.2 Runtime 四态

| 状态 | 触发条件 | 用户语义 |
|---|---|---|
| **Online** | 最近 45 秒内有心跳 | 健康，能接任务 |
| **Recently Lost** | 离线但 < 5 分钟 | 可能短暂网络抖动 |
| **Offline** | 离线 5 分钟 ~ 7 天 | 长期离线，需要排查 |
| **About to GC** | 离线接近 7 天阈值 | 系统将自动清理 |

```ts
function deriveRuntimeHealth(runtime: AgentRuntime, now: number): RuntimeHealth {
  if (runtime.status === "online") return "online"

  const lastSeen = runtime.last_seen_at
    ? new Date(runtime.last_seen_at).getTime()
    : 0
  const offlineFor = now - lastSeen

  if (offlineFor < 5 * 60 * 1000) return "recently_lost"
  if (offlineFor > 6 * 24 * 3600 * 1000) return "about_to_gc"  // 7d - 1d
  return "offline"
}
```

> **关于 CLI 未安装等"runtime 在线但跑不了"的场景**：归并到 `offline`，tooltip 写明具体原因（"CLI 未安装"、"Daemon 启动中"）。不为此引入第六态——状态空间膨胀代价高，分辨率应该用 tooltip / detail 而不是顶层枚举。

### 3.3 Task 阶段化

用于 Issue Detail 和 Chat 显示当前任务在哪一步：

```ts
type TaskStage =
  | "queued"
  | "dispatched"     // dispatched 但进程没起来
  | "thinking"       // running，最后一条 message 是 thinking
  | "using_tool"     // running，最后一条是 tool_use / tool_result
  | "generating"     // running，最后一条是 text
  | "completed"
  | "failed"
  | "cancelled"
```

派生函数读 task 状态 + 最后一条 message 的 type 即可决定阶段。

### 3.4 失败原因映射

`task.failure_reason` 来自后端 migration 055，5 个值映射到中文标签：

```ts
const FAILURE_REASON_LABEL: Record<TaskFailureReason, string> = {
  agent_error:      "Agent 执行报错",
  timeout:          "执行超时",
  runtime_offline:  "Daemon 离线",
  runtime_recovery: "Daemon 重启回收",
  manual:           "用户取消",
}
```

每种原因要给用户对应的处理建议（具体文案待设计师定）。

> ⚠️ **当前差异**：后端 schema 已有 `failure_reason`（migration 055），但前端 `packages/core/types/agent.ts` 的 `AgentTask` 接口**未暴露此字段**。阶段 0 必须同步：
> - 前端类型补 `failure_reason: TaskFailureReason | null`
> - 检查后端 `ListAgentTasks` / 新增的 `ListActiveTasksByWorkspace` 是否 SELECT 了这个字段

---

## 四、数据架构

### 4.1 数据流

```
                 后端真相
   ┌────────────┬─────────────┬──────────────┐
   │  agents    │  runtimes   │  active_tasks│
   │  (HTTP)    │  (HTTP)     │  (HTTP)      │
   └─────┬──────┴──────┬──────┴───────┬──────┘
         │             │              │
         └─────────────┴──────────────┘
                       ▼
            TanStack Query cache
                  (全局共享)
                       ▼
            派生函数（纯函数）
       ┌───────────────┴───────────────┐
       ▼                               ▼
   AgentPresence                 RuntimeHealth
       │                               │
       ▼                               ▼
   组件渲染（5 态视觉）         组件渲染（4 态视觉）

   ──────────────────────────────────────────────
   
                    实时更新
                  
   后端 WS 事件 ──→ 前端 invalidate query ──→ 重拉 ──→ 派生重算 ──→ UI 更新
   
   桌面端额外：本机 IPC ──→ setQueryData 直接预填 ──→ 亚秒级响应
```

### 4.2 三个 query

```ts
// 进工作区时一次性拉
useQuery(['ws', wsId, 'agents'])         // listAgents
useQuery(['ws', wsId, 'runtimes'])       // listRuntimes
useQuery(['ws', wsId, 'active-tasks'])   // ★ 新增：getActiveTasksForWorkspace
```

**`active-tasks` 是新增 query**——返回当前工作区所有 status ∈ {queued, dispatched, running} 的任务。这份数据天然小（活跃任务不会很多），值得做成"全工作区一次拉"。

> ⚠️ **当前差异**：后端**没有此 endpoint**。现有只有 `listAgentTasks(agentId)`（per-agent）和 `getActiveTasksForIssue(issueId)`（per-issue）。阶段 0 必须新增 `GET /api/workspaces/:slug/active-tasks`——它仍然返回原始 task 列表，不做任何派生，**不违反"零后端聚合"原则**。

#### Runtime → Agents 是 reverse query

后端没有 "runtime 服务的 agent 列表" API。Agent 持有 `runtime_id` 外键，但 Runtime 没有反向关联。

**前端处理**：从已缓存的 `agents` 列表 `filter(a => a.runtime_id === rtId)` 即可。这不算 N+1（agents 列表本来就要拉），是免费的 join。

### 4.3 WS 事件接线表

| WS 事件 | 触发的 invalidate |
|---|---|
| `agent:created` / `agent:archived` / `agent:updated` | `['ws', wsId, 'agents']` |
| `agent:status` | `['ws', wsId, 'agents']`（即使我们不用这个值，缓存 fresh 仍要） |
| `daemon:register` | `['ws', wsId, 'runtimes']` |
| `task:dispatch` / `task:completed` / `task:failed` / `task:cancelled` | `['ws', wsId, 'active-tasks']` |
| `task:progress` | （考虑节流，避免高频任务刷新缓存） |
| `daemon:heartbeat` | **故意忽略** —— 后端发送但前端不订阅，避免每 15 秒过度 refetch。后果：runtime online → recently_lost 切换最坏延迟 75 秒（45s sweeper + 30s 间隔）。设计上接受这个延迟。 |

**关键不变量**：每个派生函数依赖的字段都必须有对应 WS 事件覆盖。任何字段没事件覆盖，状态会卡住。每次新增派生维度都要回头检查这张表。

### 4.4 桌面端 IPC 桥接

桌面端通过 Electron IPC 直接读本机 daemon。这份数据：
- 比 server WS **快 75 秒**（IPC 亚秒，server sweep 最坏 75s）
- 包含 server 看不到的 `starting` / `stopping` / `cli_not_found` 等中间态

**不修改派生函数签名**——桌面端把 IPC 数据用 `setQueryData` 直接写进 runtime cache：

```ts
// 仅桌面端：apps/desktop/src/renderer/src/platform/daemon-ipc-bridge.ts
window.electron.onDaemonStatus((status) => {
  queryClient.setQueryData(
    ['ws', wsId, 'runtimes'],
    (old) => old?.map(rt =>
      rt.id === status.runtimeId ? mergeDaemonStatus(rt, status) : rt
    )
  )
})
```

派生函数完全不知道数据从哪来——它只读 cache。**桌面端自动获得亚秒级体验，零派生函数改动**。

---

## 五、跨平台策略

### 5.1 状态系统是平台无关的

派生函数、5 态/4 态规范、UI 视觉——**两端共享同一套**。设计稿只画一份。

### 5.2 数据源平台相关

| 平台 | Runtime 数据来源 | 状态变化感知延迟 |
|---|---|---|
| Web | server WS / HTTP | 最坏 75 秒 |
| Desktop（本机 daemon） | IPC + server | < 1 秒 |
| Desktop（别人的 daemon） | server WS / HTTP | 跟 Web 一样 |

数据源不同不影响 UI——派生函数读 cache，cache 是"哪边更新就更新"。

### 5.3 操作能力平台相关

| 操作 | Web | Desktop（自己机器） | Desktop（别人机器） |
|---|---|---|---|
| 看状态 | ✅ | ✅ | ✅ |
| 重启 daemon | ❌ | ✅ | ❌ |
| 看 daemon logs | ❌ | ✅ | ❌ |
| 看 CLI 安装详情 | ❌ | ✅ | ❌ |

实现方式：组件按 `isLocalDaemon && isOwner` 条件渲染按钮。**不需要写进派生函数 / 状态系统**，是 UI 局部决策。

### 5.4 Daemon 卡片视觉对齐

桌面端 settings 里的"本机 daemon 卡片"必须跟云端 runtime 列表项视觉一致。同一台机器同一个概念，不能两套设计。

---

## 六、设计语言

### 6.1 直接复用 Skills 界面（PR #1607、#1614、#1618、#1610）

Skills 界面已经在 2026-04 完成重设计，是当前产品的视觉锚点。Agents / Runtimes 直接照搬其规则：

1. **统一页头** `PageHeader`（h-12 + mobile sidebar trigger）
2. **响应式网格列表**：`grid-cols-[minmax(0,1.6fr)_minmax(0,0.8fr)_minmax(0,1.2fr)_minmax(0,6rem)_auto]`
3. **每行三层信息**：主标题 → 描述（line-clamp-1 muted）→ 元数据（xs muted）
4. **关联对象用头像堆栈**：最多 3 + `+N`，size=22 + `ring-2 ring-background` + `-space-x-1.5`
5. **卡片化列表 + 卡片内工具栏**：搜索和 scope tab 在 `CardToolbar`（h-12），不在页面级
6. **创建用多步 dialog**：chooser → 表单，可回退；宽度按方法切换，300ms 过渡
7. **空状态分文案**：图标 + 标题 + 三行说明 + 清晰 CTA
8. **长列表 `useScrollFade`**：上下边缘淡出
9. **头像统一 `ActorAvatar`**：传 `size`，自动支持 agent / 人
10. **权限检查 hook 化**：`useCanEdit...`，UI 提前隐藏/禁用

### 6.2 状态视觉的三个层级

每种派生状态在三个层级要保持一致：

- **Dot**（圆点）：列表项、头像旁，最紧凑
- **Badge**（徽章）：详情页头部、卡片角落，带图标 + 文字
- **Tooltip / Hover Card**：鼠标悬停展开完整信息

**跨界面一致性**：同一个 agent 无论出现在哪（agents 列表 / issue assignee picker / autopilot 编辑 / chat 选择面板 / 评论 @），状态视觉必须完全一致。

---

## 七、实施分阶段

### 阶段 0 — 数据层地基（无 UI 改动）

**目标**：派生函数 + cache + WS + IPC 桥接全部就位。UI 暂不动。完成后所有 UI 阶段都能放心假设"派生状态可用、零额外请求"。

#### 后端工作（Go）

| 文件 | 改动 |
|---|---|
| `server/pkg/db/queries/agent_task.sql` | 新增 sqlc query：`ListActiveTasksByWorkspace`，SELECT 所有字段含 `failure_reason`，过滤 status ∈ {queued, dispatched, running} |
| `server/pkg/db/agent_task.sql.go` | `make sqlc` 自动生成 |
| `server/internal/handler/agent.go`（或新建 task handler） | `ListActiveTasksByWorkspace` handler，权限校验：用户必须是 workspace member |
| `server/cmd/server/router.go` | 注册路由 `GET /api/workspaces/{slug}/active-tasks` |
| **核查**：现有 `ListAgentTasks` query | 确认 SELECT `failure_reason` 字段；如未 SELECT，补上 |

#### 前端类型补全

| 文件 | 改动 |
|---|---|
| `packages/core/types/agent.ts` | `AgentTask` 接口加 `failure_reason: TaskFailureReason \| null`；新增 `TaskFailureReason = "agent_error" \| "timeout" \| "runtime_offline" \| "runtime_recovery" \| "manual"` |

#### 前端 API client

| 文件 | 改动 |
|---|---|
| `packages/core/api/client.ts` | 新增方法 `getActiveTasksForWorkspace(wsSlug): Promise<AgentTask[]>` |

#### 前端派生函数 + 类型

| 文件 | 内容 |
|---|---|
| `packages/core/agents/types.ts`（如不存在则新建） | `AgentPresence` / `AgentPresenceDetail` / `RuntimeHealth` 等类型 |
| `packages/core/agents/derive-presence.ts` | `deriveAgentPresence` / `deriveAgentPresenceDetail` 纯函数 |
| `packages/core/agents/derive-presence.test.ts` | 5 态全分支 + 边界 case（runtime null / tasks 空 / 时钟边界） |
| `packages/core/runtimes/derive-health.ts` | `deriveRuntimeHealth` |
| `packages/core/runtimes/derive-health.test.ts` | 4 态全分支 |

#### 前端 query + hook

| 文件 | 内容 |
|---|---|
| `packages/core/agents/active-tasks-query.ts` | `activeTasksOptions(wsId)` query options |
| `packages/core/agents/use-agent-presence.ts` | `useAgentPresence(agentId)` hook：读 3 份 cache → 派生 |
| `packages/core/runtimes/use-runtime-health.ts` | `useRuntimeHealth(runtimeId)` hook |
| `packages/core/runtimes/use-runtime-agents.ts` | `useRuntimeAgents(runtimeId)` hook：从 agents cache filter 出绑定的 agents |

#### 前端 WS 接线

| 文件 | 改动 |
|---|---|
| `packages/core/realtime/agent-runtime-sync.ts` | 新增专用 sync。订阅 `agent:*` / `task:dispatch` / `task:completed` / `task:failed` / `task:cancelled` / `daemon:register` → invalidate 对应 query。**显式不订阅 `daemon:heartbeat`**（接受 75 秒延迟） |
| `packages/core/realtime/use-realtime-sync.ts`（如已有全局 hook） | 集成新 sync |

#### 桌面端 IPC 桥接

| 文件 | 内容 |
|---|---|
| `apps/desktop/src/renderer/src/platform/daemon-ipc-bridge.ts` | 监听 `window.daemonAPI.onStatus(...)`，用 `queryClient.setQueryData` 把本机 daemon status merge 进对应 runtime cache |

#### 完成标准

- [ ] 后端 `GET /api/workspaces/:slug/active-tasks` 通 curl 测试，返回 active tasks 列表
- [ ] `deriveAgentPresence` / `deriveRuntimeHealth` 单测全部通过
- [ ] 控制台调用 `useActiveTasks(wsId)` 能拿到全工作区活跃任务
- [ ] 控制台调用 `useAgentPresence(agentId)` 能拿到正确的 5 态状态
- [ ] WS 接线表里所有事件都能正确 invalidate（手测覆盖）
- [ ] 关本机 daemon 后桌面端 runtime cache **1 秒内**变 offline
- [ ] 不动任何 UI 文件——这阶段 zero UI delta

### 阶段 1 — Agents + Runtimes 列表页

**目标**：两个列表用上派生状态，互相能看到对方。

#### 设计师产出（先于代码）

- 5 态 dot / badge / tooltip 三层视觉规范
- Working + 排队角标的复合视觉
- Failed 状态的 2 分钟时间窗口动效
- Runtime 4 态的视觉差异（不能再是同一个浅灰圆点）

#### 改造文件

| 文件 | 改动 |
|---|---|
| `packages/views/agents/components/agent-list-item.tsx` | 替换 `statusConfig[agent.status]` 为 `useAgentPresence(agentId)` |
| `packages/views/agents/components/agents-page.tsx` | 接 WS 订阅 |
| `packages/views/agents/config.ts` | 删除 `statusConfig`，新建 `presenceConfig` |
| `packages/views/runtimes/components/runtime-list.tsx` | 用派生 4 态；展示 last_seen、关联 agent 数、当前任务数 |
| `packages/views/runtimes/components/runtimes-page.tsx` | 接 WS 订阅 |
| `apps/desktop/.../local-daemon-card.tsx` | 视觉对齐云端 runtime 卡片 |

### 阶段 2 — Agents + Runtimes 详情页

**目标**：详情页头部、profile card 状态联动；Runtime token usage 信息架构整理。

#### 改造文件

| 文件 | 改动 |
|---|---|
| `packages/views/agents/components/agent-detail.tsx` | 头部 status badge 用派生 |
| `packages/views/agents/components/agent-profile-card.tsx` | 状态行和 runtime 行联动；展示当前任务数 + 最近失败原因 |
| `packages/views/runtimes/components/runtime-detail.tsx` | Token usage 主次重排：核心指标置顶，5 个图表折叠 / 下沉 |
| `packages/views/runtimes/components/usage-section.tsx` | API 调用按时间窗口拉（不再总是 90 天） |

### 阶段 3 — Issue Detail 任务展示

**目标**：多 agent 全景视图；任务阶段化；失败原因显式。

#### 新增文件

| 文件 | 内容 |
|---|---|
| `packages/core/agents/derive-task-stage.ts` | `deriveTaskStage` |
| `packages/core/agents/derive-task-stage.test.ts` | 单测 |
| `packages/views/issues/components/agent-task-row.tsx` | 单 agent 单任务一行 |

#### 改造文件

| 文件 | 改动 |
|---|---|
| `packages/views/issues/components/agent-live-card.tsx` | 从"sticky 一个 + 折叠列表"改为"每个 agent 一行" |
| `packages/views/issues/components/agent-transcript-dialog.tsx` | 失败时展示 failure_reason |

### 阶段 4 — 跨界面 Hover Card

**目标**：所有 agent 头像出现的位置都用统一的 hover card。

#### Hover Card 必须显示的内容（按重要度排序）

1. 派生 5 态状态
2. Runtime 健康（在线性 + last_seen 相对时间）
3. 当前任务（N running / M queued）
4. 最近失败（如果有）：原因 + 时间
5. Agent 名称 + description
6. 关联 skills（前 3 个 + `+N`）
7. Owner

#### 必须接入的位置

| 位置 | 当前状态 |
|---|---|
| Agents 列表 / 详情 | ✅ 已有 |
| Issue Assignee Picker | ❌ 仅头像无状态 |
| Issue Detail 头部 assignee | ❌ 仅头像无状态 |
| Issue 列表 / 看板的分配头像 | ❌ 仅头像 |
| Autopilot 列表 / 编辑（assignee） | ❌ 仅头像 |
| Project lead picker | ❌ 仅头像 |
| Chat 选择 agent 面板 | ❌ 待确认 |
| 评论里的 @agent | ❌ 仅头像 |

#### 实施

`ActorAvatar` 组件挂载 hover card——一处改动，上面所有位置自动获得统一卡片。

**N+1 风险已经被阶段 0 的"全工作区 active-tasks"消除**——hover card 只读 cache，零额外请求。

### 阶段 5 — Chat 状态分阶段（独立 PR）

工作量较大，跟流式渲染相关，单独排期。

#### 改造文件

| 文件 | 改动 |
|---|---|
| `packages/views/chat/components/chat-message-list.tsx` | `AssistantMessage` 用 `deriveTaskStage` 替代单 spinner |
| `packages/views/chat/components/chat-page.tsx` | WS 断线重连后的消息回拉 fallback |

#### 必须解决

- 取代单 spinner，按阶段显示
- Failed task 显示原因
- WS 断线重连后能拉回历史消息

#### 加分项

- Typing indicator（generating 阶段的逐字感）
- 全局任务进度 FAB
- Stop 按钮的明确反馈

---

## 八、验收标准

### 阶段 0

- [ ] `deriveAgentPresence` / `deriveRuntimeHealth` 单测覆盖所有分支 + 边界 case（runtime null / tasks 空 / 时钟边界）
- [ ] 控制台调用 `useActiveTasks(wsId)` 能拿到数据
- [ ] WS 事件接线表里每个事件都能正确 invalidate（手测）
- [ ] 桌面端关本机 daemon 后 runtime cache 在 1s 内变 offline

### 阶段 1

- [ ] Daemon 关闭后，Agent 列表项 75 秒内变成 Offline 灰点
- [ ] Agent 跑任务时，列表项变成 Working 蓝点；排队 N 个时带 `+N` 角标
- [ ] Agent 任务失败后，列表项 2 分钟内显示 Failed 红点 + tooltip 含失败原因，2 分钟后自动恢复
- [ ] Runtime 列表能区分 Online / Recently Lost / Offline / About to GC 四态
- [ ] Runtime 列表行展示关联 agent 数 + 当前任务数
- [ ] 桌面端本机 daemon 卡片视觉跟云端 runtime 列表项一致
- [ ] 全局 grep `agent.status` 在 views 层无引用

### 阶段 2

- [ ] Agent 详情头部状态跟列表一致
- [ ] Profile card 状态行 + runtime 行不再自相矛盾
- [ ] Runtime 详情页能在不展开图表的前提下看到本期成本
- [ ] Token usage API 按选中的时间窗口拉（不再总是 90 天）

### 阶段 3

- [ ] 同一 issue 多 agent 工作时，每个 agent 一行实时状态
- [ ] queued / dispatched / running 三态视觉差异清晰
- [ ] 任务失败时，行内显示中文 failure reason + 处理建议
- [ ] 不支持 live log 的 provider 显式说明"等任务完成后查看结果"

### 阶段 4

- [ ] 所有展示 agent 头像的位置 hover 都能看到完整状态卡
- [ ] 渲染 30+ agent 头像的页面，hover 不触发任何新 HTTP 请求

### 阶段 5

- [ ] Chat 中能看到任务在哪个阶段
- [ ] WS 断线后重连能补齐历史消息
- [ ] Failed task 显示原因

---

## 九、边界与不做的事

明确**不在本次范围内**：

1. **不改后端 schema**——`agent.status` / `blocked` / `error` 字段保留（迁移风险大，无收益）
2. **不让后端做派生**——所有派生在前端做（迭代速度 + UI 解耦）
3. **不新增 WS 事件类型**——只订阅现有但未用的（如 `agent:status`）
4. **不动 `agent_runtime.status` 的 online/offline 二态**——前端从这两态 + `last_seen_at` 派生四态
5. **不重构 Skills 界面**——它已经是参考样板
6. **Phase 5 不做"逐字流式渲染"**——后端 stream-json 是批量推送，typing indicator 是视觉技巧
7. **不做"agent 健康综合评分"**——只暴露原始信号，不算综合分
8. **不为 CLI 未安装等场景引入第六态**——归到 offline 的 tooltip 子分类
9. **不在 Zustand 里存派生结果**——server data 不能复制进 store

---

## 十、风险与注意事项

### 10.1 WS 事件覆盖完整性

派生函数的"实时性"靠的是依赖 query 都被 WS 正确 invalidate。一个事件没接好，状态就会卡住。

**缓解**：阶段 0 的接线表是必须维护的"契约"。每次新增派生维度，回头检查这张表，确保新依赖的字段有事件覆盖。

### 10.2 时钟一致性

Failed 状态依赖客户端时间和 `completed_at` 的差。客户端时钟漂移会让 2 分钟窗口不准。

**缓解**：2 分钟 ± 30 秒不影响判断，可接受。需要时可用 server 在 WS 心跳里携带的时间作为参考。

### 10.3 旧字段引用残留

`statusConfig` 删除后，遗漏的引用会运行时错误。

**缓解**：
- TypeScript 严格模式 + 改类型
- 全局 grep 验证
- 长期防御：在 `packages/core/agents/types.ts` 把 `agent.status` 从外部消费的 Agent 类型里 omit 掉，仅保留在 RawAgent 内部类型里给 API 层用

### 10.4 跨界面状态一致性

不同地方调用同一个 agent 的派生函数，必须结果一致。

**缓解**：所有调用方走 `useAgentPresence(agentId)` 这个唯一 hook，不允许直接调派生函数。Hook 集中管理输入数据收集。

### 10.5 active-tasks 数据量

如果工作区某天有 1000+ 活跃任务，全工作区一次拉的设计会受影响。

**缓解**：
- 当前活跃任务有天然上限（受 `max_concurrent_tasks` × agent 数量约束）
- 监控加上：cache 大小超过阈值时上报
- 必要时再考虑 windowing（最近 N 个）或 server 端聚合

---

## 十一、参考

- [产品全景文档](./product-overview.md) —— Agent / Runtime / Daemon 的产品定位
- [Skills 界面源代码](../packages/views/skills/) —— 设计语言样板
- 关键 PR：#1607（Skills 重设计）、#1614（Card + PageHeader）、#1618（描述恢复）、#1610（Dialog 闪烁修复）
- 后端关键代码：
  - `server/internal/service/task.go` —— `agent.status` 更新逻辑（`ReconcileAgentStatus`）
  - `server/cmd/server/runtime_sweeper.go` —— Runtime 心跳 / sweeper 时间常量
  - `server/migrations/055_task_lease_and_retry.up.sql` —— Task `failure_reason` 五态
  - `server/migrations/037_fix_pending_task_unique_index.up.sql` —— 一个 issue 多 agent 处理的设计依据

---

## 附录 A：当前现状清单（保留作为重设计前的存档）

> 这部分原本在 design-brief.md 里，迁移过来作为重设计前的现状记录。设计师在画稿前可以贴上当前界面截图作为对照。

### A.1 Agent 字段可见性

| 字段 | 当前状态 | 备注 |
|---|---|---|
| `name` / `avatar_url` / `description` | ✅ | 列表 + 详情都展示 |
| `archived_at` | ✅ | 列表项灰显 + 详情头部 banner |
| `status`（idle/working/...） | ✅ 但语义错误 | **要替换成派生 5 态** |
| `runtime_mode`（local/cloud） | ✅ 图标 | 列表项右侧 Cloud / Monitor 图标 |
| `instructions` | ✅ | Instructions tab |
| `custom_env` / `custom_env_redacted` | ✅ | Env tab |
| `custom_args` | ✅ | Custom Args tab |
| `visibility`（workspace / private） | ✅ | Settings tab |
| `max_concurrent_tasks` | ✅ | Settings tab |
| `model` | ✅ | Settings tab |
| `runtime_id` 关联 | ✅ | Settings tab |
| `skills` | ✅ | Skills tab + profile card 前 3 个 |
| `owner_id` | ✅ | Profile card |
| `created_at` / `updated_at` | 🆕 | 后端有，UI 完全没展示 |
| `archived_by` | 🆕 | 后端有，UI 隐藏 |

### A.2 Runtime 字段可见性

| 字段 | 当前状态 | 备注 |
|---|---|---|
| `name` | ✅ | 列表 + 详情头部 |
| `provider` | ✅ Logo | 9 种 |
| `runtime_mode` | ✅ 文字 | `RuntimeModeIcon` 组件存在但从未被调用 |
| `status`（online/offline） | ✅ 圆点 + 徽章 | 离线圆点浅色主题下几乎不可见 |
| `last_seen_at` | ✅ 仅详情页 | **列表完全看不到** |
| `device_info` | ✅ 详情页 | 没有人类可读化 |
| `daemon_id` | ✅ mono 字体 | 不可复制 |
| `metadata.cli_version` | ✅ | CLI 更新部分 |
| `metadata.launched_by` | ✅ | "Managed by Desktop" |
| `owner_id` | ✅ | 头像 + 名字 |
| `created_at` / `updated_at` | ✅ | ISO 时间戳 |

### A.3 桌面端 IPC 数据

```
DaemonStatus (本地 IPC)
├─ state: running / stopped / starting / stopping / installing_cli / cli_not_found
├─ pid, uptime
├─ daemonId, deviceName, serverUrl
├─ agents: 当前运行的 agent IDs
├─ workspaceCount
└─ profile
```

### A.4 截图占位区

设计师拿到这份文档后，请把以下界面的当前截图贴在对应位置：

- **Agents 界面**：列表页 / 详情页（每 tab 一张）/ 创建对话框
- **Runtimes 界面**：列表页 / 详情页（含 5 个图表）/ 桌面端 daemon 卡片
- **Issue Detail**：无任务执行时 / 单 agent 执行中 / 多 agent 并发 / 全屏 transcript dialog

### A.5 Token Usage 5 个图表的数据细节

Runtime 详情页底部 token usage 部分，5 个图表 + 1 张表格全部展开，没有主次。阶段 2 改造时按下表理解每个图表的数据契约：

| 图表 | 数据源 | 时间粒度 | 维度 | 度量 |
|---|---|---|---|---|
| **Activity Heatmap** | `getRuntimeUsage?days=90` | 日 | date | 4 级强度，按 token 总量百分位分级 |
| **Hourly Activity** | `getRuntimeTaskActivity` | 小时（0-23） | hour | 任务数 |
| **Daily Token Chart** | 同 Heatmap，客户端聚合 | 日 | date | input/output/cacheRead/cacheWrite 总和 |
| **Daily Cost Chart** | 同上 + 客户端定价计算 | 日 | date | 美元成本，按 model pricing 表 |
| **Model Distribution** | 同上聚合 by model | 全周期 | model | tokens 占比 + cost |

**当前实现的问题**：API 总是取 90 天数据，客户端做 7d/30d 过滤——浪费服务端资源，首次加载慢。改造时按选中窗口拉。

### A.6 Issue Detail 任务展示的当前实现

主要组件：`packages/views/issues/components/agent-live-card.tsx`

#### 已有能力

- 多 agent 并发执行同一 issue 时，第一个卡片是 sticky（顶部固定），其他在下方滚动
- 每个 task 卡片可展开 timeline，显示 tool_use / tool_result / thinking / text / error 五种消息类型
- 实时滚动：WS 事件 `task:message` 到达时追加 timeline
- 工具调用计数 badge
- Stop 按钮可取消任务
- 全屏 transcript dialog（支持事件类型筛选 + 复制）
- 已完成/失败/取消的任务进入 TaskRunHistory 折叠区

#### 数据来源

| 数据 | API |
|---|---|
| 当前 issue 的活跃 task 列表 | `getActiveTasksForIssue(issueId)` |
| 每个 task 的历史消息 | `listTaskMessages(taskId)` |
| 实时消息流 | WS `task:message` |
| 状态变化 | WS `task:dispatch` / `task:completed` / `task:failed` / `task:cancelled` |
| 取消任务 | `cancelTask(issueId, taskId)` |

阶段 3 在此基础上重构成"每个 agent 一行的全景视图"。
