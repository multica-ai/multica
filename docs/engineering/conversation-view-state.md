# 本地会话视图状态

当前状态：步骤 0–3、5 大体落地，步骤 4 部分完成，真实用户会话尚未验收。方案见 [迁移执行手册](local-conversation-view-migration.md)；下面分别记录实现、自动验证和既有行为边界。

## 迁移对照

| 步骤 | 已落地内容 |
| --- | --- |
| 0：基线与隔离 | 从 `origin/main` 的 `115bfc7c0` 建独立 worktree；固定假宿主和性能夹具；独立 Electron HOME、配置和端口。未完成：T0.5 的 `perf_load.go` 和 `scripts/perf/` |
| 1：移除 engine | 原生 Prompt、Activation、SessionLoad、Composer Service 接管命令；GUI 会话状态从原生 Model 派生，保留轮次决策前的 flush |
| 2：界面订阅 | 删除隐藏会话栏；队列面板和 fork 按钮直接观察原生 Service |
| 3：按轮转录 | 每轮 computed、按 key 读行、独立待发送尾部和稳定结构索引；不改变虚拟化和滚动算法 |
| 4：拆分 controller（部分完成） | 已有 Shell context；原生状态、交互门控、设置、详情和状态条的 GUI computed state；草稿独立保留原存储。未完成见下节 |
| 5：删除翻译层 | 直接 typed host services；移除 ledger 写路径、旧投影、无效 Side 和 Goal 文案；旧身份及待发送记录一次性迁移 |

## 步骤 4 的剩余工作

- 七组数据仍由 `useAgentGUIViewAssembly` 汇总并经 props 传递，`agentGuiNodeTypes.ts` 仍保留七个 `*ViewModel` 类型。手册的完成标准 grep 只检查旧名字，通过不代表拆分完成。
- `useAgentGUILocalState`、`useAgentGUIControllerRefs`、`useAgentGUIControllerActions` 待删除；会话选择的三个 hook 待合成 `ConversationSelectionService`。
- `useAgentGUISubmitInteractionActions`、`useAgentGUIComposerSettingsActions`、`useAgentGUIQueueActions` 待拆，动手前先补草稿保存和恢复测试。
- `AgentGUIFrame.tsx` 承接了原 `AgentGUINode.tsx` 的逻辑，待缩减到只负责布局。
- 部分 `useMemo` 和 effect 依赖仍读 Model 字段；`useAgentGUISessionPresentation` 在依赖变化时新建 state 实例，待改为稳定实例。

原生状态不再经 `useAgentGUIViewAssembly` 复制成一组提交、停止、交互和 fork 状态值。`AgentComposer` 直接读取稳定的展示 Model，`AgentSessionChrome` 读取可观察的 chrome source。宿主订阅和延迟提示由 `SessionPresentationHostService` 管理，卸载后释放。

草稿更新只改变原有客户端草稿 props，不重建原生展示 Model，避免技能列表引用变化引起编辑器重置光标。详情缓存、消息和轮次在同一 MobX action 发布；Model 的首次加载也在 action 内完成。

## 已实现的主体

视图按会话、轮次和行订阅状态。命令由原生 Service 执行，GUI 不维护第二份命令 reducer。

## 转录边界

`TranscriptModel` 保留每轮消息和顺序；`ConversationRowsModel` 为每轮创建 computed，调用现有纯投影函数。消息行按 key 读取，工具折叠产生的片段按消息 ID 读取当前内容。列表结构、用户定位信息和虚拟化判断单独计算。流式正文不进入父组件的订阅。

历史轮次不依赖其他轮次正文。虚拟化复杂度按轮计算，列表只汇总分数；达到原有阈值时更新布局。原有跟随到底、滚动记忆、翻页锚点和展开动画的行为保持不变。

原生模型收到事件后先更新；GUI 的消息提交与轮次呈现共享原子提交边界。会话派生通过提交版本读取原生状态，避免在批量提交前暴露一半更新。提交、队列操作、激活和详情加载在决定轮次操作前调用 `commits.flush()`。

## 会话身份与旧存储

新建会话使用客户端生成的 ID，并将它传入 daemon 现有的 `POST /sessions` 的 `id` 字段。daemon 的唯一键阻止同一 ID 创建重复会话。结果未知时先读取该 ID 并核验归属，再决定重试；不生成替代 ID。Fork 的新 ID 由 daemon 返回，客户端核验来源会话和轮次后采用返回 ID。

旧 `runtime-ledger-v1` 仅作为迁移输入：静态别名用于转换旧草稿、排队内容和打开请求。新会话不生成别名。草稿继续由现有 draft store 保存。新队列继续由 PromptService 和 desktop pending store 保存，消息版本仅在内存中用于当前视图的读取游标。

旧队列的 accepted 收据不重发；未知收据转成原生队列中暂停的可见草稿，并显示发送前检查记录的提示。恢复草稿持久化成功后才清除迁移输出中的旧队列；存储失败则继续保留旧记录。只有迁移输出持久化成功后才记录迁移完成。原始旧记录保留供恢复，完成标记阻止下次启动再次导入。

## 验证边界

Model 测试检查未变化轮次的对象身份、轮次顺序和定位信息。组件测试使用真实 Model/Service 和可控宿主，断言流式时只更新受影响的行。`application.streaming.test.tsx` 用真实应用断言流式时页面外层、详情面板和输入框不重渲染，并覆盖轮次结束后停止按钮消失。整页冒烟覆盖无选择挂载和会话切换；翻页依赖真实滚动，由浏览器回归覆盖。浏览器测试使用独立假宿主，不启动用户的模型 CLI 或已安装 daemon。

