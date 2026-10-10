# 仓库指令

Multica 是一个任务管理平台，人与智能体围绕任务（issue）展开协作。本文件的指令对所有在本仓库工作的编码智能体生效。

## 范围与阅读顺序

- 修改 `apps/mobile/` 之前，另读 [apps/mobile/AGENTS.md](apps/mobile/AGENTS.md)；修改 `apps/mobile-harmony/` 之前，另读 [apps/mobile-harmony/AGENTS.md](apps/mobile-harmony/AGENTS.md)，即使你的工具不会自动加载嵌套指令。下方的平台专属章节只适用于所指名的平台。
- 修改 `multica` CLI 或智能体守护进程（`server/cmd/multica`）之前，先读 [CLI_AND_DAEMON.md](CLI_AND_DAEMON.md)。`make cli ARGS="..."` 和 `make daemon` 可从源码运行它们。
- 子系统专项文档（任务唤醒、后台维护作业、任务状态生命周期切换）位于 `docs/` 和 `docs/engineering/`；改动这些子系统之前先读对应文档。
- 涉及命名、翻译或中文 UI/文档文案时，先读 [conventions.mdx](apps/docs/content/docs/developers/conventions.mdx) 和 [conventions.zh.mdx](apps/docs/content/docs/developers/conventions.zh.mdx)。
- 共享规则维护在本文件，mobile 专属规则维护在 mobile 文件。`CLAUDE.md` 文件只做导入引用。改动所引用的工作流或边界时，在同一变更中同步更新指令；不要添加事故时间线、依赖版本列表或重复规则。

## 共享规则与包边界

| 位置 | 职责与约束 |
| --- | --- |
| `server/` | Go 后端；Chi、sqlc、WebSocket |
| `packages/core/` | 无 UI 的逻辑、API client、Query hooks、共享 Zustand store。禁止 UI 库、`react-dom`、`localStorage`、`process.env`；持久化一律走 `StorageAdapter`。 |
| `packages/ui/` | UI 基础组件与共享样式。禁止业务逻辑和 `@multica/core` 导入。 |
| `packages/views/` | Web/桌面共享页面与业务组件。禁止定义 store、`next/*`、`react-router-dom`；使用 `NavigationAdapter`、`useNavigation()` 和 `<AppLink>`。 |
| `packages/plugin-sdk/` | Multica 插件接入面的客户端 SDK；导出原始 TypeScript（`index.ts`、`protocol.ts`），无运行时依赖。 |
| `apps/web/` | Next.js 路由/布局与 Web 专属 UI。框架 API 留在这一层；共享导航 adapter 放在 `apps/web/platform/`。 |
| `apps/desktop/` | Electron 与桌面专属 UI/状态。应用内导航统一走 `apps/desktop/src/renderer/src/platform/`。 |
| `apps/mobile/` | 独立的 Expo/React Native 客户端：自成一体地拥有 UI、状态、hooks、provider、i18n、构建与发布。只共享 core 的类型与纯工具函数，包括平台无关 schema。 |
| `apps/mobile-harmony/` | 独立的 RNOH（React Native for OpenHarmony）鸿蒙客户端，当前为垂直切片。与 iOS mobile 同一共享边界（core 类型 + 纯函数）；RNOH 版本矩阵与原生壳规则见其 AGENTS.md。 |
| `apps/docs/` | Fumadocs 文档站 |
| `apps/ui-lab/` | 迭代共享 UI/views 组件的 Vite playground（`pnpm dev:ui-lab`，端口 4310）；消费 core、ui、views。 |

- 依赖方向是 `views -> core + ui`；core 与 ui 保持互相独立。共享包导出原始 TypeScript，由消费方 app 编译。
- Web 和桌面共用的逻辑必须抽到合适的共享包。框架/Electron API 留在 app 层；平台专属 UI 通过 props/slot 注入。
- 共享功能要同时接入 Web 路由和桌面 router 或 overlay。复用现有的守卫/Provider，例如 `packages/views/layout/` 中的 `DashboardGuard`。
- 每个 workspace 声明其直接导入的外部依赖。共享依赖使用 `catalog:`；mobile 在自己的 manifest 中锁定 Expo/React Native 依赖。

## 开发与验证

以 `Makefile`、各 workspace 的 `package.json` 和 `pnpm-workspace.yaml` 为命令与版本的当前来源。环境搭建与 worktree 操作见 [CONTRIBUTING.md](CONTRIBUTING.md)。

- 使用检出目录自带的受管环境：`make up`、`make status`、`make down`。`make down` 保留数据；`make destroy` 删除环境及其数据。
- Worktree 共享 PostgreSQL，但数据库/端口相互隔离。使用环境脚本和 `.env.worktree`；不要复制主检出的 `.env`，也不要在假设的 PostgreSQL 实例上手工建库。
- SQL 变更后用 `make sqlc` 重新生成 sqlc 代码。
- 迭代时先跑最窄的有效检查，风险升高再扩大范围。如实报告实际跑了什么、跳过了什么。

