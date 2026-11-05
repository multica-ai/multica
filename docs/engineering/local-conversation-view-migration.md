# AI 会话视图层迁移执行手册

接 [AI 会话状态迁移到 MobX 方案](local-conversation-mobx-models.md) 的阶段 6、7。状态已经进了 Service 和 Model（#130、#131、#132），本手册处理剩下的部分：视图直接读 Model，并删掉 engine、controller 汇总和翻译层。

**这是给执行者照做的步骤。** 按顺序做，不要跳步，不要把两个任务合成一个。遇到第 2.4 节列出的情况就停下来问。

状态：已按此方案实施并合入（#134），步骤 4 部分完成，当前进度见 [本地会话视图状态](conversation-view-state.md)。本文保留原方案，数字和文件清单取自 `115bfc7c0`，不含测试，不反映当前代码。

路径缩写：

| 缩写 | 路径 |
| --- | --- |
| `G/` | `vendor/tutti/packages/agent/gui/` |
| `N/` | `vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/` |
| `C/` | `vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/controller/` |
| `V/` | `vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/view/` |
| `T/` | `vendor/tutti/packages/agent/gui/shared/agentConversation/components/` |
| `H/` | `packages/tutti-agent-gui-host/src/` |
| `CC/` | `packages/conversation-client/` |
| `E/` | `vendor/tutti/packages/agent/activity-core/src/engine/` |

## 1. 现状

### 1.1 数据流

```text
ConversationModel（CC/models）
  → H/runtime/create-multica-agent-gui-runtime.ts 的 project()
    （projection.ts、v2-projection.ts、local-transcript.ts、ledger.ts）
  → E/createAgentSessionEngine.ts 的简单存储 + TranscriptModel
  → H/runtime/desktop-queue-engine.ts 叠加 PromptService、ActivationService、SessionLoadService，拼成 engine 快照
  → 选择器（useEngineSelector）
  → C/useAgentGUINodeController.ts 汇总成 viewModel + actions
  → N/AgentGUINode.tsx → N/AgentGUINodeView.tsx → V/AgentGUIDetailPane.tsx → 转录和输入框
```

### 1.2 数量

| 项目 | 数量 |
| --- | --- |
| controller 目录 | 约 1.6 万行 |
| view 目录 | 约 1.06 万行 |
| `observer` 组件（GUI 内） | 0 |
| engine 目录 | 约 3000 行 |
| 用 engine 的文件 | 22 个：`useEngineSelector` 36 处、`getSnapshot` 23 处、`dispatch` 11 处 |
| `useMemo` | 159 处，分布在 66 个文件 |
| `viewModel` 字段 | 61 个，分 7 组 |
| `AgentGUIRuntime` 成员 | 约 32 个 |

桌面端用户看到的会话列表是 `apps/desktop/src/renderer/src/pages/local-conversation-flow.tsx`，**已经是 `observer`，不在本手册范围内**。GUI 里的 `V/AgentGUIProviderRail.tsx` 被 `conversationRailCollapsed: true` 隐藏，但一直挂载，步骤 2 删除它。

### 1.3 重新统计的命令

```bash
G=vendor/tutti/packages/agent/gui
grep -rE 'useEngineSelector\(' $G --include='*.ts*' | grep -vE '\.spec\.|\.test\.|shared/testing' | wc -l
grep -rE 'ngine\.getSnapshot\(\)' $G --include='*.ts*' | grep -vE '\.spec\.|\.test\.|shared/testing' | wc -l
grep -rE 'ngine\.dispatch\(' $G --include='*.ts*' | grep -vE '\.spec\.|\.test\.|shared/testing' | wc -l
grep -rE 'useMemo\(' $G --include='*.ts*' | grep -vE '\.spec\.|\.test\.' | wc -l
```

### 1.4 性能基线

2026-11-04 在隔离的生产构建上测得，合成数据，无模型调用。真实会话的流速 p50 为 18/s，p95 为 84/s，p99 为 186/s。

| 场景 | 结果 |
| --- | --- |
| 流式 120/s，8 秒内 | 约 105 fps，p95 帧 17 ms，布局 1.8 s，JS 1.4 s，主线程任务 4.5 s |
| 流式 200/s | 99 fps，p95 帧 17 ms |
| 切换已打开过的会话 | 约 110 ms，长任务约 65 ms |
| 首次打开 500 轮的会话 | 约 500 ms |
| 快速向上滚动并翻页 | 58 fps，p95 帧 52 ms，最长一帧 277 ms |
| GC 后的堆 | 反复切换后稳定，没有泄漏 |

**性能预期（推算，未实测）：** 流式 JS 从 1.4 s 降到 0.6–0.8 s，主线程总耗时降 15–20%，布局基本不变，这台机器上肉眼差别不大。这次迁移的主要理由是架构，不是性能。

## 2. 必须遵守的规则

### 2.1 不能做的事

1. **不能影响本机正式版 Multica。** 不启动 `pnpm dev:desktop`、`make dev`、`make desktop-dev`，除非用步骤 0 做出来的隔离模式。这些命令会抢走正式版的运行时。
2. **不能重启正式版的守护进程**，也不能写 `~/.multica/desktop-runtime/`、`~/.multica/profiles/desktop-runtime/`、`~/.codex/config.toml`。
3. **不能未经确认运行 `make up`、`make gc`、`make destroy`。** `make up` 每次都会回收旧环境，删除它们的数据。
4. **不能动 5432 端口上的 `data-analysis-postgres`。** 它是别的项目的。
5. 不启动真实 agent，不调用真实模型。
6. 不推送到 `upstream`，`gh` 命令都带 `--repo chovy-ai/multica`。不用 `--admin`。
7. 不加兼容层、双写或回退路径。例外：任务 T1.1 到 T1.12 之间，engine 和新 Model 的双写只存在于同一个 PR 内，合入前必须删除。

### 2.2 `observer` 写法

```tsx
import { observer } from "mobx-react-lite";

// 普通组件
export const AgentQueuedPromptPanel = observer(function AgentQueuedPromptPanel(props: Props) {
  const { prompts } = useAgentGUIRuntime();
  const queue = prompts.queue(props.agentSessionId); // 渲染时读，自动跟踪
  return <div>{queue?.items.length}</div>;
});

// 带自定义比较的组件，memo 在外
export const AgentTranscriptItemView = memo(observer(function AgentTranscriptItemView(props: Props) {
  /* ... */
}), areEqual);
```

规则：

1. **凡是读 Model 字段的组件，必须是 `observer`。** 否则界面不更新，测试还查不出来。
2. **在 `observer` 组件里调用的 hook，直接读 Model，不用 `useModelValue`。** 只有调用方还不是 `observer` 时才用 `useModelValue(read, deps)`。
3. **`useModelValue` 的 `read` 必须返回稳定的引用。** 不能写 `() => id ? read(id) : []`，要用模块级常量 `NO_ITEMS`。违反这条会无限重渲染，这个 bug 真实发生过。
4. **不要在 `useMemo`、`useCallback`、`useEffect` 的函数体或依赖数组里读 Model 字段。** 依赖数组只放 id 和 Model 实例。要缓存派生值，就在 Model 上加 `computed`。
5. 事件处理函数里读 Model 没问题。
6. 改动的组件如果有 `"use memo"` 指令，顺手删除。它不生效。
7. 工具函数不改成 `observer`，只改组件。

### 2.3 每个任务的固定流程

1. 只改任务列出的文件，加上它们的 spec。
2. 先写或改测试，确认失败原因正确，再改实现。
3. 跑检查（第 2.5 节）。
4. 用任务写好的提交信息提交一次。信息末尾加 `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`。
5. 进入下一个任务。

新分支从最新的 `origin/main` 拉，名字用 `claude/conversation-views-<步骤号>`。一步一个 PR。

### 2.4 必须停下来问的情况

- 测试要改**断言的含义**才能通过，而不只是改夹具。
- 要删一个行为测试，又说不清那个行为为什么不再需要。
- 发现本手册描述的文件、函数或数量和代码对不上，差别超过几行。
- 同一个任务的检查连续失败 3 次。
- 要改 daemon、Go 代码或数据库（步骤 0 的端口覆盖除外）。
- 手册写着“开工前评审”的地方。
- 怀疑自己影响了正式版。立刻停，不要自己修。

### 2.5 检查命令

```bash
pnpm typecheck
pnpm lint
pnpm test
(cd vendor/tutti/packages/agent/activity-core && node --test --experimental-strip-types --no-warnings './src/**/*.test.ts')
```

步骤 0 完成前，还要手动对比 GUI spec：

