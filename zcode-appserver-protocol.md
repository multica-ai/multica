# ZCode app-server Session Protocol — 逆向文档

> 逆向对象:`/Users/mac/work/zcode-cli/vendor/zcode.cjs`(ZCode Desktop runtime bundle,`node vendor/zcode.cjs app-server`)
> 探测方式:spawn `node vendor/zcode.cjs app-server`(cwd = workspace),stdin/stdout 上走 NDJSON 行,逐字段对 zod 报错补齐直到请求成功,并跑通了真实 turn。
> 所有示例均为真实探测得到的 JSON(非猜测)。报告不含任何 API key / secret。
> 探测日期:2026-08-14;runtime 版本见 `vendor/extraction.json`(CLI zcode-cli-stream 3.7.5-11)。

---

## 0. 传输层总览(Transport)

- 启动方式:`/Users/mac/.nvm/versions/node/v22.22.1/bin/node vendor/zcode.cjs app-server`,cwd 必须是一个工作目录(通常就是 workspace 目录)。
- 通信:stdin/stdout 上每行一个 JSON(NDJSON),UTF-8,`\n` 结尾。协议是"JSON-RPC 2.0 风格"但并非严格 JSON-RPC(见下)。
- **客户端 → 服务端 请求**:`{"id": 1, "method": "<method>", "params": {...}}\n`。`id` 是客户端自选数字。
- **服务端 → 客户端 响应**:`{"id": 1, "result": ...}` 或 `{"id": 1, "error": {"code": ..., "message": ..., "data": ...}}`。
- **服务端 → 客户端 请求**(也需要客户端应答):`{"id": "server-1", "method": "<method>", "params": {...}}`。`id` 形如 `server-1`、`server-2`(字符串)。客户端必须回 `{"id": "server-1", "result": ...}`,否则服务端会一直等待(最终报 `ZCode Protocol client connection closed`)。
- **服务端 → 客户端 通知**(无 `id`):`{"method": "session/event"|"v4/telemetry/event"|"state.updated"|"process/resourceSample", "params": {...}}`。
- **进程生命周期**:只要 stdin 保持打开,进程就存活,可连续处理多条请求;stdin 收到 EOF 后进程以 exit code 0 正常退出。
  - 注意:官方 CLI 客户端(`src/app-server-client.ts`)的用法是"每个请求 spawn 一个新 app-server 进程、发一条请求、读到进程退出"。但这只是 CLI 的用法;**协议本身支持单个进程内多请求连续会话**,本报告的所有持久会话探测都在单进程内完成。
- 错误码(实测):`-32602` Invalid params(zod 校验失败,`error.data` 为 `{"name":"ZodError","message":"[...JSON issues...]"}`);`-32603` Internal error;`-32004` Session not found / Session is not active;`-32010` A prompt is already running for this session;`-32031`(provider runtime headers 未应用 / restore warning)。

zod 报错示例(空 params 调 session/create):

```json
{"error":{"code":-32602,"data":{"name":"ZodError","message":"[\n  {\n    \"expected\": \"object\",\n    \"code\": \"invalid_type\",\n    \"path\": [\"workspace\"],\n    \"message\": \"Invalid input: expected object, received undefined\"\n  }\n]"},"message":"Invalid params — workspace: Invalid input: expected object, received undefined"},"id":1}
```

---

## 1. 完整方法表

从 bundle 中提取的完整 JSON-RPC 方法注册表(`zr` 对象):

```
session/create  session/resume  session/list  session/subagents
session/requestRuntimePreferences  session/read  session/messages  session/events
session/subscribe  session/send  session/stop  session/cancelBackgroundTask
session/fork  session/compact  session/goal  session/close
session/setModel  session/setThoughtLevel  session/updateRuntimeModelConfig  session/setMode
workspace/readState  workspace/updateProviderRegistry  workspace/updateInteractionPreferences
workspace/upsertModelProvider  workspace/removeModelProvider  workspace/setDefaultModel
workspace/setDefaultThoughtLevel  workspace/setDefaultMode  workspace/generateText
mcp/list  plugins/*  automation/*  usage/stats  session/usage
interaction/requestPermission   (server→client)
interaction/requestUserInput    (server→client)
interaction/requestProviderRuntimeHeaders  (server→client)
interaction/browserList  interaction/browserExecute  (server→client)
```

本报告详述 session 相关方法(任务要求范围);`workspace/*`、`mcp/*`、`plugins/*`、`automation/*` 不作展开。

---

## 2. session/create

### 2.1 请求

```json
{"id":1,"method":"session/create","params":{
  "workspace": {
    "workspaceKey": "zprobe",
    "workspacePath": "/tmp/zprobe-work"
  },
  "model": {"providerId":"bigmodel","modelId":"GLM-5.2"},
  "mode": "build",
  "thoughtLevel": "high",
  "persistence": "immediate"
}}
```

