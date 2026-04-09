# Multica Design System

本文档定义 Multica 的视觉语言和交互规范。所有 UI 开发以此为准。

---

## 1. 设计哲学

三条核心原则：

1. **克制即高级。** 默认做减法。每个元素必须有存在的理由——多余的分割线、装饰性图标、"以防万一"的提示文字，都是噪音。留白本身就是设计。
2. **层次靠灰度，颜色是信号。** 界面的主体是中性色。颜色只在需要传递语义时出现（状态、品牌、错误）。如果两个区域在视觉上竞争注意力，解法是让一个退后，而不是两个都加色。
3. **一致性大于个性。** 同类交互必须有相同的视觉反馈。一个 hover 效果在 sidebar、dropdown、table row 里应该"感觉一样"。这种一致性通过 token 而非硬编码实现。

---

## 2. 颜色体系

基于 OKLCh 色彩空间，通过 CSS 变量定义。所有颜色使用 shadcn token，**禁止硬编码 Tailwind 色值**（如 `text-gray-500`、`bg-blue-600`）。

### 2.1 中性色阶梯

界面 90% 的面积由中性色构成。灰度等级即信息层级：

| 角色 | Light Token | Dark Token | 用途 |
|------|-------------|------------|------|
| 背景 | `background` | `background` | 页面底色 |
| 卡片/浮层 | `card` / `popover` | `card` / `popover` | 容器表面 |
| 次级表面 | `muted` / `secondary` | `muted` / `secondary` | hover 背景、标签底色 |
| 边框 | `border` | `border` | 分隔线、输入框边框 |
| 输入框边框 | `input` | `input` | 比 border 略重 |
| 主要文字 | `foreground` | `foreground` | 标题、正文 |
| 次要文字 | `muted-foreground` | `muted-foreground` | 描述、元数据、placeholder |
| 最强调文字 | `primary` | `primary` | 按钮文字（反色）、关键标签 |

**规则：** 同一屏幕内，文字颜色最多使用 3 个层级（`foreground` / `muted-foreground` / 某个语义色）。超过 3 级说明层次设计有问题。

### 2.2 语义色

颜色只用于传递含义，不做装饰：

| Token | 含义 | 使用场景 |
|-------|------|----------|
| `brand` | 品牌标识 | Logo、品牌按钮、极少量强调 |
| `destructive` | 危险/错误 | 删除按钮、表单校验错误、危险操作 |
| `success` | 成功 | 状态标签（完成、已解决） |
| `warning` | 警告 | 注意状态、到期提醒 |
| `info` | 信息 | 提示、链接、次要信息标记 |
| `priority` | 优先级 | 高优先级标签 |

**规则：**
- 语义色主要用于小面积元素（badge、icon、border）。大面积着色用该色的 10%-20% 透明度变体（如 `bg-destructive/10`）。
- 每屏同时出现的语义色不宜超过 2-3 种。如果一个界面同时有红黄绿蓝紫，说明信息密度过高，需要重新组织。

### 2.3 暗色模式

暗色模式不是简单的反转。它是独立设计的一套配色：

- 背景使用深灰（`oklch(0.18 ...)`），不是纯黑——纯黑在 LCD 屏上刺眼。
- 边框使用 `oklch(1 0 0 / 10%)`（白色 10% 透明度），比 light 模式更微妙。
- 语义色在 dark 模式下适当提亮（如 `success` 从 `0.55` 提到 `0.65`），保证对比度。
- 所有 UI 变更必须同时在两个模式下验证。

---

## 3. 字体规范

### 3.1 字体家族

| 角色 | 字体 | 用途 |
|------|------|------|
| 正文/UI | Geist Sans (`--font-sans`) | 所有界面文字的默认字体 |
| 代码/数据 | Geist Mono (`--font-mono`) | 代码块、ID、时间戳、等宽数据 |
| 标题 | `--font-heading`（= `--font-sans`） | 页面标题、区块标题 |

### 3.2 字号纪律