```bash
export LC_ALL=C
(cd vendor/tutti/packages/agent/gui && NO_COLOR=1 ../../../../../packages/tutti-agent-gui-host/node_modules/.bin/vitest run --reporter=dot > /tmp/gui.out 2>&1)
grep -E 'Tests +' /tmp/gui.out
grep -E '^ FAIL ' /tmp/gui.out | sed -E 's/^ FAIL +(\|?[a-z]+\|?) +//; s/^ FAIL +//' | sort -u > /tmp/now.fail
sort -u .tmp/gui-spec-baseline.fail | sed -E 's/^ FAIL +(\|?[a-z]+\|?) +//' | sort -u > /tmp/base.fail
comm -23 /tmp/now.fail /tmp/base.fail   # 输出必须为空
```

踩过的坑，都真实发生过：

- 不设 `NO_COLOR=1`，输出带颜色码，grep 匹配不到，会误以为 0 个失败。**先看 `Tests` 那一行的总数。**
- 不设 `LC_ALL=C`，两个列表排序不一致，`comm` 会误报新增。
- 新写的回归测试，要把修复临时撤掉再跑，确认它**因为预期的原因**失败。只看到它失败不够，可能是别的原因。
- 检查全绿不等于页面能打开。步骤 0 的冒烟测试就是为此加的。

## 3. 待评审的决定

下面按“建议”写。评审时如果改了，要先改本文再动手。

| 问题 | 建议 | 代价 |
| --- | --- | --- |
| 保留展示类型 `AgentActivitySession`、`AgentActivityMessage` 吗？ | 保留到步骤 5 之后 | 这层类型留得更久 |
| 怎么交付？ | 每步一个 PR，步骤之间在真实应用里验证 | 周期更长 |
| 隔离启动要改产品代码吗？ | 要，只加一个开发用的环境变量 | 多一个配置项 |

## 4. 步骤 0：安全网

分支：`claude/conversation-views-0`。

### T0.1 修 GUI spec 基线

目前 47 个测试失败：

| 文件 | 失败数 |
| --- | --- |
| `V/useAgentGUIDetailScroll.spec.tsx` | 15 |
| `G/shared/AgentMessageMarkdown.spec.tsx` | 8 |
| `G/tsup.dts.config.test.ts` | 7 |
| `N/AgentMentionSearchController.spec.ts` | 6 |
| `C/useAgentGUISubmitInteractionActions.spec.ts` | 3 |
| `G/shared/utils/agentTitleText.spec.ts` | 整个文件加载失败，计 1 |
| `T/tool-renderers/AgentExpandedToolContent.spec.tsx` | 1 |
| `T/AgentTranscriptView.spec.tsx` | 1 |
| `G/shared/AgentMessageMermaid.spec.tsx` | 1 |
| `C/useAgentGUILocalState.spec.tsx` | 1 |
| `N/composerFloatingMenu/ComposerFloatingMenuSurface.spec.tsx` | 1 |
| `N/AgentMentionIssueTopicGroups.spec.ts` | 1 |
| `N/AgentInteractivePromptSurface.spec.tsx` | 1 |
| `N/AgentFileMentionPalette.spec.tsx` | 1 |

逐个文件处理，每个文件一个提交。先读失败信息，归类：

| 类别 | 判断方法 | 处理 |
| --- | --- | --- |
| 测的东西已删除 | spec 引用的模块或导出已不存在；或者没有生产代码调用被测函数（用 grep 确认） | 删除 spec，提交信息写明被删模块和确认方式 |
| 夹具过期 | 生产代码没错，只是 mock 缺字段或 provider | 改夹具，不改断言 |
| 环境问题 | jsdom 缺 API、时序依赖 | 补 stub 或固定计时器 |
| 产品有 bug | 按断言描述的行为，生产代码确实错了 | **停下来问**，附上复现方式 |
| 判断不了 | | **停下来问** |

`G/tsup.dts.config.test.ts` 大概率属于第一类：GUI 不再构建 dist。同时确认 `tsup.config.ts`、`tsup.dts.config.ts`、`build/` 没人用了，一起删除。

提交信息：`test(desktop): repair <file> specs` 或 `test(desktop): remove specs for deleted <module>`。

### T0.2 把 GUI spec 接入 `pnpm test`

1. 打开 `packages/tutti-agent-gui-host/package.json` 的 `test` 脚本。它现在只跑指定的几个 GUI spec。
2. 把 `vitest run --root ../../vendor/tutti/packages/agent/gui <指定文件...>` 改成不带文件列表，跑全部。
3. 在同一个脚本里加上 engine 测试：`node --test --experimental-strip-types --no-warnings '../../vendor/tutti/packages/agent/activity-core/src/**/*.test.ts'`。
4. 跑 `pnpm test`，必须通过。
5. 从第 2.5 节删除手动对比，删除 `.tmp/gui-spec-baseline.fail`。
6. 确认 CI 的 `frontend-test` 会跑这个脚本：查 `.github/workflows/`。

提交信息：`test(desktop): run every GUI spec in pnpm test`。

### T0.3 开发版隔离启动

1. Go，`server/cmd/multica/cmd_daemon.go` 的 `healthPortForProfile` 开头加：

   ```go
   if port, err := strconv.Atoi(os.Getenv("MULTICA_DEV_HEALTH_PORT")); err == nil && port > 0 {
       return port
   }
   ```

2. TS，`apps/desktop/src/main/daemon-profile.ts` 的 `healthPortForProfile` 开头加同样的判断，读 `process.env.MULTICA_DEV_HEALTH_PORT`。
3. 确认 Electron 启动守护进程时会传递这个环境变量：查 `apps/desktop/src/main/daemon-manager.ts`，不传就加上。
4. 给两处各写一个单测。
5. 新建 `apps/desktop/scripts/dev-isolated.mjs`：

   ```text
   a. 用 mkdtemp 建临时目录，作为 HOME
   b. 设置 MULTICA_DEV_HEALTH_PORT=<空闲端口>、DESKTOP_APP_SUFFIX=isolated-<随机>、DESKTOP_RENDERER_PORT=<空闲端口>
   c. 删除环境变量 MULTICA_DAEMON_P2P_SERVER_URL
   d. 记录正式版 launch.json 和 config.json 的内容和 mtime
   e. 启动 dev.mjs，带 --remoteDebuggingPort
   f. 启动 20 秒后再读一次 d 的两个文件；变了就杀掉自己启动的进程并报错退出
   g. 退出时删除临时 HOME。Go 模块缓存是只读的，要先 chmod -R u+w
   ```

6. `apps/desktop/package.json` 加脚本 `"dev:isolated": "node scripts/dev-isolated.mjs"`。
7. 这个模式需要一个后端。写在脚本的说明里：手动用 `docker run` 起一个独立端口的 Postgres（本机 5432、55432 都被占了），并启动 API。`make up` 不能用（第 2.1 条）。
8. 在 `CONTRIBUTING.md` 的桌面开发部分加一段，写明 `dev:desktop` 会接管本机运行时，要隔离就用 `dev:isolated`。

**验证（要用户在场时做）：** 启动前后，正式版 `launch.json`、`config.json` 没变，`curl 127.0.0.1:20094/health` 的 pid 没变。

提交信息：`feat(desktop): add an isolated dev mode that leaves the installed app alone`。

### T0.4 冒烟测试

在 `H/application.test.tsx` 增加一组测试，用现有假后端（`H/runtime/local-snapshot-test-host.ts`）驱动**真实的** `AgentGuiApplication`，不要 mock controller：

| 用例 | 断言 |
| --- | --- |
| 无选中会话时挂载 | 渲染出输入框，没有错误边界文案 |
| 选中会话 | 显示最后一轮的用户消息 |
| 切换到另一个会话再切回 | 显示对应内容 |
| 向上翻页 | 显示更早的轮次 |
| 收到流式事件 | 文字追加；提交后轮次结束，停止按钮消失 |

**验证测试有效：** 临时把 `C/useAgentGUISessionDetailTransport.ts` 的 `NO_MESSAGES` 改回 `[]`，第一个用例必须报 `Maximum update depth exceeded`。验证后还原。

提交信息：`test(desktop): smoke test the conversation application`。

### T0.5 可重复的性能测量

1. 新建 `server/internal/localconversation/service/perf_load.go`，第一行 `//go:build perfload`。提供造数据和流式两种操作，通过读一个命令文件触发。写入走 `store.ImportConversation` 和 `s.batcher`，与真实链路一致。普通构建不包含它。
2. 新建 `scripts/perf/`，用 `playwright-core` 的 `connectOverCDP` 连隔离模式的调试端口，测第 1.4 节的每一行，输出 JSON。
3. 脚本开头校验：调试端口对应的进程，`HOME` 不是真实 HOME。不是就报错退出。
4. 只用生产模式构建测量。开发版的 React 开销会严重误导结论。
5. **每轮用一个新会话。** 同一个会话连续测，内容累积会污染结果。

提交信息：`chore(desktop): add repeatable conversation performance measurement`。

### 步骤 0 完成标准

- [ ] `pnpm test` 包含全部 GUI spec 和 engine 测试，并通过
- [ ] 隔离模式启动前后，正式版没有变化
- [ ] 冒烟测试能抓住那个崩溃
- [ ] 第 1.4 节的基线用新脚本重测一遍，写回本文

