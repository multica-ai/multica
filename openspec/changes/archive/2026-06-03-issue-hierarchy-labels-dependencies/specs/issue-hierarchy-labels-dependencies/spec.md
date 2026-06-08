## ADDED Requirements

### Requirement: Issue workflows support parent and child task relationships
系统 SHALL 允许用户在同一 workspace 内为 issue 设置父任务，并在 issue 详情中展示父任务与子任务列表。

#### Scenario: Creating a child issue with a parent
- **WHEN** 用户在创建 issue 时选择一个同 workspace 的父任务
- **THEN** 新 issue 被持久化为该父任务的子任务
- **AND** 子任务详情可以看到父任务引用

#### Scenario: Viewing child issues from the parent issue
- **WHEN** 用户打开一个存在子任务的 issue 详情
- **THEN** 系统展示该 issue 的子任务列表
- **AND** 用户可以从该列表跳转到子任务详情

#### Scenario: Rejecting an invalid parent relationship
- **WHEN** 用户尝试把 issue 设为自己的父任务，或把后代节点设为父任务
- **THEN** 系统拒绝该更新
- **AND** 原有父子结构保持不变

### Requirement: Issues can be labeled with workspace-scoped tags
系统 SHALL 提供 workspace 级标签，并允许用户为 issue 添加或移除这些标签。

#### Scenario: Creating and assigning a new label
- **WHEN** 用户在 issue 详情中输入一个新的标签名并确认创建
- **THEN** 系统在当前 workspace 中创建该标签
- **AND** 新标签立即关联到当前 issue

#### Scenario: Reusing an existing label
- **WHEN** 用户在另一个 issue 上选择同 workspace 已存在的标签
- **THEN** 系统复用现有标签
- **AND** 不创建重复标签记录

#### Scenario: Removing a label from an issue
- **WHEN** 用户从 issue 上移除一个标签
- **THEN** 该标签与当前 issue 的关联被删除
- **AND** 其他 issue 上的同名标签保持不变

### Requirement: Issues expose dependency and blocking relationships
系统 SHALL 允许用户查看并维护 issue 的 `blocks`、`blocked_by`、`related` 关系，并以当前 issue 视角展示阻塞状态。

#### Scenario: Adding a blocking relationship
- **WHEN** 用户在 issue 详情中新增一个 `blocks` 关系
- **THEN** 当前 issue 的依赖分组中展示被阻塞的目标 issue
- **AND** 目标 issue 以 `blocked_by` 视角可见该关系

#### Scenario: Viewing issues that block the current issue
- **WHEN** 用户打开一个被其他 issue 阻塞的 issue
- **THEN** 详情中展示 `blocked_by` 分组
- **AND** 每个阻塞项都带有可点击的 issue 引用

#### Scenario: Rejecting an invalid dependency
- **WHEN** 用户尝试给 issue 添加指向自己的依赖关系
- **THEN** 系统拒绝该关系
- **AND** 不写入无效 dependency 记录
