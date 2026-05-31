# 模块级设计总览

## 目标与范围

- 本轮覆盖 `06-settings` 下的四个二级能力：`6.1 界面设置`、`6.2 快捷键设置`、`6.3 通知设置`、`6.4 高级设置`。
- 本轮只补设计包与模块总览，不进入代码实现，不扩展到 `dashboard.md` 或其他模块。
- 本轮特别强调三类设置边界：workspace 级设置、用户级偏好、通知通道能力。

## 能力列表

| 能力 | 当前状态 | 优先级 | 备注 |
| --- | --- | --- | --- |
| 6.1 界面设置 | 部分实现 | P2 | 已有主题切换与侧边栏局部本地持久化，缺统一用户偏好层 |
| 6.2 快捷键设置 | 缺失 | P2 | 现有只是散落式键盘事件，推荐先做注册表与只读管理面 |
| 6.3 通知设置 | 部分实现 | P1 | 已有 ntfy 通道配置，缺桌面/声音/时长等设备级偏好 |
| 6.4 高级设置 | 缺失 | P3 | 明显混合时间追踪、草稿持久化、保留策略与调试边界，需先拆 ownership |

## 当前状态基线

### 设置分层基线

- 证据：`apps/workspace/src/features/settings/components/settings-page.tsx` `accountTabs`；结论：账号侧设置目前已单列 `appearance`、`notifications`、`pomodoro`，说明用户级偏好已有入口，但尚未统一成可扩展偏好层。
- 证据：`apps/workspace/src/features/settings/components/settings-page.tsx` `workspaceTabs`；结论：workspace 侧设置当前聚焦 `workspace`、`repositories`、`members`、`labels`、`ai`、`automation`，说明共享设置已按工作区容器划分。
- 证据：`apps/workspace/src/features/settings/components/workspace-tab.tsx` `WorkspaceTab`；结论：workspace 设置写入 `name`、`description`、`context` 且受 `owner/admin` 角色约束，说明共享配置不能和个人偏好混存。
- 证据：`apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` `STORAGE_KEY`；结论：番茄时长、自动开始、声音偏好当前走 `localStorage`，仓库已经存在“设备本地偏好”的既有模式。
- 证据：`server/pkg/db/generated/models.go` `NotificationPreference`；结论：通知通道配置当前是用户级服务端模型，只包含 `ntfy_url`、`ntfy_token`、`disabled_types`。

### 6.1 界面设置

- 证据：`apps/workspace/src/features/settings/components/general-tab.tsx` `AppearanceTab`；结论：界面设置当前只有主题切换，没有语言、字体、布局、侧边栏显示和列表密度控制。
- 证据：`apps/workspace/src/components/ui/sidebar.tsx` `setWidth`；结论：侧边栏宽度已有本地持久化实现，但并未接入设置页成为显式偏好项。

### 6.2 快捷键设置

- 证据：`apps/workspace/src/features/layout/components/dashboard-layout.tsx` `handler`；结论：全局搜索通过 `Cmd/Ctrl+K` 直接绑在布局层，没有快捷键注册表或设置模型。
- 证据：`apps/workspace/src/features/editor/title-editor.tsx` `createTitleKeymap`；结论：编辑器 Enter / Escape 是局部键位，不构成可管理的快捷键能力。
- 证据：`apps/workspace/src/features/editor/extensions/submit-shortcut.ts` `createSubmitExtension`；结论：`Mod-Enter` 已存在，但仍然是组件内硬编码。

### 6.3 通知设置

- 证据：`apps/workspace/src/features/settings/components/notifications-tab.tsx` `NotificationsTab`；结论：设置页已经有通知入口，但只围绕 ntfy URL、token 与通知类型开关。
- 证据：`server/internal/handler/notification_preference.go` `UpsertNotificationPreference`；结论：后端当前只保存 ntfy 通道配置，不支持桌面通知、声音或时长字段。
- 证据：`server/cmd/server/notification_listeners.go` `maybeSendNtfy`；结论：运行时真实发送路径只有 ntfy。

