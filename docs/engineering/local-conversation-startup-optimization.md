# 本机会话启动优化方案

状态：第 1 步（两期和手机回传）、第 2 步、第 3 步、第 4 步、第 5 步和第 6 步的一部分已实现。待做：第 3 步的真实 CLI 联调；桌面作为发起端的回传，见“手机回传的实现说明”；真机验证，见文末。

依据版本（2026-11-02）：代码 `6ed08c306`、Codex CLI 0.164.0、Claude Agent SDK 0.3.310、Claude Code 2.1.316。

## 目标

让每次发送都尽量走最短路径，只有确实需要时才做准备工作。这是守护进程提供的基础能力，桌面、手机等所有本机会话入口共用，不是某条链路的专项优化。

方案按两种场景设计，执行端的路径相同，差异在到达执行端之前的传输。P2P 又分两条路径，行为并不相同：

```text
本机 → 本机  ：界面 ─ 本地连接 ─ 守护进程 ─ 模型
桌面 → P2P  ：界面 ─ 本机守护进程代理 ─ 隧道 ─ 执行端守护进程 ─ 模型
手机 → P2P  ：界面 ─ 内嵌 mobilelink 代理 ─ 隧道 ─ 执行端守护进程 ─ 模型
```

| | 桌面 P2P | 手机 P2P |
|---|---|---|
| 请求超时 | 120 秒 | 30 秒 |
| 进入后台 | 不影响 | 停止整个 P2P 服务 |
| 迁移到直连 | 有 | 没有 |
| 网络切换感知 | `NetworkChanged` + 注册表事件 | 没有，靠 15 秒刷新 |
| 局域网发现 | mDNS + 登记地址 | 只有登记地址 |

本方案的“手机”目前指 iOS，原生模块只支持 Apple 平台。

上表是方案起草时的现状。第 6 步之后手机的请求超时、后台行为、迁移直连和网络切换都已改变，以第 6 步为准。

可以优化的是应用自身的启动、恢复和重复准备，以及 P2P 的连接和传输。`turn/start` 之后的模型排队和生成不会变快，但思考过程要实时显示，让用户更早看到内容。

## 现状

| 环节 | Codex | Claude Code |
|---|---|---|
| 进程 | 每个会话一个 `app-server`，首次发送时启动 | 每个会话一个 Node adapter 和 Claude Code，首次发送时启动 |
| 同一进程连续发送 | 每轮执行 `thread/resume`（已加载时约 2–3ms），再 `turn/start` | 长驻 Query，不重复恢复 |
| 切换设置 | 模型和权限对已加载线程不生效（见第 2 步） | 通过 `set_model` 等在线下发 |
| 进程回收 | 无，保留到守护进程退出 | 无，保留到守护进程退出 |

相关代码：

- `server/internal/localconversation/service/codex_runner.go`：`Send`、`ensureServer`、`thread`
- `server/internal/localconversation/service/claude_runner.go`：`Send`、`ensureAdapter`、`ApplySettings`
- `server/internal/localconversation/service/service.go`：`runners`、`keepRunner`
- `apps/desktop/src/claude-host/adapter.ts`：`start`、`supportedModels`

第 1 步第一期完成后，守护进程写入分阶段数据，契约在 `packages/core/local-conversation/timing.ts`，用法见 `apps/desktop/docs/local-turn-timing.md`。

### 基线

从用户消息落库到首个模型事件，本机守护进程时期的数据：

| 场景 | 样本数 | p50 | p90 |
|---|---:|---:|---:|
| Codex 首轮（默认模型） | 17 | 7.8s | 11.9s |
| Codex 后续轮（默认模型） | 33 | 5.9s | 14.5s |
| Codex 后续轮（显式模型） | 12 | 3.4s | 4.8s |
| Claude 首轮 | 7 | 2.5s | 4.5s |
| Claude 后续轮 | 3 | 2.0s | 2.3s |

这组数据样本不多，没有区分冷热启动，也不能区分手机和桌面发起的消息；首个事件也不一定是正文。因此方案不设具体提速目标，第 1 步完成后再定。

迁移前的 Electron 链路有分阶段数据：Codex 完成 `turn/start` 的 p50 为 2.6s、p90 为 10.7s，首个输出的 p50 为 11.4s。这说明中位数上大部分等待在提交之后，长尾中准备阶段可能较重。架构已变化，这组数据只能看比例。

其他本机数据：

- 本机直连时，事件落库到界面显示的 p50 约 70ms（32 个样本）。P2P 尚未测到。
- Multica Codex 记录中，从轮次开始到首个模型条目 p50 7.7s、p90 17.1s（39 个样本）。后续轮缓存命中率约 99%，所以这段时间主要不在提示词处理上。
- 这 39 轮中有 15 轮首个条目是思考，p50 8.6s、p90 22.5s。

## 整体流程

```text
发送消息 → 保存并显示 → 会话已就绪？
                          ├─ 是 → 直接提交 → 模型生成 → 回传
                          └─ 否 → 准备（同一会话只跑一次）→ 提交

预热请求 ───────────────────→ 同一个准备任务
切换设置、进程退出、线程关闭、回收 → 就绪失效
分段计时覆盖全程
```

## 实施步骤

全部步骤都在本方案范围内，Claude 的 `prewarm()` 和老会话预热不延后。

### 前置修复

这些问题现在就存在，预热和迁移直连会让它们更容易发生，所以最先修。

| 问题 | 后果 | 修复 |
|---|---|---|
| `Send` 绑定请求上下文直到 `turn/start` | 断流或手机 30 秒超时可能取消请求：消息已落库、轮次失败，同一 `request_id` 重试不会重跑 | 消息落库后，`Send` 脱离请求上下文继续执行 |
| Codex 把 `context.Canceled` 记为 `provider_error` | 模型可能已收到请求，却被记成失败 | 提交阶段取消按 `outcome_unknown` 处理，与 Claude 一致 |
| Claude 空闲时 `query_ended` 被丢弃，adapter 继续存活 | 此后发送持续失败，直到守护进程重启 | 没有运行中轮次时收到 Query 结束，就关闭 adapter，下次冷启动 |
| Claude 准备期间的取消丢失 | 用户点了停止，整轮照跑并消耗额度 | 准备结束后，两个 runner 都先检查取消请求，已取消则不提交 |
| `Normalizer.Close` 不写思考内容 | 结束事件缺少 schema 要求的 `text` | 第 5 步一并修复 |
| Claude `start` 传 `bypassPermissions`，但没传 SDK 要求配套的 `allowDangerouslySkipPermissions` | `full-access` 可能与 SDK 约定不符 | 按 SDK 文档，`start` 和 `prewarm` 都传 `allowDangerouslySkipPermissions: true`。这样会话中途切到 `full-access` 也能生效，实际权限仍由 `permissionMode` 决定 |

