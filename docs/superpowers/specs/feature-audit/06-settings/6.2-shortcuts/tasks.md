# 单能力 Tasks

## 实现目标

把现有散落式键位收口成统一快捷键注册表，并在设置页提供只读管理面，为后续自定义快捷键做准备。

## 前置依赖

- 先确认第一阶段只做注册表与只读展示，不开放自定义编辑。
- 确认作用域分类至少包含 `global`、`editor`、`page` 三类。

## 任务切片

### Task 1

- 目标：新增快捷键注册表与 dispatch 基础设施。
- 文件：`apps/workspace/src/features/shortcuts/` 新目录；或 `apps/workspace/src/shared/shortcuts/`。
- 改动：定义快捷键元数据、作用域与统一导出。
- 完成定义：已有键位都能通过注册表声明，不再只存在匿名 `keydown` 回调里。
- 验证方式：注册表单测覆盖唯一 id、合法 scope、默认绑定存在。

### Task 2

- 目标：把现有高频键位接入注册表。
- 文件：`apps/workspace/src/features/layout/components/dashboard-layout.tsx`、`apps/workspace/src/features/editor/title-editor.tsx`、`apps/workspace/src/features/editor/extensions/submit-shortcut.ts`、`apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx`
- 改动：改为从注册表引用描述与 action。
- 完成定义：搜索、提交、关闭等已有快捷键都有注册表来源。
- 验证方式：组件测试或交互测试验证旧行为未回退。

### Task 3

- 目标：新增快捷键设置页签与只读管理面。
- 文件：`apps/workspace/src/features/settings/components/settings-page.tsx`、`apps/workspace/src/features/settings/components/` 下新增 shortcuts tab。
- 改动：把快捷键按分组展示，提供描述、绑定与作用域说明。
- 完成定义：用户能在 `/settings` 查看当前支持的快捷键目录。
- 验证方式：Vitest / RTL 覆盖列表渲染；手动检查 `/settings`。

### Task 4

- 目标：为后续用户覆盖预留偏好层接缝。
- 文件：`apps/workspace/src/features/settings/` 或 `apps/workspace/src/features/shortcuts/`
- 改动：预留 override schema，但保持禁用状态。
- 完成定义：未来可扩展自定义快捷键，但当前 UI 不暴露不可用入口。
- 验证方式：类型检查与代码审阅确认没有悬空入口。

### Task 5

- 目标：回写审计文档。
- 文件：`docs/superpowers/specs/feature-audit/06-settings/6.2-shortcuts/spec.md`、`docs/superpowers/specs/feature-audit/06-settings/overview.md`
- 改动：更新证据、完成度与交接说明。
- 完成定义：文档与实现一致。
- 验证方式：人工复核路径与符号名。

## 执行顺序说明

必须先有注册表，再做接线，最后补设置页。否则设置页会先展示一套与真实行为无关的静态清单。

## 回写要求

- 实现后把 6.2 的“缺失”改为准确状态。
- 若后续决定开放自定义快捷键，必须先更新 `design.md`，再进入实现。
- 回写只限 `06-settings` 模块文档，不扩散到其他模块。
