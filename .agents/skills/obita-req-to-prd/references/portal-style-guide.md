# Obita Portal 前端设计规范

> **用途**：PRD 客户旅程章节中自动生成的页面 Mockup 必须严格遵循本规范，确保 Mockup 与真实 Portal 页面视觉一致。
> 来源：Obita OOMP 前端代码仓库（React + Tailwind CSS + shadcn/ui）。

---

## 一、技术栈与组件库

| 项目 | 说明 |
|------|------|
| **框架** | React 19 + TypeScript 5.8 |
| **CSS 框架** | Tailwind CSS 4（4px 基准间距） |
| **主组件库** | shadcn/ui（new-york style，基于 Radix UI） |
| **辅组件库** | Ant Design 6（仅 Breadcrumb） |
| **图标库** | Lucide React（主要）、Tabler Icons（辅助） |

---

## 二、品牌色系

### 主色系（CSS 变量）

| 用途 | CSS 变量 | 色值 |
|------|---------|------|
| 页面背景 | `--background` | `#FFFFFF` |
| 主文字色 | `--foreground` | `#1a1a1a` |
| 卡片背景 | `--card` | `#FFFFFF` |
| 主色（按钮/强调） | `--primary` | `#2d2d2d`（深灰，实际业务中主色由主题控制，默认为蓝色 `#3B82F6`） |
| 主色上的文字 | `--primary-foreground` | `#fafafa` |
| 次色/浅背景 | `--secondary` / `--muted` | `#f5f5f5` |
| 次文字色 | `--muted-foreground` | `#737373` |
| 边框色 | `--border` | `#e5e5e5` |
| 输入框边框 | `--input` | `#e5e5e5` |
| 危险/错误色 | `--destructive` | `#DC2626` |

### 业务蓝色系（Portal 核心视觉标识）

这是 Portal 最显著的视觉特征——大量使用蓝色作为边框、背景、强调色，区别于 shadcn/ui 默认的灰色系。

| 用途 | 色值 | Mockup 实现方式 |
|------|------|----------------|
| **品牌蓝（按钮/图标高亮）** | `#0072F7` | `background: #0072F7` |
| **输入框/卡片边框** | `#BFDBFE`（blue-200） | `border: 1px solid #BFDBFE` |
| **表格/卡片容器边框** | `#DBEAFE`（blue-100） | `border: 1px solid #DBEAFE` |
| **表格行分隔线** | `#e9f1fc` | `border-bottom: 1px solid #e9f1fc` |
| **表格头部背景** | `#F7FAFF` | `background: linear-gradient(to bottom, transparent, #F7FAFF)` |
| **表格行悬停** | `#EFF6FF`（blue-50） | `background: #EFF6FF` |
| **蓝色链接文字** | `#3B82F6`（blue-500） | `color: #3B82F6` |
| **蓝色标签背景+文字** | `bg-blue-50` + `text-blue-600` | `background: #EFF6FF; color: #2563EB` |
| **loading 覆盖层** | `#E9F1FB` 70% 透明 | `background: rgba(233,241,251,0.7)` |
| **选中态边框** | `#3B82F6`（blue-500） | `border-color: #3B82F6` |

### 状态色

| 状态 | 色值 | 用途 |
|------|------|------|
| 成功/已完成 | `#0072F7`（蓝色） | StepIndicator completed、流程完成 |
| 进行中/警告 | `#F59E0B`（amber） | StepIndicator in_progress |
| 错误/失败 | `#EF4444`（红色） | StepIndicator error、校验失败 |
| 待处理/默认 | `#e5e5e5`（灰色） | StepIndicator pending、禁用状态 |

> **注意**：Obita Portal 使用蓝色（非绿色）表示「已完成/成功」状态，这与常见的绿=成功惯例不同。

---

## 三、排版规范

| 属性 | 值 |
|------|-----|
| **字体族** | `system-ui, Avenir, Helvetica, Arial, sans-serif` |
| **表头字体** | `Inter`（仅表格列头使用） |
| **根行高** | `1.5` |
| **根字重** | `400` |

### 字号层级

| Tailwind 类 | 值 | 用途 |
|------------|-----|------|
| `text-xs` | 12px | Badge、标签、侧边栏收起文字 |
| `text-sm` | 14px | **正文、按钮、表格、标签、描述、分页** |
| `text-base` | 16px | 输入框文字（移动端）、段落 |
| `text-lg` | 18px | Dialog/弹窗标题 |
| `text-xl` | 20px | 页面标题 |

### 字重

| 类 | 值 | 用途 |
|----|-----|------|
| `font-normal` | 400 | 表格内容、表头文字 |
| `font-medium` | 500 | 按钮、标签、侧边栏菜单项、表头、面包屑最后一项 |
| `font-semibold` | 600 | CardTitle、DialogTitle、页面标题 |

---

## 四、间距与圆角

### 间距（4px 基准）