params schema(从 bundle 提取,`XWt`,strict + 部分 superRefine):

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `workspace` | object | **必填** | strict,见 2.2 |
| `sessionId` | string | 否 | **仅** `importedHistory` 为真时可用,否则 `-32602 "sessionId is only supported for imported history creates"` |
| `parentSessionId` | string | 否 | 父子会话 |
| `mode` | enum | 否 | `"plan"\|"build"\|"edit"\|"yolo"\|"auto"` |
| `model` | object | 否 | `{providerId, modelId, variant?}`,strict |
| `runtimeModel` | object | 否 | `{revision, generatedAt, model: Al, provider, thoughtLevel?}`,strict(provider registry 快照) |
| `persistence` | enum | 否 | `"immediate"\|"deferred"` |
| `thoughtLevel` | string | 否 | 取值来自模型能力表,如 `"max"\|"high"\|"nothink"`(GLM-5.2)/ `"enabled"\|"disabled"`(GLM-5.1) |
| `titleGenerationEnabled` | boolean | 否 | |
| `mcpServers` | array | 否 | stdio 或 http/sse MCP server 配置(`rU` union) |
| `toolAllowlist` | string[] | 否 | |
| `toolDenylist` | string[] | 否 | |
| `importedHistory` | object | 否 | 导入历史会话 |

### 2.2 workspace 对象(严格模式)

实测字段(多余 key 会报 `Unrecognized keys`):

```json
{"workspaceKey": "zprobe", "workspacePath": "/tmp/zprobe-work", "workspaceIdentity": "zid", "remoteSessionId": "rsess-1"}
```

- `workspaceKey`: string,min 1,**必填**。等价于"工作区键",可由 `workspaceIdentity` 归一化得到(bundle 里有 fallback 规则,但 create 路径未做该校验,实测 key 与 identity 不匹配也能创建)。
- `workspacePath`: string,min 1,**必填**。本地绝对路径。
- `workspaceIdentity`: string,min 1,可选。
- `remoteSessionId`: string,可选。
- strict:不接受 `workspacePurpose`、`kind`、`target`、`workspaceID`、`directory`、`path` 等(实测全部被拒)。

### 2.3 create 握手(重要!)

创建成功前,服务端几乎总是会发一个 **server→client 请求**:

```json
{"id":"server-1","method":"session/requestRuntimePreferences","params":{
  "sessionId":"sess_0b40e82e-781a-41cf-9ea7-f8327972e6c6",
  "scope":"runtime-materialization"
}}
```

客户端必须应答(schema `lHt`):

```json
{"id":"server-1","result":{
  "nativeSearchEnhancementsEnabled": false,
  "memoryEnabled": false,
  "askUserQuestionAutoResolutionEnabled": true,
  "modelContextBudgetStrategy": "preflight-v1"
}}
```

- `integratedTerminalShell` 可省略(可选),若提供:`{"mode":"auto"}` 或 `{"mode":"shell","dialect":"cmd|git-bash","id":..,"label":..,"path":..}`。
- 在 `session/send` 时还可能出现 `scope:"user-execution"` 的同类请求(用于解析 bash shell 选择),同样应答即可。
- 若不应答(或直接关闭 stdin),create 会失败:`-32603 "ZCode Protocol client connection closed"`。

### 2.4 响应(完整 snapshot)

真实返回(sessionId 在 `session.sessionId`):

```json
{"id":1,"result":{
  "messages": [],
  "projection": {
    "activeToolCalls": [], "backgroundJobs": [], "contextUsed": 0, "contextWindow": 1000000,
    "mode": "build", "pendingPermissions": [], "sessionId": "unknown",
    "status": "idle", "target": null, "totalTokenCount": 0, "turnCount": 0
  },
  "protocol": {"name": "ZCode Protocol", "version": 1},
  "runtime": {
    "eventSeq": 0, "pendingRequestIds": [], "goalVerifications": [],
    "goalVerificationTimeline": [], "stateRevision": 1
  },
  "session": {
    "createdAt": 1786699967538, "mode": "build",
    "model": {"modelId": "GLM-5.2", "providerId": "bigmodel"},
    "traceId": "81c73d96-0fbd-4c68-8eea-c7f22e923f38",
    "sessionId": "sess_0b40e82e-781a-41cf-9ea7-f8327972e6c6",
    "sessionKind": "interactive", "status": "idle", "target": null, "title": "",
    "updatedAt": 1786699967538,
    "workspace": {"workspacePath": "/tmp/zprobe-work", "workspaceKey": "zprobe"}
  },
  "settings": {
    "mode": {"current": "build"},
    "model": {"available": [...], "current": {...}, "lastUsed": {...}},
    "permission": {"mode": "build"},
    "thoughtLevel": {"available": [...], "current": "max", "defaultLevel": "max", "enabled": true}
  },
  "slashCommands": [...],
  "todos": [],
  "todoGroups": []
}}
```

要点:
- snapshot 顶层 keys:`messages / projection / protocol / runtime / session / settings / slashCommands / todos / todoGroups`。
- **`sessionId` 位于 `result.session.sessionId`**(格式 `sess_<uuid>`,见第 5 节);`projection.sessionId` 恒为 `"unknown"`,不要用。
- `settings.model.available` 是模型目录(`contextWindow`、`reasoning.levels`、`providerOptionsByLevel` 等);`settings.model.current` / `lastUsed` 为 `{providerId, modelId}`。
- 若未显式传 `model`,默认取 workspace 的 lastUsed/default(实测为 `zai/GLM-5.2`);而本地配置只配了 bigmodel 的 key,导致 turn 报 `provider_not_configured`。**因此后端实现必须显式传 `model:{providerId:"bigmodel",modelId:"GLM-5.2"}`**(见第 7 节)。

---

## 3. session/resume

### 3.1 请求

```json
{"id":2,"method":"session/resume","params":{"sessionId":"sess_0b40e82e-781a-41cf-9ea7-f8327972e6c6"}}
```

