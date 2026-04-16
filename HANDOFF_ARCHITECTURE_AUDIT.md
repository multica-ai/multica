# Architecture Audit — Workspace & Realtime Cache

> 基于代码审计整理的 4 个任务。优先级：P0 一个、P1 一个、P2 两个。每个任务都包含问题、根因、受影响的 issue、复现步骤、修复方案、改动范围。

---

## 任务 1 — [P0] 空闲后列表数据陈旧

**关联 issue**：[#951](https://github.com/multica-ai/multica/issues/951)

### 问题

用户登录后静置一段时间，Issue 列表里缺失一部分数据（其他成员期间新建/变更的 issue 不出现）。登出再登入可以恢复。`ec5af33b` 声称 "Closes #951"，但 issue 仍为 OPEN 状态 —— 因为它只修了 401 一种场景，没修 WS 半开这一种。

### 根因

系统把 cache 新鲜度的全部责任压给了 WebSocket 推送：

- `packages/core/query-client.ts:7` — `staleTime: Infinity`，cache 永不主动过期
- `packages/core/query-client.ts:9` — `refetchOnWindowFocus: false`，tab 重新获得焦点也不 refetch
- 依赖 WS 推送 `issue:created` / `issue:updated` 事件 invalidate cache

但 WS 层存在一个**不对称**：

- **服务端**：`server/internal/realtime/hub.go:83-96, 420-475` 有 54s ping / 60s pongWait，会清理死连接
- **客户端**：`packages/core/api/ws-client.ts`（142 行全貌）**完全没有心跳检测**，只靠 `onclose` 事件触发重连

浏览器原生 `WebSocket` API 不把 ping/pong 帧暴露给 JS，所以 JS 层无法主动探测 "半开" 连接。当 NAT / 负载均衡器 / 笔记本睡眠导致 TCP 连接被静默切断时：

1. 浏览器 `readyState` 仍是 `OPEN`
2. `onclose` 不触发
3. `ws-client.ts:70-73` 的 3 秒重连逻辑不跑
4. `packages/core/realtime/use-realtime-sync.ts:462-487` 的 `onReconnect` 全量 invalidate 不跑
5. 期间的 WS 事件进黑洞
6. cache 保持旧快照

### 复现

**浏览器 DevTools 里的 "Block request URL" 不行** —— 那会触发 `onclose`，走正常重连 → 不复现。真正的半开需要在网络层静默丢包。

**方法 A（推荐，最接近真实场景）**：macOS 用 pfctl 丢包

```bash
# 假设后端在 8080
sudo pfctl -E
echo "block drop out quick proto tcp to any port 8080" | sudo pfctl -f -

# 观察:
# - Console 里没有 "disconnected, reconnecting in 3s" 日志
# - Network 里 WS 连接仍显示 Pending / 101
# 用另一个账号/CLI 创建一个 issue
# 回到原客户端: 列表不更新
# 登出再登入: 列表恢复完整

sudo pfctl -d  # 解除
```

**方法 B（不动网络）**：临时修改代码，在 `packages/core/api/ws-client.ts:52` 的 `onmessage` 处理器里加一行 `return;` 在前面，吞掉所有入站消息。效果等价于半开。

### 修复方案（三个选项，推荐 C）

#### 选项 A — 浏览器端心跳探活（治本，改动大）

在 `ws-client.ts` 加客户端侧的心跳检测：记录 `lastMessageTime`，定时器检查若超过 N 秒没收到任何消息就主动 `ws.close()`，触发现有重连逻辑。

- 优点：从根本上解决半开问题
- 缺点：浏览器原生 API 没有 ping 能力，需要服务端配合发"应用层 heartbeat"消息供客户端更新 `lastMessageTime`；服务端改 + 客户端改

#### 选项 B — Page Visibility API 触发 invalidate（治标，改动小）

在 `packages/core/platform/core-provider.tsx` 加 `visibilitychange` 监听，tab 重新可见时强制 `queryClient.invalidateQueries({ queryKey: issueKeys.all(wsId) })`（及其他关键 key）。

- 优点：~10 行代码，能兜住 80% 场景（睡眠、切后台 tab）
- 缺点：treats symptom, 不是真正的半开检测；对"一直保持 tab 可见但网络层断了"的场景无效

#### 选项 C — **A + B 组合**（推荐）

- 短期上 B，立刻止血
- 中期上 A，把 cache 新鲜度从"只信 WS"改成"WS 是优化，Visibility 是兜底"
- 可选加 `refetchOnWindowFocus: true` 或把 `staleTime` 改成一个有限值（比如 5 min），作为第三层保险

### 改动范围

| 方案 | 文件 | 改动规模 |
|---|---|---|
| B | `packages/core/platform/core-provider.tsx` | ~10 行 |
| A 客户端 | `packages/core/api/ws-client.ts` | ~30 行 |
| A 服务端 | `server/internal/realtime/hub.go` | 加 app-level heartbeat message |

### 验证

修完之后：

1. 跑方法 A 复现流程，确认数据不再丢失
2. 加 e2e 测试：模拟 `document.dispatchEvent(new Event('visibilitychange'))` + 验证 issue list 被 refetch

---

## 任务 2 — [P1] Workspace 不在 URL 路径中

**关联 issue**：MUL-723（slug 不在 URL）、MUL-43（切换 workspace 报错）、MUL-509（手机端无法切换）

> **注意**：审计中提到的 MUL-43 / MUL-476 issue 编号需要当面核对一次 —— agent 查询 GitHub 后返回的标题对不上（看起来是别的 PR）。交接时请让执行人以具体症状为准。

### 问题

当前 workspace 身份完全靠 `X-Workspace-ID` HTTP header + Zustand store + localStorage 承载，URL 里没有 workspace 信息。所有路径都是 `/issues`、`/issues/:id` 这种 workspace-agnostic 的。

### 根因

**数据库和 API 已经支持 slug**：

- `server/migrations/001_init.up.sql:15-23` — workspace 表有 `slug TEXT UNIQUE NOT NULL`
- `server/pkg/db/queries/workspace.sql:11-13` — 有 `GetWorkspaceBySlug` 查询
- `packages/core/types/workspace.ts:8-19` — Workspace 类型里有 slug 字段

**但前端路由和导航层没用它**：

- Web 路由：`apps/web/app/(dashboard)/` 下 25 个 route file 都是 workspace-implicit
- Desktop 路由：`apps/desktop/src/renderer/src/routes.tsx:71-143` 同样
- Navigation 适配器 `apps/web/platform/navigation.tsx` 直接透传 `router.push`，没有任何 workspace 前缀逻辑

**workspace 切换只靠 sidebar UI**（`packages/views/layout/app-sidebar.tsx:284-286`）：

```tsx
if (ws.id !== workspace?.id) {
  push("/issues");              // 硬跳 /issues（workspace-implicit！）
  switchWorkspace(ws);           // 然后改 store
}
```

这种设计使得：

- 手机端因为没 sidebar UI，也没 URL 层切换入口，**完全切不了 workspace**（MUL-509）
- 把 `/issues/xxx` 链接发给处于不同 workspace 的同事，会打开错误 workspace 下的 issue，或找不到报错（MUL-43 系列）
- 分享链接没有 workspace 上下文，接收方必须先手动切对 workspace

### 复现

1. **MUL-723**：登录 → 观察地址栏，没有任何 workspace 标识
2. **MUL-43**：
   - 加入两个 workspace A 和 B
   - 在 A 中打开某个 issue `/issues/abc123`
   - 切到 B，URL 不变 → 访问失败 / 显示错数据
3. **MUL-509**：手机浏览器打开，尝试切 workspace → 无法切换（UI 不显示 sidebar 触发器或触发器无法切）

### 修复方案（三个选项，推荐 A）

#### 选项 A — `/ws/:slug/...` URL 前缀（根本方案，推荐）

所有路径加上 workspace slug 前缀。例如 `/issues/abc123` → `/ws/my-team/issues/abc123`。

**要改的地方**：

1. **Web 路由目录结构**：`apps/web/app/(dashboard)/` 下全部搬到 `apps/web/app/(dashboard)/ws/[slug]/...`（~25 个文件）
2. **Desktop 路由**：`apps/desktop/src/renderer/src/routes.tsx:71-143` 给所有路径加 `/ws/:slug` 前缀
3. **Navigation 适配器**：
   - `apps/web/platform/navigation.tsx` — `push(path)` 内部前置 `/ws/${workspace.slug}`，`pathname` 读取时去掉前缀
   - `apps/desktop/src/renderer/src/platform/navigation.tsx` — 同上
4. **Sidebar 切换逻辑**：`packages/views/layout/app-sidebar.tsx:284-286` 改成 `push('/ws/${ws.slug}/issues')`（或依赖适配器自动加前缀就不用改）
5. **服务端中间件**：`server/internal/middleware/workspace.go:41-46` 增加 "从 URL path 解析 slug → 查 ID → 校验 membership" 的逻辑，header 继续作为 fallback（迁移期兼容）

**预计改动**：~50-100 个文件（大部分是 route 搬迁，不是逻辑改动）、~5-7 人天

**不改也能工作的部分**：
- `packages/core/api/client.ts` — 仍旧走 header，不用改
- 所有 `packages/views/` 下的组件 —— 它们用 `useNavigation().push()` 抽象，适配器层处理前缀就行

**风险**：
- 旧的 bookmark URL 失效（如果产品还没正式 ship，问题不大）
- E2E 测试需要更新所有 URL 断言

#### 选项 B — `?ws=slug` query param（折中）

URL 形如 `/issues?ws=my-team`。改动更小（~30 个文件），URL 丑但向后兼容。推荐度低于 A。

#### 选项 C — 只修症状不动架构

在 `switchWorkspace` 和各个 query 之间加 debounce、error boundary 等 workaround。不解决根因，技术债越攒越多。**不推荐**。

### 改动范围（选项 A）

| 模块 | 文件数 | 备注 |
|---|---|---|
| Web routes | ~25 | 目录搬迁 |
| Desktop routes | 1 | 路径前缀 |
| Navigation adapters | 2 | 前缀逻辑 |
| Server middleware | 1-2 | slug → ID 解析 |
| 组件（不用改） | 30-40 | 用 `useNavigation` 的不受影响 |
| E2E tests | 20-30 | URL 断言更新 |

---

## 任务 3 — [P1] Workspace 切换时 navigation 状态未隔离

**关联 issue**：MUL-43（切换报错）、MUL-476（本地缓存未按 workspace 隔离）

> 同上，这两个编号建议交接时核对症状。

### 问题

绝大多数 workspace-scoped 的 Zustand store 都正确使用了 `createWorkspaceAwareStorage`（key 后缀加 wsId 自动隔离），但 **`useNavigationStore` 是个例外**：它持久化了 `lastPath`，但用的是 global storage，切换 workspace 后里面仍是上个 workspace 的路径。

### 根因

**`packages/core/navigation/store.ts:15-31`**：

```typescript
export const useNavigationStore = create<NavigationState>()(
  persist(
    (set) => ({
      lastPath: "/issues",
      onPathChange: (path) => { /* ... */ set({ lastPath: path }); },
    }),
    {
      name: "multica_navigation",
      storage: createJSONStorage(() => createPersistStorage(defaultStorage)), // ← 这里用的是 global，不是 workspace-aware
      partialize: (state) => ({ lastPath: state.lastPath }),
    }
  )
);
// ← 没有调 registerForWorkspaceRehydration
```

**对比：其他 store 都是正确的**：

| Store | 是否 workspace-aware | 是否注册 rehydration |
|---|---|---|
| useNavigationStore | ❌ | ❌ |
| useIssuesScopeStore | ✅ | ✅ |
| useIssueDraftStore | ✅ | ✅ |
| useRecentIssuesStore | ✅ | ✅ |
| useIssueViewStore | ✅ | ✅ |
| myIssuesViewStore | ✅ | ✅ |
| useChatStore | ✅（手动用 wsKey）| ✅ |

另外 `packages/core/platform/storage-cleanup.ts:10-19` 的 `WORKSPACE_SCOPED_KEYS` 列表里也漏了 `multica_navigation`。

**现有的 workaround**：`packages/views/layout/app-sidebar.tsx:285` 切 workspace 时硬跳到 `/issues`，正是为了绕开这个 bug。修好 navigation store 之后这行 hack 可以删掉。

### 复现

1. 在 workspace A 中打开一个具体 issue `/issues/abc123`
2. 切到 workspace B
3. 观察：如果没有 sidebar 的硬跳 workaround，会尝试恢复到 `/issues/abc123`，但那个 issue 不属于 B，导致 404 或错误

目前因为有硬跳 workaround，症状表现为"切 workspace 后总是回到 issue 首页"—— 这本身也是 bug（用户期望记住上次位置）。

### 修复方案（推荐 Option C：组合）

**三处改动**：

1. `packages/core/navigation/store.ts:28` —— 把 `createPersistStorage(defaultStorage)` 改成 `createWorkspaceAwareStorage(defaultStorage)`
2. 同文件在末尾加：`registerForWorkspaceRehydration(() => useNavigationStore.persist.rehydrate());`
3. `packages/core/platform/storage-cleanup.ts:10-19` 的 `WORKSPACE_SCOPED_KEYS` 数组里加 `"multica_navigation"`

**可选**：清理 `packages/views/layout/app-sidebar.tsx:285` 的 `push("/issues")` workaround（改完之后不再需要）。

### 改动范围

| 文件 | 改动 |
|---|---|
| `packages/core/navigation/store.ts` | 改 storage 类型、加 rehydration 注册（~3 行） |
| `packages/core/platform/storage-cleanup.ts` | 数组加一行 |
| `packages/core/platform/workspace-storage.test.ts` | 加 rehydration 的单测 |
| `packages/views/layout/app-sidebar.tsx`（可选） | 移除硬跳 workaround |

**风险**：极低。只是把 navigation store 对齐到其他 store 已经在用的模式。

---

## 任务 4 — [P2] Workspace 生命周期副作用散落

**关联 issue**：MUL-727（创建后闪页）、MUL-728（删除确认）、MUL-820（接受邀请不自动切）

### 问题

创建 / 删除 / 切换 / 加入 workspace 的副作用分散在 mutation 的 `onSuccess` 和各处 UI 回调里，没有统一抽象。几个具体 bug：

### 4.1 MUL-727 — 创建 workspace 后闪一下 `/issues` 再跳 `/onboarding`

**根因**：两个 `onSuccess` 回调同时跑，顺序不确定。

- `packages/core/workspace/mutations.ts:7-21` 的 `useCreateWorkspace.onSuccess` 里调了 `switchWorkspace(newWs)` —— 同步改 Zustand，`/issues` 路由开始用新 workspace 渲染
- `packages/views/modals/create-workspace.tsx:68-70` 的 UI `onSuccess` 里调了 `router.push("/onboarding")` —— 异步 schedule 导航

于是：`/issues` 先渲染（闪一下）→ 导航到 `/onboarding`。

**修复**：把 `switchWorkspace` 从 mutation 里拿出来，让 UI 层主导。在 `create-workspace.tsx` 的 `onSuccess` 里先 `switchWorkspace` 再 `push`，保证同一个微任务里完成。

**文件**：`packages/core/workspace/mutations.ts`、`packages/views/modals/create-workspace.tsx`、可能 `packages/views/onboarding/step-workspace.tsx`

### 4.2 MUL-728 — 删除 workspace 的"缺少确认"

**核查结果**：`packages/views/settings/components/workspace-tab.tsx:102-119, 236-255` **已经有 AlertDialog 确认**了。

**真实问题**：删除成功后**没有导航**，用户停在 `/settings`，而当前 workspace 已经是删除后系统挑的另一个。

**修复**：在 `handleDeleteWorkspace` 的 `onConfirm` 成功分支里加 `push("/issues")`。

**文件**：`packages/views/settings/components/workspace-tab.tsx`（加一行）

### 4.3 MUL-820 — 接受邀请不自动切换 workspace

**核查结果**：有两条路径：

- ✅ `/invite/:id` 独立页（`packages/views/invite/invite-page.tsx:32-52`）是**正确的**：accept → switchWorkspace → push("/issues")
- ❌ **Sidebar 下拉里的 "Join" 按钮**（`packages/views/layout/app-sidebar.tsx:203-209, 321-324`）**是错的**：只 invalidate cache，不切也不跳

**修复（推荐 Option 2）**：Sidebar 的 "Join" 改成跳转到 `/invite/:id` 页面，不再就地接受。单一入口、单一行为。

```tsx
<DropdownMenuItem onClick={() => push(`/invite/${inv.id}`)}>
  {inv.workspace_name}
</DropdownMenuItem>
```

**文件**：`packages/views/layout/app-sidebar.tsx`（~10 行）

### 复现

| Issue | 步骤 |
|---|---|
| MUL-727 | 创建新 workspace → 仔细看是否闪了一下 `/issues` 再跳 `/onboarding` |
| MUL-728 | 删除当前 workspace → 观察删完后是否留在 `/settings` 页面（BUG: 没有自动跳走） |
| MUL-820 | 被邀请用户登录 → sidebar 下拉 → 点 "Join" → 观察当前 workspace 是否切过去（BUG: 不切）|

### 长期架构建议（可选）

抽一个 `useWorkspaceLifecycle` hook 统一管这些副作用。Agent 报告里有完整设计，文件：`packages/core/workspace/hooks.ts`（新建）。但建议先修 MUL-727/728/820 三个具体 bug，hook 抽象作为后续迭代。

### 改动范围

| Issue | 文件 | 改动规模 |
|---|---|---|
| MUL-727 | mutations.ts + create-workspace.tsx | ~10 行 |
| MUL-728 | workspace-tab.tsx | ~1 行 |
| MUL-820 | app-sidebar.tsx | ~10 行 |

---

## 总览

| 任务 | Issue | 优先级 | 预估规模 | 风险 |
|---|---|---|---|---|
| 1. WS 半开 + 陈旧 cache | #951 | **P0** | Option B ~10 行；Option C ~1-2 天 | 低 |
| 2. Workspace URL 化 | MUL-723/43/509 | P1 | 5-7 人天（大部分是搬迁）| 中（影响面大、e2e 要改）|
| 3. Navigation store 隔离 | MUL-43/476 | P1 | ~0.5 天 | 低 |
| 4. Workspace 生命周期 bug | MUL-727/728/820 | P2 | ~1 天 | 低 |

### 建议推进顺序

1. **立刻做**：任务 1 的 Option B（visibilitychange 触发 invalidate）—— 代码最少、收益最明显，能当天止血
2. **同步开始**：任务 3（navigation store 隔离）—— 影响小、风险低、顺便清掉一个 workaround
3. **规划立项**：任务 2（URL 化）—— 大改造，需要单独开一个 iteration
4. **次要修补**：任务 4 的三个小 bug —— 可以拆成独立 PR，各自 review

### 重要澄清

- **Issue 编号核对**：MUL-43 / MUL-476 的编号需要核对一次，agent 查询 GitHub 返回的标题看起来对不上（可能是内部 issue tracker 编号 vs GitHub 编号混用）。以症状为准。
- **MUL-728 实际状态**：确认对话框已经存在，真实缺的是"删除后跳走"。
- **MUL-820 实际状态**：`/invite/:id` 页面路径工作正常，只是 sidebar 下拉按钮坏了。

### 所有关键代码位置索引

```
packages/core/query-client.ts:7-10          # staleTime: Infinity
packages/core/api/ws-client.ts:1-142        # 客户端 WS，无心跳
packages/core/realtime/use-realtime-sync.ts:462-487  # onReconnect 全量 invalidate
packages/core/platform/core-provider.tsx    # 加 visibilitychange 的位置
packages/core/navigation/store.ts:15-31     # lastPath 未隔离
packages/core/platform/storage-cleanup.ts:10-19  # WORKSPACE_SCOPED_KEYS
packages/core/workspace/store.ts:43-77      # hydrateWorkspace / switchWorkspace
packages/core/workspace/mutations.ts:7-57   # create/leave/delete 三个 mutation
packages/views/layout/app-sidebar.tsx:203-324  # 侧边栏切 workspace、接受邀请入口
packages/views/modals/create-workspace.tsx:63-82  # 创建 workspace 入口
packages/views/settings/components/workspace-tab.tsx:102-119  # 删除 workspace 入口
packages/views/invite/invite-page.tsx:32-52 # 接受邀请正确实现参考

server/internal/realtime/hub.go:83-96       # 服务端 WS 心跳
server/internal/middleware/workspace.go:41-46  # wsId resolution
server/migrations/001_init.up.sql:15-23     # workspace.slug 已存在
```
