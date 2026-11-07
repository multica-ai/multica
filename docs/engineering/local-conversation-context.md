# 本机对话的 Multica 查询上下文

对话流现已默认注册[内置上下文 MCP](local-context-mcp.md)，供本机 Codex / Claude Code 查询会话、任务及智能体定义。本文保留底层 CLI 查询接口和凭据隔离的说明。

## 根因与接线

原本的本机任务引用仅拼入标题和最多 6,000 字的正文，没有查询指令、完整评论、任务元信息或图片内容。本机运行器也没有绑定桌面的 Multica 登录与工作区，因此不能把引用摘要视为完整上下文。

```text
任务引用 → 用户消息中的快照（包含任务 UUID、工作区、截断标识）
每次本机执行 → 校验桌面专用 CLI Profile 与 /api/me 的账号
             → 私有临时 CLI 配置 + 当前工作区 + 查询指令
             → Codex developer_instructions / Claude --append-system-prompt
模型按需运行 Multica CLI → 官方已有只读 API → JSON / 下载的本地文件
执行完成、失败、取消或切换账号 → 清理临时 CLI 配置
```

不会预先将全部讨论灌入模型，不会自动克隆仓库或创建任务。查询得到的任务、评论和附件属于数据，不能覆盖系统指令。普通聊天的账号验证会增加一次 `/api/me` 只读请求，没有新增云端聊天创建或发送。

## 成员引用

本地输入框的 `@` 面板提供“成员”分类，只展示当前工作区成员的头像和姓名。成员引用使用 `workspace-member` 类型，entity ID 是成员的 `user_id`，scope 保留 `workspaceId` 和 `userId`；头像、邮箱和角色不写入 mention 链接。发送与队列接收前重新核对当前工作区成员。队列恢复保留 member 引用的工作区和 user ID，运行器的 `current_context` 同样返回这些身份信息。只有用户明确要求指派时，agent 才使用 `assignee_type=member`、`assignee_id=user_id` 执行；选择成员本身不写入任务。

## 查询接口

新增 `multica issue context <任务号或完整 UUID> --output json`，复用现有两个接口：

- `GET /api/issues/{id}`：完整正文、状态、负责人、优先级、标签、自定义属性、项目和已有来源上下文。
- `GET /api/issues/{id}/comments?recent=20`：最近活动的 20 个完整讨论串，不折叠已解决讨论。`--recent` 支持 1–100，使用已有复合游标继续查询。

返回 `issue`、`comments`、去重后的 `attachments` 目录、`retrieved_at` 和 `comments_page`。`comments_page.has_more`、`next_before`、`next_before_id` 明确标识下一页；`truncated` 透传服务端的防御性截断信号。不完整游标、越工作区访问、403 或评论读取失败均返回错误，不输出看似成功的部分 JSON。

```sh
multica issue context TES-124 --output json
multica issue context TES-124 --recent 20 --before '<next_before>' --before-id '<next_before_id>'
multica attachment download '<attachment-id>' -o ./attachments
multica project resource list '<project-id>' --output json
```

图片必须下载后用运行器自己的本地图片读取工具查看，附件 URL 和目录不能代表已经看过图片。相关项目可通过 `project_id` 查询项目资源；仓库地址不能代替本地代码，工作目录不正确时应说明限制。

原有 `issue get`、`issue comment list --full`、`issue timeline`、`issue children`、`project get` 与附件下载命令继续可用。没有修改官方 Server 协议或要求线上部署；新命令目前随本地构建的桌面内置 CLI 提供，尚未发布到官方 CLI 发行版。

## 身份与生命周期

`local-multica-context.ts` 仅读取 `desktop-<API host>` 专用 Profile，校验其账号、服务地址和 `/api/me` 返回的真实账号。校验失败时给模型明确的“查询不可用”指令，并提供空凭据的隔离配置，禁止回退到用户终端的默认 Profile。

校验后的凭据快照仅保存在单次执行私有目录（0700）内的 `config.json`（0600）。主进程为子进程设置 `MULTICA_TASK_CONFIG_ROOT`，通过已有 CLI 配置机制读取；Token 不写入提示词、argv、IPC、事件或本机会话历史。命令模板显式携带非敏感的配置目录、服务地址和工作区，避免运行器的 shell 环境过滤导致作用域丢失。不会读取或修改 `~/.multica/config.json`，不会创建新的 PAT。

模型服务环境配置和 CLI 权限策略保留。Codex 先通过原生 `app-server config/read` 读取当前工作目录生效的开发者指令，再追加查询说明，作为本次执行的 `developer_instructions`；该读取不会创建线程或调用模型。无法安全读取时明确报错，避免默默覆盖用户规则。Claude 使用追加系统指令。不会替换基础系统提示词或写入用户全局配置；项目 AGENTS.md 等原生规则仍由运行器加载。

结束时清理快照；启动时清理该专属目录内的崩溃残留。切换账号或取消若发生在上下文准备期间，不启动运行器并清理快照。网络、文件或图片工具权限仍服从运行器自身设置，无法读取时必须说明，不能绕过审批或删除 daemon 标记。

## 验证

```sh
cd server && go test ./cmd/multica -run 'Test(IssueContext|RunIssueCommentList|TimelineFilterApply)' -count=1
pnpm --filter @multica/desktop test src/local-host/local-codex-instructions.test.ts src/local-host/local-multica-context.test.ts src/local-host/local-conversation-service.test.ts src/local-host/local-provider-runner.test.ts src/renderer/src/platform/local-conversation-service.test.ts
pnpm --filter @multica/desktop typecheck
pnpm --filter @multica/desktop build
git diff --check
```

自动测试使用测试专属 CLI、配置和 HTTP 服务，不查找或执行本机真实模型 CLI。覆盖完整正文、讨论分页、附件去重、只读请求、越工作区、服务失败、错误游标、服务端截断、私有配置权限、账号验证、并行执行隔离、取消清理、凭据不落会话历史，以及真实子进程接线的假运行器验收。

31 项桌面自动测试与相关 CLI 回归、类型检查、lint 和桌面构建通过。官方只读验收使用桌面专用账号查询 TES-124，成功获得正文、43 条完整讨论评论及 9 个附件，并下载 848,481 字节的 PNG。继续翻页返回空页并正确结束；提示词里的原样命令在去掉继承的 Multica 环境变量后仍查询成功，包含空格和单引号的临时路径也正常。未发送模型消息，未写入或触发任何生产任务。原生模型是否按提示词主动选用命令未额外消耗模型额度测试；Windows 命令模板未做实机验收。
