# AI 会话视觉规范对齐

AI 会话使用移动端既有控件和主题。审查范围包括电脑/会话列表、消息、输入、引用选择、模型与权限、队列、用量、文件、附件和 Side；本次没有改动全局控件或主题。

## 对照依据

| 基准代码 | 对齐内容 |
| --- | --- |
| [Button](../../components/ui/button.tsx)、[IconButton](../../components/ui/icon-button.tsx) | 默认按钮 40 pt、紧凑按钮 36 pt、主次操作色和禁用态 |
| [TextField](../../components/ui/text-field.tsx)、[Tabs](../../components/ui/tabs.tsx) | 搜索框尺寸、输入底色、分类选中态和边框 |
| [评论与聊天输入区](../../components/composer/message-composer.tsx) | 浅层灰色输入容器、16 pt 圆角、32 pt 工具按钮与发送按钮 |
| [引用与附件标签](../../components/issue/composer-attachment-row.tsx) | 28 pt 胶囊、12 pt 文本、主色透明底与小号移除图标 |
| [优先级选择器](../../components/issue/pickers/priority-picker-body.tsx)、[账号设置](../../app/(app)/[workspace]/more/settings.tsx) | 行内文字层级、选中勾选、次要信息和导航箭头 |
| [聊天消息](../../components/chat/chat-message-list.tsx)、[评论卡片](../../components/issue/comment-card.tsx) | 用户气泡宽度、圆角和卡片层级 |

## 根因与处理

| 差异 | 处理 |
| --- | --- |
| 多数 AI 按钮将默认尺寸覆盖为 44 pt，紧凑按钮也被拉高 | 恢复 40/36 pt 视觉尺寸；保留自动高度和自然行高，适配大字号。小图标使用额外点击范围 |
| AI 的完成、加载更多、选择项和智能体标签额外使用品牌蓝色 | 操作沿用 Button 的文字颜色，选择项使用主题主色勾选，标签使用 `primary/10`；保留错误等状态色 |
| 引用和 Side 分类各自实现选项按钮 | 复用 RNR Tabs 的中性选中背景；内容宽度不足时横向滚动，避免大字号分类文字拆行 |
| 搜索框额外增加高度和内边距 | 按 TextField 的 40 pt 基准显示，并保留随字号增长的高度 |
| AI 输入区为白底、工具按钮 44 pt，发送图标另有一层圆形容器 | 与既有 MessageComposer 的背景、内边距和圆角对齐；工具图标 20 pt，发送图标 18 pt、按钮 32 pt，保留停止/排队/重试行为 |
| 引用与附件标签被移除按钮撑高，智能体标签另用蓝色 | 使用相同胶囊体系和 12 pt 移除图标，保留类型/调用文案及智能体头像 |
| 模型行使用蓝色圆圈和较重分割线，文件导航用文本字符画箭头 | 对齐现有选择器行与 20 pt 主题勾选；使用 Ionicons 导航箭头 |
| 会话及 Side 标题栏的按钮背景和返回方式不统一 | 沿用会话列表的平面工具栏；提供明确返回按钮，Side 不重复提供关闭按钮 |
| 用户气泡和执行细节卡片比外部会话更大、更重 | 用户气泡使用既有聊天的 80% 最大宽度、12 pt 圆角和 muted 底色；执行细节及问答使用 surface-1 层级 |
| 直接打开 AI 弹层时缺少底层列表路由，标题顶入状态栏 | 在电脑会话路由中沿用工作区已有的 `anchor` 配置，让原生弹层落在列表之上 |

采用复用既有控件的方案，保留 AI 独立数据与发送流程。没有新增 UI 依赖、调色板或全局控件变体。
发送、引用上限、智能体头像、每个目录 8 条及加载更多的行为保持原有实现。

## 验证

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
pnpm --filter @multica/mobile test
git diff --check
```

合入最新 main 后，移动端 54 个测试文件、405 项测试及 iOS 启动脚本断言通过。视觉调整使用原生截图检查，不增加与样式实现一一对应的单元测试。

- iPhone 17 Pro、iOS 26.5 的开发应用完成浅色、深色及辅助功能大字号截图检查。
- 选择页直接链接显示为有底层列表的原生弹层；大字号分类标签保持单行。
- 输入区原生渲染确认 16 pt 文字、12/4 pt 上下内边距、32 pt 图标按钮及 36 pt 设置按钮，配色沿用主题。
- 设备外观和字号在检查后恢复原值。开发服务的 NativeWind/Metro 样式监听发生过错误，最终结果使用完整重载检查；本次未修改这些全局依赖。

原生截图保存在 worktree 的 `.tmp/ios-ai-ui-review/style-*.png`。外部设置页作为对照；AI 检查使用不连接电脑的路由参数，未发送消息或启动智能体。电脑端仍需完成此前分页接口的升级，因此真实会话数据、实时刷新和发送中的全流程不属于这次原生视觉验证的结果。
