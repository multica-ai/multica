# Data Management Tasks

## 实现目标

把 workspace data management 从 issue-only manifest 扩展为多实体 canonical manifest v2，并提供可验证 dry-run/apply 流程。

## 前置依赖

- 本 design 包范围已确认。
- 冲突策略已选择。
- 敏感字段 denylist 已确认。

## 任务切片

### Task 1：Manifest v2 类型

- 目标：扩展 canonical manifest 类型和版本。
- 目标文件：
  - `server/internal/service/data_sync.go`
  - `apps/workspace/src/shared/types/`
- 完成定义：
  - ManifestData 包含 projects、labels、issues、time entries 等核心实体。
  - schema_version 更新并保留 unsupported version 错误。
  - secret-like 字段不在类型中暴露。
- 验证方式：
  - Go unit tests。
  - TypeScript typecheck。

### Task 2：Export 多实体

- 目标：后端导出核心 workspace 数据。
- 目标文件：
  - `server/internal/service/data_sync.go`
  - `server/pkg/db/queries/`
- 完成定义：
  - Export 返回多实体 counts。
  - 引用关系可被 manifest 表达。
  - 不导出 tokens、credentials、附件二进制。
- 验证方式：
  - Go tests 覆盖导出字段和敏感字段缺失。

### Task 3：Dry-run 校验

- 目标：扩展 dry-run 到多实体。
- 目标文件：
  - `server/internal/service/data_sync.go`
- 完成定义：
  - 校验 workspace、schema version、required fields、references。
  - 返回 warnings/errors/entity counts。
  - missing reference 是 hard error。
- 验证方式：
  - Go tests 覆盖 mismatch、unsupported version、missing refs。

### Task 4：Apply import

- 目标：按 dry-run 结果导入多实体。
- 目标文件：
  - `server/internal/service/data_sync.go`
  - `server/internal/handler/data_sync.go`
- 完成定义：
  - apply 内重复完整校验。
  - 创建实体并保持引用关系。
  - 返回 created/skipped/failed。
- 验证方式：
  - Go integration-style tests 覆盖多实体导入。

### Task 5：DataTab UI

- 目标：升级 DataTab 的导入导出体验。
- 目标文件：
  - `apps/workspace/src/features/settings/components/data-tab.tsx`
  - `apps/workspace/src/features/settings/components/data-tab.test.tsx`
- 完成定义：
  - export 前展示实体范围。
  - dry-run 后展示 entity counts、warnings、errors。
  - apply 前要求 dry-run 成功。
- 验证方式：
  - Vitest 覆盖 export、dry-run blocked、apply success。

### Task 6：回写与验证

- 目标：同步 OpenSpec 状态并完成回归。
- 目标文件：
  - 本目录 `spec.md`
  - 本目录 `design.md`
  - 本目录 `tasks.md`
- 完成定义：
  - 文档状态与实现一致。
  - 记录验证证据。
- 验证方式：
  - `pnpm typecheck`
  - `pnpm test`
  - `make test`

## 回写要求

如果实现阶段决定支持附件或外部同步，必须新开 OpenSpec change；不能扩大本能力范围。
