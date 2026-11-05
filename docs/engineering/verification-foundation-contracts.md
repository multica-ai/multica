# 自动化验证底层接口与实施契约 v1

本文细化[整体方案](automation-runtime-foundation.md)中已确认的七个模块，作为架构和语义契约。七个模块已有首版实现；实际调用参数、可恢复数据范围与手动命令见[验证包说明](../../packages/verification/README.md)。下文类型为架构示意，当前 MCP/resource schema 为参数权威；不将设计目标或协议集成测试当成整套 Electron GUI 已验收。

## 1. 模块依赖与入口

对外采用同一底层能力实例，MCP 是传输适配层。CLI 和脚本复用底层函数与结果结构；首版同一设备只允许一个完整验证工具宿主持有运行环境，第二个宿主不能接管它的活动环境。协议、资源账本、任务及应用会话管理是模块内部支撑，不增加新的对外能力模块。

| 模块 | 统一接口命名空间 | 依赖与职责 |
| --- | --- | --- |
| MCP | 工具注册与协议转换 | 调用下列接口；不持有第二套业务状态 |
| Setup | `setup` | 项目配置、构建、进程生命周期、资源归属及应用会话 |
| 数据 | `data` | 已授权来源、快照/恢复、对象映射、测试数据；调用 Setup 准备的目标环境 |
| 采集 | `sources` | 各源直连适配、状态快照、步骤事件；将产物写入证据模块 |
| 检查 | `checks` | 根据来源能力和证据执行声明式条件；页面条件调用页面读取适配器 |
| 证据 | `evidence` | 持久化文件/索引、读取、完整性核对、报告导出 |
| 页面 | `page` | 复用 Setup 应用会话，观察和交互，向证据模块提交截图 |

底层运行在独立 Node 兼容工具宿主中。Electron API、进程内 IPC 和诊断入口留在 Desktop；Go 状态读取与新增 dev 诊断留在 server。新包不得导入 Desktop main 源码来隐式启动 Electron，也不依赖业务 UI 或 React store。

实现时以运行时 schema 作为新契约的权威定义，TypeScript 类型从 schema 推导；下文类型是评审示意。新协议内部统一 camelCase，已有 snake_case API 通过来源适配器转换。校验失败标为无效来源，不能使用 UI 容错默认值伪造事实。

## 2. 公共结果和身份

### 2.1 结果结构

下面定义公共类型，未列出的业务 payload 在各接口实现时按本目录补全 schema。`ReadResult` 表示调用结果；数据来源是否完整、检查是否通过，分别属于业务结果，不与调用成功混用。