断流是否一定取消 `r.Context()` 要用测试确认。发送改为脱离上下文后，这个问题就不再影响结果。

### 第 1 步：全链路性能 Mark

链路上每个节点都打 mark，覆盖三条路径，完整清单见“性能 Mark 清单”。清单不删减，分两期落地。改动手机端前先读 `apps/mobile/AGENTS.md`。

**第一期：**

- **记录 v2：** 增加设备级记录（不需要 `conversationId`），维度和原因使用封闭枚举，不写自由文本。schema 由 Go、TypeScript 和手机共用，并用样本测试。字段沿用现有记录的 camelCase（如 `clientKind`、`turnKind`）。旧阶段名保留可读，并提供新旧映射。
- **守护进程存储：** 写入独立文件 `<ProfileDir>/local-conversations/diagnostics/daemon-timing.jsonl`，自带轮转和有界的非阻塞队列。不与 Electron 共写 `turn-timing.jsonl`。队列丢弃时要标记数据不完整。桌面运行时的 `<ProfileDir>` 是 `~/.multica/profiles/desktop-runtime`，与 `daemon.log` 不在同一目录，所以 manifest 单独带这个路径。
- **导出：** 桌面端导出和独立 MCP 都合并这个文件。`developer_logs.export` 只路由给执行端的本地客户端，没有时返回明确错误。发起端和执行端的数据按 `request_id` 合并。
- **单轮瀑布图：** 导出里提供单轮瀑布，只有分阶段百分位满足不了“任意一条慢消息都能分清”。
- **发送主路径 mark：** 约 20 个，覆盖发起、落库、等待准备、进程、线程、提交、首个模型输出和回传。

**第二期：** P2P 的服务、设备发现、建连、打洞、隧道、WebSocket 和迁移，以及手机回传。

**手机回传：**

- 手机本地使用有界环形缓冲，切后台后保留。
- `mobilelink` 增加回环诊断路由，让 JS 读取 Go 层的 mark。
- 通过 `/hello` 的能力声明协商上传，重连后批量上传并确认。
- 执行端按设备限流并校验 scope，记录收到时间，上报数据单独存放，避免挤掉本机数据。

完成标准：任意一条慢消息都能分清时间花在哪一段，并能区分路径、连接方式和冷热启动。

#### 第二期的实现说明

**存放：** 连接和隧道记录写入 `<ProfileDir>/local-conversations/diagnostics/device-timing.jsonl`（2 MiB，保留一个旧文件），手机上报写入同目录的 `timing-reports.jsonl`（5 MiB，保留一个旧文件），都不与 `daemon-timing.jsonl` 共用轮转。

**只记状态变化：**

- `peer.connect`：一次连接任务只记开始、第一次失败和最终成功（`count` 是尝试次数），设备一直连不上时不会反复写。开始时的 `addrType` 是可用地址里最直接的一种，结束时是实际连接的类型；开始是 `lan`、结束是 `relay`，就是本地网络权限被拒的样子。
- `registry.fetch`：只记转为失败、恢复和超过 5 秒的读取。
- `relay.refused`：预约被拒取 libp2p 的状态码；线路被拒 libp2p 只给文本，匹配状态名后丢弃文本，并只在一次连接任务的第一次失败时记录。
- `relay.connected`：连上中继节点本身。设备经中继连通记为 `connection.relayed`。
- `ws.retry`：只在退避时长变化时记录。
- 手机的上报请求不产生 `proxy.*` 记录，否则每次上报都会给下一次制造数据。

**按流记录：** `tunnel.*` 和发起端的 `proxy.*` 从实际的流读取连接类型；执行端由 device-link 服务的 `ConnContext` 取得。`daemon.request_received` 和 `ws.first_output_write` 的 `connection` 因此不再是 `unknown`。`connection.*` 本身就是设备级状态。

**关联：** 客户端发送时带 `X-Multica-Request-ID`，代理记录靠它进入这一轮的瀑布图，旧守护进程会忽略。只有带这个头的请求才写 `link.request_*` 和 `link.response_*`。

**迁移：** `migrate.start`、`migrate.stream_closed`、`migrate.reconnected` 由发起端的隧道记录。`migrate.resubscribed` 由执行端记录：同一客户端的中继 WebSocket 关闭后 2 分钟内，在直连流上订阅成功。发起端的隧道看不到 WebSocket 里的订阅，客户端也不区分迁移和普通断线。

**与清单的差异：**

- `holepunch.start` 不带角色。libp2p 的事件追踪器不区分双方，角色只在结束时通过指标追踪器给出，所以 `holepunch.success` 和 `holepunch.failed` 带角色。这两个结束记录的设备和耗时取自最近一次事件，多台设备同时打洞时可能对错设备。
- `provider.first_thinking` 没有写入：Claude 的思考和 Codex 一样记为 `provider.first_reasoning`，瀑布图把两者都算作首个输出。
- `snapshot.rendered` 和 `client.first_commit` 只有手机写入。桌面的快照记录从 `snapshot.request` 到 `snapshot.parsed`。桌面的 `client.pending_shown` 用下一帧近似。
- 字段沿用记录的 camelCase，“收到时间”的字段名是 `receivedAt`。
- 新增 mark `turn.output_summary`（`count`、`bytes`、`maxGapMs`），在这一轮最后一批事件提交时写一条。

#### 手机回传的实现说明

**环形缓冲有两层：**

- Go 层在 `timing.Ring`（2000 条），切到后台和 `Close` 时存到 `<StoragePath>/<账号>.timing.json`，下次启动时恢复，因为 iOS 可能直接结束后台进程，而此时 JS 读不了。
- JS 层在 `apps/mobile/data/ai-sessions/timing.ts`（2000 条），保存在内存中，切后台和重建连接后都保留。手机没有现成的文件存储依赖，所以进程被杀时 JS 层未上报的记录会丢失，Go 层的不会。超出容量的部分以 `timing.dropped` 上报。

**读取：** `GET /diagnostics?after=<游标>` 走现有的回环凭据。游标不删除数据，读取可以重复；游标超前时从头读。旧的 `mobilelink` 返回 404，JS 跳过。

