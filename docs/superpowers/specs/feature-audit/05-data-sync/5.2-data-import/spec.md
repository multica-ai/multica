# 单能力 Spec

## 背景

- 证据：`docs/功能列表清单.md` `5.2 数据导入`；结论：导入不仅要支持 CSV，还要支持 JSON、备份文件和其他工具数据。
- 证据：`apps/workspace/src/features/issues/components/bulk-import-modal.tsx` `BulkImportModal` 与 `server/internal/handler/issue.go` `BulkCreateIssues`；结论：当前已有局部导入实践，但范围只覆盖 issue，因此需要统一设计包来收敛。

## 范围

- 本次覆盖：统一导入入口、canonical JSON 导入、第三方/CSV 适配器、dry-run 契约。
- 本次不覆盖：多端同步、备份调度、跨 workspace 合并策略。

## 当前状态

- 证据：`apps/workspace/src/features/settings/components/settings-page.tsx` `SettingsPage`、`apps/workspace/src/features/settings/components/data-tab.tsx` `DataTab`；结论：Settings Data 标签已支持粘贴 manifest 执行 dry-run / apply。
- 证据：`server/internal/handler/data_sync.go` `DryRunWorkspaceImport` / `ApplyWorkspaceImport`、`server/cmd/server/router.go` `/api/data/import/*`；结论：统一导入接口已接通。
- 证据：`apps/workspace/src/features/issues/components/bulk-import-modal.tsx` `handleSubmit`；结论：Issue 批量导入已迁移为统一 import pipeline（先 dry-run，再 apply）。

## 证据

- `BulkImportModal`：已有导入 UI、校验和结果面板。
- `bulkCreateIssues`：现有导入接口只面向 issue。
- `BulkCreateIssuesRequest`：当前请求模型只有 `issues[]`。

## 缺口

1. 当前导入实体仅覆盖 issues，备份恢复和其他实体仍未接入。
2. 当前导入输入仍是手工粘贴 JSON，尚未支持文件上传与大文件处理。

## 验证记录

- 证据：`server/internal/service/data_sync_test.go` `TestDryRunImport_RejectsWorkspaceMismatch`、`TestApplyImport_CanonicalJSONRejectsWorkspaceMismatch`、`TestApplyImport_IssueCSVValidationIsAllOrNothing`；结论：服务层 dry-run/apply 契约已覆盖关键校验。
- 证据：`server/internal/handler/handler_test.go` `TestDryRunWorkspaceImportWorkspaceMismatch`、`TestApplyWorkspaceImportIssueCSV`；结论：导入接口已返回结构化结果并可执行写入。
- 证据：`apps/workspace/src/features/settings/components/data-tab.test.tsx`、`apps/workspace/src/features/issues/components/bulk-import-modal.test.tsx`；结论：DataTab 与 BulkImportModal 前端流程已迁到统一导入 API。
- 证据：`e2e/data-sync.spec.ts` `can dry-run and apply canonical import from data tab`、`e2e/bulk-import.spec.ts` `can bulk import issues via plain text`；结论：页面级导入主路径可用。

## 交接说明

- 实现前必须先读 `design.md` 中的统一导入管线方案。
- issue 导入只能作为适配器之一，不能继续扩成全局标准。
- 5.2 与 5.1 / 5.3 必须共享 manifest 版本、校验和 dry-run 摘要结构。