**整个项目只使用 3 个核心字号 + 1 个特殊字号：**

| Tailwind Class | 大小 | 角色 | 使用场景 |
|----------------|------|------|----------|
| `text-base` (16px) | 正文 | 页面标题、主要内容 | 页面标题、编辑器正文、空状态说明 |
| `text-sm` (14px) | 默认 | 界面的主力字号 | 菜单项、按钮、表单、列表项、正文 |
| `text-xs` (12px) | 辅助 | 元数据、标签 | badge 文字、时间戳、状态栏、次要信息 |
| `text-[0.8rem]` | 过渡 | 仅限 sm 按钮 | shadcn button size="sm" 专用 |

**禁止：**
- 使用 `text-lg`、`text-xl`、`text-2xl` 等——任务管理工具追求信息密度，不需要大字号。
- 使用任意像素值如 `text-[11px]`、`text-[13px]`——坚持 Tailwind 内置 scale。
- 在同一个区块里混用超过 2 个字号。如果需要第 3 个字号来区分层次，先试试用 `font-medium` vs `font-normal` 或 `text-muted-foreground` 来解决。

### 3.3 字重

只使用两个：

| 字重 | 用途 |
|------|------|
| `font-normal` (400) | 正文、描述、大部分文字 |
| `font-medium` (500) | 标签、按钮、导航项、标题、选中状态 |

**禁止** `font-bold` / `font-semibold`——它们在 Geist 字体下显得突兀，破坏界面的"轻"感。如果需要更强的强调，用更大的字号或 `foreground` 色值，而不是加粗。

---

## 4. 间距体系

基于 Tailwind 的 4px 基础网格。间距传递信息——它不只是"好看"，而是告诉用户"什么属于什么"。

### 4.1 间距语义

| 间距 | Tailwind | 含义 |
|------|----------|------|
| 4px | `gap-1` / `p-1` | **紧密关联** — icon 与文字、label 与值 |
| 6px | `gap-1.5` / `p-1.5` | **组件内部** — 按钮内部 padding、列表项间距 |
| 8px | `gap-2` / `p-2` | **同组不同项** — 表单字段间、列表项间 |
| 12px | `gap-3` / `p-3` | **小节内** — 卡片内部 padding |
| 16px | `gap-4` / `p-4` | **组间分隔** — 不同区块之间 |
| 24px | `gap-6` / `p-6` | **大节分隔** — 页面主要区域间 |

**规则：如果需要分割线，说明间距不够。** 优先通过增大间距来分隔内容，而不是加 `<Separator />`。分割线应该是最后手段。

### 4.2 容器策略（按优先级排序）

当需要在视觉上分隔两个区域时：

1. **仅间距** — 增大两个区域的间距（首选）
2. **单条分割线** — 一根细线 `border-border`
3. **背景色变化** — 一个区域用 `bg-muted` 或 `bg-card`
4. **完整卡片** — border + radius + padding（最重手段）

用最轻的工具完成分隔。

---

## 5. 交互状态

这是设计一致性的核心。每种状态必须在所有组件中表现一致。

### 5.1 状态层级概览

```
默认 (rest) → hover → active/pressed → selected/active → focused → disabled
```

### 5.2 Hover 状态

Hover 是"我注意到你了"，视觉变化应该轻微、即时：

| 元素类型 | Hover 效果 | Token |
|----------|-----------|-------|
| 列表项/菜单项 | 背景变浅灰 | `hover:bg-muted` |
| Ghost 按钮 | 背景变浅灰 + 文字变前景色 | `hover:bg-muted hover:text-foreground` |
| 次要按钮 | 背景加深 20% | `hover:bg-secondary/80` |
| 主按钮 | 背景加深 20% | `hover:bg-primary/80` |
| 文字链接 | 下划线出现 | `hover:underline` |
| Tab 标签 | 文字从次要变主要 | `hover:text-foreground`（从 `text-muted-foreground`） |
| 图标按钮 | 背景变浅灰 | `hover:bg-muted` |
| 危险按钮 | 背景透明度加深 | `hover:bg-destructive/20` |