**确认：** JS 的游标随进程重置，所以冷启动后要靠确认避免重复上报。上报成功后 `POST /diagnostics/ack`，`{next, dropped}`：`next` 是最早一条尚未上报的 Go 记录之前的位置，Go 层删掉它之前的记录并立即存盘，`after=0` 就从这里读起；`dropped` 从 Go 的丢失计数里减去，只在 JS 已上报全部丢失数时才带。确认失败时下次再发，冷启动后可能重报这一小段。

**上报：** `POST /local/v2/timing-reports`，`{batch_id, records}`，返回 `{accepted, rejected, duplicate}`。

- 时机：socket 打开时和每轮结束 2 秒后。失败保留同一个 `batch_id`，下次 socket 打开时重试。
- 能力：`/hello` 的 `timing-report-v1` 只对远端调用方声明。没有这个能力就不上报，记录留在有界缓冲里。
- 策略：`caller.go` 只允许 `callerRemote`，并要求 `X-Multica-Client-Kind: mobile`。
- 限流：每台设备突发 12 批，之后每分钟 6 批，超出返回 429。单批最多 500 条、1 MiB。最近 64 个 `batch_id` 去重。
- 只能报自己的：来源必须是 `mobile`；阶段限定在 `timing.Reportable` 的白名单内，模型和轮次的 mark 不能上报；`conversationId` 必须属于请求的 scope。`reporter`、`receivedAt`、`origin`、`clientKind` 由执行端覆盖。
- 时钟：导出按 `receivedAt` 过滤和排序，错误的手机时钟不会丢记录。瀑布图的起点取本机记录，上报记录的偏移可以是负数。`totals` 不受影响。

**导出：** 带会话、请求或轮次的记录进入 `records` 和瀑布图。什么都不带的连接记录进入 `deviceTimeline`（最多 2000 条）。`peer` 和 `reporter` 在导出时用每次导出随机生成的密钥做 HMAC，文件里是原值，所以同一设备只在一份导出内可辨认。导出的日志文本里，multiaddr 整段去掉，peer ID 换成同一个哈希（`peer:<哈希>`）。传输总耗时只配对发送方自己的 `client.first_event_received` 和 `ws.first_output_write`，否则同时打开的另一台设备会算错。

**桌面作为发起端的缺口：** 桌面发起的 P2P 轮次不会回传给执行端。桌面的 GUI 记录在 Electron 的 `turn-timing.jsonl`，代理记录在发起端的 `device-timing.jsonl`，上报需要一个新的主进程上传器，还要判断哪些记录属于远端会话，复用并不自然，所以这一版不做，`Admit` 也拒绝转发上报。这种轮次要在两台设备上分别导出，再按 `requestId` 对照；执行端单独一份导出没有 `client.*`，也就没有 `transportMs`。

### 第 2 步：Codex 设置切换改为重启进程

真实 CLI 验证否定了原计划的“跳过 `thread/resume`”：对已加载线程恢复只要 2–3ms，不值得多维护一套线程状态，这一项取消。

验证同时发现现有 bug：对已加载线程执行 `thread/resume` 时，新的模型和权限都不生效，响应仍返回旧值。实测从 `full-access` 切到 `read-only` 后，模型仍能写文件。

修复：

- `ApplySettings` 发现模型或权限确实变化时，标记进程需要重启。
- 没有运行中轮次时，下一次发送前停止旧进程，走完整启动和恢复，新设置随 `thread/resume` 应用。运行中的轮次继续使用它开始时的设置。
- 新进程恢复时，不传权限会沿用线程存储的旧权限（已实测）。所以 `default` 模式恢复时也显式传递用户配置的权限，配置没有时使用 `on-request`、`user` 和 `workspace-write`。新建线程仍交给 Codex 决定。
- 推理强度已经每轮随 `turn/start` 传递，变化时不需要重启。
- Codex 冷启动约 110–185ms，这个代价可以接受。

完成标准：切换模型或权限后的下一轮确实使用新设置，权限降级立即生效。

### 第 3 步：Prepare、并发合并、回收和预热

#### Prepare

| | Codex | Claude Code |
|---|---|---|
| 复用方法 | `ensureServer` + `thread` | `ensureAdapter`（已有会话带 `resume`） |
| 就绪标志 | 进程已就绪且线程已恢复，后续每轮的 `thread/resume` 只需 2–3ms | `start` 返回，即 `supportedModels()` 完成 |
| 发送时 | 直接 `turn/start` | 把消息放入 Query 队列 |

预热不发送用户消息，不调用 `turn/start`，不占用 `MaxRunningTurns` 名额。

Claude 必须保留 `supportedModels()` 这个就绪检查，不能因模型列表已缓存就跳过。

#### 预热范围

新会话和老会话、Codex 和 Claude 都默认预热。

| 场景 | Codex | Claude Code |
|---|---|---|
| 新会话 | 1 个备用 `app-server` | 1 个备用 sidecar，内含 `prewarm()` 的 Claude Code |
| 老会话 | 打开后提前 `thread/resume` | 打开后提前 `query({ resume })` |
| 启用条件 | 已安装 Codex 且已登录 Multica | 已安装 Claude Code 且已登录 Multica |

新会话不为具体会话创建 runner，因为 `management.go` 会拒绝已有 runner 的会话更换目录。备用进程不属于任何会话，首次发送时才绑定。

老会话预热的前提：`ProviderSessionID` 不为空、未归档、`acceptTurn` 通过。还没有原生会话的会话一律不预热，否则会提前创建线程，也会让侧边会话首条消息丢失来源上下文（`sidePrompt` 只在 `ProviderSessionID` 为空时附加）。

#### 预热触发

本机和远端查看都会触发预热。协议新增 WebSocket 消息 `conversation.focus {conversation_id, focused}`，由执行端判断和调度：

- **发起端：** 桌面按可见标签页、手机按会话页面发送，稳定停留 1–2 秒后才发 `focused: true`，离开时发 `false`。现有 `subscribe` 不能代替：手机离开页面不退订，桌面按监听者而不是可见标签页订阅。
- **多端：** 执行端按会话维护聚焦中的 `clientKey` 集合，绑定连接生命周期。同一会话只预热一次，集合清空且宽限期结束后才取消或开始计算回收。手机和桌面不会互相顶替。
- **宽限期：** 连接关闭后保留约 30 秒，避免迁移直连或短暂断线被当成离开。手机切后台不主动断开连接，iOS 挂起后连接探测失败才断开，之后随宽限期结束。
- **检查：** 校验 scope，排水时拒绝。预热不更新 `actedOn`，不算用户操作。
- **兼容：** 旧守护进程会忽略未知类型的消息，旧客户端不发送就不预热，都走现有路径。
- **副作用：** 这是首次让“只查看”在执行端启动进程，也可能触发 `SessionStart` hooks。关闭开关放在执行端的设备设置中，因为设备设置不能远程修改。

