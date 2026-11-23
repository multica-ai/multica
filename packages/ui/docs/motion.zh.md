# 动效

时长和曲线从 `@multica/ui/lib/motion` 导入。

## 使用规则

动效在变化发生的地方交代变化：选中项去了哪里，什么打开了，什么被移除了，下方内容为什么移动。凡是响应用户操作或实时数据、改变布局的交互，默认都要有动效；只是「能用」的功能还没做完。

| Token | 取值 | 用途 |
| --- | --- | --- |
| `UI_MOTION_DURATION.micro` | 100ms | 退场、图标切换、悬停和按压反馈 |
| `UI_MOTION_DURATION.fast` | 150ms | 进场、增删行、小幅尺寸变化 |
| `UI_MOTION_DURATION.standard` | 200ms | 指示器、展开折叠、容器尺寸变化 |
| `UI_EASE_OUT` / `UI_EASE_OUT_CSS` | `cubic-bezier(0.23, 1, 0.32, 1)` | 进场、移动和尺寸变化 |
| `UI_EASE_IN` | `cubic-bezier(0.4, 0, 1, 1)` | 退场 |

- 界面动效不超过 200ms。退场比进场短。
- 不阻塞输入。动画过程中的点击按终点状态生效，动画从当前位置转向新目标。
- 在 class 中写成 `ease-[cubic-bezier(0.23,1,0.32,1)]`；在 JavaScript 中使用常量。

## 按交互类型的规则

### 浮层

Dialog、AlertDialog、Sheet、Popover、DropdownMenu、ContextMenu、Select、HoverCard、Tooltip。

- 进场：淡入，并朝触发元素从 95% 缩放到 100%（Sheet 从所在边滑入），100–150ms，`UI_EASE_OUT`。退场反向，时长不超过进场。
- 直接使用 `packages/ui/components/ui/` 中的组件。Dialog 的时长和曲线在 `--dialog-*` token 中（见 `dialog.md`）；不要在调用处覆盖浮层动画。
- 退场结束前保持内容挂载。不要用 `{open && …}` 包住弹层内容，也不要在关闭时卸载根组件；Base UI 的 Portal 会在退场后卸载。一次只渲染一个弹窗的宿主，要保留上一个弹窗及其数据，直到 `onOpenChangeComplete(false)`。
- 指针扫过工具栏时立即打开的 Tooltip 跳过动画（`data-[instant]:animate-none`）。

### 展开折叠

区块、树节点、详情展开、「显示更多」、行内提示。

- 高度和透明度一起过渡，150–200ms，`UI_EASE_OUT`。下方内容跟着移动，不跳变。
- 用同样的时长把一个 `ChevronRight` 旋转 90°，不要切换两个图标。
- 使用 `CollapsibleContent`。不在 Collapsible 中的条件内容，用 `grid-rows-[0fr]` → `grid-rows-[1fr]` 过渡（参考 `packages/views/modals/create-issue.tsx` 中的 `CreateRunHint`），或 motion 的 `height: "auto"`。

### 列表增删行

归档、完成、延后、新通知、新运行。

- 移除的行收起高度、外边距和透明度，150ms，`UI_EASE_OUT`，放在 `AnimatePresence initial={false}` 中。下方的行补上空位，不跳变。
- 用户刚添加或实时到达的行淡入，最多向上移动 4px，150ms。
- 首次渲染的行，以及虚拟列表滚动时挂载的行，一律不加动效。

### 内容切换

标签、视图、改变列表的筛选、详情面板的上一项和下一项。

- 旧内容淡出（100ms，`UI_EASE_IN`），新内容再淡入（150ms，`UI_EASE_OUT`）。
- 新内容加载期间，容器保持原高度，加载完成后过渡到新高度（200ms）。不要先塌成 0 再撑开。
- 只有顺序有含义时才滑动，比如上一项和下一项：最多 12px，160ms（`.animate-inbox-enter-next` / `-prev`）。
- 加载占位使用 `.animate-skeleton`，前 200ms 不显示，数据很快到达时不会闪出骨架屏。

### 选中态指示器

标签和分段控件。

- 指示器从旧选项移到新选项，标签文字不动。使用共享控件（`Tabs`、`SegmentedToggle`）自带的指示器和过渡，不要在调用处自己做指示器。
- 切换时标签宽度会变的控件，不能用 layout 动画。比如关闭时只显示图标的标签，或 header 多出、少了一个按钮。layout 动画量到的是仍在移动的盒子，会冲过头。应在绘制前一次性量好每个部分的起点和终点，所有部分走同一条曲线；指示器用同一条曲线和时长，从当前位置直接过渡到新标签量好的终点。不要跟着移动中的盒子推算指示器：混合移动中的盒子，在新标签起点离终点较远时会冲过头。参考 `packages/views/inbox/components/inbox-tabs.tsx` 中的 `useTabSwitchMotion`。

### 状态与数值反馈

计数、进度、复制、发送/停止、保存状态、开关。

- 原地变化的数字使用 `NumberFlow`。
- 进度条和进度环过渡发生变化的属性（`transition-[width]`、`transition-[stroke-dashoffset]`），150–200ms。
- 确认操作的图标切换（复制 → 勾、发送 → 停止）交叉淡入并带轻微缩放，约 140ms；复制使用 `CopyCheckIcon`。
- 颜色和背景的状态变化使用 `transition-colors`。

## 减少动态效果

外观设置中的「减少动态效果」和系统的 `prefers-reduced-motion` 都表示瞬切：不移动，不做尺寸动画，也不用淡入淡出代替。

