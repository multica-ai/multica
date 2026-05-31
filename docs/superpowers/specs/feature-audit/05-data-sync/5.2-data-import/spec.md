# 单能力 Spec

## 背景

- 证据：`docs/功能列表清单.md` `5.2 数据导入`；结论：导入不仅要支持 CSV，还要支持 JSON、备份文件和其他工具数据。
- 证据：`apps/workspace/src/features/issues/components/bulk-import-modal.tsx` `BulkImportModal` 与 `server/internal/handler/issue.go` `BulkCreateIssues`；结论：当前已有局部导入实践，但范围只覆盖 issue，因此需要统一设计包来收敛。

## 范围

- 本次覆盖：统一导入入口、canonical JSON 导入、第三方/CSV 适配器、dry-run 契约。
- 本次不覆盖：多端同步、备份调度、跨 workspace 合并策略。

## 当前状态

- 证据：`apps/workspace/src/features/issues/components/issues-header.tsx` `BulkImportButton`；结论：导入入口只挂在 issue 页面。
- 证据：`apps/workspace/src/features/issues/components/bulk-import-modal.tsx` `parseCsv`；结论：当前已支持 issue CSV/文本导入。
- 证据：代码搜索 `apps/`、`server/`，关键词 `导入.*json|json backup|workspace import|restore data`；结论：未找到匹配，说明统一 JSON 导入和备份恢复还不存在。

## 证据

- `BulkImportModal`：已有导入 UI、校验和结果面板。
- `bulkCreateIssues`：现有导入接口只面向 issue。
- `BulkCreateIssuesRequest`：当前请求模型只有 `issues[]`。

## 缺口

1. 没有统一导入入口。
2. 没有 canonical JSON 导入。
3. 没有备份恢复导入。
4. 现有 issue 导入无法复用到其他实体。

## 交接说明

- 实现前必须先读 `design.md` 中的统一导入管线方案。
- issue 导入只能作为适配器之一，不能继续扩成全局标准。
- 5.2 与 5.1 / 5.3 必须共享 manifest 版本、校验和 dry-run 摘要结构。
