# Fixed Repo Mode Design

## 背景

#3153 要解决的是本地 daemon agent 在大仓库、非 Git VCS、不可克隆目录和超大资产项目里的执行成本问题。当前模型是：任务 claim 后，daemon 创建隔离 env，agent 再通过 `multica repo checkout <url>` 从 daemon 的 bare clone cache 创建 Git worktree。这个模型适合 GitHub 小中型仓库，但不适合 300GB 资产仓库、Perforce 工作区、已有本地 monorepo、或纯本地目录。

目标是增加一个并存模式：用户可以给 agent 配置一组固定本地路径，任务 claim 后直接锁定其中一个空闲路径并在原地执行，任务结束后释放路径。默认行为保持不变。

## 目标

- Agent 可配置固定工作目录池。
- 一个固定路径同一时间只能被一个任务占用。
- 固定工作目录任务不执行 clone / checkout，也不鼓励 agent 调用 `multica repo checkout`。
- Git、Perforce、自定义/无 VCS 都能表达，v1 不尝试统一 VCS 操作。
- 现有 Git worktree 模式保持默认且不回归。
- `cleanup_script` 先进入数据模型和 API，但 v1 不自动执行。

## 非目标

- v1 不做 Perforce 集成命令。
- v1 不自动 reset、revert、clean 或 shelve 用户目录。
- v1 不在 cloud runtime 上启用固定本地路径。
- v1 不让远端 server 验证本地路径是否真实存在；真实路径只在 daemon host 上有意义。
- v1 不改变现有 `work_dir` 任务记录语义：它仍表示 daemon 实际执行目录。

## 设计选择

采用 agent-level fixed repo pool，而不是 runtime-level pool 或纯本地 wrapper。

原因：

- #3153 的用户需求是按 agent 配置 `fixed_repo_paths`。
- agent 已经承载 runtime、custom env、custom args、MCP、model、thinking level 等执行配置，继续放在 agent 上符合现有产品模型。
- server 可以在 claim 时统一表达 “这个任务应使用 fixed repo”，daemon 只负责本机验证和执行。
- wrapper 方案太快但不是产品能力，无法支撑 UI、API、锁状态和任务可观测性。

## 数据模型

在 `agent` 表新增字段：

- `fixed_repo_enabled BOOLEAN NOT NULL DEFAULT FALSE`
- `fixed_repo_paths JSONB NOT NULL DEFAULT '[]'`
- `fixed_repo_vcs_type TEXT NOT NULL DEFAULT 'git'`
- `fixed_repo_cleanup_script TEXT`

`fixed_repo_paths` 存字符串数组，避免 Postgres `text[]` 在 Go/TS/JSON 边界上引入额外适配。API 返回仍是 JSON 数组。

`fixed_repo_vcs_type` v1 允许：

- `git`
- `perforce`
- `none`
- `custom`

新增路径锁表：

- `agent_fixed_repo_locks`
  - `agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE`
  - `path TEXT NOT NULL`
  - `task_id UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE`
  - `runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE`
  - `locked_at TIMESTAMPTZ NOT NULL DEFAULT now()`
  - `released_at TIMESTAMPTZ`
  - unique active lock on `(agent_id, path)` where `released_at IS NULL`
  - unique active lock on `task_id` where `released_at IS NULL`

锁在 server 侧持久化，原因是 claim 本身在 server 完成，且 `max_concurrent_tasks` 也是 server 侧判定。daemon 进程重启、重复 claim、任务 fail/complete 都能通过同一套生命周期释放锁。

## API

`AgentResponse` 增加：

- `fixed_repo_enabled`
- `fixed_repo_paths`
- `fixed_repo_vcs_type`
- `fixed_repo_cleanup_script`

`CreateAgentRequest` 和 `UpdateAgentRequest` 增加同名字段。更新请求保持现有 tri-state 风格：

- 字段省略：不变
- `fixed_repo_enabled: false`：关闭 fixed repo
- `fixed_repo_paths: []`：清空路径池
- `fixed_repo_cleanup_script: null`：清空脚本字段

后端校验：

