# Jira → Multica 单向 Issue 同步（v1，纯桌面客户端）

- 状态：设计已确认，待生成实现计划
- 日期：2026-06-30
- 范围：仅改动 Multica 客户端（`apps/desktop`、`packages/core`、`packages/views`），**零 Go server 改动**

## 1. 背景与目标

Multica 的 issue 目前全部由用户在产品内自建。诉求是把公司 Jira 中**分配给我**的 issue 同步进 Multica，与自建 issue 统一管理，且只改动客户端逻辑、不动后端。

用户使用的是 **Multica 桌面应用（Electron）**，因此 Jira REST 请求可在主进程发起，规避浏览器 CORS 限制（web 浏览器内直连 Jira Cloud REST 会被 CORS 拦截，v1 不支持 web 端）。

### v1 决策（已与用户确认）

- 方向：**单向 Jira → Multica**。双向回写留作后续阶段（回声循环 + 冲突策略风险高）。
- 拉取范围：**只同步分配给当前用户的 issue**（`assignee = currentUser()`）。
- 状态映射：**内置默认映射 + 设置页可覆盖**未命中项。
- 更新策略：**Jira 为准**，重复同步覆盖本地对同名字段的改动。
- 触发：**手动「立即同步」按钮 + app 开着时主进程定时轮询**。
- 字段：核心字段 + **评论 + 子任务**。

## 2. 架构与边界

```
Jira Cloud REST  ←(net, 无 CORS)→  Electron 主进程        ←IPC→  渲染进程
                                    src/main/jira.ts              界面 / 同步触发
                                        │
                                    ~/.multica/jira.json   (站点 / 邮箱 / token / JQL / 映射 / 间隔)
                                    token 仅存主进程，永不下发渲染进程

packages/core/jira/  ← 纯同步引擎（注入 Jira transport + Multica api client）
```

- 所有 Jira 请求走主进程 `jira:request` IPC 通道，仿现有 `daemon:*`、`file:download-url`、`local-directory:*` 的写法（`apps/desktop/src/main/`、`src/preload/index.ts`）。
- Jira 配置存主进程文件，仿 `daemon:get-prefs`/`daemon:set-prefs`（现 `~/.multica/desktop_prefs.json`）。**API token 只在主进程，渲染进程拿不到明文**。
- 同步引擎在 `packages/core/jira/`，为纯逻辑模块：依赖通过参数注入（一个「发 Jira 请求」的函数 + Multica api client），不直接触碰平台 API。遵守 `packages/core` 边界：无 `process.env`、无 `localStorage`（用 `StorageAdapter`）、无 UI 库。
- Jira 响应一律经 `parseWithFallback`（`packages/core/api/schema.ts`）+ zod schema 解析，不裸 `as T`，遵守仓库 API 兼容硬规则。

## 3. 数据模型

复用每条 issue 自带的 `metadata` JSONB KV（`PUT /api/issues/:id/metadata/:key`，per-key 原子写；`IssueSchema.metadata` 在列表响应中即返回）。值仅限 string/number/bool；键名匹配 `^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$`；每 issue ≤50 键、≤8KB。本方案用 6 个键，余量充足。**不新增任何数据库表或列。**

| 键 | 类型 | 用途 |
|---|---|---|
| `source` | string `"jira"` | 区分自建 / Jira 来源，用于徽标与筛选 |
| `jira_key` | string `"PROJ-123"` | 去重主键 |
| `jira_url` | string | 跳回 Jira |
| `jira_status` | string | 上次同步的 Jira 状态名（展示 / 调试） |
| `jira_updated_at` | string (ISO) | Jira `updated` 时间戳，用于变更检测 |
| `jira_comments_synced_at` | string (ISO) | 评论增量高水位 |

去重索引在运行时构建：拉一次 Multica issue 列表，读各条 `metadata.jira_key`，建 `Map<jira_key → { issueId, jira_updated_at, commentsSyncedAt }>`。

## 4. 同步算法（一次同步运行）

1. 拉 Multica issue 列表（带 metadata）→ 构建去重 `Map`。
2. Jira `search`，JQL = `assignee = currentUser() AND <用户附加条件>`（token 持有人即「我」，无需用户身份映射）。返回需含 `fields.subtasks`。
3. 逐条处理 Jira 父 issue（先父后子）：
   - **Map 无** → `createIssue`（字段/状态映射，创建人与受理人 = 当前 Multica 成员）→ 写 6 个 metadata 键。
   - **Map 有且 Jira `updated` > 存储的 `jira_updated_at`** → `updateIssue` 覆盖 title / description / status / priority / due_date（Jira 为准）→ 更新 metadata。
   - **Map 有且未更新** → 跳过字段同步，仍检查评论增量。