params schema(`eHt`,strict):`{sessionId(必填), workspace?, runtimeModel?, thoughtLevel?, mcpServers?, toolAllowlist?, toolDenylist?}`

### 3.2 响应

与 create 相同的完整 snapshot(同 2.4 结构)。恢复**同一进程内**由 session/create 创建的会话成功(返回同一 sessionId);`session/resume` 会先查内存 `sessions` 表,再查持久化 session store。

### 3.3 关键限制(实测)

- **跨进程 resume 失败**:`{"error":{"code":-32004,"message":"Session not found: sess_..."},"id":2}`。即便 create 时 `persistence:"immediate"` 也一样。
- 原因:app-server 创建的会话只存在进程内存里,不会写进 ~/.zcode 的 sqlite session store(session/list 列出的持久会话来自 Desktop 真实使用记录)。
- `session/resume` 可以恢复 Desktop 持久化的会话(disk store),但对我们后端无意义。
- 结论:**同一 app-server 进程内,create → send → events → close → EOF,整个生命周期用一个进程**(见第 5 节)。

---

## 4. session/list

### 4.1 请求

```json
{"id":7,"method":"session/list","params":{}}
```

params schema(`tHt`):`{workspace?, includeArchived? (default false), limit?}`

### 4.2 响应(截断)

```json
{"id":7,"result":{"sessions":[
  {"createdAt":1786698775889,"mode":"build","traceId":"...","sessionId":"sess_bd29816a-...",
   "sessionKind":"interactive","status":"idle","title":"...","titleSource":"first_input",
   "updatedAt":1786698783823,"workspace":{"workspaceKey":"...","workspacePath":"..."}},
  ...
]}}
```

- 返回的是**持久化 session store** 里的会话(最多 50 条,来自 Desktop 使用),**不包含**本进程 create 的内存会话。
- 对后端作用有限(可能用于"该 workspace 有没有历史会话"的判断)。

---

## 5. session/subscribe(事件订阅)

### 5.1 请求

```json
{"id":5,"method":"session/subscribe","params":{"sessionId":"sess_0b40e82e-...","deliveryKind":"desktop-continuous"}}
```

params schema(`QWt`,strict):`{sessionId(必填), deliveryKind(必填), afterSeq? (int≥0), includeSnapshot? (default false)}`
- `deliveryKind` 取值:`"desktop-continuous"` 或 `"web-remote-replayable"`(两者都接受;实测用前者)。
- 对不存在的/非 active 会话返回:`-32004 "Session is not active: sess_x"`。

### 5.2 响应

```json
{"id":5,"result":{"eventSeq":0,"events":[],"sessionId":"sess_0b40e82e-..."}}
```

(带 `includeSnapshot:true` 时还会返回 `snapshot`。)

### 5.3 作用

订阅后,服务端会把**带完整内容的** `session/event` 通知推送到 stdout(见第 6 节)。未订阅时,stdout 上只有 `v4/telemetry/event`(只含统计/计数,不含文本)和 `state.updated`。

---

## 6. session/send —— 最关键的方法

### 6.1 请求

```json
{"id":2,"method":"session/send","params":{
  "sessionId":"sess_0b40e82e-...",
  "content":"reply with exactly the word ok, nothing else"
}}
```

params schema(`cHt`,strict + superRefine):

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `sessionId` | string | **必填** | |
| `content` | string | **必填** | 用户消息文本 |
| `inputId` | string | 否 | |
| `queryId` | string | 否 | |
| `attachments` | array | 否 | 每个元素是任意 map(`g.record(string, unknown)`) |
| `browserAmbientContext` | object | 否 | |
| `expectedRevision` | int≥0 | 否 | 乐观并发控制;不匹配会报错 |
| `expectedProviderRevision` | string | 否 | |
| `expectedModelRuntimeRevision` | string | 否 | |
| `runtimeModel` | object | 否 | |
| `automationId` / `offPeakTaskId` / `offPeakRunType` / `botDeliveryTarget` / `toolDenylist` | | 否 | 少见 |

### 6.2 响应 —— 异步接受,不阻塞!

```json
{"id":2,"result":{"accepted":true,"sessionId":"sess_0b40e82e-...","stateRevision":2}}
```

**关键结论:session/send 是异步的。** 它立即返回 `accepted:true`(以及 `stateRevision`),然后 turn 在后台跑,turn 的结果/增量通过通知流推送(第 6 节)。**不是**同步阻塞返回最终结果。

- 若会话已有正在运行的 prompt,再发 send 会得到:`-32010 "A prompt is already running for this session"`(实测)。
- 发 send 时可能先出现 `session/requestRuntimePreferences`(scope `user-execution`)server 请求,必须应答(见 2.3)。
- 收到 send 接受后,会有 `state.updated` 通知:`{"method":"state.updated","params":{"type":"state.updated","scope":"session","workspace":{...},"sessionId":"...","revision":2,"reason":"prompt_started","patch":{"status":"running"}}}`。

### 6.3 一个重要的可靠性发现(实测)

在部分探测中,`session/send` 返回 `accepted:true` 之后,**stdout 上没有任何通知**,但 app-server 内部日志显示 turn 实际已 `turn.started → turn.completed`(或 `model.network.failed → turn.failed`)。也就是说:
- 通知推送不是 100% 可靠的(可能与模型请求失败/重试路径、事件循环状态有关)。
- 模型网络失败会进入重试(`model.request.status` 里 `maxAttempts: 11`),turn 可能持续几分钟且不产出可见事件。
- **因此后端实现不能只依赖通知**;应以 `session/read`/`session/events` 轮询为准,并配合超时兜底(见第 7 节)。