## 5. 步骤 1：替换 engine 读取

分支：`claude/conversation-views-1`。

**开工前先做一次 CPU profile（隔离模式）：** 确认流式时 controller 汇总占 JS 的比例，结果写到第 1.4 节。

### T1.1 建 `AgentSessionsModel`

新建 `G/state/AgentSessionsModel.ts`。放在 GUI 包，因为它依赖 `@tutti-os/agent-activity-core` 的类型，而 `CC/` 不能依赖这个包。

```ts
export class AgentSessionsModel {
  private readonly sessions = observable.map<string, CanonicalAgentSession>([], { deep: false });
  private readonly turnsBySession = observable.map<string, readonly AgentActivityTurn[]>([], { deep: false });
  private readonly interactionsBySession = observable.map<string, readonly AgentActivityInteraction[]>([], { deep: false });
  private readonly operations = observable.map<string, SessionOperationState>([], { deep: false });
  private readonly removed = observable.set<string>();

  // 写：逻辑从 E/sessionEntities.reducer.ts 和 E/sessionLifecycle.reducer.ts 搬过来
  upsertSession(source: AgentActivitySessionInput): void;
  upsertSessions(sources: readonly AgentActivitySessionInput[]): void;
  replaceHistory(session: AgentActivitySessionInput, turns: readonly AgentActivityTurn[]): void;
  removeSession(id: string): void;
  restoreSession(id: string): void;
  setRuntimeAvailability(id: string, availability: SessionRuntimeAvailability): void;

  // 读：逻辑从 E/sessionLifecycle.selectors.ts 搬过来
  session(id: string | null | undefined): CanonicalAgentSession | null;
  isDeleted(id: string | null | undefined): boolean;
  turns(id): readonly AgentActivityTurn[];
  turn(id, turnId): AgentActivityTurn | null;
  activeTurn(id): AgentActivityTurn | null;
  latestTurn(id): AgentActivityTurn | null;
  interactions(id): readonly AgentActivityInteraction[];
  interaction(id, turnId, requestId): AgentActivityInteraction | null;
  pendingInteractions(id): readonly AgentActivityInteraction[];
  hasPendingInteractions(id): boolean;
  runtimeAvailability(id): SessionRuntimeAvailability | null;
  runtimeActivity(id): "idle" | "running";
  submitAvailability(id): { state: "available" | "blocked" } | null;
  consumerSession(id): WorkspaceAgentConsumerSession | null;
  get consumerSessions(): readonly WorkspaceAgentConsumerSession[]; // computed
  get rootSessionIdsWithPendingInteractions(): readonly string[];   // computed
}
```

**必须保留的规则**（从现有 reducer 搬过来，每条写一个测试）：

1. 后写覆盖先写。
2. 内容不变时保留原对象（用 `areJsonLikeValuesEqual`），也不触发通知。
3. 列表里没有的会话不删除。
4. 删除过的会话，再次写入无效，直到 `restoreSession`。
5. 写入时，不在上报中且状态为 pending 的交互要移除。
6. `replaceHistory` 后，这个会话的轮次正好等于传入的列表，已不存在的轮次上的交互一起移除。
7. 会话 trim 后为空的 id 一律忽略。

把 `E/sessionEntities.reducer.test.ts` 的 5 个用例和 `E/sessionLifecycle.selectors.test.ts` 的用例搬成 `G/state/AgentSessionsModel.spec.ts`。

**读方法不能返回新数组。** 没有数据时返回模块级常量；`consumerSession` 用 `computedFn` 或按 id 缓存的 `computed`。

提交信息：`feat(desktop): add a MobX model for conversation sessions and turns`。

### T1.2 host 同时写入新 Model

1. `G/agentActivityRuntime.tsx` 的 `AgentGUIRuntime` 增加 `sessions: AgentSessionsModel`。
2. `H/runtime/create-multica-agent-gui-runtime.ts` 创建实例并挂到 runtime 上。
3. 每处写 engine 的地方同时写 Model：

| 现在的 dispatch | 新增调用 |
| --- | --- |
| `session/snapshotReceived` | `sessions.upsertSessions` |
| `session/upserted` | `sessions.upsertSession` |
| `session/historyAuthoritativeSnapshotReceived`、`session/detailSnapshotReceived` | `sessions.replaceHistory` |
| `session/removed` | `sessions.removeSession` |
| `session/runtimeAvailabilityChanged` | `sessions.setRuntimeAvailability` |

4. 写入要包在现有的 `runInAction` 里，保证和 transcript 在同一次提交。
5. **加一个临时断言测试：** 跑现有 host 测试时，engine 和 Model 对 `session`、`activeTurn`、`latestTurn` 的结果相同。T1.12 删除。

提交信息：`refactor(desktop): write conversation state to AgentSessionsModel too`。

### T1.3 把 Service 的读取方法补齐

engine 快照里的待定意图、队列和读取状态，改成 Service 上的方法。逻辑从 `H/runtime/desktop-queue-engine.ts` 的 `snapshot()` 和对应选择器搬过来。

| 位置 | 新方法 |
| --- | --- |
| `ActivationService` | `latest(id)`、`pending()`、`presentation(id)`、`failedResolution(id, sessionExists, options)` |
| `PromptService` | `queueRecord(id)`、`queuedPrompt(id, promptId)`、`queueError(id)`、`pendingSubmits(id)`、`latestPendingSubmit(id)`、`isSubmitting(id)`、`hasUnconfirmedSubmit(id)`、`latestStopTarget(id)`、`hasStopTarget(id)`、`hasVisibleQueuedSubmit(id)`、`wouldBeVisibleInQueue(id)` |
| `SessionLoadService` | `record(id)`、`isDetailHydrated(id)`、`isDetailLoading(id)`、`canReload(id, session)` |

`CC/` 不能依赖 `@tutti-os/*`，所以这里返回的是 `CC/` 自己的类型。**展示类型的转换放在 `H/runtime/` 的一个新文件 `presented-state.ts`**，从 `desktop-queue-engine.ts` 把 `presentActivation` 这类函数搬过去。

`AgentGUIRuntime` 增加 `activations`、`sessionLoads`，`prompts` 增加上面的方法。类型在 `G/agentActivityRuntime.tsx` 里声明。

提交信息：`refactor(desktop): read pending state straight from the services`。

### T1.4 到 T1.10：逐个文件替换选择器

每个任务一个提交。做法：

1. 打开文件，用第 5.1 节的表替换每个选择器。
2. `useEngineSelector(engine, sel)` 改成 `useModelValue(() => 新读取, [实例, id])`。这些 hook 这时还不是 `observer` 下调用，所以要用 `useModelValue`。
3. `sessionEngine.getSnapshot()` 后面跟选择器的，直接换成新读取。
4. 自定义比较函数：如果新读取返回稳定引用，就删掉比较函数。
5. 调整这个文件的 spec：engine 夹具改成 `AgentSessionsModel` 实例。

| 任务 | 文件 | `useEngineSelector` / `getSnapshot` / `dispatch` |
| --- | --- | --- |
| T1.4 | `C/useAgentGUISessionEngineState.ts` | 21 / 0 / 0 |
| T1.5 | `C/useAgentGUINodeController.ts`、`C/useAgentGUIConversationDetail.ts`、`G/contexts/workspace/presentation/renderer/agentGuiConversationList/useAgentGuiConversationList.ts`、`G/shared/agentGUIConversationSummaryProjection.ts`、`G/shared/workspaceAgentActivityListProjection.ts`、`N/model/agentGuiConversationModel.ts` | 6 / 0 / 0 |
| T1.6 | `C/useAgentGUIActivation.ts`、`C/useAgentGUINewConversationActivation.ts`、`C/useAgentGUIActiveMessages.ts` | 1 / 1 / 3 |
| T1.7 | `C/useAgentGUIConversationSelectionController.ts`、`C/useAgentGUIConversationRouting.ts`、`C/useAgentConversationSelection.ts` | 0 / 9 / 2 |
| T1.8 | `C/useAgentGUIQueueActions.ts`、`C/AgentGUIEngineSettlementController.ts` | 0 / 4 / 3 |
| T1.9 | `C/useAgentGUIProviderHome.ts`、`C/useAgentGUIOperationActions.ts`、`C/useAgentGUIConversationMetadataActions.ts`、`C/useAgentGUIComposerSettingsActions.ts` | 0 / 8 / 1 |
| T1.10 | `G/agentConversationMessageController.ts`、`C/useAgentConversationMessagePaging.ts`、`C/useAgentGUISessionDetailTransport.ts`、`C/useAgentGUIInteractionReadiness.ts`、`C/useAgentGUITargetConnectionState.ts`、`C/useAgentGUIStatus.ts`、`C/useAgentGUISubmitInteractionActions.ts`、`G/shared/agentConversation/AgentObservationGapContext.tsx`、`G/agentSideConversationRuntime.tsx`、`N/composer/quickPrompts/useAgentQuickPromptLibrary.ts` | 见下 |

