## 背景

仓库已经具备这批能力的关键底座：

- `issue.parent_issue_id` 已经存在，可作为父子任务关系的主存储字段。
- `issue_label`、`issue_to_label`、`issue_dependency` 已经存在，可直接复用。
- `apps/workspace` 与 `apps/web` 都有 issue 创建与详情流，因此本次产品化必须镜像落地，避免行为漂移。

当前缺口不在 schema，而在产品层：缺少可写 API、列表 / 详情 UI、关系校验和测试闭环。

## 目标 / 非目标

**目标：**

- 在现有 issue 模型上产品化父子任务、标签、依赖关系。
- 保持 workspace 作用域与现有 issue API 语义一致。
- 支持在 issue 创建与编辑时设置父任务。
- 支持 issue 详情中查看父任务、子任务、标签、依赖关系。
- 支持在 workspace 内创建标签并复用于多个 issue。
- 支持依赖关系的基本校验与阻塞关系可视化。

**非目标：**

- 不在本 change 中实现 checklist、模板、自定义字段、自定义状态流转、自动化规则、审批流。
- 不在本 change 中引入新的规划对象、甘特图或复杂图视图。
- 不在本 change 中实现 dependency 的全局 DAG 分析、批量编辑或跨项目报表。

## 决策

### 1. 继续以 `issue` 作为唯一工作项，直接产品化既有关系字段与表

父子任务继续使用 `issue.parent_issue_id`，标签继续使用 `issue_label` / `issue_to_label`，依赖继续使用 `issue_dependency`。本次不新增平行 task tree 或 planning object。

这样做的原因：

- 与现有后端、前端、agent 执行模型一致。
- 复用既有 schema，避免不必要迁移。
- 让人和 agent 继续围绕同一 issue 图谱协作。

### 2. 父任务设置沿用 issue create / update API，并在服务端做同 workspace 与防环校验

`CreateIssueRequest` 与 `UpdateIssueRequest` 都支持 `parent_issue_id`。服务端校验：

- 父任务必须存在且属于同一 workspace。
- 不能把 issue 设为自己的父任务。
- 不能把某个后代节点设为当前 issue 的父任务，避免形成环。

这样做的原因：

- 复用现有 issue 更新语义，减少额外接口。
- 校验放在服务端，避免 UI 绕过。

### 3. 标签建模为 workspace 级实体 + issue 关联，不做项目级或用户级分叉

标签通过独立 API 管理 workspace 范围内的 label 列表，再通过 issue-label 关联 API 给 issue 打标或移除。

这样做的原因：

- 与当前 schema 完全一致。
- 让多个 issue 共享统一标签集合，便于后续过滤与自动化。

### 4. 依赖关系通过单行记录表达方向，再按“当前 issue 视角”归一化展示

`issue_dependency` 保持一条记录表达一个方向关系。详情接口返回时，将所有直接关联当前 issue 的边归一化成当前 issue 视角下的 `blocks`、`blocked_by`、`related` 三类关系。

这样做的原因：

- 兼容现有 schema。
- UI 只需要消费“当前 issue 的关系分组”，不需要自行推导反向语义。

### 5. 第一版可视化以详情侧栏中的结构化分组为主，不引入单独关系图画布

依赖关系可视化先聚焦在 issue 详情内的阻塞分组和可点击 issue 引用，展示：

- 正在阻塞哪些 issue
- 被哪些 issue 阻塞
- 相关 issue

这样做的原因：

- 更符合最小可行交付。
- 风险和改动面明显小于新图形视图。

## 实现形态

1. 后端新增 label / dependency 查询与 issue 子任务查询封装。
2. 路由新增：
   - workspace labels API
   - issue labels API
   - issue dependencies API
   - issue child issues API
3. issue create / update 增加父任务校验逻辑。
4. issue detail 返回标签、依赖分组、子任务摘要。
5. `apps/workspace` 与 `apps/web` 同步增加：
   - parent issue picker
   - label picker
   - dependency editor / summary
   - issue detail 中的父任务、子任务、标签、依赖展示
6. 补齐 handler、前端、E2E 测试。

## 风险与权衡

- [父任务形成环] -> 通过服务端递归向上检查祖先链，拒绝非法更新。
- [标签重复创建导致 workspace 噪音] -> 服务端按 workspace + 名称查重并复用已有标签。
- [依赖方向语义混乱] -> 统一由后端按“当前 issue 视角”归一化返回。
- [双前端行为漂移] -> 以 `apps/workspace` 为主实现后，同步镜像到 `apps/web`。
- [环境缺少 openspec CLI] -> 本次按仓库既有结构手工维护 change 工件，后续可在具备 CLI 的环境中继续标准化校验。

## 迁移计划

1. 先补 OpenSpec change 工件，明确范围与任务分解。
2. 补后端查询、handler、路由与服务端校验。
3. 补 `apps/workspace` UI 与数据流。
4. 镜像 `apps/web`。
5. 补单测与 E2E，并完成验证。

## 未决问题

- 当前 change 不引入标签过滤器；如果后续需要 board / list 过滤，可在下一轮基于同一标签 API 扩展。