---

## 7. 事件流(Event Stream)

### 7.1 服务端推送的形式

三类通知(都是**无 id** 的 notification):

1. **`session/event`**(仅订阅后推送;带完整内容)
2. **`v4/telemetry/event`**(未订阅也会推;只含统计/生命周期,不含文本内容)
3. **`state.updated`**(会话状态补丁)
4. **`process/resourceSample`**(周期性资源采样,约每 60s)

### 7.2 `session/event` 通知(订阅后)

真实示例:

```json
{"method":"session/event","params":{
  "deliveryKind":"desktop-continuous",
  "eventId":"5fcb907d-a25e-4887-b036-a71c175f3395",
  "payload":{"assistantMessageId":"msg_mssqw4rg_6cc132e0-...","delta":"ok","done":false,"kind":"text_delta"},
  "seq":6,
  "sessionId":"sess_25dd7b36-...",
  "timestamp":1786699740753,
  "traceId":"df15e5b2-8411-4ae1-a24a-09f6d058e2c6",
  "turnId":"turn_e626aacb-0fb6-4bcd-a6e1-7de5dca363b3",
  "type":"model.streaming"
}}
```

- envelope 字段:`deliveryKind / eventId / payload / seq / sessionId / timestamp / traceId / turnId? / type`。
- `seq` 是会话内单调递增事件序号;`type` 是事件类型。
- 事件类型枚举(来自 bundle `U1a` + 实测):
  `session.created / session.resumed / session.updated / session.titleUpdated / session.closed / turn.started / turn.steerQueued / turn.steerDrained / turn.completed / turn.failed / message.upserted / message.removed / part.started / part.delta / part.upserted / part.removed / model.streaming / tool.updated / permission.requested / permission.resolved / userInput.requested / userInput.resolved / checkpoint.created / rewind.triggered / streamRecovery.updated`

**实测完整事件序列(一次成功 turn)**,按 `seq` 顺序:

| seq | type | payload 关键字段 |
|---|---|---|
| 1 | `session.titleUpdated` | `{previousTitle:"", source:"first_input", title:"<prompt>"}` |
| 2 | `turn.started` | `{turnNumber:0, input:"<prompt>", messageId:"msg_mss...", foregroundExecutionId:"runtime_command_1", queryId:"query_..."}` |
| 3 | `session.updated` | `{messageCount:6, model:"bigmodel/GLM-5.2", modelRef:{...}, toolCount:29, iteration:0}` |
| 4 | `session.updated` | model_request_started: `{baseURL, maxAttempts:11, model:{...}, requestId, spanId, parentSpanId, querySource:"main_turn", queryId, providerKind:"anthropic", transport:"sse", turnId, modelCall:{operation:"agent_step", stepIndex, reasoning:{...}}, attempt:1, requestHeaderCount, requestHeaders, timestamp, type:"model_request_started"}` |
| 5 | `session.titleUpdated` | 生成标题 `{modelRef:{...,role:"lite",...}, messageID, previousTitle, source:"generated", title}` |
| 6 | `model.streaming` | **thinking 增量**:`{assistantMessageId, delta:"The", done:false, kind:"reasoning_delta"}` |
| 7 | `model.streaming` | thinking 增量 `{delta:" user wants me to reply with exactly the word \"ok\", nothing else.", kind:"reasoning_delta"}` |
| 8 | `model.streaming` | **文本增量**:`{assistantMessageId, delta:"ok", done:false, kind:"text_delta"}` |
| 9 | `session.updated` | model_request_completed: `{..., durationMs, responseHeaderCount, responseHeaders, providerRequestId, finishReason:"stop", usage:{inputTokens,outputTokens,totalTokens,cacheReadTokens,cacheWriteTokens}, timeToFirstProviderEventMs, timeToFirstContentMs, timeToFirstTextMs, streamStallCount, streamOutputCommitted, type:"model_request_completed"}` |
| 10 | `session.updated` | 内容提交: `{content:"ok", contextWindow, querySource:"main_turn", stopReason:"stop", usage:{...}, cacheHit:{...}, contextUsageBreakdown:[{source:"system_prompt",chars:2624},...], toolCallCount:0}` |
| 11 | `turn.completed` | `{response:"ok", tokenCount:16762, usage:{source:"provider", modelRequestCount:1, inputTokens, outputTokens, totalTokens, cacheReadTokens, cacheWriteTokens, reasoningTokens, webFetchRequests, webSearchRequests}, toolCallCount:0, historyRoundCount:1, duration:8136, resultType:"success", cacheStats:{totalMessages:7, cachedMessages:6, lastCacheHit, cacheReadTokens}}` |

要点:
- **文本/思考增量**:`model.streaming` payload 的 `kind` 为 `text_delta`(assistant 文本)或 `reasoning_delta`(thinking),`delta` 是本次增量,按事件顺序拼接即得完整内容;`done` 字段标记流结束(末尾 chunk 为 `true`)。
- turn 结束标识:`turn.completed`(成功,payload 有 `response` 全文 + usage)。失败时服务端会推 `turn.failed` 或 telemetry `turn.terminal` `status:"failed"`(见下)。
- `turn.started` 里 `turnNumber` 从 0 开始;`queryId`、`messageId` 可用于关联。

