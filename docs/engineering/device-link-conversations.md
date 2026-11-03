# 设备互联对话流

状态：已实现，见 #84（前置）、#86（守护进程）、#88（Desktop）。下文按实现更新。

在本机 Desktop 中，通过已有的 p2p 设备链路使用同账号另一台设备上的本地对话流。功能名为**设备互联**（英文 **Device link**）。「Relay」只保留传输层含义：两台设备无法直连时，由 libp2p circuit relay 转发连接。

## 范围

第一版包含：

- 被访问端的开关：允许同账号的其他设备使用本机对话流，默认关闭。
- 每台可用设备对应一个对话流侧边栏入口和 tab。没有可用设备时，侧边栏保持不变。
- 远端 tab 复用对话流界面，目录、Session、消息、运行、模型和文件都读取远端设备，执行也在远端设备上。
- 通过现有的事件游标和重新同步机制实现实时更新和重连。
- 被访问端的对话流用一个标识标出正在被其他设备使用的会话。
- 优先直连，尽量不经中继。

暂不包含：

- 两台设备同时操作同一个 Session 时的协调。不做限制，也不额外提示。
- 设置中的设备列表和逐台管理。侧边栏入口是临时方案。
- Session 跨设备迁移，以及离线设备的展示。

## 交互

### 设置 → 本设备 → 设备互联

仅 Desktop 提供，通过 `extraDeviceTabs` 加在实验室和守护进程旁边。p2p 未启用时不显示。页面只有一个 `Switch`：

| 元素 | zh-Hans | en |
| --- | --- | --- |
| Tab | 设备互联 | Device link |
| 开关标签 | 允许其他设备访问本机对话流 | Allow other devices to use conversations on this device |
| 开关说明 | 同一账号的其他设备可以查看本机对话、读取对话中的文件，并在本机运行智能体。 | Other devices signed in to this account can view conversations on this device, read their files and run agents here. |

保留这句说明，是因为它说明的是权限后果（见开发规范「UI 描述性文案」）。页头等其他位置不再重复。

打开开关时弹出 `AlertDialog`（重要操作确认，见 `packages/ui/docs/dialog.zh.md`）：

- 标题：允许其他设备访问本机？ / Allow other devices to use this device?
- 说明：它们可以在本机运行智能体并读写本机文件。 / They can run agents and read and write files on this device.
- 操作：取消 / Cancel，允许 / Allow。守护进程确认成功后再关闭弹窗；失败时保留弹窗并显示错误，开关保持关闭。

关闭开关不需要确认。其他设备上已打开的远端 tab 会显示「已关闭」状态。

### 侧边栏

- 没有可用设备：只有一个 **对话流 / Conversations** 入口，保持不变。
- 有一台或多台可用设备：先显示 **对话流（本地）**，再为每台设备显示 **对话流（{设备名}）**。「可用」指登录同一账号、当前已连通，并且开启了设备互联。同一台机器上的其他 profile（例如 Canary）是独立设备，也会出现。
- 入口复用 `NAV_ITEM_CLASS_NAME` 和对话流的路由图标，不新增徽标、颜色或说明。设备名过长时截断；悬停时仍能认出选中项。
- 选中状态：现有 `isNavActive` 按前缀匹配，会让「对话流（本地）」在 `/conversations/devices/…` 下同时高亮。本地入口改为排除 `/conversations/devices/` 前缀，设备入口精确匹配自己的路径。
- 设备变为不可用时，入口从侧边栏消失。已打开的 tab 保留，并显示断线状态。

### 远端 tab

- 标题为 **对话流（{设备名}）**；图标与其他 tab 一样，由路由推导。
- 路由：`/{slug}/conversations/devices/{peerId}`。设备必须放在 pathname 中：tab 去重只看 pathname（`tab-store.ts`），`?session=` 等查询参数属于 tab 内的视图状态。
- tab 内容与现有对话流完全一致，不做任何改变。区别只在数据来源和执行位置：都换成远端设备。
- 远端操作归属：

| 操作 | 行为 |
| --- | --- |
| 添加目录、修改会话目录、选择预览文件（`local-v2:choose` 的三种 purpose） | 入口不变，点击后用 Dialog 列出远端设备上的目录或文件：「选择 {设备名} 上的文件夹」/ Choose a folder on {device}。不使用原生选择器。 |
| 点击文件路径、文件树、图片和文档预览 | 从远端设备读取。 |
| 消息中的附件、会话图片 | 通过文件协议从远端读取。 |
| 在输入框添加附件 | 发送前先上传到远端设备。 |
| 文件右键菜单 | 隐藏「在 Finder 中显示」和「打开」，提供「保存到本机」。 |
| 网页预览、Side 浏览器 | 公网地址照常打开，预览实例按设备隔离。回环地址（`localhost`、`127.0.0.1`、`[::1]`）不打开，提示「这是 {设备名} 上的本地地址」。 |
| 智能体打开 Side 会话、文件 tab | 照常在远端 tab 的右侧工作区打开。 |
| 账号或工作区设置确认 | 在发起这轮运行的设备上确认，弹窗标明来自哪台设备，用这台设备的登录凭据应用到云端。 |
| 智能体控制 Side 浏览器 | 在发起这轮运行的设备上执行。 |
| 设备设置（读写偏好、快捷键、守护进程和更新操作） | 拒绝：「这项设置只能在 {设备名} 上修改」。 |
| 导出开发者日志 | 第一版不支持。 |

