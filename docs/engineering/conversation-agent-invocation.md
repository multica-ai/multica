# 对话流中的已定义智能体调用

验收日期：2026-10-23（America/Los_Angeles）。

## 结果与范围

`@ → 智能体` 使用当前 Multica 工作区中的实际 Agent 记录，排除名称为 Codex、Claude Code 的通用执行入口（忽略大小写、空格、连字符和下划线）。本机会话按面板确认选择次数降序展示智能体，同次按最近选择时间排序，再按是否配置指令、描述或系统角色及原始顺序排序；云端会话保留配置优先的排序。共享同一个 Codex provider 的多个智能体仍保留各自身份。没有新增虚拟的 provider 选项。这一过滤仅影响智能体提及目录，不修改 Agent 记录、会话引用或右下角执行选项。

本机频次从功能上线后累计：每次在「智能体」标签确认插入计一次，之后删除引用仍计数；浏览、关闭、会话／任务引用和自动调用不计数。排序在搜索结果截断前完成。统计通过已有 `storage.read/write` 保存到 Desktop 主页面的 `StorageAdapter`（默认 `localStorage`），存储键为 `multica:local-agent-mentions:v1:` 加 API 地址、用户 ID、工作区 ID 的 JSON 数组；记录格式为 `{ version: 1, agents: { [agentId]: { count, lastUsedAt } } }`。主会话与 Side 共用同一作用域，父页面同步累计以避免覆盖，队列和草稿仍分别存储。重启后保留，不上传或跨设备同步；清除本地存储会重置次数。

同一条消息可以选择 1–5 个不同智能体。每个目标都通过 Multica 原有任务执行链路，使用自己的运行时、模型、指令和调用权限；各自结果带智能体名称出现在当前对话流。对话右下角的模型只覆盖没有 `@ 智能体` 的默认发送，不覆盖被提及智能体的配置。

本次保持两栏对话流和原「聊天」渲染器；会话、任务引用仍作为上下文。本次没有调用 Claude Code，没有新增依赖、数据库迁移或修改 vendored Tutti 源文件。

## 根因与定位证据

| 检查点 | 原行为与验证结果 |
| --- | --- |
| 目录是否只有 provider | 实际已经读取工作区 Agent，身份 ID 正确；缺少已配置智能体优先级。 |
| mention 是否丢失身份 | 富文本和桥接协议保留了 Agent ID。 |
| 发送是否执行所选 Agent | `parent-service.ts` 只把 Agent 名称、描述追加到提示词，仍调用默认会话的发送接口。第一个错误状态出现在这里。 |
| 后端是否能在一个会话中运行多个 Agent | 原发送服务固定使用 `session.AgentID`；历史任务查询也仅覆盖默认 Agent。只修改列表无法实现调用。 |
| provider 会话是否隔离 | 原会话恢复指针属于默认 Agent；多目标调用需要按 Agent 隔离读取和更新，否则可能恢复其他智能体的执行上下文。 |

根本修复是把所选 Agent ID 作为明确的执行目标传到服务端，事务性创建独立任务，再按会话加载所有目标的运行结果。兼容路径保留原单 Agent 发送接口；禁止将多目标请求降级成默认 Agent 执行。

## 数据流与代码边界

```text
工作区 Agent → @ 提及（实际 Agent ID）
  → GUI 编码引用 → parent-service 校验目标
  → POST /api/chat/sessions/{sessionId}/agent-messages
  → 校验整个目标集合 → 同一事务创建每个目标的输入和任务
  → 原 Multica daemon claim / 执行 / 流式输出 / 完成
  → GET /api/chat/sessions/{sessionId}/tasks
  → 当前对话流：合并显示一次提问，分别显示具名结果
```

| GUI 概念 | Multica 对接 |
| --- | --- |
| `agent-target` mention | 工作区 `Agent.id`，不以 provider 作为身份 |
| 目标可用状态 | 已有调用权限与运行时绑定 |
| 一次多目标提交 | 一个 invocation group，多个独立 task |
| 每个运行的输入 | 独立 `chat_input_task_id` 和 user message |
| 运行结果归属 | 实际 task Agent ID；保存调用时名称用于历史展示 |
| 当前对话的历史和流式状态 | 当前 chat session 的全部任务 |

主要路径：

