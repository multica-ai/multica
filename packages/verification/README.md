# Multica 自动化验证基础能力

一个 24 小时常驻的本机守护进程，只通过 MCP 对外提供两类能力：**操作**（准备和启停环境、导入数据、控制 fake provider、操作页面）和**取数**（环境状态、日志、应用状态、事件、页面元素、截图）。宿主不判断被测行为是否正确，也不产出结论；完整性、对错和报告由调用方的智能体决定。

守护进程固定监听 `http://127.0.0.1:47311`：智能体连接 `/mcp`（MCP Streamable HTTP），浏览器打开 `/` 是管理台。两者都不需要凭据，只接受发往本机这个地址的请求。

工具 schema 位于 [MCP 适配器](src/mcp/server.ts)，资源 schema 位于 [contracts](src/contracts/resources.ts)，接口约定见[接口契约](../../docs/engineering/verification-foundation-contracts.md)。

## 接口

每个工具单独可用，不要求先调用其他工具。需要较长时间的工作采用 start/query：调用返回 job，用 `job_query` 读取进度和结果。

| 工具 | 方式 | 说明 |
| --- | --- | --- |
| `env_inspect` | 同步 | 本机工具链、磁盘、图形会话、构建依赖、数据库目标、端口和 CU 前提 |
| `env_start` | 异步 | 从远端拉取指定分支（或 commit），建独立 worktree、构建，并启动后端和开发 Electron；`liveReadOnly` 启动只读 Web 页面。job 的 `resourceRefs` 在准备开始后即包含 environmentId；应用启动后宿主自动登录测试账号 `verification@localhost` 并进入 `verification` 工作区，无需人工操作 |
| `env_stop` | 异步 | 停止该环境自己的进程和 daemon，数据保留 |
| `env_status` | 同步 | 环境记录及当前观察到的应用、daemon、后端身份 |
| `env_log_read` | 同步 | 分页读取 prepare/backend/desktop/stop 日志，凭据样式文本会脱敏 |
| `job_query` / `job_cancel` | 同步 | 按 jobId 或 requestId 读取任务；请求协作取消 |
| `data_snapshot` | 异步 | 从宿主绑定的来源导出选定会话、项目或事项 |
| `data_snapshot_read` | 同步 | 分页读取快照的实体、关系、附件清单 |
| `data_restore` | 异步 | 把快照或模板导入环境，生成一份新副本；输出原始 ID 到副本 ID 的映射 |
| `fixture_control` | 同步 | 查看 fake provider，输出文本、模拟工具调用及结果，或结束当前请求 |
| `state_read` | 同步 | 原样返回一个来源的当前状态：daemon 会话快照（`beforeTurnIndex` 读取更早轮次）、Electron main、renderer 会话模型、后端事项/项目、只读页面运行时 |
| `events_read` | 同步 | 按 cursor 读取 daemon 持久事件、main 诊断缓冲或只读页面事件，原样返回 generation、cursor、保留范围 |
| `page_windows` | 同步 | 列出窗口，窗口 ID 是 CDP targetId；Desktop 窗口附带 Multica Computer Use 目标 `computerUse` |
| `page_observe` | 同步 | 列出窗口内可见的可交互元素 |
| `page_read` | 同步 | 按定位器读取匹配数量及每个匹配的可见性、可用性、文本、值、位置 |
| `page_action` | 同步 | 对定位器匹配项或窗口坐标执行 click/fill/select/pressKey/scroll/scrollIntoView；Desktop 环境只接受 scroll 和 scrollIntoView；只报告输入已送达 |
| `page_screenshot` | 同步 | 截取窗口并直接返回图片 |

窗口参数省略时使用第一个窗口。定位器支持 testId、text、label、role、css；匹配多个元素时 `page_action` 要求传 `index`。

验证模式的开发 Electron 以不激活的方式打开窗口，自动登录也不聚焦窗口。Desktop 环境的点击和输入由调用方用 Multica Computer Use（`computer_execute`）在后台完成；Electron 窗口在后台丢弃系统滚轮事件，滚动改用 `page_action` 的 scroll 或 scrollIntoView，经 CDP 送达页面，不激活应用。读取和截图使用 `page_read`、`page_observe`、`page_screenshot`：

```javascript
let app = await computer.getApp(target.appId); // page_windows 返回的 computerUse
await app.selectWindow(target.windowId);       // "<pid>:<CGWindowID>"
let state = await app.observe();
```

