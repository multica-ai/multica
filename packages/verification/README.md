# Multica 自动化验证基础能力

独立于被测应用运行的验证宿主，只提供两类能力：**操作**（准备和启停环境、导入数据、控制 fake provider、操作页面）和**取数**（环境状态、日志、应用状态、事件、页面元素、截图）。宿主不判断被测行为是否正确，也不产出结论；完整性、对错和报告由调用方的智能体决定。

工具 schema 位于 [MCP 适配器](src/mcp/server.ts)，资源 schema 位于 [contracts](src/contracts/resources.ts)，接口约定见[接口契约](../../docs/engineering/verification-foundation-contracts.md)。

## 接口

每个工具单独可用，不要求先调用其他工具。需要较长时间的工作采用 start/query：调用返回 job，用 `job_query` 读取进度和结果。

| 工具 | 方式 | 说明 |
| --- | --- | --- |
| `env_inspect` | 同步 | 本机工具链、磁盘、图形会话、构建依赖、数据库目标、端口和 CU 前提 |
| `env_start` | 异步 | 从指定 commit 准备 worktree、构建，并启动后端和开发 Electron；`liveReadOnly` 启动只读 Web 页面。job 的 `resourceRefs` 在准备开始后即包含 environmentId；等待登录时 job 为 `awaitingUser` |
| `env_stop` | 异步 | 停止该环境自己的进程和 daemon，数据保留 |
| `env_status` | 同步 | 环境记录及当前观察到的应用、daemon、后端身份 |
| `env_log_read` | 同步 | 分页读取 prepare/backend/desktop/stop 日志，凭据样式文本会脱敏 |
| `job_query` / `job_cancel` | 同步 | 按 jobId 或 requestId 读取任务；请求协作取消 |
| `data_snapshot` | 异步 | 从宿主绑定的来源导出选定会话、项目或事项 |
| `data_snapshot_read` | 同步 | 分页读取快照的实体、关系、附件清单 |
| `data_restore` | 异步 | 把快照或模板导入环境，生成一份新副本；输出原始 ID 到副本 ID 的映射 |
| `fixture_control` | 同步 | 查看 fake provider，或 release/chunk/fail 当前请求 |
| `state_read` | 同步 | 原样返回一个来源的当前状态：daemon 会话快照（`beforeTurnIndex` 读取更早轮次）、Electron main、renderer 会话模型、后端事项/项目、只读页面运行时 |
| `events_read` | 同步 | 按 cursor 读取 daemon 持久事件、main 诊断缓冲或只读页面事件，原样返回 generation、cursor、保留范围 |
| `page_windows` | 同步 | 列出窗口，窗口 ID 是 CDP targetId |
| `page_observe` | 同步 | 列出窗口内可见的可交互元素 |
| `page_read` | 同步 | 按定位器读取匹配数量及每个匹配的可见性、可用性、文本、值、位置 |
| `page_action` | 同步 | 对定位器匹配项或窗口坐标执行 click/fill/select/pressKey/scroll；只报告输入已送达 |
| `page_screenshot` | 同步 | 截取窗口并直接返回图片 |

窗口参数省略时使用第一个窗口。定位器支持 testId、text、label、role、css；匹配多个元素时 `page_action` 要求传 `index`。

异步工具的 requestId 用于去重：同一 requestId 只对应一种输入，响应丢失时用 `job_query` 按 requestId 查询原任务，不要换 ID 重放。同步工具不经过任务账本。

## 准备一台 Mac

设备需要 Git、Xcode Command Line Tools，以及仓库托管环境可用的 PostgreSQL/Docker 前提，参见[贡献指南](../../CONTRIBUTING.md)。

```sh
bash scripts/verification/bootstrap.sh
```

脚本将官方 Node 24 下载到 `~/.multica/verification-tools` 并核对 SHA-256，安装仓库指定的 pnpm，过滤安装和构建验证包。它不修改 shell 配置或智能体的全局 MCP 配置。可用 `MULTICA_VERIFICATION_TOOLS_ROOT` 指定工具目录。

已有 Node 24 和指定 pnpm 的设备也可以手动运行：

```sh
pnpm install --filter @multica/verification... --frozen-lockfile --ignore-scripts
pnpm --filter @multica/verification build:manual
node packages/verification/dist/mcp/stdio.js --project /absolute/path/to/checkout
```

最后一条是等待 stdin 协议消息的 MCP 服务；stdout 只输出协议，错误到 stderr。MCP 客户端也可配置 `bash /absolute/path/to/scripts/verification/mcp.sh`，wrapper 已绑定其 checkout，可追加 `--state-root`、`--bindings-file`、`--recover-lock`。

