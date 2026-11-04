# 本地会话客户端模型与去 iframe 方案

日期：2026-11-03。状态：已实现（分支 `claude/ai-conversation-design`）。基于 `2e4755e7e`，已合入两轮代码评审的结论。

本方案解决三个问题：

1. 对话 GUI 运行在 iframe 中，只为隔离第三方样式，却带来一层 RPC 和重建成本。
2. Desktop 没有保存会话内容的领域模型。切回 tab 时内容已经丢失，只能重新读快照。
3. 打开会话固定读最近 20 轮，比实际需要的多；更早的历史又读不到。

目标是一步到位：先去掉 iframe，再做领域模型和读取策略，中间不写过渡代码。所有阶段完成后一起部署。

daemon 仍然不缓存会话内容，请求直接读 SQLite。daemon 只做两处读取改动：增加 `turn_limit`，以及在快照中带上窗口外仍在运行的后台 item（第 7 节）。

## 1. 现状

| 现象 | 代码依据 |
| --- | --- |
| 同一时间只挂载一个 tab，切 tab 会卸载 iframe | `apps/desktop/src/renderer/src/components/tab-content.tsx` |
| 会话内容 `V2Conversation` 只存在于 iframe 内，随之销毁 | `packages/tutti-agent-gui-host/src/runtime/create-multica-agent-gui-runtime.ts` |
| 每次同步会话都读快照 | 同上，`ensureSessionSynchronized` → `loadSession` → `local.snapshot` |
| 快照固定读最近 20 轮 | `server/internal/localconversation/service/service.go` 的 `snapshotTurns` |
| GUI 读不到更早的历史 | runtime 把 `hasOlderMessages` 写死为 `false`；Desktop 没有调用 `/sessions/{id}/history` |
| GUI 定制只存在于构建阶段 | `packages/tutti-agent-gui-host/vite.config.ts` 用 Vite 插件改写 vendor 源码约 102 处，并有 14 个 `@multica-*` 别名 |
| 列表已经增量更新，并且切 tab 后保留 | `apps/desktop/src/renderer/src/platform/local-v2.ts` 的 `LocalV2ScopeClient` |

daemon 已具备增量能力：内容订阅支持 `after_seq` 和 `store_generation`，游标失效时返回 `snapshot_required`；列表支持 `after_scope_seq`。Hub 补齐时每页 500 行，等客户端读完才读下一页，内存有界。

## 2. 已定决策

| 项目 | 决策 |
| --- | --- |
| 实施顺序 | 先去 iframe，再做领域模型和读取策略，不写过渡代码 |
| 模型范围 | 只处理 daemon 中的会话，包括本机和 P2P 设备。云端 Chat 和 iOS 不变 |
| 模型位置 | Desktop 渲染进程，模块级单例，不放主进程 |
| 实例层级 | 工作区 → AI 对话（对应设备）→ ConversationStore |
| 实例管理 | 由一个 Manager 统一管理所有实例 |
| Transport | 独立的公共模块，不归 Manager 管理 |
| 逻辑与 UI | 逻辑层不依赖组件。UI 决定什么时候拉，逻辑层负责怎么拉和存到哪 |
| 会话列表 | 一直保留，不淘汰；切到其他工作区后仍保持订阅 |
| 会话内容 | 模型、权限模式和队列随会话一起保存在 Store；按会话数 LRU 淘汰 |
| 离开会话 | 通知逻辑层：正在拉的这批拉完存好，然后停止，不再发起新拉取 |
| 补差与快照 | 不设差距阈值 |
| 首次读取 | 逻辑层一定先读最近 10 轮；一轮再大也按一轮计，不拆分 |
| 预拉取 | UI 判断视口上方不足三屏时，调接口让逻辑层再拉 10 轮 |
| 窗口关闭 | 逻辑层随渲染进程结束。重新打开时读快照，本机数据不丢 |
| iframe | Web 和 Desktop 的对话 GUI 都去掉；HTML、PDF 等不可信内容的预览继续用 iframe |
| `vendor/tutti` | 不再同步上游，作为自有代码直接修改 |
| 部署 | 所有阶段完成后一起部署 |

