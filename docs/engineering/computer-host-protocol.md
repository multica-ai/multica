# Computer Host 协议：Desktop 与 daemon 解耦

状态：已实现。Computer Use 的整体说明见 [computer-use.md](computer-use.md)。

## 目标

- Desktop（Electron）和 daemon 生命周期解耦：任一方单独升级、重启，另一方无需重启，能力以当前连接上报的为准。
- Computer Use 的可变部分，即 JS SDK、提示词、原生实现和授权规则，归 Desktop 所有，随安装包升级。daemon 只提供稳定外壳：MCP 路由、沙箱 REPL、任务注入。
- 每项能力独立声明 semver 版本，版本不兼容时该能力不可用，不做兼容适配。
- 普通智能体任务自动授权的前提是：执行任务的 daemon 账号与当前登录 Desktop 的账号相同。

不在范围内：远端设备间转发 Computer Use、锁屏操作、非 macOS 平台。

## 角色

| 角色 | 进程 | 职责 |
| --- | --- | --- |
| Desktop | Electron 主进程 | 连接 daemon；上报能力、提示词、REPL 运行时和当前用户；Broker 授权；原生执行（cua-driver） |
| daemon | Go | 校验并缓存能力；决定是否给会话、任务挂 Computer Use；运行沙箱 REPL；转发动作 |
| 智能体 | Codex / Claude 等 CLI | 通过 MCP 调用 `computer_status / computer_execute / computer_reset` |

## 传输与认证

- **通道**：沿用 `GET /local/v2/computer-host` WebSocket。只有带 profile 本地 API token 的 Desktop 主进程能连（`callerMain`，见 `server/internal/daemon/localhost/caller.go`）；同一时间只允许一个连接，第二个连接返回 409。这些规则不变。
- **格式**：每个 WebSocket 文本帧是一条 JSON-RPC 2.0 消息，单帧不超过 1 MiB，不使用批量消息。
- **身份**：通道认证只证明对方是本机 Desktop 主进程，不证明 Desktop 登录的是哪个账号。账号由 `initialize` 上报，与 daemon 账号比对，见下文"账号认证"。

## 连接生命周期

```
WebSocket 建立
  │  Desktop → initialize（请求）
  │  daemon  → initialize 响应：各能力 enabled / incompatible / unavailable
  │  Desktop → notifications/initialized
  ▼
ready ── Desktop → notifications/client/updated（用户或能力变化，整体替换）
  │      daemon  → computer/control（请求）       Desktop → 响应
  │      daemon  → notifications/cancelled
  │      Desktop → notifications/computer/stopped
  ▼
WebSocket 关闭：daemon 作废全部能力，停止进行中的执行（StopComputerHost）
```

daemon 侧状态：

| 状态 | 进入条件 | Computer Use |
| --- | --- | --- |
| `connected` | WebSocket 建立 | 不可用。不设超时；旧 Desktop 不发 `initialize`，就一直停在这里 |
| `initialized` | `initialize` 成功响应 | 不可用，等待 `notifications/initialized` |
| `ready` | 收到 `notifications/initialized` | 按能力状态决定 |
| `closed` | 连接关闭 | 不可用，能力描述作废 |

规则：

- 在 `ready` 之前收到其他方法，返回错误 `-32002 not_initialized`。重复发送 `initialize` 返回 `-32600 already_initialized`。
- daemon 在 `ready` 之前不发送任何请求。
- Desktop 发出 `initialize` 后 5 秒内没有收到响应，就判定 daemon 不支持本协议：关闭连接，30 秒后重试，并记录日志。这样旧 daemon 不会让新 Desktop 卡住。

## 消息定义

### initialize（Desktop → daemon，请求）

```json
{ "jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
  "protocolVersion": "1.0.0",
  "clientInfo": { "name": "multica-desktop", "version": "0.7.2" },
  "user": { "id": "4f6c…" },
  "capabilities": {
    "computer": {
      "version": "1.0.0",
      "prompts": { "conversation": "…", "task": "…", "taskBrief": "…" },
      "runtime": { "executable": "/Applications/Multica.app/Contents/MacOS/Multica",
                   "entry": "/Applications/Multica.app/Contents/Resources/local-conversation-host/computer-host.cjs" }
    }
  } } }
```