**规则：**
- hover 时不改变尺寸（无 `scale`）、不加阴影（无 `shadow`）。
- hover 的背景色永远比 selected/active 更淡。这样用户能区分"悬停"和"已选中"。
- 所有 hover 使用 `transition-colors` 或 `transition-all`，时长由 Tailwind 默认值（150ms）处理，不需要自定义。

### 5.3 Active / Selected 状态

Active 是"我已经被选中了"，视觉比 hover 更重：

| 元素类型 | Active 效果 | Token |
|----------|------------|-------|
| Sidebar 菜单项 | 背景 + 文字加重 + font-medium | `data-active:bg-sidebar-accent data-active:font-medium` |
| Tab | 下方指示条 + 文字变前景色 + font-medium | `data-[state=active]:text-foreground` |
| 列表选中行 | 背景加深 | `bg-muted` 或 `bg-accent` |
| Toggle（开） | 背景反色 | `data-[state=on]:bg-primary data-[state=on]:text-primary-foreground` |

**关键区分：** Hover = `bg-muted`，Active = `bg-muted` + `font-medium` + `text-foreground`。Active 始终比 hover 多一个视觉维度（字重或颜色变化），而不仅仅是背景更深。

### 5.3.1 Active 不被 Hover 覆盖

这是最容易出 bug 的地方：用户 hover 到一个已选中的项目上，hover 样式覆盖了 active 样式，导致选中态"闪回"普通 hover 态，视觉上像取消了选中。

**原则：Active 状态在任何时候都必须保持可辨识——包括被 hover 时。**

实现方式：

**方式一：Active 使用 hover 不涉及的维度**

如果 hover 只改背景，那 active 用字重 + 文字颜色来区分。即使 hover 背景叠上去，字重和颜色不变，用户仍能识别"这个是选中的"：

```
// ✅ hover 只管背景，active 靠字重和颜色
hover:bg-muted                          // hover：浅灰背景
data-active:font-medium data-active:text-foreground  // active：字重+颜色（hover 不会覆盖）
```

**方式二：Active + Hover 组合样式**

当 active 也用了背景色时，需要显式定义 "active 且 hover" 的复合状态，确保 hover 不会把 active 的背景拉回低层级：

```tsx
// ✅ 显式处理 active+hover 复合态
cn(
  "hover:bg-muted/50",                              // 普通 hover
  "data-active:bg-muted data-active:text-foreground", // active
  "data-active:hover:bg-muted"                       // active+hover：保持 active 背景，不降级
)
```

```tsx
// ❌ 反例：hover 覆盖 active
cn(
  "hover:bg-muted/50",           // hover 背景比 active 更淡
  "data-active:bg-muted",        // active 背景
  // 没有处理复合态 → hover 到 active 项时背景从 muted 闪回 muted/50
)
```

**方式三：CSS 选择器优先级**

利用 `:not()` 让 hover 只作用于非 active 的元素：

```
// ✅ hover 不作用于 active 项
[data-active]:bg-muted [data-active]:text-foreground
not-data-active:hover:bg-muted/50
```

**检查方法：** 写完任何带 hover + active 状态的组件后，必须手动验证——先点击选中一项，然后鼠标移到该项上再移开，确认视觉不会"闪烁"或"降级"。

### 5.4 Pressed 状态

物理反馈感——按下按钮时有微小的位移：

```
active:not-aria-[haspopup]:translate-y-px
```

这个 1px 的下移在 shadcn button 上已全局配置。对于触发弹出菜单的按钮不添加（因为弹出即松开，位移会闪烁）。

### 5.5 Focus 状态

Focus 为键盘导航服务。所有可交互元素统一使用：

```
focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50
```

- 使用 `focus-visible`（非 `focus`），避免鼠标点击时出现 focus ring。
- ring 颜色使用 `ring` token（中灰），不跟组件颜色走——保持全局一致。