```ts
export type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
export type Ref<K extends string> = string & { readonly resourceKind: K };
export type EnvironmentId = Ref<"environment">;
export type VerificationId = Ref<"verification">;
export type StepId = Ref<"step">;
export type SourceId = Ref<"source">;
export type JobId = Ref<"job">;
export type ReceiptId = Ref<"receipt">;
export type DataSnapshotId = Ref<"dataSnapshot">;
export type StateSnapshotId = Ref<"stateSnapshot">;
export type DatasetId = Ref<"dataset">;
export type EvidenceId = Ref<"evidence">;

export type ErrorCode =
  | "invalidInput" | "forbidden" | "notFound" | "requestConflict"
  | "dependencyMissing" | "permissionRequired" | "instanceBusy"
  | "identityMismatch" | "unsupportedVersion" | "unsupportedCapability"
  | "sourceUnavailable" | "coverageIncomplete" | "dataPartial"
  | "staleReference" | "deadlineExceeded" | "outcomeUnknown"
  | "interrupted" | "cancelled" | "cleanupIncomplete" | "internalError";

export interface Problem {
  code: ErrorCode;
  message: string;
  recovery: "none" | "inspect" | "sameRequest" | "reobserve" | "userAction";
  relatedRefs: string[];
}

export type ReadResult<T> = {
  schemaVersion: 1;
  observedAt: string;
} & (
  | { kind: "ok"; value: T }
  | { kind: "error"; problem: Problem }
);

export type CommandResult<T> = {
  schemaVersion: 1;
  receiptId: ReceiptId;
  requestId: string;
} & (
  | { kind: "completed"; value: T }
  | { kind: "accepted"; jobId: JobId }
  | { kind: "rejected"; problem: Problem }
  | { kind: "unknown"; problem: Problem }
);

export interface MutationOptions {
  requestId: string;
}

export interface CallOptions {
  deadlineAt?: string;
  signal?: AbortSignal;
}

export interface Job {
  jobId: JobId;
  receiptId: ReceiptId;
  operation: string;
  resourceRefs: string[];
  phase: string;
  updatedAt: string;
  state:
    | "queued" | "running" | "awaitingUser" | "cancelling"
    | "completed" | "failed" | "cancelled" | "interrupted" | "unknown";
  output?: { schema: string; value: Json };
  problem?: Problem;
}

export interface Coverage {
  state: "complete" | "partial" | "unknown";
  scope: Json;
  fromCursor?: string;
  throughCursor?: string;
  reasons: string[];
}

export interface SourceObservation {
  sourceId: SourceId;
  sourceKind: "renderer" | "main" | "daemon" | "backend" | "providerFixture";
  instanceRef: string;
  status: "available" | "partial" | "unavailable";
  captureStartedAt: string;
  captureEndedAt: string;
  generation?: string;
  revision?: string;
  payloadSchema: string;
  payload?: Json;
  coverage: Coverage;
  problem?: Problem;
}

export interface StateSnapshot {
  stateSnapshotId: StateSnapshotId;
  environmentId: EnvironmentId;
  subjectRef: string;
  consistency: "perSource";
  observations: SourceObservation[];
}

export interface ObservedEvent {
  eventId: string;
  sourceId: SourceId;
  instanceRef: string;
  generation: string;
  sourceCursor: string;
  occurredAt?: string;
  receivedAt: string;
  type: string;
  payloadSchema: string;
  payload: Json;
  correlation: {
    conversationId?: string;
    requestId?: string;
    turnId?: string;
  };
}

export type Temporal =
  | { mode: "now" }
  | { mode: "eventually"; timeoutMs: number }
  | { mode: "throughout"; stepId: StepId };

export interface CheckResult {
  checkId: string;
  environmentId: EnvironmentId;
  stepId?: StepId;
  status: "passed" | "failed" | "unavailable";
  predicateSchema: string;
  predicate: Json;
  temporal: Temporal;
  expected: Json;
  actual?: Json;
  startedAt: string;
  endedAt: string;
  evidenceIds: EvidenceId[];
  coverage: Coverage[];
  problem?: Problem;
}
```

所有时间为 UTC RFC 3339 字符串；边界 schema 校验时间和有限正数超时。TypeScript 的品牌 ID 只防代码误用，实际权限由宿主绑定、资源账本和应用认证验证，不能把 ID 或调用者填写的字段当作授权证明。

### 2.2 命令、任务和取消

- 查询返回 `ReadResult<T>`；影响文件、采集状态、进程或业务对象的操作返回 `CommandResult<T>`。入参无法解析或宿主无法持久化收据时返回调用错误，不伪造 receiptId。
- 先持久化 requestId、操作名、授权主体/环境作用域、规范化输入摘要和资源预留，再执行副作用。相同作用域下相同 requestId、相同输入返回原收据；不同操作或输入返回 requestConflict。不能仅在 MCP 内存里去重。
- accepted 只承诺任务已经登记。返回的 jobId 可查询 phase、resourceRefs 和实际进度；只有 completed 和通过 schema 校验的 output 才表示完成。查询入口为底层 jobs.get/getReceipt，MCP 初版经 setup_status 暴露公共任务查询，不将其新增为第八个能力模块。
- deadline/连接中断结束的是调用等待，不能据此宣称已取消后台任务。长任务使用 jobs.cancel 显式取消，返回取消收据，继续查询直到终态。MCP 的请求取消传播到等待；需要中止后台工作时显式调用 setup_cancel，不停止其他任务或未知进程。
- 已发生副作用后错误不自动回滚真实来源或重放动作。unknown/interrupted 先核对账本与目标；数据部分导入提供已确认映射与可恢复阶段。
- MCP 在请求进入时绑定已鉴权宿主上下文；工具 JSON 不接收任意 principal/token 来切换身份。CLI/脚本使用同一绑定规则。

