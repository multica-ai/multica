# Onboarding 重新设计 — 项目提案

**日期**：2026-04-21
**作者**：Naiyuan
**状态**：方案定稿，待评审后进入执行

---

## 一、为什么要做

### 1.1 数据层面的两个漏斗

当前产品数据暴露了两个关键的用户流失点：

1. **第一漏斗**：很多用户创建完 workspace 后，**从未连接本地 daemon**。没有 runtime = 没有 agent = 产品价值归零。这是最严重的漏斗。
2. **第二漏斗**：连接了 daemon 的用户中，**约一半从未创建 issue**。他们跨过了最难的技术门槛，却倒在了空 issue 列表面前——因为"该让 agent 做什么"对新用户并不直观。

这两个漏斗说明：**我们把用户送到了门口，但没有送他们进门**。

### 1.2 当前 Onboarding 的不足

代码层面现状（`packages/views/onboarding/` + `apps/web/app/(auth)/onboarding/page.tsx` + `apps/desktop/src/renderer/src/components/window-overlay.tsx`）：

| 环节 | 现状 | 问题 |
|---|---|---|
| Welcome | 纯打招呼 + "Get started" 按钮 | 0 价值、+1 次点击、文案"takes about a minute"对 web 用户不诚实 |
| Workspace 创建 | 复用 `CreateWorkspaceForm` | ✅ 基本合理，保留 |
| Runtime 连接 | Desktop 静默、Web 显示 CLI 指南 | ✅ 机制对，但 web 体验上**一路走到第 3 步才撞上 CLI 这堵墙**，没有提前分流 |
| Agent 创建 | 2 个模板（Master / Coding）+ 手填 name | Master 模板对 96% 的 solo 用户是噪音；手填 name 是多余决策；没有 Assistant 这种零门槛兜底 |
| Complete | 仪式感庆祝 + "Enter workspace" | **aha moment 没发生**。用户被告知 agent 准备好，却看不到它工作，进去就是空 issue 列表——正好是第二漏斗 |
| 个性化 | 无 | 所有用户看到同一套流程，不利用任何已知信息 |
| 进度持久化 | `useHasOnboarded()` 硬编码 `false` | 中途退出会从头开始；跨端切换完全无法恢复 |

### 1.3 行业对标

调研多篇一线案例和数据后，业界已收敛到几条硬原则：

- **激活 > 教育**：Onboarding 唯一的 KPI 是用户到达 aha moment 的速度和比例。Slack 的 "2000 条消息 → 留存 93%" 是最经典案例
- **2 分钟到首次价值**：通用 SaaS 目标
- **<90 秒 TTFAC**：Stripe / Vercel 为开发者工具设定的标杆
- **开发者工具转化率天然低**：通用 SaaS 试用转化 15–25%，开发者工具只有 8–15%，**68% 放弃原因是 setup 太复杂**
- **问卷是杀手**：每多一个表单字段完成率下降 3–5%，某 case 强制问卷导致转化率下降 80%+，另一 case 6→3 题响应率 +11%
- **Progressive disclosure 淘汰前置大 tour**：学习应该分散在使用过程中，不是一次性塞给用户
- **Notion 模式是黄金范本**：1 题驱动模板选择 + 邮件路径 + 界面预览——"一题多用"

### 1.4 对标 Multica 的定位

Multica 不是"做一个 agent"的产品。它的核心价值是**把一支由用户编排的 AI agent 小队组织起来协作**——一个 agent 写代码、一个规划任务、一个做研究、一个写内容——每个 agent 是带配置（provider / runtime / instructions / skills）的独立工作者，像同事一样被指派 issue。

这意味着：
- 用户不是单一场景（"AI 帮我写代码"），而是多角色用户都在编排 agent：开发者、产品 / 项目负责人、writer、founder
- "用户在用什么本地 CLI"是 daemon 自动探测的技术事实（`claude` / `codex` / `opencode` / `openclaw` / `hermes` / `gemini` / `pi` / `cursor-agent` 扫 PATH 即可），**不需要问用户**
- 真正值得问的是**用户是谁、想让 agent 干什么**——这个答案驱动 Step 4 模板、Step 5 first issue 和 Onboarding Project 的内容

---

## 二、调研结论与核心原则

- 主流程必须严格以激活为目的——Welcome、功能介绍、问卷这些"非激活"内容都要极限压缩或后置
- 问卷题数 ≤3 题，且每题答案必须能直接改变下游某个屏的内容，否则砍掉
- "Onboarding Project + sub-issues" 属于**教育载体**，不是 onboarding 主流程——它应该在 aha moment 发生后以侧边栏常驻形式出现
- Web 不应该是 desktop 的"平行路径"，而应该是**漏斗入口**：鼓励用户下载 desktop，保留 web+CLI 作为备选
- 进度必须后端持久化，跨端 resume 是硬要求

主要 Sources 列在文末第八节。

---

## 三、方案要点

### 3.1 主流程：5 步（严格有序）