4. **子任务**：对每个已同步的父 issue，按其 `subtasks[].key` 拉取并建成 Multica 子 issue，`parent_issue_id` 指向父的 `issueId`。子任务即使分配给他人也一并同步，以保持任务结构完整。子任务自身同样走第 3 步去重 / 更新逻辑。
5. **评论**（append-only）：拉 Jira 评论中 `created > jira_comments_synced_at` 的，逐条 `createComment` → 抬高 `jira_comments_synced_at`。不处理 Jira 端评论的编辑 / 删除（v1 范围外）。

幂等：以 `jira_key` 去重，重复运行安全。逐条 try-catch，单条失败不中断整轮，运行结束给汇总（成功 / 更新 / 失败计数 + 错误）。

## 5. 字段与状态映射

| Jira | Multica | 说明 |
|---|---|---|
| summary | title | |
| description (ADF) | description | ADF → Markdown / 纯文本转换 |
| duedate | due_date | |
| priority | priority | 内置默认表 |
| status | status | 内置默认映射 + 可覆盖 |
| assignee / reporter | — | Multica 侧创建人与受理人统一为当前成员 |

状态默认映射（未命中 → `backlog`，设置页可改）：

- `Backlog` → `backlog`
- `To Do` / `Open` → `todo`
- `In Progress` → `in_progress`
- `In Review` → `in_review`
- `Done` / `Closed` / `Resolved` → `done`

Multica 状态合法值（DB CHECK 约束）：`backlog`、`todo`、`in_progress`、`in_review`、`done`、`blocked`、`cancelled`。

## 6. UI（`packages/views/`，web/desktop 共享视图；实际 Jira 请求在 desktop 主进程）

- **设置页**：Jira 站点 / 邮箱 / API token、JQL（含命中条数预览）、状态映射编辑器、轮询间隔、「立即同步」按钮、上次同步结果与错误展示。
- **issue 列表**：`source=jira` 显示来源徽标 + 跳 Jira 链接；支持按来源筛选。
- 用语言/中文文案遵循 `apps/docs/content/docs/developers/conventions*.mdx`。

## 7. 触发

- 手动「立即同步」（设置页 / issue 列表）。
- 主进程定时器，间隔取自配置，仅在 app 运行时生效。无离线 / 服务端级定时同步（纯客户端固有限制）。

## 8. 边界与取舍

- 离开 JQL 范围（被关闭 / 转派走）的 issue：保留 Multica 副本不动，**不自动删除用户数据**。
- v1 明确不做：回写 Jira、附件同步、评论双向 / 编辑删除反映、自定义字段、多 Jira 站点、web 浏览器端（CORS）。
- 「Jira 为准覆盖本地」意味着：用户在 Multica 看板上拖动同步来的 issue 状态，下次同步会被 Jira 状态覆盖——这是用户已确认的取舍。

## 9. 测试

- `packages/core/jira/*.test.ts`：去重、字段 / 状态映射、更新检测（基于 `jira_updated_at`）、评论高水位、**畸形 Jira 响应经 `parseWithFallback` 兜底**。mock 注入的 Jira transport + mock `@multica/core/api`。
- `packages/views/*.test.tsx`：设置表单与来源徽标渲染；不 mock `next/*` 或 `react-router-dom`。
- `apps/desktop`：`jira:request` IPC 与配置读写（主进程，可仿 `daemon-manager.test.ts`）。

## 10. 待实现文件清单（概览）

- `apps/desktop/src/main/jira.ts`：`ipcMain.handle("jira:request" | "jira:get-config" | "jira:set-config")`，主进程 `net` 发请求、配置文件读写。
- `apps/desktop/src/preload/index.ts`：`contextBridge` 暴露 `window.jira.{ request, getConfig, setConfig }`。
- `apps/desktop/src/main/index.ts`：注册定时轮询触发。
- `packages/core/jira/`：Jira zod schema、ADF→Markdown、字段 / 状态映射、同步引擎、去重逻辑。
- `packages/views/settings/jira/`：连接配置页。
- `packages/views/` issue 列表：来源徽标 + 筛选。
