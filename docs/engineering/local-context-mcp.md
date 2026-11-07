# 本机对话的内置上下文 MCP

## 数据流

Multica 原有的工作区 MCP 库、智能体绑定和 daemon 注入用于管理外部 MCP，并不自动提供这里的会话、任务与智能体数据。本机对话使用的普通 Codex / Claude Code 也不经过云端 Agent 的 daemon claim。

```text
@ 会话 / 任务 → 带 ID、范围及省略标记的近期快照
             → 本机执行主进程准备账号及工作区上下文
             → 每轮内置 MCP + 原生 CLI 临时注册
模型调用 MCP → 本机历史 / 官方只读 API / 已有 Multica CLI
             → MCP 文本、图片或临时文件路径 → 原生模型 → 对话流
@ 智能体     → 原有官方执行接口 → 结果同步到本机会话
@ 新建任务   → 插入动作 mention → 发送给本机 Codex / Claude Code
             → create_task MCP → 官方 POST /api/issues → 实际任务编号与链接
```

内置服务器名为 `multica_desktop_context`，只在本机执行期间存在。它直接读取选定账号、API 地址和工作区下保存的会话，查询官方任务、讨论、智能体定义与附件。没有新增官方 Server 路由或线上部署，也不向云端创建 MCP 库记录、修改智能体绑定或同步本机历史。

## 工具

Settings operations extend this built-in server through a shared capability
registry. See [Settings MCP](settings-mcp.md) for scope, direct execution, CLI access
and the distinction between readonly context tools and settings mutations.

| 工具 | 数据及边界 |
| --- | --- |
| `current_context` | 当前工作区、会话和本次 @ 引用 ID |
| `search_context` | 当前范围的会话、任务、已定义智能体元信息；名称为 Codex / Claude Code 的执行工具条目不作为智能体候选 |
| `read_context` | 本机或官方云端会话、完整任务正文和评论讨论串、智能体定义；`has_more` / `next_cursor` 表明更早分页 |
| `read_message` | 使用字符偏移读取被会话列表截取的长消息，直到 `next_offset` 为空 |
| `read_execution` | 本机会话保存的工具调用与结果，按事件分页，不包含内部思考 |
| `read_attachment` | 本机附件或官方附件下载；4 MiB 内的 PNG/JPEG/GIF/WebP 直接返回 MCP 图片，文本附带摘录，其他文件提供临时本机路径 |
| `create_task` | “新建任务” mention 指定的创建动作；使用已验证的桌面云端账号在固定工作区创建任务，返回编号、ID 和任务链接 |

`source=local` 默认指对话流的本机会话，`source=cloud` 指官方聊天。任务和智能体始终读取官方数据。客户端与原生运行器的输出预算仍然生效，不能将一个分页或截断文本视为全部历史。没有保存的 CLI 内部状态或隐藏推理不能通过本机历史恢复。图片元信息、附件 URL 与下载路径都不能代替查看图片内容。

任务读取复用已实现的 `multica issue context`，保留完整讨论串、复合游标和服务端截断信号。官方聊天复用现有消息分页 API；读取智能体只投影公开定义字段，排除 MCP 配置、环境值及内部系统指令。读取定义不会调用或扮演该智能体；GUI 的 @ 智能体执行通路保持原样。

## 按复制的会话 ID 查询

本机会话的右键和“…”菜单提供“复制会话 ID”，复制原始 Multica 会话 ID，而非 Codex / Claude Code 的原生会话 ID。Agent 可以直接使用内置 `multica_desktop_context` 的 `read_context`，不需要先 @ 会话：

```json
{"kind":"conversation","id":"<复制的会话 ID>","source":"local","limit":10}
```

返回消息、会话元信息及 `has_more` / `next_cursor`。有更早消息时，将 `next_cursor` 的值作为 `cursor` 继续读取；长文本、工具结果和附件分别使用 `read_message`、`read_execution`、`read_attachment`。`search_context` 的会话搜索支持名称或会话 ID。重命名和归档不会改变 ID；分叉后的新会话有独立 ID。

这些查询只读取当前桌面账号及工作区的数据。复制 ID 不会授予其他账号或工作区访问权限。Codex / Claude Code 每轮执行都会收到相应的查询提示，并沿用现有临时 MCP 自动注册。

## 默认注册与生命周期

“新建任务”是动作 mention（`workspace-issue` / `multica:create-task`），不对应已有任务，也不触发创建弹窗。搜索文字会保留在 mention 文案中，发送前不会写入服务器。基础上下文模式下，主进程根据本次消息的引用标记开放 `create_task`，不把历史消息或引用快照中的同名文字视为新的创建请求。未开启完整工作区操作工具时，普通会话仍保留上述六个读取工具。

