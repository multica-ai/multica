# M7 — Knowledge Management Spec

> **里程碑类型：** 纯 spec，不写代码。  
> **目标：** 让团队就"知识管理"在本产品里的含义达成共识，评估是否值得近期实现。

---

## 1. 问题描述：当前产品中"知识"在哪里，缺口是什么

### 1.1 现有知识相关实体（探索发现）

通过代码库探索，发现以下对象承载着不同类型的"知识"：

| 对象 | 范围 | 谁在用 | 当前角色 |
|------|------|--------|----------|
| **Skill**（`skill` + `skill_file` + `agent_skill` 表） | Workspace 级 | AI agent | 可复用操作指令，任务执行时通过 `LoadAgentSkills` 注入 daemon |
| **Workspace Context**（`workspace.context` TEXT 列） | Workspace 级 | AI agent（弱） | 全局上下文自由文本，已建表但在任务分发链路中暂未显式注入 |
| **Issue Description**（TipTap rich text） | Issue 级 | 人 + AI | 每个任务的主信息单元，作为 task prompt 的核心输入 |
| **Comment**（`comment` 表） | Issue 级 | 人 + AI | 执行历史 + 讨论，agent 写 `progress_update` 和 `comment` 类型 |
| **Project Description**（`project.description`） | Project 级 | 人 | 项目背景自由文本，未被 AI 读取 |
| **Issue `context_refs`**（JSONB 字段） | Issue 级 | 未使用 | 字段已存在于 schema，前端未展示也未填写 |

### 1.2 缺口分析

**AI 知识层**面基本成型：Skills 是现有最结构化的知识形式，已有 CRUD、文件树 UI、任务执行时注入。

**团队知识层**存在三个真实缺口：

1. **Skills 对人类不透明**：Skills 目前是 agent 的配置项，没有搜索入口，非技术成员看不懂也不知道该去哪找。
2. **跨 issue 知识不可达**：团队的技术决策、工作规范散落在 issue description 和 comment 里，没有提炼出口，也无法被后续 issue 引用。
3. **Workspace Context 未接通 AI**：Migration 006 添加了 `workspace.context` 字段，但在任务分发链路（`ClaimTaskByRuntime` → daemon）中，仅 repos 字段从 workspace 传递，context 没有被注入任务上下文。

---

## 2. 对象模型：知识的核心对象是什么

### 2.1 选项评估

**Option A：Issue description 就是知识**
- 优势：零成本，已有 TipTap 富文本，AI 已在读取
- 缺陷：Issues 是任务，有状态（done/cancelled），长期知识和任务执行混在一起会产生大量噪音；"知识"无法脱离 issue 生命周期独立存在
- **结论：不适合作为主知识对象**

**Option B：Project 文档（每个 project 附带 rich text 文档）**
- 优势：Project 是团队协作的自然单元，文档与执行上下文天然绑定
- 缺陷：需要新增数据模型和 UI，且 workspace 级的通用知识（如架构规范、工作习惯）没有归属点
- **结论：适合作为 V2 场景补充，不适合作为 V1 核心模型**

**Option C：独立 Wiki/Doc 系统（类 Notion）**
- 优势：功能完整，体验最佳
- 缺陷：对 2-10 人团队来说是过度设计；独立 wiki 会分裂信息，维护成本高，与 issue 工作流割裂
- **结论：明确排除，不在当前路线图范围内**

**Option D：Skills 是 AI 的知识库（已有）**
- 优势：已实现，有结构（name + content + files），已接通任务执行
- 缺陷：当前设计是 agent-scoped 的（`agent_skill` 多对多），不是团队共享的；没有对人类的可读性设计
- **结论：Skills 是正确方向，但需要角色扩展**

### 2.2 推荐对象模型：Skill 作为团队知识单元

**核心判断：** Skill 已经是本产品中最接近"知识"的对象——它有名称、markdown 内容、支持文件，且 workspace 级归属。要做的不是引入新对象，而是让 Skill 兼顾两个角色：AI 行为指令 + 团队参考文档。

**扩展后的 Skill 对象字段（概念层，不变更现有字段）：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID | 现有 |
| `workspace_id` | UUID | 现有，workspace 级归属 |
| `name` | TEXT | 现有 |
| `description` | TEXT | 现有，用于搜索摘要 |
| `content` | TEXT | 现有，markdown 正文 |
| `config` | JSONB | 现有 |
| `files` | skill_file[] | 现有，支持文件 |
| `visibility` | TEXT | **新增概念**：`agent_only`（仅 AI 读取）vs `team`（团队可见、可搜索） |
| `created_by` | UUID | 现有 |

**所有权：** Skills 属于 workspace，不属于个人或 project。任何成员均可创建和编辑。

---

## 3. 所有权模型

| 知识类型 | 归属 | 创建者 | 读取权限 |
|---------|------|--------|---------|
| Skill（团队可见） | Workspace | 任意成员 | 全部成员 + 所有 agent |
| Skill（agent-only） | Workspace → Agent | 任意成员 | 指定 agent |
| Workspace Context | Workspace | 管理员 | 所有 agent |
| Issue Description | Issue | 创建者 | 成员 + 指派 agent |
| Project Description | Project | 项目管理者 | 成员 |

