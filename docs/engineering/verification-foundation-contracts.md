# 自动化验证底层接口与实施契约 v2

本文是验证宿主的接口约定。实际参数以 [MCP 适配器](../../packages/verification/src/mcp/server.ts) 和 [resource schema](../../packages/verification/src/contracts/resources.ts) 为准，使用说明见[验证包说明](../../packages/verification/README.md)。

## 1. 原则

- **只提供操作和取数。** 宿主准备和启停环境、导入数据、控制 fake provider、操作页面，并原样返回环境、应用、事件和页面数据。它不计算"通过/失败"，不维护验证计划、结论或报告，也不评估采集是否完整。这些判断属于调用方。
- **接口互不依赖。** 每个工具只要求自身参数，不要求先建立验证记录、开始步骤或先观察页面。资源 ID（environmentId、dataSnapshotId）是参数，不是调用顺序约束。
- **异步采用 start/query。** 需要较长时间的操作返回 job，调用方用 `job_query` 读取；其余操作同步返回结果。
- **只有 MCP。** 一个 24 小时常驻的守护进程在固定地址 `http://127.0.0.1:47311/mcp` 提供 MCP（Streamable HTTP），不提供 CLI 或其他 API；根路径是给人看的管理台。两者不需要凭据，只服务发往该本机地址的请求，管理台的写操作要求来自管理台页面。

## 2. 模块

| 模块 | 代码 | 职责 |
| --- | --- | --- |
| 守护进程 | `src/daemon/` | 固定端口的 HTTP 服务（`/mcp` 与管理台）、健康巡检、空闲回收 |
| MCP | `src/mcp/` | 工具注册与协议转换，不持有业务状态 |
| 任务 | `src/host/jobs.ts` | 异步操作的收据、requestId 去重、状态和取消 |
| 环境 | `src/setup/` | 设备自检、远端分支镜像与 worktree、构建缓存、进程生命周期、环境名额 |
| 数据 | `src/data/` | 来源导出快照、导入副本、模板数据、fake provider 控制 |
| 状态 | `src/state/` | 按来源读取当前状态和事件 |
| 页面 | `src/page/` | 通过 CDP 连接同一应用的窗口，观察、读取、操作和截图 |

底层运行在独立 Node 宿主中。Electron API 和诊断入口留在 Desktop main/preload/renderer，Go 状态读取与 fake provider 留在 server 的 `verificationdev` 文件，生产构建排除这些实现。

## 3. 公共结果

```ts
type ReadResult<T> = { schemaVersion: 1; observedAt: string } & (
  | { kind: "ok"; value: T }
  | { kind: "error"; problem: Problem }
);

type CommandResult<T> = { schemaVersion: 1; receiptId: string; requestId: string } & (
  | { kind: "accepted"; jobId: string }
  | { kind: "completed"; value: T }
  | { kind: "rejected" | "unknown"; problem: Problem }
);

interface Problem {
  code: ErrorCode;
  message: string;
  recovery: "none" | "inspect" | "sameRequest" | "reobserve" | "userAction";
  relatedRefs: string[];
}
```

同步工具返回 `ReadResult`。异步工具返回 `CommandResult`：首次提交为 `accepted`；同一 requestId、同一输入再次提交返回原收据（任务已结束时为 `completed`、`rejected` 或 `unknown`）；同一 requestId 的不同输入返回 `requestConflict`。

`Problem` 描述的是调用失败，例如参数无效、来源不可达、身份不匹配。它不表达被测行为是否正确。

## 4. 异步任务

| 操作 | 输入 | 输出 |
| --- | --- | --- |
| `env.start` | branch、mode、dataMode，可选 owner、idleTimeoutMinutes、sourceRef、apiOrigin | 环境记录 |
| `env.stop` | environmentId | 环境记录 |
| `data.snapshot` | sourceRef、selection | 快照清单（不含内容） |
| `data.restore` | environmentId、content（snapshot / conversationTemplate / workspaceTemplate） | 副本：datasetId、映射、转换列表 |

- job 的 `resourceRefs` 在创建资源时立即写入。`env.start` 在准备开始时写入 environmentId，调用方无需等任务完成即可读环境状态和日志。
- `env.start` 在应用启动后自动登录本地测试账号并进入 `verification` 工作区，不需要人工操作；`env.stop` 会取消同一环境未完成的 `env.start`。
- `branch` 是远端分支名或可达 commit。守护进程在自己的部分克隆 `<state-root>/repository.git` 中拉取并固定 commit，再建独立 worktree；只验证已推送的代码。
- 守护进程是唯一宿主，所有 MCP 会话共享环境、任务和快照。环境持续到 `env_stop`、管理台停止或空闲超时（默认 120 分钟无 MCP 调用，`idleTimeoutMinutes` 可覆盖）；任何带 `environmentId` 的调用都刷新使用时间。
- 同时运行的环境数有上限（默认 2）。满额时 `env.start` 失败为 `instanceBusy`，并列出占用环境及其 owner；启动阶段按顺序执行。
- 守护进程使用固定的宿主目录。重启或被强制结束后，下一次启动接管该目录：仍在运行的 Desktop 环境继续保留，过渡中的环境和 `liveReadOnly` 环境被停止并清理。
- 宿主重启后，未完成的任务标为 `interrupted`，不自动重放。响应丢失时按原 requestId 查询，不换 ID 重放。
- `job_cancel` 请求协作取消，不撤销已产生的资源。

## 5. 同步工具