宿主为每个 Desktop 环境设置专属 bundle ID `ai.multica.verification.<环境 ID>`，Multica Computer Use 对它不显示授权框，`getApp` 能唯一定位这个环境。Computer Use 的 `app.scroll` 对 Electron 窗口返回 `background_unavailable`，请改用 `page_action`。拖拽使用 Computer Use 的 `app.drag`：宿主启动开发 Electron 时以随机端口打开主进程 inspector（`V8_INSPECTOR_PORT=127.0.0.1:0`），cua-driver 经它在后台完成拖拽，多个环境不争用同一端口。

异步工具的 requestId 用于去重：同一 requestId 只对应一种输入，响应丢失时用 `job_query` 按 requestId 查询原任务，不要换 ID 重放。同步工具不经过任务账本。

## 安装守护进程

设备需要 Git、Xcode Command Line Tools，以及可用的 Docker，参见[贡献指南](../../CONTRIBUTING.md)。

```sh
bash scripts/verification/install-daemon.sh
```

脚本先运行 `bootstrap.sh`（把官方 Node 24 下载到 `~/.multica/verification-tools` 并核对 SHA-256，安装仓库指定的 pnpm，构建验证包），再写入用户级 launchd 服务 `ai.multica.verification`：登录后自动启动，退出后自动拉起，日志在 `~/.multica/verification/daemon/daemon.log`。守护进程运行安装脚本所在 checkout 的代码。可用环境变量调整：

| 变量 | 默认 | 作用 |
| --- | --- | --- |
| `MULTICA_VERIFICATION_PORT` | `47311` | 固定端口；被占用时守护进程启动失败，不会换端口 |
| `MULTICA_VERIFICATION_MAX_ENVIRONMENTS` | `2` | 同时运行的环境上限，满额时 `env_start` 返回 `instanceBusy` 并列出占用者 |
| `MULTICA_VERIFICATION_IDLE_MINUTES` | `120` | 环境连续这么久没有 MCP 调用就自动停止并清理；`env_start` 可按环境覆盖 |
| `MULTICA_VERIFICATION_BINDINGS_FILE` | 无 | 真实数据来源绑定文件，见下文 |

安装后在 AI 会话使用的 MCP 客户端里登记一次即可，地址之后不会变化：

```sh
claude mcp add --scope user --transport http multica-verification http://127.0.0.1:47311/mcp
```

`scripts/verification/uninstall-daemon.sh` 先通过管理台停止所有环境，再停止守护进程并移除 launchd 服务；工具链、构建缓存和仓库镜像保留。

## 守护进程与环境生命周期

- 守护进程是唯一的验证宿主。所有 MCP 会话共用它的环境、任务和数据快照，会话断开不影响环境；`job_query` 能查到其他会话提交的任务。
- 环境属于守护进程，直到有人调用 `env_stop`、在管理台停止，或者超过空闲时间。任何带 `environmentId` 的工具调用都会刷新最近使用时间。
- 守护进程重启（包括崩溃后被 launchd 拉起）时保留环境：重新探测仍在运行的 Desktop 环境并继续管理和守护，最近使用时间从重启时刻重新计算；正在准备、启动或停止的环境，以及 `liveReadOnly` 环境（浏览器随守护进程退出）会被停止并清理。
- 管理台每 10 秒采样一次：磁盘、Docker、已安装的 Multica、环境名额，以及每个环境的应用、daemon、后端、数据库容器是否存活和探测延迟，保留约 15 分钟历史；也可以查看任务、日志末尾和停止环境。

## 环境

`fixture` 和 `realSnapshot` 环境的登录全自动：宿主用本地测试后端的开发验证码（`.env.worktree` 中的 `MULTICA_DEV_VERIFICATION_CODE`）为 `verification@localhost` 换取令牌，创建 `verification` 工作区并完成引导，再通过开发 Electron 诊断接口的 `/login` 把令牌交给渲染进程，与浏览器登录走同一条 `auth:token` 路径。

`env_start` 的参数是 `branch`（远端分支名或可达的 commit SHA）、`mode`（`desktop` 或 `renderer`）、`dataMode`（`fixture`、`realSnapshot` 或 `liveReadOnly`），以及可选的 `owner`（是谁的环境，显示在管理台）、`idleTimeoutMinutes` 和 `sourceRef`。

守护进程在 `<state-root>/repository.git` 维护一份只属于自己的部分克隆（远端默认是安装 checkout 的 `origin`），每次 `env_start` 拉取指定分支并固定到当时的 commit，再从这份镜像建独立 worktree，不使用任何人的工作目录。只能验证已推送到远端的代码。