#### Codex 新会话

```text
提前：启动 app-server → initialize → 生成 MCP token 并放入环境变量
发送时：config/read（带 cwd）→ 绑定 MCP → thread/start → turn/start
用完后：后台补充备用进程
```

不提前创建线程：用户不发送时会留下空线程，发送前目录也可能变化，而线程配置还依赖这个会话的 MCP 和指令。

绑定失败时丢弃备用进程，走现有启动流程。此时用户消息尚未提交，不会重复执行。

#### Claude 新会话

```text
prewarm（不带 MCP）→ 首次发送 → claim（目录、模型、权限）
→ set_mcp_servers → 放入第一条消息 → 后台补充备用进程
```

`mcpServers` 和 `allowedTools` 在 `prewarm` 时固定，而 MCP 按会话绑定，所以 claim 后才绑定 MCP，且绑定完才放入第一条消息。`allowedTools` 的工具名前缀固定，可在预热时设置。

现有 `start` 的每个字段都要明确归属，漏掉不会报错，只会静默改变行为：

| 归属 | 字段 |
|---|---|
| `prewarm` | `allowDangerouslySkipPermissions`、`pathToClaudeCodeExecutable`、`includePartialMessages`、`settingSources`、`plugins`、`strictMcpConfig`、`env`、`settings`、`allowedTools`、`systemPrompt` 预设 |
| `claim` | `cwd`、`model`、`permissionMode`、`appendSystemPrompt`、`settings` 覆盖层（推理强度） |
| claim 之后 | `set_mcp_servers` |

就绪信号改为 `await claimed`。SDK 文档说明 `claim` 被拒绝后 `supportedModels()` 仍描述停放进程，不能作为判断依据。

现有 `claude.Start` 启动时就绑定事件处理函数，备用进程需要改为认领时切换。adapter 协议新增 `prewarm` 和 `claim`，手工同步 `claude-adapter-v1.schema.json`、`protocol.ts`、`conn.go` 和样本。耗时通过 `start` 结果的 `data` 带回，不新增帧类型，否则旧守护进程会报协议错误。

版本判断不比较版本号，因为文档没有写最低版本：

- CLI 太旧时 `prewarm()` 报错，记下该“可执行文件 + 版本”不支持，升级后再试。
- `host.ts` 目前没有发送 `capabilities`，要补上，用它判断 sidecar 是否支持。

第一条消息要等 `claimed` 和 MCP 绑定都成功才放入，所以下面的失败都发生在消息运行之前，都可以安全回退：

| 失败情况 | 处理 |
|---|---|
| `claim` 被拒绝，如项目设置了 `env` 或 `model` | 回退普通 `query()` |
| `option_not_applied`（模型没应用上） | 同上，不带着错误模型跑第一轮 |
| `settings_not_applied` | 同上，SDK 会关闭这个进程 |
| `set_mcp_servers` 失败 | 同上 |
| 备用进程退出 | 监听 `exited`，补充新的 |

回退规则：

- adapter 的 `claim` 等待 `claimed`，压制被拒绝的 Query 产生的结束和错误帧，返回带类型的“未认领”错误。否则 Go 会把轮次提前标记失败。
- 回退使用新的 adapter 进程和新的 normalizer。
- 绑定成功前不保存 `session_id`，否则回退会带上一个没有记录的 `resume`。

SDK 中 `prewarm()` 标记为 `@alpha`。项目已锁定 SDK 精确版本，升级时复查接口；任何失败都回退普通启动。

#### 老会话

用户在当前会话稳定停留 1–2 秒后触发。

- Codex 走 `ensureServer` + `thread`，进程已就绪时不做任何事。
- Claude 走 `ensureAdapter`。`claim` 不支持 `resume`，所以老会话用不上 `prewarm()`。
- Claude 默认继续用 `query()` 加空消息队列；只有实测更快才改用 `startup()`。
- 不用 `resumeSessionAt` 截断历史提速，因为模型会丢失后面的上下文。

#### 并发合并

- 同一 runner 同时只有一个准备任务，后来者等待它的结果。
- 准备任务不绑定发起请求的上下文，取消预热不影响正在等待的发送。
- 准备任务使用 runner 持有的上下文，`Close` 可以取消，并设一个总期限。现在 `config/read`、`model/list` 和 Claude `start` 都没有自己的超时，而队列分发会在持锁时等待准备。
- `Close` 在锁内设置关闭标记。准备完成时如果已关闭，就停止进程，不再安装到 runner。
- `Send` 等待准备后再次确认轮次仍然有效，已取消或已结束就不提交。
- 预热失败只记录日志，发送失败才走现有报错流程。
- 设置带版本号。准备期间设置变化时，Codex 不标记就绪、发送时再恢复一次；Claude 就绪后补发在线设置。

#### 资源控制

- 只预热聚焦中的会话，后台标签页不触发。
- 会话的聚焦集合清空且宽限期结束时，取消尚未开始的预热。
- 预热同时只跑 1 个，其余排队；不再被任何客户端聚焦的请求移出队列。
- 真实发送不排在预热之后。

回收对象是空闲进程，不是会话。回收后保留 runner 和会话 ID，下次发送时恢复。

| 参数 | 起步值 |
|---|---|
| 空闲进程上限（不含备用进程） | 4 个 |
| 预热后未发送 | 5 分钟回收，优先回收 |
| 发送过的进程 | 空闲 30 分钟回收 |
| 备用进程 | Codex 和 Claude 各 1 个 |

运行中的会话不计入上限，上限也不会拒绝发送。

| | Codex | Claude Code |
|---|---|---|
| 回收动作 | `server.Stop()` | `adapter.Close()`，先结束输入流以保存记录 |
| 恢复方式 | `ThreadID` + `thread/resume` | `ProviderSessionID` + `resume` |
| 不可回收 | 有运行中的轮次或待处理的交互 | 同左，另外还有未结束的后台任务 |

`Running()` 不足以判断 Claude 能否回收，因为后台任务可能在轮次结束后继续运行。