### 7.3 `v4/telemetry/event` 通知(未订阅也会来)

真实示例(turn 生命周期,`kind` 字段是类型):

```json
{"method":"v4/telemetry/event","params":{
  "version":1,"eventId":"e575bbb9-...","eventSeq":2,"occurredAt":1786699512763,
  "sessionId":"sess_56f48a4c-...","turnId":"turn_86b851b4-...",
  "kind":"turn.started"
}}
```

实测 `kind` 及 payload:

- `turn.started` — 无额外字段。
- `turn.terminal` — `{status:"success"|"failed", resultType, durationMs, tokenCount, toolCallCount}`;失败时还有 `errorCode`、`errorMessage`、`turnPhase`。真实失败例:
  ```json
  {"kind":"turn.terminal","status":"failed","errorCode":"provider_not_configured",
   "errorMessage":"Model provider is missing an API key: zai","turnPhase":"processing_input"}
  ```
- `model.request.status` — `{requestId, status:"model_request_started"|"model_request_completed", providerId, modelId, providerKind:"anthropic", providerHostname:"open.bigmodel.cn", transport:"http"|"sse", querySource:"session_title"|"main_turn", queryId, attempt, maxAttempts:11, durationMs?(completed 时)}`。
- `stream.chunk` — `{channel:"thought"|"text", chunkLength, firstChunk, assistantMessageId}`。**注意:telemetry 版只给长度、不给内容**;内容要订阅后从 `session/event` 的 `model.streaming` 拿,或从 `session/read` 拿。
- `usage.delta` — `{requestId, providerId, modelId, providerKind, providerHostname, inputTokens, outputTokens, totalTokens, reasoningTokens, cacheReadTokens, cacheWriteTokens}`。

### 7.4 `state.updated` 通知

```json
{"method":"state.updated","params":{
  "type":"state.updated","scope":"session",
  "workspace":{"workspacePath":"/tmp/zprobe-work","workspaceKey":"zprobe"},
  "sessionId":"sess_0b40e82e-...",
  "revision":2,
  "reason":"prompt_started",          // 或 prompt_completed / prompt_failed
  "patch":{"status":"running"}
}}
```

- 出现在 prompt 开始/结束/失败等状态迁移时;`patch` 是会话状态增量(如 `{"status":"running"}`,或完整 settings 快照)。`revision` 与 `session/send` 响应里的 `stateRevision` 一致。
- schema(`Z1a`):`{type:"state.updated", scope:"server"|"workspace"|"session", workspace?, sessionId?, revision:int≥0, reason?, patch:unknown}`。

### 7.5 `process/resourceSample`

```json
{"method":"process/resourceSample","params":{
  "platform":"darwin","arch":"arm64","logicalCpuCount":10,"intervalMs":60001,
  "cpuCores":0.0261,"cpuPercent":0.2605,"rssKb":409024
}}
```

- 约每 60s 一条,进程级资源采样;后端可忽略。

---

## 8. session/read、session/messages、session/events

### 8.1 session/read —— 获取完整会话(含消息)

请求:`{"id":3,"method":"session/read","params":{"sessionId":"sess_..."}}`
params schema(`iHt`):`{sessionId, deliveryKind?, messageLimit? (int>0), afterSeq? (int≥0)}`

响应:完整 snapshot(同 create/resume 结构,但 `messages` 有内容)。turn 之后 `runtime` 会多出 `contextUsage`、`deliveryKind` 字段,`projection.status` 反映会话状态。

消息结构(`messages[]`,每条 `{info, parts}`):
- `info`:role 区分。user:`{messageId, sessionId, role:"user", time:{created}, agent, model, tools:{<toolName>:bool,...}, semantics:{origin, kind, uiVisibility, providerVisibility, transcriptVisibility}}`;assistant:`{messageId, sessionId, role:"assistant", time:{created,completed}, parentMessageId, agent, model, path:{cwd,root}, cost, tokens:{input,output,reasoning,cache:{read,write}}, finish:"stop"|"completed", semantics:{...}}`。
- `parts[]`:discriminated union on `type`(实测出现的):
  - `{"type":"text","text":"..."}` — 文本
  - `{"type":"reasoning","text":"..."}` — 思考
  - `{"type":"step-start","snapshot"?}` / `{"type":"step-finish","reason","cost","tokens"}` — 执行步标记
  - `{"type":"timeline","timelineType":"model_change","display":"separator",...}` — 模型切换分隔
  - 其它类型(bundle `Wze`):`file / tool / snapshot / patch / compaction / subagent / agent / retry`

真实 assistant 消息(回复 "ok"):

```json
{
  "info": {
    "agent":"zcode-agent","cost":0,"finish":"stop",
    "messageId":"msg_mssqw4rg_6cc132e0-...",
    "model":{"modelId":"GLM-5.2","providerId":"bigmodel","variant":"max"},
    "parentMessageId":"msg_mssqw3og_5d6c0533-...",
    "path":{"cwd":"/tmp/zprobe-work","root":"/tmp/zprobe-work"},
    "role":"assistant",
    "semantics":{"origin":"agent_runtime","kind":"assistant_response","uiVisibility":"visible","providerVisibility":"visible","transcriptVisibility":"visible"},
    "sessionId":"sess_...",
    "time":{"created":1786699737629,"completed":1786699740764},
    "tokens":{...}
  },
  "parts": [
    {"type":"step-start","partId":"...","messageId":"...","sessionId":"..."},
    {"type":"text","text":"ok","partId":"...","messageId":"...","sessionId":"..."},
    {"type":"step-finish","reason":"completed","cost":0,"tokens":{...},"partId":"...","messageId":"...","sessionId":"..."}
  ]
}
```

