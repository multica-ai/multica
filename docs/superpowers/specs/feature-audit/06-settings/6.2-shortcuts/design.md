# 单能力 Design

## 目标

- 为 6.2 建立统一的快捷键注册表与设置入口。
- 把现有散落式键位收口成可见、可验证、可逐步扩展的用户偏好能力。

## 非目标

- 不做操作系统级全局快捷键。
- 不在第一阶段直接开放任意自定义组合键。
- 不重写所有输入组件的键位系统。

## 当前架构基线

- 当前入口：无。
- 当前核心逻辑：`dashboard-layout.tsx`、`title-editor.tsx`、`submit-shortcut.ts`、`MyTimeCalendarPage.tsx` 各自监听键盘事件。
- 当前存储或状态：无统一模型。
- 当前 UI 或接口：设置页没有 shortcuts 页签，也没有后端 API。

### 代码证据

- `apps/workspace/src/features/layout/components/dashboard-layout.tsx` `handler`：已有全局搜索快捷键残片。
- `apps/workspace/src/features/editor/title-editor.tsx` `createTitleKeymap`：已有局部编辑键位残片。
- `apps/workspace/src/features/editor/extensions/submit-shortcut.ts` `createSubmitExtension`：已有提交流程残片。

## 缺口定义

- 6.2 当前缺的是“快捷键能力层”，而不只是“少一个设置页”。
- 如果没有注册表、作用域和默认值，自定义快捷键会立刻撞上冲突和回写问题。

## 方案与权衡

### 方案 A：继续保留散落式键位，只补文档说明

- 做法：保持现有硬编码，只在设置页展示静态文本。
- 优点：改动最少。
- 风险：文档和真实行为持续漂移，无法支持冲突检测与后续扩展。

### 方案 B：先做注册表 + 只读管理面，推荐

- 做法：新增中心化快捷键注册表，统一声明 `id / scope / default binding / description / action`；设置页先展示默认绑定和启停状态，用户覆盖留作下一阶段。
- 优点：能收敛现有残片，又不会一次性闯进复杂的自定义编辑器问题。
- 风险：需要逐步把现有键位接进注册表。

### 方案 C：直接做完整自定义快捷键系统

- 做法：首版就支持录制组合键、冲突检测、重置默认值。
- 优点：功能完整。
- 风险：当前没有注册表、用户偏好 schema、作用域建模与测试基础，复杂度过高。

## 推荐方案

选择方案 B。

6.2 的正确起点不是“先让用户录快捷键”，而是“先让系统知道自己有哪些快捷键”。只有中心注册表存在后，后续才可能在 6.1 的用户偏好层上叠加覆盖值和重置动作。

## 数据模型或状态模型

```text
shortcutRegistry
├─ id
├─ scope(global | editor | page)
├─ defaultBinding
├─ description
└─ action

shortcutUserOverrides
└─ 未来阶段再引入
```

- 当前阶段只需要 `registry + enabled state`。
- 用户覆盖值先不开放，以免在没有冲突检测时制造更多未决问题。

## 接口契约

### 输入

- 设置页读取注册表，按作用域展示已有快捷键。
- 用户可查看绑定、查看说明、按类别过滤。

### 输出

- 统一注册表成为快捷键唯一事实源。
- 第一阶段不要求服务端 API；如需后续用户覆盖，可延用 6.1 的用户偏好层。

## UI 或交互流程

### 页面交互流

```text
/settings
  -> Shortcuts
     -> 读取 shortcutRegistry
     -> 按 global / editor / page 分组展示
     -> 点击某项查看说明与作用域
     -> 后续阶段才允许编辑绑定
```

### 状态机

```text
[registry-loading]
   -> [registry-ready]
   -> [viewing]
   -> [future-editing-disabled]
```

### 数据变化流

```text
shortcutRegistry.ts
   -> useShortcutRegistry()
   -> Settings / runtime listeners
   -> keydown dispatcher
   -> action callback
```

## 权限、边界条件、异常路径

- 谁可以使用：所有登录用户都能查看快捷键目录。
- 哪些输入非法：当前阶段不接受用户自定义输入，因此不存在录制非法键位。
- 失败时如何处理：若某个 action 未注册，设置页展示“未接线”，执行层不得静默吞掉冲突。

## 实现约束

- 不要把局部输入框的所有 Enter/Escape 都包装成全局快捷键。
- 不要承诺操作系统级快捷键；当前仓库只有浏览器级事件证据。
- 必须先有注册表，再讨论自定义绑定。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 现有键位接线分散 | 注册表和真实行为可能不一致 | 先只纳入已有高频键位：搜索、提交、关闭、视图切换 |
| 过早开放自定义 | 冲突检测与作用域混乱 | 第一阶段只读展示，不开放编辑 |
| 把局部键位误当全局能力 | 影响输入体验 | 注册表必须显式标 `scope` |

## 验收检查

1. 设置页存在快捷键分组视图，能列出现有全局与局部键位。
2. `Cmd/Ctrl+K`、`Mod-Enter`、Escape 等已有键位都能在注册表中找到对应声明。
3. 第一阶段不出现“可编辑但不会生效”的伪自定义入口。
4. 文档明确操作系统级全局快捷键不在当前范围。