- 异常提示复用现有的 `LocalRuntimeNotice`，位置和样式不变，只补充远端设备的文案。连接正常时不显示。

| 状态 | 错误码 | zh-Hans | en |
| --- | --- | --- | --- |
| 正在重连 | `device_unreachable` | 与 {设备名} 的连接已断开，正在重连 | Lost connection to {device}. Reconnecting… |
| 被访问端准备中 | `host_not_ready` | {设备名} 正在准备，稍后自动重试 | {device} is getting ready. Retrying shortly… |
| 被访问端已关闭 | `device_link_off` | {设备名} 已关闭设备互联 | {device} turned off Device link |
| 版本不一致 | `incompatible` | 两台设备的 Multica 版本不一致，更新后再试 | Update Multica on both devices to the same version |
| 服务端不一致 | `api_url_mismatch` | {设备名} 连接的是另一个 Multica 服务端 | {device} is connected to a different Multica server |
| 中继下的大文件 | `relay_transfer_limited` | 当前经中继连接，暂不能传输大于 5 MB 的文件 | Files over 5 MB are unavailable while connected through the relay |

被访问端的 Desktop 没有打开时，只要守护进程在运行、登录账号没变，其他设备照常可以使用；被访问端此时看不到使用中标识。

断线期间保留已加载内容和草稿，不把正在运行的任务标记为失败。连接恢复前禁用发送。重连后按 `after_seq` 续传，或重新读取快照；待处理的设置确认和 Side 控制在宽限期内保持有效。被访问端更新时进入 drain，期间发起执行返回 `draining`，复用现有文案。

### 被访问端：使用中标识

被访问端不弹提示，也不限制本机操作，只在对话流的会话列表中标出正在被其他设备使用的会话：

- 标识：会话行的标题右侧显示设备图标（lucide `MonitorSmartphone`），和运行中、置顶等现有状态图标放在一起，不加文字。
- 悬停：Tooltip 显示「{设备名} 正在使用」/ In use on {device}。多台设备时列出所有设备名，以顿号分隔（英文用逗号）。
- 「正在使用」：远端设备打开了这个会话（有订阅），或者远端发起的运行还没结束。远端断开后，宽限期（60 秒）结束时标识消失。
- 只在被访问端显示：本机对话流显示其他设备的使用情况；查看端的远端 tab 不显示。守护进程照常向所有连接推送 presence，是否显示由界面决定。

| 元素 | zh-Hans | en |
| --- | --- | --- |
| Tooltip | {设备名} 正在使用 | In use on {device} |

## 能力归属原则

每项能力按它作用于哪台机器划分：

- **机器**：会话数据、执行、文件、目录、执行环境、端口。实现放在守护进程（`server/internal/localconversation`）。本地 tab 经回环访问，远端 tab 经 p2p 访问，只有一份实现。
- **查看端**：窗口、弹窗、确认、网页渲染、保存到本机。实现留在 Electron，由发起请求的那台查看端处理。

守护进程内部只有一份能力，本机渲染进程、本机主进程和经 p2p 来的其他设备都是调用方，区别只在入口认证和策略表（见「实现」）。host request 只保留需要人操作的请求。今后新增涉及机器数据的能力，写在守护进程中，不写进 Electron 主进程，否则每项能力都要为远端 tab 单独补一次。

## 前置：渲染进程直连守护进程

见[渲染进程直连守护进程](./local-conversation-renderer-direct.md)，先于本方案合入。之后渲染进程直接连本机守护进程（渲染进程 token、WS 票据、文件 key），主进程只做渲染进程做不到的事：原生选择器、设置确认、Side 浏览器、保存到本机、drain。本文在它的基础上增加 `remote` 调用方和 `/local/devices` 转发。

## 数据链路

```text
MacBook（查看端）                                         Mac Studio（被访问端）
渲染进程  设备专属 LocalV2Transport（basePath=/local/devices/{peerId}，context 注入）
  → 回环 HTTP/WS /local/devices/{peerId}/local/v2/...（渲染进程 token、WS 票据或文件 key）
本机守护进程  鉴权 → ReverseProxy，删除 Authorization 和票据参数，去掉 /local/devices/{peerId} 前缀
  → p2p.Dial(peerId, /multica/local-api/1)  ══优先直连══▶  p2p.Listen
                                                          V2.ServeRemote
                                                            → localconversation service
```

