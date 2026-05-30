# 模块级设计总览

## 目标与范围

- 本轮补齐 `05-data-sync/` 下 5.1、5.2、5.3、5.4 的设计包，并固定“导入/导出/备份共用数据契约”的模块级约束。
- 本轮包含导出、导入、备份、多端同步的现状证据、推荐方案、优先级与阶段判断。
- 本轮不包含代码实现、不扩到其他模块、不把 websocket 内部刷新误记为真正多端同步。

## 能力列表

| 能力 | 当前状态 | 优先级 | 阶段判断 | 备注 |
| --- | --- | --- | --- | --- |
| 5.1 数据导出 | 缺失 | P1 | 当前阶段目标 | 先定义统一导出契约 |
| 5.2 数据导入 | 部分完成 | P1 | 当前阶段目标 | 当前仅有 issue 批量导入，需并入统一契约 |
| 5.3 数据备份 | 缺失 | P3 | 低优先级 | 依赖统一契约与数据管理入口，建议在导入/导出稳定后再做 |
| 5.4 多端同步 | 缺失 | P4 | 非当前阶段目标 | 当前 websocket 只是内部刷新，不是离线/冲突/文件层同步 |

## 当前状态证据

- 证据：`apps/workspace/src/features/settings/components/settings-page.tsx` `SettingsPage`；结论：设置页只有 `general/notifications/appearance/ai` 等标签，没有数据管理、导入导出、备份或同步标签。
- 证据：`apps/workspace/src/router.tsx` `routeTree`；结论：仓库当前没有 `data-export`、`data-import`、`backup`、`sync` 专属路由。
- 证据：`apps/workspace/src/features/issues/components/bulk-import-modal.tsx` `BulkImportModal`；结论：当前唯一接近数据导入的入口是 issue CSV/批量文本导入。
- 证据：`server/internal/handler/issue.go` `BulkCreateIssues`；结论：后端现有导入能力也只覆盖 issue 批量创建，不是统一导入框架。
- 证据：`apps/workspace/src/features/realtime/provider.tsx` `RealtimeProvider`；结论：当前有 websocket 实时事件订阅。
- 证据：`apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts` `useTimeTrackingSync`；结论：websocket 目前只做 query invalidation 与局部刷新，不提供离线队列、冲突解决或文件同步。
- 证据：代码搜索 `apps/`、`server/`，关键词 `export.*json|json.*export|导出.*JSON`、`workspace backup|backup file|恢复数据|restore data|导入.*json|json backup`、`webdav|WebDAV|local file sync|本地文件同步|手动触发同步|冲突解决|同步状态显示`；结论：未找到匹配，说明统一导出、备份、真正多端同步都未实现。

## 非目标

- 不把单一 issue 批量导入夸大成“统一数据导入模块”。
- 不把 websocket 实时刷新夸大成“多端同步已完成”。
- 不在当前阶段直接承诺 WebDAV、本地文件同步、冲突合并 UI。

## 优先级与推进顺序

1. 先做 5.1 数据导出：证据：当前所有能力都缺统一数据契约；结论：导出契约是导入与备份的公共基础。
2. 再做 5.2 数据导入：证据：`BulkImportModal` 与 `BulkCreateIssues` 说明已有局部导入体验；结论：最适合在统一契约下先收敛已有导入入口。
3. 然后做 5.3 数据备份：证据：设置页与路由都没有数据管理入口；结论：备份适合在契约和入口稳定后补齐，优先级低于导入导出主链路。
4. 5.4 多端同步维持非当前阶段目标：证据：当前只有 websocket invalidation，没有离线缓存或冲突解决实现；结论：真正多端同步需要新的产品阶段与架构前提。

## 共享约束

### 5.1 / 5.2 / 5.3 统一数据契约

- 统一事实源：导出、导入、备份都以同一个 workspace 级 canonical JSON manifest 为准，不能三套格式各自演化。
- 统一版本字段：契约必须包含 `schema_version`、`workspace_id`、`exported_at`、`source_app_version`。
- 统一实体集合：至少覆盖 `issues`、`time_entries`、`pomodoro/session-related settings` 与必要引用关系。
- 统一校验流程：导入与备份恢复必须共享 manifest 校验、dry-run、结果摘要与错误列表。
- 统一范围约束：数据导出允许做“窗口化导出”，但备份恢复默认以全量 workspace 快照为单位，不做半恢复。
- 统一兼容策略：CSV 只能作为派生投影视图，不能成为主备份格式。

### 模块级实施约束

- 数据管理入口必须是独立设置页或独立数据管理页，不能散落在 issue/time-tracking 各自页面。
- 当前架构以服务端为单一事实源；任何同步设计都必须先承认这一点。
- 如果未来进入多端同步阶段，必须先补本地状态、冲突模型和同步状态可视化，再谈外部存储连接器。

## 风险与依赖

| 风险或依赖 | 影响 | 处理方式 |
| --- | --- | --- |
| 导出/导入/备份各自定义格式 | 后续兼容性失控 | 先锁统一 canonical manifest |
| 继续沿用 issue 专属导入实现 | 其他实体无法复用 | 把 issue 导入改为“统一导入管线的一个适配器” |
| 把 websocket 刷新误判成同步 | 产品阶段判断失真 | 5.4 文档中明确二者不是一回事 |
| 备份先于契约落地 | 恢复链路无法验证 | 5.3 明确依赖 5.1/5.2 完成后再实现 |

## 回写规则

- 5.1/5.2/5.3 实现后都要回写同一个 canonical manifest 版本与字段变更，保持三个能力文档一致。
- 5.4 若未来被正式立项，必须先回写本 overview 的“阶段判断”，把 websocket 刷新与真正同步继续区分开。
- 如果后续新增数据管理入口，要回写本 overview 的“当前状态证据”和“推进顺序”。