| 字段 | 约束 |
| --- | --- |
| `protocolVersion` | 通道本身的 semver，按下文"兼容规则"判断；不兼容时整个 `initialize` 返回错误 |
| `user.id` | Desktop 当前登录用户；未登录为 `null` |
| `capabilities.computer.version` | Computer 能力的 semver |
| `prompts.conversation` | AI 会话系统提示词中的 Computer Use 段（含"每个应用需桌面授权"） |
| `prompts.task` | 任务 `multica_computer` MCP 的 `instructions`（含"任务自动授权"） |
| `prompts.taskBrief` | 追加到任务提示词末尾的 `[Computer Use]` 段 |
| `prompts.*` 通用 | 英文；每份不超过 16 KiB 的 UTF-8 文本，daemon 原样使用，不拼接也不改写 |
| `runtime.*` | 绝对路径，必须是已存在的普通文件；daemon 用它启动沙箱 REPL，并据此推导沙箱的只读路径 |

### initialize 响应（daemon → Desktop）

```json
{ "jsonrpc": "2.0", "id": 1, "result": {
  "protocolVersion": "1.0.0",
  "serverInfo": { "name": "multica-daemon", "version": "0.7.2" },
  "user": { "id": "4f6c…" },
  "capabilities": {
    "computer": { "version": "1.0.0", "status": "enabled" }
  } } }
```

| `status` | 含义 |
| --- | --- |
| `enabled` | 版本兼容且字段有效；AI 会话可用，任务是否可用另看账号 |
| `incompatible` | 版本不兼容，附带 `supported`（例如 `"1.0.x"`） |
| `invalid` | 字段校验失败，附带 `reason` |

响应里的 `user.id` 是 daemon 账号，未知时为 `null`。Desktop 只用它显示诊断信息，不据此做授权决定。

### notifications/initialized（Desktop → daemon）

无参数。收到后 daemon 进入 `ready`。

### notifications/client/updated（Desktop → daemon）

参数与 `initialize` 的 `user`、`capabilities` 相同，**整体替换**当前描述。用于 Desktop 切换账号、退出登录、修改设置，不需要重连。daemon 重新校验，不回应答；账号或能力发生变化时，停止按旧描述进行中的执行。

### computer/control（daemon → Desktop，请求）

```json
{ "jsonrpc": "2.0", "id": "42", "method": "computer/control", "params": {
  "capabilityVersion": "1.0.0",
  "envelope": {
    "op": "action", "conversationId": "task:8c1e…", "turnId": "8c1e…",
    "origin": "task",
    "scope": { "userId": "4f6c…", "workspaceId": "…" },
    "action": { "op": "click", "app": "…", "observationId": "…", "ref": "…" }
  } } }
```

- `capabilityVersion`：**发出这个动作的 REPL 启动时**的 Computer 能力版本。Desktop 按兼容规则和自己的当前版本比较，不兼容就返回 `runtime_outdated`。
- `envelope`：沿用现有信封（`op` 为 `status / begin / action / release`）。`origin` 只有 `"task"` 一个取值，省略表示 AI 会话。
- 响应：成功为 `result`，内容是原生结果。失败为 JSON-RPC 错误，见下文"错误"。

### notifications/cancelled（daemon → Desktop）

`{ "requestId": "42" }`，与 MCP 同名同义。Desktop 中止对应请求并停止原生输入。

### notifications/computer/stopped（Desktop → daemon）

`{ "conversationId": "…", "turnId": "…" }`。用户在菜单栏或用快捷键停止后发送；daemon 终止对应 REPL 执行（`StopComputer`）。

## 兼容规则

所有版本号都是 semver：

- **兼容**：`major` 和 `minor` 都相同。只有 `patch` 不同时可以混用。
- **不兼容**：`major` 或 `minor` 不同。

适用于三处：

| 比较对象 | 不兼容时 |
| --- | --- |
| `initialize.protocolVersion` 与 daemon 的通道版本 | 整个 `initialize` 返回错误，连接保持在 `connected` |
| `capabilities.computer.version` 与 daemon 支持的 Computer 版本 | 该能力 `incompatible`，不影响其他能力 |
| `computer/control.capabilityVersion` 与 Desktop 当前 Computer 版本 | 返回 `runtime_outdated`，daemon 重建 REPL |

版本号的维护规则：

- 契约有任何变化（方法、参数 schema、信封字段、SDK 动作列表）都必须升 `minor` 或 `major`。
- 契约不变、只改实现或提示词时升 `patch`。
- 由契约锁强制执行，见下文"契约锁"。

## 账号认证

### daemon 账号