### 6.4 高级设置

- 证据：`apps/workspace/src/features/settings/components/settings-page.tsx` `accountTabs`；结论：当前设置页没有 `advanced` 页签或同类入口。
- 证据：`docs/superpowers/specs/feature-audit/02-time-management/2.1-time-tracking/spec.md` `缺口`；结论：idle detection 已在时间追踪模块被识别为独立缺口，不适合直接塞进未分层的高级设置。
- 证据：`apps/workspace/src/features/issues/stores/draft-store.ts` `useIssueDraftStore`；结论：草稿自动保存以固定 `persist` 方式存在，但没有“自动保存间隔”设置位。

## 非目标

- 不把 workspace 级配置、用户级偏好、设备级通知行为合并到一个无边界的大表。
- 不把产品边界未定的“高级设置”先做成杂项收纳页，再让执行 Agent 自行解释各字段归属。
- 不把局部键盘事件、局部 `localStorage` 或现有 ntfy 通道误判成“已完成整类能力”。

## 优先级与推进顺序

1. **先做 6.3 通知设置**：`product-overview.md` 已把“更强提醒能力”列为当前阶段目标，而仓库已有 ntfy 通道与服务端偏好模型，可直接作为扩展基线。
2. **再做 6.1 界面设置**：主题和侧边栏本地偏好已存在，可顺势整理出统一用户偏好层。
3. **然后做 6.2 快捷键设置**：应复用 6.1 建好的用户偏好存储边界，先收敛成注册表，再决定是否开放自定义。
4. **最后处理 6.4 高级设置**：其中多数条目需要先拆归属，当前不应抢在前三项之前实现。

## 共享约束

### workspace 级设置

- 证据：`apps/workspace/src/features/settings/components/workspace-tab.tsx` `canManageWorkspace`；结论：workspace 级设置必须受成员角色约束，不能退化成任意成员可写的个人偏好。
- 证据：`product-overview.md` `4.5.3 Workspace 承接哪些能力`；结论：workspace 是协作上下文容器，成员关系、工作对象与 AI 配置都在这个边界内。

### 用户级偏好

- 证据：`apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` `loadSettings`；结论：设备相关体验偏好目前默认走浏览器本地存储。
- 证据：`apps/workspace/src/components/ui/sidebar.tsx` `setWidth`；结论：纯个人 UI 偏好可先走本地设备，不应强行同步成 workspace 共享配置。

### 通知通道能力

- 证据：`server/pkg/db/generated/models.go` `NotificationPreference`；结论：通道配置是用户级、服务端可漫游的数据。
- 证据：`product-overview.md` `当前阶段的项目目标与展望`；结论：通知增强目标是“邮件、ntfy 或多通道组合”，不是单一浏览器开关。

## 风险与依赖

| 风险或依赖 | 影响 | 处理方式 |
| --- | --- | --- |
| 用户偏好与 workspace 配置混存 | 会导致权限和同步边界混乱 | 先在各 design.md 明确 owner：workspace / user / device |
| 快捷键缺少中心注册表 | 会让设置页和真实行为继续漂移 | 6.2 先收口现有硬编码键位，再谈自定义 |
| 通知把设备能力误做成服务端字段 | 会把浏览器权限与多设备差异错误同步 | 6.3 推荐拆成“服务端通道配置 + 本地投递偏好” |
| 高级设置承接过多产品未决策项 | 容易把低优先级边界问题提前实现 | 6.4 明确 P3，先文档化 ownership 再决定是否进入代码 |

## 回写规则

- 实现任一能力后，先更新对应能力目录下的 `spec.md` 交接说明与状态，再回写本模块 `overview.md`。
- 若能力状态发生变化，只更新 `06-settings/overview.md` 与对应能力文档，不触碰 `dashboard.md`。
- 若实现阶段发现设置归属与本轮 design 不一致，必须先更新对应 `design.md` / `tasks.md`，再继续实现。
