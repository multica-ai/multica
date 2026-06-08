# Data Management Spec

## 背景

Multica 已经有 Settings → Data 入口、workspace export、dry-run import 和 apply import。但 manifest 当前主要覆盖 issue，仍不能作为下一里程碑稳定的数据管理基础。`docs/superpowers` 中的数据导出、导入、备份设计应迁移为 OpenSpec，并以现有 DataTab / DataSyncService 为基线收敛。

## 范围

本能力定义：

- canonical manifest v2。
- workspace export 覆盖 issues、projects、labels、time entries、pomodoro settings/sessions、agents/runtimes metadata 的安全子集。
- import dry-run 和 apply 共用校验器。
- DataTab 支持 manifest summary、entity counts、error list。
- backup 仅作为 manifest export/import 的应用方式，不做离线多端同步。

不覆盖 WebDAV、本地文件同步、CRDT、离线冲突解决、附件二进制备份。

## 当前状态

- 状态：部分完成。
- 已完成：DataTab、export JSON、import dry-run、apply import、canonical manifest 雏形。
- 缺失：多实体 manifest、引用关系校验、版本迁移策略、导入冲突策略、备份语义。

## 证据

- `apps/workspace/src/features/settings/components/settings-page.tsx` `SettingsPage`：Settings 中已有 `DataTab` 入口。
- `apps/workspace/src/features/settings/components/data-tab.tsx` `DataTab`：已支持 export、dry-run、apply import。
- `server/internal/service/data_sync.go` `WorkspaceExportManifest`：已有 canonical JSON manifest 结构。
- `server/internal/service/data_sync.go` `ManifestData`：当前只包含 `Issues []ManifestIssue`。
- `server/internal/service/data_sync.go` `BuildExportManifest`：导出逻辑只查询 issues。
- `server/internal/service/data_sync.go` `ApplyImport`：当前 import apply 只创建 issues。

## 缺口

1. 覆盖缺口：manifest 只覆盖 issues，不能代表 workspace 数据。
2. 兼容缺口：缺少 manifest version migration 策略。
3. 引用缺口：project/label/time entry 等实体引用未建模。
4. 冲突缺口：import apply 缺少 upsert/skip/rename 等明确策略。
5. 备份缺口：当前 export/import 不能被称为完整 backup。

## 交接说明

执行 Agent 必须先扩 manifest 契约，再更新后端 export/import，最后更新 DataTab。禁止先做 UI 文案宣称“完整备份”。
