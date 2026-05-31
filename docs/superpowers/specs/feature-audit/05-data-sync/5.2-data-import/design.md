# 单能力 Design

## 目标

- 把现有 issue 批量导入收敛成统一导入管线的一种适配器，并补 canonical JSON 导入与服务端 dry-run。

## 非目标

- 不把 issue 导入继续当作唯一导入入口。
- 不在本能力里引入多端同步。
- 不让前端单独决定导入是否合法。

## 当前架构基线

- 当前入口：  
  - `BulkImportButton` 位于 issue 头部。  
  - `SettingsPage` 没有统一导入入口。
- 当前核心逻辑：  
  - `BulkImportModal` 解析 CSV / 文本并调用 `bulkCreateIssues`。  
  - `BulkCreateIssues` 在服务端批量创建 issue。
- 当前存储或状态：  
  - `BulkCreateIssuesRequest` 只有 `issues[]`，没有 manifest、dry-run 或 source metadata。

### 代码证据

- `apps/workspace/src/features/issues/components/bulk-import-modal.tsx` `BulkImportModal`：已有解析与预校验交互。
- `apps/workspace/src/shared/api/client.ts` `bulkCreateIssues`：现有接口只面向 issue。
- `server/internal/handler/issue.go` `BulkCreateIssues`：服务端只做 issue 批量创建。
- `apps/workspace/src/shared/types/api.ts` `BulkCreateIssuesRequest`：请求模型无法扩展为统一导入。

## 缺口定义

- 导入入口散落在 issue 页面。
- 导入契约不能承载非 issue 数据。
- 没有服务端 dry-run，无法为 JSON/备份恢复共用校验结果。

## 方案与权衡

### 方案 A：继续扩 bulk issue import

- 做法：在 `BulkImportModal` 里逐步加入更多字段和格式。
- 优点：短期最快。
- 风险：issue 页面会成为“导入中心”，而且无法自然承接时间记录、设置、备份恢复。

### 方案 B：统一导入管线 + 适配器

- 做法：新增统一导入页与 import API；canonical JSON 是主格式，issue CSV/第三方格式作为 adapter，全部先走服务端 dry-run。
- 优点：可直接复用到 5.3 备份恢复，也能保留现有 issue 导入交互经验。
- 风险：需要额外设计导入结果、错误摘要与适配器注册方式。

## 推荐方案

- 推荐方案 B。
- 推荐顺序：  
  1. 支持 canonical JSON re-import；  
  2. 把现有 issue CSV 导入迁到统一 import pipeline；  
  3. 再接更多第三方工具适配器。  
- dry-run 必须在服务端执行，返回 `summary + errors + warnings + apply_plan`，前端只负责呈现。

## 数据模型或状态模型

- `WorkspaceImportPayload`
  - `schema_version`
  - `source_type`: `canonical-json | issue-csv | external-adapter`
  - `payload`
- `WorkspaceImportDryRunResult`
  - `summary`
  - `warnings[]`
  - `errors[]`
  - `apply_plan`
- `WorkspaceImportApplyResult`
  - `created`
  - `updated`
  - `skipped`
  - `failed`

## 接口契约

### 输入

- `source_type`
- `payload`
- `mode`: `dry-run | apply`

### 输出

- dry-run：返回结构化校验结果
- apply：返回逐实体结果摘要
- 错误场景
  - schema version 不兼容
  - 必填字段缺失
  - workspace 不匹配

## UI 或交互流程

1. 用户进入统一导入页并选择来源类型。
2. 上传 canonical JSON 或 CSV 文件。
3. 系统先做服务端 dry-run，展示摘要和错误。
4. 用户确认后才执行 apply。

### 页面交互流

```text
[数据管理页 / 导入]
        |
        v
[选择来源: JSON / CSV / 其他适配器]
        |
        v
[上传文件]
        |
        v
[服务端 dry-run]
   |           |
   |           +--> [展示错误/警告]
   |
   +-----------> [用户确认 apply]
```

### 状态机

```text
[idle]
  |
  v
[uploading]
  |
  v
[dry-run]
  | \
  |  \-> [error]
  |
  +--> [reviewing]
            |
            v
         [applying]
            |
            +--> [success]
            |
            +--> [error]
```

### 数据变化流

```text
[JSON / CSV / 外部适配器输入]
              |
              v
[Import parser + validator]
              |
              v
[dry-run result / apply result]
              |
              v
[导入结果页]
```

## 权限、边界条件、异常路径

- 谁可以使用  
  - 当前 workspace 成员，在当前 workspace 内导入。
- 哪些输入非法  
  - schema version 不兼容、文件内容损坏、实体引用缺失、workspace 不匹配。
- 失败时如何处理  
  - dry-run 失败不进入 apply；apply 部分失败也要返回结构化结果。

## 实现约束

- 5.1/5.2/5.3 必须共享 `schema_version` 与 manifest 字段。
- 现有 issue CSV 导入必须改造成 adapter，不能继续作为“总入口”。
- dry-run 校验必须在服务端，不允许仅靠前端 parse 结果决定可导入。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 继续在 issue 页堆导入逻辑 | 其他实体无法复用 | 抽离统一导入管线 |
| 前端校验与后端校验不一致 | 用户体验混乱 | 固定服务端 dry-run 为唯一准入门 |
| JSON 与备份恢复分离 | 恢复无法复用 | 让备份恢复直接复用同一 import pipeline |

## 验收检查

1. 导入入口不再只存在于 issue 页面。
2. canonical JSON 与 issue CSV 都能经过同一 dry-run / apply 管线。
3. 导入结果能输出 summary、warnings、errors、apply result。
