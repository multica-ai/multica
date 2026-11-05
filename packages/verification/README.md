# Multica 自动化验证基础能力

独立于被测应用运行的验证宿主。底层统一接口管理环境、数据、采集、检查、证据和页面；stdio MCP 只负责让智能体调用这些能力。实际工具 schema 位于 [MCP 适配器](src/mcp/server.ts)，资源 schema 位于 [contracts](src/contracts/resources.ts)。

## 当前可用范围

七个模块已有实现，而非空目录或仅接口声明：

| 模块 | 代码 | 能力 |
| --- | --- | --- |
| MCP | `src/mcp/` | 20 个工具，参数校验、命令收据、任务查询、真实图片读取 |
| Setup | `src/setup/` | 工具链检查/准备、固定源码 worktree、依赖和开发构建、应用启动/就绪/停止 |
| 数据 | `src/data/` | 已授权真实会话快照、完整历史分页、附件校验、语义导入、ID 映射、恢复/重置、受控模板 |
| 采集 | `src/sources/` | daemon 直连、main/renderer 开发诊断、后端实体读取、步骤事件与水位校验 |
| 检查 | `src/checks/` | 当前状态、有限时间内满足、完整步骤区间检查；缺失/未知事实返回 unavailable |
| 证据 | `src/evidence/` | 私有文件、SHA-256、真实图片读取、固定报告版本、JSON/Markdown/HTML/文件包、保留策略 |
| 页面 | `src/page/` | Playwright 连接同一应用的 renderer，窗口/元素观察、有限操作、语义定位检查、截图 |

首版完整运行环境是专用干净 macOS。`realSnapshot/fixture` 使用开发 Electron 和同构建 bundled daemon；`desktop` 额外构建原生 Computer Use helper。`liveReadOnly` 使用 `renderer` 模式和已授权的 backend 来源，启动独立 Web/Chromium 页面，支持查看、搜索和滚动，阻止写请求、WebSocket 和其他外部网络请求。原生系统界面由调用方的外部 Computer Use 操作。Docker/macOS VM 调度、跨设备派发和全账号复制不在首版范围。

真实数据的写入验证使用**副本**。本地格式恢复会话、轮次、消息、事件、暂停队列、附件及 Side 关系闭包。`multica.workspace/v1` 恢复选定项目/事项的状态目录、工作流、父子关系、标签、评论和附件；人员映射到测试身份，负责人/执行分配清空，工作流自动执行停用，原始与目标 ID 的映射独立保存。含执行 source context、自定义属性定义或项目执行资源的选择明确返回不支持，不把这些对象静默当作已复制。

## 准备一台 Mac

设备需要 Git、Xcode Command Line Tools，以及仓库托管环境可用的 PostgreSQL/Docker 前提，参见 [贡献指南](../../CONTRIBUTING.md)。系统安装、登录、图形会话和 macOS 权限不是一次版本探测就能证明就绪的。

先准备工具宿主：

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

最后一条是等待 stdin 协议消息的 MCP 服务；stdout 只输出协议，错误到 stderr。MCP 客户端也可配置 `bash /absolute/path/to/scripts/verification/mcp.sh`。wrapper 已绑定其 checkout，参数可添加 `--state-root`、`--bindings-file`、`--attachment-root` 和 `--recover-lock`，不要再传一遍 `--project`。

## 从环境到报告

1. 调用 `setup_inspect`，检查工具版本、磁盘、图形会话、构建依赖、数据库目标、端口、现存运行时和 CU 前提。系统权限来自启动后的实际 dev capabilities；外部 CU 配置不能由本宿主推断。
2. 调用 `setup_prepare`，例如下面的参数。收到 accepted 后通过 `setup_status` 查询任务，不能当成构建已完成。
准备出错时可通过 `setup_status` 的 `environmentLog` 目标分页读取 prepare/backend/desktop/stop 日志；只能选择本环境的固定日志名，凭据样式文本会脱敏。

3. 调用 `setup_start`。本地副本使用该 worktree 的 `make up C=api,web`，然后启动开发 Electron，由它启动同构建 bundled daemon。应用/CLI commit、私有发现信息、owner/run/PID、API 目标、工作区和 Local API 必须吻合。
4. 系统前提或正常登录/工作区选择未完成时，任务为 `awaitingUser`，查询原任务可看到等待原因。登录完成后原启动任务继续核对就绪；停止环境会取消其待完成的启动/准备任务。测试后端的开发验证码配置由仓库现有环境脚本管理，不复制主 checkout 的 `.env` 或真实 PAT。
5. `verification_record/create` 声明场景、范围、输入快照、provider 脚本、必需目标/边界检查和截图要求；`bindEnvironment` 绑定 `baseline/candidate/smoke` 角色。
6. 用 `setup_data` 绑定来源、创建快照并恢复副本，或创建 `conversation.controlled/v1`、`workspace.basic/v1` 模板。保存 dataset 和 refs；基线、候选各自恢复同一快照，不共用数据库。
7. `verification_step/begin` 开始采集。查询其收据完成且 coverage 合格后再操作页面或外部 Computer Use。`page_observe` 返回的引用只能用于当前观察，操作后重新观察。
8. `verification_step/end` 收齐结束水位；`verification_check` 执行具体检查。需要看事实时用 snapshot/events/evidence，不能用日志没有报错代替区间检查。
9. 读取实际截图和证据，调用 `setup_stop` 核对清理结果，再 `verification_record/finalize` 产生不可变报告版本，`verification_export` 导出本地文件或 bundle。环境关闭不影响归档读取。