## 环境

`env_start` 的参数是 `gitRef`、`mode`（`desktop` 或 `renderer`）、`dataMode`（`fixture`、`realSnapshot` 或 `liveReadOnly`）、`includeWorkingChanges` 和可选的 `sourceRef`。

- `fixture` 和 `realSnapshot` 先为环境启动一个专属 PostgreSQL 容器（`pgvector/pgvector:pg17`，空闲端口，容器名 `multica-verification-<environmentId>`），把该 worktree 的 `.env.worktree` 指向它，再运行 `make up C=api,web`，然后启动开发 Electron，由它启动同构建的 bundled daemon。`make up`/`make down` 使用环境目录下的私有登记表，不会回收或改动本机其他开发环境，也不使用共享的 5432 数据库。`env_stop` 运行 `make down` 后删除该容器。
- `liveReadOnly` 要求 `mode=renderer` 和 backend 来源，启动独立 Chromium 页面显示真实 API 数据；写请求、WebSocket 和其他外部请求被拦截。
- `includeWorkingChanges` 只能覆盖当前 HEAD，并记录源码摘要。目标版本必须已有开发验证接点，否则返回 `unsupportedVersion`。
- 同一台设备同时只运行一个完整 Desktop 验证环境；设备锁在 `~/.multica/verification-device.lock`。
- 验证环境可以和已安装的 Multica 同时运行：开发 Electron、bundled daemon 和 CLI 使用环境私有的 HOME（`<环境目录>/runtime-home`）和独立的 daemon 健康端口，不读写已安装 Multica 的运行时注册、CLI 链接、偏好设置和 Codex 配置，并剔除 `ANTHROPIC_`、`OPENAI_`、`CLAUDE_`、`CODEX_` 开头的环境变量。宿主每秒核对已安装 Multica 的运行时文件，发生变化就停止该验证环境。
- `env_status` 的 `components.cliCommand` 是该环境的 `multica-dev` 命令，绑定环境的 HOME 和健康端口，可直接操作环境内的 CLI 和 daemon；它不安装到用户 PATH。

构建缓存以源码、锁文件、工具/OS/架构版本和构建环境为键，复用 CLI 和原生 helper；Electron/Next 开发服务按本轮源码编译。

## 绑定真实来源

来源由宿主启动参数绑定，不接受模型输入 token 或任意 SQL。以下是 `--bindings-file` 私有 JSON 的结构示例，替换为设备实际来源；文件和 token 文件均需 `chmod 600`：

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

`data_restore` 每次导入一份新副本：会话历史冻结，运行中的轮次转为取消，队列暂停，工作目录重新绑定，云端引用移除。workspace 副本恢复状态、工作流、项目、事项、父子关系、标签、评论和附件；人员执行身份和自动工作流执行不随内容复制。含执行 source context、自定义属性定义或项目执行资源的选择明确返回不支持。

导入响应丢失时 job 为 `unknown`，`resourceRefs` 中保留副本的 datasetId；可以用 `state_read` 查看副本是否存在，再决定是否导入另一份。

默认开发验证使用受控 fake provider，经过真实 service/runner/队列链路；不调用真实模型。`fixture_control` 的 release/chunk/fail 必须指定 `expectedRequestId`，陈旧请求会被拒绝。

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

`test:desktop` 是显式原生验收入口，默认不执行。它启动真实 Electron/bundled daemon，等待测试应用完成正常登录和工作区选择，再发送 A/B、检查队列和导航边界、保存截图并停止环境，结果写到本轮状态目录的 `desktop-acceptance.json`。判断逻辑在这个脚本里，宿主只提供操作和数据。

后端 workspace 导入的检查需先用本 checkout 的托管环境准备数据库：

```sh
(cd server && go test -race -tags=verificationdev ./internal/verificationworkspace ./internal/handler -run '^TestVerification|^TestValidateGraphBeforeImport$|^TestAttachmentIntegrityBeforeMutation$' -count=1)
(cd server && go test -race ./internal/handler -run '^TestVerificationProductionRoutesStayAbsent$' -count=1)
```

## 已知限制

SQLite 导出器只识别会话库 schema 4 和 5。会话库升级到 schema 7 后，配置 `databaseFile` 的来源会返回 `unsupportedVersion`，`test:integration` 中依赖该路径的用例同样失败；不配置 `databaseFile` 时走 Local API 导出。