**关键原则：** 知识的最顶层归属是 workspace，不做个人化知识库。2-10 人团队的知识首先是团队知识。

---

## 4. 检索路径：用户如何找到知识

### 当前状态
- `/skills` 路由存在，有列表 + 文件树 UI，但没有搜索
- Command Palette（M3）可以搜索 issue、project、member，不覆盖 skills
- 没有全局知识搜索

### 目标检索路径（V1）

1. **Command Palette（Cmd+K）**：将 `visibility = team` 的 skills 纳入搜索范围，显示 name + description 摘要，跳转到 `/skills/:id`
2. **`/skills` 页面内搜索**：现有 UI 补充实时过滤
3. **Issue 编辑器引用**：在 issue description 中通过 `@skill-name` 或 slash command 引用 skill（V2 候选）

---

## 5. 与 AI 的关系

### 5.1 当前实际状态

```
Task claim (daemon) → LoadAgentSkills(agentID) → skills 注入 TaskAgentData.Skills
                    → workspace.context → 【未注入，存在 gap】
```

`workspace.context` 已建表但在 `ClaimTaskByRuntime` 响应中未被传递给 daemon，这是一个需要修复的 bug（不在本 spec 范围，但值得记录）。

### 5.2 知识与 AI 的两种关系

| 关系类型 | 当前实现 | 说明 |
|---------|---------|------|
| **Skill → Agent 执行时读取** | ✅ 已实现 | 任务分发时注入，agent 知道"怎么做" |
| **Workspace Context → 全局背景** | ⚠️ 已建表，未接通 | 应补全注入链路 |
| **Team Knowledge → Agent 按需检索** | ❌ 不存在 | V2 候选：AI 主动查询 skill 库 |

### 5.3 Skills vs Context 的语义边界

- **Skills**：操作性知识（"如何做 X"），结构化，可分配给特定 agent
- **Workspace Context**：背景性知识（"我们是谁，在做什么"），非结构化，全局适用
- 两者不是竞争关系，是互补的

---

## 6. 实现建议

### 6.1 V1 做什么（推荐优先级）

**推荐：不立即实现新模块，先做三件小事来填补最关键缺口：**

| 优先级 | 工作内容 | 规模 |
|--------|---------|------|
| P0 | 修复 `workspace.context` 未注入 daemon task 的 bug | ~0.5 天 |
| P1 | 为 Skill 添加 `visibility` 字段，区分 `team` 和 `agent_only` | ~1 天 |
| P2 | 将 `visibility = team` 的 skills 纳入 Command Palette 搜索 | ~1 天 |

这三件事合计约 2.5 天，不引入新对象，不新增路由，利用已有架构完成最高价值的知识可发现性提升。

### 6.2 V1 不做什么

- 不建独立 Wiki/Doc 系统
- 不建 Project 级文档
- 不建个人知识库
- 不建 AI 主动检索知识的 RAG 系统
- 不做 issue ↔ skill 双向引用

### 6.3 是否推荐立即实现

**结论：推荐实现上表 P0 + P1，P2 可选。**

理由：
1. P0 是 bug，workspace context 已建表但未接通，补全成本极低，收益高
2. P1 的 `visibility` 字段一旦缺失，Skills 永远无法从 AI 配置项演进为团队知识；越晚加越难迁移
3. P2 是 M3 Command Palette 的自然延伸，不需要新页面

全面知识管理模块（wiki、项目文档、RAG 检索）应在产品主链路（任务+时间+AI 执行）稳定后再定义，预计 M8 以后。

---

## 7. 如果推迟全面实现：当前替代方案

如果团队决定完全推迟（含 P0/P1），以下是当前产品中可用的替代路径：

| 知识需求 | 当前替代方案 | 缺陷 |
|---------|------------|------|
| AI 需要知道工作规范 | 在 Agent Instructions 字段中手写 | 无法在多个 agent 间复用 |
| AI 需要项目背景 | Issue description 写详细点 | 每次重写，无法积累 |
| 团队需要查工作方式 | 翻 Skills 页面 | 没有搜索，不可发现 |
| 团队需要查技术决策 | 翻 issue 历史 | 埋在任务流水中，信噪比差 |

**最近似的当前方案：** Skills（`/skills`）是现有产品中最接近"知识库"的东西，具备名称、内容、文件树结构。缺点是它目前是 agent 配置 UI，没有面向团队的知识检索设计。

---

## 8. 下一步决策点

完成本 spec 后，团队需要回答：

1. **是否同意** Skill 作为统一知识对象（而非引入 Wiki/Doc）？
2. **是否批准** P0 bug fix（`workspace.context` 接通）立即进入下一个 sprint？
3. **`visibility` 字段** 的名称和枚举值是否合适（`team` vs `agent_only`）？
4. **Command Palette 中的知识搜索**（P2）是否与 M3 的范围界定冲突，是否需要重新对齐？

---

*Spec 状态：草稿，待团队评审*  
*创建时间：M7*  
*覆盖代码探索范围：`apps/workspace/src/features/skills/`、`server/migrations/`、`server/internal/handler/`、`server/internal/service/task.go`、`product-overview.md`*
