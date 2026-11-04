# AI 会话状态迁移到 MobX 方案

日期：2026-11-03。状态：实施中，按第 9 节分阶段合入。基于 [本地会话客户端模型与去 iframe 方案](./local-conversation-client-store.md)（#122）。

把 AI 会话模块的状态统一到 MobX：数据是带方法的 `XxxModel`，逻辑在维护它们的 `XxxService`，并替换 Tutti engine。Web 上的 AI 会话页面删除，Tutti GUI 只服务 Desktop 的本地会话，最终由我们自己管理，view 直接读 Model、调 Service。

## 1. 为什么改

增量目前只做到 `V2Conversation`。每批事件之后的链路仍是全量的：

```text
daemon 事件 → V2Conversation（增量）
 ① 投影：遍历所有轮次和 item
 ② ledger：每条消息 JSON.stringify 后 hash
 ③ engine：克隆并排序全部消息，所有 selector 重跑
 ④ GUI：全量转时间线、生成行、重建分组和索引
 ⑤ React 提交
```

问题有三个：

1. **开销和已加载内容总量相关。** 翻页后可加载的内容变多，而且这些计算跑在主线程上。
2. **单轮很长时接近 N²。** 流式中的文本每批都被重新序列化和 hash。
3. **同一份数据存了两份。** Store 一份，engine 一份，中间靠投影转换。

以上来自读代码，耗时占比没有实测。各段分别由哪个阶段消除，见第 9 节。

## 2. 已定决策

| 项目 | 决策 |
| --- | --- |
| 状态方案 | AI 会话模块统一 MobX，不再用自研状态或 React 状态方案 |
| Tutti engine | 替换并最终删除；`vendor/tutti` 可以直接改 |
| Agent GUI | 归我们自己管。最终去掉 runtime、RPC 和 controller 层，view 直接读 Model、调 Service |
| 数据 | 定义为 `XxxModel` 类，同时持有数据和方法 |
| 逻辑 | 放在 `XxxService`，由它维护内部的 Model |
| 状态放哪 | 看 UI 销毁后下次还要不要保留。不需要的（如 UI 开关）放组件；Agent 数据放 Service 和 Model |
| 深层传递 | 用 provider + `useContext` 传 Service 或 Model 实例，不传数据副本 |
| observable | 默认不标注；UI 要监听的字段才标注，只到需要的那一层 |
| 发送和队列 | 状态放客户端逻辑层，用于乐观更新；执行和派发仍在 daemon；未确认的发送存 `sessionStorage` |
| daemon | 有必要就改 |
| 代码位置 | 新包 `packages/conversation-client`，不依赖 vendor |
| Web | 删除 `/conversations` 页面和相关代码，旧链接重定向到 `/chat`。原生 `/chat` 不变 |
| engine 外围模块 | 云端路径删除后用不到的直接删，不做等价迁移 |
| 测试和验证 | 用户负责 e2e 和真实应用验证 |
| 部署 | 每个阶段单独合入、单独部署 |

## 3. 分层

```text
view（observer 组件）
  │ 读 Model，调 Service
  ▼
XxxService
  │ 请求、订阅、重试、生命周期；创建并维护 Model
  ▼
XxxModel
    数据、computed 派生值、action；不做 IO
```

| 层 | 可以做 | 不能做 |
| --- | --- | --- |
| Model | 持有数据，应用事件，计算派生值 | 发请求、订阅、依赖 React |
| Service | 调 Transport，管理订阅和生命周期，更新 Model | 持有视图状态、依赖 React |
| view | 读 Model，调 Service；持有开关、测量、焦点 | 直接发请求，或绕过 Service 改 Model |

Transport 仍是独立的公共模块。滚动位置继续由 `ScrollRestorationProvider` 管理。

### 代码位置

```text
packages/conversation-client/
├─ models/
└─ services/
```