### 2.3 三种不能混用的快照与对象引用

| 身份 | 表示什么 | 生命周期 |
| --- | --- | --- |
| dataSnapshotId | 可恢复的真实业务内容及关联 | 不可变，跨基线/候选复用 |
| stateSnapshotId | 某次运行状态观察 | 绑定实际环境、来源身份与采集区间 |
| datasetId | 已绑定的真实来源或已恢复的测试数据集 | 绑定环境，重置后其运行引用需要重新取得 |

对象引用同时校验环境、数据恢复版本和实际应用运行身份。跨环境使用对象引用直接拒绝。数据来源 scope、环境 scope 与 step scope 由宿主核对，不因 agent 传了一个合法字符串就认为匹配。

## 3. 七个模块的操作契约

### 3.1 MCP

工具注册表固定映射到底层操作，复用输入/输出 schema。文本结果携带结构化结果，截图读取由 evidenceId 解析并返回实际图片；大量日志返回有界分页和证据引用。业务检查 failed 是正常工具结果，参数/调用错误才标协议调用失败。unknown 必须保留不确定语义。

MCP 入口不自动启动应用、读取真实来源或全局注册到用户的模型配置。宿主内部诊断地址和凭据不出现在工具返回值。工具发现可在应用未启动时工作。

初版工具映射如下。复合工具用 operation 区分子操作，并按 operation 校验独立的输入 schema；read/list 等纯查询不要求 requestId，产生记录或副作用的操作必须提供。

| MCP 工具 | 底层操作 |
| --- | --- |
| setup_inspect / setup_prepare / setup_start / setup_stop | setup 的对应操作 |
| setup_status / setup_cancel | 环境状态或 jobs.get/getReceipt / jobs.cancel |
| setup_data | data.bindSource/inspectSource/createSnapshot/restoreSnapshot/bindLiveDataset/createFixture |
| setup_fixture_control / setup_reset | data.controlFixture / data.resetDataset |
| verification_record | evidence.createVerification/readVerification/finalizeVerification |
| verification_snapshot / verification_events | sources.readSnapshot/readEvents |
| verification_step | sources.startObservation/stopObservation |
| verification_check | checks.listPredicates/check |
| verification_evidence | evidence.attachEvidence/readEvidence/listEvidence |
| verification_export | evidence.exportReport |
| page_windows / page_observe / page_action / page_screenshot | page 的对应操作 |

来源能力通过 verification_snapshot 的 capabilities 子操作映射 sources.getCapabilities，snapshot 子操作读取实际状态；不启动采集也可先查询能力。底层函数可保留清晰命名，MCP 的子操作与字段由一张注册表定义，不在两个位置独立演进。

### 3.2 Setup

| 操作 | 必填输入 | 完成结果 |
| --- | --- | --- |
| inspect | projectRef、renderer/desktop 模式 | 工具版本、设备条件、占用与准备计划 |
| prepare | projectRef、gitRef、运行模式、数据模式、requestId | environmentId、固定 commit、工件与能力清单 |
| start | environmentId、requestId | 各组件实际身份、运行状态及可用能力 |
| status | environmentId 或 jobId/receiptId | 环境组件状态，或公共任务/收据 |
| stop | environmentId、requestId | 已停止、保留资源与清理失败清单 |