### 8.2 session/messages —— 只取消息列表

请求:`{"id":5,"method":"session/messages","params":{"sessionId":"sess_..."}}`
params schema(`aHt`):`{sessionId, afterMessageId?, limit? (int>0)}`
响应:`{"messages":[{info,parts},...]}`(无 projection/settings 等)。可用于增量拉取 `afterMessageId` 之后的新消息。

### 8.3 session/events —— 拉取事件日志

请求:`{"id":2,"method":"session/events","params":{"sessionId":"sess_..."}}`
params schema(`sHt`):`{sessionId, afterSeq? (int≥0), limit? (int>0)}`
响应:`{"events":[<eventEnvelope>]}`(eventEnvelope 即 7.2 的 envelope,含 `seq/type/payload`)。无事件时为 `{"events":[]}`。`afterSeq` 可用于增量消费(配合 `seq` 单调递增)。

---

## 9. 其它 session 方法

### session/stop

```json
{"id":4,"method":"session/stop","params":{"sessionId":"sess_..."}}
```
- params:`{sessionId}`;响应:`{}`(实测)。作用:中止正在运行的 prompt(abort controller),并暂停 active goal(若有)。

### session/close

```json
{"id":3,"method":"session/close","params":{"sessionId":"sess_..."}}
```
- params schema(`bHt`):`{sessionId, expectedPersistence?: "immediate"|"deferred"}`
- 响应:成功 `{"closed":true}`;若 `expectedPersistence` 与会话实际 persistence 不匹配:`{"closed":false}`(实测)。

### session/setModel

```json
{"id":2,"method":"session/setModel","params":{"sessionId":"sess_...","model":{"providerId":"bigmodel","modelId":"GLM-5.2"}}}
```
- params schema(`_Ht`):`{sessionId, model:{providerId,modelId,variant?}, runtimeModel?, expectedRevision?, persistAsWorkspaceLastUsed? (default true)}`
- 响应:完整 snapshot。
- **注意大小写敏感**:modelId 必须命中模型目录,如 `bigmodel/glm-5.1`(小写)可用,`GLM-5.1` 会报 `-32603 "Unsupported model: bigmodel/GLM-5.1. Available models: main, bigmodel/GLM-5.2, lite, zai/glm-5.1, zai/GLM-5.2, bigmodel/glm-5.1, bigmodel/glm-4.7."`(实测)。

### session/setThoughtLevel

```json
{"id":3,"method":"session/setThoughtLevel","params":{"sessionId":"sess_...","thoughtLevel":"high"}}
```
- params schema(`vHt`):`{sessionId, thoughtLevel?, runtimeModel?, expectedRevision?, persistAsWorkspaceLastUsed?}`
- handler 层要求 `thoughtLevel` 必填(缺省报 `-32602 "thoughtLevel is required"`);响应为完整 snapshot。

### session/setMode

```json
{"id":4,"method":"session/setMode","params":{"sessionId":"sess_...","mode":"edit"}}
```
- params schema(`xHt`):`{sessionId, mode:"plan"|"build"|"edit"|"yolo"|"auto", expectedRevision?}`;响应为完整 snapshot。

### 其它(简要)

- `session/subagents` — params `{sessionId, endedCursor?, endedLimit?}`;响应 `{revision, childSessionIds, running:[...], ended:{total,items,nextCursor?}}`。**实测对内存会话报 `-32004 "Session not found"`**(它查的是持久化 session store),只对 Desktop 持久会话有效。
- `session/cancelBackgroundTask` — `{sessionId, taskId}`。
- `session/usage` — 返回 token 用量统计。
- `session/fork` / `session/compact` / `session/goal` / `session/updateRuntimeModelConfig` / `session/requestRuntimePreferences`(server→client)——存在,未展开。
- `workspace/generateText` — `{workspace, modelRef:{providerId,modelId}, prompt, querySource, maxOutputTokens?, temperature?}` → `{text, modelRef}`。非会话式一次性生成,可用作后端轻量调用。

---

## 10. 会话连续性(Session Continuity)

- **sessionId 格式**:`sess_<uuid>`(如 `sess_0b40e82e-781a-41cf-9ea7-f8327972e6c6`)。turnId:`turn_<uuid>`;messageId:`msg_mss…_<uuid>`(前缀带消息种类,如 `msg_mssr12j8_…` 用户消息、`msg_mssr152a_…` assistant 消息)。
- **session 是进程内存态**:session/create 创建的会话不会写入持久化 store(即使 `persistence:"immediate"`),因此:
  - 跨进程 `session/resume` 报 `-32004 Session not found`。
  - 同进程内 `session/resume` 可以恢复(返回同一会话)。
- **多轮 turn**:同一进程内,`session/send` 每次携带同一个 `sessionId` 即可连续多轮(每轮 accept 后 turn 异步跑,事件流持续,`seq` 继续累加)。不订阅也能收到 `v4/telemetry/event` + `state.updated`;订阅后收到带内容的 `session/event`。
- **推荐后端生命周期**:spawn 一个 app-server 进程 → `session/create` → `session/subscribe` → 循环 `session/send` / 消费通知(或轮询 `session/read`)→ `session/close` → 关闭 stdin 让进程退出。不要在进程间迁移会话。