- `fixture` 和 `realSnapshot` 先为环境启动一个专属 PostgreSQL 容器（`pgvector/pgvector:pg17`，空闲端口，容器名 `multica-verification-<environmentId>`），把该 worktree 的 `.env.worktree` 指向它，再运行 `make up C=api,web`，然后启动开发 Electron，由它启动同构建的 bundled daemon。`make up`/`make down` 使用环境目录下的私有登记表，不会回收或改动本机其他开发环境，也不使用共享的 5432 数据库。`env_stop` 运行 `make down` 后删除该容器。
- `liveReadOnly` 要求 `mode=renderer` 和 backend 来源，启动独立 Chromium 页面显示真实 API 数据；写请求、WebSocket 和其他外部请求被拦截。
- 分支必须已包含开发验证接点，否则返回 `unsupportedVersion`。
- 多个环境可同时运行，上限由守护进程配置；启动按顺序执行，避免端口分配冲突。
- 正常运行的环境只占配置名额；已失败、中断或停止的环境若仍有残留应用或守护进程，须先确认清理，再启动新环境。
- 验证环境可以和已安装的 Multica 同时运行：开发 Electron 和 bundled daemon 使用环境私有的 HOME（`<环境目录>/runtime-home`）和独立的 daemon 健康端口，不读写已安装 Multica 的运行时注册、CLI 链接、偏好设置和 Codex 配置，并且宿主启动的所有子进程都不继承 `MULTICA_`、`ANTHROPIC_`、`OPENAI_`、`CLAUDE_`、`CODEX_` 开头的变量和 `DESKTOP_APP_SUFFIX`，因此宿主运行在正式 Multica 会话里时，会话身份、daemon 端点和模型凭据都不会传给验证环境。宿主每秒核对已安装 Multica 的运行时文件，发生变化就停止该验证环境。

构建缓存以源码、锁文件、工具/OS/架构版本和构建环境为键，复用 CLI 和原生 helper；Electron/Next 开发服务按本轮源码编译。

## 绑定真实来源

来源由守护进程启动参数绑定（安装时设置 `MULTICA_VERIFICATION_BINDINGS_FILE`），不接受模型输入 token 或任意 SQL。以下是绑定文件私有 JSON 的结构示例，替换为设备实际来源；文件和 token 文件均需 `chmod 600`：

```json
{
  "schemaVersion": 1,
  "sources": [{
    "ref": "my-real-conversations",
    "kind": "daemon",
    "baseUrl": "http://127.0.0.1:ACTUAL_PORT",
    "scope": {
      "apiOrigin": "https://YOUR_API_ORIGIN",
      "userId": "ACTUAL_USER_ID",
      "workspaceId": "ACTUAL_WORKSPACE_ID"
    },
    "tokenFile": "/private/path/to/actual-local-api.token",
    "access": "readOnly",
    "identity": {
      "ownerId": "ACTUAL_OWNER_ID",
      "runId": "ACTUAL_RUN_ID",
      "commit": "ACTUAL_CLI_COMMIT",
      "pid": 12345
    },
    "databaseFile": "/private/path/to/local-conversations/conversations.sqlite",
    "filesRoot": "/private/path/to/local-conversations/files"
  }]
}
```

不要把 token 内容粘贴进对话。`data_snapshot` 的 `sourceRef` 就是这里的 `ref`。快照只存在系统临时目录里本次宿主进程私有的子目录中，宿主关闭时删除；宿主重启后需要重新导出。配置 `databaseFile` 后使用只读 SQLite 事务导出；否则通过来源的 Local API 分页导出历史和附件。backend 附件下载跳转到对象存储时，在 binding 中用 `attachmentOrigins` 显式声明允许的来源。

## 数据副本

`data_restore` 每次导入一份新副本：会话历史冻结，运行中的轮次转为取消，队列暂停，工作目录重新绑定，云端引用移除。未完成的提交状态随副本保留：排队项连同暂停原因保留；"需要确认"的提交保持该状态和原因；复制时正在发送的提交转为"需要确认"。

SQLite 导出不按 schema 版本号判断，而是核对它读取的表和列是否存在；会话库 schema 6 起额外导出未完成的提交状态，与 Local API 快照的格式一致。缺少所需列时返回 `unsupportedVersion` 并指明缺失的列。workspace 副本恢复状态、工作流、项目、事项、父子关系、标签、评论和附件；人员执行身份和自动工作流执行不随内容复制。含执行 source context、自定义属性定义或项目执行资源的选择明确返回不支持。

