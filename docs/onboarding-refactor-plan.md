# Onboarding v3 — Thin Server, Frontend-Orchestrated Welcome

## 1. 背景

两次错位重构(MUL-2438 + da0ecb6a)叠加产出了一个味道很重的 onboarding:
- `users.onboarded_at` 在 4 个 handler 里分别 `MarkUserOnboarded`,每处副作用还不一样
- 同样的"种 install-runtime issue"在 4 个调用点重复,靠 advisory lock 在底层做去重
- Step 3 Connect 与 Skip 路径不对称(一个延迟 mark,一个立即 mark)
- 工作区 Modal 自己 query runtime 列表挑第一个,完全丢弃用户在 Step 3 的选择
- `onboarded_at` 字段同时承担"完成状态"和"Modal 触发条件"双语义

最初的 v2 方案(本 wip 分支的 PR 1-7)走"持久化字段 + 工作区 init 4 分支 dispatcher"路线 — 把 Step 3 的选择 PATCH 进 `users.onboarding_runtime_id` / `onboarding_runtime_skipped`,工作区入口读这两个字段决定弹什么。可工作,但把过去 React 内存里的瞬态意图变成了数据库的持久化字段,后端业务复杂度不降反升:多了一个 `OnboardingService`,多了一个 `WorkspaceContentService`,handler 多了一个 `EnsureOnboardingContent` endpoint,前端多了 4 分支 dispatcher 组件。

数据分析师确认生产里 0 个 mid-flow 用户(`有 workspace + onboarded_at IS NULL`),v2 加的两字段没有真实使用,可以安全删除。

v3 在 v2 之上做更激进的清理:**后端 onboarding 业务复杂度归零,Helper agent / starter issue / 中英文案全部在前端 hook 里调通用 `createAgent` / `createIssue` 完成**。

## 2. 设计 — 5 条核心原则

1. **`users.onboarded_at` 是唯一 onboarding 状态字段**(删 v2 加的两字段)
2. **Onboarding 阶段(Step 1-3)纯收集**,Step 3 按钮 = mark + welcome-store.set + navigate
3. **后端 onboarding 业务复杂度归零** — 删 `BootstrapOnboarding*` / `OnboardingService` / `WorkspaceContentService` / `EnsureOnboardingContent` 全套;后端 onboarding 只剩 mark + 问卷
4. **进工作区 hook 看 welcome-store**,有信号 → 按 runtime/skip 分两路调通用 `createAgent` / `createIssue`
5. **Helper instructions / starter prompt / install-runtime issue 描述(EN/ZH)全部在前端 TS 模块**(`packages/views/onboarding/templates/`)

外加双向路由 hard gate:`apps/web/app/[workspaceSlug]/layout.tsx` 拦未 onboarded → `/onboarding`;`apps/web/app/(auth)/onboarding/page.tsx` 拦已 onboarded → workspace。桌面端用 `App.tsx` 的 overlay 决策达成同样效果(no URL bar, no router.replace)。

## 3. 4 类用户链路

### 3.1 新用户,Step 3 选 runtime

```
Step 3 [开始探索]
  → completeOnboarding("full", workspace.id)      ─ 后端只 mark
  → welcomeStore.set({wsId, choice:"runtime", runtimeId})
  → navigate(/<slug>/issues)
workspace layout gate: onboarded_at != NULL ✓ 放行
<WelcomeAfterOnboarding /> consumes welcome-store:
  phase 1: full-screen loading "Preparing your Helper…"
           api.listAgents → 查重名 Multica Helper(workspace 可见的)
           → 找到 → 复用
           → 没找到 → api.createAgent({name, instructions: HELPER_INSTRUCTIONS[lang], runtime_id, ...})
  phase 2: blocking Modal(无关闭按钮 / Escape no-op / outside-click no-op)
           Helper avatar + name + description + 3 张 starter cards
  phase 3: 用户挑卡 → api.createIssue({title, description: prompt,
           assignee_type:"agent", assignee_id: agent.id}) → navigate(issue)
```

### 3.2 新用户,Step 3 点 Skip

```
Step 3 [跳过]
  → completeOnboarding("runtime_skipped", workspace.id)
  → welcomeStore.set({wsId, choice:"skip"})
  → navigate(/<slug>/issues)
workspace layout gate ✓ 放行
<WelcomeAfterOnboarding /> consumes signal:
  Modal 打开(可关闭)
  后台并发 api.createIssue × 2:
    - Connect a runtime to start using agents
    - Create your first Multica Agent
  渲染 2 张 issue 卡;点卡 → navigate;关 Modal → 留在 /issues 列表
```

### 3.3 被邀请用户