### 5.6 Disabled 状态

```
disabled:pointer-events-none disabled:opacity-50
```

简单统一。不需要为每个组件定制 disabled 样式。

### 5.7 Error / Invalid 状态

```
aria-invalid:border-destructive aria-invalid:ring-destructive/20
```

- 使用 `aria-invalid` 属性触发，与表单校验库自然对接。
- 只改变边框和 ring，不改背景。错误信息用内联文字展示，不用 toast 或 alert banner。

---

## 6. 图标规范

### 6.1 图标库

统一使用 **Lucide React**（`lucide-react`）。

禁止混用其他图标库（Heroicons、Phosphor 等），也禁止自制 SVG 图标（除非 Lucide 确实没有合适的）。

### 6.2 图标尺寸

图标尺寸与组件尺寸绑定：

| 组件尺寸 | 图标尺寸 | 示例 |
|----------|---------|------|
| xs（h-6） | `size-3` (12px) | 紧凑按钮、badge 内图标 |
| sm（h-7） | `size-3.5` (14px) | 小按钮、紧凑列表 |
| default（h-8） | `size-4` (16px) | 标准按钮、菜单项、表格操作 |
| lg（h-9） | `size-4` (16px) | 大按钮（图标不需要更大） |

**规则：**
- 独立装饰性图标（如空状态插图）最大 `size-8` (32px)。
- 所有图标默认继承父元素文字颜色。需要弱化时用 `text-muted-foreground`。
- 图标与文字的间距：`gap-1`（xs）/ `gap-1.5`（sm/default）/ `gap-2`（宽松排列）。

### 6.3 图标颜色

- **导航/操作图标：** `text-muted-foreground`，hover 时跟随文字变为 `text-foreground`
- **状态图标：** 使用对应语义色（如 `text-success`、`text-destructive`）
- **Active 状态图标：** `text-foreground`

---

## 7. 圆角规范

基于 `--radius: 0.625rem`（10px）的动态 scale：

| Token | 值 | 用途 |
|-------|-----|------|
| `rounded-sm` | 6px | Checkbox、小标签 |
| `rounded-md` | 8px | 输入框、小按钮、dropdown item |
| `rounded-lg` | 10px | 标准按钮、卡片、dialog |
| `rounded-xl` | 14px | 大卡片、sheet |
| `rounded-full` | 999px | 头像、pill badge |

**禁止** 硬编码像素值如 `rounded-[6px]`（除非 shadcn 组件内部需要响应式计算如 `rounded-[min(var(--radius-md),12px)]`）。

---

## 8. 动效规范

### 8.1 原则

- **快速、克制。** 动效是为了帮助用户理解变化，不是展示技术。
- **淡入淡出优先。** 元素出现/消失优先用 opacity 过渡，而不是滑动。
- **无弹跳。** 不使用 spring / bounce 缓动。缓动曲线统一用 `ease-out`。

### 8.2 时长

| 场景 | 时长 | 示例 |
|------|------|------|
| 颜色/透明度变化 | 150ms | hover 背景变化、文字颜色变化 |
| 展开/收起 | 200ms | accordion、collapsible |
| 弹层出入 | 150-200ms | dialog、dropdown、popover |
| 页面切换 | 无动效 | 路由跳转无过渡动画 |

### 8.3 使用的 transition

| Tailwind Class | 用途 |
|----------------|------|
| `transition-colors` | 纯颜色变化（hover、active）— 首选 |
| `transition-all` | 多属性同时变化 |
| `transition-opacity` | 元素淡入淡出 |
| `transition-transform` | 位移动画（pressed 效果） |

---

## 9. 组件使用规范

### 9.1 shadcn 优先

所有 UI 组件优先使用已安装的 shadcn 组件（55 个可用）。新增 UI 需求时：

1. 先查 shadcn 是否有对应组件 → `npx shadcn add <component>`
2. 需要变体 → 用 CVA 在现有组件上扩展
3. 确实没有 → 自建组件，但必须遵循本规范的 token / 交互状态