- 依赖 `packages/core` 的协议 schema 和 `mobx`，不依赖 `@tutti-os/*` 和 React。这样 vendor GUI 可以正常声明依赖，模型也不会误引 Tutti 类型。
- `mobx-react-lite` 由 vendor GUI、host 和 Desktop 页面依赖，并加入 `electron.vite.config.ts` 的 `dedupe`，避免出现多份实例。
- `V2Conversation` 从 `packages/core` 移走。`focus.ts`、`timing.ts` 和协议 schema 留在 core。

## 4. Model

| Model | 持有 | 替代 |
| --- | --- | --- |
| `ItemModel` | 单个输出块的状态；`applyEvent()` | `V2Item` |
| `UserMessageModel` | 用户消息 | `V2UserMessage` |
| `InteractionModel` | 交互及其回应 | `V2Interaction`、`interactionResponsesById` |
| `TurnModel` | 轮次状态；派生这一轮的消息列表（含用户消息和交互） | `V2Turn`、投影 |
| `PromptQueueModel` | 队列、`revision`、`itemsRevision`、暂停状态、错误 | `promptQueue`、`desktop-queue-engine` |
| `ConversationModel` | item 映射、轮次、设置、队列、用量、fork 来源、游标；`applyEvents()`、`mergeHistory()` | `V2Conversation`、`sessionMessages`、`sessionEntities` |
| `ConversationListModel` | 工作区的会话摘要、排序、分组、未读 | `ConversationStore` 的列表部分、`attentionReadState` |

#122 中 `V2Conversation` 的规则都要带过来：item 按 `lastSeq` 幂等、合并历史不移动游标、重读快照时的连续性判断、队列 revision 单调。

### 结构规则

- **item 由 `ConversationModel` 持有，不由 Turn 持有。** `open_background_items` 可能指向未加载的轮次，Turn 按 id 引用。
- **消息从 Turn 派生，不是一个 item 对应一条消息。** 有的 item 不产出消息（`ask_user_question`、无附件图片、未知 kind）；子 item 喂给父 background_task；`turnSettling`、reasoning、compaction 依赖轮次状态；用户消息和交互不来自 item。
- **Model 实例保持稳定。** refresh 原地重置 `ConversationModel`，不替换实例，否则 observer 继续盯着旧对象，界面不更新也不报错。Turn、Item 这些内部实例会换新，它们从 `ConversationModel` 的 observable 映射读取，映射变化时读取方会跟着更新。generation 失效时内容已不属于当前数据库，整个 Model 丢弃重建。
- **待确认的发送不放在 `ConversationModel`。** 它会被 refresh、generation 失效和 LRU 影响，新建中的会话也还没有 Model。放在 `PromptService`，见第 6 节。

### observable 规则

```ts
class ItemModel {
  // Not annotated: only logic reads these.
  readonly id: string;
  readonly order: number;
  lastSeq = 0;

  // Annotated: the UI updates with these.
  kind = "";
  status = "in_progress";
  text = "";
  output = "";
  state: Record<string, unknown> = {};

  constructor(/* ... */) {
    makeObservable(this, {
      kind: observable,
      status: observable,
      text: observable,
      output: observable,
      state: observable.ref,
      applyEvent: action,
    });
  }
}
```

- 用 `makeObservable` 显式标注，不用 `makeAutoObservable`。
- 字符串和数字是原始值，不存在深层代理。结构化数据用 `observable.ref`，整体替换。`item.updated` 和 `item.completed` 本来就是整体替换，拆成离散字段容易留下旧值，所以 `state` 保持一个对象，只有流式的 `text`、`output` 单独存。
- 列表用 `observable.shallow`，元素已是 Model 实例。
- computed 读到的字段也算“要监听”，必须标注，否则 computed 返回旧缓存，且不会报错。新增 UI 依赖时同步标注，并在 Model 测试里覆盖。
- **没有被 reaction 观察的 computed，每次读取都会重算。** Service 自己读派生数据时，不能假设有缓存。
- 开启 `enforceActions`，所有修改都走 action。

### 组件规则

- **不要在 `useMemo` 或 effect 依赖里读 Model 字段。** 这些值即使在 observer 里也会过期，lint 查不出来。vendor 有 341 处 `useMemo`，迁移时逐个处理，改用 computed。
- 带自定义比较的组件写成 `memo(observer(fn), cmp)`。
- 不启用 React Compiler，它可能缓存可变对象的读取。vendor 里的 `"use memo"` 指令目前不生效。

