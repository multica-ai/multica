# 单能力 Tasks

## 实现目标

当前不执行 7.1；只固定进入条件与未来落点，避免执行阶段误做伪实现。

## 前置依赖

- 产品明确确认 7.1 进入范围。
- 运行时 / daemon 能力有可用落点。

## 任务切片

### Task 1

- 目标：补产品确认与 owner 判定。
- 文件：`docs/superpowers/specs/feature-audit/07-utilities/7.1-blocklist/`
- 改动：确认是用户级规则还是 workspace 级规则。
- 完成定义：范围、owner、平台支持矩阵明确。
- 验证方式：设计评审通过。

### Task 2

- 目标：设计规则模型与 API。
- 文件：`server/internal/handler/`、`server/internal/service/`、`server/pkg/db/queries/`
- 改动：定义规则 CRUD、启停与状态同步接口。
- 完成定义：规则模型可表达 domain / application / schedule。
- 验证方式：接口测试计划与 schema 审阅通过。

### Task 3

- 目标：设计并实现运行时执行层。
- 文件：`apps/daemon/` 或未来 runtime 目录
- 改动：实现系统阻断、权限校验、状态回传。
- 完成定义：规则能真正影响系统行为。
- 验证方式：跨平台手动验证与集成测试。

### Task 4

- 目标：补 UI 与文档回写。
- 文件：`apps/workspace/src/features/` 下 future blocklist 页面、`docs/superpowers/specs/feature-audit/07-utilities/overview.md`
- 改动：新增配置 UI，并回写状态。
- 完成定义：文档与实现一致。
- 验证方式：人工复核 + E2E。

## 执行顺序说明

必须先产品确认，再建规则模型，再做运行时，最后才有 UI。7.1 不允许反向从页面倒推底层。

## 回写要求

- 若未来正式进入实现，先把本 `design.md` 从“挂起”更新为“已批准方案”。
- 回写范围只限 `07-utilities` 模块相关文档。