```
Step 0: Welcome (产品介绍, 首次进入时展示, 不入后端 state)
Step 1: 3-Q 问卷               （team_size / role / use_case）
Step 2: 创建 workspace
Step 3: 连接 runtime           ← 两端最大差异在这一步
Step 4: 创建 agent             ← 按 Q1 × Q3 预填
Step 5: 🎯 First Issue         ← aha moment，按 Q3 驱动文案
```

**Onboarding Project** 在 Step 5 完成的那一刻后台创建，作为进入 workspace 之后的侧边栏常驻项——**不算 onboarding 的一步**。

### 3.2 两端差异表

| Step | Desktop | Web |
|---|---|---|
| 1. 问卷 | 一屏 3 题 | 一屏 3 题（完全一致） |
| 2. Workspace | `CreateWorkspaceForm` | 完全一致 |
| 3. Runtime | **静默自动**：bundled daemon 1–2s 内 online → 直接跳 Step 4。只在失败时显示诊断 | **分流决策屏**（见 3.3） |
| 4. Agent | 一键 Create（按 Q1×Q3 预填模板 + provider） | 完全一致 |
| 5. First issue | 跳到 issue 详情页，观察 agent reply | 完全一致 |

唯一真正不同的是 Step 3。其他"差异"本质是问卷答案驱动的个性化，跨端一致。

### 3.3 Web 端 Step 3 分流屏

这是 web 用户创建完 workspace 后看到的屏，**取代当前直接展示 CLI install 指南的做法**：

```
┌─────────────────────────────────────────────┐
│  Multica runs on your machine               │
│  Agents need a local runtime to run.        │
│  How would you like to set up?              │
│                                             │
│  ┌───────────────────────────────────────┐  │
│  │ [Primary CTA, 80% 视觉权重]           │  │
│  │ ⬇ Download for macOS (recommended)    │  │
│  │ Fastest setup, bundled runtime        │  │
│  └───────────────────────────────────────┘  │
│                                             │
│  Or: Continue on web with CLI               │
│  Or: I want cloud agents (join waitlist)    │
└─────────────────────────────────────────────┘
```

三条路径：

- **下载桌面端（默认，目标 60%+）**：点下载 → 写 `platform_preference: "desktop"` → 桌面端装完登录同账号 → 后端 state 触发跳 Step 3 → bundled daemon 1s pass → 进 Step 4
- **CLI 继续（次选）**：保留现有 `CliInstallInstructions`，但新增预期管理（"通常 2–4 分钟"）和 60s stuck-state fallback（"Stuck? 常见问题"）
- **Cloud waitlist（soft exit）**：邮箱 capture → 标记为"临时完成"（`onboarded_at` 写当前时间，保留 `cloud_waitlist_email`）→ 进 workspace + 顶部 banner

### 3.4 三个问题的设计

**Q1：Who will use this workspace?**（单选）
- ○ Just me
- ○ My team (2–10 people)
- ○ Other ⇒ 展开 80 字符文本框

注意：删掉了"Just exploring for now"——它本质是"态度"而不是"人数结构"，和这题的题意不契合；评估型用户如果真的选项都不合适，可以通过 Other 写自由文本（"just trying it out" 等）表达。

**Q2：What best describes you?**（单选）
- ○ Software developer
- ○ Product / project lead
- ○ Writer or content creator
- ○ Founder / solo operator
- ○ Other ⇒ 展开 80 字符文本框

**Q3：What do you want to do first?**（单选）
- ○ Write and ship code
- ○ Plan and manage projects
- ○ Research or write
- ○ Just explore what's possible
- ○ Other ⇒ 展开 80 字符文本框

**提交策略（必答）**：
- Continue 按钮只在**三题全部有具体选择**时启用；否则禁用
- 任一问题选了 Other 但文本框为空 → 也禁用
- 从 Other 切回其他选项 → 对应的 `*_other` 字段自动清空
- **没有 Skip 路径**。理由：三个答案驱动 Step 4 agent template、Step 5 first-issue prompt、Onboarding Project sub-issue 排序；partial 答案会在下游每一步都留洞。Other 自由文本（+ 80 字符上限）已经兜住所有非典型用户，不需要再开 null 这个口子
- 之前允许"全部不选 Skip"的策略在 commit 中已反悔——实测下来"给自由 = 问卷质量塌方"的风险比"多一点摩擦"更值得警惕

**"Other" 的下游价值——不是兜底，是 escape hatch + 个性化输入**

Q3 的 `use_case_other` 会**直接嵌入到 Step 5 first issue 的 prompt** 里：

> "Hi, I'm {user}. I told Multica I want to use you for \"{use_case_other}\". Introduce yourself and give me 3 concrete ways you could help with that."

也就是说，选 Other 的用户**反而**得到最个性化的 first issue——他们给 agent 的任务描述就是他们亲口写的。Q2 `role_other` 没有同样的嵌入位置，但会存进 state 给市场研究用。

**被砍掉的问题及理由**：