- daemon 启动并加载令牌后，用自己的令牌调用一次 `GET /api/me`，缓存返回的 `id`。令牌只在 daemon 重启时更换，所以只需获取一次。
- 调用失败时账号记为未知，每 60 秒重试一次。账号未知时不给任务注入 Computer Use。
- 令牌类型无法调用 `/api/me` 时（例如非 PAT 的 daemon 令牌），结果同样是账号未知，即任务不可用。

### 比对与执行

| 位置 | 检查 |
| --- | --- |
| 任务启动时（daemon） | Computer `enabled`，daemon 账号已知，且等于 Desktop 上报的 `user.id`。任一条件不满足就不注入，并记录原因 |
| 每次 `computer/control`（Desktop Broker） | `scope.userId` 必须等于 Desktop 当前用户，AI 会话和任务都一样。任务的 `scope.userId` 填 daemon 账号 |
| Desktop 切换账号 | Desktop 发送 `notifications/client/updated`；Broker 的会话代次变化，撤销当前控制；daemon 停止为新任务注入 |

这样会删掉现在"任务只要求 Desktop 已登录"的特例。两条路径统一为"信封账号必须等于当前 Desktop 账号"；`origin: "task"` 只决定是否弹授权框。

## 注入决策

| 时机 | 条件 | 行为 |
| --- | --- | --- |
| AI 会话启动（绑定 `multica_desktop_context`） | Computer `enabled` | 挂三个工具；系统提示词加入 `prompts.conversation` |
| 普通智能体任务启动 | 见上文"账号认证" | 绑定 `multica_computer` 路由，令牌在 URL 路径中；MCP `instructions` 用 `prompts.task`；任务提示词末尾追加 `prompts.taskBrief` |
| 其他情况 | — | 不挂工具，也不出现任何 Computer Use 提示词 |

提示词在会话或任务**启动时**定下，运行期间不变：provider 的系统提示词和 MCP `instructions` 在会话期间都是固定的。新提示词从下一个会话或任务开始生效。

已绑定的路由在 Desktop 断开期间保留，调用返回 `desktop_unavailable`；Desktop 重新进入 `ready` 后恢复可用。

## REPL 运行时

- 每个 REPL 进程记录启动时使用的 `runtime` 路径和 Computer 版本。
- 执行 `computer_execute` 时：
  1. Computer 不是 `enabled`：返回 `desktop_unavailable`，不启动 REPL。
  2. REPL 的版本与当前版本不兼容：先重建 REPL，结果中带上 `"runtime": { "restarted": true, "reason": "capability_updated" }`，并在说明里提示变量已清空。
  3. 只有 `patch` 不同：保留 REPL 和其中的变量。
- 收到 `runtime_outdated`：重建 REPL，本次调用返回该错误，不自动重放。
- 沙箱只读路径按启动时的 `runtime` 推导；不再从 daemon 启动参数（`local_conversations.go` 里的 `computer.Command`）读取。

## 错误

JSON-RPC 错误码：

| code | message | 场景 |
| --- | --- | --- |
| -32700 | parse_error | 帧不是合法 JSON |
| -32600 | invalid_request / already_initialized | 结构错误或重复 `initialize` |
| -32601 | method_not_found | 未知方法 |
| -32602 | invalid_params / protocol_incompatible | 参数校验失败、通道版本不兼容 |
| -32002 | not_initialized | `ready` 之前收到业务方法 |
| -32000 | 业务错误 | `computer/control` 的领域错误，`data.code` 给出机器可读的码 |

`computer/control` 的 `data.code` 沿用现有领域错误：`app_not_allowed`、`app_forbidden`、`app_busy`、`native_restarted`、`account_changed`、`device_busy`、`execution_ended`、`permission_required`、`stale_observation`、`background_unavailable` 等，新增 `runtime_outdated`。错误的 `message` 保持 `<code>: <说明>` 的格式，daemon 原样交给模型。

## 契约锁

- 契约文件：`packages/computer-use/contract/computer.json`，包含 `version`、通道方法、`computer/control` 参数 schema、信封字段、SDK 动作列表。
- 提示词：`packages/computer-use/prompts.mjs`，Desktop 主进程直接导入并在 `initialize` 中上报。
- 锁文件：`packages/computer-use/contract/computer.lock.json`，按顺序记录每个已发布版本的 `{ version, contractSha256, promptsSha256 }`（`history`）。
- 测试（TS 和 Go 各一份，Go 测试按相对路径读取仓库中的契约文件）：
  - TS 常量 `COMPUTER_CAPABILITY_VERSION`、Go 常量 `computerCapabilityVersion` 都等于契约的 `version`。
  - Broker 的 `actionSchema` 动作列表和信封字段等于契约；Go 构造的信封字段等于契约。
  - 契约哈希变了，而 `major.minor` 与锁记录相同：失败，提示需要升 `minor` 或 `major`。
  - 提示词哈希变了，而版本与锁记录完全相同：失败，提示至少升 `patch`。
  - 历史中每相邻两条也按上述规则检查，最后一条必须与当前文件一致。
  - 升版本后运行 `node packages/computer-use/contract/update-lock.mjs` 追加记录，随代码一起提交；没有按规则升版本时脚本拒绝写入。

