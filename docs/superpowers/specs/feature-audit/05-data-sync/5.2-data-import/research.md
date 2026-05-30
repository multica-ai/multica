# 单能力 Research

## 调研目标

- 确认当前“数据导入”已经做到哪一步。
- 确认现有 issue 批量导入能否作为统一导入管线的基础。
- 确认为什么导入必须和导出/备份共享同一契约。

## 现状链路

1. 入口  
   - 证据：`apps/workspace/src/features/issues/components/issues-header.tsx` `BulkImportButton`；结论：当前导入入口挂在 issue 页面。
   - 证据：`apps/workspace/src/features/issues/components/bulk-import-modal.tsx` `BulkImportModal`；结论：前端已有批量导入模态框。
2. 数据流  
   - 证据：`apps/workspace/src/features/issues/components/bulk-import-modal.tsx` `parseCsv` / `handleImport`；结论：当前只支持 issue 文本或 CSV 解析后提交。
   - 证据：`apps/workspace/src/shared/api/client.ts` `bulkCreateIssues`；结论：客户端调用的是 issue 专属批量创建接口。
   - 证据：`server/internal/handler/issue.go` `BulkCreateIssues`；结论：服务端只处理 issue 的批量创建请求。
3. 输出结果  
   - 证据：`apps/workspace/src/features/issues/components/bulk-import-modal.tsx` `importResults`；结论：当前已有逐条结果反馈，但范围只限 issue。

## 关键代码证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/issues/components/issues-header.tsx` | `BulkImportButton` | 当前只有 issue 页面能触发导入。 |
| `apps/workspace/src/features/issues/components/bulk-import-modal.tsx` | `BulkImportModal` / `parseCsv` / `handleImport` | 当前导入只面向 issue 文本/CSV。 |
| `apps/workspace/src/shared/api/client.ts` | `bulkCreateIssues` | 前端导入请求没有统一 import API。 |
| `server/internal/handler/issue.go` | `BulkCreateIssues` | 后端只支持 issue 批量创建。 |
| 代码搜索 `apps/`、`server/` | `rg(导入.*json|json backup|workspace import|restore data)` | 未找到匹配，说明还没有统一 JSON 导入或恢复链路。 |

## 数据模型或状态流

- 当前导入模型  
  - 证据：`apps/workspace/src/shared/types/api.ts` `BulkCreateIssuesRequest`；结论：现有导入模型只包含 `issues[]`。
- 当前验证模型  
  - 证据：`BulkImportModal` `validationErrors`；结论：前端已具备“先校验、再提交、再反馈结果”的交互雏形。
- 当前读取边界  
  - 证据：`authHeaders`；结论：导入仍受 workspace header 约束。

## 边界条件

- 权限边界  
  - 当前导入只在当前 workspace 内创建 issue。
- 空状态  
  - 空输入与非法 CSV 已有提示，但没有统一导入首页空状态。
- 错误路径  
  - 当前失败只返回 issue 批量创建结果，没有 manifest 级错误摘要。

## 未决问题

- 统一导入是否先支持 canonical JSON，再逐步接第三方/CSV 适配器；该项在 `design.md` 中给出推荐方案。
- dry-run 是仅前端预校验还是服务端正式校验；该项在 `design.md` 中固定为服务端 dry-run。