“这批”的定义：历史是正在进行的那一页；增量是订阅时刻 daemon 已提交的内容，收到 `live` 后退订。

## 3. 分层

```text
工作区
└─ AI 对话（每个设备一个入口）
   └─ ConversationStore  ←  ConversationStoreManager 统一管理
            │ 创建时注入
            ▼
Transport（按设备的单例，设备设置和预览等也在使用）
            │ HTTP + WS
            ▼
         daemon
```

| 模块 | 负责 | 不负责 |
| --- | --- | --- |
| Transport | HTTP 和 WS 连接、重连、凭据、文件 URL、连接状态 | 会话语义、游标、缓存 |
| Manager | 创建、查找和销毁实例；内容 LRU 总上限；`store_generation` 失效 | 连接和请求 |
| ConversationStore | 一个工作区在一台设备上的列表、会话内容、游标和订阅；列表的故障恢复 | 连接的建立和重连 |
| runtime | 把 Store 的内容投影给 Tutti GUI，可随时丢弃 | 保存会话状态 |

代码位置：

| 内容 | 位置 |
| --- | --- |
| Manager、ConversationStore | `apps/desktop/src/renderer/src/platform/`，由 `LocalV2ScopeClient` 演进 |
| `V2Conversation`、Store 对 runtime 暴露的接口类型 | `packages/core/local-conversation/` |
| Transport | 不变：接口在 `packages/core/local-conversation/v2.ts`，实现在 `local-v2-transport.ts` |

Store 持有服务端数据但不走 TanStack Query，因为它是按 `seq` 增量更新的流。这是状态规则的例外，写入 `AGENTS.md`。其他普通 GET 数据（`/providers`、`/folder-projects`）仍归 TanStack Query。

Web 没有 daemon，不使用 Store，继续通过 `parent-service.ts` 读云端数据。

## 4. Manager

```ts
interface ConversationStoreManager {
  /** A transport is one device. */
  get(transport: LocalV2Transport, scope: LocalV2Scope): ConversationStore;
  disposeWorkspace(userId: string, workspaceId: string): void;
  /** Signing out or a session ending. */
  disposeAll(): void;
}
```

- `workspaceId` 来自路由，Store 不保存第二份当前工作区状态。
- Store 在第一次使用时创建，是模块级单例，不随组件销毁。
- 内容 LRU 由 Manager 统一计算，初始上限为 100 个会话。淘汰只删除内容；正在使用的会话和所有列表都不淘汰。
- `store_generation` 属于设备上的 daemon 数据库。连接时比较 `/hello` 的值，任一 Store 发现变化，Manager 就让这台设备上所有 Store 失效：清空列表、`scope_seq` 和内容，正在打开的使用方收到通知后原地重载。
- 退出登录和会话过期两条路径都调用 `disposeAll`，先让 Store 进入停止状态，再调用 `transport.release(scope)`，避免 release 后的发送重建连接。`release` 要加到 `LocalV2Transport` 接口。
- 离开或删除工作区时，`DesktopShell` 根据工作区列表的变化调用 `disposeWorkspace`。
- 设备关闭共享或离线时不销毁：内容保留，禁止发送，恢复后补差；退出登录时一起清理。

## 5. ConversationStore

```ts
interface ConversationStore {
  // List: complete, incremental, never evicted.
  summaries(): LocalV2Summary[];
  summary(id: string): LocalV2Summary | undefined;
  loadSummaries(): Promise<void>;
  onSummaries(listener: () => void): () => void;

  // Content. UI calls these; none of them needs a mounted component to finish.
  /** Starts syncing; the returned function is the leave event. */
  open(id: string): () => void;
  /** Reads the first turns if missing, for callers that need the result. */
  ensure(id: string): Promise<V2Conversation>;
  /** Pulls the next page unless one is running or none is left. */
  loadOlder(id: string): Promise<void>;
  /** Watches changes only; never affects the network. */
  observe(id: string, listener: () => void): () => void;
  conversation(id: string): V2Conversation | undefined;
  version(id: string): number;
  status(id: string): { error: Error | null; loading: boolean };
  retry(id: string): void;

  command(id: string, body: Record<string, unknown>): Promise<unknown>;
}
```

