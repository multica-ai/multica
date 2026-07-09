# Agent 快速创建 — 三阶段实施计划

> Status: Draft (设计阶段,未动工)
> Owner: TBD
> Last updated: 2026-05-13

## TL;DR

- **目标**:降低用户创建 Agent 的门槛,从「手工填表 + 一个个挑 skill」演进到「一键模板」「AI 推荐 skill」「AI 直接创建 agent」三档
- **三阶段**:Template(必做、独立)→ Skill Finder(AI 推荐 skill)→ AI Create Agent(AI 直接创建)
- **架构关键**:Phase 2/3 复用现有 Quick-create Issue 基础设施(派任务给 agent + tool calling + inbox 通知),不引入新 LLM 调用路径
- **不需要新基础设施**:无 SSE、无 server-side LLM、无新 WS channel
- **soft blocker**:两处 routine 重构(`createSkillWithFiles` TX 拆分、skill 同名 find-or-create)
- **不做**:接入 Anthropic 官方 marketplace(plugin 体系跟单体 skill 形态不匹配)、接入 ClawHub(战略对位错误 + 实际使用率低,见 §5)

---

## 1. 背景与目标

### 1.1 当前现状

当前用户创建一个 Agent 需要走的步骤:

1. 进 `/agents` 页面 → 点 "Create Agent"
2. 手工填 name / description / runtime / model
3. 手工写 instructions(空白文本框,用户自己思考措辞)
4. 创建完后进 Agent 详情页 → 点 "Add Skill" → 一个一个挑 skill 关联
5. 如果 workspace 还没有需要的 skill,得先去别处建/导入 skill(`POST /api/skills/import` 支持 skills.sh / GitHub / ClawHub 三种 URL)

**痛点**:
- 用户得**预先知道**自己需要哪些 skill,这要求他对 skill 生态熟悉
- 写 instructions 是空白文本编辑,大多数用户不知道写什么
- 跨多页操作,体感上"创建一个能用的 Agent"是个项目,不是个动作

### 1.2 三阶段方案

| Phase | 提供给用户的能力 | 是否需要 AI | 独立可发布 |
|---|---|---|---|
| **1. Template** | 选模板 → 自动 import 模板带的 skill + 预填 instructions | 否 | ✅ |
| **2. Skill Finder** | 描述需求 → AI 推荐 skill 列表 → 一键导入到 workspace | ✅ | ✅(独立功能,任何场景都能用) |
| **3. AI Create Agent** | 描述需求 → AI 自己 find skill + 写 instructions + 创建 agent | ✅ | 依赖 Phase 2 |

每个 phase **本身有用户价值**,不需要等下一个 phase 才能用:
- Phase 1 用户能用模板创建 agent,即使后两阶段没做
- Phase 2 用户能在任何地方"用 AI 找 skill"(创建 agent 时、给现有 agent 加 skill 时、单纯逛 skill 时)
- Phase 3 是 1+2 的组合

### 1.3 不在范围内

明确不做的事(及理由,见 §5):
- 接入 Anthropic 官方 plugin marketplace(`anthropics/claude-plugins-official`)
- 接入 ClawHub 的"发现/搜索"层(import 路径已经存在,但是死代码,建议下线)
- 让 AI 直接装 skill 到用户本地 `~/.claude/skills/`(npx skills CLI 行为)
- Server-side LLM 调用(后端目前没有 LLM SDK,这条路引入新基础设施,而 Quick-create 模式可以避开)

---

## 2. 关键概念回顾

> 这一节给没参与前期讨论的同事看。已经熟悉 skill 系统的可跳到 §3。

### 2.1 Skill 是什么

Skill 是一个**按需加载的能力包**,本质是 SKILL.md 文件 + 可选附件。Anthropic 2025-12 把它发布为开放标准(agentskills.io),Cursor / OpenAI / GitHub Copilot 等都已采纳——同一份 SKILL.md 跨多个 agent 工具都能用。