在仓库根目录运行：

| 范围 | 检查 |
| --- | --- |
| 除 mobile 外的前端 | `pnpm typecheck`、`pnpm lint`、`pnpm test` |
| Go 后端 | `make test` |
| 端到端 | `pnpm exec playwright test` |
| Web/后端联合验证 | `make check` |
| Mobile | 见 [apps/mobile/AGENTS.md](apps/mobile/AGENTS.md#verification) 与 [apps/mobile-harmony/AGENTS.md](apps/mobile-harmony/AGENTS.md#verification) 中的命令 |

根目录前端命令和 `make check` 不验证 mobile 与 mobile-harmony。纯文档变更可以用链接/引用检查加 `git diff --check`；并说明未运行代码测试。

性能（`e2e/perf`）与仅 Chrome 的 iframe 套件使用独立配置：`pnpm exec playwright test --config=playwright.perf.config.ts` 与 `--config=playwright.iframe.config.ts`。

## 状态管理规则

- TanStack Query 拥有 API/服务端数据。Zustand 拥有客户端状态，如筛选、草稿、弹窗和标签页布局；只持久化持久性偏好/草稿/布局，不持久化服务端数据或临时 UI 状态。
- Web/桌面共享 store 放在 `packages/core/`。桌面平台 store 留在桌面端；mobile store 留在 mobile。不要在 `packages/views/` 中定义 store。
- 在 Web/桌面上，工作区身份由路由驱动；平台镜像仅用于请求头、存储命名空间和重连。React Context 用于平台底层接线，不是第二份服务端状态仓库。
- 所有 store 中，只有 auth/workspace store 可以直接调用 `api.*`；其他服务端交互归入 query/mutation。
- 工作区范围的 query key 必须包含 `wsId`；账号级 key 保持账号范围。需要工作区上下文的 hook 接受 `wsId`，除非保证运行在对应 provider 之下。
- Zustand selector 返回稳定引用；对新分配的对象/数组使用浅比较。
- WebSocket 事件对 Query 缓存做修补或失效，不操作 Zustand 里的服务端数据。允许清除客户端自有指针，但仅限单一响应方，且当事件可能由本客户端引发时需带自发守卫。
- 乐观字段修补要求结果可预测、失败罕见、回滚简单、且停留在当前屏幕。修补前先快照，失败时回滚，落定后失效不确定的投影。
- 创建/删除/退出与确认流程必须先等待服务端，再导航或清理；不要乐观删除实体。例外：桌面端规则中记载的既有工作区退出竞态，以及 mobile 说明中记载的收件箱标记已读。
- 消息发送要有可见的 pending 状态，失败时重试。

## API 兼容性

已安装的桌面客户端可能连接到更新的后端。在 API 边界保持响应兼容。

- UI 消费的 JSON 必须经过 zod schema 与 `parseWithFallback`，不能用 `as T` 强转。Web/桌面使用 `packages/core/api/schema.ts`；mobile 使用自己的请求辅助函数。
- 为可选字段提供默认值，为未知服务端枚举提供回退。优先使用显式布尔判断；当还有其他契约信号可用时，不要把关键操作入口绑在单一后端 flag 上。
- 新增/修改 endpoint 时，同步更新对应 schema，并补充字段缺失或格式错误的测试。

## 数据库与迁移规则

- 不添加外键、级联删除或级联更新。在应用代码中校验关系并清理依赖数据；操作必须原子时使用事务。
- 迁移创建的所有索引（包括新表上的索引）一律使用 `CREATE [UNIQUE] INDEX CONCURRENTLY`。每条并发索引构建单独占用一个单语句迁移文件；runner 在显式事务之外执行这些文件。
- 被条件跳过的迁移同样要记录进 `schema_migrations`。后续触碰条件对象的 DDL 必须幂等（`IF EXISTS` / `IF NOT EXISTS`）；若对象缺失会破坏运行时行为，需记录恢复方法。

## 后端 UUID 规则

在 `server/internal/handler/` 中，写入前先区分 UUID 的来源：

- UUID 或人类可读的资源参数：用 `loadIssueForUser`、`loadSkillForUser`、`loadAgentForUser`、`requireDaemonRuntimeAccess` 等 loader 解析，然后用解析得到的 `entity.ID` 写入。
- 纯 UUID 请求输入：`parseUUIDOrBadRequest(w, s, fieldName)`；`ok=false` 时立即返回。
- 可信的 sqlc/测试 fixture 回环：`parseUUID(s)`，非法输入会 panic。
- handler 之外：`util.ParseUUID(s)` 并检查 error。

工作区范围的查询按 `workspace_id` 过滤；成员资格控制访问权限，`X-Workspace-ID` 用于选择工作区。负责人是多态的：将 `assignee_id` 与 `assignee_type` 结合解读。

## 桌面端规则

- 工作区会话路由是标签页目的地。进入工作区前的一次性流程（创建工作区、接受邀请）使用 `apps/desktop/src/renderer/src/stores/window-overlay-store.ts` 中的 `WindowOverlay`，而不是新增路由。过期的工作区标签页通过丢弃过期标签组自愈。
- 工作区路由布局负责调用 `@multica/core/platform` 的 `setCurrentWorkspace(slug, uuid)`；离开工作区上下文时调用 `setCurrentWorkspace(null, null)`。
- 跨工作区导航走 adapter 的 `switchWorkspace(slug, targetPath)` 流程；不要绕过它直接操作 router。
- 工作区删除必须等待服务端。既有工作区退出先清理/导航以避开 `member:removed` 竞态；这是 `packages/views/settings/components/workspace-tab.tsx` 中的已知债务，不是新流程可效仿的模式。
- 仪表盘 shell 之外的整窗视图要把 `<DragStrip />`（来自 `@multica/views/platform`）作为第一个 flex 子元素挂载。顶部 48px 内的交互控件需要设置 `WebkitAppRegion: "no-drag"`。

## UI 文案

- 描述（description）是可选项，默认省略。不要复述标题、标签、取值、状态或按钮动作。仅在存在不直观的选择、约束、后果或后续步骤时才添加帮助；每个事实只在相关控件旁陈述一次。
- 权限、费用、破坏性后果、执行前提与错误恢复信息应在相关处保持可见。高级用法与诊断信息放进可访问、可显式打开的帮助中。保留标签与无障碍名称；不要把冗余文案整段搬进 `sr-only` 文本。
- 结合周围控件与全部已支持翻译审阅文案，包括 mobile 的独立文案。遵循既有 conventions 页面中的 UI 文案规则；有 description prop 不等于要写一段话。

## Web/桌面 UI 规则

- 使用 Button 和 Dialog 前，先读 `packages/ui/docs/button.md` 与 `packages/ui/docs/dialog.md`。这些组件契约同时支撑 UI Lab 文档。

- 优先使用现有 shadcn/Base UI 基础组件；用 `pnpm ui:add <component>` 添加组件。
- 运行 `pnpm ui:add @reui/<name>` 时，拒绝覆盖提示。`REUI_LICENSE_KEY` 只保存在环境中，绝不进仓库文件。vendored 的基础组件适配进 `packages/ui/components/ui/`，组合组件放进 `packages/views/`。
- 使用 `packages/ui/styles/` 中的共享语义 token。字号层级使用 `packages/ui/styles/tokens.css` 中按角色命名的 `--text-*` 阶梯，而不是 Tailwind 默认的字号序列。
- 选中态在悬停时仍可辨识。有意识地处理溢出、长文本与滚动；避免不必要的局部状态与分隔线。

## 测试

- 测试与实现同目录：共享逻辑在 core，共享组件在 views，平台接线在 app 层，E2E 在 `e2e/`，Go 测试在 server。不要在 app 测试套件里测共享行为。
- 每个行为只归一个权威测试层：helper 测试负责解析/状态矩阵；组件测试覆盖接线、无障碍、正常路径与具名回归。行为修复前优先先写失败的回归测试。
- 无 DOM 的 `.test.ts` 文件以 `// @vitest-environment node` 开头；若这会让被测代码静默切换到 SSR 路径，则不要使用。
- Views 测试不得 mock `next/*` 或 `react-router-dom`。mock store 时保留 Zustand 可调用形态并带 `getState`；API 调用在 `@multica/core/api` 层 mock。
- E2E 的 setup/teardown 使用 `TestApiClient`。
- 依赖数据库的 Go 测试使用 `server/internal/testutil` 的 fixture（`dbfx.Issue`、`dbfx.Task`、`dbfx.Insert`）与 `testutil.Call(h, req).Want(status).JSON(&out)`。产品断言与用例专属诊断留在测试内，不进 fixture helper。
- 默认测试不得解析或执行用户安装的智能体 CLI；传入测试自建的假可执行路径或不存在的路径。新增默认智能体命令写入 `scripts/agent-cli-command-names.txt`。
- 只有在获得明确授权时才运行真实智能体冒烟测试。以 `agentintegration` tag 门控，并在查找可执行文件/访问账号前检查 `MULTICA_RUN_REAL_AGENT_SMOKE=1`。运行指定测试：`(cd server && MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags=agentintegration ./pkg/agent -run '<test-name>' -count=1 -v)`。

## 变更与发布规则

- 变更保持聚焦；复用既有模式。代码注释只用英文。
- 除非另有要求，不添加内部兼容 shim、双写、回退路径或遗留 adapter。这不会放宽上面的 API 响应兼容要求。
- 新的全局前置工作区路由使用单个单词或 `/{noun}/{verb}` 格式，不用连字符根名称。变更保留字（reserved slugs）时，更新 `server/internal/handler/reserved_slugs.json`，运行 `pnpm generate:reserved-slugs`，并提交 `packages/core/paths/reserved-slugs.ts`。
- 使用原子化 conventional commit 与仓库 PR 模板。发布遵循 [.github/RELEASING.md](.github/RELEASING.md)；除非另有说明，默认只升 patch 版本。