创建工具接受 `title`、可选 Markdown `description` 和稳定的 `request_id`，使用当前桌面账号及固定工作区调用已有 `POST /api/issues`；工作区和凭据不能由模型参数覆盖。运行期间重复调用同一请求键会复用结果，键相同而内容不同会拒绝。网络超时或响应不完整时不能宣称创建成功，应先查询任务；跨执行的请求键不提供服务端持久去重。提示词要求模型从当前消息及相关上下文整理任务，内容不明确时先询问，创建成功后返回实际编号及 `mention://issue/<id>` 链接。

Claude Code 的精确许可列表仅在该轮追加创建工具。@ 已定义智能体通过另一条官方执行通路运行，无法访问本机临时 MCP，因此“新建任务”和智能体调用引用不能混用；查询任务、会话和智能体定义不受此限制。

对话流仅加载 Multica 内置 MCP。Codex 先通过不执行模型的 `config/read` 读取生效的 MCP 名称，再逐项传入 `enabled=false`，同时关闭插件和账号连接器，最后注册 `mcp_servers.multica_desktop_context` 与 Bearer 环境变量。Codex 配置表会跨层合并，传入空的 `mcp_servers` 不能清除已有服务器。

Claude Code CLI 使用私有 `mcp.json` 和 `--strict-mcp-config`；SDK 使用同等的 `strictMcpConfig: true`。启动时读取已配置及已安装的插件 ID，以本次执行的 `enabledPlugins: false` 覆盖并关闭云端插件同步。SDK 继续加载 user、project、local 设置来源；独立 Skills、Hooks、CLI 登录、模型配置与项目 `AGENTS.md` / `CLAUDE.md` 保留。Claude 同时使用 `--system-prompt-snapshot off`，让续聊重新加载本次执行的提示词及临时 CLI 路径；该参数需要 Claude Code 2.1.257 或更新版本，当前本机 CLI 已核对支持。

新会话、续聊、旁支及分叉均采用上述隔离策略，不修改用户全局配置。基础工具和 Multica 内置 MCP 按原有权限策略运行；内置 MCP 的工具许可不会自动扩大 Bash 或写文件权限。参考 [Codex MCP 配置](https://developers.openai.com/codex/mcp) 与 [Claude Code MCP 配置](https://code.claude.com/docs/en/mcp)。

使用官方 MCP TypeScript SDK 的无状态 Streamable HTTP 传输，监听 `127.0.0.1` 随机端口。每次执行使用独立随机 Bearer 凭据，校验 Host 并拒绝浏览器 Origin。私有配置目录为 0700，文件为 0600；凭据不进入提示词、argv、IPC 或本机会话历史。结束、取消、切换账号及应用退出时关闭服务并清理临时配置与下载文件。协议依据 [MCP Streamable HTTP](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports)。

云端账号仍通过桌面专用 Profile 和 `/api/me` 校验，不回退到终端默认 Profile。云端不可用时，本机 MCP 读取仍可使用，提示词分别说明两者状态，避免把本机历史误判为需要重新登录。

## 验证

```sh
pnpm --filter @multica/desktop test src/local-host/local-context-mcp.test.ts src/local-host/local-multica-context.test.ts src/local-host/local-conversation-service.test.ts src/local-host/local-provider-runner.test.ts src/local-host/local-provider-fork.test.ts src/local-host/local-codex-instructions.test.ts src/renderer/src/platform/local-conversation-service.test.ts
pnpm --filter @multica/desktop typecheck
pnpm --filter @multica/desktop build
PLAYWRIGHT_BASE_URL=http://localhost:16099 pnpm exec playwright test e2e/tutti-mention-panel.spec.ts e2e/agent-html-preview.spec.ts e2e/local-provider-gui.spec.ts
git diff --check
```

自动测试不查找或执行用户安装的模型 CLI，使用实际 MCP 客户端、合成数据、模拟 API 及测试专用可执行文件，覆盖两类运行器的新会话和续聊、完整历史分页、长文本、工具结果、图片、访问隔离、取消及凭据清理。正常执行结束也会取消尚未完成的官方查询，再删除临时目录。当前相关回归为 50 项单元及协议测试、11 项浏览器交互测试，均通过；类型检查、相关 ESLint 与桌面构建通过。构建和浏览器回归需串行执行，避免构建更新 GUI 静态资源时影响运行中的测试页面。

单独的获准 Codex 验收实际调用了 `read_context` 并取回只存在于临时会话中的验证标记。模型在结果前加了操作说明，因此不以整段输出严格等于标记作为取数判据；依据实际 MCP 工具结果事件及返回标记核对。Claude 未额外调用真实模型。官方只读 MCP 验收获得 TES-124 正文、43 条评论、智能体定义并直接返回 PNG 图片，没有写入官方数据或触发任务执行。
