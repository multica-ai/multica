# 单能力 Tasks

## 实现目标

在 5.1 / 5.2 稳定后，再补手动备份与恢复；当前阶段先冻结契约和执行前置条件。

## 前置依赖

- 5.1 canonical manifest 已落地。
- 5.2 import pipeline dry-run / apply 已落地。
- 数据管理入口已存在。

## 任务切片

### Task 1

- 目标：补备份入口与页面。
- 文件：
  - `apps/workspace/src/features/settings/components/` 或 data-management 目录
  - `apps/workspace/src/router.tsx`
- 完成定义：
  - 用户能发起“创建备份”和“恢复备份”。
- 验证方式：
  - 页面与路由测试。

### Task 2

- 目标：定义 backup bundle 类型。
- 文件：
  - `apps/workspace/src/shared/types/`
  - `apps/workspace/src/shared/api/client.ts`
- 完成定义：
  - backup bundle 与 restore result 类型稳定。
- 验证方式：
  - 类型测试。

### Task 3

- 目标：补服务端 create-backup / restore 接口。
- 文件：
  - `server/cmd/server/router.go`
  - `server/internal/handler/`
- 完成定义：
  - create-backup 能返回 bundle；restore 复用 import pipeline。
- 验证方式：
  - handler / 集成测试覆盖 checksum、schema、workspace 不匹配。

### Task 4

- 目标：回写文档并确认阶段升级。
- 文件：
  - `docs/superpowers/specs/feature-audit/05-data-sync/`
- 完成定义：
  - overview 的“低优先级”标记被显式更新为已进入实现阶段。
- 验证方式：
  - 文档回写完成。

## 执行顺序说明

- 5.3 只能在 5.1 / 5.2 之后启动；当前阶段不允许提前实现。

## 回写要求

- 回写 `overview.md` 的优先级与阶段判断。
- 回写 `spec.md` 的“低优先级”说明是否仍成立。
- 若 backup bundle 字段变化，联动回写 5.1 / 5.2 文档。
