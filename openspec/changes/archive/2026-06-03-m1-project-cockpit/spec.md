# M1 项目驾驶舱 - Product Spec

## 现状分析

### 当前 Project 详情页包含：
- 项目 icon、title、description（可编辑表单）
- 项目状态下拉（planned/in_progress/paused/completed/cancelled）
- Board 入口按钮
- 关联 issue 列表（仅计数 + 平铺列表，无进度分组）

### 当前缺口：
1. **无进度信号**：只有原始 issue 列表，看不出整体进展
2. **无负责人展示**：`lead_type` + `lead_id` 已存入数据库，但 UI 完全不显示
3. **列表页卡片无分量**：`ProjectListItem` 只显示 title + status badge，没有进度感知

---

## 目标

打开一个项目详情，3 秒内回答：
- **这个项目进展到哪里了？**（进度信号）
- **谁在负责？**（负责人）
- **还剩多少事没做？**（issue 分类计数）

---

## 进度模型

### Issue 状态分组

| 分组 | 包含的 IssueStatus | 显示颜色 |
|------|-------------------|---------|
| Done | `done` | success (绿) |
| In Progress | `in_progress`, `in_review` | warning (橙) |
| Todo | `backlog`, `todo`, `blocked` | muted (灰) |

- `cancelled` 排除在外，不计入进度分母
- 若所有 issue 均为 cancelled（或无 issue），显示"No issues yet"空状态

### 数据来源
- 直接从 `useIssueStore` 中 filter `issue.project_id === project.id`（当前已有此逻辑）
- **无需新增后端接口或 SQL**

---

## UI 设计决策

### 1. 项目详情页顶部进度区块（ProjectDetailPanel）

在 header 下方、表单上方，新增一个进度概览区块：

```
┌─────────────────────────────────────────────┐
│ 📁 Mobile App Rollout      [In Progress] [Board] [Delete] │
│ 12 linked issues                                          │
├─────────────────────────────────────────────┤
│ ████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  │  ← 堆叠进度条
│ 4 done · 3 in progress · 5 todo            │  ← 文字计数
├─────────────────────────────────────────────┤
│ Lead: [Avatar] Alice Wang                   │  ← 负责人
└─────────────────────────────────────────────┘
```

**进度条规格：**
- 使用 shadcn `div` 叠加三段（done/in-progress/todo），宽度按百分比
- 颜色：done → `bg-success`，in_progress → `bg-warning`，todo → `bg-muted-foreground/30`
- 高度 6px，圆角

**计数行格式：**
- `{done} done · {inProgress} in progress · {todo} todo`
- 若某分组为 0，隐藏该段文字

**负责人显示：**
- 使用现有 `useActorName()` hook 解析 `lead_type` + `lead_id`
- 显示 Avatar（圆形，24px）+ 名字
- 若 `lead_id` 为 null，显示"No lead"（muted 文字）
- 负责人区块仅展示，不做编辑（本次范围）

### 2. 项目列表卡片（ProjectListItem）

在 title 下方，status badge 前，增加迷你进度条：

```
┌─────────────────────────────────────────────┐
│ [📁]  Mobile App Rollout                    │
│        No description yet                   │
│        ████░░░░░░░░░░░░░░░░░░░  4/12 done  │  ← 新增
│                                  [In Progress] │
└─────────────────────────────────────────────┘
```

- 显示规则同上，但更精简：只展示完成率（done/total done+inprogress+todo）
- 无 issue 时隐藏此行

---

## 负责人字段解析规则

| lead_type | lead_id 含义 | 查找方式 |
|-----------|-------------|---------|
| `"member"` | user_id | `members.find(m => m.user_id === lead_id)` |
| `"agent"` | agent_id | `agents.find(a => a.id === lead_id)` |
| `null` | — | 显示"No lead" |

`useActorName()` hook 已封装上述逻辑（`getMemberName` 按 user_id 查，`getAgentName` 按 agent id 查）。

---

## 范围约束

- **本次不实现**：负责人编辑（在 UI 中选 Lead）
- **本次不实现**：日期/里程碑展示
- **本次不实现**：活动流 feed
- **本次不新增**：后端接口、SQL migration

---

## 验收标准

1. 项目详情页顶部有进度条，颜色与 done/in_progress/todo 分组一致
2. 进度条旁显示文字计数（如"4 done · 3 in progress · 5 todo"）
3. 负责人区块可见（有 lead 时显示头像+姓名，无 lead 时显示"No lead"）
4. 项目列表每个卡片下方显示迷你进度条 + "X/Y done"
5. 无 issue 时进度区块显示空状态，不报错
6. TypeScript 不报错，Vitest 不退步
