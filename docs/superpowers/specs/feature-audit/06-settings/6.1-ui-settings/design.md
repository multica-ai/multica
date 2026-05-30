# 单能力 Design

## 目标

- 为 6.1 建立统一的“用户界面偏好”设计边界，把主题、侧边栏、密度、布局等个人展示偏好收口到同一条路径。
- 明确哪些偏好可以先走设备本地存储，哪些需要等待更上层产品决策。

## 非目标

- 不做自定义主题编辑器。
- 不把个人界面偏好升级成 workspace 共享策略。
- 不在本轮引入完整 i18n 基础设施。

## 当前架构基线

- 当前入口：`apps/workspace/src/features/settings/components/settings-page.tsx` `accountTabs`。
- 当前核心逻辑：`apps/workspace/src/features/settings/components/general-tab.tsx` `AppearanceTab` 仅操作 `useTheme`。
- 当前存储或状态：`apps/workspace/src/components/ui/sidebar.tsx` `setWidth` 与 `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` `STORAGE_KEY` 都说明设备本地存储已是既有模式。
- 当前 UI 或接口：没有 appearance API，也没有统一偏好 schema。

### 代码证据

- `apps/workspace/src/features/settings/components/general-tab.tsx` `AppearanceTab`：说明当前入口仅覆盖主题。
- `apps/workspace/src/components/ui/sidebar.tsx` `setWidth`：说明已有局部 UI 偏好本地持久化。
- `apps/workspace/src/features/settings/components/workspace-tab.tsx` `WorkspaceTab`：说明 workspace 级配置已单独建模，不应与 6.1 混写。

## 缺口定义

- 当前 6.1 缺的不是单个控件，而是“用户界面偏好层”本身。
- 主题、侧边栏、密度、布局的存储策略还未统一，执行时若不先定义边界，很容易把个人偏好错误落到 workspace 配置里。

## 方案与权衡

### 方案 A：继续逐项补散落式 localStorage

- 做法：每个新选项各自直接写 `localStorage`。
- 优点：开发快。
- 风险：键名、回写、重置逻辑持续分裂，后续无法形成高级设置和快捷键的统一偏好层。

### 方案 B：建立统一的界面偏好模型，推荐

- 做法：新增单一 `appearance preferences` 模型，主题、侧边栏显隐/宽度、列表密度、局部布局模式都从这里读写；仍以设备本地持久化为默认。
- 优点：与现有主题/番茄设置的本地偏好模式兼容，也为 6.2 与 6.4 预留统一重置入口。
- 风险：需要先梳理哪些字段属于个人视图偏好，不能把 workspace 共享行为塞进来。

### 方案 C：直接做服务端同步的用户偏好

- 做法：为全部 appearance 设置建立用户级后端表。
- 优点：跨设备可同步。
- 风险：当前仓库没有现成 user preference 表；语言、侧边栏、布局这类字段的跨设备一致性也未被产品确认，过早服务端化会放大未决问题。

## 推荐方案

选择方案 B。

原因是当前仓库已经有两类清晰证据：主题切换是个人显示偏好，侧边栏宽度和番茄设置已接受设备本地存储。与其继续新增散落键值，不如先建立统一的本地偏好层。只有当未来出现明确的跨设备同步需求，再把其中一部分字段升级到服务端。

## 数据模型或状态模型

建议的用户界面偏好模型：

```text
appearancePreferences
├─ theme: light | dark | system
├─ sidebar_visible: boolean
├─ sidebar_width: number
├─ issue_list_density: comfortable | compact
├─ layout_mode: default | focus
└─ font_scale: default | large
```

- `language` 暂不进入实现字段，先保留为未决项。
- `sidebar_width` 继续本地设备化，不跨 workspace 共享。
- `layout_mode` 仅覆盖用户个人视图，不改变 workspace 结构。

## 接口契约

### 输入

- 用户在 `/settings -> appearance` 中修改主题、侧边栏显隐、密度、布局或字号。
- 所有字段先走客户端 schema 校验，非法值回退默认值。

### 输出

- 写入统一的本地偏好存储。
- 页面立即应用新值，并在重载后恢复。
- 出错场景：本地存储不可写时退回内存值并提示“仅当前会话生效”。

## UI 或交互流程

### 页面交互流

```text
/settings
  -> Appearance
     -> 读取 appearancePreferences
     -> 用户修改主题/侧边栏/密度/布局
     -> 立即预览
     -> 保存到本地偏好存储
     -> 其他页面按统一 hook 读取并生效
```

### 状态机

```text
[loading]
   -> [ready]
   -> [editing]
   -> [persisting]
   -> [ready]
   -> [storage-error] -> [ready]
```

### 数据变化流

```text
AppearanceTab
   -> useAppearancePreferences()
      -> localStorage("multica_appearance_preferences")
      -> AppShell / Sidebar / Issue views
      -> UI re-render
```

## 权限、边界条件、异常路径

- 谁可以使用：所有登录用户都可修改自己的界面偏好。
- 哪些输入非法：字号、密度、布局模式超出枚举值时一律回退默认值。
- 失败时如何处理：`localStorage` 写失败不阻断 UI，应保留当前会话态并给出 toast。

## 实现约束

- 不要把 6.1 写进 `WorkspaceTab` 或 workspace API。
- 不要把语言切换当作“顺手加个 select”直接落地；没有 i18n 设计就保持缺口。
- 必须复用统一偏好 hook，禁止继续在单个组件里偷偷新增无命名约束的 `localStorage` 键。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 偏好字段过早服务端化 | 扩大未决的跨设备同步问题 | 本轮推荐本地统一偏好层 |
| 视图模式和 workspace 结构混淆 | 可能破坏共享上下文一致性 | `layout_mode` 只作用于个人视图，禁止修改 workspace 级信息 |
| 继续散落写 `localStorage` | 后续难以重置与回写 | 建立单一 schema 与存储键 |

## 验收检查

1. 用户可在 Appearance 页面统一管理主题、侧边栏显隐、列表密度、布局模式与字号。
2. 刷新页面后，界面偏好能够恢复。
3. workspace 设置页不出现任何 6.1 字段。
4. 语言切换仍保留为未决项，不被执行 Agent 擅自补实现。