渲染进程与守护进程之间的 `/local/v2` 原样承载在 p2p 流上的 HTTP 中。版本检查、WebSocket、按 seq 续传、文件 Range 读取和上传都直接复用。

### 0. 设备发现

1. 守护进程向 p2p-server 登记公钥和地址（含中继地址），并拉取同账号设备生成 allowlist。
2. 连接顺序：局域网 mDNS 或直连，失败时经中继，之后尝试 DCUtR 打洞升级为直连。
3. 被访问端打开开关后注册 `/multica/local-api/1`，通过 identify 推送给其他设备。
4. 查看端 `GET /local/devices` 返回 allowlist 中已连接且支持该协议的设备：`{ peerId, deviceName, connection: direct | relayed }`。Electron 在窗口聚焦时每 5 秒轮询一次，更新侧边栏。

### 1. 握手

```text
打开远端 tab → 渲染进程为该设备创建 transport(basePath=/local/devices/{peerId})
 → GET …/local/v2/hello      version 不同，或 capabilities 不含 device-link → incompatible
 → 请求和 WS 带 scope 请求头（API origin、userId、workspaceId）
       被访问端 scope()：userId ≠ .desktop-user-id → account_mismatch
                        API origin ≠ 被访问端 ServerBaseURL → api_url_mismatch
 → 每个请求和 WS 带 X-Multica-Client-ID（transport 实例生成，重连不变）；被访问端记为 {peerId}/{clientId}
 → WS …/local/v2/ws
 → subscribe_scope {after_scope_seq}；打开会话时 subscribe {after_seq, store_generation}
```

### 2. 读取和执行

```text
渲染进程 LocalV2ScopeClient.request(method, path) → transport(deviceId)
 → 本机守护进程 /local/devices/{peerId}/local/v2/... → ReverseProxy
 → 隧道 → ServeRemote（p2p 入口）→ serve（策略表）→ service → 原路返回
send、answer、入队时，被访问端把该会话的发起方记为这个 clientKey，供第 5 节路由
```

### 3. 实时更新

```text
service 提交事件 → hub 按订阅分发（每个订阅 4 MiB 缓冲，超出则 resubscribe）
 → WS conversation.events / scope.summaries → 隧道 → 查看端守护进程（透传）
 → 渲染进程的 WS → 按 seq 应用
```

v2 推送的是事件本身（含流式文本），不需要「changed → 重新读取」，也不需要在隧道上额外合并。

查看端保存读过的远端快照（桌面 IndexedDB，手机缓存目录，各 40 个，退出登录时删除），打开会话时先显示。桌面随后重新读取，内容未变时 ETag 返回 304；手机从保存的 seq 订阅，无法续传时收到 `snapshot_required` 再读快照。

### 4. 文件

```text
读取：文件 URL = http://127.0.0.1:{port}/local/devices/{peerId}/local/v2/<文件路由>?file_key&api_origin&user_id&workspace_id
 → <img src> 等直接请求 → 本机守护进程校验文件 key，把 scope 还原为请求头 → ReverseProxy（Range 透传）→ 被访问端 serveLocalFile
     文件 key 由本机守护进程校验；被访问端只看 p2p 身份
上传：添加附件时就有最终 id（客户端生成，32 位十六进制），传输在后台进行，发送只等待结果
     计算 SHA-256 → POST …/attachments/link（内容已存在则不再传输）→ 否则 POST …/attachments（X-Attachment-ID）
     从草稿移除 → 中止传输，DELETE …/attachments/{id}；守护进程每 6 小时清理 7 天未被消息或队列引用的上传
     传输持续有进展就不超时，60 秒无进展才算失败；单次上传失败不影响设备可用状态
消息图片：直接用文件 URL，附件响应带 immutable 缓存
保存到本机：文件右键菜单（远端只有「复制路径」「保存到本机」）→ 主进程 showSaveDialog → 主进程用主进程 token 经 /local/devices/{peerId} 读取 /preview/content 并写入本机文件（受预览 10 MB 上限约束）
仅经中继时：读取超过 5 MB（按 Content-Length）或上传超过 5 MB → relay_transfer_limited
```

### 5. 反向请求

```text
被访问端执行中需要查看端（settings.confirm、settings.host、settings.refresh、side.control、developer_logs.export）
 → service.askHost(scope, conversationId, kind) → hosts.request 优先发给该会话发起方 clientKey 的连接
 → 隧道 → 查看端渲染进程的 WS → 按第 ③ 类处理（需要原生能力时经 IPC 交给主进程）→ host.reply → 隧道 → service 继续
```

现在 `hosts.request` 发给该 scope「最后一次 subscribe 的连接」。远端和本机同账号同工作区，属于同一个 scope，所以 B 设备发起的运行可能把确认弹到 A 上。改为按会话的发起方路由，找不到时才使用现有规则。

