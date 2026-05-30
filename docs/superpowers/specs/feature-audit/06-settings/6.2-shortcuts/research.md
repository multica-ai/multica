# 单能力 Research

## 调研目标

1. 确认仓库中是否已有可管理的快捷键设置能力。
2. 确认现有键盘交互残片分布在哪些模块。
3. 判断 6.2 应先做“注册表”还是直接做“自定义快捷键”。

## 现状链路

1. 入口：`apps/workspace/src/features/settings/components/settings-page.tsx` `accountTabs` 当前没有 shortcuts 页签。
2. 键位残片：全局搜索、标题编辑、评论提交、日历上下文菜单等位置各自绑定键盘事件。
3. 状态更新：这些键位直接在组件或扩展内部执行动作，没有统一状态模型。
4. 输出结果：用户确实能触发少量快捷键，但无法查看、修改或冲突检测。

## 关键证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/settings/components/settings-page.tsx` | `accountTabs` | 设置页没有 shortcuts 入口。 |
| `apps/workspace/src/features/layout/components/dashboard-layout.tsx` | `handler` | 全局搜索通过 `Cmd/Ctrl+K` 硬编码在布局层。 |
| `apps/workspace/src/features/editor/title-editor.tsx` | `createTitleKeymap` | 标题编辑器使用 Enter / Escape 局部快捷键，没有对外暴露管理接口。 |
| `apps/workspace/src/features/editor/extensions/submit-shortcut.ts` | `createSubmitExtension` | `Mod-Enter` 作为提交快捷键存在，但仅属于编辑器扩展内部。 |
| `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx` | `handleKey` | 日历上下文菜单通过 Escape 关闭，说明更多键位依旧是散落式实现。 |

## 空搜索证据

| 路径 | 符号 / 搜索关键词 | 结论 |
| --- | --- | --- |
| `apps/workspace/src` | `rg(hotkey|keybind|shortcut settings|custom shortcut|global shortcut)` | 未找到匹配，说明仓库没有中心化快捷键设置实现。 |
| `server` | `rg(hotkey|keybind|shortcut settings|custom shortcut|global shortcut)` | 未找到匹配，说明后端也没有对应的用户偏好或冲突校验接口。 |

## 数据模型或状态流

- 当前没有快捷键模型。
- 现有状态流是“组件监听 keydown -> 直接执行动作”，没有注册表、作用域、默认键位与用户覆盖层。
- 这意味着 6.2 若直接做自定义快捷键，会先撞上缺少中心注册表的问题。

## 边界条件

- 证据：`apps/workspace/src/features/layout/components/dashboard-layout.tsx` `handler`；结论：全局级键位已经存在，但只在浏览器内生效，不是系统级全局快捷键。
- 证据：`apps/workspace/src/features/editor/title-editor.tsx` `createTitleKeymap`；结论：一部分键位属于局部输入组件，应保留局部作用域，不应全部提升为全局快捷键。
- 证据：`apps/workspace/src/features/settings/components/settings-page.tsx` `accountTabs`；结论：6.2 更接近用户级偏好，而不是 workspace 配置。

## 未决问题

1. `全局快捷键` 是否仅指 Web App 内全局，还是要扩展到操作系统级快捷键；当前仓库没有 daemon / desktop 接口证据支持后者。
2. 自定义快捷键是否进入当前阶段；仓库没有注册表与冲突检测能力，不应由执行 Agent 自行假设。
3. 任务操作与视图切换快捷键的第一批范围要不要限于只读展示；推荐先确认这一点。
