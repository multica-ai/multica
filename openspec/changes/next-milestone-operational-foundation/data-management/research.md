# Data Management Research

## 调研目标

确认现有数据导入导出能力的入口、manifest 格式、后端服务和缺口，确定下一里程碑能安全扩展的边界。

## 现状链路

当前链路：

1. 用户在 Settings → Data 点击 export。
2. `DataTab` 调用 `api.exportWorkspaceData()`。
3. `DataSyncService.BuildExportManifest` 读取 workspace 和 issues，生成 manifest。
4. 用户粘贴 manifest 后，`DataTab` 构造 import payload。
5. dry-run 调用 `DryRunImport`，apply 调用 `ApplyImport`。
6. `ApplyImport` 当前只创建 issue。

## 关键代码证据

| 文件 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/settings/components/settings-page.tsx` | `SettingsPage` | Settings 已挂载 DataTab。 |
| `apps/workspace/src/features/settings/components/data-tab.tsx` | `DataTab` | UI 已有 export、dry-run、apply import。 |
| `apps/workspace/src/features/settings/components/data-tab.tsx` | `buildImportPayload` | 前端兼容 canonical manifest 和 import payload 两种形状。 |
| `server/internal/service/data_sync.go` | `WorkspaceExportManifest` | 后端已有 canonical manifest 类型。 |
| `server/internal/service/data_sync.go` | `ManifestData` | manifest data 当前只包含 issues。 |
| `server/internal/service/data_sync.go` | `BuildExportManifest` | 导出只调用 `ListIssues`。 |
| `server/internal/service/data_sync.go` | `DryRunImport` | dry-run 只校验 issue payload。 |
| `server/internal/service/data_sync.go` | `ApplyImport` | apply 只创建 issue。 |

## 数据模型或状态流

当前 manifest：

- `schema_version`
- `workspace`
- `data.issues`

下一版需要保留 versioned manifest，并扩展：

- projects
- labels
- issue_label_links
- time_entries
- time_entry_labels
- pomodoro settings/sessions
- agents metadata safe subset
- workspace settings safe subset

## 边界条件

- workspace_id mismatch 必须继续作为硬错误。
- import dry-run 必须覆盖所有 entity validation。
- apply import 必须明确冲突策略。
- 附件二进制不进入第一版 backup。
- secret、token、daemon credential 不进入 export。

## 未决问题

1. 现有 import 是否允许导入到不同 workspace，还是继续强制 workspace_id match？
2. 冲突策略默认 skip、rename，还是 update existing？
3. agents/runtimes 是否只导出 display metadata，不导出 execution credentials？
