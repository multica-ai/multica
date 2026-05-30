# 单能力 Tasks

## 实现目标

把 6.1 收敛为统一的用户界面偏好层，覆盖主题、侧边栏、密度、布局与字号，并保持设备本地存储边界清晰。

## 前置依赖

- 先确认 `布局调整` 与 `字号调整` 仅作用于个人视图，不影响 workspace 共享配置。
- 语言切换保持未决，不纳入当前实现切片。

## 任务切片

### Task 1

- 目标：建立统一界面偏好 schema 与读取 hook。
- 文件：`apps/workspace/src/features/settings/`、`apps/workspace/src/shared/` 下新增 appearance preference hook / schema 文件。
- 改动：替代零散 `localStorage` 键，集中定义默认值、校验与持久化。
- 完成定义：主题、侧边栏、密度、布局、字号都能通过同一 hook 读取。
- 验证方式：补 hook 单测；运行 `pnpm test -- appearance` 或对应 Vitest 文件。

### Task 2

- 目标：扩展 Appearance 页面 UI。
- 文件：`apps/workspace/src/features/settings/components/general-tab.tsx`
- 改动：在现有主题区块之外增加侧边栏显隐、密度、布局与字号控件。
- 完成定义：`AppearanceTab` 不再只显示主题，且所有控件均走统一 hook。
- 验证方式：组件测试覆盖默认值、切换、刷新恢复；手动验证 `/settings`。

### Task 3

- 目标：把侧边栏和个人视图接入统一偏好层。
- 文件：`apps/workspace/src/components/ui/sidebar.tsx`、`apps/workspace/src/features/issues/` 相关视图组件或 store。
- 改动：让侧边栏显隐/宽度、列表密度、个人布局模式都读取统一偏好。
- 完成定义：不再存在同能力的第二套本地键值。
- 验证方式：`pnpm typecheck`；手动验证切换后跨页面生效。

### Task 4

- 目标：回写审计台账。
- 文件：`docs/superpowers/specs/feature-audit/06-settings/6.1-ui-settings/spec.md`、`docs/superpowers/specs/feature-audit/06-settings/overview.md`
- 改动：补实现证据、状态与交接说明。
- 完成定义：文档状态与代码一致。
- 验证方式：人工复核文档中的路径、符号名与实现一致。

## 执行顺序说明

先建统一 schema，再补设置页 UI，随后接线各消费方，最后回写文档。这样可以避免 UI 先行造成多个组件继续各自写状态。

## 回写要求

- 实现后更新 `6.1-ui-settings/spec.md` 的证据与完成度。
- 实现后同步 `06-settings/overview.md` 中 6.1 的优先级备注与当前状态。
- 若实现中发现字段归属需要改成服务端同步，先更新 `design.md` 再改代码。
