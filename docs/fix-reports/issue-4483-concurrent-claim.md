# Issue #4483 修复分析报告

## 问题概述

并发任务 claim 请求可能突破 `agent.max_concurrent_tasks` 上限，导致同一 agent 同时运行超过配置允许数量的 active 任务。

## 根因分析

`TaskService.ClaimTask` 原先将 claim 流程拆成三次独立的数据库操作：

1. `GetAgent` — 读取 agent 配置（含 `max_concurrent_tasks`）
2. `CountRunningTasks` — 统计当前 active 任务数（`dispatched` / `running` / `waiting_local_directory`）
3. `ClaimAgentTask` — 通过 `FOR UPDATE SKIP LOCKED` 领取下一条 queued 任务

`ClaimAgentTask` 本身能防止**同一条 queue 行**被重复 claim，但**无法**在并发场景下保护 agent 级别的容量判断。两个并发请求可能同时观察到 `running = 0`（当 `max_concurrent_tasks = 1` 时），随后各自成功 claim 不同的 queued 任务，最终 active 任务数变为 2。

这是典型的 **check-then-act 竞态**：容量检查与任务领取之间没有原子性保证。

## 方案选择

| 方案 | 描述 | 结论 |
| --- | --- | --- |
| A. 事务 + `SELECT ... FOR UPDATE` on agent | 在事务内锁定 agent 行，再 count + claim | 可行，但需 Go 层事务包装，增加往返 |
| B. 单条 SQL 原子化 | 在 `ClaimAgentTask` 内用 CTE 锁定 agent 并内嵌容量检查 | **采用** — 与现有 sqlc 模式一致，单次 round-trip |
| C. Advisory lock | 按 agent ID 使用 `pg_advisory_xact_lock` | 可行但不如 row lock 直观，且需额外 hash 约定 |

**最终方案（B）**：扩展 `ClaimAgentTask` SQL：

```sql
WITH locked_agent AS (
    SELECT id, max_concurrent_tasks FROM agent WHERE id = $1 FOR UPDATE
)
UPDATE agent_task_queue ...
WHERE id = (
    SELECT atq.id ...
    CROSS JOIN locked_agent la
    WHERE ... AND (active_count) < la.max_concurrent_tasks
    ...
)
```

- `locked_agent` CTE 在单条语句生命周期内对 agent 行加排他锁，串行化同一 agent 的所有 claim 请求
- 容量检查与任务选取在同一 SQL 语句中完成，消除 TOCTOU 窗口
- 保留原有 per-(issue, agent) 序列化逻辑与 `FOR UPDATE SKIP LOCKED` 行为

Go 层 `ClaimTask` 移除独立的 `GetAgent` + `CountRunningTasks` 前置检查，直接调用增强后的 `ClaimAgentTask`。

## 验证方式

1. **单元/集成测试**：新增 `TestClaimTask_ConcurrentRespectsMaxConcurrentTasks`
   - 创建 `max_concurrent_tasks = 1` 的 agent
   - 为两个不同 issue 各入队一条 queued 任务（绕过 per-issue 序列化）
   - 并发 8 个 goroutine 调用 `TaskService.ClaimTask`
   - 断言：恰好 1 次 claim 成功，active 任务数 = 1

2. **回归测试**：运行现有 claim 相关测试套件
   ```bash
   go test ./internal/handler/ -run 'ClaimTask' -count=1
   make test
   ```

3. **手动复现（修复前）**：按 Issue 复现步骤，修复后应无法突破上限

## 风险评估

| 风险 | 影响 | 缓解措施 |
| --- | --- | --- |
| agent 行锁增加 claim 延迟 | 同一 agent 的高并发 claim 会串行等待 | claim 本身已是 per-agent 语义；锁仅在单条 SQL 语句内持有，范围可控 |
| 死锁 | 与其他锁顺序冲突 | agent 锁 → task 行锁的单向顺序与现有 `FOR UPDATE SKIP LOCKED` 模式兼容；无交叉锁 agent 集合 |
| `no_capacity` vs `no_tasks` 日志合并 | 调试粒度略降 | 合并为 `no_claim`；容量与空队列在业务上均返回 nil task |
| sqlc 生成代码需同步 | CI 可能失败 | 修改 `agent.sql` 后运行 `make sqlc` |

**总体风险：低。** 改动局部、行为与产品语义一致，且修复了明确的并发安全漏洞。