导入响应丢失时 job 为 `unknown`，`resourceRefs` 中保留副本的 datasetId；可以用 `state_read` 查看副本是否存在，再决定是否导入另一份。

默认开发验证使用受控 fake provider，经过真实 service/runner/队列链路；不调用真实模型。`fixture_control` 除 inspect 外必须指定 `expectedRequestId`，陈旧请求会被拒绝。

### 模拟工具调用

先在测试会话发送消息，再用 inspect 取得 `pendingRequestId` 作为 `expectedRequestId`。沿用该会话的 `environmentId`、`conversationId`，通过 MCP client 分别发送工具开始和结果：

```ts
await client.callTool({
  name: "fixture_control",
  arguments: {
    environmentId, conversationId, expectedRequestId,
    action: "tool_start",
    tool: { id: "search-1", name: "mcp__example__search", input: { query: "mock" } },
  },
});
await client.callTool({
  name: "fixture_control",
  arguments: {
    environmentId, conversationId, expectedRequestId,
    action: "tool_result",
    tool: { id: "search-1", output: "Found two matches", isError: false },
  },
});
await client.callTool({
  name: "fixture_control",
  arguments: { environmentId, conversationId, expectedRequestId, action: "release", text: "Done" },
});
```

`tool_start` 的 input 必须是 JSON 对象；`tool_result` 的 output 可以是文本或 JSON，`isError: true` 生成工具失败状态。两种工具动作均不结束轮次，可在结果之前观察运行中的工具；最后用 release/fail 结束轮次。工具 ID 在当前请求内唯一，结果必须对应尚未完成的调用；重复开始、重复结果和未知 ID 会被拒绝。

工具事件经过现有 normalizer、事件持久化和会话渲染链路，支持 Claude/Codex 测试会话。工具名为 `Bash` 时沿用命令展示。这里只模拟调用及结果，不执行命令、不调用 MCP 服务，也不拦截真实模型的工具执行。工具动作不接受 text；原有 chunk/release/fail 文本控制保持原用法。通过 `state_read` 和 `events_read` 查看事件，Desktop 展示需使用含本能力的验证构建。

## 状态与事件

`state_read` 和 `events_read` 只转发来源的原始响应，宿主不计算完整性。调用方需要判断数据是否连续时，自行比较 `generation`、`cursor`、`highWater`、`earliestCursor`：daemon 事件来自持久日志；Electron main 和只读页面的事件来自最近 2000 条的内存缓冲。

## 生产边界

生产 Electron 的模块解析直接移除 main/preload/renderer 验证实现；Go 用 `verificationdev` 构建标签排除诊断和 fake 代码，生产 bundler 显式使用空 tags。设置验证环境变量也不能重新启用生产入口。

新包不定义 Turbo 的标准 `build/typecheck/lint/test` 任务，默认 CI 不运行这些检查。

## 手动检查

```sh
pnpm --filter @multica/verification check:types
pnpm --filter @multica/verification check:lint
pnpm --filter @multica/verification test:manual
pnpm --filter @multica/verification test:integration
pnpm --filter @multica/verification test:desktop
node scripts/verification/check-production.mjs
(cd server && go test -tags=verificationdev ./internal/daemon/localhost ./internal/localconversation/service ./internal/daemon ./internal/verificationenv -run '^TestVerification' -count=1)
(cd server && go test -tags=verificationprodtest ./internal/daemon/localhost -run '^TestVerification' -count=1)
```

`test:integration` 构建临时 Go harness，使用真实 SQLite、service、Local API 和 fake provider，并启动受控 Chromium 测试页面适配器；缺少浏览器缓存时先运行 `pnpm --filter @multica/verification exec playwright install chromium`。它不能代替完整 Electron GUI 的验收。

`test:desktop` 是显式原生验收入口，默认不执行。它启动真实 Electron/bundled daemon，由宿主自动登录并进入工作区，再发送 A/B、检查队列和导航边界、保存截图并停止环境，结果写到本轮状态目录的 `desktop-acceptance.json`。判断逻辑在这个脚本里，宿主只提供操作和数据。

后端 workspace 导入的检查需先用本 checkout 的托管环境准备数据库：

```sh
(cd server && go test -race -tags=verificationdev ./internal/verificationworkspace ./internal/handler -run '^TestVerification|^TestValidateGraphBeforeImport$|^TestAttachmentIntegrityBeforeMutation$' -count=1)
(cd server && go test -race ./internal/handler -run '^TestVerificationProductionRoutesStayAbsent$' -count=1)
```