- ~~"你在用哪些 AI agent？"~~（原方案 Q1）→ daemon 启动时自动扫 PATH 探测已安装的 CLI（`claude` / `codex` / `opencode` / `openclaw` / `hermes` / `gemini` / `pi` / `cursor-agent`），比问用户更准——用户可能说"我用 Claude Code"但 PATH 里并不存在。从"问"改成"测"，问卷压掉一题
- ~~"你是做什么的"（职业）~~ → 原方案砍掉过；现因为定位校准（Multica 不是 coding-focused 产品），重新作为 Q2 加回，驱动 agent template 选择
- ~~"公司规模"~~ → solo/team 二分已经够用；具体公司规模属于 Day 3 邮件采集范围
- ~~"从哪里知道 Multica"~~ → 归因数据走分析系统，不占问卷位

### 3.5 个性化映射

所有个性化来自这三个答案 + daemon 自动探测到的 runtime 列表。**不做 Q 之外的任何猜测**——透明、可预期、可调试。

#### Runtime 优先级（来自 daemon 探测，不来自问卷）

Step 3 结束时 daemon 会报告"当前 PATH 上探测到的 CLI 列表"。Step 4 的 provider 预选逻辑：

| daemon 探测结果 | Step 4 provider 预选 |
|---|---|
| 有 online runtime | 第一个 online 的 provider |
| 列表非空但全 offline | 列表中第一个 |
| 列表为空（Cloud waitlist 或 CLI 没装成功） | 不预选，在 Step 4 给用户手选或跳过 |

provider 值对齐 `packages/views/runtimes/components/provider-logo.tsx` 中已支持的：`claude` / `codex` / `opencode` / `openclaw` / `hermes` / `gemini` / `pi` / `cursor`。

#### Q1 (team_size) → Onboarding Project sub-issue 排序

| Q1 | Onboarding Project 顶部 sub-issue |
|---|---|
| `solo` | "Assign a real task to your agent" |
| `team` | **"Invite teammates"** 置顶 |
| `other` | 按 `solo` 路径处理（不强行归类；`team_size_other` 文本存下做市场研究） |

#### Q2 (role) → Step 4 agent template 默认选择（× Q3 细化）

Multica 是服务多角色 agent 编排用户的平台，不同 role 在 agent template 上应该看到默认的 role-matched 模板：

| Q2 role | Q3 use_case | 默认 template |
|---|---|---|
| `developer` | `coding` | Coding Agent |
| `developer` | `planning` | Planning Agent |
| `developer` | `writing_research` / `explore` / `other` | Coding Agent（仍默认，因为角色是开发者） |
| `product_lead` | `coding` | Coding Agent |
| `product_lead` | `planning` | Planning Agent |
| `product_lead` | `writing_research` / `explore` / `other` | Planning Agent |
| `writer` | `writing_research` | Writing Agent |
| `writer` | 其他 | Writing Agent |
| `founder` | 任意 | Assistant（founder 什么都干，通用兜底） |
| `other` | 任意 | Assistant |

**Agent 模板集从 3 个扩到 4 个**：Coding Agent / Planning Agent / **Writing Agent（新增）** / Assistant。砍掉旧的 "Master Agent"（对 solo 用户完全不适用）。Writing Agent 的增加是因为产品定位校准——原方案默认 coding-focused，新方案支持 writer 作为一等用户。

#### Q3 (use_case) → Step 5 first issue prompt

First issue 的标题和 prompt 都由 Q3 单独驱动（与 Q2 role 解耦——同一个 role 做不同的 first task 是正常的）：

| Q3 | First Issue 标题 | First Issue 描述（= 给 agent 的 prompt） |
|---|---|---|
| `coding` | "Welcome me and show me what you can do" | "Hi, I'm {user}. I'll use you mostly for coding work. Introduce yourself and suggest 3 concrete coding tasks I could try." |
| `planning` | "Help me plan my first project" | "Hi, I'm {user}. I want you to help me plan and break down work. Introduce yourself and suggest 3 types of projects we could tackle." |
| `writing_research` | "Show me how you help with research and writing" | "Hi, I'm {user}. I'll use you for research and writing. Introduce yourself and give me 3 examples of how you can help — drafting, summarizing, analysis, etc." |
| `explore` | "What can you do?" | "Hi. I'm exploring what Multica can do. Give me a quick tour of what you can help with and suggest 3 concrete things to try." |
| `other` | "Help me with what I had in mind" | "Hi, I'm {user}. I told Multica I want to use you for \"{use_case_other}\". Introduce yourself and give me 3 concrete ways you could help with that." |

`{use_case_other}` 的嵌入是 Other 选项的关键价值——选 Other 的用户不是被降级成通用兜底，反而得到最精准的 first issue。

### 3.6 Onboarding Project 设计

Project 名称："Getting Started"。在 Step 5 完成那一刻后台创建，包含以下 sub-issues。

**Core sub-issues（所有用户都有）**：

1. **"Chat with your agent without creating an issue"**
   > Some tasks are quick back-and-forth — you don't need a full issue. Open the chat panel from the top-right and try asking your agent a question.