内存估算：Codex 约 150 MB/个（本机 2 个进程实测）；Claude 本机 RSS 为 189–298 MB/个，SDK 文档称停放进程约 230–260 MB，另加未实测的 Node adapter。最坏情况约 1.6 GB，另加 Node adapter。备用进程的实际占用待实测。

回收通过 `runner.Evict()` 完成：在锁内确认没有轮次和准备任务，断开进程并清空已加载记录，然后在锁外释放和停止。这样避免发送拿到正在退出的进程，被误记为 `outcome_unknown`。

回收会释放上下文绑定，并重置这个会话的 Computer Use JS 运行时。空闲 30 分钟后，上一轮留下的变量会丢失，这一点要在 Computer Use 文档中说明，或者在运行时存活期间不回收。

#### 备用进程管理

备用进程由 Service 统一持有：

- `Close`、`StopProviders` 时停止，排水期间不补充，`Undrain` 后补充。否则更新时会继续占用旧可执行文件。
- 只在有本机或远端客户端连接时补充，无人使用的机器不保留。
- 补充失败时指数退避，连续失败 N 次后停用，避免 CLI 启动即崩时反复拉起。
- CLI 版本、环境或登录用户变化时丢弃并重建。
- `prewarm()` 会冻结认证、用户技能和插件。设置固定存活时长，并在 provider 用户配置文件变化时丢弃，否则 `/login` 或修改设置后，下一个新会话仍用旧状态。

#### 第 3 步的实现说明

下面是实现时才确定、或比上文更具体的部分。

**准备任务：**

- 准备任务的总期限是 4 分钟，低于发送的 5 分钟，这样挂起的预热结束后，等待它的发送还能自己报错。
- 用户在准备期间点停止，发送立刻结束并记为取消，不再等任务结束；任务继续跑完，进程留给下一次发送。
- 预热失败后，等待它的发送会自己再准备一次。预热的 `thread/resume` 失败时停止这个进程，避免后面的发送卡在同一个进程上。发送自己发起的准备失败仍走现有报错。
- Codex 的设置变化沿用 `restart` 标记：进程在 `thread` 之前就装入 runner，`ApplySettings` 总能看到它。任务结束后发送发现需要重启，就再准备一次。Claude 用版本号，就绪后补发在线设置；补发失败时准备算失败，不让第一轮带着旧权限运行。在线下发一次只跑一个，总是发最新版本，先发权限，不跟随请求的上下文，发送也等它结束。失败时没有轮次在跑就关闭 adapter，有轮次在跑就等轮次结束后关闭，下一轮冷启动并使用新设置。
- Claude 进程在第一次收到输入之前的输出不属于任何轮次，直接丢弃，也不保存其中的 `session_id`，否则预热的 `query({ resume })` 启动消息会开出一个原生轮次。发送已经在等待时也一样，否则这段输出的结果会结束用户的轮次，提示词却没有送达。代价：如果 `SessionStart` hook 自带第一条消息，预热后这条消息的回复不会显示。这种情况未实测。

**聚焦：**

- `conversation.focus` 由 `/hello` 的 `conversation-focus-v1` 决定是否发送。发起端的稳定时长是 1.5 秒。桌面只有当前标签页的内容处于挂载状态，所以按 `active` 加窗口可见判断；手机按页面聚焦判断。
- 客户端按 `clientKey` 记录，同一客户端的多条连接分别计数，最后一条关闭后才开始宽限。没有客户端 ID 的连接各算一个客户端。
- 定时检查的间隔是 5 秒，所以实际宽限是 30–35 秒。宽限只用于连接关闭；客户端明确发 `focused: false` 时立即生效，尚未开始的预热随之出队。
- 被聚焦的会话不回收，但计入空闲上限。预热和发送过的进程，空闲时间都从“最后一个客户端离开”和“进程空闲”中较晚的时间算起。
- 排水时只记录聚焦，`Undrain` 后补做预热。

**回收：**

- 不回收的情况：有轮次、有准备任务、Claude 有未结束的后台任务或未送达的后台结果、有等待回答的问题、会话正被聚焦、Computer Use 运行时存活。Computer Use 选了“运行时存活期间不回收”，所以文档不需要说明变量丢失。
- 回收不从 `s.runners` 移除 runner，所以 `SetDirectory` 的判断不变。

**备用进程：**

- 条件由 daemon 提供：CLI 路径和版本、登录用户、环境变量哈希，以及 provider 的用户配置文件（Codex：`config.toml`、`auth.json`；Claude：`settings.json`、`.credentials.json`）。Claude 运行时约每分钟重写一次 `.claude.json`，所以这个文件只比较账号字段（`oauthAccount` 的账号和组织、`primaryApiKey`）。认领时重新读取一次，其余时候最多 30 秒读一次。
- 补充失败按 5 秒起指数退避，连续 5 次后停用这个“路径 + 版本”，换版本后重试。备用进程存活不到 1 分钟就退出也算一次失败。旧 sidecar（`ready` 没有 `prewarm` 和 `claim`）和 `prewarm()` 报 `--await-claim` 的 CLI 直接记为不支持。
- 最长存活 30 分钟，没有客户端 30 秒后丢弃。
- 配置变化后立即替换一次。之后如果备用进程存活不到 10 分钟又失效，下一次替换等 1 分钟，连续发生时翻倍，最长 30 分钟。
- Codex 备用进程在 `<ProfileDir>/local-conversations/spare` 启动。`thread/start` 失败也丢弃备用进程走普通启动，因为此时同样没有提交消息。
- Claude 的 `claim` 在 `permission_mode` 为 `default` 时不传 `permissionMode`，这样项目设置了 `permissions.defaultMode` 时 claim 被拒绝并回退，和普通启动的行为一致。
- 认领期间 Claude 的输出先缓存，绑定成功后才交给 runner，所以绑定成功前不会保存 `session_id`。`claim` 结果里的 `session_id` 只给 adapter 自己用；runner 仍从消息中获得，这样发送前被停止时不会留下没有记录的 `resume`。
- `set_mcp_servers` 的结果里 `errors` 不为空也算失败。

**开关：** 老会话预热的开关保存在 `<ProfileDir>/conversation-preheat.json`，路由是 `/local/conversation-preheat`，只在本机入口提供，界面放在设置的“设备互联”页。备用进程不受这个开关影响，因为停放时不运行 hooks。

**待真实 CLI 验证：** `prewarm` 的完整选项、`--await-claim` 的报错文本、认领后第一轮的工具调用、预热老会话时 hooks 的输出。默认测试只用假进程。