projectRef 来自已授权项目绑定，不允许工具任意执行外部路径命令。prepare 内部将 gitRef 解析成不可变 commit；分支名继续遵循 codex/ 约定。数据模式指定 liveReadOnly 时必须提供已授权 sourceRef 以固定 API 目标；realSnapshot/fixture 可先准备空测试环境，再绑定选定数据。

环境记录包含构建类型、诊断能力、工件摘要、组件依赖、预期/实际 API 目标及资源归属。实际凭据与发现记录保存在私有宿主存储，公开结构仅返回引用。应用会话由 Setup 持有；页面模块借用连接，不启动第二份 Electron 或 daemon。

环境生命周期为 preparing → prepared → starting → ready → stopping → stopped；失败/中断独立记录。准备或启动需要登录、系统权限时，job 为 awaitingUser，环境保留原阶段和等待原因。单个组件可降级或离线，status 逐组件返回；需要该能力的操作返回 unavailable。ready 表示本轮运行模式声明的应用/服务、账号、连接及能力就绪；数据集和具体场景前提另行返回准备状态，不能把进程存在或运行环境 ready 当作场景已具备全部初始数据。

### 3.3 数据

| 操作 | 必填输入 | 完成结果 |
| --- | --- | --- |
| bindSource | 已授权 sourceRef、选择范围、requestId | sourceId、来源种类和有效 scope |
| inspectSource | sourceId | 可读对象、导出能力、附件及一致性限制 |
| createSnapshot | sourceId、selection、requestId | dataSnapshotId、manifest、完整性结果 |
| restoreSnapshot | environmentId、dataSnapshotId、requestId | datasetId、语义对象引用、原始/目标 ID 映射 |
| bindLiveDataset | environmentId、sourceId、selection、requestId | liveReadOnly datasetId 和实际来源范围 |
| resetDataset | datasetId、requestId | 恢复结果、新数据恢复版本和对象引用 |
| createFixture | environmentId、template、parameters、requestId | datasetId、语义对象引用、fixtureId 与声明的控制动作 |
| controlFixture | fixtureId、声明动作、requestId | fake provider 接收/释放等操作结果及证据 |

快照 manifest 至少包含 schemaVersion、源版本/能力、来源范围、采集时间区间、一致性方式、实体清单、关联清单、附件的相对路径/字节数/摘要、缺失与完整性。内容工件摘要不依赖目标生成 ID；恢复映射单独保存。schemaVersion 不支持时拒绝，业务内容含有未知状态时保留原始事实并返回能力限制，不悄悄套用默认状态。

完整性校验和目标能力检查先于写入。来源不支持一致导出时，标记限制；有重复对照需求的场景不能直接将不断变化的遍历结果宣称为一致快照。恢复适配器声明 offlineBeforeStart 或 onlineAfterServiceReady 及前置条件；前者只允许 prepared 且目标存储未打开时使用受控导入实现，后者走已就绪服务的正常导入/业务接口，不直接改运行中的 SQLite。恢复依赖排序、幂等收据与已创建对象映射；部分失败可检查，不重新创建已确认对象。账号凭据、provider 会话恢复和待执行任务不随内容快照自动导入续跑。

liveReadOnly 数据集禁止 reset 和写操作。复制数据集的 reset 仅恢复本轮测试目标；基线/候选各用自己的数据库与目标 schema，读取相同不可变快照，不能让旧版本打开已升级数据库。

### 3.4 状态与事件采集

| 操作 | 必填输入 | 完成结果 |
| --- | --- | --- |
| getCapabilities | environmentId、来源集合 | 版本、状态对象、事件类型及可追赶能力 |
| readSnapshot | environmentId、subjectRef、来源集合 | StateSnapshot，含每源可用性和游标 |
| startObservation | verificationId、environmentId、subjectRef、必需/可选来源、驱动、requestId | stepId、起始状态、每源就绪状态与游标 |
| readEvents | stepId、过滤、cursor、limit | 事件页、nextCursor、hasMore 和 coverage |
| stopObservation | stepId、结束说明、requestId | 结束状态、各源确认水位、覆盖及缺口 |

