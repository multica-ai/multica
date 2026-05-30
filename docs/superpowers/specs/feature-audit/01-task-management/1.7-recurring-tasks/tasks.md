# 1.7 重复任务执行任务

## 1. 实现目标

- 建立重复任务的系列/实例模型，并与现有 issue 工作流打通。

## 2. 前置依赖

- 1.2 负责属性入口与摘要展示。
- 1.1 负责归档生命周期，避免实例删除语义混乱。

## 3. 任务切片

### 切片 A：补数据库与服务端规则模型

- 目标文件 / 目录：
  - `server/migrations/`
  - `server/pkg/db/queries/issue.sql`
  - `server/internal/handler/issue.go`
- 完成定义：
  - 新增系列规则与例外表。
  - 完成 issue 时可安全生成下一实例。
- 验证方式：
  - 测试覆盖 daily/weekly/monthly/yearly/custom 与重复创建保护。

### 切片 B：补前端类型与规则编辑器

- 目标文件 / 目录：
  - `apps/workspace/src/shared/types/issue.ts`
  - `apps/workspace/src/features/issues/components/issue-detail.tsx`
  - `apps/workspace/src/features/issues/components/`
- 完成定义：
  - 详情页可编辑重复规则。
  - UI 明确区分改单次与改未来。
- 验证方式：
  - 手动验证创建规则、修改规则、删除规则。

### 切片 C：补实例生成后的列表回显

- 目标文件 / 目录：
  - `apps/workspace/src/features/issues/mutations.ts`
  - `apps/workspace/src/features/issues/queries.ts`
  - `apps/workspace/src/features/issues/components/issue-list-page.tsx`
- 完成定义：
  - 完成实例后自动刷新列表并出现下一实例。
  - 规则摘要同步回显。
- 验证方式：
  - 联动验证“完成实例 -> 新实例出现 -> 再次编辑系列”闭环。

## 4. 回写要求

1. 若规则字段或例外模型调整，先同步更新 1.2 文档。
2. 实现完成后回写 `spec.md` 与 `overview.md` 的共享约束。
