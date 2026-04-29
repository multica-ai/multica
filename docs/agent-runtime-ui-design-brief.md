# Agents & Runtimes 界面重设计 · 设计师 Brief

> **文档定位**：这份文档专门给负责重设计 Agents 和 Runtimes 界面的 UI/UX 设计师。
>
> 看完这份文档，你应该能：
> - 理解我们在解决什么问题、目标体感是什么
> - 知道每个界面有哪些数据可用、哪些是新展示的、哪些需要工程补
> - 知道有哪些现有交互不能动（避免破坏用户已建立的习惯）
> - 知道可以参考哪些已经做完的设计（Skills 是样板）
> - 直接进入设计稿环节
>
> **必读伴侣文档**：[Agent / Runtime 状态系统重设计](./agent-runtime-status-redesign.md) —— 完整的工程方案、状态规范、实施阶段。本 brief 是它的"设计师视角切片"。

---

## 目录

1. [一句话目标](#一一句话目标)
2. [必读：状态视觉规范](#二必读状态视觉规范)
3. [Agents 界面](#三agents-界面)
4. [Runtimes 界面](#四runtimes-界面)
5. [跨界面统一：Agent Hover Card](#五跨界面统一agent-hover-card)
6. [跨平台差异处理](#六跨平台差异处理)
7. [设计语言参考：Skills 界面](#七设计语言参考skills-界面)
8. [工程会同步交付的能力](#八工程会同步交付的能力)
9. [设计师产出清单](#九设计师产出清单)
10. [附录：截图占位区](#十附录截图占位区)

---

## 一、一句话目标

**当前所有界面都在直接展示后端字段，缺少"用户视角"的状态翻译层。**

用户看到 `Idle` / `Online` / spinner，但这些词没有回答"agent 现在能不能用 / 在做什么 / 出问题了没"。我们要做的是一套**用户视角的状态系统**：让用户一眼就知道一个 agent / runtime 的健康状况，跨界面一致。

---

## 二、必读：状态视觉规范

这套规范是所有界面的共同基础。**先输出这套规范，再画具体界面**。

### 2.1 Agent 五态

| 状态 | 颜色 | 用户语义 | 出现条件 |
|---|---|---|---|
| **Available** | 🟢 绿 | 在线空闲，可以接活 | runtime 在线 + 没有活跃任务 |
| **Working** | 🔵 蓝（品牌色） | 正在干活 | runtime 在线 + 至少一个任务在执行 |
| **Pending** | 🟡 黄 | 任务排着但没在跑 | runtime 在线 + 0 个执行中 + ≥1 个排队 |
| **Failed** | 🔴 红 | 最近一次失败 | 最近 2 分钟内有任务失败 |
| **Offline** | ⚫ 灰 | Daemon 离线，不可用 | runtime 离线 |

**复合维度**：当 agent 是 Working 但同时有任务排队，主状态保持 Working，旁边带 `+N` 角标（"还有 N 个排队"）。

**Failed 状态特别说明**：失败显示**保持 2 分钟**，之后自动恢复（避免红点黏太久）。设计上要表达"这是临时强提示"。

### 2.2 Runtime 四态

| 状态 | 触发条件 | 用户语义 |
|---|---|---|
| **Online** | 最近 45 秒内有心跳 | 健康 |
| **Recently Lost** | 离线但 < 5 分钟 | 可能短暂网络抖动 |
| **Offline** | 离线 5 分钟 ~ 7 天 | 长期离线，需排查 |
| **About to GC** | 离线接近 7 天阈值 | 系统将自动清理 |

> **关于 CLI 未安装等"runtime 在线但跑不了"的场景**：归并到 Offline，tooltip 写明具体原因（"CLI 未安装"、"Daemon 启动中"）。**不要为这些子情况新设状态色**，色彩枚举太多反而失去信号。

### 2.3 视觉表达分三层

每种状态在以下三个层级要保持一致：

- **Dot**（圆点）：列表项、头像旁的小圆点，最紧凑场景
- **Badge**（徽章）：详情页头部、卡片角落，带图标 + 文字
- **Tooltip / Hover Card**：鼠标悬停时展开完整信息

**跨界面一致性**：同一个 agent，无论出现在哪（agents 列表 / issue assignee picker / autopilot 编辑 / chat 选择面板 / 评论 @），状态视觉**必须完全一致**。这是这次设计能立住的关键。

---

## 三、Agents 界面

### 3.1 当前界面长什么样

主要文件：
- `packages/views/agents/components/agents-page.tsx` — 列表页容器
- `packages/views/agents/components/agent-list-item.tsx` — 列表项
- `packages/views/agents/components/agent-detail.tsx` — 详情页（含 tabs：Instructions / Skills / Tasks / Environment / Custom Args / Settings）
- `packages/views/agents/components/agent-profile-card.tsx` — 详情顶部的 profile 卡片
- `packages/views/agents/components/create-agent-dialog.tsx` — 创建对话框

**当前的视觉语言跟 Skills 重设计前一样——这次目标是迁到 Skills 风格。**

### 3.2 当前的核心痛点

按用户感知严重度排列：

1. **列表上的 "Idle" 绿点会骗人**——daemon 已经死了，agent 仍然显示 Idle。用户分配任务后没有任何反馈。**根因**：`agent-list-item.tsx` 完全忽略 runtime 在线性，只读 `agent.status` 这个后端字段。
2. **`agent-profile-card` 状态行和 runtime 行不联动**——状态显 Idle，runtime 显 Offline，自相矛盾。
3. **跨界面状态展示不一致**——issue assignee picker、autopilot picker、chat 选择 agent 时**完全不显示状态**，用户做选择时不知道哪个能用。
4. **创建 agent 时无法预知能否立即跑起来**——dialog 显示了 runtime 在线点，但不知道 model 是否合法、初始化会不会失败。
5. **Archived agent 和 active agent 混在同一列表**——切换视图全靠手动按钮，不够清晰。
6. **零 WS 订阅**——离线/上线必须手动刷新页面才能感知。

### 3.3 重设计目标

#### 必须达成

- 列表项一眼能看出 agent 是否真的可用（融合 runtime 在线性的派生 5 态）
- Hover card 内必须看到：派生状态、当前任务数、runtime 健康、最近失败原因
- 跨界面一致：所有显示 agent 头像的位置都用同一个 hover card（详见第 5 节）
- 创建/编辑 agent 时，能预看"如果保存，agent 会变成什么状态"
- 实时更新——状态变化 75 秒内反映到 UI

#### 加分项

- Archived agent 独立 tab/折叠区，不再和 active 混在一起
- 列表支持按"最近活动时间"排序
- 支持按状态筛选（"显示所有 Failed"、"显示所有 Pending"）

### 3.4 可用数据清单（设计稿可以放心假设可用）

> **图例**：✅ = 当前已展示；🆕 = 已可用但当前 UI 没用；🔧 = 工程会在阶段 0 补上；📡 = 实时事件可用

#### Agent 主体字段（来自 `Agent` type）

| 字段 | 类型 | 当前状态 | 说明 |
|---|---|---|---|
| `name` / `avatar_url` / `description` | string | ✅ | 已展示 |
| `archived_at` / `archived_by` | string | ✅ / 🆕 | 列表灰显；archived_by 后端有但 UI 隐藏 |
| `runtime_mode` | "local" / "cloud" | ✅ 图标 | Cloud / Monitor 图标区分 |
| `instructions` | string | ✅ | Instructions tab |
| `custom_env` / `custom_env_redacted` | KV / bool | ✅ | Env tab，权限控制隐藏值 |
| `custom_args` | string[] | ✅ | Custom Args tab |
| `visibility` | "workspace" / "private" | ✅ | Settings tab |
| `max_concurrent_tasks` | number | ✅ | Settings tab，默认 6 |
| `model` | string | ✅ | Settings tab |
| `runtime_id` | string | ✅ 选择器 | Settings tab |
| `skills` | Skill[] | ✅ | Skills tab + profile card 前 3 个 |
| `owner_id` | string | ✅ | Profile card 显示名字 |
| `created_at` / `updated_at` | string | 🆕 | **后端有，UI 完全没展示** —— 可以做"最近创建"、"最近修改"标签 |
| ~~`status`~~（idle/working/blocked/error/offline） | enum | ✅ 但**已废弃**展示 | 后端字段保留但 UI 完全不读 |

#### 派生数据（工程在阶段 0 提供，设计稿可放心引用）

| 派生信息 | 来源 |
|---|---|
| **Agent 派生 5 态状态** | 🔧 由 agent + runtime + active tasks 派生 |
| **当前 running 任务数** | 🔧 派生 |
| **当前 queued 任务数** | 🔧 派生 |
| **最近一次失败的原因**（5 种 enum） | 🔧 派生 |
| **关联 runtime 健康状态** | 🔧 派生 |
| **runtime last_seen 相对时间** | 🔧 已有工具函数 |

**失败原因 5 个枚举（中文文案待你定）**：
- `agent_error` — Agent 执行报错
- `timeout` — 执行超时
- `runtime_offline` — Daemon 离线
- `runtime_recovery` — Daemon 重启回收
- `manual` — 用户取消

每种原因用户的处理方式不同，UI 应给出对应建议（你来设计文案）。

#### 实时事件（WS）

| 事件 | 状态 | 用途 |
|---|---|---|
| `agent:status` | 📡 后端发，工程接 | agent 字段变化 |
| `agent:created` / `agent:archived` / `agent:restored` | 📡 工程接 | 列表增删 |
| `task:dispatch` / `task:completed` / `task:failed` / `task:cancelled` | 📡 工程接 | 状态派生关键信号 |
| `daemon:register` | 📡 工程接 | runtime 上下线 |

设计上**不需要为"加载/loading"过度设计**——状态变化是实时的，几乎不会出现"等数据"的 loading 态。

### 3.5 不能动的现有交互（必须保留）

- 创建 agent dialog 的 chooser → 表单两步流（见 Skills 的 `create-skill-dialog`）
- 各 tab 的编辑能力：Instructions / Env / Custom Args / Settings 全部可编辑
- Archive / Restore 操作
- Tasks tab：展示该 agent 的历史 task 列表（按状态分组：活跃 → 完成）
- Skills tab：可挂载/卸载 skills

### 3.6 关键问题留给你定的

1. **Archived agent 怎么收纳**：独立 tab、折叠区、还是 segment 切换？
2. **状态筛选**是 chips 还是 dropdown？
3. **Failed 红点的 2 分钟动效**：脉冲？颜色渐变？
4. **复合状态（Working + 排队 N）**：角标位置（dot 旁 / 头像下角 / Badge 内嵌）？
5. **创建 dialog 的"预览状态"**：怎么不打扰主流程的同时让用户知道"这个 agent 创建出来会是什么色"？

---

## 四、Runtimes 界面

### 4.1 当前界面长什么样

主要文件：
- `packages/views/runtimes/components/runtimes-page.tsx` — 容器，含 owner filter（mine/all）
- `packages/views/runtimes/components/runtime-list.tsx` — 列表
- `packages/views/runtimes/components/runtime-detail.tsx` — 详情头部
- `packages/views/runtimes/components/usage-section.tsx` — Token usage 主区
- `packages/views/runtimes/components/charts/` — 5 个图表组件
- `packages/views/runtimes/components/update-section.tsx` — CLI 更新流程
- `apps/desktop/src/renderer/src/components/daemon-runtime-card.tsx` — 桌面端独有：本机 daemon 卡片（通过 IPC，独立于 server runtime 列表）

### 4.2 当前的核心痛点

按严重度排列：

1. **离线圆点几乎不可见**——浅色主题下 `bg-muted-foreground/40` 视觉上消失。
2. **列表无 last_seen**——无法区分"刚断线 5 分钟"和"3 个月前断线"。
3. **看不到 runtime 服务了哪些 agent / 当前有几个 task 在跑**——runtime 在用户心智里成了"孤岛"。
4. **7 天 GC 阈值无任何 UI 提示**——runtime 突然消失，用户不知道为什么。
5. **桌面端 daemon 卡片和云端 runtime 卡片视觉分裂**——同一台机器同一概念，两套设计。
6. **Token usage 信息过载**——5 个图表 + 1 张表全部展开，普通用户找不到"本月花了多少钱"。
7. **无 ping / 诊断按钮**——遇到断线没法主动验证。
8. **`RuntimeModeIcon` 死代码**——本地 vs 云端在列表项里没有图标区分。

### 4.3 重设计目标

#### 必须达成

- 列表项一眼区分四态：Online / Recently Lost / Offline / About to GC
- 列表项展示**关联 agent 数量** + **当前任务数**（runtime 不再是孤岛）
- 桌面端 daemon 卡片和云端 runtime 卡片用统一视觉语言
- Token usage 区分主次：核心指标（本期成本、token 总量）放顶部，详细图表折叠或下沉

#### 加分项

- Runtime 健康综合评分（在线 + 心跳新鲜度 + 任务负载）
- 7 天 GC 倒计时提示
- Ping / 诊断按钮
- 高使用量 runtime 的视觉强调（成本警告、利用率热图 sparkline）
- Local vs Cloud 图标区分（启用废弃组件 `RuntimeModeIcon`）

### 4.4 可用数据清单

#### Runtime 主体字段（来自 `RuntimeDevice` type）

| 字段 | 类型 | 当前状态 | 说明 |
|---|---|---|---|
| `name` | string | ✅ | 列表 + 详情头部 |
| `provider` | string | ✅ Logo | 9 种：claude / codex / opencode / openclaw / hermes / gemini / pi / cursor 等 |
| `runtime_mode` | "local" / "cloud" | ✅ 文字 | 列表项里没图标（死代码） |
| `status` | "online" / "offline" | ✅ 圆点 | 浅色主题下离线几乎不可见 |
| `last_seen_at` | string | ✅ 仅详情页 | **列表完全看不到** |
| `device_info` | string | ✅ 详情页 | 显示原始字符串如 `darwin-arm64`，无人类可读化 |
| `daemon_id` | string | ✅ mono 字体 | 不可复制 / 不可点击 |
| `metadata.cli_version` | string | ✅ | CLI 更新部分用 |
| `metadata.launched_by` | string | ✅ | 桌面端启动时显示 "Managed by Desktop" |
| `owner_id` | string | ✅ | 头像 + 名字 |
| `created_at` / `updated_at` | string | ✅ | 详情页底部 ISO 时间戳 |

#### 派生数据（工程阶段 0 提供）

| 派生信息 | 来源 |
|---|---|
| **Runtime 4 态健康** | 🔧 由 status + last_seen_at 派生 |
| **服务的 agent 列表 + 数量** | 🔧 前端 join（agent 持有 runtime_id，列表数据已在 cache） |
| **当前在跑 task 数** | 🔧 前端 filter active-tasks |
| **last_seen 相对时间字符串**（"5 minutes ago"） | 🔧 工具函数已有 |

#### Token Usage 5 个图表的数据契约

| 图表 | 数据源 | 时间粒度 | 维度 | 度量 |
|---|---|---|---|---|
| **Activity Heatmap** | `getRuntimeUsage?days=90` | 日 | date | 4 级强度，按 token 总量百分位分级 |
| **Hourly Activity** | `getRuntimeTaskActivity` | 小时（0-23） | hour | 任务数 |
| **Daily Token Chart** | 同 Heatmap，客户端聚合 | 日 | date | input/output/cacheRead/cacheWrite 总和 |
| **Daily Cost Chart** | 同上 + 客户端定价 | 日 | date | 美元成本 |
| **Model Distribution** | 同上聚合 by model | 全周期 | model | tokens 占比 + cost |

**当前实现的问题**：API 总是取 90 天数据，客户端做 7d/30d 过滤——浪费服务端资源。改造时按选中窗口拉。

#### 实时事件

| 事件 | 状态 |
|---|---|
| `daemon:register` | 📡 已订阅，触发列表刷新 |
| `daemon:heartbeat` | 📡 后端发但前端**故意忽略**（防过度刷新）。设计时假设状态变化最坏 75 秒内可见 |

#### 桌面端独有的本机 IPC 数据（仅桌面端可见）

```
DaemonStatus (本机 IPC，亚秒级实时)
├─ state: running / stopped / starting / stopping / installing_cli / cli_not_found
├─ pid, uptime, daemonId, deviceName, serverUrl
├─ agents: 当前运行的 agent IDs
├─ workspaceCount
└─ profile
```

工程会把这份数据自动喂进 cache，**设计上不要为"桌面端有更多数据"做特殊视觉**——派生状态对设计师而言是统一的，只是桌面端响应更快。但桌面端**对自己的本机 daemon 有更多操作能力**（见第 6 节）。

### 4.5 不能动的现有交互（必须保留）

- Owner filter（mine/all toggle）
- Delete runtime 操作（含权限检查）
- CLI 更新流程（`update-section.tsx`）：检查更新、触发更新、查看更新状态
- 5 个图表的数据展示（信息架构可以重排，但数据本身要保留）
- 桌面端：start/stop/restart 本机 daemon 的按钮

### 4.6 关键问题留给你定的

1. **列表项到底放多少信息**：last_seen + agent count + active tasks 放哪？避免拥挤
2. **桌面端 daemon 卡片和云端 runtime 卡片"视觉对齐"的尺度**：100% 同模板？还是同卡片框 + 内容差异化？
3. **Token usage 主次怎么排**：本期成本数字 + 单图表 + "查看更多" 折叠？或者 dashboard 化？
4. **About to GC 怎么提示**：横幅？badge？倒计时？
5. **Ping / 诊断按钮的位置**：详情页头部？右上角菜单？
6. **关联 agent 列表展示**：堆叠头像？文字 "3 agents"？还是子区块？

---

## 五、跨界面统一：Agent Hover Card

### 5.1 现有组件

`packages/views/agents/components/agent-profile-card.tsx`——已经存在，但只在 Agents 主页 hover 时出现。

这次会把它升级成**统一 hover card**，挂到所有展示 agent 头像的地方。

### 5.2 必须出现的位置

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

### 5.3 卡片必须显示什么（按重要度）

1. **派生 5 态状态**（不是 `agent.status` 原始值）
2. **Runtime 健康**：在线性 + last_seen 相对时间
3. **当前任务**：N running / M queued
4. **最近失败**（如果有）：原因 + 时间
5. **Agent 名称 + description**
6. **关联 skills**（前 3 个 + `+N`）
7. **Owner**

### 5.4 设计要点

- 卡片宽度跨多种使用场景要适配（issue 列表很窄、设置页很宽）
- 触发延迟（hover delay）跟 Skills 已有的卡片保持一致
- 暗色主题下信息层级要清晰

---

## 六、跨平台差异处理

### 6.1 状态视觉是平台无关的

派生 5 态 / 4 态、视觉规范、hover card——**两端共享同一套设计**。设计稿只画一份。

### 6.2 数据响应速度差异（不影响视觉）

| 平台 | Runtime 状态变化感知延迟 |
|---|---|
| Web | 最坏 75 秒 |
| Desktop（看自己机器） | < 1 秒（IPC） |
| Desktop（看别人机器） | 最坏 75 秒（跟 Web 一样） |

设计上不需要透出"快慢"——用户感知不到这是 IPC 还是 server。

### 6.3 操作能力差异（影响按钮可见性）

| 操作 | Web | Desktop（自己机器） | Desktop（别人机器） |
|---|---|---|---|
| 看状态 | ✅ | ✅ | ✅ |
| 重启 daemon | ❌ | ✅ | ❌ |
| 看 daemon logs | ❌ | ✅ | ❌ |
| 看 CLI 安装详情 | ❌ | ✅ | ❌ |

**设计要点**：操作按钮在不该有权限的位置应该**直接隐藏**，不要灰显（避免视觉噪音）。

### 6.4 桌面端独有的"本机 daemon 卡片"

当前桌面端有一个独立卡片显示本机 daemon。重设计后：
- 视觉上跟云端 runtime 列表项**用同一套视觉语言**
- 但承载更多本地操作（重启、看日志、安装 CLI、profile 切换）
- 位置：列表顶部 sticky / 列表头部突出 / 右侧独立 panel —— 由你定

---

## 七、设计语言参考：Skills 界面

### 7.1 Skills 是这次重设计的视觉锚点

Skills 界面已经在 2026-04 完成重设计（PR #1607、#1614、#1618、#1610）。这次 Agents 和 Runtimes **直接照搬 Skills 的视觉语言**，保持产品体感一致。

参考目录：`packages/views/skills/`

### 7.2 必须复用的 10 条规则

1. **统一页头** `PageHeader`（h-12 + mobile sidebar trigger）
2. **响应式网格列表**：`grid-cols-[minmax(0,1.6fr)_minmax(0,0.8fr)_minmax(0,1.2fr)_minmax(0,6rem)_auto]`，不用 flexbox
3. **每行三层信息**：主标题（font-medium）→ 描述（line-clamp-1 muted）→ 元数据（xs muted）
4. **关联对象用头像堆栈**：最多 3 + `+N`，size=22 + `ring-2 ring-background` + `-space-x-1.5`
5. **卡片化列表 + 卡片内工具栏**：搜索和 scope tab 在 `CardToolbar`（h-12），不在页面级
6. **创建用多步 dialog**：chooser → 表单可回退；Dialog 宽度按方法切换（manual/url 用 `!max-w-md`，runtime 用 `!max-w-2xl`），300ms 平滑过渡
7. **空状态 / 筛选无结果分别有详细文案**：图标 + 标题 + 三行说明 + 清晰 CTA
8. **长列表加 `useScrollFade`**：滚动容器上下边缘淡出
9. **头像统一用 `ActorAvatar`**：传 `size`，自动支持 agent / 人员
10. **权限检查 hook 化**：`useCanEdit...`，UI 提前隐藏/禁用操作按钮

---

## 八、工程会同步交付的能力

阶段 0（数据层地基）完成后，**设计稿可以放心假设以下能力都到位**：

- 任意位置都能拿到一个 agent 的派生 5 态状态
- 任意位置都能拿到 agent 的当前任务数（running / queued）
- 任意位置都能拿到 agent 关联的 runtime 4 态健康
- 任意位置都能拿到 runtime 服务的 agent 列表 + 数量
- 任意位置都能拿到 runtime 当前任务数
- 状态变化是实时的（订阅 WS 事件后会自动更新）
- 桌面端会自动获得"亚秒级"响应——不需要为此画两套稿

### 已有 API（不需要新加，可放心引用）

- `listAgents` / `getAgent` / `createAgent` / `updateAgent` / `archiveAgent` / `restoreAgent`
- `listAgentTasks(agentId)` — 单 agent 历史任务
- `listRuntimes` / `deleteRuntime`
- `getRuntimeUsage(runtimeId, { days })` — token 用量
- `getRuntimeTaskActivity(runtimeId)` — 小时级活动

### 工程要在阶段 0 补的（设计稿可以假设有，但要知道这是新增）

- **后端**：`GET /api/workspaces/:slug/active-tasks` — 全工作区活跃任务一次拉
- **后端**：诊断 / Ping API（如果你的设计稿用到，工程要评估优先级）
- **前端类型**：`AgentTask.failure_reason` 字段（5 枚举：agent_error / timeout / runtime_offline / runtime_recovery / manual）暴露到前端类型
- **前端**：派生函数（`deriveAgentPresence` / `deriveRuntimeHealth`）+ 全工作区 active-tasks query + WS 接线

### 工程不会做的（设计稿不要假设有）

- 不引入"agent 健康综合评分"——只暴露原始信号
- 不做"从历史 task 自动推断 agent 类型"等 AI 派生
- 不为 runtime 引入新状态色（稳定在 4 态）
- 不做后端聚合 API（除了 active-tasks 这个补全）

---

## 九、设计师产出清单

按优先级排列，**P0 必须先于 P1**。

### P0 — 状态视觉规范（基础，所有界面共用）

- 5 态颜色 token（Available / Working / Pending / Failed / Offline）
- 4 态颜色 token（Online / Recently Lost / Offline / About to GC）
- Dot / Badge / Tooltip 三层视觉规范
- 复合维度（Working + 排队角标）的视觉表达
- Failed 状态的 2 分钟时间窗口动效（强提示 + 自动消失）

### P0 — Agents 界面

- 列表页（含派生状态、关联 runtime 健康、最近活动时间）
- 详情页头部 + Profile Card（状态行联动）
- 创建对话框（保留两步流，加入 runtime 在线状态预览）

### P0 — Runtimes 界面

- 列表页（暴露 last_seen、关联 agents、当前 task 数）
- 详情页头部（4 态 badge、device info 人类可读化）
- Token usage 信息架构重整：核心指标置顶，详细图表下沉/折叠

### P1 — Hover Card 跨界面统一

- 一个适配多场景宽度的卡片设计
- 7 项内容的信息层级
- hover 触发交互（与 Skills 一致）

### P1 — 桌面端本机 daemon 卡片

- 视觉对齐云端 runtime 卡片
- 本机操作按钮（重启 / 日志 / CLI 安装）的位置
- Profile 切换（如果做多 profile）

### P2 — 加分项

- Runtime 健康综合评分（视觉化）
- 7 天 GC 倒计时
- 高使用量 runtime 的成本警告
- Local vs Cloud 图标区分
- Agent archived 独立 tab/折叠区
- 列表按"最近活动"排序、按状态筛选

---

## 十、附录：截图占位区

> 设计师拿到这份文档后，请把以下三个界面的当前截图贴在对应位置，作为重设计前的现状记录，方便对比。

### 10.1 Agents 界面

- 列表页截图：__待贴__
- 详情页截图（每个 tab 一张：Instructions / Skills / Tasks / Environment / Custom Args / Settings）：__待贴__
- Profile Card 截图：__待贴__
- 创建对话框截图（chooser + form 两步）：__待贴__
- Hover card 当前样式截图：__待贴__

### 10.2 Runtimes 界面

- 列表页截图（mine / all 两态）：__待贴__
- 详情页截图（含 5 个图表全部展开）：__待贴__
- 桌面端 daemon 卡片截图：__待贴__
- CLI 更新流程截图：__待贴__

### 10.3 跨界面 agent 头像出现的位置

- Issue Assignee Picker：__待贴__
- Issue Detail 头部 assignee：__待贴__
- Issue 列表 / 看板：__待贴__
- Autopilot 列表 / 编辑：__待贴__
- Project lead picker：__待贴__
- Chat agent 选择面板：__待贴__
- 评论 @agent：__待贴__

---

## 参考文档

- [Agent / Runtime 状态系统重设计（主文档）](./agent-runtime-status-redesign.md) — 完整工程方案、状态规范、实施阶段
- [产品全景文档](./product-overview.md) — 理解 agent / runtime / daemon 在整个产品里的位置
- [Skills 界面源代码](../packages/views/skills/) — 直接参考的设计语言样板
- 相关 PR：#1607（Skills 重设计）、#1614（Card + PageHeader）、#1618（描述恢复）、#1610（Dialog 闪烁修复）