### 流式更新的节奏

一个 action 只对应一条 WS 消息。现在的微任务合并和 engine 批处理迁走后，高峰时每条消息都可能触发一次同步渲染。所以 Service 按帧或约 30 ms 缓冲事件，用户操作时立即 flush。窗口隐藏时 rAF 会暂停，要用定时器兜底。

这只限制更新频率。流式长文本的 Markdown 解析仍接近 N²，等打点后再决定是否做增量解析。

### 版本和位置

- **版本：** 在提交层统一生成，内容变化时递增。不绑定 `lastSeq`：用户消息和交互没有它，`turn.completed`、子 item 更新和语言切换也不改变它。engine 删除后不再需要版本。
- **位置：** 定义为 `(turnIndex, rank)`，用户消息 rank 为 0，其余按 item 或交互的首个 seq，再编码成一个正数写入 `sequence`。前插旧轮次不影响已有消息。
- **旧 daemon：** 没有 `first_seq` 时，快照重建的 `order` 是结束事件的 seq。这时按 `first_at` 缺失回退到时间排序，不把它当主排序键。

转换结果含本地化文案，语言切换时整体重算。

## 5. Service

| Service | 职责 | 替代 |
| --- | --- | --- |
| `ConversationService` | 每个工作区 + 设备一个；快照、订阅、补差、翻页、离开时拉完这批；新建会话和临时 id | `ConversationStore`、ledger 的 `aliases` 和 `creations`、`pendingIdentities` |
| `ConversationServiceManager` | 实例管理、内容 LRU、generation 失效、登出清理 | `ConversationStoreManager` |
| `PromptService` | 见第 6 节 | `promptExecution`、`sessionMutations`、`pendingSubmit`、`pendingIntents`、`editRetry`、发送可用性 |
| `AttachmentService` | 上传、取消、重试、复用 | `attachment-upload-tracker`、`local-v2-uploads` |
| `ComposerService` | 模型、推理强度、权限的选项和偏好；草稿 | `composerOptions`、`sessionSettings`、preferences 类、`composer-draft-store` |
| `SideService` | Side 反馈、移动排队项 | `movedQueueRequests`、`use-local-v2-conversation` |

- **生命周期：** 每个工作区 + 设备一组 Service，由 Manager 创建和销毁。LRU 和 refresh 只影响 `ConversationModel`。有未确认发送的会话不能被淘汰。
- **保留在 TanStack Query：** 提及数据、云端智能体、模型提供方、文件夹项目，由 Service 注入或页面读取。
- **计时：** `presentationTiming` 现在挂在 `applyDetail` 上，阶段 3 要重新定义“已展示”的时机。

#122 中 `ConversationStore` 的规则不变：UI 决定什么时候拉，逻辑层负责怎么拉；离开时拉完这批就停；订阅状态表；epoch 丢弃旧页面；`focus` 只由视图发出。

## 6. 发送和队列

`PromptService` 对外是一个门面，内部按提交（含新建会话）、队列操作、取消、编辑重试拆分。它对应约 1.2 万行 reducer，是最复杂的部分。

状态放客户端逻辑层，用于乐观更新；轮次执行和队列派发仍在 daemon，窗口关闭后继续运行，其他设备也能看到结果。

### 状态机

```text
本地待发 → 已入队（revision） → 派发中 → 已送达（消息 request_id）
    │            │                 ├→ 已删除
    │            │                 └→ 失败
    └────────────┴→ 结果未知
```

- 发送或入队时立即更新 Model，界面马上显示。
- 未确认的发送存 `sessionStorage`，由 `PromptService` 读写，key 必须带设备 id（现在没有，本机和 P2P 设备共用一份）。窗口重载后恢复显示并对账。
- 队列显示始终按消息的 `request_id` 去重，避免派发窗口内出现两次。

### 重试