| 工具 | 输入 | 返回 |
| --- | --- | --- |
| `env_inspect` | mode | 工具版本、设备条件、准备步骤 |
| `env_status` | environmentId | 环境记录，以及 application、daemon、backend 的当前可达性和身份 |
| `env_log_read` | environmentId、日志名、offset、limit | 文本页、`truncated` |
| `data_snapshot_read` | dataSnapshotId、section、cursor、limit | 清单页 |
| `fixture_control` | environmentId、conversationId、action、text、expectedRequestId | fake provider 的状态或操作结果 |
| `state_read` | environmentId、source、subjectRef、beforeTurnIndex | 来源原始 payload |
| `events_read` | environmentId、source、subjectRef、after | 来源原始事件页 |
| `page_windows` | environmentId | 窗口 ID（CDP targetId）、标题、URL |
| `page_observe` | environmentId、windowId、frameUrl、offset、limit | 可见的可交互元素 |
| `page_read` | environmentId、target、limit | 匹配数量，以及每个匹配的可见性、可用性、文本、值、位置 |
| `page_action` | environmentId、target 或坐标、action | `{ performed: true }` |
| `page_screenshot` | environmentId、windowId、fullPage | PNG 图片及 colorScheme、capturedAt |

## 6. 数据语义

### 6.1 状态和事件

| source | state_read | events_read |
| --- | --- | --- |
| daemon | `/local/v2/sessions/{id}/snapshot`；传 `beforeTurnIndex` 时读 `/history` | 持久事件日志，按 seq 分页，每页最多 500 条 |
| main | Electron main 进程、窗口、版本 | main 诊断缓冲，保留最近 2000 条 |
| renderer | 当前工作区和会话模型（轮次、队列） | 无 |
| backend | `issue:<id>` 或 `project:<id>` 的业务 GET | 无 |
| page | 只读 Web 运行时的窗口 | 只读页面的浏览器事件缓冲 |

事件页原样包含来源的 `generation`、`cursor`，以及 `highWater`（daemon）或 `earliestCursor`（main、page）。daemon 不再要求调用方传 generation，`after` 超过 highWater 时返回空页。调用方据此判断数据库是否被替换、事件是否连续或已被缓冲淘汰。

### 6.2 快照与数据副本

快照保存在系统临时目录下本次宿主私有的子目录（权限 0700），宿主关闭时删除，不写入状态目录。宿主重启后旧的 dataSnapshotId 不再可用。

每次 `data.restore` 生成新的 datasetId 作为导入命名空间，副本 ID 由命名空间和原始 ID 派生。同一命名空间重复导入不会产生第二份。导入策略：历史冻结，运行中轮次转为取消，队列暂停，工作目录重新绑定，云端引用移除，provider 原生会话不恢复。未完成的提交随副本保留：排队项保留暂停原因，"需要确认"保持原状态，复制时正在发送的提交转为"需要确认"（`inFlightSubmissionsNeedReview`）。SQLite 导出核对实际读取的列而不是 schema 版本号。

不支持的内容（执行 source context、自定义属性定义、项目执行资源、Side 上下文的 API 导出）明确拒绝，不当作已复制。

### 6.3 页面

- 定位器为 testId、text、label、role（可带 name）、css，可附加 frameUrl 和 index。
- `page_action` 在定位器匹配多个元素且未传 index 时拒绝；坐标目标只支持 click 和 scroll。
- 同一环境的页面动作串行执行。动作只说明输入已送达，业务结果由调用方读取。
- 密码字段的值以 `[REDACTED]` 返回。

## 7. 安全边界

- 来源由宿主启动参数绑定（`--bindings-file`），工具输入不接收 token、任意路径或 SQL。
- 只读来源（`access: readOnly`）拒绝写请求；导入和 fake provider 控制只作用于宿主自己启动的测试环境。
- 环境名额限制同时运行的环境数量；回收只针对守护进程自己创建的进程和容器，不接管未知进程。
- 验证运行时与已安装的 Multica 并存：开发 Electron 和 bundled daemon 使用环境私有 HOME 和独立健康端口（`MULTICA_DEV_ISOLATED`、`MULTICA_DEV_HEALTH_PORT`），并剔除模型提供方凭据环境变量。宿主持续核对已安装 Multica 的 `launch.json`、`daemon.pid`、CLI 链接、`desktop-runtime` profile 配置和 Codex 配置，发生变化即停止验证环境。
- 生产 Electron 构建移除 main/preload/renderer 验证实现；Go 端由 `verificationdev` 标签控制，设置环境变量不能在生产构建中重新打开。

## 8. 与既有代码的接入点

- [托管开发环境](../../scripts/dev-env.sh)处理 checkout 环境；`env.start` 使用 worktree 的 `make worktree-env` 和 `make up C=api,web`，并通过 `MULTICA_DEV_HOME` 等变量把登记表、profile 和临时目录放在环境目录内，通过 `MULTICA_DEV_POSTGRES_CONTAINER` 在环境专属的 PostgreSQL 容器里建库，不启动共享容器。
- [Desktop 构建配置](../../apps/desktop/electron.vite.config.ts)、[CLI 构建](../../apps/desktop/scripts/bundle-cli.mjs)和 [dev 启动](../../apps/desktop/scripts/dev.mjs)提供开发构建；[发行流程](../../apps/desktop/scripts/package.mjs)排除诊断产物。
- [Local API](../../server/internal/daemon/localhost/v2.go) 的 verification 路由提供事件分页、导入和 fake provider；诊断来源仍遵循账号、workspace 和 owner 校验。
- Desktop main 的[诊断服务](../../apps/desktop/src/main/verification-dev/index.ts)提供 `/capabilities`、`/snapshot`、`/events`、`/connection`。