```
AcceptInvitation handler:
  事务内 qtx.CreateMember + qtx.MarkUserOnboarded(必须保留!)
navigate(/<slug>/issues) 不带 welcome-store 信号
workspace layout gate ✓ 放行
<WelcomeAfterOnboarding /> 无信号 → 渲染 null
```

### 3.4 老用户回访

```
resolvePostAuthDestination: hasOnboarded ✓ + workspace[0] → /<slug>/issues
layout gate ✓ 放行
welcome-store 无信号 → 不弹
```

## 4. 数据 / 字段终态

| 字段 | 含义 | 谁读 |
|---|---|---|
| `users.onboarded_at` | 唯一 onboarding 完成信号 | layout hard gate × 2 |
| `users.onboarding_questionnaire` | 问卷答案 | OnboardingFlow 预填 |
| (PostHog `OnboardingCompleted` event) | Skip vs Connect 历史分析 | 数据分析 |

**删掉**:`onboarding_runtime_id`、`onboarding_runtime_skipped` 列 + `user_onboarding_runtime_choice_check` CHECK 约束。Welcome 触发改用前端 Zustand transient store(`packages/core/onboarding/welcome-store.ts`),read-once-then-clear 语义。

## 5. 关键设计决策

### D1:NavigationAdapter 不支持 state → Zustand transient store

`packages/views/navigation/types.ts` 的 `push(path: string): void` 不接 state,所以不能用 `navigate(path, {state})` 模式。改用 `packages/core/onboarding/welcome-store.ts`:Step 3 完成时 `set({...})`,Welcome hook mount 时 `consume()`(一次读 + 清空)。store 非 persist → 刷新即丢 = Welcome Modal 不会被刷新重弹,符合"一次性体验"预期。

### D2:Desktop 路由 hard gate 走 overlay 而非 router.replace

桌面端 onboarding 是 `WindowOverlay`,不是 react-router 路由。`apps/desktop/src/renderer/src/App.tsx` 的 overlay 决策 effect 增加规则:`!hasOnboarded` 一律 `setCurrentWorkspace(null,null) + open onboarding overlay`(不管 wsCount)。web 端在 `layout.tsx` 用 `router.replace(paths.onboarding())` 直接跳。两套实现在效果上等价。

### D3:`resolvePostAuthDestination` 改回 onboarded-first

V2 改成了 workspace-presence-first(为了不把 mid-flow 用户踢回 onboarding),v3 还原成 onboarded-first。理由:layout hard gate 会在 `!onboarded` 时强制 redirect,resolver 直接走 workspace 只是浪费一次 navigation;mid-flow 在 v3 下概率极低(仅 Step 2 完关 app 这一窗口)。

### D4:Helper instructions / 长文案放 TS 模块,不放 i18n JSON

instructions 是 94 行 markdown,issue 描述是 60+ 行带列表 / 代码块。放在 `packages/views/onboarding/templates/` 下 3 个 TS 模块,每个导出 `{en, zh}` 字符串 const。短文案(Modal 标题 / 副标题 / 按钮 / 卡片标题)仍走 i18n JSON。

### D5:重名 Helper 防重

刷新 / StrictMode 双 mount 都可能让 hook 多次进入。前端用:
1. `useRef` 一次性锁 prevents 同 mount 内重复触发
2. Welcome 信号 `consume()` 一读即清,即使 React 多次 mount 也只第一次拿到信号
3. 进 `createAgent` 前先 `api.listAgents` 查重名 + visibility="workspace" + !archived,有则复用

### D6:失败处理

- runtime 路径:阻塞 Modal + 失败 retry UI(无关闭),用户卡死时 `onAbandon` 把信号清空恢复路由
- skip 路径:Modal 可关闭,每条 issue 失败显示 per-card retry
- `onboarded_at` 已 set,失败不阻塞 — 用户最坏情况是手动建 agent / 关 Modal 用空 workspace

## 6. 文件改动盘点(相对 main)