T1.10 里要注意：`useEngineSelector` **同时用于非 engine 的外部存储**。`AgentGUITargetConnectionController`、`AgentStatusController`、`useAgentQuickPromptLibrary`、`AgentObservationGapContext`、`useAgentGUIInteractionReadiness` 订阅的不是会话 engine，只是借用这个绑定。这几处保留行为，把 `useEngineSelector` 改名为 `useExternalStoreSelector`，放到 `G/shared/useExternalStoreSelector.ts`。

提交信息：`refactor(desktop): read <area> from models instead of the engine`。

### 5.1 选择器替换表

`state` 指 engine 快照。`runtime` 是 `useAgentGUIRuntime()` 或参数里的 `agentActivityRuntime`。

| 选择器 | 替换为 |
| --- | --- |
| `selectEngineSession(state, id)` | `runtime.sessions.session(id)` |
| `selectEngineSessionDeleted(state, id)` | `runtime.sessions.isDeleted(id)` |
| `selectEngineTurn(state, id, t)` | `runtime.sessions.turn(id, t)` |
| `selectEngineTurnsForSession(state, id)` | `runtime.sessions.turns(id)` |
| `selectEngineActiveTurn(state, id)` | `runtime.sessions.activeTurn(id)` |
| `selectEngineLatestTurn(state, id)` | `runtime.sessions.latestTurn(id)` |
| `selectEngineInteraction(state, id, t, r)` | `runtime.sessions.interaction(id, t, r)` |
| `selectEnginePendingInteractions(state, id)` | `runtime.sessions.pendingInteractions(id)` |
| `selectEngineHasPendingInteractions(state, id)` | `runtime.sessions.hasPendingInteractions(id)` |
| `selectEngineSessionRuntimeAvailability(state, id)` | `runtime.sessions.runtimeAvailability(id)` |
| `selectEngineSessionRuntimeActivity(state, id)` | `runtime.sessions.runtimeActivity(id)` |
| `selectEngineSubmitAvailability(state, id)` | `runtime.sessions.submitAvailability(id)` |
| `selectEngineSessionOperationError(state, id)` | 删除，恒为 `null`：host 不写这个字段。`activeEngineError` 去掉这一项 |
| `selectWorkspaceAgentConsumerSession(state, id)` | `runtime.sessions.consumerSession(id)` |
| `selectWorkspaceAgentConsumerSessions(state)` | `runtime.sessions.consumerSessions` |
| `selectRootAgentSessionIdsWithPendingInteractions(state)` | `runtime.sessions.rootSessionIdsWithPendingInteractions` |
| `selectRootAgentActivitySessions` | 从 `consumerSessions` 过滤 root，搬进调用它的文件 |
| `createAgentSessionFamilySnapshotSelector(id)` | 删除。host 的 `childSessions` 恒为 `[]`，改成 `{ rootSession: sessions.consumerSession(id), childSessions: EMPTY, pendingInteractions: sessions.pendingInteractions(id) }`。先 grep 确认没有别处写入子会话 |
| `selectEngineSessionReconcile(state, id)` | `runtime.sessionLoads.record(id)` |
| `selectEngineSessionDetailHydrated(state, id)` | `runtime.sessionLoads.isDetailHydrated(id)` |
| `selectEngineSessionDetailLoading(state, id)` | `runtime.sessionLoads.isDetailLoading(id)` |
| `selectEngineSessionStateHydrated(state, id)` | `runtime.sessions.session(id)?.lifecycleCapabilitiesProjected === true` |
| `selectEngineSessionCanReload(state, id)` | `runtime.sessionLoads.canReload(id, runtime.sessions.session(id))` |
| `selectLatestActivationForSession(state, id)` | `runtime.activations.latest(id)` |
| `selectPendingActivations(state)` | `runtime.activations.pending()` |
| `selectSessionActivationPresentations(state)` | `runtime.activations.presentation(id)`，调用方按 id 取 |
| `selectFailedNewActivationResolution(state, id, o)` | `runtime.activations.failedResolution(id, Boolean(sessions.session(id)), o)` |
| `selectEnginePromptQueue(state, id)` | `runtime.prompts.queueRecord(id)` |
| `selectEngineQueuedPrompt(state, id, p)` | `runtime.prompts.queuedPrompt(id, p)` |
| `selectEnginePromptQueueError(state, id)` | `runtime.prompts.queueError(id)` |
| `selectEngineHasVisibleQueuedSubmit(state, id)` | `runtime.prompts.hasVisibleQueuedSubmit(id)` |
| `selectEngineSubmitWouldBeVisibleInQueue(state, id)` | `runtime.prompts.wouldBeVisibleInQueue(id)` |
| `selectPendingSubmitsForSession(state, id)` | `runtime.prompts.pendingSubmits(id)` |
| `selectLatestPendingSubmitForSession(state, id)` | `runtime.prompts.latestPendingSubmit(id)` |
| `selectSessionIsSubmitting(state, id)` | `runtime.prompts.isSubmitting(id)` |
| `selectSessionHasUnconfirmedSubmit(state, id)` | `runtime.prompts.hasUnconfirmedSubmit(id)` |
| `selectLatestStopTargetSubmitForSession(state, id)` | `runtime.prompts.latestStopTarget(id)` |
| `selectSessionHasPendingSubmitStopTarget(state, id)` | `runtime.prompts.hasStopTarget(id)` |

### 5.2 dispatch 和命令替换表

| 位置 | 现在 | 替换为 |
| --- | --- | --- |
| `G/agentConversationMessageController.ts:178`、`C/useAgentGUIConversationRouting.ts:93`、`C/useAgentGUISessionDetailTransport.ts:91` | `session/reconcileRequested` | `runtime.sessionLoads.request(id, { state, messages })` |
| `C/useAgentGUIActivation.ts:95` | `activation/unactivateRequested` | `runtime.activations.unactivate(id)` |
| `C/useAgentGUIActivation.ts:112` | `activation/failureRecorded` | `runtime.activations.recordFailure(...)` |
| `C/useAgentGUIActivation.ts:129` | `activation/failureCleared` | `runtime.activations.clearFailure(id)` |
| `C/useAgentGUIActivation.ts:77` | `engine.activateSession(...)` | `runtime.activations.request(normalize(input))`，`normalizeActivation` 从 `desktop-queue-engine.ts` 搬到 `presented-state.ts` |
| `C/useAgentGUIQueueActions.ts:57,96` | `submit/canceled` | `runtime.prompts.update(id, "remove", clientSubmitId)` |
| `C/useAgentGUIQueueActions.ts:62,101` | `queue/removed` | `runtime.prompts.update(id, "remove", promptId)` |
| `C/useAgentGUIQueueActions.ts:187` | `queue/sendNowRequested` | `runtime.prompts.update(id, "sendNow", promptId)` |
| `C/useAgentGUIConversationSelectionController.ts:423` | `session/removed` | `runtime.sessions.removeSession(id)`，并调用 `activations.removeSession`、`sessionLoads.remove` |
| `C/useAgentGUIComposerSettingsActions.ts:481` | `activation/settingsPatched` | `runtime.activations.patchSettings(id, settings)` |
| 所有 `engine.submitPrompt(input)` | | `runtime.prompts.submitPrompt(input)`。把 `desktop-queue-engine.ts` 里 `submitPrompt` 的校验（空内容、100 条上限、提交前校验）搬过去，并保留 `commits.flush()` |

**`commits.flush()` 不能丢。** 现在 host 的 engine 包装在 `submitPrompt` 和 `dispatch` 之前调用它。任何“根据轮次状态做决定”的新入口，都要先 flush，否则会读到 30 ms 之前的状态。逐个检查：`prompts.submitPrompt`、`prompts.update`、`activations.request`、`sessionLoads.request`。

### T1.11 host 改用 Model

1. `H/runtime/create-multica-agent-gui-runtime.ts` 的 13 处 `engine.getSnapshot()` 用第 5.1 节替换。`baseEngine.subscribe(() => composer.reconcileSettings())` 改成 `reaction`，监听 `sessions` 的变化。
2. `H/runtime/desktop-queue-engine.ts` 重命名为 `H/runtime/prompt-runtime.ts`。删除 `snapshot()`、engine 包装和 `base`，只保留创建 `PromptService`、`ActivationService`、`SessionLoadService` 的代码。
3. `service.lifecycleChanged()` 原来在 engine 订阅里调用，改成 `reaction`，同样监听 `sessions`。`transcriptCommitted` 保留。
4. 删除 `AgentGUIRuntime.getSessionEngine`。

提交信息：`refactor(desktop): host conversation services without the engine`。

### T1.12 删除 engine