2. **"Assign a real task to your agent"**
   > You've seen your agent reply in this welcome issue. Now try assigning them something you actually need done. Create a new issue, describe the task, assign it to {agent_name}.

3. **"Write your Workspace Context"**
   > Workspace Context is the shared system prompt every agent in this workspace sees. Tell them who you are, what you're building, and how they should behave. Go to Workspace settings → Context.

4. **"Create a second agent with a different role"**
   > Multica's real power is running a small team of specialized agents. Create a Planning agent to complement your Coding agent, or a Writing agent to draft content. Go to Agents → "New agent".

5. **"Configure your agent's skills"**
   > Skills let you give your agent specific tools and capabilities. Go to your agent's settings and try toggling a skill.

6. **"Set up an Autopilot for recurring work"**
   > Autopilot creates issues on a schedule — daily standup summaries, weekly bug triage, monthly reports. Your agent picks them up automatically. Go to Autopilots → "New autopilot".

**Conditional sub-issues**（按答案插入 / 置顶 / 过滤）：

- **Q1 = `team`** → "**Invite your teammates**" 置顶
- **Q2 = `developer`** 或 **Q3 = `coding`** → "**Connect a repo to your workspace**" 加入 core #2 之后
- **Q2 = `product_lead`** → "**Create a project with sub-issues**" 置顶
- **Q2 = `writer`** → 跳过 "Connect a repo"（coding-specific），其余 core 保留
- **runtime 列表为空**（Cloud waitlist 或 CLI 未装成功）→ 插入 "**Install your first local runtime**" 置顶

**设计原则**：每个 sub-issue 都可以直接 assign 给 agent。Agent 读到 description 后，用自然语言给用户一句引导 + 一个具体建议。这样 sub-issue 既是"教程"又是"和 agent 互动"的自然场景——学习动作本身就是使用产品。

### 3.7 Resume 策略

**核心原则**：恢复到上次 step，不重头开始，MVP 阶段不设过期时间，允许任意回退改答案。

理由：
- Onboarding 总时长 <10 分钟，绝大多数用户一口气走完
- 中途离开再回来的，基本都是被别的事打断——重头开始是侮辱
- 过期策略（7 天后重置之类）是用代码解决还没发生的问题——**等真观察到 abandon-return 模式再加**

跨端 resume 的完整行为表：

| 场景 | 预期行为 |
|---|---|
| Web 完成 Step 1&2，关浏览器，2h 后重开 web | 读 state → 跳过 Step 1/2 → 直接 Step 3 |
| Web 到 Step 3 点"下载桌面端"，装完登录 desktop | Desktop 读 state → 跳 Step 3 → bundled daemon 1s pass → 进 Step 4 |
| Web 到 Step 3 点"下载桌面端"，没装，3 天后回 web | 检测到 `platform_preference=desktop` 但当前是 web → 显示 "Waiting for you on desktop" 屏 + "改用 web/CLI 继续" 入口 |
| Desktop Step 5 first issue 刚创建但没看 agent reply 就关闭 | 重开 desktop → current_step 仍是 `first_issue` → 直接打开那个 issue 详情页 |
| Onboarding 完成后再登录 | `onboarded_at` 非 null → 跳过 onboarding → 正常进 workspace |
| Onboarding 中创建的 workspace 被删（边缘 case） | `workspace_id` 变 NULL → 下次进 onboarding 检测到 `current_step=runtime` 但 `workspace_id=null` → 回退到 Step 2 重新建 |

**"回退改答案" 的 UX 细节**：每一步有 "Back" 按钮回上一步。回退**不清空已保存的数据**——用户只是修改，不是重置。

---

## 四、后端数据设计

### 4.1 `user_onboarding` 表 schema

**设计决策**：稳定字段用列，灵活字段用 JSONB。问卷答案放 JSONB（题目可能演化），其他字段（FK、控制字段、enum）都是独立列。

