# 1.2 任务属性配置执行任务

## 1. 实现目标

- 补齐预计工作量字段与 UI，并为重复规则入口预留稳定契约。

## 2. 前置依赖

- 预计工作量无硬依赖。
- 重复规则入口依赖 1.7 的系列模型实现。

## 3. 任务切片

### 切片 A：补类型与持久化字段

- 目标文件 / 目录：
  - `server/migrations/`
  - `server/pkg/db/queries/issue.sql`
  - `apps/workspace/src/shared/types/issue.ts`
- 完成定义：
  - `estimated_minutes` 写入 schema 与类型。
  - issue 查询返回预计工作量和重复规则摘要字段。
- 验证方式：
  - query / 类型测试覆盖空值、设置值、清空值。

### 切片 B：补详情页属性 UI

- 目标文件 / 目录：
  - `apps/workspace/src/features/issues/components/issue-detail.tsx`
  - `apps/workspace/src/features/issues/components/attachment-list.tsx`
- 完成定义：
  - 详情页新增预计工作量编辑项。
  - 重复规则显示占位或摘要，不影响附件区。
- 验证方式：
  - 手动验证属性区编辑与保存。

### 切片 C：接 1.7 重复规则入口

- 目标文件 / 目录：
  - `apps/workspace/src/features/issues/components/issue-detail.tsx`
  - `apps/workspace/src/features/issues/components/`（重复规则弹层或触发器）
- 完成定义：
  - 详情页能打开 1.7 共用规则编辑入口。
  - 保存后回显摘要。
- 验证方式：
  - 联动验证“设置规则 -> 回显摘要 -> 再次编辑”闭环。

## 4. 回写要求

1. 1.7 字段名若调整，先同步更新本 `design.md` 与 `tasks.md`。
2. 实现完成后回写 `spec.md` 的完成项与交接说明。