---

## 11. 验证:一次真实 turn 的完整请求/响应/事件序列

以下为真实探测得到的一次成功 turn(workspace `/tmp/zprobe-work`,模型 `bigmodel/GLM-5.2`,prompt 极短;token 消耗 ~16.7k(大部分为 cache read))。

### 11.1 session/create

```
SEND: {"id":1,"method":"session/create","params":{"workspace":{"workspaceKey":"zprobe","workspacePath":"/tmp/zprobe-work"},"model":{"providerId":"bigmodel","modelId":"GLM-5.2"}}}

SRV-REQ: {"id":"server-1","method":"session/requestRuntimePreferences","params":{"sessionId":"sess_0b40e82e-...","scope":"runtime-materialization"}}
SRV-RESP-SENT: {"id":"server-1","result":{"nativeSearchEnhancementsEnabled":false,"memoryEnabled":false,"askUserQuestionAutoResolutionEnabled":true,"modelContextBudgetStrategy":"preflight-v1"}}

CLI-RESP: {"id":1,"result":{"messages":[],"projection":{...},"protocol":{"name":"ZCode Protocol","version":1},"runtime":{...},"session":{"sessionId":"sess_0b40e82e-781a-41cf-9ea7-f8327972e6c6","model":{"modelId":"GLM-5.2","providerId":"bigmodel"},...},"settings":{...},...}}
```

### 11.2 session/subscribe

```
SEND: {"id":5,"method":"session/subscribe","params":{"sessionId":"sess_0b40e82e-...","deliveryKind":"desktop-continuous"}}
CLI-RESP: {"id":5,"result":{"eventSeq":0,"events":[],"sessionId":"sess_0b40e82e-..."}}
```

### 11.3 session/send(接受)

```
SEND: {"id":2,"method":"session/send","params":{"sessionId":"sess_0b40e82e-...","content":"reply with exactly the word ok, nothing else"}}
CLI-RESP: {"id":2,"result":{"accepted":true,"sessionId":"sess_0b40e82e-...","stateRevision":2}}
```

### 11.4 事件通知(按到达顺序,节选关键)

```
NOTIFY state.updated   {"type":"state.updated","scope":"session","sessionId":"sess_0b40e82e-...","revision":2,"reason":"prompt_started","patch":{"status":"running"},"workspace":{"workspacePath":"/tmp/zprobe-work","workspaceKey":"zprobe"}}

SEVENT  seq=1 type=session.titleUpdated payload={"previousTitle":"","source":"first_input","title":"reply with exactly the word ok, nothing else"}
SEVENT  seq=2 type=turn.started       payload={"turnNumber":0,"input":"reply with exactly the word ok, nothing else","messageId":"msg_mssr12j8_...","foregroundExecutionId":"runtime_command_1","queryId":"query_..."}
SEVENT  seq=3 type=session.updated    payload={"messageCount":6,"model":"bigmodel/GLM-5.2","modelRef":{...},"toolCount":29,"iteration":0}
SEVENT  seq=4 type=session.updated    payload={model_request_started, baseURL:"https://open.bigmodel.cn/api/anthropic", requestId, modelCall:{operation:"agent_step",stepIndex:0,...}, attempt:1, maxAttempts:11, transport:"sse", querySource:"main_turn"}
SEVENT  seq=5 type=session.titleUpdated payload={source:"generated", title:"Requesting Exact OK-Only Reply", modelRef:{role:"lite",...}}
SEVENT  seq=6 type=model.streaming    payload={"assistantMessageId":"msg_mssr152a_...","delta":"The","done":false,"kind":"reasoning_delta"}
SEVENT  seq=7 type=model.streaming    payload={"assistantMessageId":"msg_mssr152a_...","delta":" user wants me to reply with exactly the word \"ok\", nothing else.","done":false,"kind":"reasoning_delta"}
SEVENT  seq=8 type=model.streaming    payload={"assistantMessageId":"msg_mssr152a_...","delta":"ok","done":false,"kind":"text_delta"}
SEVENT  seq=9 type=session.updated    payload={model_request_completed, finishReason:"stop", durationMs, usage:{inputTokens:16743,outputTokens:19,totalTokens:16762,cacheReadTokens:9728,cacheWriteTokens:0}}
SEVENT  seq=10 type=session.updated   payload={"content":"ok","contextWindow":1000000,"querySource":"main_turn","stopReason":"stop","usage":{...},"cacheHit":{...},"contextUsageBreakdown":[...],"toolCallCount":0}
SEVENT  seq=11 type=turn.completed    payload={"response":"ok","tokenCount":16762,"usage":{"source":"provider","modelRequestCount":1,"inputTokens":16743,"outputTokens":19,"totalTokens":16762,"cacheReadTokens":9728,"cacheWriteTokens":0,"reasoningTokens":0,"webFetchRequests":0,"webSearchRequests":0},"toolCallCount":0,"historyRoundCount":1,"duration":8136,"resultType":"success","cacheStats":{"totalMessages":7,"cachedMessages":6,"lastCacheHit":true,"cacheReadTokens":9728}}

NOTIFY state.updated   {"type":"state.updated","scope":"session","sessionId":"sess_0b40e82e-...","revision":3,"reason":"prompt_completed","patch":{...}}
```