```sql
CREATE TABLE user_onboarding (
  user_id              UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,

  -- 控制状态
  started_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  onboarded_at         TIMESTAMPTZ,          -- null = 未完成
  current_step         TEXT,                 -- null after onboarded_at
                                             -- 'questionnaire'|'workspace'|'runtime'|'agent'|'first_issue'

  -- 问卷答案（会演化，放 JSONB）
  questionnaire        JSONB NOT NULL DEFAULT '{}'::jsonb,
  -- 期望结构:
  -- {
  --   "team_size": "solo" | "team" | "other",                        -- Q1
  --   "team_size_other": "<= 80 chars" | null,                        -- Q1 自由文本（选 other 时必填）
  --   "role": "developer" | "product_lead" | "writer" | "founder" | "other",  -- Q2
  --   "role_other": "<= 80 chars" | null,                             -- Q2 自由文本
  --   "use_case": "coding" | "planning" | "writing_research" | "explore" | "other",  -- Q3
  --   "use_case_other": "<= 80 chars" | null                          -- Q3 自由文本（会嵌入 Step 5 prompt）
  -- }

  -- Onboarding 产物（FK，要 join / 查询）
  workspace_id             UUID REFERENCES workspaces(id) ON DELETE SET NULL,
  runtime_id               UUID REFERENCES agent_runtimes(id) ON DELETE SET NULL,
  agent_id                 UUID REFERENCES agents(id) ON DELETE SET NULL,
  first_issue_id           UUID REFERENCES issues(id) ON DELETE SET NULL,
  onboarding_project_id    UUID REFERENCES projects(id) ON DELETE SET NULL,

  -- Platform 偏好（决定 handoff 和 resume 行为）
  platform_preference      TEXT,           -- 'web' | 'desktop' | null

  -- Cloud waitlist 支路（soft exit 记录）
  cloud_waitlist_email     TEXT,

  -- 约束
  CONSTRAINT current_step_valid CHECK (
    current_step IS NULL OR
    current_step IN ('questionnaire','workspace','runtime','agent','first_issue')
  ),
  CONSTRAINT onboarded_clears_step CHECK (
    onboarded_at IS NULL OR current_step IS NULL
  )
);

-- 只对未完成的做 index（完成后不查），analytics 用
CREATE INDEX idx_user_onboarding_incomplete
  ON user_onboarding (updated_at)
  WHERE onboarded_at IS NULL;
```

**几个关键决策的理由**：

- **`ON DELETE SET NULL`** 而不是 CASCADE：用户手动删了 onboarding 中创建的 workspace，不应丢失整条 onboarding 记录。保留痕迹作为 analytics 信号，同时支持 3.7 表中"回退到 Step 2" 的自愈逻辑
- **`onboarded_clears_step` 约束**：保证不会出现"已完成但还在某 step"的脏状态，发现非法组合直接 DB 层拒绝
- **Partial index `WHERE onboarded_at IS NULL`**：绝大多数用户最终会完成，索引只关注未完成 cohort，省空间且 query 更快
- **不存步骤时间戳历史**：步骤转化漏斗走 PostHog 事件系统（项目里 agent/j/db4fefb5 分支已经在做 analytics 基建）；state 表负责流程控制，事件系统负责分析。分工清晰，不混

### 4.2 API 设计

**读**：
```
GET /api/me/onboarding
→ 200 OK { current_step, questionnaire, workspace_id, ... }
→ 404 if never started (客户端 treat as "start fresh")
```

**写（每步结束时）**：
```
PATCH /api/me/onboarding
Body: {
  current_step: "workspace",          // 下一步
  questionnaire: { ... },             // 只在 Step 1 提交
  workspace_id: "ws_xxx",             // 只在 Step 2 提交
  // ... 对应字段
}
→ 200 OK { 完整 state }
```

**完成**：
```
POST /api/me/onboarding/complete
Body: { first_issue_id, onboarding_project_id }
→ 200 OK { onboarded_at, current_step: null }
```

**关键**：每步结束立即 PATCH server。不要在前端 batch 到最后一起提交——这是 resume 能工作的前提。

### 4.3 State 流转

```
状态机:
  (record not exists)
       ↓ 用户首次进 onboarding
  current_step: "questionnaire"
       ↓ PATCH 提交问卷
  current_step: "workspace"          + questionnaire
       ↓ PATCH 工作区创建成功
  current_step: "runtime"            + workspace_id
       ↓ PATCH runtime 选择
  current_step: "agent"              + runtime_id
       ↓ PATCH agent 创建
  current_step: "first_issue"        + agent_id
       ↓ POST /complete
  current_step: null                 + onboarded_at, first_issue_id, onboarding_project_id

支路（Cloud waitlist）:
  current_step: "runtime"
       ↓ 用户选 cloud waitlist
  current_step: null + onboarded_at + cloud_waitlist_email
```

---

## 五、当前代码影响面

### 5.1 后端（Go）

**新增**：
- Migration：`server/migrations/0xx_create_user_onboarding.up.sql` + `.down.sql`
- sqlc queries：`server/pkg/db/queries/onboarding.sql`（GetOnboarding / UpsertOnboarding / CompleteOnboarding）
- Handler：`server/internal/handler/onboarding.go`（GET / PATCH / POST）
- Router 挂载：`/api/me/onboarding` 路由组
- 可能需要：`GetUserOnboarding` 也需暴露给认证回调决定重定向（或前端自取）

**迁移 sqlc**：`make sqlc` 重生成。

### 5.2 前端（TypeScript / React）

**新增**：
- `packages/core/onboarding/types.ts` — `OnboardingState` 类型定义
- `packages/core/onboarding/queries.ts` — TanStack Query options
- `packages/core/onboarding/mutations.ts` — advance / complete mutation
- `packages/views/onboarding/steps/step-welcome.tsx` — 产品介绍屏（首次进入时展示；回访自动跳过）
- `packages/views/onboarding/steps/step-questionnaire.tsx` — 3 题问卷屏
- `packages/views/onboarding/steps/step-platform-fork.tsx` — web Step 3 的分流屏
- `packages/views/onboarding/steps/step-first-issue.tsx` — **关键**，aha moment 所在
- 可能拆分 `packages/views/onboarding/utils/personalization.ts` — Q1/Q2/Q3 → 下游映射的纯函数（方便单测）