### 会话数据

Store 中一个会话就是一个 `V2Conversation`，它保存：

| 字段 | 更新方式 |
| --- | --- |
| 轮次、消息、item、交互 | 事件 |
| 设置 | 快照，之后由 `conversation.updated` 按 seq 更新 |
| 队列的 revision、暂停状态、错误 | 快照和 `queue.updated`；revision 只增不减；`error` 随 payload 整体替换 |
| 队列内容 | `queue.updated` 不带内容。revision 超过已读的 `itemsRevision` 时读 `GET /sessions/{id}/queue`，单飞并合并请求；重连和重新打开时也补读 |
| `dispatching_request_id` | 只作瞬态：收到 `turn.started` 或下一次队列更新时清除 |
| 创建时间、fork 来源 | 快照，之后不变 |
| 已加载的最早轮次、是否还有更早内容 | 快照和历史翻页 |

### 内容状态流转

```text
未加载 ──open / ensure──▶ 读最近 10 轮 ──▶ 已订阅
                                          │ 所有使用方离开：拉完这批，收到 live 后退订
                                          ▼
                             已退订（保留内容和游标）
                               │                │
                              open           LRU 淘汰
                               │                │
                 after_seq 补差后回到已订阅      回到未加载
```

### 订阅状态

| 信号 | 处理 |
| --- | --- |
| `resubscribe`、序号缺口 | 用当前 `seq` 重新订阅；连续失败后才读快照 |
| `snapshot_required`、generation 变化 | 读快照 |
| `rejected` | 进入错误状态，不自动循环，由 `retry` 或重连恢复 |
| 发送失败 | 不重试，只等 `connection(true)` |
| `subscribed` 之前到达的事件 | 丢弃，它们属于已取消的旧订阅 |

### 规则

- **打开时内存里有内容：** 立即显示，再用 `after_seq` 补差，不读快照。
- **多个使用方：** 主对话、Side 和 Side 反馈组件共用一个 `V2Conversation` 和一个订阅，用引用计数决定何时算离开。隐藏的 Side 也算离开。
- **离开不取消：** Transport 不支持取消，也没必要取消。结果照常写入，合并前核对会话的 epoch 和 `store_generation`。会话被淘汰后再打开会换新 epoch，晚到的旧页面被丢弃。
- **焦点只由视图发出：** `conversation.focus` 会触发 daemon 预热和进程保活，逻辑层不发。
- **列表恢复：** Store 自己监听连接状态，重连时用 `after_scope_seq` 带退避补差，不依赖对话页。
- **连接断开：** 保留内容，禁止发送。
- **强制刷新：** 取消和回答交互后，runtime 现在靠重读快照做服务端已提交的读屏障。这些调用方逐个改为等 WS 事件；确实需要读屏障的保留 `ensure` 的强制刷新入口。

## 6. runtime 和现有代码的去向

| 现有代码 | 去向 |
| --- | --- |
| `LocalV2ScopeClient` | 演进为 ConversationStore |
| `localV2(transport, scope)` | 由 `manager.get(key)` 取代 |
| `use-local-v2-conversation.ts` 的 `entries` 缓存 | 删除，改用 Store |
| `known` / `refreshKnown` | 删除，改用 TanStack Query |
| runtime 内的 `v2` Map、`details` 的设置和队列 | 删除，改为读 Store |
| `local.snapshot`、`local.subscribe` 等 RPC | 删除，runtime 通过注入的接口调用 Store |
| `local.storedSnapshot` 及其写入 | 读写都归 Store；只用于 P2P 设备，作为内存中没有内容时的首屏 |
| `v2-projection.ts` | 保留，已是纯函数 |

