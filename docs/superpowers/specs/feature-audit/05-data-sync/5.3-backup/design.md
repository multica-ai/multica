# 单能力 Design

## 目标

- 为未来的手动备份与恢复定义统一文件契约，并明确 5.3 在当前阶段是低优先级、依赖 5.1/5.2 的能力。

## 非目标

- 不在当前阶段承诺自动备份调度。
- 不在当前阶段承诺附件/文件资产一起打包。
- 不脱离 canonical manifest 自定义备份格式。

## 当前架构基线

- 当前入口：  
  - `SettingsPage` 和 `routeTree` 都没有备份入口。
- 当前核心逻辑：  
  - 仓库里没有备份/恢复 handler。  
  - 现有最接近的能力只有 issue 局部导入。
- 当前存储或状态：  
  - 未来备份应复用 `Issue`、`TimeEntry`、`PomodoroSettings` 等实体。

### 代码证据

- `SettingsPage`：没有备份页。
- `routeTree`：没有备份路由。
- `BulkImportModal` / `BulkCreateIssues`：说明统一导入入口都还没建好。
- 搜索 `workspace backup|backup file|恢复数据|restore data|导入.*json|json backup`：未找到匹配。

## 缺口定义

- 功能空白：没有手动备份、恢复、最近备份记录。
- 契约空白：没有备份文件格式与恢复元数据。
- 阶段依赖：统一导入导出契约尚未稳定。

## 方案与权衡

### 方案 A：先做独立备份格式

- 做法：专门定义 `backup.zip` 或同类格式，独立于导出/导入实现。
- 优点：看似能快速交付“备份”按钮。
- 风险：恢复链路会和导入导出分叉，后续兼容与维护成本最高。

### 方案 B：备份是 canonical manifest 的封装

- 做法：以 5.1 的 manifest 为数据主体，额外附加 `backup_metadata`、校验值、恢复日志需求。
- 优点：与 5.2 恢复链路天然复用，版本兼容可控。
- 风险：必须等待 5.1/5.2 契约先稳定。

## 推荐方案

- 推荐方案 B。
- 当前阶段优先级：低优先级。  
  - 证据：当前没有数据管理入口，也没有统一 import/export 契约；结论：先做备份只会放大重复建设。  
- 当前阶段目标仅保留“设计冻结”，不进入实现。

## 数据模型或状态模型

- `WorkspaceBackupBundle`
  - `manifest`: 直接复用 `WorkspaceExportManifest`
  - `backup_metadata`: `{ backup_id, created_at, created_by, checksum }`
  - `restore_policy`: `{ mode: full-restore-only }`
- 状态变化
  - 手动创建备份时生成 bundle。
  - 恢复前先走与 5.2 共用的 dry-run。
  - 恢复完成后记录 restore summary。

## 接口契约

### 输入

- `action`: `create-backup | dry-run-restore | apply-restore`
- `payload`: manifest 或 backup bundle

### 输出

- create-backup：返回 bundle
- dry-run-restore：返回结构化校验结果
- apply-restore：返回恢复摘要
- 错误场景
  - manifest 版本不兼容
  - checksum 不匹配
  - workspace 不匹配

## UI 或交互流程

1. 用户进入数据管理页的“备份”区。
2. 选择“创建备份”或“恢复备份”。
3. 创建备份时直接生成 bundle。
4. 恢复备份时先 dry-run，再确认 apply。

### 页面交互流

```text
[数据管理页 / 备份]
      |                 |
      |                 +--> [恢复备份]
      |                           |
      |                           v
      |                      [dry-run restore]
      |                           |
      |                           v
      +--> [创建备份]         [确认 apply]
                |                    |
                v                    v
          [生成 backup bundle]   [恢复结果]
```

### 状态机

```text
[idle]
  |
  +--> [creating-backup] --> [success]
  |
  +--> [dry-run-restore] --> [reviewing] --> [restoring] --> [success]
                                  \
                                   -> [error]
```

### 数据变化流

```text
[WorkspaceExportManifest]
          |
          v
[WorkspaceBackupBundle]
          |
          v
[Import pipeline dry-run/apply]
          |
          v
[恢复结果]
```

## 权限、边界条件、异常路径

- 谁可以使用  
  - 当前 workspace 成员，且只作用于当前 workspace。
- 哪些输入非法  
  - checksum 不匹配、schema version 不兼容、bundle 与当前 workspace 不匹配。
- 失败时如何处理  
  - 恢复失败必须返回结构化摘要，并支持审计追踪。

## 实现约束

- 5.3 依赖 5.1/5.2；执行顺序不可反转。
- 当前阶段不做自动备份调度，不做外部云盘目标。
- 恢复默认只支持全量 workspace 恢复，不做局部恢复。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 在契约未稳定前实现备份 | 恢复会反复破坏兼容性 | 将 5.3 明确设为低优先级 |
| 自动备份提早进入范围 | 范围膨胀 | 当前阶段只保留手动备份/恢复设计 |
| 局部恢复需求提前进入 | 复杂度暴涨 | 当前阶段固定 full-restore-only |

## 验收检查

1. 备份格式直接复用 canonical manifest。
2. 恢复流程明确复用 5.2 的 dry-run / apply。
3. 文档明确 5.3 是低优先级，未被误排到当前阶段主线。