**需要改动的现有文件**：
- `packages/views/onboarding/onboarding-flow.tsx` — 移除本地 `useState<OnboardingStep>`，改读 `useOnboardingStore`；每次 step 转换调 `advance` mutation
- `packages/views/onboarding/steps/step-welcome.tsx` — **删除**，内容合并到新的 step-questionnaire
- `packages/views/onboarding/steps/step-runtime.tsx` — web 分支改为渲染 `<StepPlatformFork />`
- `packages/views/onboarding/steps/step-agent.tsx` — 模板集改为 Coding / Planning / Writing / Assistant，按 Q2×Q3 预填，新增"Advanced"折叠区让用户改 name
- `packages/views/onboarding/steps/step-complete.tsx` — **替换**为 StepFirstIssue，或作为其前置过渡屏
- `packages/core/paths/resolve.ts` — `useHasOnboarded` 当前已从 store 读；联调期替换为 TanStack Query against `GET /api/me/onboarding`
- `packages/views/layout/use-dashboard-guard.ts` — guard 条件增加 `!hasOnboarded`，支持 "abandon 后回来自动回到 onboarding" 的 resume 行为
- `apps/web/app/(auth)/onboarding/page.tsx` — 调整 shell 以支持 resume（读 state 决定进入哪一步）
- `apps/desktop/src/renderer/src/components/window-overlay.tsx` — 同上
- `apps/desktop/src/renderer/src/stores/window-overlay-store.ts` — 可能需要 `WindowOverlay` 类型微调

**不变**：
- `packages/views/workspace/create-workspace-form.tsx` — 复用
- `packages/views/onboarding/steps/cli-install-instructions.tsx` — 仍用，在 CLI 分支里渲染
- 大部分 desktop 的 bundled daemon 启动逻辑 — Step 3 desktop 静默 pass 的前提

### 5.3 影响面估算

| 类别 | 数量 |
|---|---|
| 后端新文件 | ~4 |
| 后端修改文件 | 1–2（router） |
| 前端新文件 | ~6 |
| 前端修改文件 | ~10 |
| 测试新文件 | ~5（核心逻辑 + personalization 映射 + resume scenarios） |

---

## 六、成功指标（上线 30 天内评估）

参考调研结论设定：

| 指标 | 业界标杆 | Multica 目标 |
|---|---|---|
| Time-to-value | < 3 分钟 | Desktop 直达：≤ 3 min；Web→Desktop：≤ 5 min（含装机）；Web→CLI：≤ 8 min |
| Onboarding 完成率 | 60–80% | 目标 70% |
| Day 7 留存 | 25–40% | 目标 30% |
| Activation 率 | 40–60% | 目标 50% |
| Web→Desktop 转化（Step 3 fork） | in-product 高于 42% 冷推上限 | 目标 50–70% |

**第一漏斗目标**：workspace → runtime 连接率从当前水平提升至 80%+（主要靠 web 分流推 desktop 降 CLI 门槛）。
**第二漏斗目标**：runtime → 首个 issue 由产品主动创建，比例应接近 100%（因为 StepFirstIssue 自动完成这件事）。

---

## 七、已做的决策（不再讨论）

| 决策 | 选择 | 理由 |
|---|---|---|
| 前置问卷题数 | **3 题**：team_size / role / use_case | Notion 范式、调研甜蜜点；每题答案必须驱动下游内容 |
| 问卷 Q1 "已在用哪些 agent" | **不问**，daemon 自动探测 PATH | 技术事实不该问用户；扫 PATH 比问答更准 |
| 问卷 Q2 role | **问**，5 个具体选项 + Other | 驱动 Step 4 template 默认选择；用户画像数据回到一等位 |
| "Other" 选项机制 | **每题都有 Other**，点击展开 80 字符文本框 | Escape hatch；Q3 use_case_other 还会嵌入 Step 5 first issue prompt |
| 问卷必填 | **全可选**（Other 选了必填文本） | 给评估型用户零摩擦通道；0 选时 Continue 变 Skip |
| Welcome 步骤 | **保留独立 welcome**，但改造为"产品介绍屏"（不是打招呼）；只在首次进入时看到，回访 resume 自动跳过 | 多一次点击换来的是首次用户真正理解 Multica 是什么；Multica 无心智对标物，没有前置介绍就进问卷 = 用户没有 frame of reference；Welcome 不入后端 state，不影响 server schema |
| Web Step 3 分流 | **默认推 desktop**，CLI 次选，cloud waitlist 兜底 | 96% 是个人用户，desktop 是最快路径 |
| Cloud waitlist 放哪 | **Web Step 3 分流屏**，不作为主步骤 | 保留原方案 #3 的数据价值，但不侵占主流程 |
| Agent 模板 | **4 个**：Coding / Planning / Writing / Assistant（砍 Master） | Multica 服务多角色 agent 编排用户；Writer 不能被 Assistant 兜底 |
| Onboarding Project | **不算步骤**，Step 5 完成后台创建，侧边栏常驻 | Progressive disclosure 原则 |
| Resume 策略 | **恢复到上次 step，不过期，允许回退改答案** | 未见 abandon-return 数据前不提前优化 |
| Schema 方式 | **专门表 + JSONB 混合** | 稳定字段列化、灵活字段（问卷）JSON 化 |
| FK 删除行为 | **ON DELETE SET NULL**，不 CASCADE | 保留 analytics 痕迹 + 自愈能力 |
| 步骤时间戳 | **走 PostHog 事件系统**，不进 state 表 | 职责分离：state 管流程，events 管分析 |
| 进度 handoff 机制 | **纯后端 state**，不用 token 或 deep link | 用户 auth session 已绑身份，简化架构 |
| 开发顺序 | **前端全部搭完 → 后端实现 → 联调测试 → 上线** | 保持当前开发节奏不被后端阻塞；前端本身可以一个 step 一个 step 独立推进 |
| State 访问抽象 | **全部走 `useOnboardingStore()` 一个 hook**，component 严禁直接碰 storage | 换后端时只动这一个文件，component 不感知——让"先前端后后端"成本低的关键 |