切走期间完成的轮次不产生未读标记，这和现在一致，不在本方案范围内。

本机会话不持久化到磁盘。读本机 daemon 的成本低，渲染进程重启后走快照。

## 7. 读取策略

### 首次读取 10 轮

- `GET /sessions/{id}/snapshot` 和 `/history` 增加可选参数 `turn_limit`，范围 1 到 20，非法值返回 `invalid_request`。Desktop 传 10。
- 不按字节限制，也不拆分单轮。运行中的轮次照旧返回全部事件。
- 快照增加 `open_background_items`：发起轮次不在窗口内、仍在运行的后台 item。否则实时事件找不到所属轮次，会变成不显示的孤儿 item。

### 合并规则

`V2Conversation` 同时接收实时事件和历史页，必须幂等：

- 每个 item 记录 `lastSeq`，只应用 `event.seq > lastSeq` 的事件。旧页面晚到时不会把 delta 追加到最终文本上。
- 历史页合并不移动会话的 `seq`，并按 `turn_index` 重建轮次顺序。
- 实时 `turn.started` 的轮次序号由客户端推出：快照总是含最新一轮，新轮次就是下一个序号。item 的 `kind` 在后续事件带来时补全。
- 未加载轮次的实时事件保留在 item 中，轮次加载后显示。
- 重读快照时，只有轮次连续才保留已翻页的旧轮次；旧轮次含未结束的 item 时，丢弃该轮及更早内容。generation 变化时全部丢弃。

### 预拉取

```text
拉取条件：scrollTop < 3 × clientHeight，且还有更早内容，且当前没有拉取在进行
拉取一次：UI 调 store.loadOlder(id)，下一个 10 轮
拉取完成：UI 重新判断，仍不足三屏就继续
```

`useAgentGUIDetailScroll.ts` 的 `loadOlderMessagesNearTop` 是触发点，要改：

- 固定 240 px 阈值改为 3 个视口高度，并允许停在底部时拉取。
- 自动触发只在几何变化时发生，用户滚动才会对同样的几何再次请求。这样失败或没有新内容的页不会循环。
- 底部锁定分支也要清除 prepend 锚点，否则下次流式更新会把视口拉回底部。
- 加载期间不做前插补偿：此时高度增长只可能来自下方的流式内容。
- 窗口变大时也重新判断。

### 分页窗口

`hasOlderMessages` 现在有两个写入方：`applyDetail` 和分页控制器（`agentConversationMessageController.ts`）。控制器按 `hasMore = messages.length > 100` 判断，和轮次无关。改为控制器直接调 runtime 的 `loadOlder`，窗口只由 `applyDetail` 按 `V2Conversation` 写入。

### 消息版本号和排序

`ledger.ts` 的版本号只增不减，`sequence` 又是列表位置并计入指纹，所以合并旧轮次后现有消息会全部换新版本号，`merge.ts` 还拒绝版本号变小的更新。改法：

- 分页不再依赖 `beforeVersion`，版本号保持不透明。
- 本地会话的指纹不含 `sequence`，前插不改变已有消息的版本号。云端路径的游标读取依赖位置变化，保持原样。
- final 事件的 DTO 补 `first_seq` 和 `first_at`，item 的顺序和开始时间在实时构建和快照构建中一致。

### 兼容

- 不带 `turn_limit` 时维持 20 轮，iOS 不受影响。
- 新 Desktop 通过 P2P 连到旧 daemon 时，参数被忽略，仍返回 20 轮；`open_background_items` 和 `first_seq` 缺失时走默认值。
- `/history` 和 `/queue` 的响应补 zod schema 和异常响应测试。`/history` 响应异常时报错，不能用“没有更多”作 fallback，否则一次畸形响应会让分页到头。

### 其他依赖快照内容的地方