现在回执在交给 provider 之前就写入了。provider 启动失败时，同一个 `request_id` 重试会被当成“已处理”：队列项消失，没有新轮次，也没有报错。这是现有代码的问题，建议单独先修。所以重试按回执状态分三类：

| 回执状态 | 处理 |
| --- | --- |
| 没有回执 | 同一个 `request_id` 重试 |
| 有回执，确定未送达 | 用新 id 重试，或由 daemon 提供专门的重试操作；失败时把该项移出队列 |
| 结果未知 | 不自动重试，交给用户判断 |

### 对账

- ack 的内容写回 Model。`queue.updated` 只更新已有项，比较 `itemsRevision`，同 revision 不能用旧内容覆盖新内容。
- 错误随 payload 整体替换。`dispatching` 是瞬态，由 `turn.started` 或下一次队列更新清除。

### daemon 要补的能力

对账缺的数据只有 daemon 有：

| 能力 | 解决的问题 |
| --- | --- |
| `queue.updated` 带有序的 `request_ids`、`dispatching` 和变更原因（不带正文） | 分清“已派发”和“被另一台设备删除” |
| 按 `request_id` 批量查状态 | 重载后对账；已派发的消息可能超出已加载的轮次 |
| 删除留墓碑 | ack 丢失后重试，不让已删除的消息复活 |
| 比较时排除 `display`，或持久化首次发送的原始 payload | 重载后重建的 `display` 含 `Date.now()`，重提会报 `ErrRequestConflict` |
| 恢复单项时不改整个队列的 `paused` | 恢复一条消息不应暂停另一台设备正在跑的队列 |

旧 daemon（P2P）继续走 `GET /queue` 回退。

未传完的附件在重载后无法恢复，`saved-prompt-queues` 已经删除了内容。迁移时要给出明确的失败状态，让用户重新添加。

## 7. engine 模块去向

行数不含测试。

| 模块 | 行数 | 去向 |
| --- | --- | --- |
| `sessionLifecycle` | 5611 | 轮次进 `TurnModel`；取消、交互回应、设置更新、运行可用性进对应 Service。阶段 4 结束才迁出 |
| `pendingIntents` | 3514 | `PromptService`，按实际用到的意图迁移 |
| `promptQueue` | 3461 | `PromptQueueModel`、`PromptService` |
| `sessionMutations` | 3176 | `PromptService`、`ConversationService` |
| `sessionReconcile` | 1767 | **要迁：** 水合、加载状态和打开未知会话进 `ConversationService` |
| `engineRuntime` | 1641 | 删除，但 `workspaceReconcile` 和重连后重新加载工作区要先迁走 |
| `attentionReadState` | 1507 | **必需：** 它是列表未读的唯一来源，进 `ConversationListModel` |
| `composerOptions` | 1503 | `ComposerService` |
| `editRetry` | 894 | `PromptService` |
| `sessionEntities` | 784 | `ConversationListModel` |
| `sessionMessages` | 777 | `ConversationModel` |
| `sessionSettings` | 547 | `ComposerService` |
| `promptExecution` | 363 | `PromptService` |
| `pendingSubmit` | 327 | `PromptService` |
| `sessionCommands` | 143 | 删除 |
| `tuttiModeActivation` | 2129 | 删除，同时清理 `useAgentGUINodeController` 的挂载 |
| `sessionGoalControl` | 1689 | 确认入口关闭后删除，同时清理 `insertGoalControlRows` |
| `planDecision` | 637 | 删除，同时清理 `useAgentGUISessionPresentation` |

外围的 `sessionReconcileExecutor`、`workspaceEventCoordinator`、`ephemeralConversationProjector` 没有被 host 或 GUI 引用，随 engine 删除。

## 8. 删除 Web 的 AI 会话

已在阶段 0 完成。实施时和评审结论不同的地方：