## 改造对应关系

| 原职责 | 当前入口 |
| --- | --- |
| engine reducer 和快照 selector | `AgentSessionsModel`、原生 Prompt/Activation/SessionLoad Service |
| 全会话投影和行缓存 | `TranscriptModel.turnMessages`、`ConversationRowsModel` 的每轮 computed |
| 原统一页面 view model | Shell context、客户端临时状态 props 与职责明确的 GUI state 实例 |
| runtime 附件方法 | `ConversationServices.attachments` |
| runtime 诊断方法 | `ConversationServices.diagnostics` |
| 同一页面内的 RPC | `AgentGuiHostServices` 的分组直接调用；边界检查由 `createHostServicesConnection` 保留 |
| 新建和发送身份账本 | 原生会话 ID、PromptService admission；旧记录由一次性迁移读取 |

轮次投影继续复用既有纯函数和不可变行模型；命令、消息索引和 UI 订阅已重写。
此次改造沿用同一产品已有 UI primitive、图标和语义 token，不引入新的视觉体系。
Daemon 路径、事件结构、权限校验和跨版本响应兼容规则保持不变。

## 运行验证

从仓库根目录运行：

```sh
pnpm typecheck
pnpm lint
pnpm test
(cd server && go test ./cmd/multica -run 'Test.*(HealthPort|Isolated)' -count=1)
node packages/tutti-agent-gui-host/scripts/build-e2e-harness.mjs "$PWD/.tmp/conversation-view-browser"
node scripts/check-conversation-view-mount.mjs .tmp/conversation-view-browser
node scripts/check-conversation-view.mjs .tmp/conversation-view-browser .tmp/conversation-view-check/head
```

浏览器挂载检查使用自己的临时端口和全新浏览器上下文，不依赖应用登录或运行中的 daemon。
它覆盖样式隔离、主/Side 两个实例、主题、弹层、长记录翻页与定位条跳转、失败后用户触发重试，以及工具组展开和折叠。
性能夹具固定为 120 轮历史、120 个事件/秒；输出脚本时间、布局时间、堆大小、输入耗时、滚动漂移和截图。
第四个参数可传相同夹具在基线版本生成的 `report.json`，据此断言跟随间距不退化。
报告始终保留严格贴底目标的偏差，不能将相对基线通过等同于绝对贴底通过。

需要在独立桌面实例手工验收时使用：

```sh
pnpm --filter @multica/desktop dev:isolated
```

启动器使用临时 HOME、CODEX_HOME、Electron userData/sessionData，以及独立健康端口和渲染端口；退出时删除临时目录。
它限制 PATH、隔离供应商环境变量，并禁用隔离实例的登录 shell PATH 恢复和自动 Codex MCP 注册。它只停止自己创建的进程组，并监测正式 runtime 的 launch、PID、CLI、profile config 和 Codex config 是否发生变化。
该命令构建本 checkout 的 Multica CLI 并启动开发版 Electron。已验证隔离壳启动和退出后的正式文件不变；空配置没有登录后端，因此不将它记为真实用户会话通过。默认测试使用假宿主，不运行真实 Agent 或模型。

浏览器夹具在远端 main 和本实现均观察到流式结束后 40 px 的底部间距；脱离跟随和切换返回的漂移均为 0。
按迁移方案保留滚动控制器行为：40 px 是相同夹具在 main 上已有的严格贴底偏差，不将相对基线通过写成绝对贴底通过。真实用户会话未代替用户确认。

## 本次性能对照

基线为远端 main 的 `115bfc7c0`。相同浏览器、相同假宿主数据，先交替运行三次，再在最终代码上复测新实现三次，以下为最终采样的中位数：

| 指标 | main | 新实现 |
| --- | ---: | ---: |
| JS 脚本时间 | 695 ms | 370 ms |
| 主线程任务时间 | 1235 ms | 971 ms |
| 布局时间 | 36 ms | 44 ms |
| 样式计算时间 | 361 ms | 435 ms |
| 输入同一句话 | 817 ms | 752 ms |
| GC 后保留堆 | 27,950,940 B | 27,236,980 B |

脚本时间下降约 47%，主线程任务时间下降约 21%，保留堆下降约 2.6%；布局和样式计算仍增加。即时堆受 GC 时机影响，不作为保留内存结论。
这些数字仅说明此固定长会话夹具；手册 1.4 节的切换耗时、首开 500 轮和快速滚动帧率未重测。复杂工具输出、慢设备和用户真实会话由后续人工验收覆盖；原方案禁止为了验收运行真实模型。

三次基线和三次最终样本保存在 [性能数据](conversation-view-performance.json)，测试期间输入文本与编辑器节点均保留，脱离跟随和切换返回的漂移均为 0。

本次验证结果：`pnpm typecheck`（12 个任务）、`pnpm lint`（8 个任务）、`pnpm test`（11 个任务，13,941 项 Vitest 用例）通过。Lint 仍有既存 warning。
浏览器挂载与交互回归 7/7 通过；隔离健康端口的 Go 测试通过。Mobile、真实模型执行和用户真实会话未验证。隔离 Electron 的启动、退出和正式文件不变检查已通过。