### 第 4 步：默认模型解析收敛为公共能力

解析顺序：显式选择 → 运行时配置 → 运行时目录。

- 不把解析结果写回设置，“跟随默认”仍然是跟随默认。
- 结果记录来源和失效条件，准备状态区分已确定、解析中、失败和暂不能提前确定。
- Codex 复用 `server/pkg/agent/codex.go` 已有的解析逻辑，`catalog.Model` 补 `isDefault`。
- Claude 沿用 `server/pkg/agent/claude_models.go` 对 `default` 和 `resolvedModel` 的处理。
- 新线程可以继续使用原生默认配置，不为统一接口增加阻塞查询。

这一步主要解决语义统一，提速有限：只有恢复已有线程，且会话设置和运行时配置都没有模型时，现在才会调用 `model/list`。

#### 第 4 步的实现说明

- **共用：** 分页读取默认模型的逻辑移到 `server/pkg/codexappserver/model.go` 的 `CatalogDefaultModel`，任务执行和本机会话共用。两边原有的依赖都已包含这个包。任务执行只换了这一个函数，生命周期不变。
- **每个进程一次：** `CodexRunner` 按进程缓存 `model/list` 的成功结果。失败不缓存，下一轮再问。进程换了就重新问。
- **Claude：** `catalog` 直接引用 `pkg/agent` 导出的 `ClaudeModelID` 和 `ClaudeDefaultModelValue`。`default` 行不再作为可选项，只给解析到同一模型的行标 `isDefault`；没有这样的行时才单独生成一行。和 `pkg/agent` 不同，可选行的 ID 仍是选择器的值（如 `sonnet`），因为已保存的设置用的就是它。
- **旧设置：** 以前可以选 `default` 行，已保存的 `model = "default"` 在快照里读作跟随默认，不改存储的数据。
- **Codex 的 `isDefault`：** `models_cache.json` 没有默认标记，所以它来自运行中进程的 `model/list` 回答，保存在内存里。守护进程启动后没有进程问过时，目录里没有默认行，客户端要能容忍。
- **状态的表达：** 没有增加专门的状态对象，只用 `model.resolve` 记录。`reason` 是来源（`explicit`、`config`、`catalog`、`catalog_cached`、`native_default`）；只有开始没有结束是解析中；`outcome: failed` 是失败；`native_default` 是暂不能提前确定。失效条件就是进程退出。这与上文“结果记录来源和失效条件”的写法不同，是为了保持改动小。

### 第 5 步：Codex 思考过程实时显示

本机和 P2P 都要支持，与第 1 步同时开始。

现状：`server/internal/localconversation/provider/codex/normalize.go` 只转发 `item/agentMessage/delta`，思考内容要等 `item/completed` 才出现。

- `started` 目前忽略思考条目，要改为发出 `item.started`（`kind: "reasoning"`）并开始跟踪。
- 处理 `item/reasoning/summaryTextDelta`、`item/reasoning/summaryPartAdded` 和 `item/reasoning/textDelta`，转成 `item.delta`。现有最终内容由 `summary` 和 `content` 拼接而成，流式内容要与它的来源和分隔保持一致。
- 复用现有 `item.delta`（`field: "text"`），不需要新的 P2P 协议。要确认桌面和手机都能显示进行中的思考条目。
- 完成时仍以 `item/completed` 的最终内容为准。`Close` 目前只给 `assistant_text` 写 `text`，要修复，取消或失败时保留已收到的思考内容。
- 桌面收到完成事件会替换内容，所以要确认 Codex 完成时一定带内容，否则空的最终内容会清掉已显示的思考。
- 手机目前把思考显示成普通工具行，要和桌面对齐展示方式。
- 摘要关闭时没有增量，`item/started` 之后只显示“思考中”状态。

完成标准：模型思考期间，桌面和手机都能持续看到摘要；思考结束后内容与最终结果一致。

### 第 6 步：手机支持并优先直连

手机的连接顺序：登记的局域网地址 → 登记的公网地址 → 中继并打洞，打洞成功后迁移到直连。`Dial` 现在直接使用可用路径，跨网时通常先走中继，所以迁移是常规路径，不是边缘情况。

**迁移到直连：**

- `server/mobilelink/client.go` 接入 `OnDirect`。连接记录改为按设备保存，并保留原始连接：`p2p.Relayed` 判断的是原始类型，照搬桌面逻辑时包装后的连接永远判断为否，WebSocket 会一直留在中继。
- 已注册的直连地址晚于中继连接到达时，`Refresh` 会强制直连拨号一次。`connect` 把中继连接视为已连接，打洞也不处理私有地址，否则局域网地址到得晚就一直走中继。
- 迁移时只关闭空闲连接和已升级的 WebSocket，进行中的 HTTP 请求继续完成。桌面的 `closeRelayed` 也按同样规则修改。

**无感重连（`apps/mobile/data/ai-sessions/client.ts`）：**

- 不尝试识别“迁移导致的断开”。所有关闭一视同仁：立即重试一次，约 2–3 秒后仍未恢复才显示断线。桌面如果也要无感，要做同样的修改。
- 重连窗口内允许发送排队，现在发送会被静默丢弃。
- 增加连接中状态防护，避免立即重连产生两个 socket。
- 有缓存快照时按 `snapshot_seq` 直接订阅，`snapshot_required` 作为回退。现在每次重连都重读完整快照。
- 连接切换时中止旧读取并在失败时重试。现在旧读取失败可能让新连接永远不订阅实时事件。
- 处理 `subscription.status: rejected`。

**网络切换：** `mobilelink` 增加 `NetworkChanged()`，由 NetInfo 在网络变化时调用。现在 Wi-Fi 切蜂窝可能约 20 秒才恢复。

**前后台：**

- 现在 `apps/mobile/data/ai-sessions/provider.tsx` 进入后台就停止 P2P 服务，回到前台要完整重启，并阻塞在注册上。
- 改为切后台不主动停止，由 iOS 决定何时挂起；`beginBackgroundTask` 只用来多争取一段运行时间。回前台时立即检查回环入口和设备连接，还通就继续用，不通才恢复。iOS 上的实际效果要在真机确认。
- 先启动回环入口，注册改为异步，注册失败不阻塞连接已知设备。已知设备来自 `GET /v1/devices`，不写入本地，所以注册表完全不可达时，冷启动后没有已知设备，`/devices` 返回错误。
- Token 刷新时更新凭据，不重启整个服务。
- `Refresh` 已经会连接所有已知设备，不需要额外的“连接最近设备”。