## 重启与故障场景

| 场景 | 结果 |
| --- | --- |
| Desktop 重启，daemon 不动 | 断开时作废能力，进行中的执行停止；重连后进入 `ready`，能力以新 Desktop 为准。已绑定的路由在断开期间返回 `desktop_unavailable`，之后恢复 |
| Desktop 升级 patch 版本 | 兼容；REPL 保留 |
| Desktop 升级 minor 或 major 版本 | daemon 支持新版本时：下一次执行重建 REPL。不支持时：`incompatible`，Computer Use 不可用，直到 daemon 升级 |
| daemon 升级重启，Desktop 不动 | Desktop 重连并重发 `initialize`；兼容就继续可用 |
| 旧 Desktop 连新 daemon | 旧 Desktop 不发 `initialize`，停在 `connected`，Computer Use 不可用，不会断开重连 |
| 新 Desktop 连旧 daemon | `initialize` 5 秒内无响应，Desktop 断开，每 30 秒重试；Computer Use 不可用 |
| Desktop 切换账号 | `notifications/client/updated` 生效；当前控制被撤销；账号不一致的 daemon 不再给任务注入 |
| `/api/me` 失败 | daemon 账号未知，任务不注入，AI 会话不受影响 |

## 实现步骤

1. **契约与常量**：新增契约文件、锁文件和两侧的契约测试；`computer` 版本定为 `1.0.0`，通道版本定为 `1.0.0`。
2. **Desktop**
   - `computer-use/channel.ts` 改成 JSON-RPC：连接后发 `initialize` 和 `notifications/initialized`；账号或设置变化时发 `notifications/client/updated`；处理 `computer/control` 和 `notifications/cancelled`；用户停止时发 `notifications/computer/stopped`；5 秒握手超时，30 秒退避重试。
   - 新增 `computer-use/prompts.ts`：从 Go 搬来的 `conversation`、`task`、`taskBrief` 三份英文提示词。
   - `broker.ts`：信封的 `userId` 改为必填并严格比对；删除任务的"只要求已登录"特例；检查 `capabilityVersion`。
3. **daemon**
   - `localhost/computer_host.go`：实现状态机、JSON-RPC 编解码、能力校验和缓存、`computer/control` 请求。
   - daemon 客户端：新增 `/api/me` 调用和账号缓存，令牌更换后刷新，失败后重试。
   - `service`：
     - 能力描述替代 `ComputerInstructions` 和 `TaskComputerInstructions` 常量；
     - `instructions.go` 按能力状态拼入 `prompts.conversation`；
     - `BindTaskComputer` 增加账号判断；
     - REPL 记录版本，按兼容规则重建；
     - `computer.Manager` 改为按能力描述里的 `runtime` 启动。
   - `daemon/computer_mcp.go` 与 `prompt.go`：任务提示词改用 `prompts.task`。
4. **文档**：更新 [computer-use.md](computer-use.md) 中的分层、授权和生命周期部分；本文状态改为"已实现"。

## 测试

- Go：状态机（未初始化即拒绝、重复初始化、`notifications/client/updated` 整体替换）；兼容规则矩阵；`/api/me` 成功、失败、令牌更换；注入决策矩阵（能力状态 × 账号）；REPL 按版本重建，`patch` 不同时保留；断开时作废能力。
- TS：通道握手与超时退避；Broker 的账号比对和 `capabilityVersion` 检查；任务自动授权；`notifications/cancelled` 能中止原生动作。
- 契约测试：两侧版本常量、动作列表、信封字段，以及锁文件规则。
- 默认测试使用假的原生动作，不操作真实应用，也不调用真实模型。

## 已定

- 普通智能体任务默认可以使用 Computer Use，Desktop 不提供开关；支持任务是 Computer 1.0.0 能力的一部分，协议中不设 features。
- 提示词只用英文，供模型阅读，不随界面语言本地化。