```json
{
  "gitRef": "HEAD",
  "mode": "desktop",
  "dataMode": "realSnapshot",
  "includeWorkingChanges": true,
  "requestId": "prepare-candidate-01"
}
```

`includeWorkingChanges` 默认 false；仅允许覆盖当前 HEAD，并记录源码摘要。开发本能力但尚未提交时设为 true。正式对照通常指定已提交的固定版本。目标版本必须已有开发验证接点；对缺少接点的历史版本返回 unsupportedVersion，不悄悄改造旧源码。每个新分支使用 `codex/verification-*`。

准备缓存以源码、锁文件、工具/OS/架构版本、构建类型、诊断标记和相关构建环境为键，同键构建互斥，成功后原子发布文件摘要清单。当前缓存复用 CLI 和原生 helper；Electron/Next 开发服务仍按本轮源码编译。启动及报告通过前重新核对源码树，拒绝把准备后的源码修改当作原构建。

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

不要把 token 内容粘贴进对话。`databaseFile`/`filesRoot` 可选；配置后使用只读 SQLite 事务，限定 account/workspace 和已识别的 schema 4/5，不将数据库文件交给目标应用升级。无数据库路径时，通过来源正常 Local API 导出分页历史和附件，前后核对 generation、snapshotSeq、queueRevision；活动数据发生变化时拒绝不稳定快照。API 无法提供完整 Side 上下文时明确拒绝，使用 SQLite 适配器。

副本以版本化内容协议导入目标正常 store 的事务入口，不通过任意 SQL 写入。导入的命名空间为 `datasetId:restoreVersion`，持久记录 ID 映射和转换：不恢复 provider 原生会话、运行中的轮次转取消、队列暂停、工作目录重新绑定、移除队列云端执行引用、副本解除归档。命令重试收据及已移除队列项的去重记录不作为原运行态恢复；副本使用新的请求命名空间。新数据的身份、云端关联和运行状态不冒充原对象。

导入的网络响应丢失时，原任务变成 unknown，dataset 保持 partial。先查询原 requestId 和资源引用，再用 `setup_data/resumeImport` 复用原命名空间核对导入，避免创建第二份。`setup_reset` 只停止并归档本工具创建的旧副本，再恢复新版本；不修改源数据，不删除证据。

`liveReadOnly` 绑定 backend 来源并使用 `mode=renderer`。工具准备锁定源码及 Chromium，在本轮独立页面显示真实 API 数据；凭据由宿主受控读取，不交给页面。页面填入搜索条件、滚动和点击仍经过网络只读约束，业务写请求不会发送到来源。页面模式只覆盖 Web UI，不声明 Electron 认证/IPC 覆盖；完整 Electron 写入验证使用副本。停止仅关闭本轮浏览器和带认证身份的 Web 子进程，不停止真实来源。宿主中断后可核对保留的 Web 控制身份并关闭残留。

backend 附件下载跳转到对象存储时，在私有 binding 中用 `attachmentOrigins` 显式声明允许的来源；跳转请求不转发应用 bearer。快照移除临时下载凭据，保存附件字节、摘要和相对归档路径。`setup_data/inspectSnapshot` 按 entities/relationships/attachments 分页返回清单，不把大份业务正文直接塞入模型响应。

## 采集与检查

| 来源 | 访问方式 | 完整性 |
| --- | --- | --- |
| daemon | 直接访问已认证 Local API，dev WS 通知 + 持久事件 HTTP 追赶 | generation、实例身份、连续 seq、结束水位 |
| main | 本机随机端口、私有 bearer、拒绝普通网页 Origin | 启动 owner、事件 cursor、环形缓冲区最早 cursor |
| renderer | main 经窄 IPC 读取真实 ConversationService/model | 显式报告未加载/陈旧 queue items；不触发另一套加载/状态流 |
| backend | 现有认证业务 GET | 按实体采样，没有连续事件区间保证 |
| page | 工具拥有的只读浏览器 | 本轮 generation、浏览器事件与确认水位；不冒充 Electron main |