1. 删除双写和 T1.2 的临时断言。
2. 删除 `E/` 里的 reducer、选择器和 `createAgentSessionEngine.ts`，把仍被引用的类型搬到 `activity-core/src/types.ts`。
3. 删除 `G/shared/engine/useEngineSelector.ts` 里的 `useEngineSelector`，`useModelValue` 留下。
4. 删除 `G/shared/testing/createTestAgentSessionEngine.ts`，改为提供 `createTestRuntimeState()`。
5. `E/` 目录应该不存在了。

提交信息：`refactor(desktop): delete the session engine`。

### 步骤 1 完成标准

- [ ] `git grep -E 'useEngineSelector|getSessionEngine|createAgentSessionEngine'` 没有结果
- [ ] 全部检查通过，冒烟测试通过
- [ ] 性能不差于基线
- [ ] 用户在真实应用里验证：新建、发送、停止、回答问题、切换、翻页。**重点看轮次状态有没有回退，“运行中”会不会消失不了**

风险：中。如果列表摘要和已加载的详情同时存在，详情优先（现在 `receiveWorkspace` 已经这样做，不要改丢）。

## 6. 步骤 2：删隐藏的会话栏，定下 `observer` 样板

分支：`claude/conversation-views-2`。

### T2.1 不再渲染 `AgentGUIProviderRail`

1. `N/AgentGUINodeView.tsx` 删除第 314–332 行附近的 `<aside>` 和 `<AgentGUIProviderRail>`，以及只服务它的 props、`providerRailAllPresentation`、`renderProviderRailEmpty`。
2. 删除 `V/AgentGUIProviderRail.tsx`、`V/useAgentGUIProviderRailPreferences.ts`。
3. 用 grep 确认 `V/AgentGUIProviderManagerDialog.tsx`、`V/agentGUIProviderManagerLabels.ts` 还有没有入口，没有就一起删。
4. `G/app/renderer/agentactivity.css` 删除 `agent-gui-node__provider-rail*` 的规则，确认 `.tsx` 里没有引用。
5. `C/useAgentGUINodeController.ts` 和 `viewModel.rail` 暂时不动。`conversations` 还被 14 个 hook 用来查活动会话。

提交信息：`refactor(desktop): remove the collapsed conversation rail`。

### T2.2 第一个 `observer`：`AgentQueuedPromptPanel`

选它是因为依赖少、数据只来自 `PromptService`。

1. `N/AgentQueuedPromptPanel.tsx` 改成 `observer`（第 2.2 节）。
2. 它从 props 接收的队列数据，改成 `runtime.prompts.queueRecord(agentSessionId)`，并过滤可见项。过滤逻辑搬到 `prompts.visibleQueuedPrompts(id)`，从 `C/useAgentGUISessionEngineState.ts` 的 `activeQueuedPrompts` 搬过来。
3. props 只留 `agentSessionId` 和操作回调。
4. 从 `AgentComposer`、`AgentComposerView` 一路删除透传的 `queuedPrompts`。
5. 写测试：队列变化时面板更新，且父组件不重渲染（用 `React.Profiler` 或渲染计数）。

提交信息：`refactor(desktop): let the queued prompt panel read the prompt service`。

### T2.3 写下样板

把这次的写法补进 `AGENTS.md` 的 State Rules 或本文第 2.2 节：组件怎么拿 Service、spec 怎么造 Model、怎么断言渲染次数。**后面的步骤都照这个做。**

## 7. 步骤 3：转录

分支：`claude/conversation-views-3`。风险高，这是最容易出用户可见问题的一步。

**开工前评审：** T3.1 的数据结构和 T3.3 的边界要先出一页设计。下面是建议，不是定稿。

### 现状

```text
TranscriptModel.messages(id)                     整个会话的消息
  → C/useAgentGUIActiveMessages.ts               叠加待发送消息
  → C/useAgentGUIConversationDetail.ts           buildAgentGUIConversationModels
  → G/shared/workspaceAgentTimelineCanonical.ts  builtTurnByFirstItem 缓存
  → T/../projection/agentConversationProjection.ts rowsByTurn 缓存
  → T/../projection/useProjectedAgentConversation.ts
  → V/useAgentGUIDetailModel.tsx
  → V/AgentGUIDetailTimeline.tsx → T/AgentConversationFlow.tsx → T/AgentTranscriptView.tsx → T/AgentTranscriptItemView.tsx
```

每次提交，这条链路从头跑一遍，靠缓存省掉没变的轮次。

### T3.1 建 `ConversationRowsModel`

新建 `G/state/ConversationRowsModel.ts`，每个活动会话一个实例，放在 runtime 上按 id 取：

```ts
class ConversationRowsModel {
  get turnIds(): readonly string[];          // computed，只在轮次增减时变
  row(key: string): AgentTranscriptRowVM | null;
  rowKeys(turnId: string): readonly string[]; // 每轮一个 computed
  get locatorItems(): readonly AgentMessageLocatorItem[];
}
```

要求：

- 每轮一个 `computed`，内部复用现有的纯函数 `buildTurnConversationRows`，不重写。
- 输入是这一轮的消息，要给 `TranscriptModel` 加 `turnMessages(id, turnId)`。`H/runtime/local-transcript.ts` 已经按轮次记录变化。
- 待发送消息（`useAgentGUIActiveMessages` 的叠加）作为单独的“尾部行”，不混进已提交的轮次。
- 删除 `rowsByTurn`、`builtTurnByFirstItem`。

先写测试：只改一轮时，其他轮的 `row` 返回同一个对象，其他轮的 `computed` 不重算。

### T3.2 `AgentTranscriptItemView` 改成按 key 读

1. `T/AgentTranscriptItemView.tsx` 改成 `memo(observer(...))`，props 从 `row` 改成 `rowKey` + `agentSessionId`，自己读 `rowsModel.row(rowKey)`。
2. 它的子组件（表 9.3）继续接收不可变的 row VM，不用改接口，按规则包 `observer`。

### T3.3 `AgentTranscriptView` 只读结构

1. `T/AgentTranscriptView.tsx` 改成 `observer`。
2. props 删除 `conversation`，改成 `agentSessionId`。
3. `displayRows`、`rowKeys`、`turnGroups`、`userMessageLocatorItems` 改读 Model，删除对应的 6 处 `useMemo`。
4. **不改** `useAgentTranscriptVirtualizer`、`useEnteringTranscriptRows` 的行为。给它们传的还是 key 列表。
5. 把现有的 `T/AgentTranscriptView.observer.spec.tsx` 作为回归基础，它已经用真实虚拟列表测 observer 下的行为。

### T3.4 打通上层

1. `T/AgentConversationFlow.tsx`、`V/AgentGUIDetailTimeline.tsx`、`V/AgentGUIConversationTimelinePane.tsx` 改成 `observer`，props 从 `conversation` 改成 `agentSessionId`。
2. `V/useAgentGUIDetailModel.tsx` 删除 `useProjectedAgentConversation`。
3. 从 `useAgentGUIViewAssembly` 和 `viewModel.detail` 删除 `conversation`、`conversationDetail`。
4. 删除 `useProjectedAgentConversation.ts`、`C/useAgentGUIActiveMessages.ts`，以及 `C/useAgentGUIConversationDetail.ts` 的相应部分。

### T3.5 验证

用隔离模式手动过一遍，并截图：

| 场景 | 通过标准 |
| --- | --- |
| 流式时跟随到底 | 始终贴底，不抖动 |
| 流式时向上滚 | 脱离跟随，内容不跳 |
| 向上翻页 | 视野里的内容位置不变 |
| 切换后切回 | 恢复原位置 |
| 展开或折叠工具组 | 不跳，虚拟列表高度正确 |
| 定位条点击 | 跳到对应消息 |
| 发送第一条消息 | 乐观显示，确认后不闪、不重复 |
| 编辑重试、fork 按钮 | 显示和点击正常 |

**不要重构 `V/useAgentGUIDetailScroll.ts`。** 它牵涉跟随到底和翻页，这一步只改它读数据的方式。

### 步骤 3 完成标准

- [ ] 流式时，渲染计数断言只有变化的行重渲染，其他行和输入框都不重渲染
- [ ] 120/s 下 JS 时间明显低于基线
- [ ] 滚动指标不差于基线
- [ ] T3.5 全部通过，用户在真实应用里确认

## 8. 步骤 4：拆 controller；步骤 5：删翻译层

### 步骤 4：拆 `viewModel`

分支：`claude/conversation-views-4a`、`-4b`……每组一个 PR。顺序固定：

