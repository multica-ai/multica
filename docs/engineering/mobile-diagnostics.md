# 手机日志读取

状态：已实现，尚未在真机上验证，见[待验证](#待验证)。

让电脑上的智能体通过已有的 p2p 设备链路读取 iOS App 的日志。智能体都运行在电脑上，而一部分问题只有手机上的日志才能看到，比如手机侧的连接失败、JS 异常和请求错误。

读取是一次电脑到手机的 RPC：电脑发起请求，手机把日志作为响应流返回。手机不向电脑上传文件。

## 范围

第一版包含：

- 手机端统一日志模块：Go 和 JS 共用，按天分文件，保留 7 天。
- 手机端 RPC：`GET /logs`，由 Go 应答。
- MCP 工具 `fetch_mobile_logs`：App 运行即可读取，不需要打开会话页，手机上不需要确认。
- 手机设置：允许电脑读取日志的开关，以及手动导出。
- 手机注册设备时带上机型，主动登出时注销设备。
- 在手机现有丢弃错误的位置补上日志。

暂不包含：

- 手机被 iOS 挂起时读取，以及推送唤醒。
- 通用入站通道和需要 JS 处理的远端能力。出现第二个这类能力时再设计。
- 原生崩溃采集。
- 登录页的导出入口。
- 从手机读取电脑的日志。
- 守护进程运行的智能体使用这个工具。工具只出现在本地对话流的 `multica_desktop_context` 中，独立的 `multica_developer_logs` 不提供。

## 分层

| 层 | 职责 |
| --- | --- |
| Go（`mobilelink` 和日志模块） | p2p 传输、应答 RPC、日志落盘、滚动、清理、读取和开关 |
| Swift（`MulticaDeviceLink` 模块） | 把日志模块接口暴露给 JS，在模块加载时打开日志模块 |
| JS（`apps/mobile/data/diagnostics/`） | 采集 console、异常和打点；设置开关；手动分享。不参与 RPC |
| 电脑守护进程 | 提供 MCP 工具、选择手机、发起 RPC、存储文件 |

## 读取流程

```text
1. 智能体调用 fetch_mobile_logs；守护进程选择手机
2. 电脑开一条 p2p 流 /multica/mobile-logs/1，发送 GET /logs?since_minutes=60
3. 手机 Go 校验来源设备在白名单内，检查开关
4. 手机 Go 边读日志边写入响应
5. 电脑边收边写文件，把路径和摘要返回给智能体
```

与手机读取电脑上的图片是同一种方式：HTTP 跑在 libp2p 流上，只是发起方相反。

- libp2p 连接本身是双向的，RPC 复用已有连接，不需要手机有公网地址。手机用 `svc.Listen` 监听，电脑用 `svc.NewTunnel` 发起，流用完关闭。
- 权限只由手机开关决定。电脑不需要开启设备互联，它管的是其他设备能否使用这台电脑。
- 关闭流就是取消：工具调用被取消时，手机停止发送。
- 流程不依赖 JS，所以 JS 卡死时仍能读到日志。

### 超时

| 阶段 | 上限 |
| --- | --- |
| 等待响应头 | 30 秒。手机被挂起时不会应答 |
| 整次读取 | 2 分钟 |

### 响应

| 响应 | 含义 |
| --- | --- |
| `200` | JSONL 流，格式见[日志流格式](#日志流格式) |
| `403` | 用户关闭了读取开关 |

## 统一日志模块

Go 实现，Go 和 JS 共用。日志模块的生命周期独立于 p2p 连接，因此登录前的日志和连接启动失败的原因也能记录下来。

```text
Go 代码（p2p、mobilelink）→ slog Handler ─┐
Go 打点（timing Sink）    → 复制一份     ─┼→ 日志模块 → 按天的文件
JS console / 异常 / 打点  → appendLogLines ┘
```

存放在 Application Support 下的 `MulticaDeviceLink/logs/`，文件权限 0600，并排除出设备备份。

每行一条 JSON：

```json
{"at":"2026-11-03T12:00:00.000Z","source":"app","level":"warning","workspaceId":"...","message":"[ws] socket closed"}
```

`workspaceId` 只出现在 JS 写入的 `app` 记录上，表示写入时所在的工作区，没有工作区时省略。`link` 和 `marks` 是设备级的，不带这个字段。

### 保留和清理

| 来源 | 内容 | 大小上限 |
| --- | --- | --- |
| `app` | JS console 和异常 | 10 MiB |
| `link` | Go 日志，包括 Debug | 10 MiB |
| `marks` | JS 和 Go 的 timing 打点 | 5 MiB |

- 每个来源按 UTC 日期分文件：`app-2026-11-03.jsonl`。单个文件写到 1 MiB 后换下一个序号。
- 超过 7 天的文件整个删除；超过大小上限时从最旧的文件开始删除。两个条件同时生效。
- 模块打开、日期变化和换文件时都会触发清理。App 被挂起时不运行，下次运行时补上。
- 写入失败直接丢弃，不影响业务。

### 账号

日志模块保存一个归属账号。

| 事件 | 行为 |
| --- | --- |
| link 启动前写入 | 正常写入，等 link 启动后归属 |
| 以 A 启动 link，当前无归属 | 归属 A，保留已有日志 |
| 以 A 启动 link，当前已是 A | 不变 |
| 以 A 启动 link，当前是 B | 先清空，再归属 A |
| 主动登出 | 丢弃 JS 缓冲，等 link 停止后 `clearLogs`，清除归属 |
| 401 被动登出 | 保留，以便重新登录后还能查到被登出前的日志 |

- 主动登出和 401 都走 `logout()`，所以清理放在主动登出的两个调用点（`more/settings.tsx`、`select-workspace.tsx`），不放在 `logout()` 内部。
- RPC 只返回归属账号与当前 link 账号相同的日志，否则按空结果处理。
- 读取开关是设备偏好，由日志模块保存在单独的文件里，`clearLogs` 不清除。

### Swift 暴露给 JS 的接口

```ts
appendLogLines(source, lines)                     // 同步
clearLogs()                                       // 同步
remoteLogReadAllowed()                            // 同步
setRemoteLogReadAllowed(allowed)                  // 同步
setLogRuntime(runtime)                            // 同步
exportLogs(sinceMinutes)                          // 手动导出，返回 { path, summary }
clearLogExports()
unregisterDevice()                                // 主动登出前调用
```

- `appendLogLines` 用同步原生函数，不用 loopback HTTP：JS 崩溃前的最后一条错误价值最高，异步请求可能来不及发出。gomobile 不能绑定 `[]string`，`lines` 是以换行分隔的字符串。
- 读取不持有写锁，避免导出时阻塞 JS 线程上的 `appendLogs`。
- App 启动时 JS 把运行信息（App 版本、构建号、系统版本、机型）交给日志模块，供 RPC 写入文件头。

### JS 采集

- 在 `app/_layout.tsx` 最靠前的 import 中包装 console，保留原有输出。
- 用 `ErrorUtils.setGlobalHandler` 捕获未处理异常，保留原 handler。
- 生产环境在 Hermes 上跟踪未处理的 Promise rejection。
- `MobileTimingBuffer.write` 只复制手机 JS 自身的打点（`native === undefined`）。Go 的打点在 Go 的 Sink 处复制，避免重复。
- 每 2 秒批量写一次，切到后台时写一次，error 级别立即写。
- 每条 `app` 记录带上当前工作区。
- JS 的 debug 级别不落盘，单条消息超过 16 KiB 时截断。
- `lib/parse-response.ts` 校验失败时不再打印 `received`，只保留 `issues`，避免把整个服务端响应写入日志。

### 补充日志

这些错误目前被直接丢弃，而它们正是这个功能要查的问题：

| 位置 | 内容 |
| --- | --- |
| `server/mobilelink/client.go` `p2p.Start` | 传入 `Logger`，现在未设置 |
| `server/mobilelink/client.go` `keepRegistered` | registry 注册失败 |
| `server/mobilelink/client.go` 代理 `ErrorHandler` | 代理错误原因 |
| `apps/mobile/data/ai-sessions/daemon-manager.ts` | link 启动失败 |
| `apps/mobile/data/ai-sessions/client.ts` | `/hello` 失败和连接失败 |

## 日志流格式

JSONL，一行一条，读取方可以用 `grep` 查询，流中断时已收到的行仍然可用。

```text
{"kind":"multica-mobile-diagnostics","schemaVersion":1,"runtime":{...},"filter":{...}}
{"at":"...","source":"app","level":"warning","message":"..."}
{"at":"...","source":"link","level":"INFO","message":"..."}
{"kind":"summary","sources":{...},"truncated":false}
```

- 第一行是文件头，最后一行是摘要，因为边读边发时开头还不知道总数。
- 记录按来源分段，每段内按时间排序。每行都有 `at`，读取方可以自行合并。
- 单次不超过 4 MB，低于中继的 5 MB 传输限制。这个限制在现有代理路径上，不会自动作用于新协议，所以由总量保证。
- 各来源有预算：`app` 1.4 MiB，`link` 1.4 MiB，`marks` 0.9 MiB。超出时保留最新的记录，在摘要中标记 `truncated`。
- Go 边读边写，不把全部内容读入内存。

手动导出生成同一种文件，由 `Share.share({ url })` 调起系统分享，关闭后删除。这是手机一直连不上电脑时的兜底方式。临时文件放在单独的目录中，模块打开和 `clearLogs` 时清空。

## MCP 工具

`fetch_mobile_logs`，注册在本地对话流的内置 MCP 中。与其他内置工具一样始终注册，不随手机是否在线变化；读不到时由工具返回原因。

| 参数 | 说明 |
| --- | --- |
| `request_id` | 必填。成功或进行中的请求，同一 ID 不重复读取；失败的可以用同一 ID 重试 |
| `device_id` | 选填，手机的 `daemon_id` |
| `since_minutes` | 1–10080，默认 60 |

返回文件路径、设备 ID、设备名称、连接方式、字节数、摘要，以及文件是否完整。

文件写入 `<ProfileDir>/local-conversations/diagnostics/mobile-exports/`，目录权限 0700，文件权限 0600，保留 7 天、最多 20 份。

### 选择手机

手机由 `Profile == "mobile"` 且 `DaemonID` 以 `ios-` 开头识别，不会选到其他电脑。设备 ID 是 `ios-<peerID>`，重装 App 或换账号后会变，旧记录在 registry 中最多保留 24 小时。

1. 指定了 `device_id`：按 ID 选择。
2. 否则，当前这一轮由手机发起：读这台手机。它离线时返回错误，不换到别的手机。
3. 否则，只有一台已连接：选它。
4. 否则，没有已连接的手机，且账号下只有一台：尝试它。
5. 其他情况不猜测：报错并列出所有手机的 ID、名称和连接状态。

### 发起设备

工具需要知道当前这一轮由哪台设备发起，这由 daemon 记录，不需要智能体传递。不使用环境变量，原因是：

- 内置 MCP 是 daemon 内的 HTTP 服务，读不到智能体进程的环境变量。daemon 通过路由 token 已经知道是哪个会话。
- 智能体进程在多轮之间复用，发起设备变化后环境变量不会更新。
- 预热进程启动时还不知道它会服务哪个会话，无法写入这些值。

记录方式：

```text
发送消息 → daemon 从 caller 取发起设备 → 记录到这一轮的 turn → 工具读取当前 turn
```

| 字段 | 含义 |
| --- | --- |
| `origin_peer_id` | 发这条消息的设备的 peer ID，本机发送时为空。取自已验证的 p2p 连接，不信任客户端传入的值 |
| `origin_client_kind` | `mobile` 或 `desktop` |

- 用 peer ID 而不是 `daemon_id`：电脑的 `daemon_id` 是整台机器一个，多个 profile 共用，不能唯一标识设备。手机的 `daemon_id` 是 `ios-<peerID>`，可以与 peer ID 互相对应。
- 排队消息在入队时记录发起设备，不等开始执行时再取。
- 智能体创建的 Side 会话没有直接发起方，继承来源会话当前这一轮的值。
- 工作区不需要新增字段，从会话的 `scope_id` 取得。
- 字段写入 `turn` 和 `prompt_queue_item`（迁移 `0004_request_origin`）。
- `current_context` 也返回发起设备和类型，方便智能体判断是否需要查手机日志。

### 错误

| 情况 | 工具返回 |
| --- | --- |
| 电脑的 p2p 未运行 | 说明设备链路未启动 |
| 账号下没有手机 | 提示登录 iOS App 并保持打开 |
| 开流失败，或 30 秒内没有响应 | 提示打开 App，iOS 会在 App 离开前台后不久将其挂起 |
| 手机不支持该协议 | 提示更新 App |
| `403` | 提示可在 App 设置中打开 |
| 流中途断开 | 返回已收到的部分，标记文件不完整 |

## 手机设置

在设置页新增“诊断”分组。开关默认开启。开关的值由日志模块保存，Go 在每次 RPC 时检查，修改立即生效，也不存在冷启动时尚未加载的问题。

| 元素 | zh-Hans | en |
| --- | --- | --- |
| 分组标题 | 诊断 | Diagnostics |
| 开关 | 允许电脑读取日志 | Allow computers to read logs |
| 开关说明 | 登录此账号的电脑上的智能体可直接读取此 App 的日志，无需确认。日志未脱敏，可能包含私密信息。 | Agents on computers signed in to this account can read this app's logs without asking. Logs aren't redacted and may contain private details. |
| 操作 | 导出日志 | Export logs |

zh-Hant 与 zh-Hans 同步翻译。

## 隐私和信任

日志不脱敏，Go 和 JS 都原样落盘，peer ID 也不替换。文件可能包含 IP、中继地址、libp2p 错误原文、请求路径和搜索词、服务端错误信息，以及代码打印到 console 的任意内容。

- **信任根是 registry：** 手机把 registry 返回的设备列表作为白名单，列表没有签名，这次不改 registry。代码默认地址是明文 HTTP，“只有同账号设备能读”成立的前提是 registry 连接和服务本身可信。
- **日志不分工作区：** 工作区 A 的会话可能读到工作区 B 相关的日志。`workspaceId` 只用于区分来源，不是隔离。
- **日志会发给模型服务商：** 智能体读取文件后，内容进入对话上下文。
- **“未经用户要求不读取”只是指引：** 被提示注入的智能体可以直接调用工具。手机开关是唯一的硬性限制。
- **电脑上的副本：** 手机登出或关闭开关后，电脑上已有的文件仍会保留到过期。

工具说明和智能体指引要写明：日志未脱敏（这一点与 `export_developer_logs` 不同）、分享前需要检查、未经用户要求不得上传。

## 限制

- **手机被挂起时读不到：** Go 和 JS 在同一进程，会一起被挂起。切到后台后只有系统给的短暂时间。日志已在磁盘上，App 回到前台、连接恢复后可以读到。
- **进程被突然终止：** 可能丢失最后约 2 秒未写入的非 error 日志。
- **单次最多 4 MB：** 时间范围较长时只返回最新部分，可以缩小 `since_minutes` 分段查看。

## 待验证

| 项目 | 为什么重要 | 验证方式 |
| --- | --- | --- |
| 只有中继时，真实蜂窝网络下的读取耗时 | 本地中继测试已验证两个方向的流都能到达，真实网络未测 | 真机测试 |
| 前台未打开会话时，电脑和手机之间是否有可用连接 | 决定“不需要打开会话页”是否成立。代码上成立，真机未验证 | 真机测试 |
| Claude 的 MCP 工具超时 | 整次读取最长 2 分钟，仓库中没有 Claude 侧的超时配置 | 开发时确认 |
| 机型名称的显示 | `device_name` 用 `expo-device` 的型号名，也显示在其他设备的列表中，要确认放得下 | 真机测试 |
| `Share.share` 关闭后删除临时文件的时机 | 过早删除会导致分享失败 | 真机测试 |

## 验证

`go test`（`internal/daemon/...`、`internal/localconversation/...`、`mobilelink`）、`pnpm --filter @multica/mobile typecheck`、`lint`、`test`。重建 XCFramework 后在真机上验证读取流程，包括断网、杀进程、切换账号，以及切到后台后的提示。