- 开启 fixed repo 时，runtime 必须是 local。
- 开启 fixed repo 时，`fixed_repo_paths` 至少 1 条，最多 16 条。
- 每条 path 必须是非空字符串，trim 后长度不超过 4096。
- 路径值不在 server 上强制绝对化，因为 server 可能不是 daemon host；daemon 执行前做本机校验。
- `fixed_repo_vcs_type` 必须是允许枚举。
- `cleanup_script` v1 只保存，不执行；长度不超过 4096。

## Claim 行为

Claim 仍从 `TaskService.ClaimTaskForRuntime` 开始。claim 某个 agent 前，server 会读取 agent fixed repo 配置。

当 fixed repo 关闭时，完全走现有逻辑。

当 fixed repo 开启时：

1. 保持 `max_concurrent_tasks` 判定。
2. 查询该 agent 的可用 path。
3. 在同一个 claim 事务语义下尝试为任务插入 active lock。
4. 如果没有可用 path，视为该 agent 暂无容量，当前 runtime poll 返回 no task 或继续尝试其他 agent。
5. 如果 path 可用，任务进入 `dispatched`，claim response 携带 fixed repo metadata。

Claim response 在 `AgentTaskResponse` 上增加：

- `fixed_repo_mode: true`
- `fixed_repo_path`
- `fixed_repo_vcs_type`
- `fixed_repo_cleanup_script`

`fixed_repo_cleanup_script` 发给 daemon 但 v1 daemon 不执行，只可用于后续 UI/日志提示。

`PriorWorkDir` 处理：

- fixed repo 任务忽略 `PriorWorkDir` 对 env 选择的影响。
- fixed repo 任务仍可使用 `PriorSessionID`，因为 conversation resume 和目录选择是两件事。
- task completion/failure 继续上报 `work_dir`，值为锁定的 `fixed_repo_path`。

## Lock 生命周期

释放锁的路径：

- `CompleteTask`
- `FailTask`
- `CancelTask`
- runtime recovery / orphan recovery
- stale task sweeper 将任务置为 failed 后

为避免遗漏，释放逻辑应放在 TaskService 的任务终态转换层，而不是各 handler 分散处理。

锁释放是幂等操作：`UPDATE agent_fixed_repo_locks SET released_at = now() WHERE task_id = $1 AND released_at IS NULL`。

重复 claim 恢复：

- `ReclaimStaleDispatchedTaskForRuntime` 返回同一 task 时，应复用已有 active lock。
- 如果 lock 不存在但 agent fixed repo 仍开启，应重新分配 path。
- 如果无法分配 path，应 fail 或 cancel 该任务会造成破坏性体验；v1 推荐返回 server error 并保留 dispatched 状态给下一次 recovery，日志清楚说明 lock missing。

## Daemon 执行

daemon `Task` 结构增加 fixed repo 字段。`runTask` 选择环境时：

- `FixedRepoMode == false`：现有 `execenv.Prepare` / `execenv.Reuse` 不变。
- `FixedRepoMode == true`：
  - 校验 `FixedRepoPath` 非空。
  - 本机校验路径必须存在且是目录。
  - 使用新的 `execenv.Fixed` 或等价函数构造 `Environment`。
  - `Environment.WorkDir = FixedRepoPath`。
  - `Environment.RootDir` 使用 daemon workspaces root 下的轻量 task metadata 目录，不能等于 fixed repo path，避免 GC 删除用户仓库。
  - 在 fixed repo path 内刷新 `.agent_context/`、`.multica/project/resources.json`、provider runtime brief 等上下文文件。
  - Codex/OpenClaw 仍使用 per-task config home/root，避免污染用户全局配置。

GC 行为：

- fixed repo 任务不能把用户固定目录登记成可 GC 的 env root。
- fixed repo 的 `EnvRoot` 可以写 task meta，但它必须是 daemon 自己创建的 metadata root；GC 只能清理这个 metadata root，不能清理 fixed repo path。

环境变量：

- 增加 `MULTICA_FIXED_REPO_MODE=true`
- 增加 `MULTICA_FIXED_REPO_PATH=<path>`
- 增加 `MULTICA_FIXED_REPO_VCS_TYPE=<type>`

`multica repo checkout` 行为：

- CLI 在发现 `MULTICA_FIXED_REPO_MODE=true` 时直接返回清晰错误：
  - 当前任务已绑定固定工作目录。
  - 不会 clone 或 checkout。
  - 需要切换分支/同步代码时请使用项目自己的 VCS 工具。