### 删
- `server/internal/service/onboarding.go`
- `server/internal/service/workspace_content.go`
- `server/internal/handler/onboarding.go`:`BootstrapOnboardingRuntime`、`BootstrapOnboardingNoRuntime` handler 整体 + 相关常量(`onboardingAssistantInstructions` 94 行 markdown、`onboardingAssistantName`、`onboardingIssueTitle`、`onboardingIssueDescription`、`onboardingAssistantAvatarURL`、`onboardingAssistantDescription`、`onboardingAgentTemplate`)
- `server/internal/handler/workspace.go`:`EnsureOnboardingContent` handler
- `server/cmd/server/router.go`:3 条路由(`runtime-bootstrap` / `no-runtime-bootstrap` / `ensure-onboarding-content`)
- `server/internal/handler/handler.go`:`OnboardingService` / `WorkspaceContent` 字段 + 实例化
- `server/internal/handler/onboarding_test.go`:5 个 Bootstrap* 测试 + 2 个 PatchOnboarding 测试(只留 JoinCloudWaitlist 测试)
- `packages/views/workspace/workspace-onboarding-init.tsx` + 测试
- `packages/views/workspace/onboarding-helper-modal.tsx` + 测试
- `packages/core/onboarding/store.ts`:`recordOnboardingRuntimeChoice` / `recordOnboardingRuntimeSkipped` / `bootstrapRuntimeOnboarding` / `bootstrapNoRuntimeOnboarding`
- `packages/core/api/client.ts`:`bootstrapOnboardingRuntime` / `bootstrapOnboardingNoRuntime` / `ensureOnboardingContent` + Response interfaces + EMPTY consts
- `packages/core/api/schemas.ts`:两个 Bootstrap schemas + User schema 的两字段
- `packages/core/types/workspace.ts`:User 的两个新字段
- `packages/views/locales/{en,zh-Hans}/onboarding.json`:`onboarding_helper_modal.*` 和 `workspace_init.*` keys

### 改
- `server/migrations/098_user_onboarding_runtime_choice.up.sql`:反转为 DROP CONSTRAINT/COLUMN IF EXISTS(同名文件、保留 098 序号)
- `server/migrations/098_user_onboarding_runtime_choice.down.sql`:no-op + 注释说明不可逆
- `server/pkg/db/queries/user.sql`:`PatchUserOnboarding` 回退到只接 questionnaire
- `server/internal/handler/onboarding.go`:`CompleteOnboarding` 简化为直接 `qtx.MarkUserOnboarded`(无 service 包装)+ `PatchOnboarding` 只接 questionnaire
- `server/internal/handler/invitation.go`:`AcceptInvitation` 改回直接 `qtx.MarkUserOnboarded`(保留 mark 调用 — 注释强调 layout gate 依赖)
- `server/internal/handler/workspace.go`:`CreateWorkspace` 删 `ClaimStarterContentStateIfUnset` 调用(后端不再触碰 `starter_content_state`)
- `server/internal/handler/auth.go`:`UserResponse` / `userToResponse` 删两字段
- `server/internal/handler/workspace_test.go`:测试断言保留,注释更新引用 layout gate 而非 OnboardingHelperModal
- `apps/web/app/[workspaceSlug]/layout.tsx`:加 hard gate effect + 改挂 `<WelcomeAfterOnboarding />`
- `apps/desktop/src/renderer/src/App.tsx`:overlay 决策加规则 — `!hasOnboarded` 一律开 onboarding overlay 并先 `setCurrentWorkspace(null,null)`
- `apps/desktop/src/renderer/src/components/workspace-route-layout.tsx`:`<WorkspaceOnboardingInit />` → `<WelcomeAfterOnboarding />`
- `packages/core/onboarding/index.ts`:精简 exports + 加 welcome-store exports
- `packages/core/paths/resolve.ts`:回到 onboarded-first 优先级
- `packages/core/paths/resolve.test.ts`:断言同步翻新
- `packages/views/onboarding/onboarding-flow.tsx`:`handleRuntimeNext` 改为 await `completeOnboarding` + `welcomeStore.set` + navigate
- `apps/web/test/helpers.tsx`:mockUser 删两字段
- `packages/views/package.json` exports:删 v2 两个 workspace 入口,加 `./workspace/welcome-after-onboarding`

### 加
- `packages/core/onboarding/welcome-store.ts` + 测试:Zustand transient store
- `packages/views/onboarding/templates/helper-instructions.ts`:Helper agent 系统提示词,EN/ZH 各一份
- `packages/views/onboarding/templates/install-runtime-issue.ts`:Skip 路径第 1 条 issue 文案(从 server 的 EN/ZH 双语描述完整搬过来)
- `packages/views/onboarding/templates/create-agent-guide-issue.ts`:Skip 路径第 2 条新 issue 文案(EN/ZH)
- `packages/views/onboarding/templates/index.ts`:barrel + `pickContentLang(language)`
- `packages/views/workspace/welcome-after-onboarding.tsx`:替代 v2 两个组件的统一 Welcome 体验
- `packages/views/workspace/welcome-after-onboarding.test.tsx`:5 个测试覆盖 runtime / skip / 重名复用 / navigate / 渲染门
- `packages/views/locales/{en,zh-Hans}/onboarding.json`:新增 `welcome_after_onboarding.{loading_helper,error_*,retry,dismiss_error,runtime.{title,subtitle,helper_description,cards.*},skip.{title,subtitle,open_issue,close,cards.*}}`

## 7. 老用户兼容性