快照是 `perSource`，不声称跨进程原子一致。renderer 步骤同时采集 main。`sources` 是必需来源，`optionalSources` 保存可选来源及缺口；backend 作为必需区间来源时不能标 complete。只读页面步骤可将 page 设为必需来源，并单独读取 backend 快照。WS 断连可由持久 HTTP 追赶补齐，generation 变化、序号缺口、保留截断或结束水位未确认都会阻止区间通过。宿主重启将活动步骤和环境标 interrupted，不自动恢复交互。`setup_status/environment` 核对各来源的实际在线状态；`verification_events` 游标绑定步骤、来源 generation、数据恢复版本和过滤条件，混用或重置后的游标被拒绝。

`verification_check/list` 必须传 environmentId，返回该环境的条件 schema、来源、操作符、时间语义、完整性要求及可用性。条件包括：queue 数量/暂停、轮次状态、fake provider 接收次数、诊断错误次数、页面可见性/文本/可用状态。`now` 检查一次，`eventually` 在有限期限内采样；`throughout` 当前支持完整步骤内 provider 接收次数和 main 错误次数，未实现的操作符/时间组合返回 unsupportedCapability；区间来源不完整则检查为 unavailable。诊断错误计数只支持 throughout。页面语义定位支持 testId/text/label，缺失元素可以检查不可见；歧义定位拒绝。元素引用过期时不能视为“不可见”。

默认开发验证使用受控 fake provider，经过真实 service/runner/batcher/队列链路；关闭模型发现、spare/preheat、真实 provider 自动标题调用和云端任务认领。fake 的 Codex 身份也复用受控 Claude 消息适配，不宣称验证真实 Codex app-server 协议。控制 release/chunk/fail 必须指定 `expectedRequestId`，陈旧完成请求不会落到下一条队列消息。真实模型兼容性需要独立、明确授权的集成验证。

## 收据与证据

命令统一经 `host.jobs.submit(operation, input, requestId)` 进入底层账本；读取经对应 namespace。MCP 复用这些操作。直接嵌入的仓库适配器也应使用命令入口；namespace 中的服务方法是组合底层实现，不提供另一套请求去重承诺。

```ts
import { createVerificationHost } from "@multica/verification";

const host = await createVerificationHost({ projectRoot: "/absolute/checkout" });
try {
  const receipt = await host.jobs.submit("verification.create", {
    title: "Queue verification", kind: "smoke",
    plan: {
      scenario: "queue-empty/v1", scope: ["Controlled conversation queue"],
      providerScript: "controlled/v1",
      checks: [{ key: "empty", purpose: "target", kind: "queue.itemCount",
        operator: "eq", expected: 0, temporal: "now" }],
      ui: { required: false, colorSchemes: ["light"] },
    },
  }, "queue-verification-01");
  if (receipt.kind === "accepted") {
    console.log(await host.jobs.get(receipt.jobId)); // Poll to a terminal state.
  }
} finally {
  await host.close();
}
```

状态默认放在 `~/.multica/verification/<canonical-project-path-sha256>/`，与应用目录分开。另一个项目或活动宿主不能复用该状态目录。同一个 requestId 的规范输入只能对应一个操作；响应丢失查询原 requestId，不能换 ID 重放未知点击。进程中断后任务不自动重放；旧宿主的迟到结果不能覆盖新账本。

完整 Desktop 的设备锁固定在 `~/.multica/verification-device.lock`，跨项目和 state-root 共用。清理确认前保留占用，不因宿主退出就接管未知运行时。

宿主崩溃留下的状态目录锁默认拒绝启动。核对原宿主后可加 `--recover-lock`，仅在记录 PID 已不存在时恢复，并保留旧锁记录；不杀进程，不自动删除无法核对的锁。

证据文件导入拒绝宿主私有状态、已绑定的 token/源数据库以及常见凭据文件；环境日志走受控查询。

归档单文件限 32 MB，截图必须有真实 PNG/JPEG 内容；图片读取返回实际 MCP image。导出前重新校验字节与 SHA-256。HTML 内嵌截图且转义文本；bundle 包含报告、证据、输入快照、附件字节和摘要 manifest。外部 CU 的内存图片可用 `verification_evidence/attachImage` 接收 PNG/JPEG base64。外部文件和内存截图均要求环境/步骤归属、capturedAt 和 colorScheme；实际图片读取仍返回 MCP image。所有输出只保存在本机，不自动上传、发布或发消息。

每轮对照使用新 verification；失败或不可用检查不会被同一记录的后续采样掩盖，重跑新建记录并保留旧证据。

