# 智能体驾驶舱（Agent Cockpit）MVP 实现说明

本分支基于上游 `main` 的提交 `2b35f8017ab3b773e0356e562ecb04e55a7a9bd7`，需求来源为只读文件：

- `/Users/woody/Documents/MulticaAI/docs/Multica_Agent_Cockpit_Proposal.md`

## 已实现

Issue 右侧执行日志在存在运行中 task 时显示 **Open Agent Terminal**；历史运行也可从行内终端按钮进入驾驶舱。当前提供：

- WebSocket 实时显示智能体消息、思考、工具调用、命令和工具输出；
- 打开时立即补拉持久化消息，运行中每 5 秒校准一次，进入终态后再做一次最终补拉，降低 WebSocket 短暂断线造成的缺口；
- **File activity** 按文件显示成功、失败或仍待确认的编辑事件，以及事件内已有的逐行 Diff；
- **Stop Agent**：活动 task 会等待 daemon 确认本地进程已经退出、尾部 transcript 已完成上报；
- 已取消或失败运行的 **Restart**；
- **Redirect**：先停止当前运行并等待确认，只有确认成功后才向同一智能体发布定向新指令；
- 终态运行若仍是该智能体最新 task，则提供 **Continue**，发布定向后续指令。

## 复用的数据链

```text
daemon structured events
  -> task_message persistence (including provider call_id)
  -> task:message WebSocket event
  -> React Query task-message cache (seq merge)
  -> 5-second persisted reconciliation + terminal final fetch
  -> Agent Cockpit timeline and file activity
```

MVP 复用现有结构化事件协议，没有新增第二套日志通道，也没有把 Codex 改造成浏览器直连的 PTY。

## Proposal 验收矩阵

| Proposal 需求 | 状态 | 当前实现与缺口 |
| --- | --- | --- |
| 4.1 Live Terminal View | 部分实现 | 已实时显示智能体消息、工具调用、命令和结构化 stdout/stderr，并有断线补拉；没有原始 PTY、完整 Codex TUI 或 GPU 遥测。 |
| 4.2 Agent Interrupt | MVP 已实现 | Stop 先取消服务端 task；已被 daemon 领取的 task 在本地进程退出并冲刷 transcript 后写入取消确认。前端最多等待 20 秒；未确认会明确报错。不是浏览器发送原始 Ctrl+C。 |
| 4.3 Live Diff Viewer | 部分实现 | 已显示结构化文件编辑事件、逐事件 Diff，以及成功/失败/待确认状态；不是工作树实时快照，也不是完整的 commit 前 review。 |
| 4.4 Human Takeover | 部分实现 | Redirect 会在停止确认后插入定向新指令，并在可用时续接最近上下文；没有向运行中 PTY 热注入，也没有人工 shell 键盘接管。 |
| Phase 2 Control | 已实现 | 驾驶舱提供 Stop、Restart；历史执行日志保留 Retry。 |
| Phase 3 Collaboration | 部分实现 | Redirect / Continue 可用；原始 PTY takeover 仍需单独设计。 |

因此，本分支已经是可使用的结构化驾驶舱 MVP，但不等同于 Proposal 中完整的 PTY 控制系统。

## Stop / Redirect 安全语义

取消请求成功只代表服务端 task 已进入 `cancelled`。对于已经被领取的 task，daemon 会终止进程树、等待 agent 调用返回、冲刷最后一批 transcript，再调用 `cancel-ack`；服务端以幂等时间戳 `cancel_acknowledged_at` 保存确认。

- 尚未领取的 `queued` task 没有本地进程，取消响应即为完成；
- 其他活动状态由前端每 750 毫秒查询一次，最长等待 20 秒；daemon 对瞬时网络错误和 5xx 做有界重试；
- Stop 超时会显示“取消已请求，但进程停止未确认”；
- Redirect 在确认前不会发送新指令，避免旧进程与新任务并发修改同一目录。

## 文件活动可信边界

daemon 会把 provider 的 `call_id` 同时写入 `tool_use` 与 `tool_result`。前端优先按 `call_id` 精确配对，因此同一种编辑工具并发、乱序完成时不会串线。滚动升级期间，仅允许无 `call_id` 的结果匹配同样无 `call_id` 的旧事件；遇到新旧事件混杂且无法唯一判断时保留为待确认，不猜测成功。

- `applied`：结果确认成功，才累计 `+/-` 行数；
- `failed`：结果明确失败、拒绝或取消，不计入成功行数；
- `pending`：尚未收到匹配结果；task 已终止时界面标为未确认；
- 所有统计都是本次运行的结构化编辑活动，不是当前工作树的 `git diff --numstat`，也不保证等于最终落盘状态。

## 上下文连续性

Redirect / Continue 使用显式 `mention://agent/<id>` 评论定向到当前 task 的智能体。服务端已有的非 rerun 后续运行逻辑会在可用时复用最近的同一 `(agent, issue)` session 和工作目录。为了避免从旧历史记录误导性地续接最新上下文，只有该智能体最近一次运行显示 Continue。

这是一种“停止后按新指令继续”的协作语义，不等同于向正在运行的 PTY 写入字符。界面使用“可用时复用”的文案，因为 provider session 可能无法恢复。

## 尚未实现

- 浏览器直连原始 PTY、Codex/Claude 完整 TUI；
- 运行中热注入按键或人工 shell 键盘接管；
- GPU 利用率等机器遥测；
- 基于工作树快照的完整 commit 前 review。

## 2026-08-09 本地验证

```bash
pnpm -C packages/core typecheck
pnpm -C packages/core test
pnpm -C packages/core lint

pnpm -C packages/views typecheck
pnpm -C packages/views test
pnpm -C packages/views lint

cd server
IS_SANDBOX=1 go test ./...
```

结果：

- Core：120 个测试文件、1346 个测试全部通过；typecheck 与 lint 通过；
- Views：319 个测试文件、3754 个测试全部通过；typecheck 通过，lint 为 0 error、20 个既有 warning；
- Go：全部 package 通过；
- PostgreSQL 集成：取消确认的幂等/跨工作区保护，以及 `call_id` 的 daemon → DB → 用户补拉 API 链路通过；
- 四种语言的 `agents.json` 均通过 JSON 解析；
- `git diff --check` 通过；
- `make selfhost-build` 生产构建通过；部署后端 `/health`、前端登录页、验证码认证、工作区 API、同源 `/ws` 鉴权升级均通过；最新迁移为 `266_task_message_call_id`。

## 本地自托管

在仓库根目录执行：

```bash
make selfhost-build
```

构建完成后：

- 前端：`http://localhost:3000`
- 后端健康检查：`http://localhost:8080/health`
- 查看本地验证码（未配置邮件服务时）：`docker logs multica-backend-1`
- 停止服务：`make selfhost-stop`

首次运行会从 `.env.example` 生成被 Git 忽略的 `.env`，写入随机 JWT、PostgreSQL 和 VCS 密钥，并将文件权限设为 `600`。
