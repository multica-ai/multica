# 故障场景推演

下面选取三个与 Multica 现有任务系统强相关的场景，至少前两个满足题目要求。

## 场景 A：Agent 崩溃时的任务泄漏

### 问题描述

Agent Daemon 在执行任务时突然被 kill，进程没有机会主动上报完成/失败。

### 代码路径

- `server/internal/handler/daemon.go`
- `server/internal/handler/task_lifecycle.go`
- `server/cmd/server/runtime_sweeper.go`
- `server/pkg/taskfailure/failure.go`
- `server/cmd/server/runtime_sweeper_test.go`

### 推演

1. 任务在数据库中已经进入 `running`。
2. Daemon 进程消失后，前端不会自动得到“任务已结束”的事件。
3. 如果没有后台 sweeper，任务会长期停留在 `running`。
4. 当前仓库通过 runtime 心跳和 `FailStaleTasks` 扫描来兜底，把超时的 `dispatched` / `running` 任务转为失败。

### 结论

任务并不会永久卡死在 `running`，前提是 sweeper 正常运行，并且能依据 `dispatched_at` / `started_at` 识别超时行。

### 证据

- `server/cmd/server/runtime_sweeper.go` 中有明确的 stale task 扫描逻辑。
- `server/cmd/server/runtime_sweeper_test.go` 里专门覆盖了 stale running task 的失败与事件广播。

## 场景 B：并发任务领取的竞态条件

### 问题描述

两个 Agent 同时尝试领取同一个待处理任务。

### 代码路径

- `server/internal/handler/daemon.go`
- `server/internal/service/task.go`
- `server/internal/service/task_claim_race_test.go`
- `server/migrations/037_fix_pending_task_unique_index.up.sql`
- `server/migrations/067_task_queue_claim_candidate_index.up.sql`

### 推演

1. 两个领取请求几乎同时到达。
2. 如果领取逻辑只是“先查后改”，就可能出现双重领取。
3. 当前仓库通过数据库约束、候选索引和原子更新路径降低竞态风险。
4. `task_claim_race_test.go` 说明这块逻辑曾经值得被单独验证。

### 结论

只要领取动作是单条事务内的原子更新，就不会把同一个任务分配给两个 Agent。

### 证据

- 领取相关逻辑集中在 `server/internal/service/task.go` 一带。
- 数据库迁移里有专门的 pending 唯一约束和 claim candidate 索引。

## 场景 C：WebSocket 重连后的状态一致性

### 问题描述

前端或 Daemon 的 WebSocket 断开后重连，期间可能错过任务状态事件。

### 代码路径

- `server/internal/daemonws/hub.go`
- `server/internal/handler/daemon_ws.go`
- `server/cmd/server/listeners.go`
- `packages/core/realtime/use-realtime-sync.ts`
- `packages/views/common/task-transcript/transcript-button.tsx`

### 推演

1. 连接断开后，实时事件可能在网络层丢失。
2. 重连时如果只依赖增量事件，不做补偿刷新，就会出现本地状态和后端状态不一致。
3. 仓库里有 reconnect 之后的失效/重拉策略，保证关键查询缓存会被刷新。

### 结论

重连机制的核心不是“把断线期间的所有事件都补回来”，而是让缓存重新与后端对齐。

### 证据

- `packages/core/realtime/use-realtime-sync.ts` 里有 reconnect 相关的全量失效逻辑。
- `server/cmd/server/listeners.go` 明确区分了需要 replay 和不需要 replay 的事件。