---

## 八、开放问题 / 不在本次范围

- **Cloud agent runtime 本身**：本次只实现 waitlist 邮箱捕获，不做 cloud runtime。这是下一阶段的产品决策
- **Onboarding project sub-issue 文案的 iterate**：先上线现有文案（见 3.6），等真实用户反馈再打磨
- **A/B test 框架**：等用户量达到业界标准（每组 ≥500）再启动，现阶段全量发
- **个性化 Day 3 邮件**：问卷只问 3 题，剩余的用户画像数据（团队规模、角色等）可以后置到运营邮件收集，本次不实现
- **Onboarding 完成后的 re-engagement**：如"用户 7 天没创建第 2 个 agent 时发通知"，属于 retention loop，不属于 onboarding
- **自定义 agent template**：当前 3 个硬编码模板够用，自定义模板留到后面

---

## 九、执行计划

### 9.1 详细执行文档

本提案评审通过后，拆出 `docs/plans/2026-04-21-onboarding-redesign.md`，按现有 plan 文档格式（参考 `docs/plans/2026-04-16-remove-onboarding-and-fix-daemon-bootstrap.md`）精确到文件 + 行号 + 代码片段。

### 9.2 执行阶段

**原则：前端全部搭完 → 后端实现 → 联调测试 → 上线。**

目的是让当前开发节奏不被后端阻塞——前端可以一个 step 一个 step 独立迭代，每完成一个 step 都能在浏览器里直接看到效果。后端在前端定稿之后一次性实现，联调阶段统一解决跨端 resume 等场景。

**前端阶段**（按顺序推进，每个 step 独立可交付）：

1. **建立 `useOnboardingStore()` 骨架**（已完成）——位于 `packages/core/onboarding/`。dev 期间是内存 Zustand store（刷新重置，方便迭代），联调阶段换成 TanStack Query + PATCH mutation。严禁 component 绕过
2. **Step 1（welcome + 问卷拆两屏）**：新建 `step-welcome.tsx`（产品介绍，首次进入时展示）+ `step-questionnaire.tsx`（3 题）；抽出 `<OptionCard>` / `<OtherOptionCard>` 复用组件
3. **Step 2（workspace）**：基本保留，接入 `useOnboardingStore()`
4. **Step 3（runtime）**：在 web 分支里新建 `step-platform-fork.tsx`；desktop 分支保留静默自动；CLI 分支加预期管理和 60s fallback
5. **Step 4（agent）**：模板集从 3 扩成 4（加 Writing），按 Q2×Q3 预填 template + provider（provider 来自 daemon 探测），移除手填 name 的强制性
6. **Step 5（first issue）**：新建 `step-first-issue.tsx`，这是 aha moment 发生的地方；`use_case=other` 时把 `use_case_other` 嵌入 prompt
7. **Flow orchestrator 改造**：`onboarding-flow.tsx` 改由 `useOnboardingStore()` 驱动，不再用本地 useState 管 step 切换
8. **Web + Desktop shell 适配**：读 store 决定进入哪一步，支持单浏览器内的 resume

**后端阶段**：

9. Migration + sqlc queries + handler + router（API shape 见 4.2）
10. 按 4.1 schema 实现 `user_onboarding` 表 + partial index + 约束

**联调阶段**：

11. `useOnboardingState()` 实现从 localStorage 切换为 TanStack Query + PATCH mutation——**component 0 改动**，这是 hook 抽象的回报
12. 跨端 / 多 session resume 全场景验证（3.7 表）
13. E2E 覆盖 4 类用户路径 + 分流屏三条支路 + resume 一条

建议独立 worktree 开发（参考 `superpowers:using-git-worktrees`），避免污染主 checkout。

### 9.3 测试阶段

