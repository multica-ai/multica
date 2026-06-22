# TC-088: Agent Task Snapshot 查询性能优化索引（OPE-2566）

## 关联信息

- **OPE 编号**: OPE-2566
- **Gitee PR**: !391, !381
- **Commit SHA**: 7ae591aa6, 595b2dc9f
- **特性摘要**: 为 workspace agent task snapshot 的 latest outcome 查询与 ListWorkspaceAgentTaskSnapshot 查询添加针对性的（partial）索引，优化大数据量下的快照查询性能

## 涉及源文件

- `server/migrations/117_agent_task_queue_outcome_latest_index.up.sql`
- `server/migrations/117_agent_task_queue_outcome_latest_index.down.sql`
- `server/migrations/117_agent_task_queue_active_partial_index.up.sql`
- `server/migrations/117_agent_task_queue_active_partial_index.down.sql`
- `server/pkg/db/queries/agent.sql`
- `server/pkg/db/generated/agent.sql.go`

## 验证要点

1. 两个新增迁移可正常 up/down，且 down 能正确删除对应索引
2. 索引创建后，latest outcome 与 ListWorkspaceAgentTaskSnapshot 查询命中索引（EXPLAIN 验证使用 index scan 而非 seq scan）
3. 优化后查询结果与优化前一致（无功能性回归：返回的快照数据、排序、过滤行为不变）
4. partial index 的 WHERE 条件与查询的过滤条件匹配，能被规划器利用
5. 大数据量场景下查询延迟较优化前明显下降（性能回归基线）

## 备注

本用例为性能优化型回归（无 UI 行为变化），重点验证迁移可逆性、索引命中与查询结果一致性，建议通过后端集成测试 / EXPLAIN ANALYZE 验证，而非浏览器回归。
