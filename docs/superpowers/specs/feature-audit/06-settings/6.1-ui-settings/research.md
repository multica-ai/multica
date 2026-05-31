# 单能力 Research

## 调研目标

1. 确认界面设置当前已经落地了哪些能力。
2. 确认这些能力属于 workspace 级、用户级还是设备级。
3. 确认语言、字体、布局、侧边栏、列表密度为何仍处于缺失状态。

## 现状链路

1. 入口：`apps/workspace/src/router.tsx` `settingsRoute` 把 `/settings` 指向 `SettingsPage`。
2. 页面分发：`apps/workspace/src/features/settings/components/settings-page.tsx` `accountTabs` 把界面设置挂在 `appearance` 页签下。
3. 核心逻辑：`apps/workspace/src/features/settings/components/general-tab.tsx` `AppearanceTab` 只通过 `useTheme` 渲染 light / dark / system 三个主题选项。
4. 状态更新：`next-themes` 负责主题状态；`apps/workspace/src/components/ui/sidebar.tsx` `setWidth` 负责侧边栏宽度本地持久化；两者并未汇成统一偏好模型。
5. 输出结果：用户能切换主题、被动记住侧边栏宽度，但不能管理语言、字体大小、布局模式、侧边栏显隐或任务列表密度。

## 关键证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/router.tsx` | `settingsRoute` | 界面设置当前只存在于 `/settings` 路径下，没有独立 appearance 路由。 |
| `apps/workspace/src/features/settings/components/settings-page.tsx` | `accountTabs` | 设置页把 `appearance` 归在账号侧，说明 6.1 主要是用户级偏好而不是 workspace 共享配置。 |
| `apps/workspace/src/features/settings/components/general-tab.tsx` | `AppearanceTab` | 当前页面只渲染主题选择区块，没有其他界面偏好控件。 |
| `apps/workspace/src/features/settings/components/general-tab.tsx` | `themeOptions` | 已实现能力仅包含 `light`、`dark`、`system` 三种主题模式。 |
| `apps/workspace/src/components/ui/sidebar.tsx` | `setWidth` | 侧边栏宽度已有 `localStorage` 持久化，但没有设置页入口和统一偏好键。 |
| `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` | `STORAGE_KEY` | 仓库已有设备本地偏好模式，可作为界面偏好存储方式的先例。 |

## 空搜索证据

| 路径 | 符号 / 搜索关键词 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/settings` | `rg(语言切换|字体大小|任务列表密度|侧边栏显示|布局调整)` | 未找到匹配，说明 6.1 清单中的缺失项没有设置页实现。 |
| `apps/workspace/src/features/settings` | `rg(custom theme|font size|density|sidebar toggle|layout settings)` | 未找到匹配，说明英文命名路径下也没有对应实现残片。 |

## 数据模型或状态流

- `apps/workspace/src/features/settings/components/general-tab.tsx` `AppearanceTab`：主题状态来自 `useTheme`，属于用户当前设备的展示偏好。
- `apps/workspace/src/components/ui/sidebar.tsx` `width`：侧边栏宽度直接写入 `localStorage`，说明同样是设备级 UI 状态。
- 目前缺少统一的 `appearance preferences` 结构，因此字体大小、密度、布局等目标项没有可挂载的数据入口。

## 边界条件

- 证据：`apps/workspace/src/features/settings/components/workspace-tab.tsx` `WorkspaceTab`；结论：workspace 设置已受成员角色控制，因此 6.1 不应把个人界面偏好提升为 workspace 公共配置。
- 证据：`apps/workspace/src/components/ui/sidebar.tsx` `setWidth`；结论：侧边栏体验已证明“同一用户不同设备可不一致”是可接受的。
- 证据：`apps/workspace/src/features/settings/components/general-tab.tsx` `AppearanceTab`；结论：当前没有 i18n 基础设施接入设置页，因此语言切换不能当成简单下拉框补完。

## 未决问题

1. `布局调整` 是否指 issue 视图模式、导航布局，还是更大范围的页面编排；当前仓库没有统一定义。
2. `侧边栏显示/隐藏` 是否需要跨设备同步；现有侧边栏宽度模式偏向本地设备，不宜由执行 Agent 自行扩 scope。
3. 语言切换是否进入当前阶段；仓库没有 i18n setting 证据，需先确认是否接受只保留英文界面。