- 外观设置会把整个文档的 CSS 动画和过渡时长归零。系统设置只能通过媒体查询影响 CSS，所以每个 CSS 动画或过渡都要加 `motion-reduce:animate-none` 或 `motion-reduce:transition-none`。
- JavaScript 动效（motion/react、WAAPI）读取 `@multica/core/appearance` 的 `useReducedMotion()`，渲染之外用 `prefersReducedMotion()`，两者都覆盖这两个设置。任一设置开启时传 `duration: 0` 或跳过动画。motion 自带的 `useReducedMotion` 和单独的 `matchMedia` 判断只能看到系统设置。应用根部的 `MotionProvider` 会让 motion/react 在任一设置开启时跳过位移和布局动画，但透明度和颜色仍会过渡，所以上面的 `duration: 0` 仍然需要。
- 测试减少动态效果的分支：开启后，终点状态在同一帧出现。

## 不要加动效

以下场景保持瞬切：

- Desktop 标签正文切换（`tab-content.tsx`）。
- J/K 等高频键盘导航，比如在 `IssuePeek` 中逐个翻看 Issue。
- 虚拟列表在滚动或恢复历史时挂载的行。只有实时到达、或用户刚触发的内容可以加动效。
- 跟随指针的操作：拖拽调整面板尺寸（`data-sidebar-resizing`、`data-resizing`），以及画布的平移和缩放。

## 性能

- 在 Virtuoso 等虚拟列表中，只给实时到达或用户刚操作的行加动效，在行上用 WAAPI 播放一次（`useNewRunIds`、`useRunCommentMotion`）。不要用 Base UI 的 `Collapsible.Panel` 包住虚拟列表中的长内容。
- 动画期间不要每帧读布局：不要在 `onUpdate` 或 `requestAnimationFrame` 循环中调用 `getBoundingClientRect()`。在 layout effect 中量一次，然后在量好的值之间做动画。
- 优先使用 `transform` 和 `opacity`。只对尺寸真正变化的容器做宽高动画。
- 循环动效离开屏幕时暂停（`useRunAnimationVisibility`）。

## 共享原语

| 需求 | 使用 | 位置 |
| --- | --- | --- |
| 时长和曲线 | `UI_MOTION_DURATION`、`UI_EASE_OUT`、`UI_EASE_IN`、`UI_INDICATOR_SPRING`（选中态指示器） | `packages/ui/lib/motion.ts` |
| 浮层进退场 | 浮层组件；Dialog 的 `--dialog-*` token | `packages/ui/components/ui/`、`packages/ui/styles/tokens.css` |
| 展开折叠 | `CollapsibleContent` | `packages/ui/components/ui/collapsible.tsx` |
| 标签指示器 | `Tabs` | `packages/ui/components/ui/tabs.tsx` |
| 分段控件指示器 | `SegmentedToggle` | `packages/views/common/segmented-toggle.tsx` |
| motion/react 的减少动态效果 | `MotionProvider`（挂在 web 和桌面端根部） | `packages/views/common/motion-provider.tsx` |
| 标签名会展开收起的标签 | `useTabSwitchMotion` | `packages/views/inbox/components/inbox-tabs.tsx` |
| 加载期间保持高度的内容切换 | `InboxTabBody`（模式；出现第二个使用者时再抽成原语） | `packages/views/layout/panels/inbox-section.tsx` |
| 行退场收起 | Inbox 行的 `motion.li` 退场 | `packages/views/layout/panels/inbox-section.tsx` |
| 右侧栏展开收起 | `AnimatedRightSidebar` | `packages/views/layout/animated-right-sidebar.tsx` |
| 详情的上一项和下一项 | `.animate-inbox-enter-next`、`-prev`、`-fade` | `packages/ui/styles/base.css` |
| 虚拟列表中的实时到达 | `useNewRunIds`、`useRunCommentMotion`、`useRunDisclosureMotion` | `packages/views/issues/components/use-run-comment-motion.ts` |
| 变化的数字 | `NumberFlow` | `packages/ui/components/ui/number-flow.tsx` |
| 复制确认 | `CopyCheckIcon` | `packages/ui/components/common/copy-check-icon.tsx` |
| 加载占位 | `.animate-skeleton` | `packages/ui/styles/base.css` |

新增原语时在表中加一行，并让上面的规则指向它。

## 验证

jsdom 没有布局，单元测试只能覆盖接线和减少动态效果的分支，测不了动画本身。静态截图也看不出跳变和冲过头。

UI PR 只要包含改变尺寸、位置或显示内容的交互，就附上这段交互的 GIF，以及在真实应用中逐帧采样的结果。`scripts/motion/capture.mjs` 在 `make up` 环境中完成这两件事；`scripts/motion/inbox-tabs.mjs` 是可以照着写的完整场景：

```bash
make up C=api,web ARGS="--ephemeral"
make env-exec ARGS="-- node scripts/dev-fixtures.mjs sidebar"
make env-exec ARGS="-- node scripts/motion/capture.mjs scripts/motion/inbox-tabs.mjs --out ../motion"
```

场景在每个动画帧记录相关元素的位置和尺寸，默认检查：

- 指示器终点准确落在目标上，途中不冲过头（`landsOn`、`monotonic`）；
- 容器高度单向变化，并且分布在多帧中（`monotonic`、`animated`）；
- 下方区块不会跳开再跳回（`monotonic`）；
- 减少动态效果时，每个变化都在一帧内完成（`instant`）。

用 `delay()` 拖住标签或页面的首个请求，复现加载期间的表现。脚本开头的注释列出了选项和限制。
