# 本机运行器与官方智能体的对话流

## 范围与根因

桌面“对话流”的主选择器原先从工作区智能体生成，创建、模型查询、发送都依赖真实 `agent_id`，因此选择器实际执行的是 Multica 智能体。仅改变名称或图标不能满足“普通本机 Codex / Claude Code”的语义。

本次将主执行目标与引用目录分开；保留原两栏布局、Tutti 对话组件和 Multica 桌面导航。原“聊天”仍承载官方聊天历史。用户明确选择普通会话历史先保存在本机。

## 请求与状态流

```text
普通消息 → GUI runtime → 桌面 parent service → preload IPC
         → 主进程本机会话存储 → 本机 CLI → 结构化事件
         → IPC 失效通知 → GUI 重新读取消息与执行事件

@ 已定义智能体 → 本机会话保存用户消息与独立执行记录
             → 官方 createChatSession / sendChatMessage（每个目标独立）
             → 官方消息与任务事件 → 各自结果保存并显示在本机会话
```

| 概念 | 当前实现 |
| --- | --- |
| 主执行目标 | `AgentGuiExecutionTarget`，桌面只提供 `local:codex`、`local:claude` |
| @ 智能体目录 | 官方工作区 `Agent`，继续遵循权限、归档和运行时绑定检查 |
| 普通历史 | Electron userData 下 `local-conversations/<scope hash>/index.json`（摘要）和 `details/<内容哈希>.json`（详情） |
| 本机执行记录 | 中性 `LocalConversationTurn`，只在 GUI 适配边界投影为已有 DTO |
| 云端 @ 执行 | 每个本机会话、每个智能体单独关联官方会话，后续引用复用关联 |
| 官方聊天历史 | 保留在原“聊天”；旧对话流云端标签页回到本机新会话首页 |

作用域哈希包括 API 地址、账号和工作区，避免不同环境、账号或工作区混读。文件采用原子替换写入，权限为目录 0700、文件 0600；切换账号会停止旧账号的本机执行。退出时等待本机执行结束并保存。意外中断的本机执行在重开时标为中断，不自动重复发送。

GUI 请求切换会话时，先记录选择顺序和宿主当前导航意图，再异步读取历史。读取完成后，仅最新请求且宿主意图未改变时才能导航；旧读取可以完成，但不能把用户刚选中的会话切回去。页面通过现有会话、运行器、账号和工作区组成的意图键识别外部导航，不改会话协议或历史内容。

本机会话列表保留置顶优先级，组内按最后一轮的 `createdAt` 倒序排列；无轮次时使用会话创建时间。同一时刻先按会话创建时间倒序，再按 ID 升序确定顺序。排序在主进程裁剪列表历史之前完成，使用已保存的轮次开始时间，重启后规则一致。流式输出、工具事件、执行结束、标题和归档操作不改变排序依据；排队消息仅在出队并建立轮次时更新排序。导入和分叉使用保留历史中的最后一轮开始时间，Side 尚无轮次时使用自身创建时间。

### 按项目点击加载会话

每个本地文件夹项目及“未分配项目”分别通过 `listPage` 拉取摘要：`folderProjectId`、`archived`、`search` 和可选 `cursor`。每批固定 8 条，返回 `sessions` 和可空的 `nextCursor`。只有点击“加载更多”才请求下一批；滚动不请求。重复点击共享同一次请求，失败保留已加载条目并可再次点击。搜索与归档在宿主摘要索引中筛选，能查到尚未加载的会话。

续页游标绑定账号、工作区及筛选条件，使用创建游标时的排序快照；会话置顶或新轮次不会使当前续页重复或漏项。游标闲置 30 分钟后失效；点击过期游标会刷新已加载批次，再次点击可继续。实时事件只失效 Query 缓存，刷新最多重取用户已加载的批数。切换搜索、归档或项目显示偏好使用独立缓存键。

列表及 GUI 初始化不再调用全量 `list`。GUI 引用目录复用已加载摘要，`runtimeState` 单独返回全局运行中轮次和 Side 队列回执；目录继承使用 `directorySource`。深链、分叉来源、已打开的 Side 与选中会话按 ID 读取，不要求它们位于首批中。显式上下文工具的 `list` 仍提供摘要目录，不读取详情或附件。

### 存储迁移与恢复

宿主启动只读 `index.json` 的摘要、排序字段、详情版本引用和本地目录清单。`read` 及对特定会话的操作才打开该会话的详情；附件、图片和预览仍使用独立文件接口。重启时本地运行状态在摘要中标为中断；详情在打开时恢复并暂停待发送队列，不自动执行。已确认的运行中云端轮次需要恢复轮询，因此会读取对应详情。

旧作用域第一次打开时，允许一次完整读取 `conversations.json`：校验旧数据，写入内容寻址的详情文件，最后原子替换新索引。失败不会启用半成品索引，原文件保留，可解除磁盘或权限故障后重试。后续会话保存先写详情新版本，索引 rename 一次提交包括队列转 Side 在内的全部更新，成功后删除替换掉的旧版本。Stable/Canary 的旧目录合并也识别新索引，只导入缺失或时间更新的记录。

升级前可备份整个作用域目录。恢复时应在宿主停止后恢复 `index.json`、`details/` 和 `files/` 的同一份备份。保留的 `conversations.json` 是升级时的恢复副本，不包含升级后的新消息；旧版应用不能读取新格式，不应将这个副本视为最新历史。

回归见 `local-conversation-pagination.test.ts`、`legacy-migration.test.ts`、`local-conversation-flow.test.tsx` 和 `local-conversation-client.test.ts`，覆盖分页边界、稳定续页、按需读取、迁移失败重试、跨渠道合并、连续点击、搜索、刷新和异常响应。

## 代码边界

本机消息队列由 Electron `LocalConversationService` 保存和调度，和会话历史一起原子落盘。GUI 只提交队列操作并展示主进程快照；桌面切换标签页仍卸载旧视图，但不会停止队列。前一轮正常完成后，主进程自动取下一条，保留附件、引用、模型、思考强度、权限及稳定提交编号。引用智能体的排队执行和结果同步也在主进程内完成，凭据不经过队列或界面。