### 6. 目录和文件选择

```text
本地 tab：local-v2:choose → 原生选择器 → 主进程 POST /folder-projects | /preview/roots | set_directory（不变）
远端 tab：渲染进程远端选择 Dialog
     每层目录：GET …/local/v2/directories?path= → { path, parent, entries: [{ name, path, kind }] }
 → IPC local-v2:choose-remote(deviceId, scope, request, path) → 主进程按 purpose 发出同样的 POST
```

`isLocalV2RendererPost` 禁止渲染进程直接提交这三类路径，保证路径是用户亲手选的。远端没有原生选择器，路径来自渲染进程的 Dialog，这条约束在远端变弱。第一期在同账号信任下接受；`choose-remote` 只接受远端 deviceId，本地 tab 仍只能走原生选择器。

### 7. 断线、迁移到直连和恢复

```text
p2p 断开 → WS 关闭 → transport 断线 → 「正在重连」
 → 该 clientKey 的待处理 host request 保留 60 秒；到期仍未重连则失败，不转给被访问端本机处理（凭据和弹窗属于发起方）
 → libp2p 重新连接 → transport 退避重连 WS（新票据，带原 clientId）→ 渲染进程重新订阅（after_seq）
     宽限期内的 host request 重新下发给新连接
     after_seq 仍可续传 → 补发事件；store_generation 变化或超出 → snapshot_required，重新读取快照
中继升级为直连 → 查看端守护进程关闭走中继的隧道流 → transport 立即重连（不显示断线提示），新流走直连
被访问端关闭开关 → 移除协议并断开远端连接 → device_link_off → 下次轮询时侧边栏入口消失
关闭最后一个该设备的 tab → 关闭该设备的 transport
```

### 8. 使用中标识

```text
远端连接 subscribe {conversation_id} / 远端发起的运行开始或结束 / 远端连接断开且宽限期结束
 → 被访问端 V2 更新 presence：conversation_id → 设备集合（peerId、设备名来自 allowlist）
 → 向本机连接（非远端）的 scope 订阅推送 scope.presence {conversation_id, devices}
本机连接 subscribe_scope 时先收到一次全量 scope.presence
 → 渲染进程保存在 LocalV2ScopeClient，会话列表行读取
```

presence 只在内存中，不写入 SQLite，不占用 `summary_seq`。

## 优先直连

- 直连顺序：局域网 mDNS → 登记的直连地址（含 NAT 端口映射和观察到的公网地址）→ 中继，随后 DCUtR 打洞。
- 建立隧道时：`p2p.Dial` 直接使用现有路径，不等打洞。打洞和直连重试在后台进行，直连出现后再迁移。
- 打洞成功后：守护进程订阅连接事件，出现非 circuit 连接时，关闭 HTTP 连接池里的空闲连接和走中继的已升级 WebSocket，进行中的 HTTP 请求继续在中继上完成；transport 立即在直连上重建 WebSocket。libp2p 新开流时优先选择直连。
- 仍然只有中继时：会话读写照常；单个文件读取或上传超过 5 MB 时返回 `relay_transfer_limited`。
- `/local/devices` 返回 `connection`，并写入诊断日志，便于统计中继占比。侧边栏不显示连接类型。

## 能力清单

远端 tab 涉及的全部读写，按链路分为六类。

### ① `/local/v2` 请求：自动经隧道

| 类别 | 接口 |
| --- | --- |
| 配置和环境 | `GET /providers`、`/models`、`/usage` |
| 目录 | `GET/POST /folder-projects`（POST 只经主进程）、`GET /directory-source`，新增 `GET /directories` |
| 会话 | `GET/POST /sessions`、`GET /sessions/{id}/snapshot`、`POST /sessions/{id}/commands` |
| 队列和 Side | `GET /sessions/{id}/queue`、`GET /sessions/{id}/side-workspace` |
| 相关文件 | `GET /preview/locate|list|file|content`、`POST /preview/roots`（只经主进程）、`GET /sessions/{id}/image` |
| 附件 | `POST /attachments`、`GET /attachments/{id}` |
| 导入 | `GET/POST /imports`、`POST /imports/cancel`（导入被访问端本机的 Claude/Codex 历史） |
| 实时 | `GET /ws` |

远端禁止：`/drain`、`/undrain`、`/stop-providers`（被访问端本机的更新流程）。

### ② 文件：文件 key

文件 URL 带 `file_key` 和 scope 参数，路径带 `/local/devices/{peerId}` 前缀。文件 key 只能 GET 附件、会话图片和预览内容三类路由，由本机守护进程校验。

### ③ 反向请求：由查看端渲染进程按设备处理