没有验证计划不能生成 passed。Bug 计划要求相同 dataSnapshotId/provider 脚本、目标检查和至少一个边界检查；每个环境的检查必须来自当前恢复版本，comparisonKey 和类型/期望/时间语义必须匹配计划。UI 检查需要同一步骤的截图；纯页面计划要求浅色和深色。候选检查真实通过、必需步骤完整且基线在目标检查失败后才可生成通过结论。不同 ID 的对应检查可使用相同 `comparisonKey`，同时仍核对谓词类型、操作符、期望和时间模式。waived 仅接受宿主从用户明确上下文提供的引用，不接受模型自己声称获批。

`verification_evidence/pin` 固定报告，`prune` 使用默认 7 天/20 轮策略，只清理已结束且所属环境已停止、未固定的归档。未绑定环境的已结束报告也可清理。数据快照、数据集和 worktree 单独保留用于重现，不自动删除。子进程日志限 16 MB；截断会留下 `.truncated` 标记，截断日志不能证明无错误。私有归档可能包含真实业务内容，外发前需检查。

## 手动检查与整机验收

```sh
pnpm --filter @multica/verification check:types
pnpm --filter @multica/verification check:lint
pnpm --filter @multica/verification test:manual
pnpm --filter @multica/verification test:integration
pnpm --filter @multica/verification test:desktop
pnpm --filter @multica/desktop typecheck
pnpm --filter @multica/desktop lint
pnpm --filter @multica/desktop exec vitest run
pnpm --filter @multica/desktop exec electron-vite build
node scripts/verification/check-production.mjs
(cd server && go test -tags=verificationdev ./internal/daemon/localhost ./internal/localconversation/service ./internal/daemon ./internal/verificationenv -run '^TestVerification' -count=1)
(cd server && go test -tags=verificationprodtest ./internal/daemon/localhost -run '^TestVerification' -count=1)
(cd server && go test ./internal/runtimeinstance ./internal/cli ./internal/daemon/localhost ./internal/localconversation/service)
```

手动集成的 Chromium 冒烟若缺少浏览器缓存，先运行 `pnpm --filter @multica/verification exec playwright install chromium`；实际 Electron 页面连接不依赖另装 Chromium。

手动集成构建临时 Go harness，使用真实 SQLite、service、Local API、HTTP/WS 和 fake provider；页面部分启动独立受控 Chromium，通过真实 CDP/Playwright 测试适配器。它们不读取用户模型账号，也不查找或执行用户安装的模型 CLI。它们**不能代替整套 Electron GUI 的干净 Mac 验收**。

整机验收按“从环境到报告”的顺序执行一次：正常登录 → 恢复指定真实快照 → 页面操作 → 采集结束 → 检查 → 读截图 → 导出 → 停止；核对源数据不变、正常 Desktop 不受影响、所有所属进程停止。当前设备有活动正常 Desktop，Setup 会阻止竞争启动，所以这轮没有运行该整机验收，也没有读取用户真实业务数据或调用真实模型。

`test:desktop` 是显式原生验收入口，默认不在单元测试或 CI 中执行。它准备真实 Electron/bundled daemon，等待测试应用完成正常登录和工作区选择，再通过 Playwright 发送 A/B、验证队列与导航边界、采集截图、停止并导出证据包。输出本轮私有目录的 `desktop-acceptance.json`，含实际阶段耗时及有范围说明的采样内存/磁盘峰值；失败或设备占用写入 inconclusive，不伪造通过。自定义 checkout/state-root 可设置 `MULTICA_VERIFICATION_PROJECT`、`MULTICA_VERIFICATION_STATE`。

后端新内容协议的手动检查需先用本 checkout 的托管环境准备数据库，再运行：

```sh
(cd server && go test -race -tags=verificationdev ./internal/verificationworkspace ./internal/handler -run '^TestVerification|^TestValidateGraphBeforeImport$|^TestAttachmentIntegrityBeforeMutation$' -count=1)
(cd server && go test -race ./internal/handler -run '^TestVerificationProductionRoutesStayAbsent$' -count=1)
```

生产 Electron 的模块解析直接移除 main/preload/renderer 验证实现；Go 用 `verificationdev` 构建标签排除诊断和 fake 代码，生产 bundler 显式使用空 tags。设置验证环境变量也不能重新启用生产入口。现有生产 Local API 不受此边界改变。

新包有意不定义 Turbo 的标准 `build/typecheck/lint/test` 任务，默认 CI 不增加验证场景或原生应用启动。手动命令单独执行；运行记录和编译输出不提交。架构背景见[整体方案](../../docs/engineering/automation-runtime-foundation.md)与[接口契约](../../docs/engineering/verification-foundation-contracts.md)。

## 验收状态

需求、实现位置及实际验证结果见[原方案对齐清单](../../docs/engineering/verification-foundation-acceptance.md)。单元、协议集成、数据库和生产排除检查分别记录，不能代替干净 Mac 整机结果。首次安装、原生权限、完整 Electron GUI 以及整机资源成本只按真实执行记录认定。
