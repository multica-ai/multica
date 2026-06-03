# DB-backed execution scheduler RFC

## 1. 背景

GitHub issue
[#3015](https://github.com/multica-ai/multica/issues/3015)
暴露了 `v0.3.4 -> v0.3.5` 升级路径的两个问题：

1. `task_usage_hourly` 需要历史 backfill 才能让 migration
   `103_drop_legacy_daily_rollups` 放行。
2. 持续 rollup 依赖 operator 手工注册 `pg_cron` 或外部 cron。

`pg_cron` 对一部分部署是可用的兼容路径，但不适合作为 Multica
默认依赖：

- self-host 默认的 `pgvector/pgvector:pg17` 镜像不带 `pg_cron`。
- managed Postgres / Supabase / 自建镜像对 extension、database name、
  superuser 权限的约束不一致。
- 官方多实例部署需要统一的执行审计、失败重试、lag 指标和告警。

本文决定采用 app-managed, PostgreSQL-backed scheduler。应用进程负责
枚举 due plan 并执行业务 handler；PostgreSQL 的执行记录表负责分布式
锁、lease、retry 状态和审计。

## 2. 目标与非目标

### 目标

- self-host 默认部署不需要 Redis、Kubernetes CronJob、systemd timer、
  外部 cron 或手工 `pg_cron` 注册。
- 多个 backend / worker 实例可以同时启动 scheduler，同一个
  `(job, scope, plan_time)` 最多只有一个 lease owner。
- `RUNNING` 记录在 runner 崩溃后可以按 job policy 自动重入或转为
  失败告警。
- missed plan 的 catch-up 有上限，不能因为停机几天就在恢复时无限
  回放。
- `task_usage_hourly` ongoing rollup 是首个落地用例，并继续复用现有
  `rollup_task_usage_hourly()` / `task_usage_hourly_rollup_state`。
- `pg_cron` 降级为兼容路径：存在时不破坏，缺失时不影响默认行为。

### 非目标

- 不把 scheduler 做成通用用户可配置 cron 产品。它只服务内部系统任务。
- 不要求 PostgreSQL 承担业务调度逻辑。DB 只保存时间、lease、审计和
  并发控制状态。
- 不为非幂等任务提供自动重复执行保证。非重入任务必须显式关闭 stale
  reentry。
- 不在首版引入 Redis / MQ。官方大规模场景如果需要 Redis lock 或 MQ，
  可以在 job handler 层替换执行方式，执行记录表仍保留为审计面。

## 3. 总体决策

| 问题 | 决策 |
| --- | --- |
| 表结构 | 新增 `sys_cron_executions`，一行代表一个 job 的一个 scope 在一个 canonical `plan_time` 上的一次计划执行。唯一键是 `(job_name, scope_kind, scope_id, plan_time)`。 |
| `plan_time` 时区 | `plan_time` 一律是 UTC bucket start，类型为 `TIMESTAMPTZ`。scheduler 每轮先读取 DB `now()` 作为统一时间源，再按 job cadence 计算 UTC bucket。 |
| stale `RUNNING` | `RUNNING` 持有 `lease_token`、`runner_id`、`heartbeat_at`、`stale_after`。只有 `allow_stale_reentry=true` 的 job 可以在 `stale_after < db_now` 后被其他 runner 原子 steal lease。 |
| 重入 | 自动重入只允许幂等或业务层已有互斥保护的 job。非重入 job stale 后转 `FAILED` 并告警，由人工或专门 repair job 处理。 |
| catch-up | 每个 job 显式声明 `catch_up_mode` 和 `catch_up_window`。默认不做无限 catch-up。watermark-driven job 使用 `latest_only`，只跑最新 plan，由业务 watermark 覆盖历史数据。 |
| scope 维度 | 表级唯一键包含 `scope_kind` / `scope_id`。首个 use case 使用 `global/global`；官方分布式任务可用 `shard/<n>`、`workspace/<uuid>`、`runtime/<uuid>`。 |
| observability | 表记录审计；后端导出 metrics；官方部署配置 lag / stale / failure alerts。首版不做用户可见状态页，先提供 admin SQL/API 和 dashboards。 |
| pg_cron 迁移 | app scheduler 默认接管 ongoing rollup；`pg_cron` 若仍存在，与 app scheduler 并跑也安全，因为现有 SQL 函数有 advisory lock。后续 migration / startup best-effort unschedule。 |

## 4. 表结构

首版只需要一张执行表。状态使用 `TEXT + CHECK`，避免 enum migration
给后续状态调整带来额外摩擦。

```sql
CREATE TABLE sys_cron_executions (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_name       TEXT        NOT NULL,
    scope_kind     TEXT        NOT NULL DEFAULT 'global',
    scope_id       TEXT        NOT NULL DEFAULT 'global',
    plan_time      TIMESTAMPTZ NOT NULL,

    status         TEXT        NOT NULL,
    attempt        INTEGER     NOT NULL DEFAULT 1,
    max_attempts   INTEGER     NOT NULL DEFAULT 3,
    next_retry_at  TIMESTAMPTZ,

    runner_id      TEXT,
    lease_token    UUID        NOT NULL DEFAULT gen_random_uuid(),
    heartbeat_at   TIMESTAMPTZ,
    stale_after    TIMESTAMPTZ,

    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    duration_ms    INTEGER,
    rows_affected  BIGINT,
    result         JSONB       NOT NULL DEFAULT '{}'::jsonb,

    error_code     TEXT,
    error_msg      TEXT,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_sys_cron_status
        CHECK (status IN ('RUNNING', 'SUCCESS', 'FAILED')),
    CONSTRAINT chk_sys_cron_attempt
        CHECK (attempt >= 1 AND max_attempts >= attempt),
    CONSTRAINT chk_sys_cron_duration
        CHECK (duration_ms IS NULL OR duration_ms >= 0),
    CONSTRAINT uq_sys_cron_execution
        UNIQUE (job_name, scope_kind, scope_id, plan_time)
);

CREATE INDEX idx_sys_cron_exec_job_plan
    ON sys_cron_executions (job_name, scope_kind, scope_id, plan_time DESC);

CREATE INDEX idx_sys_cron_exec_running_stale
    ON sys_cron_executions (stale_after)
    WHERE status = 'RUNNING';

CREATE INDEX idx_sys_cron_exec_failed_recent
    ON sys_cron_executions (job_name, plan_time DESC)
    WHERE status = 'FAILED';

CREATE INDEX idx_sys_cron_exec_finished
    ON sys_cron_executions (finished_at)
    WHERE status IN ('SUCCESS', 'FAILED');
```

### 字段说明

- `job_name`：代码中注册的稳定任务名，例如
  `rollup_task_usage_hourly`。
- `scope_kind` / `scope_id`：分布式 scope。全局任务固定为
  `global/global`，不要用 `NULL`，避免 nullable unique 语义。
- `plan_time`：计划执行的 canonical bucket，不是任务开始时间。它用于
  幂等、审计和 lag 计算。
- `attempt`：当前 plan 的第几次尝试。FAILED 且未达到 `max_attempts`
  时，下一轮可以按 `next_retry_at` 重试同一行。
- `lease_token`：每次 claim / steal 都生成新的 token。heartbeat 和
  terminal update 必须带 `id + lease_token + status='RUNNING'` 条件，
  防止旧 runner 在 lease 被偷后把新 attempt 覆盖成 `SUCCESS`。
- `stale_after`：lease 过期时间。它由 app 按 job 的 `stale_timeout`
  写入和 heartbeat 延长。
- `result` / `rows_affected`：小体积结构化结果。大日志不进表，只进
  structured logs。

不单独设计 `STALE` 状态。stale 是 `status='RUNNING' AND stale_after <
db_now` 这个可查询条件。这样 steal lease 可以用一条 `UPDATE ...
WHERE ... RETURNING` 完成，不需要先把状态改成 `STALE` 再二次 claim。

## 5. 时间源与 `plan_time`

### 决策

`plan_time` 的唯一时间源是 PostgreSQL `now()`。

每轮 scheduler loop 的第一步读取 DB 时间：

```sql
SELECT now();
```

app 将返回值转为 `time.Time.UTC()` 后按 job cadence 计算 bucket：

```text
eligible_time = db_now - schedule_delay
plan_time     = floor_utc(eligible_time, cadence)
```

对于 5 分钟 cadence，`2026-06-03T08:17:42Z` 的 plan_time 是
`2026-06-03T08:15:00Z`。

### 理由

- 多实例应用时钟可能有小幅 skew；同一个 Postgres 的 `now()` 是所有
  contender 共享的 canonical source。
- `TIMESTAMPTZ` 在 PostgreSQL 内部以 UTC 存储。显示时受 session
  timezone 影响，但存储值和比较语义不受影响。
- 业务数据的时间逻辑仍在业务层。DB 时间只用于调度窗口和 lease 判断。

### 约束

- 所有 scheduler SQL session 应显式 `SET TIME ZONE 'UTC'` 或只通过
  Go 的 UTC format 写入日志，避免 dashboard / log 中出现本地时区歧义。
- 不使用用户 timezone、workspace timezone、runtime timezone 计算
  `plan_time`。这些时区只属于产品报表和用户展示。

## 6. Claim、heartbeat、finish 流程

### 6.1 新 plan claim

每个 runner 对 due plan 先尝试插入：

```sql
INSERT INTO sys_cron_executions (
    job_name, scope_kind, scope_id, plan_time,
    status, attempt, max_attempts,
    runner_id, lease_token,
    heartbeat_at, stale_after,
    started_at, updated_at
) VALUES (
    $1, $2, $3, $4,
    'RUNNING', 1, $5,
    $6, gen_random_uuid(),
    $7, $7 + $8::interval,
    $7, $7
)
ON CONFLICT ON CONSTRAINT uq_sys_cron_execution DO NOTHING
RETURNING id, lease_token, attempt;
```

插入成功的 runner 拥有本次 lease；冲突说明其他 runner 已经创建了同一
plan。

### 6.2 FAILED retry 或 stale steal

冲突后，runner 可以按 job policy 尝试重试同一行：

```sql
UPDATE sys_cron_executions
   SET status        = 'RUNNING',
       attempt       = attempt + 1,
       runner_id     = $runner_id,
       lease_token   = gen_random_uuid(),
       heartbeat_at  = $db_now,
       stale_after   = $db_now + $stale_timeout::interval,
       started_at    = $db_now,
       finished_at   = NULL,
       duration_ms   = NULL,
       next_retry_at = NULL,
       error_code    = NULL,
       error_msg     = NULL,
       updated_at    = $db_now
 WHERE job_name   = $job_name
   AND scope_kind = $scope_kind
   AND scope_id   = $scope_id
   AND plan_time  = $plan_time
   AND attempt < max_attempts
   AND (
        (status = 'FAILED' AND COALESCE(next_retry_at, $db_now) <= $db_now)
        OR
        (status = 'RUNNING' AND stale_after < $db_now AND $allow_stale_reentry)
   )
RETURNING id, lease_token, attempt;
```

如果 `allow_stale_reentry=false`，stale `RUNNING` 不会被自动 steal。claim
path 或 sweeper 将它转为 `FAILED(error_code='stale_timeout')` 并告警。

### 6.3 Heartbeat

runner 运行期间每 30 秒 heartbeat 一次。长 SQL handler 也要用独立
goroutine heartbeat，不能等 SQL 返回后才更新。

```sql
UPDATE sys_cron_executions
   SET heartbeat_at = $db_now,
       stale_after  = $db_now + $stale_timeout::interval,
       updated_at   = $db_now
 WHERE id = $id
   AND lease_token = $lease_token
   AND status = 'RUNNING';
```

如果 `RowsAffected = 0`，说明 lease 已丢失，runner 必须停止或只允许
业务层的幂等收尾完成，不能再写 terminal 状态。

### 6.4 Terminal update

成功：

```sql
UPDATE sys_cron_executions
   SET status        = 'SUCCESS',
       finished_at   = $db_now,
       duration_ms   = $duration_ms,
       rows_affected = $rows_affected,
       result        = $result,
       error_code    = NULL,
       error_msg     = NULL,
       updated_at    = $db_now
 WHERE id = $id
   AND lease_token = $lease_token
   AND status = 'RUNNING';
```

失败：

```sql
UPDATE sys_cron_executions
   SET status        = 'FAILED',
       finished_at   = $db_now,
       duration_ms   = $duration_ms,
       next_retry_at = $next_retry_at,
       error_code    = $error_code,
       error_msg     = LEFT($error_msg, 4000),
       updated_at    = $db_now
 WHERE id = $id
   AND lease_token = $lease_token
   AND status = 'RUNNING';
```

terminal update 同样必须带 `lease_token`。旧 runner 在 lease 被 steal 后
返回，不能覆盖新 attempt 的状态。

## 7. Stale `RUNNING` 与重入策略

每个 job 必须声明：

| 配置 | 含义 |
| --- | --- |
| `run_timeout` | 单次 handler 的 context timeout。 |
| `stale_timeout` | heartbeat 消失多久后认为 lease 可疑。必须大于 `run_timeout`。 |
| `allow_stale_reentry` | stale 后是否允许自动 steal lease 重跑。 |
| `max_attempts` | 同一个 plan 自动尝试次数上限。 |
| `retry_backoff` | FAILED 后下一次 retry 的延迟。 |

### 允许自动 stale reentry 的任务

必须满足至少一个条件：

- handler 完全幂等，多跑不会产生额外副作用。
- handler 内部已有更细的业务互斥或 watermark，使重复调用只会 no-op
  或重算同一结果。
- handler 的外部副作用带 idempotency key，key 至少包含
  `(job_name, scope_kind, scope_id, plan_time)`。

### 不允许自动 stale reentry 的任务

`allow_stale_reentry=false`。stale 后转 `FAILED` 并告警，不自动重跑。
这类任务需要单独的 repair path，不能借 scheduler 表假装 exactly-once。

### 对 `task_usage_hourly` 的判断

`rollup_task_usage_hourly` 可以开启 stale reentry：

- 现有 SQL 函数先拿 advisory lock `4246`，并发调用时 loser no-op。
- 业务状态由 `task_usage_hourly_rollup_state.watermark_at` 推进。
- `rollup_task_usage_hourly_window(from, to)` 从 raw rows 重算 bucket 并
  upsert / delete-empty，重复执行同一窗口是幂等的。
- dirty queue drain 和 watermark update 在同一次 SQL function 调用中
  完成；连接中断时 PostgreSQL 事务回滚。

建议配置：

| 配置 | 值 |
| --- | --- |
| `run_timeout` | `25m` |
| `stale_timeout` | `30m` |
| `heartbeat_interval` | `30s` |
| `allow_stale_reentry` | `true` |
| `max_attempts` | `3` |
| `retry_backoff` | `1m`, `5m`, `15m` |

## 8. Catch-up 策略

每个 job 必须声明 catch-up mode。

### 8.1 `every_plan`

适用于每个 plan bucket 都有业务意义的任务。scheduler 按 plan_time
从旧到新补跑，但受窗口和批量限制：

- `catch_up_window`：最多补多旧的 plan，默认 `24h`。
- `max_plans_per_tick`：每轮最多 claim 几个 plan，默认 `3`。
- 超出窗口的 plan 直接跳过，并通过
  `scheduler_skipped_plan_total{reason="outside_catch_up_window"}` 计数。

### 8.2 `latest_only`

适用于业务 handler 自己有 watermark 的任务。scheduler 只 claim 最新
due plan，不枚举所有 missed plan。错过的调度 tick 不等于错过业务数据，
业务 watermark 负责从上次处理位置继续。

`task_usage_hourly` 使用 `latest_only`。停机 6 小时后恢复时，不创建 72
条 5-minute execution 记录；只创建当前 latest plan，然后
`rollup_task_usage_hourly()` 根据 `task_usage_hourly_rollup_state` 推进。

### 8.3 `task_usage_hourly` catch-up 窗口

`rollup_task_usage_hourly()` 仍保留现有 SQL 内部的单次窗口上限：

```text
v_to = LEAST(now() - 5 minutes, watermark_at + 1 day)
```

因此：

- 普通停机或部署重启后，一次 scheduler run 通常可以追平。
- 如果 watermark 落后多天，每次成功 run 最多推进 1 天，避免一条 SQL
  长时间重算多周数据。
- scheduler 可以每 30 秒 tick，但同一个 job 的 plan cadence 仍是
  5 分钟。若 lag 持续大于阈值，靠 alert 暴露，而不是无限枚举历史
  plan。

## 9. Scope 维度

`scope_kind` / `scope_id` 是调度锁粒度，不是业务权限模型。

| scope | 用法 |
| --- | --- |
| `global/global` | 全库唯一任务。`task_usage_hourly` 首版使用这个 scope。 |
| `shard/<n>` | 官方部署中把大任务拆成固定逻辑 shard，例如 `shard/00` 到 `shard/63`。 |
| `workspace/<uuid>` | 每个 workspace 独立执行且需要独立审计的任务。 |
| `runtime/<uuid>` | 每个 runtime 独立执行的任务。 |

scope 由 job 的 `ScopeProvider` 枚举。scheduler 不理解业务对象，只把
scope 放入唯一键。

### 对官方分布式系统的决策

首版 scheduler 支持 scope，但 `task_usage_hourly` 不立即分 shard。
原因是现有 rollup SQL 只有一张全局 `task_usage_hourly_rollup_state`，
并且 `rollup_task_usage_hourly()` 使用全局 advisory lock `4246`。如果
在 scheduler 层拆 shard，但 handler 仍调用全局函数，只会让多个 shard
互相 no-op 或重复竞争同一 watermark。

如果未来要把 `task_usage_hourly` 拆 shard，必须同时改业务层：

1. `task_usage_hourly_rollup_state` 增加 scope 维度。
2. `rollup_task_usage_hourly_window` 增加 shard/workspace filter。
3. dirty queue drain 只 drain 对应 scope。
4. scheduler scope 从 `global/global` 改为 `shard/<n>`。

## 10. Observability

### 10.1 Metrics

后端导出以下指标。label 中的 `scope` 使用低基数字符串：
`global`、`shard`、`workspace`，不要直接把 workspace UUID 作为
Prometheus label。

| Metric | 类型 | 含义 |
| --- | --- | --- |
| `scheduler_claim_total{job,scope,result}` | counter | claim 结果：`won`、`conflict`、`retry`、`stale_steal`、`skipped`。 |
| `scheduler_execution_total{job,scope,status}` | counter | terminal execution 数量。 |
| `scheduler_execution_duration_seconds{job,scope,status}` | histogram | handler 运行时长。 |
| `scheduler_plan_lag_seconds{job,scope}` | gauge | `db_now - latest_success_plan_time`。 |
| `scheduler_running_stale_total{job,scope}` | gauge | 当前 stale `RUNNING` 数量。 |
| `scheduler_failed_unretried_total{job,scope}` | gauge | 已失败且不会再自动 retry 的 execution 数。 |
| `scheduler_heartbeat_total{job,scope,result}` | counter | heartbeat 成功或 lease lost。 |
| `task_usage_hourly_rollup_lag_seconds` | gauge | 现有 SQL helper 的结果，继续作为业务 lag 指标。 |

### 10.2 Alerts

`task_usage_hourly` 首版告警：

- Warning：`task_usage_hourly_rollup_lag_seconds > 900` 持续 10 分钟。
- Critical：`task_usage_hourly_rollup_lag_seconds > 3600` 持续 10 分钟。
- Critical：`scheduler_failed_unretried_total{job="rollup_task_usage_hourly"} > 0`。
- Critical：`scheduler_running_stale_total{job="rollup_task_usage_hourly"} > 0`
  持续超过 10 分钟。

### 10.3 状态查询

首版不做用户可见状态页。官方部署和 support runbook 使用只读查询：

```sql
SELECT job_name, scope_kind, scope_id, plan_time,
       status, attempt, runner_id, started_at, finished_at,
       duration_ms, rows_affected, error_code, error_msg
  FROM sys_cron_executions
 WHERE job_name = 'rollup_task_usage_hourly'
 ORDER BY plan_time DESC
 LIMIT 50;
```

如果后续要做后台状态页，直接从同一张表和 metrics 派生，不再新增审计
来源。

### 10.4 Retention

新增 `prune_sys_cron_executions` 内部 job：

- `SUCCESS` 保留 30 天。
- `FAILED` 保留 90 天。
- `RUNNING` 不按 retention 删除，只通过 stale policy 转 terminal。

这个 prune job 自己也通过 `sys_cron_executions` 调度，scope 为
`global/global`，每天运行一次。

## 11. `task_usage_hourly` 首个落地用例

### 11.1 Job spec

| 字段 | 值 |
| --- | --- |
| `job_name` | `rollup_task_usage_hourly` |
| `scope` | `global/global` |
| `cadence` | `5m` |
| `schedule_delay` | `5m` |
| `catch_up_mode` | `latest_only` |
| `catch_up_window` | `24h` for skipped-plan accounting only |
| `run_timeout` | `25m` |
| `stale_timeout` | `30m` |
| `heartbeat_interval` | `30s` |
| `max_attempts` | `3` |
| `retry_backoff` | `1m`, `5m`, `15m` |
| `allow_stale_reentry` | `true` |

### 11.2 Handler

handler 只调用现有 SQL 入口：

```sql
SELECT rollup_task_usage_hourly();
```

返回值写入 `rows_affected`。`result` 可以记录：

```json
{
  "watermark_before": "2026-06-03T08:00:00Z",
  "watermark_after": "2026-06-03T08:15:00Z"
}
```

`watermark_before/after` 由 handler 在 SQL 调用前后读取
`task_usage_hourly_rollup_state.watermark_at`，便于 review 一个 execution
是否真的推进了业务状态。

### 11.3 与现有 SQL advisory lock 的关系

保留 `rollup_task_usage_hourly()` 内部的 `pg_try_advisory_lock(4246)`。

理由：

- app scheduler 表是新的默认调度锁。
- SQL advisory lock 是兼容保护：如果旧 `pg_cron` 仍在跑，或 operator
  手动调用函数，不会与 app scheduler 重算同一个窗口。
- backfill 命令也继续使用 advisory lock `4246`，和 ongoing rollup
  串行。

app scheduler 与 SQL advisory lock 是双层保护，不是重复设计。外层
负责审计和 retry，内层保护旧入口和手动入口。

### 11.4 成功标准

- 无 `pg_cron` 环境下，新写入的 `task_usage` 能在 5-15 分钟内进入
  `task_usage_hourly`。
- 多 backend 同时运行时，每个 5-minute plan 只有一条
  `SUCCESS` execution。
- 故意 kill winning process 后，stale timeout 到期，另一个 runner
  steal lease，最终 plan `SUCCESS` 或在 3 次失败后告警。
- `task_usage_hourly_rollup_lag_seconds()` 稳定低于 300 秒，短暂部署
  重启后能自动收敛。

## 12. 从 `pg_cron` 平滑迁移

### 12.1 新装 self-host

1. migrations 创建 `task_usage_hourly`、rollup SQL、`sys_cron_executions`。
2. backend / worker 启动 scheduler，默认启用
   `rollup_task_usage_hourly` job。
3. 不要求 operator 注册 `pg_cron`、systemd timer 或 Kubernetes CronJob。
4. docs 删除 "必须调度 rollup" 的外部 cron 主路径，只保留
   `pg_cron` 为兼容说明。

### 12.2 已在 v0.3.5+ 且手工注册了 `pg_cron`

1. 发布 app scheduler。
2. app scheduler 和 `pg_cron` 可以短期并跑。`sys_cron_executions`
   记录 app scheduler 的 attempt；`pg_cron` 如果先推进 watermark，app
   这次 run 可能 `rows_affected=0`，这是可接受的。
3. 新 migration 或 startup hook best-effort unschedule：

   ```sql
   DO $$
   BEGIN
       IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_cron') THEN
           PERFORM cron.unschedule('rollup_task_usage_hourly')
             FROM cron.job WHERE jobname = 'rollup_task_usage_hourly';
       END IF;
   EXCEPTION WHEN OTHERS THEN
       RAISE NOTICE 'could not unschedule pg_cron rollup_task_usage_hourly: %', SQLERRM;
   END
   $$;
   ```

4. unschedule 失败不阻塞启动。重复执行受 SQL advisory lock 保护。

### 12.3 从 v0.3.4 直接升级到带 scheduler 的版本

这是最容易被误判的路径：app scheduler 不能在 migration `103` 失败后
再修复，因为 server 尚未启动。

因此 implementation 必须把 `backfill_task_usage_hourly` 逻辑抽成可复用
库，并在 `cmd/migrate up` 中加入 migration hook：

1. 正常 apply pending migrations，直到 `102_task_usage_hourly_pipeline`
   完成。
2. 如果下一步将执行 `103_drop_legacy_daily_rollups`，检查：
   - `task_usage` 是否有历史行。
   - `task_usage_hourly_rollup_state.watermark_at` 是否落后
     `MAX(COALESCE(updated_at, created_at))` 超过 1 小时。
3. 如果落后，migrator 在同一个 migration advisory lock 下运行
   idempotent hourly backfill：
   - 按月调用 `rollup_task_usage_hourly_window(from, to)`。
   - 持有 advisory lock `4246`，和旧 cron / 手动 backfill 串行。
   - 成功后 stamp `watermark_at = now() - interval '5 minutes'`。
4. 继续执行 `103/104`。
5. server 启动后，app scheduler 接管 ongoing rollup。

这个 hook 是为了修复已发布 migration 顺序造成的启动前问题，不属于
通用 scheduler catch-up。

### 12.4 回滚

- 如果 scheduler 代码回滚，但 migration 已创建 `sys_cron_executions`，
  表可以保留，无业务影响。
- 如果 operator 仍保留 `pg_cron` job，回滚版本仍可继续靠 `pg_cron`
  推进 rollup。
- 如果已 unschedule `pg_cron` 后需要回滚到旧版本，operator 可临时重新
  注册：

  ```sql
  SELECT cron.schedule(
    'rollup_task_usage_hourly',
    '*/5 * * * *',
    $$SELECT rollup_task_usage_hourly()$$
  );
  ```

## 13. Implementation outline

1. DB migration:
   - create `sys_cron_executions` and indexes。
   - optional helper views for recent failures and latest success per job。
2. Go scheduler package:
   - job registry。
   - DB-time due-plan calculation。
   - claim / heartbeat / finish primitives。
   - catch-up mode implementation。
   - metrics hooks。
3. `task_usage_hourly` registration:
   - `rollup_task_usage_hourly` job spec。
   - handler around existing SQL function。
   - dashboard metric bridge for `task_usage_hourly_rollup_lag_seconds()`。
4. Migration/backfill reuse:
   - move `server/cmd/backfill_task_usage_hourly` core into an internal
     package。
   - add `cmd/migrate up` hook before 103。
5. Docs:
   - update self-host quickstart / troubleshooting。
   - demote `pg_cron` to optional compatibility。
   - remove manual registration as the default path。

## 14. Test plan

| 场景 | 验证 |
| --- | --- |
| 单实例 self-host，无 `pg_cron` | 写入 `task_usage` 后 app scheduler 调用 rollup，lag 收敛。 |
| 多实例并发 | N 个 scheduler 同时 tick，同一 `(job, scope, plan_time)` 只有一个 winner。 |
| conflict skip | loser 不执行 handler，只增加 conflict metric。 |
| FAILED retry | handler 故意失败，按 backoff 重试，达到 `max_attempts` 后停重试并告警。 |
| stale steal | winner heartbeat 停止，`stale_after` 到期后另一个 runner steal lease；旧 lease terminal update 不生效。 |
| 非重入 job | `allow_stale_reentry=false` 时 stale 转 FAILED，不自动重跑。 |
| latest-only catch-up | 停 scheduler 6 小时后恢复，只创建 latest plan，不枚举 72 个 missed plan。 |
| `task_usage_hourly` watermark catch-up | watermark 落后多天时，每次 run 最多推进 1 天，lag 逐步下降。 |
| pg_cron 并跑 | app scheduler 与 `pg_cron` 同时调用函数，无重复窗口写入，无错误。 |
| direct v0.3.4 upgrade | `cmd/migrate up` 在 102 后自动 backfill，103 guard 通过，不需要手工 SQL。 |

## 15. Open risks

- `cmd/migrate up` hook 会让部分升级路径变长。需要清晰日志和可配置
  throttle，避免 operator 误判为卡住。
- `latest_only` 依赖业务 handler 自己有 watermark。新 job 默认不能使用
  该模式，除非设计文档说明业务 watermark。
- scope shard resize 需要独立设计。不要在没有 scope-version 的情况下
  直接改变 `hash % shard_count`。
- metrics label 必须控制基数。workspace / runtime UUID 不进 Prometheus
  label。