每个 runtime(Claude Code / Cursor / Codex 等)启动时**自动扫**自己约定的目录(`~/.claude/skills/`、`.cursor/skills/` 等),读 SKILL.md 的 frontmatter 形成"我手上有这些 skill"的清单注入 system prompt。具体 skill 正文只在被触发时才进 context。

### 2.2 Multica 的 Skill 数据模型

3 张表(migration `008_structured_skills.up.sql`):

| 表 | 关键字段 |
|---|---|
| `skill` | `id, workspace_id, name, description, content (=SKILL.md 正文), config (含 origin 元数据)` |
| `skill_file` | `skill_id, path, content`(SKILL.md 的附件,如 examples/*.md、scripts/*.py) |
| `agent_skill` | `agent_id, skill_id`(M:N 关联) |

**关键约束**:`UNIQUE(workspace_id, name)` — 同 workspace 内 skill 名字必须唯一。

### 2.3 Skill 流转链路(数据库 → runtime)

任务运行时,skill 从 PG 到 runtime 的完整路径:

```
1. 数据库:skill + skill_file + agent_skill 三张表的行

2. Daemon claim 任务:
   POST /api/runtimes/{runtimeId}/tasks/claim
   handler/daemon.go:1018-1098 (ClaimTaskByRuntime)
   → service/task.go:1447-1463 (LoadAgentSkills)
   → 把 agent 关联的所有 skill 全文塞进 HTTP 响应

3. Daemon 算工作目录:
   server/internal/daemon/execenv/execenv.go:114, 124
   workDir = {WorkspacesRoot}/{wsID}/{shortTaskID}/workdir

4. Daemon 按 runtime 算 skill 目录:
   server/internal/daemon/execenv/context.go:121-158 (resolveSkillsDir)
   claude   → {workDir}/.claude/skills
   cursor   → {workDir}/.cursor/skills
   codex    → 特殊:{codexHome}/skills

5. Daemon 把字符串写成磁盘文件:
   context.go:175-204 (writeSkillFiles)
   核心就两行 os.WriteFile

6. Daemon 启动 runtime,cwd = workDir
   runtime 自己扫 .claude/skills/(等)→ 加载 frontmatter

7. 任务结束:os.RemoveAll(workDir)
   PG 是真相源,workDir 是每次任务临时复印件
```

**核心 invariant**:Multica 不教 runtime 怎么用 skill,只把文件摆到 runtime 已经会扫的位置。

### 2.4 Template = Instructions + Skill 引用

Template 是个**静态 JSON 定义**,包含:
- 预写好的 instructions
- 一组 skill 引用(用 URL 指向 skills.sh / GitHub)

用户选模板时,后端:
1. 对每个 skill 引用,**复用现有 `/api/skills/import` 的 fetcher**(`fetchFromSkillsSh` / `fetchFromGitHub`)拉内容
2. 物化到 workspace(同名复用 / 新建)
3. CreateAgent + setAgentSkills
4. 整个流程一个事务

skill 引用为什么用 URL 而不是内联 SKILL.md 内容:
- 复用现有 import 基础设施,零新代码
- skill 内容跟 GitHub 同步,不需要 vendoring 进 multica 仓库
- 模板 JSON 体积小,git review 友好

### 2.5 Quick-create Issue 模式(Phase 2/3 复用的基础设施)

当前 `POST /api/issues/quick-create`(handler/issue.go:877-982)的流程:

```
1. 后端 enqueue 任务:
   - agent_task_queue 加一行,issue_id = NULL,context JSONB = {type: "quick-create", prompt: ...}
   - 立即返回 202 Accepted + task_id

2. Daemon claim 任务时识别 quick-create:
   - 检查 task.Context != nil AND !task.IssueID.Valid
   - 解析为 QuickCreateContext (service/task.go:1810-1811)

3. Daemon 构造 prompt:
   - daemon/prompt.go:45-106 (buildQuickCreatePrompt)
   - 把用户的自然语言 prompt 作为语义核心
   - 加上"调用 multica issue create CLI 命令"的指令

4. Agent 跑 LLM + tool calling:
   - LLM 输出形如 `multica issue create --title="..." --description="..."` 的命令
   - daemon 执行 CLI 命令,CLI 调 POST /api/issues 创建 issue
   - CLI 自动在请求里带上 MULTICA_QUICK_CREATE_TASK_ID env(daemon/daemon.go:2081)
     → 让创建出来的 issue 带 origin_type='quick_create' + origin_id=<task_id>

5. 后端 link + 通知:
   - 完成检测:GetIssueByOrigin(workspace_id, "quick_create", task_id)
   - LinkTaskToIssue(task_id, issue_id) 把任务行的 issue_id 补上
   - 写 inbox_item 通知用户(notifyQuickCreateCompleted, service/task.go:1908-1920)
```

**关键洞察**:这个模式**完全通用化**了。复用它只需要:
1. 新的 context JSONB type(比如 `"skill-find"`、`"agent-create"`)
2. 新的 prompt builder
3. 新的"完成检测 + inbox 通知"

不需要任何 daemon / 任务队列层面的改动。

---

## 3. 三阶段详细设计

### Phase 1:Agent Template

**目标**:用户选模板 → 一键得到一个可用的 agent(自带 skill + instructions),不需要 AI 参与。

#### 设计

- **Template 定义存放**:静态 JSON,commit 在 `server/internal/agenttmpl/templates/*.json`
- **Template JSON 形态**:
  ```json
  {
    "slug": "code-reviewer",
    "name": "Code Reviewer",
    "description": "审代码用的 agent",
    "instructions": "你审代码,关注 N+1 查询、错误处理、类型安全...",
    "skills": [
      { "source_url": "https://skills.sh/obra/superpowers/tdd" },
      { "source_url": "https://github.com/foo/bar/tree/main/skills/code-style" }
    ]
  }
  ```
- **新 endpoint**:`POST /api/agents/from-template`
  - 请求:`{template_slug, name, runtime_id, ...overrides}`
  - 后端流程(**全部在一个事务里**):
    1. 加载 template JSON
    2. 对每个 skill source_url:
       - 调用 `detectImportSource(url)`(skill.go:586-617)分发到对应 fetcher
       - 通过 GetSkillByWorkspaceAndName 检查 workspace 是否已有同名 skill
         - 有 → 复用现有 skill_id
         - 无 → 调 `createSkillWithFilesInTx`(待重构,见 §4)物化
    3. `CreateAgent`(复用 agent.go:CreateAgent 的内部逻辑)
    4. 批量 `AddAgentSkill` 关联
  - 响应:`{agent: {...}, imported_skill_ids: [...], reused_skill_ids: [...]}`
- **前端**:`CreateAgentDialog`(packages/views/agents/components/create-agent-dialog.tsx)加 "From template" 模式,跟现有 manual / duplicate 模式并列
  - 模板选择器 → 预览(instructions + skill 列表)→ 提交调新 endpoint
  - 响应里的 `reused_skill_ids` 用 toast 提示"以下 skill 已存在,沿用了 workspace 现有版本"

#### 起步模板清单(初版,可调)

- `code-reviewer` — 代码审查
- `tdd-pair` — TDD 配对编程
- `db-reviewer` — 数据库 / SQL 审查
- `pr-summarizer` — PR 摘要
- `docs-writer` — 文档撰写

具体每个模板选哪些 skill URL,在 Phase 1 启动时单独决定(需要逛 skills.sh 选高质量 skill)。

#### Phase 1 改动清单

| 文件 / 位置 | 改动 |
|---|---|
| `server/internal/agenttmpl/`(新包) | 加载 JSON 模板的代码 |
| `server/internal/agenttmpl/templates/*.json`(新文件) | 5 个起步模板 |
| `server/internal/handler/agent.go` | 新 handler `CreateAgentFromTemplate` |
| `server/internal/handler/skill_create.go` | **重构**:拆出 `createSkillWithFilesInTx` 变体(见 §4) |
| `server/pkg/db/queries/skill.sql` | 加 `GetSkillByWorkspaceAndName`(见 §4) |
| `server/cmd/server/router.go` | 注册新 endpoint |
| `packages/views/agents/components/create-agent-dialog.tsx` | 加 template 模式 |
| `packages/core/api/agent.ts` | 加 `createAgentFromTemplate` API 调用 |
| `packages/views/agents/components/template-picker.tsx`(新文件) | 模板选择器组件 |

### Phase 2:Skill Finder

**目标**:用户用自然语言描述需求(如"我想审 SQL"),AI 推荐一组 skill,用户勾选一键导入到 workspace。

#### 设计

- **架构选型**:走 quick-create 模式,**不是后端直接调 LLM**
- **新 endpoint**:`POST /api/skills/find`
  - 请求:`{prompt, agent_id}`(agent_id 是用来跑这个 LLM 任务的 agent,跟 Quick-create Issue 一样要求预先有 agent)
  - 后端流程:
    1. enqueue 任务:`agent_task_queue` 加一行,context JSONB = `{type: "skill-find", prompt}`
    2. 返回 202 + task_id
- **Daemon prompt builder**:`daemon/prompt.go` 加 `buildSkillFindPrompt`(类比 buildQuickCreatePrompt)
  - 喂给 agent 的 prompt 大致:
    ```
    用户需求:{user_prompt}
    
    你的任务:从以下 curated skill 清单里选 3-5 个最相关的推荐给用户。
    
    可选 skill 清单(JSON):
    {curated_skill_index}
    
    输出:调用 `multica skill find --output-results '<JSON>'` 命令,
    JSON 形态为 [{name, description, source_url, reason}, ...]
    ```
- **CLI 命令**(新):`multica skill find --output-results <JSON>`
  - 不发起 HTTP 请求,只把 JSON 写到 daemon 通过 env 指定的临时文件
  - daemon 读这个文件,把内容塞进 inbox notification 的 payload
- **Curated skill 索引**:`server/internal/agenttmpl/skill_index.json`(新文件)
  - 几十到上百条精选 skill,每条:`{name, description, source_url, tags, install_count}`
  - 维护方式:工程师/产品手工维护,代码 review 卡内容质量
  - MVP **不做**实时 GitHub Code Search 或 skills.sh 爬虫
- **完成通知**:写 inbox_item,type = `skill_find_done`,payload 含推荐结果数组
- **前端**:
  - 独立"Find Skill"页面(`/skills/find` 或 `/skills?ai=true`)
  - skill list page 上"用 AI 找 skill"按钮入口
  - 用户输入 prompt → 提交 → 等通知 → inbox item 里展示 skill 卡片(name + description + source_url + reason)
  - 用户勾选 → 一键批量调现有 `POST /api/skills/import`(每个 skill 一次,可考虑加 batch endpoint 但 MVP 不必要)

#### Phase 2 改动清单

| 文件 / 位置 | 改动 |
|---|---|
| `server/internal/handler/skill.go` | 新 handler `FindSkill`(enqueue task) |
| `server/internal/service/task.go` | 加 `EnqueueSkillFindTask` + 完成检测 + inbox 通知 |
| `server/internal/daemon/prompt.go` | 加 `buildSkillFindPrompt` |
| `server/internal/daemon/daemon.go` | 加 `SkillFindContext` 识别 + env 注入 |
| `server/cmd/multica/cmd_skill.go` | 加 `find --output-results` 子命令 |
| `server/internal/agenttmpl/skill_index.json`(新文件) | curated 清单 |
| `packages/views/skills/components/find-skills-dialog.tsx`(新文件) | UI |
| `packages/core/api/skill.ts` | 加 `findSkills` API |
| `packages/views/inbox/items/skill-find-result.tsx`(新文件) | inbox item 渲染 |

### Phase 3:AI Create Agent

**目标**:用户描述需求,AI 自己 find skill + 写 instructions + 创建 agent。

#### 设计

- **架构选型**:走 quick-create 模式,**组合 Phase 2 的 find 能力 + 新的 agent create CLI**
- **新 endpoint**:`POST /api/agents/ai-draft`
  - 请求:`{prompt, host_agent_id}`(host_agent_id 是跑这个元任务的 agent)
  - 后端:enqueue 任务,context = `{type: "agent-create", prompt}`,返回 202 + task_id
- **Daemon prompt builder**:`buildAgentCreatePrompt` 指挥 agent 三步走:
  ```
  1. 调用 `multica skill find --output-results ...` 选 skill
     (或直接看 curated 清单选)
  2. 基于选定 skill 写 instructions
  3. 调用 `multica agent create --name ... --instructions ... --skill-ids ...`
     创建 agent 并关联 skill
  ```
- **CLI 命令**(新):`multica agent create`
  - 后端 handler 已存在(handler/agent.go:CreateAgent),只需要绑 CLI(~50 行)
  - 创建时带 `MULTICA_AI_DRAFT_TASK_ID` env,服务端用它做 origin 标记 + LinkTaskToAgent
- **完成通知**:inbox_item type = `agent_draft_done`,payload 含 agent_id + 摘要
- **前端**:`CreateAgentDialog` 加 "AI" 模式
  - 输入需求 → 提交 → 等通知 → inbox 通知里点击 → 跳新 agent 详情页(用户在那儿编辑/调整)

#### Phase 3 改动清单

| 文件 / 位置 | 改动 |
|---|---|
| `server/internal/handler/agent.go` | 新 handler `AIDraftAgent`(enqueue task) |
| `server/internal/service/task.go` | 加 `EnqueueAgentDraftTask` + 完成检测 + inbox 通知 |
| `server/internal/daemon/prompt.go` | 加 `buildAgentCreatePrompt` |
| `server/cmd/multica/cmd_agent.go` | 加 `create` 子命令(handler 已有) |
| `packages/views/agents/components/create-agent-dialog.tsx` | 加 "AI" 模式 |
| `packages/core/api/agent.ts` | 加 `aiDraftAgent` API |
| `packages/views/inbox/items/agent-draft-result.tsx`(新文件) | inbox item 渲染 |

---

## 4. Blocker 清单与修复方案

### 4.1 [SOFT] `createSkillWithFiles` 不可组合事务

**问题**:`server/internal/handler/skill_create.go:21-71` 这个函数自己 `Begin()` 一个事务,执行完 `Commit()`。Phase 1 需要在外层事务里**多次**调用它(import N 个 skill + createAgent + setAgentSkills 都在一个 TX),但现在没法这么用。

**影响范围**:Phase 1

**修复方案**:

```go
// 拆成两个函数(保持原 API 向后兼容):

// 新增:接受外部 qtx,不管事务
func createSkillWithFilesInTx(
    ctx context.Context,
    qtx *db.Queries,
    input skillCreateInput,
) (*SkillWithFilesResponse, error) {
    // 不 Begin/Commit,只调 qtx.CreateSkill + qtx.UpsertSkillFile loop
}

// 改造:原函数变成包装层,内部调 InTx 版
func (h *Handler) createSkillWithFiles(
    ctx context.Context,
    input skillCreateInput,
) (*SkillWithFilesResponse, error) {
    tx, _ := h.TxStarter.Begin(ctx)
    defer tx.Rollback()
    qtx := h.Queries.WithTx(tx)
    result, err := createSkillWithFilesInTx(ctx, qtx, input)
    if err != nil { return nil, err }
    tx.Commit()
    return result, nil
}
```

旧调用方完全不变。Phase 1 新 endpoint 自己 Begin,然后多次调 `*InTx` 变体,最后统一 Commit。

**工作量**:小(< 100 行重构)

### 4.2 [SOFT] Skill 同名冲突

**问题**:`skill` 表有 `UNIQUE(workspace_id, name)` 约束。Phase 1 模板导入时,如果模板里的 skill 跟 workspace 已有 skill 同名,INSERT 会报 PG 错误 23505,整个 from-template 流程挂掉。

**影响范围**:Phase 1

**修复方案**:加 find-or-create 模式:

1. 新 query `GetSkillByWorkspaceAndName`(`server/pkg/db/queries/skill.sql`)
2. Phase 1 流程改成:
   - 对每个模板 skill,先查 workspace 是否已有同名
   - 有 → 复用现有 skill_id,跳过 import
   - 无 → 调 `createSkillWithFilesInTx` 物化
3. 响应里返回 `reused_skill_ids: [...]`,前端 toast "以下 skill 已存在,沿用现有版本"

**不选择"覆盖"或"加后缀"的原因**:用户可能已经改过本地版本,覆盖会丢用户修改;加后缀污染 skill 列表。

**工作量**:小(< 50 行 + 1 条 sqlc query)

### 4.3 [SOFT] 缺 `multica skill find` CLI

**影响范围**:Phase 2

**方案**:加一个 CLI 子命令,模仿 `multica skill import` 的实现(`server/cmd/multica/cmd_skill.go:55-60, 323-357`)。**注意**:这个命令不发 HTTP 请求,只是 LLM agent 用来"输出推荐结果"的 channel——它把 LLM 推荐的 JSON 写到 daemon 指定的临时文件,daemon 读完塞进 inbox notification。

**工作量**:小(~80 行)

### 4.4 [SOFT] 缺 `multica agent create` CLI

**影响范围**:Phase 3

**方案**:后端 handler 已有(`handler/agent.go:CreateAgent`),只需在 `server/cmd/multica/cmd_agent.go` 加 `create` 子命令。

**工作量**:小(~50 行)

### 4.5 [非 blocker] System Agent 问题

**之前误判为 hard blocker,实际不是**:

Quick-create Issue 当前的设计就要求用户**预先有一个 agent** 才能用——AI 路径不为"零 agent 起步"服务。Phase 2/3 沿用这个前提,所以**新 workspace 没 agent 时 AI 功能不可用**是符合现有产品模型的,不需要 bootstrap 一个 system agent。

产品自然解锁路径:
1. 新用户进 workspace
2. 用 **Phase 1 Template**(无需 AI、无需现有 agent)创建第一个 agent
3. 之后 Phase 2/3 即可用,host_agent 就用刚创建的那个

---

## 5. 关键设计决策(及理由)

### 5.1 为什么不接 Anthropic 官方 marketplace?

**结构错配**。Anthropic 官方 marketplace(`anthropics/claude-plugins-official`)是 **plugin 体系**:每个 plugin 是个 bundle,包含 `.claude-plugin/plugin.json` + `skills/` + `agents/` + `hooks/` + `.mcp.json`。

Multica 只有**单体 skill**(SKILL.md + skill_file),没有 plugin / bundle 概念。要接入得新写 plugin parser + 拆分逻辑,工作量大,而 skills.sh 已经覆盖了同一批高质量内容(skills.sh 后端就是 GitHub raw,绝大多数 skill 作者就在 GitHub 上,Anthropic plugin 体系里的 skill 通常也在作者的 GitHub repo 里有单体副本)。

### 5.2 为什么走 quick-create 模式而不是后端直接调 LLM?

代码事实:`server/` 目前**完全没有任何 LLM SDK**(grep `anthropic-sdk-go` / `openai-go` / 任何 LLM provider 都是 0 命中)。所有 LLM 调用都通过 daemon → runtime → CLI 这条路。

走 quick-create 模式的优势:
- **不引入新基础设施**(SSE / LLM client / API key 管理)
- **复用 agent 的 instructions / model / runtime 配置**(用户已经在某个 agent 里配置过的偏好自动生效)
- **统一计费 / 用量监控**(LLM 调用都计在用户 agent 的 quota 里)

代价:
- 用户得**预先有一个 agent**(参见 §4.5,这跟 Quick-create Issue 现状一致)
- LLM 调用通过 daemon 多一跳,延迟略增(但不阻塞 202 响应)

### 5.3 为什么 Skill Finder 是 endpoint 不是 SKILL.md?

**Skill Finder 名字里的 "Skill" 是它的产物(找的是 skill),不是它自己实现成 SKILL.md**。

如果做成 SKILL.md 文件:
- 它得装进某个 agent 里才能用 → 单点功能变得需要前置配置
- skill 教 agent 调什么?调 `npx skills`(装到本地,目标错)?调 Multica API(那要写 tool channel,绕一大圈)
- AI 创建 Agent(Phase 3)那条路要"启动 agent → agent 调 skill → skill 调 tool",链路复杂三倍

做成 endpoint:
- 用户独立可用(独立 UI 入口)
- AI 创建 Agent 后端直接调 endpoint,两个功能共用一段逻辑
- 简单

### 5.4 Curated Skill 索引 vs 实时搜索

**MVP 用 curated 清单**(几十条精选 URL + 摘要 commit 在 repo 里)。理由:
- 质量可控
- 不踩 GitHub Code Search rate limit
- 不被 LLM 编 URL(LLM 知识 cutoff + hallucinate URL 是真问题)
- 维护成本低

进阶可加 `search_skills(query)` tool 实时打 GitHub Code Search,等用户反馈"清单太窄"再做。

### 5.5 不做 ClawHub(顺手清理建议)

**现状**:`POST /api/skills/import` 当前支持 3 个 source(`fetchFromClawHub` skill.go:642-744、`fetchFromSkillsSh` skill.go:757-879、`fetchFromGitHub` skill.go:1363-1463)。ClawHub 是个独立 HTTP 客户端,不复用 GitHub 基础设施。

**判断**(详见之前讨论):
- ClawHub 服务的是 OpenClaw 平台(Multica 同生态位竞品的内容生态)
- UI 没有发现/搜索层,用户只能粘 URL,而 ClawHub 装机量远低于 skills.sh,用户主动逛的概率极低
- 独立代码路径,API 演进时单独跟进

**建议**(独立于本计划,可以一起做也可以延后):
- 跑 `SELECT count(*) FROM skill WHERE config->'origin'->>'type' = 'clawhub'` 看实际使用量
- 接近 0 → 渐进下线(先去 UI SourceCard,后续 release 删 fetcher)
- 有量 → 留着,但仍不为它做新功能

---

## 6. 实施依赖与排期

```
[Phase 1] Template
  └── 独立,无依赖
  └── 包含 2 个 soft blocker 的修复(§4.1 §4.2)
       ↓
[Phase 2] Skill Finder
  └── 依赖 Phase 1 中的 skill import 路径(已存在,沿用)
  └── 含 1 个 soft blocker(§4.3)
       ↓
[Phase 3] AI Create Agent
  └── 依赖 Phase 2(复用 find skill 能力)
  └── 含 1 个 soft blocker(§4.4)
```

**真实排期建议**:
- Phase 1 可单独发版,有独立价值
- Phase 2 独立可发版(找 skill 是高频独立场景)
- Phase 3 等 Phase 2 ready 后开始

每个 phase 启动时单独开 PR 设计 doc,本文档只是路线图。

---

## 7. 风险与缓解

| 风险 | 缓解 |
|---|---|
| GitHub rate limit(模板 import 多个 skill 时) | 已有 `GITHUB_TOKEN` env 支持(skill.go:1163-1166),5000/h 配额够用。生产环境确保配置 |
| 模板里引用的 skill repo 被作者删除 | from-template handler 容错:某个 skill fetch 失败 → 整个事务回滚,前端展示具体哪个 URL 挂了。模板自己也定期 review |
| LLM 推荐编造 URL(Phase 2) | 用 curated 清单作为 context,**不让 LLM 自由发挥 URL**,推荐范围限定在清单内 |
| Phase 3 LLM 写出离谱 instructions | 用户在 inbox 通知里点击 → 跳新 agent 详情页**编辑模式**,不直接进入"已就绪"状态。用户必须确认 |
| 模板格式后续要演进(加字段) | Template JSON 加 `version` 字段,后端按 version 兼容老格式 |
| Curated skill 清单过时(作者改 repo / 删 skill) | 加 CI 任务定期跑一遍清单 URL,挂掉的报警通知维护者 |

---

## 8. 不在本文档范围(已识别的下一步话题)

- 跨 workspace 模板共享 / marketplace 化(用户能把自己的 agent 存成模板分享)
- 实时 GitHub Code Search tool(Phase 2 进阶)
- Server-side LLM 调用基础设施(如果未来需要 streaming 等场景)
- ClawHub 下线决策(独立讨论,见 §5.5)
- Skill 版本管理(workspace skill 版本号 / 升级提示)

---

## 附录 A:代码索引

> 给接手开发的同事的快速参考。每条 file:line 都在本计划里被引用过,记录在这里方便跳转。

| 主题 | 位置 |
|---|---|
| Skill DB 模型 | `server/migrations/008_structured_skills.up.sql:4-32` |
| Skill 创建 handler + 事务 | `server/internal/handler/skill.go:143-162` + `skill_create.go:21-71` |
| Skill import 入口(支持 3 个 source) | `server/internal/handler/skill.go:1538` |
| Skill import source 分发 | `server/internal/handler/skill.go:586-617` (`detectImportSource`) |
| Skills.sh fetcher | `server/internal/handler/skill.go:757-879` (`fetchFromSkillsSh`) |
| GitHub fetcher | `server/internal/handler/skill.go:1363-1463` (`fetchFromGitHub`) |
| ClawHub fetcher | `server/internal/handler/skill.go:642-744` (`fetchFromClawHub`) |
| Agent 创建 handler | `server/internal/handler/agent.go:380-399` (request) + `:422-564` (CreateAgent) |
| Agent 创建 sqlc | `server/pkg/db/queries/agent.sql:19-25` |
| Agent-Skill 关联 sqlc | `server/pkg/db/queries/agent.sql:86-103` |
| 当前 Agent Duplication(前端模式) | `packages/views/agents/components/agents-page.tsx:286-301`(post-create skill copy) |
| Agent 创建 dialog | `packages/views/agents/components/create-agent-dialog.tsx` |
| Skill add dialog | `packages/views/agents/components/skill-add-dialog.tsx` |
| Quick-create Issue handler | `server/internal/handler/issue.go:877-982` (`QuickCreateIssue`) |
| Quick-create task enqueue | `server/internal/service/task.go:488+` (`EnqueueQuickCreateTask`) |
| Daemon claim + load skills | `server/internal/handler/daemon.go:1018-1098` + `service/task.go:1447-1463` |
| Daemon prompt build | `server/internal/daemon/prompt.go:17-36` (dispatch) + `:45-106` (`buildQuickCreatePrompt`) |
| Daemon execenv prepare | `server/internal/daemon/execenv/execenv.go:103-176` |
| Skill 目录约定(runtime mapping) | `server/internal/daemon/execenv/context.go:121-158` (`resolveSkillsDir`) |
| Skill 文件落盘 | `server/internal/daemon/execenv/context.go:175-204` (`writeSkillFiles`) |
| Quick-create 完成检测 + inbox | `server/internal/service/task.go:1810-1949` |
| LinkTaskToIssue | `server/internal/handler/agent.go:97-105` |
| Quick-create Issue 前端 modal | `packages/views/modals/quick-create-issue.tsx:48-570+` |
| Multica CLI 入口 | `server/cmd/multica/main.go:62-79` |
| Skill CLI 命令 | `server/cmd/multica/cmd_skill.go:17-96`(已有 import,无 find) |
| Agent CLI 命令 | `server/cmd/multica/cmd_agent.go:101-112`(已有 list/get,无 create) |