本机 HTTP/WS 入口按 main 与 daemon 分别连接，renderer 经 main 的窄 IPC 读取。来源返回 cursor/generation 等元数据，工具的分页 cursor 再绑定 scope、过滤及观察版本；收到不匹配游标时拒绝，不从零默默重读。来源缺少原生事件序号时可记录宿主接收序号，但不得将它宣称为源端无丢失保证。

开始观察后必需源均确认起点，才向 agent 返回可开始动作；无法满足时返回已落盘的 step 及原因，允许结束并取回已有证据。结束操作以每源可确认水位收尾，晚到或超时分别标记；有限等待不保证任意异步业务已经结束。业务完成条件由检查器判断。

step 记录只表示该区间内观察到的事件。跨层精确关联优先使用已有 request/turn/conversation ID；缺少时列为未关联，不能将同一时间段全部活动都归因于一个点击。

### 3.5 确定性检查

| 操作 | 必填输入 | 完成结果 |
| --- | --- | --- |
| listPredicates | environmentId | 条件 schema、所需能力及支持的时间语义 |
| check | verificationId、environmentId、subjectRef、predicate、temporal、requestId | CheckResult，或可查询的检查 job |

check 会落盘结果和证据，因此按命令去重。条件目录首版为 queue.itemCount、queue.paused、turn.status、providerFixture.receivedCount、diagnostic.errorCount、page.visible、page.text、page.controlState。每个条件有独立 schema，operation/operator 不支持时返回 unsupportedCapability，不执行任意 JavaScript 谓词或 SQL。

now 使用有效当前观察；eventually 在有限时间等待；throughout 读取已结束 step 的确认区间。若要同时等待某个业务结束状态并检查整段无重复，先 startObservation，再动作，再 eventually，最后 stopObservation 和 throughout。区间内已有确定反例可 failed；缺失证据时不能 passed；持续有效但不满足直到超时则 failed。

providerFixture.receivedCount 仅证明 fake provider 接收边界的次数。真实 provider 无对应能力时不提供同等检查，不能把已有发送记录或 turn 数当作实际接收次数。

### 3.6 证据、验证记录与报告

| 操作 | 必填输入 | 完成结果 |
| --- | --- | --- |
| createVerification | 目标、范围、基线/候选角色、证据要求、requestId | verificationId |
| readVerification | verificationId | 环境角色、步骤/检查/证据索引及报告 revisions |
| attachEvidence | verificationId、environmentId、可选 stepId、授权文件/图片、来源、requestId | evidenceId、摘要、类型、归属 |
| readEvidence | evidenceId、日志范围或图片读取方式 | 实际内容、截断标识及元数据 |
| listEvidence | verificationId、过滤、cursor、limit | 证据页与游标 |
| finalizeVerification | verificationId、结论、解释、引用、未覆盖项、requestId | 不可变报告 revision |
| exportReport | verificationId、reportRevision、格式、requestId | 工件引用及真实存在的绝对路径 |

Verification 是证据聚合记录，由证据模块管理；步骤放在 sources，检查放在 checks，各模块通过 ID 关联。一次验证可以关联串行运行的基线与候选环境，各证据保留明确角色。创建验证、查询状态及终结报告通过拟议 verification_record 工具的 create/read/finalize 子操作提供；不能依赖手填一个不存在的 verificationId。

finalize 接受 agent 的分析，但检查事实不可被覆盖。passed 必须满足该验证声明的证据要求；Bug 验证还要求有效基线复现、修复与边界检查。缺关键证据拒绝 passed，结论可为 inconclusive；waived 仅接受宿主可核对的用户明确豁免引用，不能把 agent 的布尔字段当作用户决定。普通冒烟采用自己的证据要求，不强制故意失败的基线。

