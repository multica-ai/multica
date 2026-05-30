# 单能力 Tasks

## 实现目标

当前不执行 7.2；只保留未来重启时的收敛方向与目标文件范围。

## 前置依赖

- 产品确认 7.2 进入范围。
- 先决定第一批可书签化的对象类型。

## 任务切片

### Task 1

- 目标：确认对象边界。
- 文件：`docs/superpowers/specs/feature-audit/07-utilities/7.2-bookmarks/`
- 改动：明确首批对象只允许来自 issue view / project / repository 中的一类或多类。
- 完成定义：对象范围、owner、是否共享都已决策。
- 验证方式：设计评审通过。

### Task 2

- 目标：设计书签实体与存储。
- 文件：`server/pkg/db/queries/`、`server/internal/handler/`、`server/internal/service/`
- 改动：定义 bookmark CRUD、排序与读取接口。
- 完成定义：有稳定的数据模型与接口契约。
- 验证方式：schema 审核与接口测试计划完成。

### Task 3

- 目标：补对象侧创建入口与书签列表 UI。
- 文件：`apps/workspace/src/features/issues/`、`apps/workspace/src/features/projects/`、future bookmark 页面目录
- 改动：在首批对象页面添加“保存为书签”，并提供个人书签列表。
- 完成定义：用户可保存和重新打开书签对象。
- 验证方式：前端测试与手动验收。

### Task 4

- 目标：回写文档。
- 文件：`docs/superpowers/specs/feature-audit/07-utilities/7.2-bookmarks/spec.md`、`docs/superpowers/specs/feature-audit/07-utilities/overview.md`
- 改动：更新状态与范围。
- 完成定义：文档与真实实现一致。
- 验证方式：人工复核。

## 执行顺序说明

必须先定对象边界，再建模型，再补 UI。7.2 不允许从“先做收藏页”反推对象语义。

## 回写要求

- 若未来正式进入实现，先把本 `design.md` 从挂起状态更新为已批准方案。
- 回写范围只限 `07-utilities` 模块文档。