手动停止先保存暂停状态，再取消执行；执行失败保留后续消息并暂停。应用重启恢复未发送消息时保持暂停，避免无提示重放。旧版浏览器保存的队列只在确认主进程已接收后清除；发送结果不明的旧记录不自动重发。队列转为 Side 时，来源移除和 Side 队列使用同一份会话文件保存，Side 执行不依赖面板保持打开。

点击队列“引导”立即提交 `sendNow` 操作；若消息尚未确认入队，先等待原编号的入队回执。GUI 通过已有的 `inFlight` 与 `sendNextPromptId` 展示处理中，并合并同一消息的重复引导，直到宿主报告开始派发、消息移出、失败或用户停止。操作回执仅表示引导已接收，旧执行仍可能正在退出。宿主负责暂停队列、取消旧执行，再恢复队列；只有旧运行器释放后才自动发送选中的消息，不需要再次点击。已经派发或已建立轮次的同编号重复请求返回当前队列，不取消该轮。失败保留消息供显式重试，GUI 卸载不影响宿主续发。

队列回归：`pnpm --filter @multica/desktop exec vitest run src/local-host/local-conversation-queue.test.ts src/renderer/src/platform/local-conversation-service.test.ts`，以及 `pnpm --filter @multica/tutti-agent-gui-host exec vitest run src/runtime/desktop-queue-engine.test.ts src/runtime/runtime.test.ts`。测试使用注入的假运行器和 HTTP 响应，不调用真实模型。

当前连接不支持把消息注入正在运行的回合。Cmd/Ctrl+Enter 引导提交会在清空草稿、接收排队消息前拒绝，并提示使用普通发送或先回答当前问题；不把它静默改为中断执行。普通发送中显式 `@智能体` 的消息进入队列，不作为当前问题的回答；不带智能体提及的纯文本继续沿用问答回复路径。队列“直接发送”仍由宿主中断旧执行后启动新一轮。宿主通过受保护的构建适配接入统一提交校验，vendored Tutti 快照保持不变。快捷键和问答回归见 `packages/tutti-agent-gui-host/src/guidance-submission.test.tsx`。

未发送的文字草稿由 GUI 宿主的 Zustand store 管理，每次编辑同步投影到窗口的 `sessionStorage`，新 GUI 文档在首次渲染前恢复。作用域包括 API 地址、账号、工作区、主对话流或对应 Side 面板，再沿用 GUI 的会话和首页草稿键；不同会话互不覆盖，首页切换运行器仍共用原有首页草稿。清空和已有的发送结算回调通过同一个 setter 更新存储，切换 tab 本身不会提交内容。构建适配只替换 GUI 的草稿状态所有者，vendored 源码快照保持不变。

桌面确认接收前，提交内容和原提交编号同步保存在同作用域的窗口会话状态中。GUI 文档重建时只恢复为可操作的排队条目，不自动重发；用户显式重试沿用原编号，主进程按编号去重。收到接收回执或包含该编号的主进程队列快照后清除检查点；明确删除成功也清除，拒收和结果不明时继续保留。

每个本机会话最多排队 100 条消息。GUI 在清空草稿之前同步计入主进程快照中的队列项（包括正在派发但尚未移出的项）及未确认提交，满额时提示用户等待或移除消息，保留原输入，不创建第 101 条恢复项；恢复内容计入容量，其他会话独立计数。主进程继续在串行入队时执行同一上限，约束其他窗口并发提交。GUI 快照可能滞后，因此提交后的拒收或结果不明仍沿用原检查点处理；本限制不改变这些条目的删除契约。

该机制只保存文字及换行，不保存附件上传状态，不保证应用退出后恢复，也不改会话历史文件或消息队列协议。回归见宿主的 `composer-draft-store.test.ts`、`composer-drafts.test.tsx`，以及 `e2e/local-conversation-drafts.spec.ts`；浏览器用例实际移除并重建 iframe，覆盖会话隔离、多次往返、清空和发送结算。

模型菜单的“思考强度”使用本机能力目录：Codex 读取 `models_cache.json` 中每个模型的 `supported_reasoning_levels`；Claude 通过无提示词的 SDK 控制连接读取 `supportedModels()`，不提交模型请求。Claude 目录缓存一分钟，“刷新模型”跳过缓存；目录获取失败时显示重试，不猜测模型支持的档位。原生默认模型未解析时，先显式选择模型再调整强度。

强度选择按运行器草稿和会话分别持久化，并绑定所选模型；切换为不匹配的模型时不沿用旧值。发送前再次核对能力目录。请求和队列保存可空的 `reasoningEffort`，分叉与旁支继承其来源设置。Codex 使用 app-server `turn/start.effort`，一次性 CLI 路径使用 `model_reasoning_effort`；Claude 初次启动使用 SDK `effort`，保留后台任务的会话通过 `applyFlagSettings({ effortLevel })` 更新，选择“默认”时用原生 API 清除覆盖。未选择强度时启动参数不覆盖原生设置，标题生成和被 @ 的智能体不继承当前会话强度。不修改全局配置；旧会话缺失字段按默认处理，无数据库迁移。

本机 CLI 的 `status` 事件在 GUI 协议中单独保留。重连提示映射到已有的 `transport_retry` 系统通知，原始错误放在详情中；其他状态映射为普通系统通知。这些事件不拼入助手正文，也不结束运行。运行结束后不再显示临时重连提示，原始事件仍保留在执行记录中。重试次数、传输方式和最终成功/失败仍由原生 CLI 决定，界面不自动重发用户请求。

### 后台子智能体的子对话（Go V2）

Claude 后台任务卡片可展开子对话，依次展示任务指令、公开回复、工具输入/输出、图片和错误。工具调用单独展开，执行中的结果原位更新；主会话继续对话不改变子任务归属。已有历史未采集的详情显示未记录，不补造内容。后台 shell 保留原来的输出详情。

Go Claude normalizer 为每个 `parent_tool_use_id` 使用独立解析器，将子任务 item 的 ID 隔离，并保存 `kind=subagent`、公开的 `item_kind`、首次出现的 `item_order` 及父工具调用。子任务的 session、result 和私有 reasoning 不进入主会话。GUI 按 task 的 `tool_use_id` 关联这些 item；旧客户端跳过未知的 `subagent` 类型。事件继续使用现有 V2 SQLite 存储，无表结构迁移。