| 顺序 | 组 | 字段 | 主要消费方 | 来源 hook |
| --- | --- | --- | --- | --- |
| 1 | `shell` | `nodeId workspaceId workspacePath currentUserId data` | 到处透传 | 改成一个 context：`AgentGUIShellProvider`，不是 Model |
| 2 | `rail` | `selectedAgentTarget agentTargets agentTargetsLoading providerRailMode comingSoonProviders conversationFilter conversations userProjects activeConversation activeConversationId revealRequest isLoadingConversations listError` | `V/AgentGUIDetailPane.tsx`、`V/useAgentGUIDetailModel.tsx`、`V/useAgentGUIWorkspaceReferencePicker.ts`、`V/useAgentGUIExternalRequests.ts` | `useAgentGUIConversationListState`、`useAgentGUIConversationPresentation`、`useAgentGUIProviderCatalogSelection` |
| 3 | `readiness` | `activeLiveState activationError providerReadinessGate` | `N/AgentGUINodeView.tsx`、`V/AgentGUIDetailPane.tsx` | `useAgentGUIActivation`、`useAgentGUISessionPresentation` |
| 4 | `operations` | `forkThroughTurnPendingTurnIds isDeletingConversation isUserProjectMutationPending pendingDeleteConversation pendingDeleteProjectConversations` | `V/AgentGUIDetailPane.tsx`、`V/AgentGUIConversationActionsMenu.tsx`、`T/AgentForkThroughTurnButton.tsx` | `useAgentGUIOperationActions`、`useAgentGUIConversationDeletion` |
| 5 | `detail` | `availability isLoadingMessages isLoadingOlderMessages hasOlderMessages usage hasSentUserMessage avoidGroupingEdits` | `V/useAgentGUIDetailModel.tsx`、`V/useAgentGUIDetailScroll.ts` | `useAgentGUIConversationDetail`、`useAgentGUISessionDetailTransport` |
| 6 | `interaction` | `approvalDisabledReason canAnswerPendingInteractivePromptFromComposer interactivePromptDisabledReason isRespondingApproval isRespondingInteractivePrompt pendingApproval pendingInteractivePrompt sessionChrome inlineNotice` | `N/AgentSessionChrome.tsx`、`T/AgentInteractivePromptSurface.tsx`、`T/AgentInteractiveDecisionPromptSurfaces.tsx`、`V/AgentGUIBottomDockPane.tsx` | `useAgentGUISessionPresentation`、`useAgentGUISubmitInteractionActions` |
| 7 | `composer` | `handoffAgentTargets availableCommands availableSkills draftPrompt draftContent isCreatingConversation isSubmitting isInterrupting isCancelPending hasPendingSubmitStopTarget promptImagesSupported compactSupported gate composerSettings queuedPrompts queueStatus drainingQueuedPromptId` | `N/AgentComposer.tsx`、`N/composer/*`、`V/AgentGUIBottomDockPane.tsx` | `useAgentGUIComposerPresentation`、`useAgentGUIComposerSettingsActions`、`useAgentGUIComposerCapabilities` |

**每个字段的固定做法：**

1. 跑 `git grep -n 'viewModel\.<组>\.<字段>\|<组>\.<字段>' vendor/tutti/packages/agent/gui`，列出所有读取处。
2. 判断字段是哪一种：

   | 类型 | 例子 | 做法 |
   | --- | --- | --- |
   | Model 的直接读取 | `isSubmitting` | 消费组件改 `observer`，直接读 Service |
   | Model 的派生 | `pendingApproval` | 在 Model 或 GUI 的 state 类上加 `computed`，消费组件读它 |
   | 组件临时状态 | 开关、弹窗 | 搬到用它的组件里 |
   | 草稿 | `draftContent` | 留在现有 Zustand 或 context，**不改存储位置** |
   | 不再用 | | 删除 |

3. 从 `N/model/agentGuiNodeTypes.ts` 和 `N/model/useAgentGUIViewModel.ts` 删除字段。
4. 一路删除透传的 props。

每拆完一组，检查 `C/` 里有没有因此没人调用的 hook，有就删。

**`useMemo` 核对：** 碰到的文件，每处 `useMemo` 判断：函数体或依赖读 Model 字段 → 改 `computed`；只算 props → 留下；没有实际收益（比如只拼一个小对象，又没传给 `memo` 组件）→ 删除。

**拆 `composer` 之前先补测试：** `C/useAgentGUISubmitInteractionActions.ts`（722 行）和 `C/useAgentGUIComposerSettingsActions.ts`（616 行）牵涉草稿保存和恢复。先确认下面每种情况都有测试，没有就先补，再拆：发送成功清空草稿、发送失败恢复草稿、新建失败恢复首页草稿、切换会话保留草稿、回答问题的草稿、设置在会话创建前修改。

**完成标准：** `git grep -E 'useAgentGUINodeController|AgentGUINodeViewModel'` 没有结果；`AgentGUINode.tsx` 只负责布局和 provider。

### 步骤 5：删翻译层

分支：`claude/conversation-views-5a`、`-5b`……

#### T5.1 `AgentGUIRuntime` 逐个成员处理

| 成员 | GUI 调用方 | 做法 |
| --- | --- | --- |
| `conversationActivityViewEnabled`、`deleteSession`、`listAgentGeneratedFiles`、`load`、`subscribeSessionEvents` | 没搜到 | 再 grep 一次确认（host 里也搜），没有就删 |
| `getSessionEngine` | 步骤 1 已删 | |
| `mutations`、`composer`、`transcript`、`prompts`、`sessions`、`activations`、`sessionLoads` | 多处 | 留下，改名 `useConversationServices()` |
| `workspaceLoad`、`loadOlderSessionMessages`、`ensureSessionSynchronized` | 列表状态、翻页 | 改成调用 `ConversationService` |
| `origin`、`projectPathIsRemote`、`promptContentUploadSupport`、`devDiagnosticConsoleSink` | 少量 | 改成常量或 context |
| `getSession`、`getSnapshot`、`subscribe`、`getComposerOptions`、`listSessionMessages` | 少量 | 改读 Model |
| `uploadPromptContent`、`stagePastedText`、`promptAttachmentsRemoved` | 输入框附件 | 搬到新的 `AttachmentService` |
| `readSessionAttachment`、`readSessionAttachmentSource`、`readPromptAsset` | 图片和菜单 | 搬到 `AttachmentService` |
| `setCollaborationAdoption` | `T/AgentCollaborationRow.tsx` | 确认协作功能在桌面端是否可用；不可用就删 |
| `trackSettingsProjectChange`、`trackDraftComposerSettingsChange`、`reportDiagnostic` | 诊断 | 留成一个 `diagnostics` 对象 |

全部处理完，删除 `AgentGUIRuntime` 类型和 `G/agentActivityRuntime.tsx`。

#### T5.2 RPC

`H/protocol.ts` 的 `AgentGuiRpcMethods` 按下表处理。**跨 Electron 进程的留下**，其他的改成直接调用：

| 类别 | 方法 | 做法 |
| --- | --- | --- |
| 宿主能力 | `host.showMessage`、`host.openSide`、`host.openResource`、`host.openInternalResource`、`host.selectSession`、`host.promptApplied`、`host.showFileContextMenu` | 留下，改名为 `hostApi` |
| 本地会话 | `workspace.load`、`session.create`、`session.rename`、`session.pin`、`session.fork`、`session.compact`、`queue.*`、`interaction.respond` | 改成直接调用 `ConversationService`、`ConversationMutationService` |
| 附件 | `attachment.*`、`image.url` | 进 `AttachmentService` |
| 其他 | `models.list`、`usage.read`、`context.tasks`、`storage.*` | 逐个看，`storage.*` 换成 `StorageAdapter` |

`H/in-process-client.ts` 删除或只留 `hostApi`。

#### T5.3 投影

- `H/runtime/v2-projection.ts`、`H/runtime/projection.ts`：改成 `AgentSessionsModel` 从 `ConversationModel` 和 `ConversationListModel` 派生，用 `computed`，host 不再写入。这时 `AgentSessionsModel` 的写方法只剩 `removeSession`、`restoreSession`。
- `H/runtime/local-transcript.ts`：逻辑进 `ConversationRowsModel` 的输入。

#### T5.4 ledger（开工前评审）

`H/runtime/ledger.ts` 做四件事：GUI ID 和服务端 ID 的映射、消息版本号、创建中的会话、保存的待发送队列。

**这一项要先出设计，停下来问。** 要回答：

1. 新建会话的 ID 由谁生成？客户端生成并传给 daemon 最简单，但要改 daemon 的创建接口，并要有幂等处理。
2. 已有用户 `localStorage` 里的 `runtime-ledger-v1` 怎么迁移？里面的映射丢了，打开中的标签可能找不到会话。
3. 消息版本号有没有别的消费者？（`ledger.version` 只有一处调用，但 `message.version` 被翻页和列表用。）
4. 保存的待发送队列搬到哪？`PromptService` 的 `save` 选项已经抽象了这个，大概率只是换存储。

#### T5.5 清理

