# 单能力 Design

## 目标

- 定义统一 workspace 导出契约，并明确 JSON/CSV/Excel/PDF 的角色分工，为 5.2 导入和 5.3 备份提供同一事实源。

## 非目标

- 不在本能力内定义多端同步。
- 不把每种格式都做成独立数据源。
- 不把单页打印输出误当成正式导出契约。

## 当前架构基线

- 当前入口：  
  - `SettingsPage` 没有数据管理入口。  
  - `routeTree` 没有导出路由。
- 当前核心逻辑：  
  - `Issue`、`TimeEntry`、`PomodoroSettings` 已经定义了主要实体。  
  - `authHeaders` 与 `RequireWorkspaceMember` 说明所有读取都受 workspace 边界约束。
- 当前 UI 或接口：  
  - 代码搜索未命中任何导出实现。

### 代码证据

- `apps/workspace/src/features/settings/components/settings-page.tsx` `SettingsPage`：无导出入口。
- `apps/workspace/src/router.tsx` `routeTree`：无导出页面。
- `apps/workspace/src/shared/types/issue.ts` `Issue`：任务实体可直接纳入导出。
- `apps/workspace/src/shared/types/time-entry.ts` `TimeEntry`：时间记录实体可直接纳入导出。
- `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` `PomodoroSettings`：配置类数据也在范围内。

## 缺口定义

- 缺统一导出入口。
- 缺统一 manifest。
- 缺多格式转换规则，若直接实现会导致 5.2/5.3 无法复用。

## 方案与权衡

### 方案 A：每种格式单独实现

- 做法：JSON、CSV、Excel、PDF 各自从不同接口或页面生成。
- 优点：短期可以先做单一格式。
- 风险：格式之间字段漂移，后续导入和备份无法复用。

### 方案 B：统一 canonical JSON，再派生其他格式

- 做法：先定义 workspace export manifest，以 JSON 为唯一主格式；CSV/Excel/PDF 从同一快照派生。
- 优点：与 5.2、5.3 共用契约，版本管理简单，验证一次即可。
- 风险：首轮需要先补 manifest 设计和导出流水线。

## 推荐方案

- 推荐方案 B。
- JSON 是 canonical 格式；CSV/Excel/PDF 都是面向阅读或表格流转的派生投影。
- 推荐入口：新增数据管理页（或 settings 下数据管理标签），统一承接导出、导入、备份，而不是散落在 issue/time-tracking 页面。

## 数据模型或状态模型

- `WorkspaceExportManifest`
  - `schema_version`
  - `workspace`: `{ id, slug, exported_at, source_app_version }`
  - `data`: `{ issues, time_entries, pomodoro_settings, metadata }`
  - `filters`: `{ range?: { since, until }, included_entities[] }`
- 派生格式
  - `CSV`: 表格友好视图，只覆盖结构化列表数据
  - `Excel`: 多 sheet 投影，来源仍是 manifest
  - `PDF`: 面向阅读的摘要导出，不作为可回灌格式

## 接口契约

### 输入

- `format`: `json | csv | xlsx | pdf`
- `scope`: 默认当前 workspace
- `range`: 可选，仅对时间型数据和统计投影生效

### 输出

- `json`: 完整 manifest
- `csv/xlsx`: 从 manifest 投影出来的结构化表
- `pdf`: 从 manifest 摘要渲染出的阅读材料
- 错误场景
  - 非法 format
  - 非法 range
  - 读取任一实体失败

## UI 或交互流程

1. 用户进入数据管理页。
2. 先选导出范围与格式。
3. 系统先生成 canonical manifest。
4. 若格式不是 JSON，则从 manifest 派生最终文件。

### 页面交互流

```text
[数据管理页]
     |
     v
[选择导出格式/范围]
     |
     v
[生成 canonical manifest]
     |
     +--> [直接下载 JSON]
     |
     +--> [派生 CSV/XLSX/PDF]
```

### 状态机

```text
[idle]
  |
  v
[validating]
  |
  +--> [error]
  |
  +--> [exporting]
           |
           +--> [success]
           |
           +--> [error]
```

### 数据变化流

```text
[Issue / TimeEntry / PomodoroSettings]
              |
              v
[WorkspaceExportManifest builder]
              |
              v
[JSON / CSV / XLSX / PDF]
```

## 权限、边界条件、异常路径

- 谁可以使用  
  - 当前 workspace 成员，在现有鉴权下读取自己的 workspace 数据。
- 哪些输入非法  
  - 非法 format、非法 range、包含不支持的实体类型。
- 失败时如何处理  
  - 任一实体导出失败都视为整次导出失败，并返回错误摘要。

## 实现约束

- 5.1/5.2/5.3 必须共享同一个 `schema_version`。
- JSON 是唯一 canonical 数据源；CSV/Excel/PDF 不是回灌主格式。
- 导出入口必须集中，不能在每个业务页各自新增按钮形成多套契约。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 各格式各自定义字段 | 后续兼容性失控 | 先锁 manifest，再派生格式 |
| PDF 想承载完整恢复数据 | 格式不适合回灌 | 明确 PDF 只做阅读摘要 |
| 范围导出与全量导出混用 | 备份/导入契约混乱 | 在 manifest 中显式记录 `filters` |

## 验收检查

1. 存在统一导出入口。
2. JSON manifest 可稳定表示 workspace 快照。
3. CSV/Excel/PDF 都能说明自己是从同一 manifest 派生，而不是另起数据源。