- **重定向：** Next 配 `/:workspaceSlug/conversations` → `/:workspaceSlug/chat`。`/chat` 读同样的 `?session=` 和 `?agent=`，目标路径不带查询时 Next 会保留原查询，不需要映射。
- **Web 入口：** `app-sidebar.tsx` 直接去掉 `personalNav` 里的条目，分组由 `conversationFlowDevices` 决定；`AgentsPage` 用 `showConversationFlowEntry`，`SearchCommand` 用 `showConversationFlow`，默认都不显示。
- **路径：** 删除 `conversationFlowSession` 和 `conversationFlowWithAgent`，两者都没有调用方了。`conversationFlow`、`conversationFlowDevice`、`WORKSPACE_PAGES.conversationFlow` 保留给 Desktop。
- **`TuttiAgentGuiPanel`：** 只有类型来自 `use-chat-draft-restore`。这个 hook 还被 Chat 和 builder 使用，不能跟着页面删除。
- **`parent-service.ts`：** 移到 Desktop 后改名 `agent-gui-parent-service.ts`，工作区部分为 `createAgentGuiWorkspaceService`，只保留云端智能体、`context.tasks`、`storage.*`、`host.openTask`、`host.openInternalResource`、`host.showMessage`、`host.promptApplied`。
- **runtime：** `projectDetail` 虽然不是纯云端（`replaceMetadata` 和被 LRU 淘汰的会话会走它），但本地传入的 detail 永远为空，所以替换成只投影元数据的 `projectLocalMetadata`。`session.load` 留在协议里，只有测试台的 `localSourceFromClient` 使用。`send()` 和 `session.send` 保留：它们是 engine 必须提供的 `sendInput`，队列接管后没有已知调用方，等阶段 4 迁移时处理。host 其余文件里按 `local` 的分支（`host-api`、`context-mentions`、`composer-drafts`）不在 runtime 内，没有动。
- **e2e：** 只直接访问 Web 路由的五个 spec 已删除。测试台继续输出到 `apps/web/public/tutti-agent-gui`，只有测试需要它，Web 不再构建或发布 GUI。runtime 不再有 `session.send` 路径的队列，所以假后端只实现 `session.send`、快照不声明 `queueOwner` 的 spec 需要改成走 `queue.enqueue`；`invocation-avatars` 的 cloud 用例要删除。

## 9. 实施阶段

每个阶段单独合入、单独部署，结束时应用都可用。

| 阶段 | 内容 | 消除第 1 节的哪段 |
| --- | --- | --- |
| 0. 删除 Web 页面 | 第 8 节 | 无，GUI 只服务本地会话 |
| 1. 打点 | 投影（①②）、engine 分发（③）、React 提交（④⑤，`Profiler` 无法再细分）按 5 秒窗口汇总，并记录 item 数和堆大小，用真实长会话记录基线 | 无 |
| 2a. Model 和 Service | 新包；`ConversationStore`、`V2Conversation` 迁入，Model 实例稳定；派生逻辑从投影移进 Turn；测试假后端移到 `LocalV2Transport` 层 | 无 |
| 2a+. 读取一致性 | 第 13 节：旧快照和换库前的读取不再覆盖 Model，`refresh` 合并 | 无，修复现有问题 |
| 2b. 增量提交 | 第 10 节；merge 保留未变化消息的引用并改为有序插入 | ①、②，③ 的大部分 |
| 3. 转录 | 按轮次的行 computed 和不依赖内容的结构索引；view 直接读 Model；engine 不再持有 assistant 内容，只留 reducer 需要的轮次和用户消息 | ③、④ |
| 3b. 列表 | 列表和未读迁入 `ConversationListModel`；**删除** rail 的分页查询层，本地列表本来就全在内存里 | 列表的全量计算 |
| 4. 写路径 | 第 6 节和 daemon 能力；提交、新建会话、队列、取消、交互、发送可用性一起迁；结束时迁出 `sessionLifecycle` | 数据存两份的主体 |
| 5. 其余领域 | composer → detail，同时删除对应的 runtime 方法、controller hook、RPC 方法和 `useAgentHostApi` | |
| 6. 删除 engine 和翻译层 | 删除 engine、`useEngineSelector`、投影、ledger、`AgentGuiRpcClient`、`AgentGUIRuntime`、runtime | 只有 MobX |
| 7. 收尾 | 按第 2 节的规则拆完剩余 controller，用 provider 替换 view model，删除用不到的目录 | |