| 调用方 | 问题 | 处理 |
| --- | --- | --- |
| `local-conversation-service.ts` 的 fork 分叉点 | 分叉点从快照轮次计算，超过 10 轮后标记消失 | 由已加载内容实时计算 |
| `side-entry.tsx` 定位源轮次 | 超过 10 轮后定位失败 | 循环 `loadOlder`，直到找到或到头 |
| `local-side-workspace.tsx` 的 `resolutionSource` | 从快照解析文件路径 | 直接用 Store 中的会话，用户点击的链接必然已加载 |
| `local-conversation-flow.tsx`、`side-workspace-bridge.tsx`、`local-file-context-menu.ts`、`main/index.ts` 的 `assertTurn` | 只取摘要或存在性 | 不受影响 |

## 8. 去掉 iframe

### 第一步：把构建阶段的定制落到源码

这是其他改动的前提。Next 不运行 Vite 插件，不落地的话 Web 上这些定制全部失效。

1. 把 `hostPresentationAdaptations` 全部写入 `vendor/tutti`，删除插件。
2. 14 个 `@multica-*` 别名改为 host 的正式 `exports`。vendor 从仓库根的 `node_modules` 解析 host，依赖声明在根 `package.json`，不写在 vendor 包里：写在 vendor 包里会形成 `node_modules` 符号链接环，webpack 遍历目录时停不下来（`RangeError: Invalid array length`）。
3. `virtual:multica-html-libraries` 改为 `scripts/prepare.mjs` 生成的 TS 模块。

### 第二步：可挂载

1. host 增加 `./application` 入口。views 用 `React.lazy` 加载，不用 `next/dynamic`，GUI 不进主包，也不进 SSR。
2. `AgentGuiRpcClient` 接口保留，实现换成进程内调用。保留异步派发和 `latestInit`、`unappliedPrompts` 回放，按需复制参数。
3. 主题、语言和 `sidePane` 改为实例根节点的属性。`document.querySelector`、document 级监听限定到实例根节点。portal 容器跟随实例。
4. `inactive_host` 的语义保留：隐藏或失焦实例不能发起写操作。判断移到进程内 client。
5. 卸载时释放所有监听、定时器和 observer；`pagehide` 改为卸载。

### 第三步：样式和依赖

1. GUI 保留自己的 Tailwind 输出。`scripts/prepare.mjs` 用 PostCSS 把所有规则限定到 `.multica-agent-gui`，`html`、`body`、`:root` 改写为这个根元素，生成 `src/agent-gui.generated.css`，随懒加载 chunk 加载。这样不必合并两套 token，GUI 和主应用互不影响。
2. overlay 通过 `getPortalContainer()` 进入带主题的容器，不再直接挂到 `body`。主题不能写在 `<html>` 上，主应用的 token 也响应 `data-theme`。
3. React 和 Tiptap 各自保留版本，由各 app 的 dedupe 和 `catalog` 决定；Desktop 渲染构建本来不压缩，不受 Rolldown minifier 问题影响。
4. Web：`next.config.ts` 补 `transpilePackages`；vendor 的图片按 URL 导入，要绕开 `next-image-loader`，规则的 `test` 用函数，否则 Next 会停止处理所有 `.svg`。服务端编译把挂载模块别名为空，GUI 只在客户端 effect 之后渲染。

### 第四步：删除和替换

- 删除：Panel 的 iframe 和握手、`AgentGuiPreviewHost`、`build.mjs` 和 `publish.mjs`。两个 app 改为调用 `prepare.mjs`。
- e2e 用例靠 MessagePort 喂假数据，所以 `rpc-client`、`frame-messaging` 和入口移到 `packages/tutti-agent-gui-host/e2e-harness/`，由 `build-e2e-harness.mjs` 构建到 `apps/web/public/tutti-agent-gui`（不提交）。这只是测试台，两个 app 都不加载。
- 删除：`sync.mjs`、`patches/`、`source-manifest.json`、`desktop-smoke.yml` 的 `--verify`。
- 保留并发布：Tutti 的 `LICENSE`、`NOTICE`，ui-system 的 `NOTICE`，Chart.js 和 D3 的许可证。现在由 `build.mjs` 写入产物，要改到 Desktop 和 Web 的发布产物中。
- 替换：`desktop-smoke.yml` 检查 Desktop 产物含 GUI chunk 和许可证，并用测试台在真实 Electron 的 `file://` 页面验证渲染。
- 新增：`e2e/agent-gui-direct-mount.spec.ts` 在同一 document 中放主应用 CSS 和两个 GUI 实例，验证样式互不影响、实例状态独立、overlay 有样式。