| kind | 本地 tab | 远端 tab |
| --- | --- | --- |
| `side.control` | 现有处理 | 照常处理，预览 owner 带 deviceId，`side_workspace_sync` 的 reconcile 按设备进行；回环地址拦截 |
| `settings.confirm` | 现有处理 | 照常确认，弹窗标明来源设备；查看端根据 operation 和 input 重新生成 `apply`，不直接执行被访问端给出的 `apply` |
| `settings.refresh` | 现有处理 | 照常处理 |
| `settings.host` | 现有处理 | 按 operation 分流：界面类（`settings_assert_scope`、`settings_cancel`、`settings_capabilities`、`settings_open`）照常处理；设备类（`settings_read_device`、`settings_update_device`、`settings_set_shortcut`、`settings_device_action`）拒绝，提示只能在该设备上修改 |
| `developer_logs.export` | 现有处理 | 拒绝 `unsupported_host_request` |

不需要确认的写操作和读取由被访问端守护进程直接调用云端（`settings/tools.go`），不经过查看端。

### ④ Electron 主进程直接操作本机的能力

| 能力 | 归属 | 远端 tab |
| --- | --- | --- |
| 目录和文件选择（`local-v2:choose`） | 界面，路径属于机器 | 新增 `local-v2:choose-remote`，见数据链路第 6 节 |
| 文件右键菜单（`local-file-context-menu`） | 机器和界面，无法跨机 | 远端只有「复制路径」和「保存到本机」；菜单 IPC 带 deviceId，主进程读取远端文件后写入本机 |
| 网页预览（`resource-preview`）、Side 浏览器 | 渲染在查看端 | owner 带 deviceId；回环地址拦截 |
| 开发者日志导出 | 守护进程日志属于机器，Web 日志属于查看端 | 第一版不提供 |

### ⑤ 查看端本地状态：key 加 deviceId

| 状态 | 位置 |
| --- | --- |
| scope client | `LocalV2ScopeClient` 的缓存实例（含会话列表、订阅和游标） |
| 输入框草稿 | `composerDraftScope` |
| GUI 偏好（模型、权限模式、推理强度等） | `storage.read/write` 的 `local-provider:` 前缀 |
| 待发送队列 | `desktop-pending-store` |
| 右侧工作区布局 | `local-side-workspace` 的 localStorage |
| 列表布局、项目可见性 | `useConversationLayoutStore`、`useProjectVisibilityStore` |
| Query 缓存 | 本地对话相关的 query key，以及 `resource-folder`、`resource-file` 等单独拼接的 key |
| 网页预览实例 | `resource-preview` 的 owner |

### ⑥ 云端数据：直连

工作区、智能体、任务引用、云端聊天等，两台设备同一账号，查看端直接请求云端。例外：云端任务的 `chat_session_id` 可能指向其他设备上的会话（`agent-task-panel.tsx`）。打开链接时先在本地查找；找不到时依次在可用设备上读取快照，找到则打开对应设备的 tab；都找不到时提示「这个会话在其他设备上」。

## 实现

前置 PR 之后分三个 PR：守护进程 → Desktop 主进程和 preload → 渲染进程和 views。

### 守护进程（Go）

#### p2p 适配器（`server/internal/daemon/p2p/`）

`Service.Handle` 已经检查 allowlist，`Service.Open` 已经带 `WithAllowLimitedConn`，适配器包在这两个函数外面：

```go
// stream_conn.go
type streamConn struct{ network.Stream }                // 实现 net.Conn
func (c streamConn) RemoteAddr() net.Addr               // 返回 peer ID
func (s *Service) Listen(proto protocol.ID) (net.Listener, func())   // Handle 把流推进 chan；返回的 func 调用 RemoveStreamHandler
func (s *Service) Dial(ctx context.Context, p peer.ID, proto protocol.ID) (net.Conn, error)
func (s *Service) OnDirect(fn func(peer.ID))            // 出现到某设备的非 circuit 连接时回调
```

- 不使用 `gostream`：它的 `Listen` 直接调用 `SetStreamHandler`，绕过 allowlist。`Close` 需要处理半关闭（先 `CloseWrite`，读到 EOF 再关闭，参考 gostream 的 `IgnoreEOF`）。
- `Dial`：直接在现有连接上开流。
- `OnDirect` 复用 `addrsLoop` 所在的事件订阅。设备名和连接类型复用 `Peers()` 的 `PeerStatus{DeviceName, Connection}`。

#### 分层：入口 → 策略 → 能力（`daemon/localhost/`）

守护进程的能力只有一份，调用方可能是本机主进程、本机渲染进程，或经 p2p 来的其他设备。入口只负责认证并得出 `caller`，权限由一张策略表决定，能力层不区分调用方。

