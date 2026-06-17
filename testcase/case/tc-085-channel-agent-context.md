# TC-085: Channel 消息作者名 + Channel/Issue Agent 机制分离（OPE-1943）

## 关联信息

- **OPE 编号**: OPE-1943
- **Gitee PR**: 待填
- **Commit SHA**: 待填（维护者 cherry-pick 后补充）
- **特性摘要**: Channel 消息接口返回 Member/Agent 显示名；Agent 在 channel 场景下的运行时配置（CLAUDE.md/AGENTS.md）与 Issue 场景完全分离 —— channel 任务获得 channel CLI 菜单 / 回复格式 / 工作流，不再泄漏 Issue 命令菜单、Issue Metadata、Instruction Precedence、Local Preview、Skills 等 Issue 专属段落。无 thread root 时默认回复到触发消息（auto-creates thread），不再默认发顶层消息落到主消息区。Channel 文案全部集中在 Fork 专属文件 `runtime_config_channel.go`（上游无此文件），`runtime_config.go` 保持上游内联形态、仅留少量 `if isChannelTask(ctx)` hook，便于合入上游

## 涉及源文件

- `server/internal/handler/channel_v2.go`（消息响应附加 `author_name`，批量解析 Member/Agent 名称）
- `server/internal/handler/channel_v2_test.go`
- `server/internal/daemon/execenv/runtime_config_channel.go`（**Fork 专属新文件**：`isChannelTask` 谓词 + `writeChannel*` helper + channel 措辞常量）
- `server/internal/daemon/execenv/runtime_config.go`（保持上游内联形态，新增 8 处带 `Fork (OPE-1943)` 注释标记的 hook）
- `server/internal/daemon/execenv/runtime_config_test.go`

## 验证要点

1. AC1: `GET /api/v2/channels/:id/messages` 返回的每条消息带 `author_name`（Member 显示名或 Agent 名），前端无需二次查询
2. AC2: channel @mention 触发的 Agent 任务，其运行时配置包含 `multica channel context/message list/send/reply/member list` 命令菜单与 `## Channel Reply Formatting`，`issue create` 仅在「频道对话明确要求开 issue」时允许；`message reply` 排在 `message send` 之前并被标注为默认结果载体
3. AC3: channel 任务的运行时配置不包含 Issue 命令菜单条目、`## Issue Metadata`、`## Sub-issue Creation`、`## Instruction Precedence`、`## Comment Formatting`、`## Local Preview`、`## Skills`（Go 测试 `TestChannelTaskBriefOmitsIssueWorkflow`）
4. AC4: 普通 Issue 指派/评论任务的运行时配置与改动前逐字节一致（Local Preview / Skills 仍保留），不出现任何 channel 命令文案（`TestIssueTaskBriefStillCarriesIssueWorkflow` + golden 逐字节回归）
5. AC5: channel 判别优先级锁定为 chat / quick-create 优先于 channel（`TestIsChannelTaskPrecedence`）
6. AC6: 无 thread root 的 channel 任务，per-turn prompt 与 brief 均默认指示 `multica channel message reply <channel-id> <triggering-message-id>`（auto-creates thread），`message send` 仅用于非回复的独立广播（`TestBuildChannelMentionPromptUsesChannelContextNotIssueWorkflow`）