主轮次结束但后台任务仍在运行时，快照保留该轮未结束的 item；后台任务终止或 daemon 重启时结算子项，保留已收到的内容。最终事件保存首次顺序，刷新后的子对话顺序与实时展示一致。覆盖假 SDK 并发隔离、主轮次续聊、失败/停止、重启及快照回放的回归分别位于 `provider/claude/subagent_test.go`、`service/subagent_test.go` 和 GUI host 的 `v2-subagent.test.ts`、`subagent-transcript.test.tsx`。

### 上下文压缩分割线（Go V2）

本机 Codex 和 Claude 的压缩生命周期统一保存为 V2 `compaction` item。`item.started` 显示压缩中的分割线；同一 item 的 `item.completed` 将其替换为完成、失败或取消分割线。压缩 item 不结束当前轮次，也不生成助手正文。GUI 复用已有 `noticeCommand=compact` 展示，在压缩期间用专用进度替代普通处理提示，完成后停止该节点的计时。

| 运行器 | 开始信号 | 完成信号 | 失败与取消 |
| --- | --- | --- | --- |
| Codex app-server | `item/started`，类型为 `contextCompaction` | 同 ID 的 `item/completed` | 已开始但未完成的 item 随轮次终态结算 |
| Claude Agent SDK | `system/status`，`status=compacting` | `system/compact_boundary` | `compact_result=failed` 保存错误；未完成的 item 随轮次终态结算 |

Codex 使用原生 item ID；Claude 将开始与边界信号关联到同一 ID，并按消息 UUID 去重。只收到完成信号时仍记录完成分割线，不补造开始时间。Claude 的普通 status 重置不代表压缩成功。只处理主会话的压缩信号，子智能体的信号不放入主会话流；不根据上下文占用百分比或等待时间推断压缩。

事件继续使用现有 SQLite 存储和 V2 快照回放，无表结构迁移。刷新后保留 item 的首次事件时间，重复事件不增加分割线；旧历史不补造压缩节点。新类型是 V2 的增量扩展，旧客户端按既有规则跳过不认识的 item。

实现入口：`server/internal/localconversation/provider/{codex,claude}/`、`server/internal/localconversation/protocol/schema/local-v2.schema.json` 和 `packages/tutti-agent-gui-host/src/runtime/v2-projection.ts`。原生信号依据为 [Codex app-server 协议](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)及锁定版本 Claude SDK 的 `SDKStatusMessage` / `SDKCompactBoundaryMessage` 类型。

回归使用假运行器，覆盖两种 provider 的解析、开始与终态、失败/取消、自动压缩不结算轮次、持久化、刷新及去重：

```sh
cd server
go test ./internal/localconversation/provider/codex ./internal/localconversation/provider/claude ./internal/localconversation/protocol ./internal/localconversation/service
cd ..
pnpm --filter @multica/tutti-agent-gui-host exec vitest run src/runtime/v2-compaction.test.ts src/runtime/v2-conversation.test.ts
```

Codex 的独立助手消息聚合到同一轮正文时，补齐上一条消息末尾的两个换行以保留 Markdown 段落边界；消息内部的增量、列表和代码块原样保留。一次性 CLI 与普通会话的 app-server 路径使用同一分隔规则，实时 text 事件拼接结果与最终保存正文一致。空消息不新增分隔，重复完成事件不重复输出。该规则只影响新执行，不改写已保存的历史，也不改变 IPC 或持久化格式。回归见 `local-provider-runner.test.ts`、`local-codex-stream.test.ts` 和 `local-conversation-service.test.ts`，覆盖流式输出、完成与重开会话。

- `packages/tutti-agent-gui-host/src/protocol.ts`：执行目标与云端智能体分离，未改 vendored Tutti 源码。
- `packages/core/local-conversation/protocol.ts`：仅桌面的本机会话 IPC 协议。
- `apps/desktop/src/local-host/local-provider-runner.ts`：发现已安装 CLI、解析一次性执行输出；继续服务标题生成等调用。
- `apps/desktop/src/local-host/local-codex-pool.ts`：仅供对话流 Codex 普通轮次使用的双 app-server 实例池。
- `apps/desktop/src/local-host/local-provider-fork.ts`：创建独立的原生会话分支，不向模型提交消息。
- `apps/desktop/src/local-host/local-conversation-service.ts`：本机持久化、附件、工作目录、执行生命周期。
- `apps/desktop/src/renderer/src/platform/local-conversation-service.ts`：本机协议和 GUI DTO 适配，官方 @ 请求与结果同步。
- `apps/desktop/src/renderer/src/pages/local-conversation-flow.tsx`：本机会话列表、归档、置顶和工作目录入口。
- `packages/views/tutti-agent-gui/tutti-agent-gui-panel.tsx`：注入桌面服务；网页默认服务保留。

本机会话列表与内容区使用 Multica 现有 `ResizablePanelGroup` 分栏。列表默认宽度 320px，限制在 240–480px，对话区至少占 40%；拖动结束后使用 `useDefaultLayout` 保存布局，键名为 `react-resizable-panels:multica_local_conversation_layout`。分隔条可通过左右方向键调整。窄窗口停用分隔条并保持单栏切换，两侧仍挂载，避免重建对话或丢失未发送草稿；单栏布局不覆盖宽度偏好。

分栏回归使用实际共享组件，验证指针拖拽、两侧边界、重挂载恢复，以及窄窗口切换时的草稿保留。桌面实窗已核对分隔条和键盘调宽；实窗自动化的鼠标拖动未产生可靠移动结果，不计入通过项。

普通发送不调用云端创建会话或发送消息。CLI 继承本机已配置的账号、模型和权限策略，没有新增跳过审批的参数。Codex 仅从模型缓存读取模型目录；不读取或导出登录凭据。没有修改官方 Server 协议或部署官方后端。