```text
入口层（认证，产出 caller）
  回环 /local/v2/*：主进程 token → caller{kind: main}
                    渲染进程 token、WS 票据、文件 key → caller{kind: renderer}
  p2p  /multica/local-api/1：gater + allowlist + 开关 → caller{kind: remote, peer}
        peer 由 device-link http.Server 的 ConnContext 从 RemoteAddr 放进 context，不信任请求头
  共同：clientKey = {local | peer}/{X-Multica-Client-ID}
策略层（一张表：caller.kind × 路由 → 允许或拒绝）
能力层（现有 V2 路由和 localconversation service，不区分调用方）
```

```go
type caller struct { kind callerKind; peer peer.ID; clientKey string }
func (v *V2) ServeHTTP(w, r)   // 回环入口：认证 → caller{main|renderer} → v.serve
func (v *V2) ServeRemote(w, r) // p2p 入口：开关 → caller{remote} → v.serve
func (v *V2) serve(w, r, c caller) // allowed(c.kind, route) → 现有路由
```

| 路由 | main | renderer | remote |
| --- | --- | --- | --- |
| 会话、命令、配置、预览读取、附件、导入、`/directories`、WS | ✓ | ✓ | ✓ |
| `ws-ticket` | ✓ | ✓ | ✗（查看端守护进程自己签发） |
| 扩大读取范围：`/folder-projects` POST、`/preview/roots`、`set_directory` | ✓ | ✗ | ✓ |
| `/drain`、`/undrain`、`/stop-providers` | ✓ | ✗ | ✗ |

- remote 可以扩大读取范围：查看端守护进程只在请求带主进程 token 时才转发这三类请求（也就是只能来自查看端主进程的 `choose-remote`），所以策略在查看端的入口执行。
- 所有调用方共用的检查：`scope()` 校验账号；API origin 必须等于 `serverOrigin`，否则返回 `api_url_mismatch`。`NewV2` 增加 `serverOrigin`，在 `daemon/local_conversations.go` 传入 `d.cfg.ServerBaseURL`；否则 `EnsureScope` 会为另一个服务端新建 scope，`contextCloud` 也会返回 nil。
- `wsConn` 记录 `caller`。反向请求路由、宽限期、presence 都只依赖 `clientKey`，与调用方类型无关。
- 仅经中继时，文件读取和上传超过 5 MB 返回 `relay_transfer_limited`。这属于传输层，在查看端的反向代理中处理，不进入能力层。
- 查看端的 `/local/devices/{peerId}/...` 反向代理也走回环入口认证。它只做转发，不是能力。

#### 反向请求路由（`daemon/localhost/v2_host.go`、`localconversation/service`）

- `clientKey`：渲染进程为每个 transport 实例生成一个 UUID，HTTP 和 WS 都放在 `X-Multica-Client-ID` 请求头中，重连时自然沿用。服务端加上前缀：本机是 `local/`，远端是 `{peerId}/`，所以远端伪造不出本机的 key。
- 按会话路由：`ContextBinder` 按 provider 进程绑定，settings 工具的 `Host` 闭包拿不到 turn，所以不按 turn 路由。`V2` 在内存里维护 `conversationID → clientKey`，在 `send`、`answer`、入队时更新为发起方；service 的 `Host` 带上 conversationID。`askHost` 增加 `conversationID` 参数，`hosts.request` 先找该 clientKey 的连接，找不到时沿用「最后一次 subscribe 的连接」。两台设备同时操作同一个会话不处理，所以按会话路由足够。
- 宽限期：现在 `hosts.attach` 的 detach 会立即让该连接的待处理请求失败。改为移入 `orphaned[clientKey]` 并计时 60 秒；同一 clientKey 的新连接 attach 时，用原 `request_id` 重新发送 `host.request`；到期才返回 `ErrHostUnavailable`。

#### 使用中标识

- `V2` 维护 `presence: conversationID → clientKey → {peerId, deviceName}`。
  - 加入：远端连接 `subscribe` 这个会话。远端 clientKey 发起的运行在结束前也算在内：发送前先标记（避免轮次瞬间结束时漏清），service 的 `OnTurnEnd` 在轮次结束时清除。
  - 移除：`unsubscribe`、运行结束（且没有订阅），或连接断开后宽限期到期。
- 每次变化推给所有有 scope 订阅的连接，与调用方类型无关；条目中排除接收方自己的 clientKey：

  ```json
  {"version": 2, "type": "scope.presence", "conversation_id": "…", "devices": [{"peer_id": "…", "device_name": "…"}]}
  ```

- `wsSubscribeScope` 成功后先发一次全量。presence 只在内存中，不写入 SQLite，不占用 `summary_seq`。
- `protocol/schema/local-v2.schema.json` 增加这条消息，并补 valid 和 invalid 样例。

#### 开关、设备列表和反向代理（新文件 `daemon/device_link.go`）

