# 单能力 Tasks

## 实现目标

当前阶段不进入 5.4 实现；仅在未来产品阶段升级后，按前置条件启动真正多端同步项目。

## 前置依赖

- 必须先有本地状态存储方案。
- 必须先有同步队列与冲突模型。
- 必须先有同步状态 UI 与手动触发入口。
- 必须先更新 `05-data-sync/overview.md` 的阶段判断。

## 任务切片

### Task 1

- 目标：定义本地状态与同步元数据模型。
- 文件/目录：
  - 未来的本地存储目录（当前仓库未建立）
  - `apps/workspace/src/shared/types/`
- 完成定义：
  - 有 `local_revision`、`remote_revision`、`sync_queue`、`conflicts` 类型。
- 验证方式：
  - 模型与状态机测试。

### Task 2

- 目标：补同步状态页与手动触发入口。
- 文件/目录：
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/settings/components/` 或未来 sync 页面目录
- 完成定义：
  - 用户可见同步状态、重试和冲突处理入口。
- 验证方式：
  - 页面测试与交互测试。

### Task 3

- 目标：补 push/pull/conflict-resolution 服务端或中间层契约。
- 文件/目录：
  - `server/internal/handler/`
  - `server/cmd/server/router.go`
- 完成定义：
  - 有明确的同步 API 与错误处理。
- 验证方式：
  - 集成测试覆盖断线、冲突、重复同步。

### Task 4

- 目标：回写模块文档并确认阶段切换。
- 文件：
  - `docs/superpowers/specs/feature-audit/05-data-sync/`
- 完成定义：
  - overview 不再把 5.4 标成“非当前阶段目标”。
- 验证方式：
  - 文档回写完成。

## 执行顺序说明

- 当前阶段不得启动这些任务；只有阶段切换后才允许开始。

## 回写要求

- 启动前先回写 `overview.md`。
- 任何同步模型调整都要同步回写 `design.md`。