带图用户消息的复制按钮将整条消息的文字和有序图片引用写入文本剪贴板。图片使用 `multica-image://attachment/<workspaceId>/<attachmentId>` 地址，查询参数保留来源会话、名称和 MIME；复制时不读取图片字节。粘贴到同一工作区的 Multica 输入框后，先提交文字，再创建图片草稿并通过现有附件读取接口加载预览，发送复用原附件 ID。跨会话复用不重新上传图片；跨工作区或失效引用保留在失败草稿中并提示错误，移除失败图片前禁止发送。外部应用收到引用文本。纯文字复制行为不变，旧消息和会话存储格式不变。回归见宿主 `message-image-clipboard.test.ts` 和 `e2e/message-image-copy.spec.ts`。

## 在对话流复用 Codex app-server

仅 `LocalConversationService` 的普通 Codex 轮次接入双实例池，包括主对话、Side 和分叉后的普通消息。任务执行、官方聊天、引用已定义智能体的云端执行、标题生成、历史读取与原生分叉继续使用各自原有路径；Claude 执行路径不变。

第一次发送时启动两个本机 `codex app-server --listen stdio://`，分别完成初始化。备用实例的握手与当前实例并行，不提交模型消息，也不要求当前轮次等待备用实例。一个实例通过不同 thread 承载多个会话；同一原生 thread 同时只能有一轮执行。RPC 响应用请求 ID 匹配，输出按 thread ID 与 `turn/start` 返回的 turn ID 路由，避免续接历史事件进入新轮次。停止单轮使用 `turn/interrupt` 并等待终态，不用结束共享进程代替正常取消。

每次接收轮次时读取该工作目录的有效原生配置。配置、账号/工作区、运行环境或本轮 MCP 工具集合改变时，空闲实例先退出，再加载新配置并接收新轮次；旧实例继续处理已经接收的轮次。两个实例都忙时等待空位，不创建第三个实例，不中断旧任务。等待期间再次改变的原生配置在空位释放后重新读取，合并到最新版本。旧实例排空后重建为当前版本的备用实例。

每轮仍创建独立的 Multica MCP 和临时 CLI 配置。`local-codex-context-bridge.ts` 用独立随机路由将请求转发到所属轮次的 MCP，并在主进程内附加该轮凭据；结束时撤销路由。共享子进程只继承桥接令牌，不继承某轮 `MULTICA_*` 环境。原生 thread 配置只包含桥接地址与令牌环境变量名，不保存每轮 MCP 令牌。CLI 路径和工作区等非秘密环境覆盖限定在 thread 配置中。已加载 thread 可能保留旧 MCP 快照，因此轮次结束后取消订阅，并在确认 `thread/closed` 后允许下一轮恢复同一个原生 ID。

`auto` 映射到 `on-request`、`auto_review` 和 `workspace-write`；`full-access` 映射到 `never` 与 `danger-full-access`；默认模式沿用原生策略。保留既有的原生插件和外部 MCP 隔离规则。宿主不将澄清问题当成命令审批；原生返回需要宿主处理、但当前 GUI 不支持的交互请求时，明确拒绝并保存状态提示。

退出应用或切换账号时关闭池和桥接服务。进程崩溃、协议超时或中断无法确认时，受影响的轮次失败，不自动重发可能已经产生副作用的消息；下一次明确发送可以新建实例恢复。共享进程故障会影响该进程内的其他轮次，两个实例不是模型服务的限流或容灾替代品。

验证命令（全部使用测试创建的假 Codex，不调用真实模型）：

```sh
pnpm --filter @multica/desktop exec vitest run src/local-host/local-codex-pool.test.ts src/local-host/local-conversation-service.test.ts src/local-host/local-conversation-queue.test.ts src/local-host/local-provider-runner.test.ts
pnpm --filter @multica/desktop typecheck
pnpm --filter @multica/desktop exec eslint src/main/local-codex-*.ts src/local-host/local-conversation-service.ts src/local-host/local-conversation-service.test.ts
git diff --check
```

上述测试验证进程复用、跨会话并发、单轮取消、MCP 凭据隔离、连续配置更新时最多两个存活进程、进程退出后不重放，以及账号切换与关闭。没有将假进程的耗时视为真实模型首响改善的证据；本机真实模型与 Electron 实窗验收需要另行执行。

## Claude 前台回复与后台任务

Claude 普通会话通过官方 Agent SDK 的双向输入连接本机已安装的 Claude Code，继续使用该 CLI 的登录、用户/项目配置和原生会话。标题推断保持一次性执行路径。每个本机会话同一时刻只有一个 Claude 进程，后台监控存在时，下一条普通消息交给这个进程，不另起进程并发续接同一个原生 ID。

流式 `message_delta.stop_reason=end_turn` 或完整 assistant 的 `stop_reason=end_turn` 表示前台回复边界。后台任务存在时，保存回复并结束前台 turn，保留进程接收后台通知和后续输入。普通结果仍按 `result` 收尾。后台任务以 `background_tasks_changed` 的完整快照校准，`task_notification` 保存完成、失败、停止结果；过期的 task-start 事件不会复活已结束任务。延迟的旧轮次 result 通过用户消息 UUID 关联，不能结束新的前台 turn。

`task_started.is_backgrounded=false` 的普通前台命令只保留工具调用，不生成后台卡片或延长进程生命周期。`task_updated.patch.is_backgrounded` 将命令转为后台时才发布卡片。`task_progress` 的摘要、最近工具、耗时、工具次数及 token 数保存到原任务所属 turn，通过既有失效通知原位刷新。展示的数值来自 CLI，不推算完成百分比；运行期间不额外调用模型生成进度摘要。

数据流为 `CLI task_* → LocalClaudeConversation → observer.tasks → turn.backgroundTasks → IPC invalidate → projectDetail → 后台卡片`。卡片直接显示已有进度和计数，详情关联 `tool_use_id` 对应的命令与输出。旧卡片只在同一 turn 内找到唯一、描述完全一致的工具调用时补充历史输出，存在歧义时不关联。后台 shell 通过原生 `backgroundTaskId`、工具调用和会话内任务路径关联输出文件，运行期间每秒读取一次，只在输出变化时更新卡片；结束或应用关闭后停止读取。最多保存末尾 32 KiB，标明截断；文件不存在不影响原任务的完成状态。子智能体使用 CLI 提供的摘要及进度，不把其原始 JSONL 会话当成终端输出。