**controller 重写的主体在阶段 3 到 5，不在阶段 7**：删除 engine 前，controller 的 69 处 `useEngineSelector`、28 处 `getSnapshot`、18 处 `dispatch` 都要替换。

每到一个阶段结束，用打点结果重新评估下一阶段的节奏。

### 阶段 2a 的范围说明

- 迁入的是内容部分和 `ConversationService`。`ConversationListModel` 和 Service 的 observable 入口留到阶段 3、3b，那时才有 view 读它们。
- 消息列表由 Model 派生：Turn 产出 assistant 行，用户消息和交互各自产出自己的行，`ConversationModel.transcript()` 按原有顺序拼起来。按轮次排序和 `(turnIndex, rank)` 位置留到阶段 2b。
- 派生仍是普通方法，每次调用都重算，和现在的投影开销一样。`computed` 留到阶段 3，那时才有 reaction 观察它。
- 第 4 节的事件缓冲还没做：现在没有 observer 读 Model，流式更新仍走 runtime 已有的合并。阶段 3 接入 view 时一起做。
- `observer` 和 React 19 的配合只验证了 host 里的组件测试（批量事件只渲染一次，且只渲染变化的行）。虚拟列表要到阶段 3 才验证。


## 10. 增量提交通道（阶段 2b）

`message/snapshotReceived` 不适合作为主通道：它只能增改、不能删，按字段合并会留下旧值（reasoning 结束时不带 `status`，“思考中”就不会结束），也不更新轮次状态。

- **首次读取、快照、历史页、generation 变化：** 继续用 `session/historyAuthoritativeSnapshotReceived`。
- **流式更新：** 在 engine 新增一个原子 intent，包含 session、upserted turns、upserted messages、removed ids 和 window，消息整体替换，一次分发只通知一次。

结算围栏和未读回放（`applyDetail` 里的 `replayAcceptedLiveCompletion`）要在新 intent 中保留。

这个通道只存在于阶段 2b 到 6，engine 删除时一起移除。

## 11. 风险

1. **engine 里的隐含行为。** reducer 中有乐观状态、未确认发送和取消竞态。迁移时把 engine 的 31 个测试文件和 GUI 的 34 个相关 spec 当行为规格，逐个迁移或说明删除理由。
2. **阶段之间的两套数据源。** 阶段 3 之后界面读 Model，部分判断仍读 engine，比如 `selectEngineSubmitWouldBeVisibleInQueue`。engine 比 Model 晚一拍时，用户在已结束的轮次上发送会被判定为排队。阶段 3 要让 engine 需要的轮次状态在同一次提交里更新。不做兼容形状：从 Model 拼出 engine 的不可变 state，等于重建全量投影。
3. **写路径的回归保护。** 旧测试台有 35 个用例失败，发送和队列的用例要在阶段 4 前修好，这部分由负责测试的人安排。
4. **最被低估的部分。** 转录的行投影和 `AgentConversationVM` 这层契约（`shared/agentConversation` 约 1.9 万行）、Side 专用的 runtime 和 controller、`useAgentHostApi` 的第三层转译。
5. **内存。** LRU 按会话数（100 个）计算，对 item 数没有约束，长会话可能有几千个 `ItemModel`。阶段 1 记录实际数据；如果有问题，已结束的轮次改用轻量对象，等有后台事件时再升级。
6. **observer 覆盖不全。** 组件漏了 `observer` 不会报错，只是不更新。用约定和 lint 把关，迁移时逐个组件核对。
7. **MobX 和虚拟列表的配合尚未验证。** React 19 下的 `observer` 已在阶段 2a 的组件测试中验证；虚拟列表要到阶段 3 接入转录时才验证。

## 12. `AGENTS.md` 规则

替换 #122 中关于 `ConversationStore` 的那条：