**本地自测**（按用户类型逐一跑）：
- A 类：solo + Claude Code + coding → 最短路径 3 分钟
- B 类：team + Claude Code + coding/planning → 完成后侧边栏 "Invite teammates" 置顶
- C 类：无 agent + 评估 → web 分流选 cloud waitlist
- D 类：solo + writing → Assistant 模板 + 对应 first issue 文案

**Resume 场景**（按 3.7 表逐一验证）：
- Web 中途关浏览器 → 重开恢复
- Web → desktop 跨端 handoff
- Web 选下载未装 → 回 web 的"waiting"屏
- 已完成用户重登录 → 跳过 onboarding

**E2E** 测试必须覆盖：
- 完整 happy path（至少 desktop A 类）
- Resume 一条
- 分流屏三条路径各一条

**上线指标监控**：PostHog 看板跟踪第六节定义的 5 个 KPI，上线后每周 review 一次，2 周内若主指标偏离 20%+ 需排查。

---

## 十、调研参考

### 核心理论与激活
- [Chameleon — How to find your product's "Aha" moment](https://www.chameleon.io/blog/successful-user-onboarding)
- [Amplitude — The "Aha" Moment: A Guide](https://amplitude.com/blog/aha-moment)
- [Growth Letter — Slack's $3B Growth Loop](https://www.growth-letter.com/p/slacks-3-billion-growth-strategy)
- [June.so — Activation Playbook](https://www.june.so/blog/activation-playbook)

### 开发者工具特有数据
- [Daily.dev — Developer Onboarding Optimization](https://business.daily.dev/resources/developer-onboarding-optimization-from-first-click-to-paying-customer/)
- [Startup Design Journal — Hidden Micro-Friction Killing Conversion](https://startupdesignjournal.com/p/the-hidden-micro-friction-thats-killing)

### 问卷 / 表单 drop-off
- [involve.me — 6→3 题 +11% case](https://www.involve.me/blog/case-study-how-we-use-an-onboarding-survey-in-a-saas-product)
- [SaaSFactor — Why Users Drop Off During Onboarding](https://www.saasfactor.co/blogs/why-users-drop-off-during-onboarding-and-how-to-fix-it)
- [GrowthMentor — Friction Case Study](https://www.growthmentor.com/blog/user-onboarding-friction/)
- [Formbricks — Essential Onboarding Survey Questions](https://formbricks.com/blog/onboarding-survey-questions)

### Progressive Disclosure
- [LogRocket — Progressive Disclosure](https://blog.logrocket.com/ux-design/progressive-disclosure-ux-types-use-cases/)
- [Pendo — Onboarding, Progressive Disclosure, Memory](https://www.pendo.io/pendo-blog/onboarding-progressive-disclosure/)
- [Interaction Design Foundation — Progressive Disclosure](https://ixdf.org/literature/topics/progressive-disclosure)

### Notion / Linear 案例
- [Candu — How Notion Crafts Personalized Onboarding](https://www.candu.ai/blog/how-notion-crafts-a-personalized-onboarding-experience-6-lessons-to-guide-new-users)
- [Appcues Goodux — Notion's Lightweight Onboarding](https://goodux.appcues.com/blog/notions-lightweight-onboarding)
- [DesignerUp — 200 Onboarding Flows Studied](https://designerup.co/blog/i-studied-the-ux-ui-of-over-200-onboarding-flows-heres-everything-i-learned/)

### Schema / 持久化
- [Shekhar Gulati — When to use JSON data type](https://shekhargulati.com/2022/01/08/when-to-use-json-data-type-in-database-schema-design/)
- [TigerData — Wide vs Narrow Postgres Tables](https://www.tigerdata.com/learn/designing-your-database-schema-wide-vs-narrow-postgres-tables)
- [DbSchema — PostgreSQL JSONB Operators](https://dbschema.com/blog/postgresql/jsonb-in-postgresql/)
- [Pravin Tripathi — Start and Resume Journey for Onboarding](https://medium.com/@pravinyo/approaches-for-start-and-resume-journey-for-user-onboarding-to-platform-part-i-e077c73b4cd7)

### A/B 测试 & 分段
- [Appcues — A/B Testing Onboarding Flows](https://www.appcues.com/blog/flow-variation-a-b-testing)
- [M Accelerator — A/B Testing Onboarding Guide](https://maccelerator.la/en/blog/entrepreneurship/ultimate-guide-to-ab-testing-onboarding-flows/)
- [CXL — Segment A/B Test Results](https://cxl.com/blog/segment-ab-test-results/)

### 2025 综合最佳实践
- [Aakash Gupta — 10 Customer Onboarding Best Practices for PMs 2025](https://www.aakashg.com/customer-onboarding-best-practices/)
- [ProductLed — SaaS Onboarding Best Practices 2025](https://productled.com/blog/5-best-practices-for-better-saas-user-onboarding)
- [Branch — Desktop-to-App Conversions](https://www.branch.io/resources/blog/optimizing-desktop-web-to-app-conversions/)