### 9.2 按钮层级

从最强调到最弱：

| 变体 | 视觉重量 | 使用场景 |
|------|---------|----------|
| `default`（primary） | ██████ | 页面主操作（每屏最多 1 个） |
| `outline` | ████░░ | 次要操作 |
| `secondary` | ███░░░ | 辅助操作、工具栏 |
| `ghost` | █░░░░░ | 图标按钮、内联操作、紧凑工具栏 |
| `destructive` | ████░░ | 删除、危险操作（红色调） |
| `link` | █░░░░░ | 内联文字链接 |

**规则：** 一个视图里的 primary 按钮最多 1 个。其他都用更弱的变体。如果有多个同等重要的操作，全部用 `outline` 或 `secondary`。

### 9.3 Dropdown / Popover

- 内容宽度使用 `w-auto`，**禁止** 固定宽度如 `w-52`、`w-56`（会导致文字换行）。
- 菜单项统一 `text-sm`，图标 `size-4`。
- 选中项通过 checkmark 图标或左侧指示条标记，不改变背景色。
- 危险操作项使用 `text-destructive`，放在最底部，上方用分割线隔开。

### 9.4 表单输入

- 输入框统一使用 `border-input` 边框，focus 时 `border-ring` + ring。
- Label 使用 `text-sm font-medium`。
- 描述/帮助文字使用 `text-xs text-muted-foreground`。
- 错误信息使用 `text-xs text-destructive`，放在输入框正下方。

---

## 10. 反模式清单

以下做法**禁止**出现在代码中：

| 禁止 | 原因 | 替代 |
|------|------|------|
| 硬编码颜色 `text-red-500`、`bg-gray-100` | 破坏主题一致性 | 使用 token：`text-destructive`、`bg-muted` |
| 任意像素 `text-[11px]`、`w-[137px]` | 脱离设计系统 | 使用 Tailwind 内置 scale |
| `font-bold` / `font-semibold` | 过重，破坏轻感 | `font-medium` + `text-foreground` |
| `text-lg` / `text-xl` / `text-2xl` | 信息密度型工具不需要大字 | `text-base` 已是最大 |
| `shadow-sm` / `shadow-md` / `shadow-lg` | 拟物风格，与扁平设计冲突 | 使用 `border` 分隔层级 |
| hover 时 `scale-105` | 突兀，与克制风格冲突 | `hover:bg-muted` |
| 多色 gradient 背景 | 装饰性，分散注意力 | 纯色 token |
| Skeleton loading | 与简洁风格不匹配 | Spinner（`Loader2Icon animate-spin`）或内联 loading 文字 |
| Toast 做操作确认 | 转瞬即逝，用户容易错过 | 内联状态文字或 Sonner 仅用于错误/重要提示 |
| 固定宽度 dropdown `w-52` | 文字换行不可控 | `w-auto` |
| 纯黑背景 `#000` / `oklch(0 0 0)` | LCD 上刺眼 | Dark 模式用深灰 `background` token |

---

## 11. 检查清单

在提交任何 UI 变更前，过一遍：

- [ ] 所有颜色是否使用 token？有没有硬编码？
- [ ] 字号是否只在 `text-xs` / `text-sm` / `text-base` 范围内？
- [ ] 字重是否只用了 `font-normal` 和 `font-medium`？
- [ ] Hover 状态是否比 active 状态更淡？
- [ ] Active 项被 hover 时，active 样式是否仍然可辨识（不被 hover 覆盖）？
- [ ] 图标尺寸是否与组件尺寸匹配？
- [ ] 间距是否使用 Tailwind 内置 scale（无任意值）？
- [ ] Dark 模式下是否正常？
- [ ] 有没有不必要的分割线（可以用间距替代）？
- [ ] Dropdown / Popover 是否 `w-auto`？
- [ ] 一个视图里 primary 按钮是否不超过 1 个？