| 内容 | 文件 |
| --- | --- |
| 不生效的 Side 会话界面 | `V/AgentGUISideConversationPane.tsx`、`V/AgentGUISideConversationPresentationPublisher.tsx`、`V/useAgentGUIDetailSideConversation.ts`、`V/useAgentGUIDetailSideChrome.tsx`、`G/agentSideConversationRuntime.tsx`、`G/agentSideConversationViewProjection.ts`、`activity-core/src/ephemeralConversationProjector.ts`（host 不传 runtime，先确认 `side-entry.tsx` 走的是另一条路） |
| 死代码 | `slashCommandGoal*`、`markSessionUnread`、`moveProject`，用 grep 找 |
| 启动时多余的请求 | `side_workspace_sync`：内容是默认值时不发，看 `apps/desktop/src/renderer/src/platform/side-workspace-bridge.tsx` |
| 文档 | `local-conversation-mobx-models.md` 阶段 6、7 标记完成；`AGENTS.md` 去掉 Tutti 相关的过渡说明 |

**完成标准：** `git grep -E 'AgentGUIRuntime|AgentGuiRpcClient|RuntimeLedger|projectSession'` 没有结果。

## 9. 组件清单

### 9.0 怎么看这张表

| 动作 | 含义 |
| --- | --- |
| **读** | 改成 `observer`，自己从 Service 或 Model 读数据，删除对应 props |
| **包** | 只包 `observer`，接口不变。它接收的是不可变 VM 或 Model 实例；统一包裹，避免以后传入 Model 时漏掉 |
| **删** | 确认没有入口后删除 |
| **不动** | 没有业务数据的纯 UI，不包 |

括号里是行数。“步骤”是做这件事的步骤。**同一步骤内，先改父组件还是子组件都可以，但一个提交里 props 要改通。**

### 9.1 外壳和详情面板

| 组件 | 步骤 | 动作 | 说明 |
| --- | --- | --- | --- |
| `N/AgentGUINode.tsx` (613) | 4 | 读 | 最后只留布局和 provider；有 4 处 `useMemo` |
| `N/AgentGUINodeView.tsx` (516) | 2、4 | 读 | 步骤 2 删会话栏；步骤 4 删 `viewModel` |
| `V/AgentGUIDetailPane.tsx` (706) | 4 | 读 | `viewModel` 和 `actions` 最大的消费方，106 处 |
| `V/useAgentGUIDetailModel.tsx` (623) | 3、4 | 读 | hook，调用方是 `observer` 后直接读；47 处引用 `viewModel`，4 处 `useMemo` |
| `V/AgentGUIDetailTimeline.tsx` (233) | 3 | 读 | |
| `V/AgentGUIConversationTimelinePane.tsx` (132) | 3 | 读 | |
| `V/AgentGUIBottomDockPane.tsx` (182) | 4 | 读 | |
| `V/AgentGUIEmptyState.tsx` (744) | 4 | 包 | |
| `V/AgentGUIConversationActionsMenu.tsx` (604) | 4 | 读 | |
| `V/AgentGUIRenameConversationDialog.tsx` (195) | 4 | 包 | |
| `V/AgentGUIAccountConfig.tsx` (296) | 4 | 包 | |
| `V/AgentGUIEditRetryStatus.tsx` (63) | 3 | 读 | |
| `V/AgentGUITextSelectionActions.tsx` (96) | 3 | 包 | |
| `V/AgentGUIReferencePickerSurface.tsx` (92) | 4 | 包 | |
| `V/AgentTargetSetupGate.tsx` (587) | 4 | 包 | |
| `V/AgentTargetSetupRoot.tsx` (98) | 4 | 包 | |
| `V/AgentGUIProviderRail.tsx` (778) | 2 | 删 | 桌面端隐藏 |
| `V/AgentGUIProviderManagerDialog.tsx` (696) | 2 | 删 | 先确认没有入口 |
| `V/AgentGUISideConversationPane.tsx` (246) | 5 | 删 | |
| `V/AgentGUISideConversationPresentationPublisher.tsx` (53) | 5 | 删 | |
| `V/useAgentGUIDetailSideChrome.tsx` (292) | 5 | 删 | |
| `V/AgentGUIAgentTargetName.tsx` (47)、`V/AgentGUIEmptyHeroCarouselStage.tsx` (180) | | 不动 | |
| `N/AgentGUIHeroAgentCarousel.tsx` (714)、`N/AgentGUIVinylPlayer.tsx` (98)、`N/AgentGUIOwnerAvatar.tsx` (34) | | 不动 | |
| `N/AgentGUIMentionServiceBoundary.tsx` (46) | | 不动 | |
| `N/AgentHomeSuggestions.tsx` (246) | 4 | 包 | |
| `N/AgentSessionChrome.tsx` (366) | 4 | 读 | `interaction.sessionChrome` |
| `N/AgentFullAccessWarningDialog.tsx` (125)、`N/AgentFullAccessRestoredWarning.tsx` (88) | | 不动 | |
| `N/AgentProbeUsageFreshness.tsx` (163)、`N/AgentUsageMeter.tsx` (68) | 4 | 包 | |

### 9.2 输入框

| 组件 | 步骤 | 动作 | 说明 |
| --- | --- | --- | --- |
| `N/AgentComposerRegion.tsx` (36) | 4 | 包 | |
| `N/AgentComposer.tsx` (746) | 4 | 读 | `viewModel.composer` 的主要消费方 |
| `N/composer/AgentComposerView.tsx` (759) | 4 | 读 | |
| `N/composer/AgentComposerChrome.tsx` (435) | 4 | 读 | |
| `N/composer/ComposerFooter.tsx` (535) | 4 | 读 | 发送和停止按钮的状态 |
| `N/composer/useComposerPresentation.tsx` (410) | 4 | 读 | hook |
| `N/composer/ComposerPrimaryCapabilityControl.tsx` (113) | 4 | 包 | |
| `N/composer/ComposerDraftAttachments.tsx` (196) | 4 | 包 | 上传状态读 `AttachmentService`（步骤 5） |
| `N/composer/AgentComposerDraftPreview.tsx` (125) | 4 | 包 | |
| `N/composer/AgentHandoffMenu.tsx` (241) | 4 | 包 | |
| `N/composer/AgentSessionLaunchModeSelect.tsx` (74) | 4 | 包 | |
| `N/composerFloatingMenu/ComposerFloatingMenuSurface.tsx` (382) | | 不动 | 纯定位 |
| `N/AgentQueuedPromptPanel.tsx` (621) | 2 | 读 | 第一个样板 |
| `N/AgentComposerModelReasoningDropdown.tsx` (740) | 4 | 读 | 读 `ComposerService` |
| `N/AgentComposerSettingsMenus.tsx` (326) | 4 | 读 | |
| `N/AgentComposerProjectMenu.tsx` (125) | 4 | 包 | |
| `N/AgentSlashCommandPalette.tsx` (439)、`N/AgentSlashStatusPanel.tsx` (218) | 4 | 包 | |
| `N/AgentFileMentionPalette.tsx` (740)、`N/AgentReviewPickerPanel.tsx` (337) | 4 | 包 | |
| `N/AgentReferenceProvenanceFilterControl.tsx` (33) | | 不动 | |
| `N/agentRichText/AgentRichTextEditor.tsx` (791)、`AgentRichTextEditorSurface.tsx` (148)、`AgentMentionNodeView.tsx` (755) | | 不动 | 编辑器内部状态，不读 Model。**不要包**，避免干扰编辑器的受控更新 |

### 9.3 转录

