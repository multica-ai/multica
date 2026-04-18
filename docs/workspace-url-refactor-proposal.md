# Workspace URL 化重构 — 项目汇报

**日期**：2026-04-15
**作者**：Naiyuan
**状态**：调研完成，待评审

---

## 一、为什么要做

当前 workspace 上下文完全靠 `X-Workspace-ID` HTTP header + Zustand store + localStorage 承载，URL 里**不含任何 workspace 信息**。所有路径都是 `/issues`、`/issues/:id` 这种 workspace-agnostic 的。

这个设计已经在产品里直接表现为 3 个已知问题：

1. **分享链接不可靠**（MUL-43）：`/issues/abc` 发给另一个成员，会用他自己 localStorage 里的 workspace 去解析，导致 404 或看到错误 workspace 的数据
2. **手机端无法切 workspace**（MUL-509）：切换只靠 sidebar UI，手机端不展开 sidebar 就没有切换入口
3. **多 tab 互相覆盖**：`multica_workspace_id` 是全局 localStorage key，两个 tab 打开不同 workspace 会互相污染

除了这 3 个显性 bug，架构上的"多份 workspace 状态拷贝互相同步"也带来一些隐性问题（创建 workspace 闪页、切换 workspace 时 cache 竞态等），积累时间越长后续改动越难。

行业惯例（Linear / Notion / Vercel / GitHub）都是 `/{workspace-slug}/...` 的 URL 形态，把 URL 当作 workspace 的唯一来源。这是我们应该对齐的最佳实践。

## 二、调研结论

### 好消息：基础设施已经就位

- 数据库 `workspace.slug` 字段已经存在（`TEXT UNIQUE NOT NULL`），用户创建时手动指定且不可修改
- 后端已有 `GetWorkspaceBySlug` 查询
- 前端 `Workspace` 类型已包含 `slug` 字段
- Web 端认证已经切换为 HttpOnly cookie 模式，Next.js middleware 可读到登录态

也就是说这次改造**不需要大量后端改动**，主要是前端路由和状态管理的重新组织。

### 坏消息：范围比最初估计大

初看以为只是"URL 前缀加个 slug"，调研后发现必须一起做的事情有：

1. **URL 路由重组**：web 端所有 dashboard 路由迁到 `app/[workspaceSlug]/(dashboard)/*`；desktop 端所有 react-router 路由加 `/:workspaceSlug` 前缀
2. **状态管理清理**：删除 `useWorkspaceStore.workspace` 作为独立状态，改为从 URL 派生；删除 `hydrateWorkspace` / `switchWorkspace` actions（切 workspace 变成纯导航）；删除 `localStorage["multica_workspace_id"]`
3. **所有路径引用替换**：`push("/issues")` 改为 path builder（`paths.issues()`），影响 ~25 个组件文件
4. **Mutation 副作用重构**：`useCreateWorkspace` / `useLeaveWorkspace` / `useDeleteWorkspace` 里的 `switchWorkspace` 调用全部移除（这些调用正是 MUL-727 闪页、MUL-728 删除后不跳转、MUL-820 接受邀请不切 workspace 等一系列 bug 的根因）
5. **桌面端 tab 系统适配**：tab 路径天然包含 workspace，切 workspace = 开新 tab 或导航，不再有全局切换动作
6. **Shareable URL 修复**：桌面端 `getShareableUrl` 当前生成 `https://www.multica.ai/issues/abc`（缺 slug），需要更新
7. **后端保留词校验**：slug 不能和前端顶级路由冲突（`login`、`onboarding`、`invite`、`api`、`settings` 等），后端创建时校验
8. **内部 markdown 链接兼容**：issue 评论里写的 `[foo](/issues/abc)` 触发的 `multica:navigate` 事件需要自动补当前 workspace slug

### 不需要改的（边界已确认）

- 邮件邀请链接 `/invite/{id}` — 接受邀请是 pre-workspace 流程，不需要 slug
- `mention://type/id` 协议 — 只存 UUID，workspace-agnostic
- CLI 登录 URL — `/login` 也是 pre-workspace，不需要 slug
- 后端 API 路径 — 保持 `/api/workspaces/{id}`，slug 仅用于前端 URL
- 桌面端 `multica://auth/callback` — 认证回调，不涉及 workspace

## 三、方案要点

**核心原则**：URL 是 workspace 上下文的唯一 source of truth，其他状态都是派生态。

**URL 形状**：`/{workspace-slug}/issues/{id}` （和 Linear / Notion 一致）

**切换 workspace = 导航**：sidebar 下拉改为 `<Link href="/{new-slug}/issues">`，不再有命令式的 `switchWorkspace` 函数。这样一次性消除前面列出的一大批 mutation 副作用 bug。

**预估影响面**：~30-35 个文件，其中约 20 个是机械替换（hardcoded 路径 → path builder），真正需要思考的核心逻辑改动集中在 5-6 个文件。

**一个 PR 合并**：中间状态不可运行（URL 结构是原子变化），不拆 PR。worktree 里充分开发和自测，一次 review 合并。

## 四、执行与测试计划

### 执行阶段

1. **本周内**：完成方案详细实施文档（精确到文件 / 行号 / 代码片段）
2. **下一步**：在独立 worktree 上开发，AI 辅助写代码，过程中人工 review
3. **开发完成后**：本地跑全套验证（`make check` — TypeScript + 单测 + Go 测试 + E2E）

### 测试阶段

1. **本地自测**：
   - 已知功能路径（创建 / 浏览 / 搜索 issue，切换 workspace，接受邀请，分享链接）
   - 已知 bug 场景（MUL-43 / MUL-509 / MUL-727 / MUL-820）逐一验证已修复
   - 多 tab 场景（两个 tab 打开不同 workspace 互不影响）
2. **测试环境部署**：本地通过后发测试环境，全员试用几天，观察：
   - 是否有回归（特别是导航流、创建/删除 workspace、邀请流程）
   - URL 使用感受（分享、收藏、刷新）
3. **灰度 / 生产**：测试环境稳定后推生产

### 风险提示

- **唯一的硬中断点**：现有的 `/issues` 等 URL 在重构后会 404（产品还没正式 ship、用户量可忽略，所以不做兼容性重定向）
- **E2E 测试断言**：约 20-30 处 URL 断言需要更新
- **后端保留词清单**：如果现有 workspace 里有名字撞到保留词的（例如正好叫 `settings`），需要提前 migrate（可能性极低，因 slug 限制较严）

## 五、附注

这次重构会**顺带修掉**以下已登记 issue，不需要单独开 PR：

| Issue | 修复方式 |
|---|---|
| MUL-43（切换 workspace 报错 / 分享链接失效） | URL 带 slug，根本解决 |
| MUL-509（手机端无法切 workspace） | 切换变导航，手机能点链接就能切 |
| MUL-723（workspace 不在 URL） | 核心目标 |
| MUL-727（创建 workspace 闪 /issues） | 删除 mutation 里的 switchWorkspace 副作用 |
| MUL-728（删除 workspace 后留在 /settings） | 删除成功后 navigate 到下一个 workspace |
| MUL-820（sidebar Join 不切 workspace） | Join 改成跳转到 `/invite/{id}` 走统一路径 |

不在本次范围内的：Issue #951（WebSocket 半开导致 cache 陈旧）—— 这是 realtime 层独立问题，单独 PR 处理。

---

**当前状态**：准备进入详细实施方案撰写，预计完成后再同步一次。