| 尺寸 | 值 | 常见用途 |
|------|-----|---------|
| 2px | `0.5` | 图标间距 |
| 4px | `1` | 小间距 |
| 6px | `1.5` | 面包屑 gap |
| 8px | `2` | 页面 gap（移动端）、Card gap |
| 12px | `3` | 输入框内边距 |
| 16px | `4` | 页面内边距（移动端 `p-4`）、Flex gap |
| 20px | `5` | DataTable 表头/单元格 padding |
| 24px | `6` | 页面内边距（桌面端 `md:p-6`）、弹窗 padding |

### 页面布局间距

```
移动端:  gap-4 p-4
桌面端:  md:gap-6 md:p-6
页面最小宽度: 1100px
```

### 圆角

| 变量/类 | 值 | 用途 |
|---------|-----|------|
| `--radius-sm` | 6px | 按钮（rounded-md）、Badge、侧边栏子项 |
| `--radius-md` | 8px | **输入框（rounded-lg，注意覆盖了默认 rounded-md）**、Select、Dialog、侧边栏菜单项 |
| `--radius-lg` | 10px | Tooltip、TabsList |
| `--radius-xl` | 14px | **卡片（rounded-xl）**、表格容器 |

---

## 五、组件样式

### 5.1 按钮（Button）

| 属性 | 值 |
|------|-----|
| 高度 | 默认 `h-9`（36px）；sm: `h-8`（32px）；lg: `h-10`（40px） |
| 圆角 | `rounded-md`（6px） |
| 内边距 | `px-4 py-2` |
| 字号 | `text-sm`（14px） |
| 字重 | `font-medium`（500） |
| 主按钮 | `bg-primary text-primary-foreground` |
| 次按钮（outline） | `border bg-background hover:bg-accent` |
| 幽灵按钮（ghost） | `hover:bg-accent` |
| 危险按钮（destructive） | `bg-destructive text-white` |
| 焦点环 | `ring-[3px] ring-ring/50` |
| 禁用 | `opacity-50 cursor-not-allowed` |
| Loading态 | 16px Loader2 旋转图标 + 文字 |
| 业务主按钮（特例） | `h-[48px] bg-[#0072F7] text-white font-semibold rounded-[8px] w-[182px]` |

### 5.2 输入框（Input）

| 属性 | 值 |
|------|-----|
| 高度 | `h-9`（36px） |
| 圆角 | **`rounded-lg`（8px）** |
| 边框色 | **`border-blue-200`（#BFDBFE）** |
| 内边距 | `px-3 py-1` |
| 占位符色 | `muted-foreground`（#737373） |
| 焦点 | `border-ring + ring-[3px] ring-ring/50` |
| 校验失败 | `border-destructive + ring-destructive/20` |
| 禁用 | `border-gray-300` |

### 5.3 下拉选择（Select）

| 属性 | 值 |
|------|-----|
| 高度 | `h-9`（36px），sm: `h-8`（32px） |
| 圆角 | `rounded-lg`（8px） |
| 边框色 | `border-blue-200` |
| 下拉箭头 | 24px 灰色 |
| 下拉内容 | `rounded-md shadow-md`，zoom-in 动画 |
| 选项高度 | `h-10`（40px） |

### 5.4 卡片（Card）

| 属性 | 值 |
|------|-----|
| 背景 | `bg-white` |
| 圆角 | **`rounded-xl`（12px）** |
| 边框 | **`border-blue-100`（#DBEAFE）** |
| 阴影 | `shadow-sm` |
| 内边距 | `py-6 px-6`（CardHeader/Content/Footer） |
| 标题 | `font-semibold`（600） |
| 业务覆盖 | 常见 `border-blue-100 bg-white shadow-sm` |

### 5.5 表格（DataTable）

| 属性 | 值 |
|------|-----|
| 容器 | `border border-blue-100 rounded-lg bg-white overflow-auto` |
| 表头背景 | 渐变 `linear-gradient(to bottom, transparent, #F7FAFF)` |
| 表头文字 | `font-[Inter] font-medium text-sm` |
| 表头/单元格 padding | `p-5`（20px） |
| 行分隔线 | `border-[#e9f1fc]` |
| 行悬停 | `bg-[#EFF6FF]` |
| 选中行 | `bg-muted` |
| 正文文字 | `text-sm`（14px） |
| 固定列阴影 | `rgba(121,143,167,0.1)` |

### 5.6 弹窗（Dialog）

| 属性 | 值 |
|------|-----|
| 遮罩 | `bg-black/50` |
| 圆角 | `rounded-lg`（8px） |
| 内边距 | `p-6`（24px） |
| 最大宽度 | `sm:max-w-lg`（512px） |
| 标题 | `text-lg font-semibold` |
| 描述 | `text-sm text-muted-foreground` |
| 动画 | fade + zoom-in-95，200ms |
| 阻止外部关闭 | 默认 `preventOutsideClose=true` |
| 确认按钮 | default variant |
| 取消按钮 | outline variant |

### 5.7 Badge（状态标签）