daemon `/repo/checkout` 也要做同样拒绝，避免旧 CLI 或手写请求绕过。

## UI

UI 放到后续 PR。第一阶段 API 完成后，Agent settings 可以加入 “Fixed repo paths” 配置区：

- 仅 local runtime agent 可开启。
- 多行路径输入。
- VCS 类型选择：Git / Perforce / None / Custom。
- 明确提示：Multica 不会自动清理或切换 fixed repo。
- `cleanup_script` 在 v1 只保存，UI 文案明确 “not executed yet”。

## 错误处理

- fixed repo 开启但 paths 为空：Create/Update 返回 400。
- fixed repo 开启但 runtime 非 local：Create/Update 返回 400。
- claim 时所有路径被锁：不 claim 当前 agent 的任务，避免同目录并发。
- daemon 发现 path 不存在：FailTask，failure_reason 用 `agent_error`，错误信息包含 path 和 fixed repo mode。
- agent 调用 `multica repo checkout`：CLI/daemon 返回非零错误，但不直接 fail task，让 agent 有机会理解并继续。

## 测试计划

后端：

- agent create/update/list/get 会保存并返回 fixed repo 字段。
- fixed repo 校验覆盖：local runtime 可开启、cloud runtime 不可开启、空 paths 不可开启、非法 vcs_type 拒绝。
- claim 时同一 path 不会被两个任务同时锁定。
- 多路径池下并发任务分配不同 path。
- complete/fail/cancel 释放 active lock。
- stale dispatched reclaim 复用已有 lock。

daemon：

- fixed repo task 使用指定 `FixedRepoPath` 作为 agent cwd。
- fixed repo task 不调用 `execenv.Prepare` 删除/创建 workdir。
- fixed repo task 仍写 `.agent_context` 和 provider runtime config。
- missing fixed repo path 会返回明确错误。
- `multica repo checkout` 在 fixed mode 环境变量下拒绝。

TypeScript：

- Agent 类型、CreateAgentRequest、UpdateAgentRequest 增加字段。
- API response compatibility schema 若相关 endpoint 已有 zod 解析，必须同步更新 fallback。

## PR 拆分

### PR1: Agent fixed repo config

- migration 添加 agent 字段。
- sqlc query 更新。
- handler request/response 更新。
- core TS 类型更新。
- 后端校验和 handler 测试。

### PR2: Claim lock and response metadata

- migration 添加 lock 表。
- sqlc query 添加 acquire/release/list。
- TaskService claim 接入锁分配。
- task 终态释放锁。
- claim response 增加 fixed repo metadata。
- 后端并发和生命周期测试。

### PR3: Daemon fixed repo execution

- daemon Task 类型接收 fixed repo 字段。
- execenv 增加 fixed repo environment 构造。
- daemon runTask 使用 fixed path。
- CLI 和 daemon repo checkout 在 fixed mode 拒绝。
- daemon/execenv/CLI 测试。

### PR4: UI and docs

- agent settings UI。
- 文案和帮助说明。
- cleanup_script 仍只保存不执行，执行语义后续单独 issue/PR。

## 风险与缓解

- 本地路径是敏感能力：只允许 agent owner 或 workspace admin 管理，沿用现有 `canManageAgent`。
- server 无法验证 daemon 本地路径：daemon 执行前做最终校验。
- fixed repo path 可能是用户真实工作区：GC 和 cleanup 不能碰该目录。
- path 锁遗漏会卡住任务：释放必须集中在 TaskService 终态转换，并提供幂等 release query。
- Perforce 语义复杂：v1 只表达 VCS 类型和工作目录，不自动 sync/revert/shelve。
- cleanup script 风险高：v1 不执行，避免远端配置触发本地任意命令。

## 验收标准

- 默认 agent 没有 fixed repo 行为变化。
- local agent 开启 fixed repo 后，claim response 能稳定返回锁定路径。
- 同一 agent 的同一路径不会并发分配给两个任务。
- fixed repo task 的 `work_dir` 最终记录为固定路径。
- fixed repo task 内调用 `multica repo checkout` 会得到明确拒绝，而不是创建 Git worktree。
- 所有新增 Go/TS 测试通过。