> AI 会话模块（`packages/conversation-client/`）统一使用 MobX。数据定义为 `XxxModel` 类，同时持有数据和领域方法，不做 IO；逻辑放在对应的 `XxxService`，由它维护 Model。view 用 `observer` 读取 Model，通过 Service 操作；层级很深时用 provider + `useContext` 传实例。状态放哪看 UI 销毁后要不要保留：不需要的放组件，Agent 数据放 Service 和 Model。字段默认不是 observable，UI 要监听的才用 `makeObservable` 标注；不在 `useMemo` 或 effect 依赖里读 Model 字段。其他模块继续使用 TanStack Query 和 Zustand。

## 13. 读取一致性（阶段 2a+）

单独一个 PR，放在 2a 之后、2b 之前。这些问题在迁移前就存在。只改客户端，不改 daemon 和协议。

### 问题

事件、item、队列和摘要都有序号或 revision 保护，只有快照写入没有比较，会整体替换 Model。

**同一数据库内：旧快照盖掉新事件（已用临时测试复现）。** 快照走 HTTP，事件走 WebSocket，到达顺序没有保证：

```text
daemon 生成快照（seq=5）
WS 先送到 seq=6 的 turn.completed → seq=6，completed
HTTP 后送到 seq=5 的快照        → seq=5，running
```

之后不会重新订阅。后面还有事件时，gap 会把内容补回来；丢的是最后一条事件时，会话一直显示运行中，直到切走再回来。

**换库之后：旧读取没有被作废（只读了代码，未复现）。** 新旧数据库的 `seq` 不可比。`invalidate()` 之后，还在途的旧读取有三处漏洞：

| 位置 | 问题 | 后果 |
| --- | --- | --- |
| `read()` 的 epoch 检查 | 条件要求 entry 同时不存在，entry 还在时检查不生效 | 旧数据写进 Model |
| `snapshot()` | 检查之前就上报 generation 和摘要 | Manager 以为又换了一次库，再次失效整台设备；旧摘要写回刚清空的列表 |
| `read()` 的 `finally` | 无条件清空 `entry.loading` | 清掉新读取的标记，可能多发一次读取 |

**并发 `refresh`：可能读不到自己刚造成的变化。** 读取进行中再调用 `refresh` 时会共用那次读取，而它可能在调用方的写操作之前就已被 daemon 应答。未订阅时，这个旧状态会一直留到下次读取。

### 修复

**同库比较序号：** `snapshot_seq` 取自 `conv.LastSeq`，和轮次、待回答问题在同一个只读事务内读出，可以直接比较。`queue` 是事务结束后单独读的，可能更新，所以丢弃快照时队列仍要按 `revision` 接收。

```ts
// ConversationModel.replaceSnapshot
if (sameStore && snapshot.snapshot_seq < this.seq) {
  this.queue.receive(snapshot.queue);
  return;
}
```

序号相等时内容相同，正常替换。

**换库作废旧读取：**

1. `read()` 只要 `epoch` 不一致就丢弃结果，调用方改为等待当前那次读取。
2. `snapshot()` 发请求前记下失效计数，返回时计数变了就不上报 generation 和摘要。直接调用 `snapshot()` 的路径也一并覆盖。
3. `finally` 只在 `entry.loading` 还是自己时才清空；被作废的读取失败时不写 `entry.error`。

**`refresh` 合并：** 同时最多一次读取，后面最多排一次补读。

```text
没有读取在进行 → 立即读
有读取在进行   → 标记“读完再读一次”，返回那次补读的 promise
再来 N 次调用  → 共用同一次补读
```

`ensure`、`open` 不要求最新，继续共用当前读取。`loadFreshSession` 和 `dirtySessions` 这层绕路等阶段 4 写路径迁入 `PromptService` 时再删。

### 测试

都放在 `packages/conversation-client`，先写失败的回归测试：

- 迟到的同库旧快照不改变 `seq` 和轮次状态，更新的队列仍被接收。
- 换库前发出的读取无论先回还是后回，都不写入 Model，也不再次触发失效。
- 读取进行中调用 `refresh`，返回的结果来自调用之后才发出的读取；多次调用只多读一次。
- `runtime.test.ts` 里“等上一次读完”的等待可以去掉。