**发送超时：** 手机 30 秒短于线程准备的 60 秒上限。手机的命令请求已改为与桌面一致的 120 秒，`mobilelink` 的响应头超时也相应放宽，重试靠 `request_id` 保证幂等。前置修复后发送不再被取消。发送请求要在消息落库后尽快返回，准备和提交转入后台，后续错误通过事件送达，手机的错误处理要同步调整；这部分尚未实现。

**中继：**

- 手机的连接列表排除其他手机。每个账号最多 8 条中继线路，手机现在会连接所有设备，包括其他手机。
- 迁移后考虑关闭已无流的中继连接，但保留预留，确保设备仍可达。
- 记录线路被拒绝的情况。
- 修正 `relay_transfer_limited` 被改写成 `device_unreachable` 的错误分类。

**局域网：** 不给手机加 mDNS。登记地址已能走局域网，只需处理本地网络权限。用户拒绝权限时会静默退回中继，要在 mark 中能看出来。

完成标准：能打洞时手机的 WebSocket 确实走直连（按流判断，不看设备级连接状态），迁移不丢事件、不显示断线，网络切换后快速恢复；Mark 能看到手机的直连占比和建连耗时。

## 性能 Mark 清单

### 通用规则

- **内容：** 只记录标识、耗时和数量，不记录提示词或模型载荷。
- **隐私：** 地址只记类型，peer ID 在导出时做哈希，错误只记分类，不写原始错误文本（现有拨号错误可能带地址）。
- **关联：** 每条 mark 带 `request_id`、`conversation_id`、`turn_id`（已产生时），再加 `attempt`（重试）、`prepare_id`（关联等待的预热）、`turn_kind`（`user`、`queued`、`steer`、`background`、`native`、`agent`）。发起信息保存在守护进程内存中按 `request_id` 关联，重启后记为 `unknown`。后台和原生轮次不计入 p50、p90。
- **维度：** `origin`（本机或 P2P）、`client_kind`（桌面或手机）、`connection`（直连或中继，取这条流自身的连接，不用设备级状态）、`provider`、`prepare_kind`（热路径、已预热、备用进程、冷启动）、`turn_index`。
- **“首个”的定义：** 指这一轮首个模型输出。每轮第一次提交的是用户消息，直接取第一次提交会把慢轮次也算成很快。按订阅者记录，并区分实时和补发。
- **高频事件：** 不给每个 token 打 mark，只记录每轮首个事件，并按轮汇总数量、字节数和最大间隔。
- **连接层：** 只记状态变化，不记定时探测。设备级记录使用单独文件，避免挤掉轮次数据。
- **失败：** 每个阶段的失败和取消都要记录，带上错误分类。
- **跨设备耗时：** 协议没有对时消息，第一版不估算时钟偏移。每台设备只算自己的耗时，传输总耗时用 `(client.first_event_received − client.send) − (ws.first_output_write − daemon.request_received)` 计算，不需要同步时钟。确实需要单向耗时时，再增加 `time.sync` 能力。
- **上报：** 见第 1 步的手机回传。

### 所有场景：发送到首个输出

| 位置 | Mark |
|---|---|
| 发起端 | `client.send`、`client.pending_shown`、`client.request`（开始和结束） |
| 守护进程入口 | `daemon.request_received`、`queue.dispatch`、`turn.persist` |
| Runner | `runner.create`、`runner.limiter`、`prepare.wait`（等待准备任务） |
| 回传 | `batcher.first_output_commit`、`hub.first_output_publish`、`ws.first_output_write` |
| 发起端 | `client.first_event_received`、`client.first_event_applied`、`client.first_commit`（React Native 没有绘制回调，记录 React 提交） |
| 结束 | `turn.finished`、`client.terminal_applied` |

### Codex

`provider.spawn`、`provider.handshake`、`provider.config`、`context.bind`、`model.resolve`、`thread.prepare`（记录 `start`、`resume` 或 `skipped`）、`provider.turn_start`、`provider.first_frame`、`provider.first_reasoning`、`provider.first_text`、`provider.first_tool`、`provider.exit`。

### Claude Code

`adapter.spawn`、`adapter.ready`、`sdk.load`、`context.bind`、`query.init`（`supportedModels()` 完成）、`spare.claim`、`mcp.set`、`provider.input_queued`、`provider.first_message`、`provider.session_ready`、`provider.first_thinking`、`provider.first_text`、`provider.first_tool`、`provider.exit`。`resume` 时附带会话记录文件大小。`sdk.load` 和 `query.init` 由 adapter 通过 `start` 结果带回。

### 预热和回收

`focus.changed`（聚焦集合变化）、`preheat.requested`、`preheat.cancelled`、`preheat.started`、`preheat.finished`、`preheat.failed`、`spare.created`、`spare.claimed`、`spare.rejected`（带原因）、`spare.discarded`、`runner.evicted`（带空闲时长和进程类型）。

### P2P：服务和连接

| 阶段 | Mark |
|---|---|
| 服务 | `link.start`、`link.ready`、`link.registered`、`link.stop`（手机记录前后台触发） |
| 设备发现 | `registry.fetch`（只记失败和耗时异常）、`peer.known`、`peer.mdns_found`（仅桌面） |
| 建立连接 | `peer.connect`（开始和结束，记录地址类型）、`relay.reserve`、`relay.connected`、`relay.refused` |
| 打洞 | `holepunch.start`、`holepunch.success`、`holepunch.failed`，带角色。需要在 `newHost` 接入 libp2p 的 tracer |
| 状态变化 | `connection.direct`、`connection.relayed`、`connection.lost`、`network.changed` |

### P2P：隧道和实时连接

| 阶段 | Mark |
|---|---|
| 隧道 | `tunnel.conn`（用 `httptrace`，记录是否复用）、`tunnel.stream_open`（协议协商是惰性的，并入这里） |
| 代理 | `proxy.request`、`proxy.first_byte`（发起端和执行端各记录） |
| WebSocket | `ws.ticket`（仅桌面）、`ws.hello`、`ws.open`、`ws.subscribe`、`ws.subscribed`、`ws.catch_up` |
| 迁移直连 | `migrate.start`、`migrate.stream_closed`、`migrate.reconnected`、`migrate.resubscribed` |
| 断线重连 | `ws.closed`（带原因）、`ws.retry`（带退避时长）、`ws.resubscribed` |

### P2P：跨设备的事件和快照