生产数据(数据分析师 read-only audit):
- `users_with_workspace` = 33,172
- 其中 `onboarded_at IS NULL` = **0**

→ 没有 mid-flow 用户需要 backfill 或邮件通知。migration 098 反转 SQL 在生产里是 no-op(没有列要删,因为生产从未运行过 v2 的 add-column);在 dev 数据库上 IF EXISTS 保护让任何状态都能干净收敛到"无字段"。

老用户(`onboarded_at != NULL`)100% 安全:
- web layout hard gate `if (user.onboarded_at == null)` → 永不触发
- desktop App.tsx overlay 决策 `!hasOnboarded && wsCount > 0` → 永不触发
- welcome-store 在 onboarding-flow.tsx Step 3 之外永不 `set` → Welcome Modal 永不弹

被邀请用户(invitee)的 `AcceptInvitation` 必须保留 `qtx.MarkUserOnboarded` — 注释里强调:"DO NOT REMOVE 否则 layout gate 把 invitee 踢回 /onboarding"。

## 8. 验收 — invariant + deprecation shim

每条可用 grep 或单元测试机械验证:

1. `onboarded_at` 的写入收敛到 3 个 handler:`CompleteOnboarding`(v3 主路径)、`AcceptInvitation`(invitee 必须)、`BootstrapOnboarding*`(**shim,见 §10**)。全部直接调 `qtx.MarkUserOnboarded`,无 service 包装。`grep -rln "qtx.MarkUserOnboarded\|h.Queries.MarkUserOnboarded" server/internal/handler/ | grep -v _test.go`
2. v3 前端永不调旧 bootstrap endpoint:`grep -rn "bootstrapOnboardingRuntime\|bootstrapOnboardingNoRuntime\|ensureOnboardingContent" packages/ apps/` → 0 命中
3. v2 字段全删:`grep -rn "onboarding_runtime_id\|onboarding_runtime_skipped" packages/ apps/` → 0 命中
4. Welcome Modal 不再自查环境:`grep -rn "runtimeListOptions" packages/views/workspace/` → 0 命中
5. welcome-store 信号只来自 onboarding-flow,只被 welcome-after-onboarding 消费(`grep -rln "useWelcomeStore"` 应该只命中 4 个文件:store、welcome 组件、onboarding-flow、各自的测试)
6. 老用户登录 → 直接进工作区 → 不弹 Modal、无 loading veil(场景 4 手动验证)

## 10. Deprecation shim — `BootstrapOnboarding*`

**为什么保留**:v3 server 发布到 desktop auto-update 完成之间有 ~30 分钟真空期,期间老桌面会调旧 endpoint。删了 endpoint → 老桌面 404 → 新用户 onboarding 死循环。

**位置**:`server/internal/handler/onboarding_shim.go`(独立文件,所有 deprecated 代码都在这里;v3 主路径 `onboarding.go` 不含一行 shim 代码)。

**约束**:
- 简化版实现:handler 直接 `qtx.MarkUserOnboarded` + `qtx.CreateAgent` + `qtx.CreateIssue` + `qtx.SetStarterContentState`,不重新引入 `OnboardingService` / `WorkspaceContentService` 抽象层(那是 v2 的复杂度,已经死了)
- 文案常量完整保留(`onboardingAssistantInstructions` 94 行 markdown、`enNoRuntimeIssueDescription` 等),和前端 `packages/views/onboarding/templates/` 双轨并行,**这两份必须保持同步**直到 shim 删除
- `claimStarterContentStateIfUnset` 局部 helper 也保留,因为老桌面靠这个字段抑制 legacy starter-content dialog
- 5 个回归测试(`TestBootstrapOnboarding*`)保留,保护 shim 行为不被无意改动

**删除条件**:`X-Client-Version` telemetry 确认无任何活跃 desktop 在调这俩 endpoint(典型窗口:2-3 个 release 后)。删的时候一并删:
- `server/internal/handler/onboarding_shim.go` 整文件
- `server/cmd/server/router.go` 里 2 条 deprecated 注释 + 路由
- `server/internal/handler/onboarding_test.go` 末尾 5 个 `TestBootstrapOnboarding*`

## 9. 未来工作

- `starter_content_state` 列(老桌面端兼容)— v3 后端不再触碰,但列保留(老桌面读 NULL → 渲染 legacy 导入 dialog)。可在 desktop 0.2.x 之前的版本全部 EOL 后单独 PR 删
- `OnboardingCompletionPath` 里的 `cloud_waitlist` enum 值实际无人发送 — 可清理
- `apps/web/app/[workspaceSlug]/layout.test.tsx`(新增)+ `apps/desktop/src/renderer/src/App.test.tsx`(新增用例)覆盖两端 hard gate 的 redirect 行为 — 当前由手动 E2E 覆盖