- `packages/tutti-agent-gui-host/src/context-mentions.ts`：真实目录、已配置智能体优先和提及作用域。
- `packages/views/tutti-agent-gui/parent-service.ts`：引用校验、多目标发送、会话级任务分页与实时接线。
- `packages/views/tutti-agent-gui/agent-avatar.ts`：将 Multica 的 emoji 头像适配为 GUI 可用的图片 URL，保留已上传头像的解析方式。
- `packages/tutti-agent-gui-host/src/runtime/create-multica-agent-gui-runtime.ts`：提交前校验各目标，保留普通发送路径。
- `packages/tutti-agent-gui-host/src/runtime/projection.ts`：提问去重、具名回复、独立运行状态。
- `packages/core/api/{client.ts,schemas.ts}`、`packages/core/types/{chat.ts,agent.ts}`：新接口和响应校验。
- `server/internal/handler/{chat.go,chat_agent_tasks.go,daemon.go}`：请求边界、授权、历史和 provider 会话恢复。
- `server/internal/service/{task.go,chat_invocation.go}`：原子批量提交、运行元数据和默认会话指针保护。
- `server/pkg/db/queries/{chat.sql,attachment.sql}`：会话任务分页、按 Agent 恢复和独立附件绑定；已重新生成 sqlc 文件。

## 接口与兼容边界

新发送接口：`POST /api/chat/sessions/{sessionId}/agent-messages`。

```json
{
  "content": "带有原有富文本提及标记的消息",
  "agent_ids": ["实际 Multica Agent UUID"],
  "attachment_ids": ["可选的已上传附件 UUID"]
}
```

- `agent_ids` 请求长度必须为 1–5，服务端去重；每个目标必须属于当前工作区、未归档、已绑定运行时且允许当前发起者调用。
- 保留现有会话所有权、工作区和身份鉴权。创建任何输入或任务前验证整个集合；事务内重新校验目标权限、归档和运行时绑定。
- 此接口拒绝 `model` 覆盖。返回 201，保留 `message_id`、`task_id`、`supports_queue`、`queued`、`attachment_ids`，增加必需的 `agent_runs` 数组，每项为 `agent_id`、`agent_name`、`task_id`、`message_id`。前端严格校验该响应。
- 原 `POST .../messages` 协议与普通发送行为保留；传入非空 `agent_ids` 会报错，避免静默忽略目标。
- 新 `GET .../tasks?limit=200&before=...` 返回原 AgentTask 数组，增加可选 `chat_invocation_group_id`、`chat_invoked_agent_name`。沿用 `X-Agent-Tasks-Next-Cursor` 分页头。
- 非法参数返回 400；无调用权限返回 403；目标不存在返回 404；归档或缺少可运行绑定按原分派拒绝协议返回 409。整个目标集合失败时不留下部分消息或任务。
- 新功能需要配套更新后端。旧服务器没有新端点时请求失败，不回退成调用默认 Agent。

每个目标独立拥有输入和附件绑定。附件内容沿用已上传的不可变 URL，数据库绑定行独立；取消一个任务不会解除其他任务的附件绑定。任务组按固定 Agent ID 顺序加锁，完成事务后再广播和通知 daemon。

默认会话的 provider 恢复指针只有默认 Agent 可以读写。其他目标从当前会话中按 Agent 和运行时恢复自己的历史任务；取消、失败及正常完成均避免覆盖默认 Agent 的指针。

## 验证

以下命令均已通过：

| 命令 | 结果 |
| --- | --- |
| `make sqlc` | 成功生成，无 schema 迁移 |
| `make env-exec ARGS='-- go test -C server ./internal/handler -run TestChatAgentInvocation -count=1 -v'` | 3 个集成测试通过：目标身份/配置/恢复隔离、无权限整组拒绝、附件与取消隔离 |
| `make env-exec ARGS='-- go test -C server ./internal/handler ./internal/service -run "Chat\|chat\|DirectChat\|PinTaskSession\|ClaimTask\|RuntimeBind" -count=1'` | handler 与 service 回归通过 |
| `pnpm --filter @multica/tutti-agent-gui-host test` | 34 个测试通过，包含通用执行入口过滤、搜索和业务智能体保留 |
| Views 相关测试（9 个文件，含 parent-service、Chat 回归） | 193 个测试通过 |
| Core API 测试 `api/client.test.ts` | 127 个测试通过 |
| `pnpm --filter @multica/tutti-agent-gui-host typecheck` | 通过 |
| `pnpm --filter @multica/core typecheck` | 通过 |
| `pnpm --filter @multica/views typecheck` | 通过 |
| Views parent-service 及测试文件的 ESLint | 通过 |
| `make env-exec ARGS='-- pnpm exec playwright test e2e/tutti-agent-gui.spec.ts --trace retain-on-failure'` | 原对话流端到端回归通过 |
| `make env-exec ARGS='-- pnpm exec playwright test e2e/tutti-agent-mentions.spec.ts --trace retain-on-failure'` | 多目标提及、独立任务、同会话结果及重新加载通过 |
| `pnpm --filter @multica/tutti-agent-gui-host build` | 通过 |
| `pnpm --filter @multica/desktop build` | 通过；保留已有 emoji-picker 混合导入提示 |
| `node vendor/tutti/sync.mjs --verify` | 2,491 个上游文件与 22 个包元数据 overlay 校验通过 |
| `git diff --check` | 通过 |