报告 revision 固定清单与文件摘要，补充分析或证据生成新 revision；重复导出相同 revision 复用对应收据。图片/日志源文件复制进归档，校验存在性与摘要后导出；不引用上一轮固定临时路径。HTML 关键图片嵌入，完整证据包可单独导出；不自动上传或发布任务评论。

### 3.7 页面

| 操作 | 必填输入 | 完成结果 |
| --- | --- | --- |
| listWindows | environmentId | 本轮应用窗口引用与实际运行身份 |
| observePage | environmentId、windowRef、可选 frameRef、有界范围 | observationId、frame/元素引用和页面摘要 |
| performAction | verificationId、environmentId、stepId、observationId、targetRef、动作、requestId | 动作收据、实际执行状态和关联证据 |
| captureScreenshot | verificationId、environmentId、stepId、windowRef、范围、requestId | evidenceId、尺寸、采集范围及时间 |

页面动作 union 为 click、fill、select、scroll、pressKey；每种动作有对应参数 schema。目标引用绑定环境/运行身份、窗口、frame 和观察；脚本语义 locator 必须在动作前重新解析并校验唯一性。元素过期或目标歧义返回 staleReference/invalidInput，不点击第一个相似对象。

页面模块不重复提供业务断言和等待 DSL；页面条件经 checks 调用只读适配器。动作统一进入本轮写操作串行协调，与外部 CU 的协调通过 agent 的步骤编排明确记录；首版不声称能锁住未接入的外部鼠标或其他进程。动作超时后先观察和查询原收据，不自动再次发送或点击。

## 4. 从设备到报告的调用顺序

1. inspect → 如需绑定真实来源先 bindSource/inspectSource → prepare 固定版本及数据模式。没有对应能力时明确阻塞。
2. createVerification 得到 verificationId。使用真实副本时 createSnapshot 固定 dataSnapshotId；快照可以在版本准备期间独立执行，但不自动开始读取真实数据。
3. 按目标适配器能力执行恢复：offlineBeforeStart 在 prepared 阶段先 restoreSnapshot 再 start；onlineAfterServiceReady 先 start 核对应用/服务，再 restoreSnapshot。运行环境就绪与场景数据就绪分别查询，避免双方等待。禁止通过写正在使用的 SQLite 完成恢复。
4. 确认恢复结果，或使用 bindLiveDataset，获取语义对象引用；getCapabilities 确认页面和所需事件来源。fixture 场景显式创建并取得 fixtureId。每次开始场景前核对数据恢复版本与应用运行身份。
5. startObservation → observePage → performAction / 外部 CU 操作 → eventually check → stopObservation → throughout check。页面截图和检查结果关联同一个 step。
6. agent readEvidence，确认实际截图与日志；必要时增加边界步骤。生成结论并 finalizeVerification，exportReport。
7. stop 本轮环境。若清理结果需进入报告，先 stop 再 finalize，或生成包含清理结果的新报告 revision；保留原始产品检查结果。
8. 基线/候选对照时串行重复版本环境和步骤，使用同一 dataSnapshotId，分别恢复到目标版本自己的测试环境。两轮证据进入同一个 verificationId 并区分角色。

## 5. 实施顺序与手动验收

按整体方案 P0—P4 推进，先交付公共契约和持久化支撑，再逐步接入真实能力；未接入的工具不返回伪造成功。

