# Data Management Design

## 目标

把现有 DataTab 和 DataSyncService 升级为可靠的 workspace data management 基线，提供多实体 canonical manifest、统一 dry-run 校验和可解释的 import apply。

## 非目标

- 不实现 WebDAV、本地文件同步、CRDT 或离线冲突解决。
- 不导出 secrets、tokens、daemon credentials。
- 不导出附件二进制。
- 不宣称完整灾备，第一版只提供 workspace data snapshot。

## 当前架构基线

- Settings 已有 DataTab。
- DataSyncService 已有 WorkspaceExportManifest。
- ManifestData 当前只包含 issues。
- DryRunImport 和 ApplyImport 已存在，但只处理 issue payload。

## 缺口定义

需要补齐：

1. Manifest v2 多实体字段。
2. 引用关系校验。
3. dry-run entity count 和错误列表。
4. apply 冲突策略。
5. DataTab 的 manifest summary 和状态反馈。

## 方案与权衡

### 方案 A：继续 issue-only manifest

优点：无需大改。缺点：无法支撑 workspace snapshot。

### 方案 B：扩 canonical manifest 到核心实体

优点：可复用现有 DataTab 和 service，范围可控。缺点：需要多实体校验。

### 方案 C：完整 backup 包含附件和 secrets

优点：最完整。缺点：安全和存储复杂度过高。

## 推荐方案

采用方案 B。

## 数据模型或状态模型

Manifest v2 建议：

- `schema_version`
- `workspace`
- `data.projects`
- `data.labels`
- `data.issues`
- `data.issue_label_links`
- `data.issue_dependencies`
- `data.time_entries`
- `data.time_entry_labels`
- `data.pomodoro`
- `data.settings`

每个实体使用 portable ID 或 stable local reference，import apply 再映射到目标 workspace DB ID。

## 接口契约

保留并扩展：

- `GET /api/data/export`
- `POST /api/data/import/dry-run`
- `POST /api/data/import/apply`

响应必须包含：

- entity counts。
- warnings。
- errors。
- created/skipped/updated/failed。
- reference mapping summary。

## UI 或交互流程

DataTab：

1. Export：展示导出实体范围和生成时间。
2. Import input：粘贴 JSON 或上传文件。
3. Dry-run result：显示实体 counts、warnings、errors。
4. Apply：只有 dry-run 无 hard error 时可执行。
5. Result：展示 created/skipped/failed 和可复制错误列表。

## 权限、边界条件、异常路径

- 只有 workspace admin/owner 可 apply import。
- 普通成员可 export 的范围需要产品决策；默认建议 admin/owner only。
- workspace mismatch 默认 hard error。
- unsupported schema_version hard error。
- secret-like fields 必须拒绝导出。

## 实现约束

- manifest parser 必须强类型化，不允许任意 JSON 透传。
- import apply 必须先 dry-run 或在 apply 内重复完整校验。
- issue CSV 只能作为 adapter，不能成为 canonical backup 格式。

## 风险与对策

| 风险 | 对策 |
| --- | --- |
| 多实体引用映射出错 | dry-run 展示 reference mapping 和 missing refs |
| 导出敏感信息 | 明确 denylist secrets/tokens/credentials |
| schema 版本演进混乱 | 保留 manifest migrator 或版本分支 |
| 用户误以为是完整备份 | UI 文案使用 data snapshot，不使用 full backup |

## 验收检查

- Export manifest v2 至少包含 projects、labels、issues、time entries。
- Dry-run 能发现 workspace mismatch、unsupported version、missing references。
- Apply import 能创建多实体并保留引用关系。
- DataTab 能展示 entity counts、warnings、errors。
- 不导出 tokens、daemon credentials 或附件二进制。