自动端到端测试使用测试自有、未连接 daemon 的运行时，并写入合成结果检查历史投影，不调用用户的模型账号。服务端集成测试另行覆盖真实 claim / 完成 / 再次恢复链路。

### 真实桌面执行

在 managed 开发环境 `multica-925` 中，通过原生桌面窗口选择两个真实 Multica Agent 的提及并发送一次验收消息：

| 智能体 | 实际执行配置 | 状态与结果 |
| --- | --- | --- |
| 验收·交互评审师 | Codex，`gpt-6-luna`，自身角色指令 | completed，`MULTICA_DESIGN_AGENT_CONNECTED` |
| 验收·代码审查师 | Codex，`gpt-6-luna`，自身角色指令 | completed，`MULTICA_CODE_AGENT_CONNECTED` |

当前会话 `01a16bb4-0d1c-7870-b8f9-ec257c38b236`。两个任务分别为 `01a16bb7-623d-7f2a-bfc2-e72407abb115` 和 `01a16bb7-6236-788b-a140-0c037f8c15aa`，对应不同 provider session；默认 Codex Agent 没有额外创建任务。桌面重启后，两条具名结果仍在该会话中。通用入口过滤更新后，再次在桌面确认 @ 智能体列表只剩两位已定义智能体，并可选择生成提及；这次列表检查没有再调用模型。

本地验收截图和安全结果摘要保存在 `.tmp/agent-mentions/`：

- `05-desktop-results-after-restart.png`
- `07-desktop-agent-mentions-without-tools.png`
- `08-desktop-agent-avatars.png`
- `09-desktop-selected-agent-avatar.png`
- `real-execution-results.json`

### 智能体头像检查

原桥接层只传递图片头像，并把 `emoji:` 头像置空，导致 GUI 将不同智能体都回退成 Codex 图标。现在使用共享的 `parseAvatarEmoji` 读取实际头像，通过原有 `iconUrl` 字段传递；图片头像继续走现有公开文件 URL 解析，无头像时使用机器人占位。SVG 中的文本已转义，不加入可执行内容或外部资源。宿主层仅用 Multica 的 `--muted` token 和圆形裁切补齐图片端口的样式，不修改上游 GUI 源文件或接口协议。

本次验证：

- `pnpm --filter @multica/views exec vitest run tutti-agent-gui/parent-service.test.ts`：19 个测试通过，覆盖不同 emoji、图片头像和缺省头像。
- `pnpm --filter @multica/tutti-agent-gui-host test`：34 个测试通过。
- Views、GUI host 类型检查，以及 parent-service、agent-avatar 的 ESLint：通过。
- GUI host 与桌面构建：通过；managed 桌面已重启。
- `make env-exec ARGS='-- pnpm exec playwright test e2e/tutti-agent-mentions.spec.ts --trace retain-on-failure'`：通过，验证两位同 provider 智能体的不同头像均实际加载，并保留多目标执行与结果恢复。
- 已检查浅色端到端截图和深色桌面实窗；列表、已选 @ 标签及历史提及显示同一个智能体的实际头像。本次没有调用真实模型。

emoji 字形使用系统字体，跨系统外观可能略有差异；未验收 Windows/Linux 桌面实窗。

## 限制与未覆盖范围

- 真实模型验收只覆盖 Codex；按用户要求未调用 Claude Code。
- 同一 Agent 仍遵循 Multica 原有排队约束，不同目标可分别执行；不保证输出完成顺序与提及顺序相同。
- 原「聊天」渲染器未改动。为保持每个任务独立的输入所有权，底层保存每个目标的 user row；本次仅在「对话流」中合并显示同组提问。若在原「聊天」打开多目标会话，它可能展示多条底层输入。
- 尚未进行多台物理运行时同时断线、重连的真实模型压力验收；目标权限、附件、取消和恢复隔离由自动测试覆盖。