| 切片 | 归属 | 交付 | 手动验收 |
| --- | --- | --- | --- |
| 1 | P0 | schema、底层门面、收据/任务/资源账本、stdio MCP、inspect | 独立宿主工具发现；同请求复用；冲突输入拒绝；中断后可查 |
| 2 | P1 | 项目配置、固定版本构建、共享应用会话、启动/停止策略 | 专用 Mac 启动正确 Electron/CLI；不触发真实模型或无关全局集成；不接管未知实例 |
| 3 | P2 | 版本化真实数据协议，来源绑定、选择性快照和恢复 | 含附件及关联的数据往返一致；部分导入可恢复；源数据不被 reset 修改 |
| 4 | P2 | main/renderer dev 诊断及构建排除、daemon 直连、步骤/游标及覆盖 | 新增入口随实现即受构建限制；采集断线可追赶或标缺失；Electron 退出后 daemon 采集独立可用 |
| 5 | P3 | 页面驱动、证据索引/文件归档、实际图片读取 | 操作同一实例；窗口/frame 更新拒绝旧引用；截图落盘与步骤关联 |
| 6 | P3 | 条件注册、fake provider 控制、检查和报告 | 真实会话副本+受控 provider 的队列场景；完整区间次数检查；报告可独立读 |
| 7 | P4 | 中断恢复、资源清理、生产工件排除、引导与说明 | 干净 Mac 全流程；发行包无新增诊断入口，环境开关无法开启；证据保留 |

实现单元的实际包名、导出入口及手动命令随对应切片落地；只为当前切片增加实际使用的模块与依赖，不创建空模块、不增加 CI。默认测试使用假进程、假数据来源及 fake provider，不查找用户模型 CLI。真实应用手动集成在专用测试环境执行，真实模型仍需另行明确授权。

## 6. 对既有代码的接入边界

- [托管开发环境](../../scripts/dev-env.sh)处理 checkout 环境；底层适配器读取真实状态和身份，避免使用按端口盲杀的旧命令。
- [Desktop 构建配置](../../apps/desktop/electron.vite.config.ts)、[CLI 构建](../../apps/desktop/scripts/bundle-cli.mjs)及[dev 启动](../../apps/desktop/scripts/dev.mjs)落实 dev 构建能力；[发行流程](../../apps/desktop/scripts/package.mjs)排除诊断产物。不能仅依据连接云端或 mode=production 判断构建类型。
- [Local API](../../server/internal/daemon/localhost/v2.go)、[订阅服务](../../server/internal/localconversation/service/hub.go)与[队列](../../server/internal/localconversation/service/queue.go)提供既有事实；诊断来源仍遵循真实账号、workspace 和 owner 校验。
- [现有时序记录](../../server/internal/localconversation/timing/contract.go)和[日志导出](../../apps/desktop/src/main/developer-log-export.ts)可复用，保留不同来源覆盖差异；验证工具不把日志尾部声明成完整事件历史。
- [页面队列场景](../../e2e/local-conversation-queue.spec.ts)可参考定位与测试范围；其模拟页面与[模块冒烟](../../apps/desktop/scripts/side-preview-smoke.cjs)不能当作完整 Electron 接入已存在。

采用的根本方案是统一能力和稳定契约，现有脚本通过相应适配器复用。局部兼容入口只声明实际覆盖，不作为最终交付的替代，也不新增双写、任意写库或旧协议伪装。

## 7. 实现与校验范围

实际底层入口为 `createVerificationHost`。修改命令通过 `host.jobs.submit` 的收据/任务统一执行，MCP 只做映射；读取复用数据、采集、证据、页面等 namespace。开发接点分别位于 Desktop main/preload/renderer 与 Go `verificationdev` 文件，发行构建解析/标签排除实际实现。

实现已经覆盖七个模块的可用闭环，具体状态、已运行命令和边界集中在[验证包说明](../../packages/verification/README.md)和[整体方案的验收范围](automation-runtime-foundation.md#16-首版实现与验收边界)。手动协议集成使用独立临时 SQLite/Go 服务和受控 Chromium，并验证源数据不变、事件不完整不能通过、未知导入可核对恢复。没有新增 CI 或真实模型调用。

范围收敛：本地会话可完整内容导入；后端实体只能采集参考样本。`liveReadOnly` 只管理引用观察记录，不创建页面、不停止真实来源。不同版本通过独立 dev profile/SQLite 对照，同机串行；生产状态根与多实例能力不在本次改动中。干净 Mac 上的完整 Electron GUI 验收仍需实际执行。