### iframe 去掉后要自行处理的问题

| 问题 | 去掉后 |
| --- | --- |
| 崩溃隔离 | ErrorBoundary，提供重新挂载，不用 `location.reload` |
| 视图状态 | 滚动位置接入 `ScrollRestorationProvider` |
| 字体 | 与主应用合并，避免重复加载 |

按仓库规则，不加运行时开关，一次切换，不保留 iframe 路径。

## 9. 实施阶段

阶段只是工作顺序，所有阶段完成后一起部署。

1. **去掉 iframe：** 第 8 节全部内容。这一阶段不改数据流。
2. **领域模型：** `V2Conversation` 移到 core 并保存设置、队列和历史边界；实现 Manager 和 Store；runtime 改读 Store；统一读取路径；补生命周期调用点。
3. **读取策略：** daemon 增加 `turn_limit`、`open_background_items`、`first_seq`；合并规则、分页窗口、版本号和预拉取；修复第 7 节列出的调用方。

## 10. 验证

| 层 | 内容 |
| --- | --- |
| Go | `turn_limit` 范围校验；窗口外后台 item；不带参数时行为不变；`first_seq` 和 `first_at` 正确 |
| core | 设置和队列；item `lastSeq` 幂等；旧页面晚到；合并不移动 `seq`；连续性判断；重复事件和序号缺口 |
| Store | 重新打开不读快照；离开后拉完这批就停；订阅状态表；epoch 丢弃旧页面；队列单飞；列表重连补差 |
| Manager | LRU 不淘汰使用中的会话和列表；generation 使同设备的 Store 失效，不影响其他设备；登出和过期都清理 |
| GUI | 打开时补足三屏、读到最早一轮后停止、失败不循环、底部锁定和流式时滚动不跳、输入框历史翻页入口 |
| 实测 | 长会话打开和切回 tab 的耗时；反复切 tab 后内存；翻页到数千 item 时的流式性能；多工作区和两台设备 |

`e2e/agent-gui-direct-mount.spec.ts` 在真实浏览器中覆盖：样式互不影响、多实例、主题、overlay、长会话三屏预拉取和翻页、失败不循环。还需要在真实应用中确认：连接真实 daemon 的完整流程、HTML 预览全屏、Web 版首屏加载、Windows。

`snapshot.server_read` 只记录远端调用方，测本机耗时需要补打点。

## 11. 风险

1. **样式冲突的工作量未知：** 导入时的结论是加前缀和 ShadowRoot 都隔离不干净。
2. **全量投影的性能：** 每批事件都全量投影和 hash。去 iframe 后这些计算跑在主应用线程，翻页多了可能卡顿。已 settled 的轮次要缓存投影结果。
3. **最新几轮很大时，首次读取仍然重：** 已确定不拆分单轮，这是接受的取舍。
4. **长时间离开后补差慢：** 不设阈值，大量输出要逐批回放。如果实测影响体验，再引入阈值。
5. **已回答的问题卡片：** `interaction.requested` 不是 final，settled 快照只含 `resolved`，已回答的问题在重读后消失。这是现有行为，新模型下更明显，需要单独决定。
6. **文件 URL：** 现有假设是 daemon 重启后视图重载，新方案不重载，缓存的文件 URL 可能失效，重连时要刷新。

## 12. 相关文档

- [本地对话流 Go 直连与 SQLite WAL 方案](./local-conversation-go-sqlite-plan.md)
- [渲染进程直连守护进程](./local-conversation-renderer-direct.md)
- [设备互联对话流](./device-link-conversations.md)
- [Tutti Agent GUI 导入](./tutti-agent-gui-import.md)