| 阶段 | Mark |
|---|---|
| 请求 | `link.request_sent`、`link.request_received`、`link.response_sent`、`link.response_received`（不用 `remote.*`，它已表示云端 agent 轮次） |
| 事件 | 执行端 `link.event_sent`，发起端 `link.event_received`，按轮汇总批次数、字节数、最大间隔 |
| 快照 | `snapshot.request`、`snapshot.server_read`、`snapshot.first_byte`、`snapshot.received`、`snapshot.parsed`、`snapshot.rendered`，带字节数 |

打开会话目前不是性能卡点，快照 mark 只用于观测，不对应优化项。

## 范围边界

- 任务执行只接计时和模型解析，不合并生命周期，因为任务的凭据、目录和 MCP 都是隔离的。
- Agent 对话继续走云端路径。

### 不做的事

- **不减少提供给模型的工具：** 直接减少后，模型就不知道这些工具存在。只有运行时提供工具搜索这类按需发现的机制时才可以考虑延迟加载，并要确认模型仍能找到并调用它们。现有数据也显示收益有限（工作区和设置工具共 86 个、约 61 KB，首轮缓存命中率约 38%，首轮只比后续轮慢约 1.6s，仅 4 个样本）。
- **不精简会话快照：** 打开会话不是性能卡点。

## 验证

默认测试全部使用假的 Codex 和 Claude 进程，覆盖：

- 首次准备、热路径、切设置、进程退出
- 准备与发送并发、取消预热、准备期间切设置
- 回收保护，包括运行中轮次、待处理交互和后台任务
- 备用进程认领、claim 失败回退、`option_not_applied` 不重启
- `start` 和 `prewarm` 都带 `allowDangerouslySkipPermissions`，`permissionMode` 按会话设置传递
- Codex 思考增量的转换、段落分隔，以及与最终内容一致
- Mark 上报失败不影响会话，记录中不含提示词和模型载荷
- `Close` 或 `StopProviders` 发生在准备期间、准备挂起、准备期间取消
- 发送期间中继流被关闭，轮次不失败、不重复
- 侧边会话不被预热，首条消息保留来源上下文
- Claude 空闲时 CLI 退出，下次发送自动恢复
- claim 成功但 `set_mcp_servers` 失败的回退，不带无效 `resume`
- 多端聚焦、迁移期间的聚焦宽限、排水时拒绝预热
- 备用进程补充的退避和停用
- 解析器接受清单中的每个 mark，用假时钟生成单轮瀑布的样本

P2P 迁移拆成两层测试：Go 层用真实中继加后到的直连地址，断言走中继的流被关闭，手机的包装连接也要覆盖；手机层用 `socketFactory` 覆盖关闭与读取的两种先后顺序。真机行为按 `apps/mobile/AGENTS.md` 手工验证。

假 SDK 只能验证回退逻辑，`prewarm` 本身的行为要靠真实 CLI。

版本兼容矩阵：新手机对旧守护进程、旧手机对新守护进程、旧桌面对新执行端、旧 adapter 对新守护进程。

效果验证按三条路径分别对比冷启动、预热后和连续发送的分段耗时，P2P 再区分直连和中继。

### 真实 CLI 验证结果

2026-11-02 使用 Codex CLI 0.164.0、Claude Code 2.1.316、SDK 0.3.310 在空项目中测得，没有计入真实 MCP 绑定和偶发长尾。

**启动耗时：**

| 项目 | 结果 |
|---|---|
| Codex 启动和握手 | 74–145ms，单个进程约 107 MB |
| Codex `config/read` / `thread/start` | 约 6ms / 32ms |
| Codex 已加载线程 `thread/resume` | 2–3ms |
| Claude SDK 加载 | 约 50ms |
| Claude 普通 `query()` 就绪（恢复小记录） | 233ms |
| Claude 恢复 5 MB / 50 MB / 100 MB 记录 | 239ms / 359ms / 461ms |
| Claude `prewarm()` | 243ms，停放约 222 MB |
| Claude `claim` / 绑定 MCP | 57ms / 48ms |

准备阶段比预想快得多，用户感知的等待主要在模型侧。预热和备用进程的实际收益，用 Mark 数据持续验证。

**Claude：**

| 项目 | 结果 |
|---|---|
| 消息队列为空时 `claimed` 能否完成 | 能 |
| claim 后绑定的 MCP，第一轮能否调用 | 能 |
| `appendSystemPrompt` 能否与预设合并 | 能 |
| `prewarm` 停放期间的模型请求 | 没有 |
| 不带开关的现有 `bypassPermissions` | 有效 |
| 带开关时 `default` 跳过权限 | 不跳过（普通启动和 claim 都验证） |
| 带开关时 `bypassPermissions` | 跳过（普通启动和 claim 都验证） |
| 会话中途切换 | `default` → `bypassPermissions` 生效，切回后也生效 |
| `auto` | 模式正确，未验证分类器的判断 |
| `SessionStart` hooks | 普通启动和 `resume` 在发消息前就触发；备用进程停放时不触发，`claim` 时触发 |
| 历史记录加载 | 启动时加载（内存随记录大小增长），预热能覆盖这部分 |

因此打开老会话预热 Claude 时，确实会运行项目 hooks。

**Codex：**

| 项目 | 结果 |
|---|---|
| 备用进程不在项目目录启动 | 可行：线程的 `cwd`、推理强度和 `AGENTS.md` 都正确，模型能读到项目指令 |
| `config/read` | 随启动目录不同，返回的推理强度可能不同：不在项目目录启动时读到项目配置，在项目目录启动时反而没有。实现要以传入的 `cwd` 为准并覆盖测试 |
| 线程不存在的错误 | `thread not found`，code `-32600` |
| 思考摘要 | 配置了摘要时有 `summaryPartAdded` 和 `summaryTextDelta`，拼接结果与最终内容一致 |
| 思考摘要的到达时机 | 本次测试只有 1 个增量，且紧贴正文之前到达，`item/started` 早约 2.7s。所以开始事件也要显示 |
| `model_reasoning_summary = "none"` | 没有摘要，`turn/start` 的 `summary` 参数也无法开启，这时只显示“思考中”状态 |

**未验证：** iOS 的 `beginBackgroundTask` 宽限、本地网络权限、真实设备的打洞和迁移、`holepunch.*` 在真实打洞中的记录，都需要真机，按 `apps/mobile/AGENTS.md` 手工验证。Swift 模块调用了新增的 `closeInBackground`，要重新构建 `MulticaLink` 才能编译，默认测试不覆盖 Swift。