同 turn 的 `v4/telemetry/event`(未订阅也会有,节选):

```
NOTIFY v4/telemetry/event  {"version":1,"eventId":"...","eventSeq":2,"occurredAt":...,"sessionId":"...","turnId":"turn_...","kind":"turn.started"}
NOTIFY v4/telemetry/event  {"version":1,...,"kind":"model.request.status","requestId":"...","status":"model_request_started","providerId":"bigmodel","modelId":"GLM-5.2","providerKind":"anthropic","providerHostname":"open.bigmodel.cn","transport":"http","querySource":"session_title","queryId":"...","attempt":1,"maxAttempts":11}
NOTIFY v4/telemetry/event  {"version":1,...,"kind":"stream.chunk","channel":"thought","chunkLength":3,"firstChunk":true,"assistantMessageId":"msg_..."}
NOTIFY v4/telemetry/event  {"version":1,...,"kind":"stream.chunk","channel":"text","chunkLength":2,"firstChunk":true,"assistantMessageId":"msg_..."}
NOTIFY v4/telemetry/event  {"version":1,...,"kind":"usage.delta","requestId":"...","providerId":"bigmodel","modelId":"GLM-5.2","inputTokens":16743,"outputTokens":19,"totalTokens":16762,"reasoningTokens":0,"cacheReadTokens":9728,"cacheWriteTokens":0}
NOTIFY v4/telemetry/event  {"version":1,...,"kind":"turn.terminal","status":"success","resultType":"success","durationMs":8136,"tokenCount":16762,"toolCallCount":0}
```

### 11.5 session/read(turn 之后)

```
SEND: {"id":3,"method":"session/read","params":{"sessionId":"sess_0b40e82e-..."}}
CLI-RESP: {"id":3,"result":{ "messages":[ 3 条:timeline 分隔 + user 文本 + assistant(text "ok")], "projection":{...}, "protocol":{...}, "runtime":{...}, "session":{...}, "settings":{...}, ...}}
```

### 11.6 session/close

```
SEND: {"id":8,"method":"session/close","params":{"sessionId":"sess_0b40e82e-..."}}
CLI-RESP: {"id":8,"result":{"closed":true}}
（随后关闭 stdin,进程 exit code 0）
```

---

## 12. 后端(Go daemon)实现要点

1. **进程管理**:为每个 workspace/session 保持一个长驻 `node vendor/zcode.cjs app-server` 子进程(cwd=workspace),stdin/stdout 常开,`sessionId` 从 create 响应取。多 workspace 需要多进程(或串行复用)。
2. **必须处理 server→client 请求**:实现一个应答器,至少覆盖 `session/requestRuntimePreferences`(返回 lHt 结构的默认值)、`interaction/requestProviderRuntimeHeaders`(返回 `{"headersApplied":true}`)、`interaction/requestPermission`(返回 `{"decision":"allow"}`)、`interaction/requestUserInput`(返回 `{"value":...}`)。不回会导致 create/send 卡死或 `-32603`。
3. **模型选择**:必须显式传 `model:{providerId:"bigmodel",modelId:"GLM-5.2"}`,否则默认 `zai/GLM-5.2` 因缺 key 直接失败(`provider_not_configured`)。BigModel 的 API key 来自 `~/.zcode/cli/config.json`(`provider.bigmodel.options.apiKey`),由 app-server 自行读取,后端**不需要也不能**在协议里传 key。
4. **send 是异步的**:`accepted:true` 不代表 turn 完成。消费通知流直到 `turn.completed`/`turn.failed`(或 telemetry `turn.terminal`),并用 `session/read` 兜底确认最终状态(通知流在部分失败/重试场景下可能缺失)。
5. **超时兜底**:模型重试 `maxAttempts:11` 会让 turn 拖很久;实现应设总超时,超时可用 `session/stop` + `session/read` 收尾。
6. **事件消费**:订阅 `desktop-continuous` 后,`session/event` 的 `model.streaming`(`text_delta`/`reasoning_delta`)是 UI 增量渲染的数据源;`turn.completed` 的 `payload.response` 是最终文本;`usage.delta`/`turn.terminal.tokenCount` 是 token 统计。
7. **关闭**:`session/close` → 关闭 stdin → 等进程退出。异常退出兜底 `SIGKILL`。
8. **配置依赖**:app-server 会按 `~/.zcode/cli/config.json` 加载 provider/MCP/插件,并尝试拉起配置里的 MCP server(失败仅 warn,不影响会话)。

---

## 13. 未确认 / 备注

- `session/events` 在无事件时返回 `{"events":[]}`;有事件后的完整响应形状未在真实 turn 后单独拉取(基于 handler 代码与 7.2 的 envelope 推断为 `{"events":[...envelope...]}`),建议实现时用 `afterSeq` 增量验证。
- `session/subagents` 对内存会话返回 `-32004`(查持久化 store),其完整成功响应未实测。
- 通知流在部分模型网络失败/重试场景下会完全缺失(实测:send 已 accept、内部日志显示 turn 完成,但 stdout 无任何通知)。这是运行时行为,不是文档遗漏;后端必须以轮询为准。
- `persistence:"immediate"` 实测未把会话写入可 resume 的 store;不排除是特定环境/时序因素,标记为"未完全确认"。