`LocalConversationTurn.backgroundTasks` 保存独立任务状态；完成的前台 turn 可以带仍在运行的后台任务。后台任务创建时记录原生任务 ID、来源 turn 和创建时间，立即出现在该轮对话流；同一条记录随完成、失败或停止原位更新，保留说明和结果，不伪造助手回复或重新激活前台 turn。会话行显示静态后台图标及任务说明，前台回复才显示转圈。菜单通过独立停止接口停止后台任务，不取消新的前台回复。后台完成后的文字回复仍回到原 turn。应用关闭或账号切换时终止对应进程并清理临时上下文；重开只把遗留后台任务标为停止，不自动重跑命令。用户手动暂停的队列保持暂停，不自动执行其中的消息。

后台结束后，任务另存 `delivery.status`：`pending` 等待交付、`requested` 已保留一次内部接续、`delivered` 已保存回复、`interrupted` 接续中断或未执行。`completionSource` 记录通知、更新、快照或用户停止；仅从快照消失的任务虽然不再显示为运行中，其结果仍未确认，不能据此报告成功。旧记录没有这些可选字段时不补发、不重跑。

主进程给原生续答 500 毫秒的优先窗口；已经开始的原生回复一直优先到回复边界，不受该窗口限制。原生回复按来源用户消息 UUID 和待交付任务关联，文本及交付回执一起保存到原 turn。SDK 只在首个流事件和首个完整助手块携带来源 UUID，后续无 UUID 的块沿用已确认的回复归属，直到 `end_turn` 或 `result` 清理绑定；显式来源切换重新绑定。尚未确认归属的多来源帧和不认识的 UUID 不猜归属。只有迟到的 `result`、没有新的助手帧时，不把旧前台结果再次作为后台回复。后台交付未完成前保留连接。

缺少原生续答时，会话服务在没有前台执行、队列派发或分叉的条件下，先保存 `requested` 再串行提交内部接续。该轮通过 `backgroundDelivery.sourceTurnId/taskIds` 关联原任务，有独立运行记录及回复，不伪造用户消息。它复用普通执行链路，刷新当前会话的 MCP 上下文、模型和权限，要求助手主动汇总结果和待决策项，只继续用户已经授权的工作；任务数据不作为新指令，不重跑原命令，也不绕过原有审批。重复通知、迟到结果或过期快照不再启动第二次接续。

暂停、归档、用户明确停止后台任务或连接意外关闭时，直接保存已有状态和必要的继续提示，交付标为 `interrupted`，不发起模型执行。自动接续失败也报告已有结果及失败原因，不自动重试。账号切换和退出清理待启动计时器；重开把遗留 `pending/requested` 标为 `interrupted`，保留已有回复，不自动补发。整个交付由主进程持有，切换当前查看的会话不会改变结果归属。

每条后续消息在提交前刷新作用域 MCP 配置、模型和显式权限；临时上下文由对应 Claude 进程持有并在关闭时清理，避免后台任务访问已删除的查询配置。不同账号、工作区及分叉会话不共享进程。没有修改用户的全局 Claude/MCP 配置。

验证使用临时目录中的假 CLI，通过真实 SDK 的初始化与双向协议完成两轮消息和后台通知，不调用用户安装的模型服务。组件回归覆盖后台图标和停止菜单；服务回归覆盖落盘、续聊、回传及重启恢复。主进程修改需重启桌面应用才能启用；旧的只读 stdin 进程不能原地升级为双向会话。

```sh
pnpm --filter @multica/desktop test src/local-host/local-claude-conversation.test.ts src/local-host/local-conversation-service.test.ts src/local-host/local-provider-runner.test.ts src/renderer/src/platform/local-conversation-service.test.ts src/renderer/src/pages/local-conversation-flow.test.tsx
pnpm --filter @multica/desktop typecheck
```

对话流后台记录的补充验证：

```sh
pnpm --filter @multica/tutti-agent-gui-host test src/runtime/runtime.test.ts
pnpm --filter @multica/tutti-agent-gui-host typecheck
PLAYWRIGHT_BASE_URL=http://localhost:16099 pnpm exec playwright test e2e/local-background-task.spec.ts --project=chromium
pnpm --filter @multica/desktop build
```

相关桌面与宿主回归合计 150 项通过，两条隔离界面用例验证深浅主题下创建记录、原位更新和后台监控时继续发送；未调用真实模型。重启 Cloud 桌面后核对原对话恢复、旧版监控进程退出、前台运行状态结束，“帮我合入”仍留在暂停队列中。旧版执行没有保存结构化后台记录，不从自然语言回复反推或自动重跑监控。

## 会话分叉

会话右上角的“分叉”通过桌面 IPC `fork({ id })` 创建新本机会话，并立即切换到新分支。仅支持已经保存原生会话 ID 的历史；空会话、正在执行的会话不能分叉。原会话在分叉完成前禁止发送，保证界面历史与底层上下文来自同一个起点。