- 开关保存在 profile 目录的 `device-link.json`。
  - 打开时：`svc.Listen("/multica/local-api/1")`，用 `http.Server{Handler: d.localV2.ServeRemote, ConnContext: withPeer, ReadHeaderTimeout: 10s, IdleTimeout: 90s}` 服务。
  - 关闭时：调用 `Listen` 返回的 stop、`srv.Close()`，并断开所有远端 `wsConn`。
  - p2p 未启用或守护进程不提供本地对话时，接口返回 `unavailable`。
- 在 `health.go` 的 mux 上注册以下接口，按前置改造的规则鉴权（渲染进程 token、WS 票据或文件 key；`choose-remote` 的 POST 需要主进程 token）：
  - `GET`/`PUT /local/device-link`。
  - `GET /local/devices`：`Peers()` 中已连接、且 `Peerstore().SupportsProtocols` 包含该协议的设备，返回 `{ peerId, deviceName, connection }`。
  - `/local/devices/{peerId}/...`：每台设备一个 `httputil.ReverseProxy`。

```go
&httputil.ReverseProxy{
    Rewrite: func(r *httputil.ProxyRequest) {
        r.Out.URL.Scheme, r.Out.URL.Host = "http", "device-link"
        r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, "/local/devices/"+peerID)
        r.Out.Header.Del("Authorization") // Admit 还删除 Origin、ticket 和文件 key 参数
    },
    Transport:     svc.NewTunnel(peerID, deviceLinkProtocol, &http.Transport{IdleConnTimeout: 90 * time.Second}),
    FlushInterval: -1,
    ErrorHandler:  deviceLinkError, // 502 device_unreachable / 503 device_link_off / 404 device_unknown
}
```

- WebSocket upgrade 由 `ReverseProxy` 原生转发。代理的 Transport 是 `p2p.Tunnel`：它记录每台设备的流，用 `httptrace` 和 101 响应识别已升级的连接；`OnDirect` 回调时调用 `MoveToDirect()`，关闭空闲连接和走中继的已升级连接。
- 仅经中继时：上传在转发前按请求的 `Content-Length` 检查，读取在 `ModifyResponse` 中按响应的 `Content-Length` 检查，超过 5 MB 返回 `relay_transfer_limited`。
- 错误格式与 v2 一致：`{ error: { kind, message } }`。

#### 其他

- `GET /local/v2/directories?path=`：service 新增 `ListDirectories`，只列目录，跳过隐藏项，空路径时返回 home 目录。所有调用方可用。
- `hello` 的 `capabilities` 增加 `device-link`。

### Desktop 主进程和 preload

在前置改造之后，主进程只增加做不到直连的部分：

- 远端 host request 的原生部分：复用 `localV2HostHandler`（`local-v2-host.ts`），按第 ③ 类处理：`settings.confirm` 由 `packages/core/settings/operations.ts` 根据 operation 和 input 重新生成 `apply`；设备类 `settings.host`、`developer_logs.export` 拒绝；`side.control` 的预览 owner 带 deviceId。
- `local-v2:choose-remote(deviceId, scope, request, path)`：拒绝 `"local"`，用主进程 token 向 `/local/devices/{peerId}/local/v2/...` 发出与 `local-v2:choose` 相同的 POST。
- 文件右键菜单 IPC 带 deviceId：远端文件显示「保存到本机」，`showSaveDialog` 后用主进程 token 流式读取并写入本机。
- 远端的设置确认弹窗写明「Requested by an agent on {设备名}」，设备名由主进程查 `/local/devices`。
- 远端的 `side.control` 由主进程补上 deviceId，快照和轮次检查发往该设备，命令交给该设备页面或全局 bridge；Side 布局按设备分开存储。
- 网页预览和 Side 控制遇到远端的回环地址（localhost、127.0.0.0/8、[::1]、0.0.0.0）时不打开，显示「这是 {设备名} 上的本地地址」。
- `local-file-context-menu`、`side-workspace`、`resource-preview` 的 owner 带 deviceId。
- `device-link:get`、`device-link:set`、`device-link:devices`。

### 渲染进程和 views