| 组件 | 步骤 | 动作 | 说明 |
| --- | --- | --- | --- |
| `T/AgentConversationFlow.tsx` (137) | 3 | 读 | 已有 `memo`，改 `memo(observer())` |
| `T/AgentTranscriptView.tsx` (796) | 3 | 读 | 6 处 `useMemo` |
| `T/AgentTranscriptItemView.tsx` (138) | 3 | 读 | 已有 `memo`；重渲染边界，最关键 |
| `T/useAgentTranscriptTurnAttachments.tsx` (181) | 3 | 读 | hook |
| `T/AgentMessageLocatorRail.tsx` (560) | 3 | 读 | items 读 `locatorItems` |
| `T/AgentTurnWorkSection.tsx` (267) | 3 | 读 | 运行中计时 |
| `T/AgentProcessingRow.tsx` (47) | 3 | 读 | |
| `T/AgentForkThroughTurnButton.tsx` (73) | 4 | 读 | `operations.forkThroughTurnPendingTurnIds` |
| `T/AgentUserMessageEditRetry.tsx` (74) | 3 | 读 | |
| `T/AgentInteractivePromptSurface.tsx` (218) | 4 | 读 | |
| `T/AgentInteractiveDecisionPromptSurfaces.tsx` (626) | 4 | 读 | 4 处 `useMemo` |
| `T/AgentAskUserPromptSurface.tsx` (351)、`T/AgentAskUserQuestionCard.tsx` (53) | 4 | 包 | |
| `T/AgentMessageBlock.tsx` (473) | 3 | 包 | 接收 row VM |
| `T/AgentToolGroupRow.tsx` (200) | 3 | 包 | 已有 `memo` |
| `T/AgentTurnSummaryRow.tsx` (734) | 3 | 包 | 3 处 `useMemo`，核对 |
| `T/AgentGeneratedImageRow.tsx` (25)、`T/AgentGeneratedImagePreview.tsx` (99) | 3 | 包 | |
| `T/AgentSystemNoticeMessage.tsx` (250)、`T/AgentVisibleErrorMessage.tsx` (210) | 3 | 包 | |
| `T/AgentCollaborationRow.tsx` (355) | 5 | 读或删 | 取决于协作功能 |
| `T/AgentSubAgentCards.tsx` (340) | 3 | 包 | |
| `T/AgentMessageActions.tsx` (147)、`T/AgentMessageDetailsDisclosure.tsx` (53)、`T/AgentThinkingDisclosure.tsx` (38) | 3 | 包 | |
| `T/AgentMessageImages.tsx` (536) | 5 | 包 | 附件读取搬到 `AttachmentService` |
| `T/AgentToolCallCard.tsx` (73)、`T/AgentToolCallHeader.tsx` (299) | 3 | 包 | |
| `T/AgentPlanCard.tsx` (123)、`T/AgentTaskCallCard.tsx` (64)、`T/AgentTaskStepList.tsx` (112) | 3 | 包 | |
| `T/AgentEnterPlanModeCard.tsx` (29)、`T/AgentExitPlanModeCard.tsx` (29) | 3 | 包 | 计划模式已关闭，能删就删 |
| `T/AgentTuttiModeCheckpointWakeCard.tsx` (117)、`T/AgentTuttiPlanIssueLinkCard.tsx` (63) | 5 | 删 | 先确认没有入口 |
| `T/AgentConversationParticipant.tsx` (90)、`T/AgentSelectedTextChip.tsx` (38)、`T/RawTimelineJsonDisclosure.tsx` (57) | 3 | 包 | |
| `T/AgentConversationClock.tsx` (107)、`T/AgentTurnDisclosureContext.tsx` (51) | | 不动 | 自带外部存储，不读 Model |
| `T/CollapsibleReveal.tsx` (251)、`T/AgentTranscriptSkeleton.tsx` (33)、`T/ConversationImageContextMenu.tsx` (72)、`T/interactivePromptPresentation.tsx` (331) | | 不动 | 纯 UI |

Markdown 组件（`G/shared/AgentMessageMarkdown.tsx`）**不动**。它接收字符串，内部有逐帧显现和分块缓存，这轮刚优化过。

### 9.4 controller 和 view 的 hook 去向

这些不是组件，不包 `observer`。

| hook 或文件 | 行数 | 去向 |
| --- | --- | --- |
| `C/useAgentGUINodeController.ts` | 751 | 步骤 4 删除 |
| `C/useAgentGUIViewAssembly.ts`、`N/model/useAgentGUIViewModel.ts` | 328 | 步骤 4 删除 |
| `C/useAgentGUISessionEngineState.ts` | 307 | 步骤 1 改读 Model，步骤 4 删除 |
| `C/useAgentGUISessionPresentation.ts` | 673 | 步骤 4：派生值搬到 `G/state/` 的 `computed` |
| `C/useAgentGUIConversationDetail.ts` | 349 | 步骤 3、4 删除 |
| `C/useAgentGUIActiveMessages.ts` | 103 | 步骤 3 删除 |
| `C/useAgentGUISessionDetailTransport.ts`、`C/useAgentConversationMessagePaging.ts`、`G/agentConversationMessageController.ts` | 153、287、约 300 | 步骤 5：翻页和读取进 `ConversationService` |
| `C/useAgentGUIConversationListState.ts`、`G/contexts/.../useAgentGuiConversationList.ts` | 78、约 250 | 步骤 4：只保留查活动会话需要的部分 |
| `C/useAgentGUIConversationPresentation.ts` | 257 | 步骤 4 |
| `C/useAgentGUIConversationSelectionController.ts`、`C/useAgentConversationSelection.ts`、`C/useAgentGUIConversationRouting.ts` | 438、187、186 | 步骤 4：合成一个 `ConversationSelectionService`，放在 `G/state/` |
| `C/useAgentGUINewConversationActivation.ts`、`C/useAgentGUIActivation.ts`、`C/agentGuiNewConversationRequest.ts` | 415、177、78 | 步骤 4：请求组装留纯函数，状态读 `ActivationService` |
| `C/useAgentGUISubmitInteractionActions.ts` | 722 | 步骤 4：纯函数留下，其余进组件的事件处理 |
| `C/useAgentGUIComposerSettingsActions.ts`、`C/useAgentGUIComposerOptionsSync.ts`、`C/useAgentGUIComposerCapabilities.ts`、`C/useComposerModelChoiceHistory.ts` | 616、325、172、301 | 步骤 4：进 `ComposerService` 或读它 |
| `C/useAgentGUIComposerPresentation.ts`、`C/agentGuiController.composerPresentation.ts`、`C/agentGuiController.composerHelpers.ts` | 422、414、613 | 步骤 4：纯函数留下，读取改到组件 |
| `C/useAgentGUIProviderHome.ts`、`C/useAgentGUIConversationHome.ts`、`C/useAgentGUIProviderCatalogSelection.ts` | 454、401、276 | 步骤 4；后者 10 处 `useMemo`，核对 |
| `C/useAgentGUIStatus.ts`、`C/AgentStatusController.ts` | 337、401 | 步骤 4：核对状态面板是否还有入口 |
| `C/useAgentGUIOperationActions.ts`、`C/useAgentGUIConversationDeletion.ts`、`C/useAgentGUIConversationMetadataActions.ts` | 143、205、395 | 步骤 4：调 `ConversationMutationService` |
| `C/useAgentGUIQueueActions.ts`、`C/AgentGUIEngineSettlementController.ts` | 198、232 | 步骤 4：前者进组件事件；后者改成 `reaction`，放进 `G/state/` |
| `C/useAgentGUIContinueConversation.ts`、`C/useAgentGUIComposerAppendRequest.ts`、`C/useAgentGUIEditRetryController.ts` | 145、129、69 | 步骤 4 |
| `C/useAgentGUITargetConnectionState.ts`、`C/AgentGUITargetConnectionController.ts`、`C/useAgentGUIInteractionReadiness.ts` | 65、107、66 | 步骤 4：确认 host 有没有传来源，没有就删 |
| `C/useAgentGUILocalState.ts`、`C/useAgentGUIControllerRefs.ts`、`C/useAgentGUIControllerActions.ts` | 148、175、176 | 步骤 4：状态搬到用它的组件，然后删除 |
| `C/agentGuiController.stableHelpers.ts`、`.draftMessageHelpers.ts`、`.providerHelpers.ts`、`.conversationHelpers.ts`、`.errors.ts`、`.reporting.ts`、`.promptHelpers.ts`、`.sessionHelpers.ts`、`.interactiveHelpers.ts` | 共约 2800 | 纯函数，留下。每步结束删掉没人用的导出 |
| `V/useAgentGUIDetailScroll.ts` | 795 | 只改读数据的方式，不重构 |
| `V/useAgentGUIDetailSideConversation.ts` | 538 | 步骤 5 删除 |
| `V/useAgentGUIWorkspaceReferencePicker.ts`、`V/useAgentGUIExternalRequests.ts`、`V/useAgentGUIComposerInputHistoryProps.ts` | 226、150、71 | 步骤 4：改读 Model |

## 10. 每一步结束时的检查单

- [ ] 第 2.5 节的命令全部通过，并且看过 `Tests` 那一行的总数
- [ ] 冒烟测试通过
- [ ] 隔离模式下跑性能脚本，和第 1.4 节对比；回退要写明原因
- [ ] 正式版的 `launch.json`、`config.json`、守护进程 pid 没变
- [ ] PR 描述写清楚行为变化、实际跑过什么、没跑什么
- [ ] 第 1.2 节的数字更新
- [ ] 请用户在真实应用里验证后再开下一步

## 11. 风险

| 风险 | 应对 |
| --- | --- |
| 在 `useMemo` 或 effect 里读 Model 字段，界面不更新且测试查不出来 | 第 2.2 节的规则；步骤 2 结束时评估能否加 lint 规则 |
| `useModelValue` 返回不稳定的引用，无限重渲染 | 规则 3；冒烟测试 |
| 新入口忘了 `commits.flush()`，按旧状态做决定 | 第 5.2 节逐个检查 |
| 合成数据测不出真实时序 | 步骤之间在真实应用里验证 |
| 步骤 3 破坏滚动 | 不重构滚动代码；T3.5 逐项验证 |
| 量大，出问题难定位 | 每步一个 PR，能单独回退 |
| 影响正式版 | 第 2.1 节；隔离模式启动后自动校验 |
| 拆 `composer` 丢草稿 | 先补测试再拆 |
| vendor 代码和 Tutti 上游差异越来越大 | GUI 已确定归我们所有，可以自由修改；之后要合并上游改动会很难，需要时单独评估 |