Codex 使用无提示词的 `codex exec fork <native-session-id> --json --skip-git-repo-check`；旧 CLI 不支持该命令时明确报错，不退回到复用原 ID。Claude 使用官方 `@anthropic-ai/claude-agent-sdk` 的文件操作 `forkSession(id, { dir, title })`，由官方实现处理消息 UUID、父链和压缩元数据；后续执行仍使用本机 Claude CLI 和既有登录。参考 [官方会话分叉说明](https://platform.claude.com/cookbook/claude-agent-sdk-05-building-a-session-browser#part-4-fork-and-resume)。分叉本身不调用模型。

新分支继承模型、工作目录、消息、附件与执行历史，生成独立的本机 ID、原生会话 ID、消息 ID 和执行 ID，记录 `forkedFrom`；后续消息仅续接分支原生 ID。已完成的 @ 智能体结果保留显示，但不继承官方会话或任务 ID，避免新分支继续写入原分支的智能体会话。分叉失败不增加本机列表记录；切换账号取消分叉结果发布。

分叉的是会话上下文，工作目录中的文件仍共用；不会创建 Git worktree 或撤销文件改动。当前为从最新历史完整分叉，不提供逐条消息分叉。Codex 的新版本可使用冻结的历史引用保存分支，原生文件没有重复的消息行不代表丢失上下文，验证应使用 `thread/read` 重建历史。

相关验证：

```sh
pnpm --filter @multica/desktop test src/local-host/local-provider-fork.test.ts src/local-host/local-conversation-service.test.ts src/local-host/local-provider-runner.test.ts src/renderer/src/platform/local-conversation-service.test.ts src/renderer/src/pages/local-conversation-flow.test.tsx
pnpm --filter @multica/tutti-agent-gui-host test src/runtime/runtime.test.ts
pnpm --filter @multica/desktop typecheck
pnpm --filter @multica/tutti-agent-gui-host typecheck
pnpm --filter @multica/desktop build
```

默认测试仅使用伪造 CLI 和临时 Claude 数据目录；不会查找或调用用户模型。实机无推理验收核对两种原生分支 ID、继承的消息内容和原会话文件哈希。

本次验收：上述 57 项测试、两层类型检查、桌面构建和桌面修改文件的 lint 通过。桌面分别点击两种运行器的分叉按钮后，新分支显示完整历史并持久化；重启后 Codex 分支恢复 `gpt-6.1-sol`，发送一次不使用工具的真实续聊正确复述原会话回复，两个原会话的本机记录哈希未变。Claude 分支核对原生历史和独立 ID，没有额外发送模型消息。跨平台安装包、较旧 Codex CLI 及逐条消息分叉不在本次实机验收范围。

额外借用桌面 ESLint 配置检查宿主文件时，发现既有的五处规则错误（`create-multica-agent-gui-runtime.ts` 中四处 `no-case-declarations`、`model-preferences.ts` 中一处 `no-control-regex`），不涉及本次修改的代码行，未扩大范围修改。

CLI 子进程保留桌面进程的模型服务环境配置；Multica 业务身份由下面的独立查询上下文绑定，不继承工程父进程的 `MULTICA_*`。已经打开的终端和桌面不会同步环境。排查“终端正常、桌面失败”时，要比较两边实际进程的模型服务地址、模型覆盖项和凭据是否存在，而不能只看 `~/.claude/settings.json` 中的模型别名。比较凭据时只检查存在性或是否相同，不输出值。

对话流中的 Codex / Claude Code 不加载本机插件或外部 MCP，只保留基础工具与 Multica 内置 MCP。隔离通过每次启动的参数和 SDK 设置实现，覆盖新会话、续聊、旁支、分叉及标题生成；独立 Skills、Hooks、项目规则、登录及模型配置不变，也不写入用户全局配置。具体边界见 [本机上下文 MCP](local-context-mcp.md#默认注册与生命周期)。

从另一个智能体的工程环境启动桌面时，不能将仅属于该工程的 `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_MODEL` 或 `ANTHROPIC_DEFAULT_*_MODEL` 覆盖项误带给本机 Claude Code。这些覆盖项可能将原本使用本机登录的 CLI 改投其他模型服务。应修正本次启动环境，保留用户主动配置的服务路由和全局登录；应用本身不无条件删除用户的模型服务配置。

`pnpm dev:desktop:cloud:claude-login` 是沿用本机 Claude 登录的显式开发入口，对应 `apps/desktop/scripts/claude-login-env.mjs`。它仅清理桌面子进程继承的 Claude 模型服务覆盖项，不修改调用者环境或用户配置文件。启动隔离测试使用虚构凭据，不查找或调用真实 CLI。配置了本机 API 路由的用户仍由 CLI 配置决定服务，入口名称不代表一定拥有官方订阅。

@ 显式选择的智能体通过官方已有聊天接口执行，不依赖新增的多智能体接口。每个目标结果独立展示。断网会重试同步；发送是否被服务端接受无法确认时不自动重发，提示先查看对应云端聊天，避免重复执行。

## 验证

```sh
pnpm --filter @multica/desktop test src/local-host/local-provider-runner.test.ts src/local-host/local-conversation-service.test.ts src/renderer/src/platform/local-conversation-service.test.ts
pnpm --filter @multica/tutti-agent-gui-host test
pnpm --filter @multica/tutti-agent-gui-host typecheck
pnpm --filter @multica/views test tutti-agent-gui
pnpm --filter @multica/desktop typecheck
pnpm --filter @multica/desktop build
git diff --check
```

新增 12 项测试覆盖：CLI 参数与原生续接、输出解析与去重、落盘恢复、取消与并发拒绝、账号与工作区隔离、附件作用域、普通消息零云端创建/发送、多个官方 @ 的独立执行和结果回显、完成记录早于回复可见的时序、发送前与发送交叉时的取消。宿主 34 项测试、Views 对话流 41 项测试通过。真实 Claude 调用未进行；Windows CLI 启动尚未实机验收。

## 桌面实机验收（2026-10-24）

- Cloud 桌面主选择器仅显示本机 Codex、Claude Code；模型菜单读取本机 Codex 模型目录。
- 在专用临时工作目录，通过桌面发送一条不调用工具、不读写文件的 Codex 消息，14 秒后返回 `LOCAL_CODEX_PROVIDER_CONNECTED`。
- 同期桌面 API 日志中，创建和发送云端聊天的 POST 请求数为 0。普通消息不保存在 Multica Server；本机 CLI 仍使用其正常模型服务。
- 本地文件回读确认两条消息和原生线程标识均已保存；完整重启 Cloud 桌面后，同一会话和回复恢复，输入框为空。
- `@` 的“智能体”目录显示官方 9 个已定义智能体，未发送验收引用，避免触发现有生产智能体的业务动作。多个 @ 的发送和结果回收由隔离自动测试验证。
- 原“聊天”中的官方会话和历史正文仍正常显示。
- 桌面和宿主类型检查、相关 lint、桌面完整构建、`git diff --check` 通过。Vendored 源码清单 2,491 个文件哈希全部一致。

截图保存在仓库 `.tmp/local-conversations/`：

- [本机运行器选择器](../../.tmp/local-conversations/01-local-provider-picker.png)
- [Codex 真实回复](../../.tmp/local-conversations/02-local-codex-result.png)
- [官方智能体引用目录](../../.tmp/local-conversations/03-official-agent-mentions.png)
- [完整重启后的本机历史](../../.tmp/local-conversations/04-after-restart.png)
- [保留的官方聊天](../../.tmp/local-conversations/05-original-cloud-chat.png)

## 追加回归与修复（2026-10-24）

用户要求再次完整测试后，扩大了回归范围：桌面全部 723 项、聊天相关 235 项、GUI 宿主 34 项通过。

桌面交互发现已有会话中切换运行器会回跳：宿主同时清除当前会话并设置新执行目标，而原 GUI 在该轮渲染中仍持有旧会话，随后用旧会话的元数据回写执行目标。会话与首页的输入器会重新挂载，因此待切换目标必须由宿主应用保存，不能放在输入器内部。

修复仅在宿主适配层：先清除活动会话，等 GUI 的输入器确认进入首页，再设置请求的执行目标；新目标跨输入器挂载保留，外部选中其他会话时取消待处理切换。没有改动 vendored GUI。扩充 `e2e/tutti-conversation-interactions.spec.ts`，覆盖从已有会话切换目标、进入空白输入器、返回后原草稿仍在。该测试在修复前复现失败，修复后通过。

```sh
pnpm --filter @multica/desktop test
pnpm --filter @multica/views test tutti-agent-gui chat-page chat-input chat-thread-list chat-session-header use-chat-controller floating-chat-visibility
pnpm --filter @multica/tutti-agent-gui-host test
make env-exec ARGS='-- pnpm exec playwright test e2e/tutti-conversation-interactions.spec.ts --trace retain-on-failure'
```

合计 992 项自动测试及 1 条端到端交互场景通过。修复后的宿主类型检查和桌面完整构建通过；相关 lint 为 0 个错误，存在 3 条原有 Hook 依赖警告。日志位于 `.tmp/local-conversations/retest-*.log`。

真实 Codex 追加验收：选择 `gpt-6.1-sol` 后，要求它仅重复上一条回复，消息中不提供预期答案。Codex 正确返回上一轮的 `LOCAL_CODEX_PROVIDER_CONNECTED`；本地回读确认两轮执行均完成、所选模型已保存，期间创建或发送官方聊天的 POST 请求仍为 0。桌面操作还覆盖置顶与取消置顶、归档与恢复、搜索空状态与 Escape 清空、临时工作目录选择。新增的空白测试会话只在本机，不触发 Claude 调用。

用户随后确认重启后的界面正常并置前，工具已能重新读取页面、本机会话列表和输入器。桌面菜单的自动点击仍不稳定，未将此计作实窗切换通过。

补充 `e2e/local-provider-gui.spec.ts`，通过原 MessagePort 协议挂载与桌面相同构建的 GUI，使用确切的 `local:codex` / `local:claude` 目录。验证选择 Claude 后进入空白输入器，回到原 Codex 会话后草稿和模型选择恢复，且没有任何 API 请求或模型调用。此用例为隔离浏览器中的桌面 GUI 协议验收，不是 Electron 原生菜单自动化。

```sh
make env-exec ARGS='-- pnpm exec playwright test e2e/local-provider-gui.spec.ts --trace retain-on-failure'
```

该用例与相关 lint 通过，累计 992 项自动测试和 2 条端到端场景通过。Claude 与生产智能体未真实调用。本次菜单操作产生的本机验收草稿没有发送。

- [切换到本机 Claude 的隔离 GUI 验收](../../.tmp/local-conversations/06-local-provider-switch.png)
- [返回 Codex 后恢复草稿和模型](../../.tmp/local-conversations/07-restored-local-draft.png)

## Claude 本机登录复核

使用 `dev:desktop:cloud:claude-login` 启动后，桌面主进程没有继承 Claude 模型服务覆盖项。通过同一入口的环境转换检查 `claude auth status --json`，结果为已登录、`authMethod: claude.ai`、`apiProvider: firstParty`、`subscriptionType: max`。

用户在桌面发送的 `222` 已实际完成，耗时约 4.4 秒；本机会话保存了用户消息和助手回复，执行状态为 `completed`，无错误。对应原生助手记录的实际模型是 `claude-opus-5-5`。本轮复核使用这条已完成的请求，没有额外发送模型消息。用新入口完整重启后，实窗恢复同一会话和回复。

```sh
pnpm --filter @multica/desktop test scripts/claude-login-env.test.mjs scripts/worktree-dev-env.test.mjs src/local-host/local-provider-runner.test.ts src/local-host/local-conversation-service.test.ts
pnpm --filter @multica/desktop exec eslint scripts/claude-login-env.mjs scripts/claude-login-env.test.mjs scripts/dev.mjs
pnpm --filter @multica/desktop typecheck
git diff --check
```

23 项相关测试、lint 和桌面主进程及渲染层类型检查通过。新入口启动时主进程和 preload 编译成功，本机渲染入口返回 200。本轮实测范围为普通文本对话与重启恢复；原失败请求保留原状态，没有自动重发。

## 本机会话按项目分组（2026-10-24）

参考 Tutti 的 `packages/agent/gui/agent-gui/agentGuiNode/model/agentGuiConversationRail.ts`、`agentGuiConversationRailOrganization.ts`、`agentGuiConversationRailProjectSorting.ts` 和 `view/AgentGUIConversationMove.tsx`：按明确的项目标识组织会话，未关联会话单列，项目顺序稳定，支持折叠和移动，保存失败时保留原归属。

左侧项目代表用户引入的本地文件夹。点击“新建项目”通过 `folderProjectAdd` 打开原生目录选择器，校验目录、按真实路径去重，将 `LocalFolderProject` 保存到当前账号、API 环境和工作区隔离的本机文件 `index.json` 的 `folderProjects` 中。取消选择不新增记录，不创建云端项目、任务或会话。

- `folderProjects` 读取本机目录清单。分组通过会话 `cwd` 匹配已引入目录；未匹配的会话归入“未分配项目”。已有云端 `projectId` 仍仅表示任务关联，不参与本地目录分组。
- 顶部和组内“＋”只打开 GUI 首页草稿，不调用本机会话 `create`，不分配正式会话 ID 或工作目录。桌面通过可选的 `newConversationRequest`（请求序号及项目路径）传入目录；GUI 等旧会话退出后才应用到活动的首页输入器，同一请求只应用一次，普通刷新不覆盖用户后来选择的目录。
- 首次有效发送沿用 GUI 的 `session.create.cwd` 流程，桌面适配将目录解析为 `folderProjectId` 或已有会话的 `directorySourceId`。主进程查找当前作用域的目录记录并重新校验路径；未指定目录时才建立独立临时工作目录。失效目录明确报错，不会悄悄换成其他目录。新建入口不改输入、附件上传状态、模型或权限，已有的身份绑定、队列接收及显式重试继续使用原有机制。
- 旧版已保存的空记录不删除或迁移。桌面向 GUI 投影可引用会话时，只收录已有消息、轮次或待发送队列的记录，不按标题判断。带有内容但仍名为“新会话”的记录可正常引用。本机 IPC 和历史文件格式不变。
- 顶部新建继承当前会话匹配的本地目录，分叉继续继承原工作目录。搜索支持目录名称与路径。空目录分组会保留，重启后仍可见。
- GUI 的 `AgentGuiSession.localDirectory` 独立携带实际 `cwd` 与可选的项目路径；自动生成的工作目录显示为“不使用项目”，显式选择的目录显示对应项目。该字段只在桌面 GUI 适配边界生成，不写入云端 `ChatSession` 或本机会话文件。
- 同一运行器切换模型保留目录；Codex / Claude Code 之间切换时，宿主跨输入器挂载保留草稿和项目路径。首次发送才通过 GUI `session.create.cwd` 创建新会话。桌面适配把已引入目录解析为 `folderProjectId`，手动选择过的其他目录解析为当前作用域会话的 `directorySourceId`；主进程重新校验目录及真实路径。目录失效或来源不属于当前工作区时明确失败；无项目的新会话仍创建自己的独立工作目录。
- 会话菜单中的“关联工作区项目”独立保留原 `projectId` 关联，不更改本机工作目录。项目的“从列表移除”、撤销和恢复仅操作本机显示偏好，不删除文件、项目记录或会话。
- 使用 Multica 现有 Button、IconButton、DropdownMenu 和语义字体、颜色及间距样式。分类不重新挂载对话组件，因此当前对话和草稿可保留。

验证命令：

```sh
pnpm --filter @multica/desktop test src/local-host/local-folder-projects.test.ts src/local-host/local-conversation-service.test.ts src/local-host/local-conversation-import.test.ts src/renderer/src/platform/local-conversation-projects.test.ts src/renderer/src/pages/local-conversation-flow.test.tsx src/renderer/src/stores/local-conversation-project-visibility.test.ts
pnpm --filter @multica/desktop exec vitest run src/renderer/src/platform/local-conversation-service.test.ts
pnpm --filter @multica/tutti-agent-gui-host exec vitest run src/runtime/runtime.test.ts
pnpm --filter @multica/tutti-agent-gui-host exec vitest run src/composer-footer-controls.test.tsx
pnpm --filter @multica/views exec vitest run tutti-agent-gui/tutti-agent-gui-panel.test.tsx
pnpm exec playwright test e2e/local-new-conversation.spec.ts --project=chromium
pnpm exec playwright test e2e/local-project-context.spec.ts e2e/local-provider-gui.spec.ts --project=chromium
pnpm --filter @multica/desktop typecheck
pnpm --filter @multica/desktop exec eslint src/local-host/local-conversation-service.ts src/local-host/local-conversation-service.test.ts src/renderer/src/pages/local-conversation-flow.tsx src/renderer/src/pages/local-conversation-flow.test.tsx src/renderer/src/platform/local-conversation-projects.ts src/renderer/src/platform/local-conversation-projects.test.ts src/renderer/src/platform/local-conversation-service.ts src/renderer/src/platform/local-conversation-service.test.ts
pnpm --filter @multica/desktop build
git diff --check
```

31 项相关测试、类型检查、以上范围 lint、桌面构建通过。测试覆盖项目身份、旧会话兼容、归属持久化与清除、分叉继承、搜索、折叠、项目内新建、保存失败和移动后草稿保留。桌面完整重启后，将一个既有空白本机会话移入官方“Bug列表”项目，实窗显示项目组与“未分配项目”两组；折叠与项目名称搜索生效，右侧当前对话保持显示。随后通过同一菜单将验收会话移回“未分配项目”。本次验收未发送模型消息。

截图：[项目分组](../../.tmp/local-conversations/08-project-groups.png)。项目归属只保存在本机，折叠状态暂不跨重启保存；本次未增加拖拽移动或本机目录分组模式。


## Desktop runtime compatibility

Local conversation requests first query the main process's `capabilities` endpoint.
It returns `protocolVersion` and the supported IPC method names. The shared method
registry is exhaustive over `LocalConversationMethods`, and the main dispatcher
has an exhaustive switch. Additive methods keep the protocol version; incompatible
request/response semantics require a version bump. Do not treat package versions
as protocol compatibility.

The renderer shares one in-flight capability check per account/workspace. A missing
method or incompatible/malformed response produces a persistent restart notice;
known unsupported operations are rejected before dispatch. Legacy main processes
without a handshake can still handle their existing requests. An unknown-method
failure is converted to an actionable error and is never replayed through another
mutation. Permission and I/O failures remain distinct from version errors.

Attachment hydration is independent for each file. Missing/unreadable attachments
or unsupported metadata lookup preserve the rest of the transcript and working
attachments, with a display-only unavailable note. Stored messages and attachment
IDs are unchanged. Reopening the conversation retries file reads; refreshing a
renderer does not update a running main process. Attachment recovery never falls
back to transferring entire files over IPC.

The development launcher intentionally does not force-restart Electron while
agent work may be running. After main/preload changes, finish or stop active work,
then fully restart the desktop development process. Do not use renderer HMR or a
standalone build as evidence that the running main process has updated.

Relevant regression checks:

```sh
pnpm --filter @multica/desktop exec vitest run src/renderer/src/platform/local-conversation-client.test.ts src/renderer/src/platform/local-conversation-service.test.ts src/renderer/src/pages/local-runtime-notice.test.tsx src/renderer/src/pages/local-conversation-flow.test.tsx src/renderer/src/pages/local-side-workspace.test.tsx src/local-host/local-conversation-service.test.ts
pnpm --filter @multica/desktop typecheck
pnpm --filter @multica/desktop build
```