- 直连 transport 增加 `basePath`（本机为空，远端为 `/local/devices/{peerId}`）和 `clientId`（实例生成的 UUID，放在 `X-Multica-Client-ID`）。错误 `device_unreachable`、`device_link_off`、`api_url_mismatch` 进入 availability。
- `LocalDeviceProvider` 提供 `{ deviceId, deviceName, transport }`。`LocalConversationFlowPage` 从路由读取 peerId 后提供；`useLocalDevice()` 在设备页面外返回本机（全局 bridge 和设置页等不在设备页面内）。以下 6 处直接读取 `window.desktopAPI` 的地方改为使用它：`local-conversation-flow.tsx`、`local-resource-preview.tsx`、`local-runtime-notice.tsx`、`local-side-workspace.tsx`、`side-workspace-bridge.tsx`、`use-local-v2-conversation.ts`。
- `LocalV2ScopeClient` 的缓存 key 改为 `(deviceId, scope)`；`LocalConversationScope` 不增加字段。第 ⑤ 类的其他 key 改为显式带 deviceId。
- 使用中标识：`localV2ServerMessageSchema` 增加 `scope.presence`；`LocalV2ScopeClient` 增加 `presence(id)` 和变更通知；会话列表行显示 `MonitorSmartphone` 图标和 Tooltip。
- 路由 `/{slug}/conversations/devices/{peerId}`；`paths.ts` 增加对应路径函数。
- `app-sidebar` 增加平台插槽，由 Desktop 注入「对话流（本地）」和「对话流（{设备名}）」，并按上文规则判断选中状态。
- 远端目录和文件选择 Dialog（调用 `/directories`，确认后调用 `choose-remote`）；设置「设备互联」tab 和 AlertDialog；`LocalRuntimeNotice` 按设备订阅并补充远端文案；翻译（en、zh-Hans、zh-Hant、ja、ko、fr）。
- 云端任务会话链接的跨设备查找。

## 安全边界

| 层级 | 检查 |
| --- | --- |
| 查看端回环接口 | loopback Host；主进程 token 不能带 Origin；渲染进程 token、WS 票据和文件 key 只接受 Desktop 的 origin，且不能访问 drain 和扩大读取范围的接口 |
| 转发前 | 删除本机 token、票据、文件 key 参数和 Origin；把票据或文件 key 参数里的 scope 和 clientId 还原为请求头 |
| p2p 连接 | gater 只放行同账号设备，基于握手时证明的公钥身份 |
| 被访问端入口 | 开关打开才注册协议；路径白名单；禁止 drain 和停止 provider；每次请求校验 userId 和 API origin |
| 被访问端执行环境 | 使用被访问端守护进程自己的环境，远端不能传入 env |

已知风险（第一版接受）：

- 信任范围是「同一账号」。p2p-server 接受任何能通过 `/api/me` 的 token（包括 `mat_` 智能体任务 token 和 PAT），开关也只受本机 loopback token 保护。某台机器上被注入的智能体可以注册伪设备并打开开关，从而在该账号的其他已开启设备上执行任务和读取文件。
- 远端选择的目录来自查看端渲染进程，不经原生选择器。

第二期收紧。

## 实施顺序

0. 前置 PR：[渲染进程直连守护进程](./local-conversation-renderer-direct.md)。
1. 守护进程：p2p 适配器和直连优先、开关、`remote` 入口和策略表的 remote 列（含 origin 校验）、`clientKey` 路由和宽限期、`/directories`、`/local/devices` 和反向代理、错误码。
   - 验收：用同一台机器的两个 profile，以 curl 和 websocat 验证 hello、WS 长连接、Range 读取和上传，重点覆盖 `ReverseProxy`、WebSocket 和 libp2p stream 半关闭的组合；被访问端 Desktop 关闭时，用 curl 发消息，确认 Claude 和 Codex 都能运行；开关打开后，查看端 `/local/devices` 多久能看到该设备（identify 推送）。
2. Desktop 主进程和 preload：`choose-remote`、保存到本机、远端反向请求的原生部分和 settings 分流、owner 带 deviceId、设备轮询、开关 IPC。
3. 渲染进程和 views：transport 的 `basePath` 和 `clientId`、设备 context、路由、侧边栏插槽和选中规则、第 ⑤ 类 key、远端选择 Dialog、右键菜单和网页预览的远端行为、设置 tab、提示文案、翻译、云端任务会话链接。
4. 验证：
   - Go：presence 的加入、宽限期后移除、排除接收方自己；适配器（含半关闭）、策略表（三种 caller × 路由）和 origin 校验、反向代理和错误码、`clientKey` 路由（两条连接同 scope 时确认发往发起方）、宽限期、直连迁移。
   - Desktop 主进程：`choose-remote` 拒绝本机 deviceId、设置确认不使用转来的 `apply`。
   - 渲染进程：使用中标识、侧边栏两种状态和选中规则、远端 tab、key 隔离、远端目录选择、回环地址提示、Side 控制按设备分流。
   - 未做：两台真实设备（同网和跨网）的手动验证与截图。
   - 手动：先用同一台机器的两个 profile，再用两台不同网络的机器验证打洞和中继回退，记录 `connection` 的直连占比；与中继服务端确认 `Limit=nil` 和带宽成本。浅色和深色主题截图保存在 `.tmp/device-link/`。

## 第二期

- 收紧信任：p2p-server 只接受 Desktop 和守护进程的登录 token；被访问端按设备逐台批准；开关只能从 Desktop 界面修改。
- 端口转发（`/multica/port-forward/1`，只转发被访问端的回环端口），支持预览被访问端的 dev server。
- 从被访问端获取守护进程日志。
- 设备列表改为推送。
