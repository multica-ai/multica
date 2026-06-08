## 为什么

当前仓库已经具备父子任务、标签、依赖关系的底层数据基础，但这些能力还没有形成完整的产品体验。`issue.parent_issue_id`、`issue_label` / `issue_to_label`、`issue_dependency` 已经存在于数据库模型中，却仍缺少后端 API、前端入口、可视化关系与测试闭环，导致团队和智能体无法把 issue 图谱真正用于拆解、标注和阻塞协作。

这个 change 聚焦于结构化执行基础的第一批可交付切片：父子任务 UI、标签、依赖阻塞关系可视化。目标是把既有 schema 产品化，而不是引入新的平行规划对象。

## 改什么

- 产品化 issue 的父子任务能力，支持在创建 / 编辑时设置父任务，并在详情中查看父任务与子任务。
- 产品化 workspace 级标签能力，支持创建标签、给 issue 打标签、移除标签，并在 issue 详情中可视化。
- 产品化 issue 依赖关系能力，支持添加、移除、查看 `blocks` / `blocked_by` / `related` 关系。
- 为上述能力补齐后端查询、HTTP API、必要校验、前端交互，以及 `apps/workspace` 中的主产品体验。
- 补充单元测试与 E2E，确保父子任务、标签、依赖阻塞关系具备可验证证据。

## 能力

### 新增能力
- `issue-hierarchy-labels-dependencies`: 在现有 issue 图谱上提供父子任务、标签、依赖阻塞关系的完整产品能力。

### 修改能力

## 影响范围

- `server/` 中 issue 相关查询、handler、路由、事件负载与测试。
- `apps/workspace/` 中 issue 创建、详情、查询、类型与测试。
- `e2e/` 中 issue 主流程验证。