| 属性 | 值 |
|------|-----|
| 内边距 | `px-2 py-0.5` |
| 圆角 | `rounded-md`（6px） |
| 字号 | `text-xs`（12px） |
| 字重 | `font-medium`（500） |
| 默认 | `bg-primary text-primary-foreground` |
| 次要 | `bg-secondary text-secondary-foreground` |
| 危险 | `bg-destructive text-white` |
| 轮廓 | `border text-foreground` |
| 蓝色标签 | `bg-blue-50 text-blue-600` |

### 5.8 Tabs（标签页）

| 属性 | 值 |
|------|-----|
| 容器 | `bg-muted rounded-lg h-9` |
| Tab 项 | `rounded-md border border-transparent text-sm font-medium` |
| 激活态 | `bg-background shadow-sm` |

### 5.9 Tooltip（工具提示）

| 属性 | 值 |
|------|-----|
| 背景 | `bg-primary text-primary-foreground` |
| 圆角 | `rounded-md`（6px） |
| 内边距 | `px-3 py-1.5` |
| 字号 | `text-xs`（12px） |

---

## 六、布局结构

### 整体布局

```
┌──────────────┬──────────────────────────────────┐
│              │  NavHeader (h-16, border-bottom)   │
│  NavSidebar  ├──────────────────────────────────│
│  (256px)     │  Breadcrumb + 内容区域              │
│              │  (p-4 md:p-6, gap-4 md:gap-6)      │
│              │  min-width: 1100px                  │
└──────────────┴──────────────────────────────────┘
```

### 侧边栏

| 属性 | 值 |
|------|-----|
| 展开宽度 | `256px`（16rem） |
| 收起宽度 | `96px`（6rem），只显示图标 |
| 背景色 | `#fafafa`（近白） |
| 右边框 | `border-gray-200` |
| Logo 区域高度 | `64px` |
| 菜单项 | `px-3 py-5 rounded-lg` |
| 子菜单 | `ml-6 pl-3 rounded-md` |
| 激活态背景 | `rgba(primary, 0.1)` |

### 顶部导航

| 属性 | 值 |
|------|-----|
| 高度 | `64px`（h-16） |
| 底边框 | `border-slate-200` + 微阴影 `shadow-[0_1px_2px_rgba(15,23,42,0.08)]` |
| 左侧 | 折叠按钮 + 面包屑 |
| 右侧 | 语言切换 + 用户头像 |

### 面包屑

- 分隔符：`>`
- 最后一项：`font-medium text-foreground`
- 中间项：`text-muted-foreground`（灰色）

---

## 七、交互规范

### Hover 效果

| 元素 | 效果 |
|------|------|
| 按钮（default） | 背景色透明度 90% |
| 按钮（ghost/outline） | `bg-accent`（#f5f5f5） |
| 表格行 | `bg-[#EFF6FF]`（蓝色浅底） |
| 侧边栏菜单 | `rgba(primary, 0.1)` |
| 链接 | `text-foreground` |

### Loading 状态

| 场景 | 样式 |
|------|------|
| 页面加载 | 覆盖层 `bg-[#E9F1FB]/70`，48px 旋转 Loader2 |
| 按钮加载 | 16px Loader2 `animate-spin` 替换图标位置 |
| 表格加载 | 36px 旋转图标 + 半透明覆盖 |
| 骨架屏 | `bg-accent animate-pulse rounded-md` |

### 禁用状态

| 属性 | 值 |
|------|-----|
| 透明度 | `opacity-50` |
| 光标 | `cursor-not-allowed` |
| 输入框边框 | `border-gray-300`（比正常灰） |

---

## 八、Mockup 生成规范

### 必须遵守的规则

1. **所有涉及 Obita Portal 的交互页面**，Mockup 必须严格遵循以上规范
2. **除非用户提供新的交互风格要求**，否则不得偏离
3. Mockup 中的组件样式（按钮、输入框、卡片、表格、弹窗）必须与 Portal 一致
4. 使用 Portal 的蓝色系作为主视觉标识（而非 shadcn 默认灰色）
5. 卡片使用 `border-blue-100` + `rounded-xl`，输入框使用 `border-blue-200` + `rounded-lg`
6. 状态色：蓝色=完成、amber=进行中、红色=错误、灰色=待处理
7. 按钮默认高度 36px、输入框默认高度 36px、字号 14px
8. 页面内边距遵循移动端 16px / 桌面端 24px 的规范

### Mockup 中应包含的元素

- **面包屑导航**：页面顶部，分隔符 `>`
- **页面标题**：`font-semibold`（600），18px+
- **操作按钮**：右对齐或底部固定（主色按钮 + ghost 按钮）
- **表单**：Label + 必填标记（红色 *） + 输入框 + 校验提示
- **表格**：蓝色边框容器 + 渐变表头 + hover 高亮行
- **卡片**：圆角 12px + 蓝色边框 + 白色背景
- **状态标签**：Badge 组件（不同颜色区分状态）
- **空状态**：居中提示文字 + 引导操作
