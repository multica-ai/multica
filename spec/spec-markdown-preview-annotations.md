# Markdown 预览批注并发送评论 Spec

## 背景

当前 Markdown 附件预览已通过 `AttachmentPreviewModal` 识别 `text/markdown`、`.md`、`.markdown` 文件，并使用 `ReadonlyContent`/`Markdown` 渲染为只读预览。用户希望在预览页面内直接选择一段字符，填写备注；可以连续做多轮备注，最后点击按钮，将这些备注一次性发送到当前 issue 的评论区。

本需求的关键不是“高亮样式”，而是要把用户在渲染后的网页里选择的字符，稳定还原成 Markdown 源文件中的范围：文件名、起止行、起止字符，并随备注内容一起进入评论。

## 目标

在 Markdown 文件预览中支持临时批注工作流：

1. 用户在预览内容中用鼠标选择一段文本。
2. 页面出现“添加备注”浮层或按钮。
3. 用户输入备注后，该批注进入本次预览会话的批注列表。
4. 用户可重复选择文本并添加多条备注。
5. 用户点击“发送到评论区”后，系统生成一条普通 issue 评论，评论内容包含每条备注的源文件范围、原文摘录和备注正文。

第一版以“批注作为评论内容发送”为目标，不做结构化批注持久化表。

## 非目标

- 不实现 Google Docs 式实时协同批注。
- 不在 Markdown 预览中永久展示历史评论批注。
- 不新增独立的批注列表 API 或批注数据库表。
- 不支持跨多个 Markdown 文件的一次性批注提交。
- 不支持跨 iframe、跨 HTML 预览、PDF、图片、视频、普通代码预览的批注。
- 不支持跨多个不连续选区合并为一条备注。
- 不对发送后的评论和预览中的高亮建立双向跳转。

## 用户体验

### 入口

适用位置：

- 附件预览弹窗中的 Markdown 预览：`packages/views/editor/attachment-preview-modal.tsx`
- 附件全页预览如后续支持 Markdown 渲染，也应复用同一组件：`packages/views/attachments/attachment-preview-page.tsx`

当预览类型为 `markdown` 且当前上下文能解析到 `issueId` 时，预览区域顶部右侧显示一个批注工具条：

- `批注 0`：显示当前暂存批注数量。
- `发送到评论区`：仅在批注数量大于 0 时可用。
- `清空`：仅在批注数量大于 0 时显示。

### 添加备注

用户选择文本后：

- 如果选区完全落在当前 Markdown 预览内容内，显示一个小浮层按钮“添加备注”。
- 点击后打开小型 Popover，包含：
  - 原文摘录，只读，最多显示 3 行，过长截断。
  - 备注输入框。
  - “保存备注”按钮。
  - “取消”按钮。
- 保存后：
  - 选中文本以浅色高亮显示。
  - 工具条计数加 1。
  - 右侧或底部出现“本次批注”列表，展示范围、摘录、备注。

### 多轮备注

多轮备注保存在当前预览组件的本地 state 中。关闭预览、切换附件、刷新页面后不保留。

允许范围重叠。重叠高亮第一版不做复杂分层，只要求：

- 同一段文本被多条批注覆盖时，仍能看到高亮。
- 批注列表中按添加顺序展示。
- 发送评论时按源文件范围排序，再按添加顺序兜底。

### 发送到评论区

点击“发送到评论区”后：

1. 前端将本地批注格式化成 Markdown 评论正文。
2. 调用现有评论创建能力：`api.createComment(issueId, content)`。
3. 发送成功后清空本地批注并关闭或保留预览，由产品交互决定；推荐保留预览并 toast “已发送到评论区”。
4. 发送失败时保留批注，toast 显示失败原因。

评论格式建议：

```markdown
Markdown 批注：README.md

1. `README.md:L12:C5-L12:C18`

   > 被批注的原文摘录

   备注：这里需要补充启动参数说明。

2. `README.md:L30:C1-L32:C9`

   > 多行摘录第一行
   > 多行摘录第二行

   备注：这里和实际命令不一致。
```

范围规则：

- 行号从 1 开始。
- 字符位置从 1 开始。
- 字符位置按 Unicode code point 计算，不按 UTF-16 code unit 计算，避免中文和 emoji 定位偏移。
- 结束字符使用闭区间，即 `C18` 表示选区包含第 18 个字符。

## 推荐方案

推荐采用“源文本定位 + 渲染 DOM 映射 + 普通评论提交”的方案。

核心思路：

- Markdown 源文本仍是定位的唯一真相。
- 渲染时为可选中的文本节点注入源位置元数据。
- 用户在网页上选择文本后，从 DOM selection 回溯到源位置元数据，换算出 Markdown 源文件行/字符范围。
- 批注只在前端暂存。
- 发送时走现有评论接口，不新增后端数据模型。

这样改动范围集中在前端预览和 Markdown 渲染层，后端只在必要时补 issueId 透传，不需要新增评论类型或迁移。

## 备选方案

### 方案 A：只在源码视图中批注

在 Markdown 预览旁边或切换到源码模式，用户直接选择原始 Markdown 文本。定位最准确，实现简单，但不满足“在预览网页上直接标记字符”的体验诉求。

不推荐作为主方案，可作为定位失败时的后续兜底能力。

### 方案 B：新增结构化批注表

新增 `markdown_annotation` 表，持久化附件、范围、备注、作者，再支持预览中展示历史批注。功能完整，但第一版明显过重，而且用户当前只要求发送到评论区。

不推荐第一版实现，可作为第二阶段。

### 方案 C：只记录选中文本，不记录精确行列

发送评论时只包含摘录和备注，不包含行/字符范围。实现快，但不满足明确要求。

不采用。

## 前端设计

### 组件拆分

新增组件建议：

- `packages/views/editor/markdown-annotation-preview.tsx`
  - 负责 Markdown 文本渲染、选区监听、批注列表、发送按钮。
  - 输入：`attachmentId`、`filename`、`content`、`issueId`、`attachments`。
  - 输出：调用 `api.createComment` 或接收 `onSubmitAnnotations` 回调。

- `packages/views/editor/markdown-annotation-types.ts`
  - 定义 `MarkdownAnnotationDraft`、`SourceRange`、`SourcePoint`。

- `packages/views/editor/markdown-source-position.ts`
  - 负责将 Markdown 源文本 offset 转换为 `{ line, character }`。
  - 负责从 DOM selection 转换为 `SourceRange`。

- `packages/views/editor/markdown-annotation-comment.ts`
  - 负责把批注数组格式化为评论 Markdown。

`packages/ui/markdown/Markdown.tsx` 增加可选能力：

- `sourceMap?: boolean`
- `onTextSelection?: (selection: MarkdownSourceSelection) => void`

或者更保守地新增一个内部组件 `SourceMappedMarkdown`，避免影响现有 `Markdown` 默认行为。推荐第一版采用保守做法：新增 `SourceMappedMarkdown`，复用现有 `Markdown` 的插件、sanitize、components 配置，尽量不改变普通 Markdown 渲染路径。

### 源位置映射

实现要求：

1. 渲染 Markdown 前保留原始 `content`。
2. 使用 Markdown AST 节点的 `position.start.offset`、`position.end.offset` 建立源 offset。
3. 对文本节点渲染为带元数据的 span，例如：

```html
<span data-md-start="123" data-md-end="145">selected text</span>
```

4. 用户选择文本时，读取 `window.getSelection()`。
5. 仅当 anchor/focus 都落在 `data-md-start/end` 节点内时允许保存备注。
6. 将 DOM 节点内相对字符偏移换算为源 offset，再换算成行/字符。

注意：

- 不能基于渲染后的 `innerText` 全文搜索反推位置，重复文本会误判。
- 不能在发送评论时只保存 DOM range，因为刷新后失效。
- 预处理链路 `preprocessMentionShortcodes`、`preprocessLinks`、`preprocessFileCards` 会改变输入文本；批注定位必须基于原始 Markdown，因此用于定位的渲染链路不能在 AST position 生成前破坏原文 offset。

推荐处理方式：

- 对批注预览关闭自动 linkify/file-card 预处理，或将预处理移动到不会破坏原始 source position 的 AST 层。
- 第一版优先保证普通 Markdown 文本、标题、列表、引用、代码块定位准确。
- 对自动生成的链接/file-card、图片附件卡片等无法稳定映射的元素，不允许选中后保存备注，并提示“该区域暂不支持批注”。

### 选区限制

允许：

- 普通段落文字。
- 标题文字。
- 列表项文字。
- 引用文字。
- 表格单元格文字。
- 代码块内文字。

暂不支持：

- 图片本身。
- 附件 file-card。
- Mermaid/KaTeX 渲染后的图形区域。
- 链接自动生成部分中无法映射到原始 Markdown 的字符。
- 跨越多个无法连续映射节点的复杂选区。

如果选区无效：

- 不显示“添加备注”按钮。
- 或在点击保存时提示：“当前选区无法定位到 Markdown 源文件，请选择纯文本内容。”

### 评论提交接入

`MarkdownAnnotationPreview` 需要拿到 `issueId`。当前 `AttachmentPreviewModal` 主要面向附件预览，不一定持有 issue 上下文，因此需要检查调用链：

- 评论/issue 中打开附件预览时，传入 `issueId`。
- chat 或其他非 issue 场景打开 Markdown 附件时，不显示“发送到评论区”按钮，只允许复制批注文本，或者第一版直接不启用批注功能。

推荐第一版规则：

- 只有 `issueId` 存在时启用批注。
- 没有 `issueId` 时保持现有 Markdown 预览，不显示任何新 UI。

## 数据结构

前端本地类型：

```ts
type SourcePoint = {
  line: number;
  character: number;
  offset: number;
};

type SourceRange = {
  start: SourcePoint;
  end: SourcePoint;
};

type MarkdownAnnotationDraft = {
  id: string;
  attachmentId: string;
  filename: string;
  range: SourceRange;
  quote: string;
  note: string;
  createdAt: number;
};
```

评论内容不需要新增 API 字段，使用现有 `content` 即可。

## 后端设计

第一版不新增后端接口、不新增数据库表。

继续使用：

- `POST /api/issues/{issueId}/comments`
- `api.createComment(issueId, content)`
- timeline / WebSocket 现有评论刷新机制

如果后续要支持结构化历史批注，再新增：

- `markdown_annotation` 表。
- `GET /api/attachments/{id}/annotations`
- `POST /api/attachments/{id}/annotations`
- `PATCH/DELETE /api/annotations/{id}`

这些不进入第一版。

## 权限与安全

- 只有当前用户有 issue 评论权限时才允许发送批注。
- 前端不应把附件内容或批注发送到新接口；第一版只发送普通评论内容。
- 评论内容需要继续走现有 Markdown sanitize/render 管线。
- 引用原文需要限制长度，避免用户误选整篇文档后生成过长评论。
- 建议单条批注 quote 最多 500 字符，超过时中间截断。
- 建议一次发送最多 50 条批注，超过时禁止发送并提示拆分。

## 国际化

新增文案放在 `packages/views/locales/*/editor.json` 或当前附件预览所属命名空间：

- `annotation.count`
- `annotation.add`
- `annotation.note_placeholder`
- `annotation.save`
- `annotation.cancel`
- `annotation.clear`
- `annotation.send_to_comments`
- `annotation.sent`
- `annotation.send_failed`
- `annotation.invalid_selection`
- `annotation.empty_note`

中文建议：

- “添加备注”
- “本次批注”
- “发送到评论区”
- “当前选区无法定位到 Markdown 源文件，请选择纯文本内容。”
- “请输入备注内容。”

## 测试计划

### 单元测试

`packages/views/editor/markdown-source-position.test.ts`

覆盖：

- 单行 offset 转行/字符。
- 多行 offset 转行/字符。
- 中文字符按 code point 计算。
- emoji 按 code point 计算。
- 结束位置闭区间格式化。

`packages/views/editor/markdown-annotation-comment.test.ts`

覆盖：

- 单条批注格式化。
- 多条批注排序。
- 多行 quote 转 Markdown blockquote。
- quote 过长截断。

### 组件测试

`packages/views/editor/markdown-annotation-preview.test.tsx`

覆盖：

- Markdown 预览中选中文本后出现“添加备注”。
- 保存备注后批注计数增加。
- 多条备注展示在批注列表。
- 点击“发送到评论区”调用 `api.createComment(issueId, formattedContent)`。
- 发送成功后清空本地批注。
- 无 `issueId` 时不显示批注工具条。

### 回归测试

现有 Markdown 渲染测试不能被破坏：

- `packages/views/common/markdown.test.tsx`
- `packages/views/editor/attachment-preview-modal.test.tsx`

重点确认普通评论、chat 消息、skill 文件 Markdown 渲染不受 source-map 批注逻辑影响。

## 实施步骤

1. 抽象源位置工具：offset 到行/字符、quote 截断、评论格式化。
2. 新增 source-mapped Markdown 渲染组件，只在批注预览中使用。
3. 在 Markdown 附件预览分支替换为 `MarkdownAnnotationPreview`，并仅在有 `issueId` 时启用批注 UI。
4. 在附件预览调用链中透传 `issueId`。
5. 接入 `api.createComment`，发送成功后复用现有 timeline 刷新机制。
6. 补齐 i18n 文案和测试。

## 风险与处理

### 渲染文本和源 Markdown 不一致

风险：自动 linkify、file-card 预处理会改变源文本，导致 DOM 位置和源文件位置不一致。

处理：第一版批注预览使用 source position 优先的渲染链路；无法映射的增强渲染区域禁用批注。

### 复杂 Markdown 节点选区跨界

风险：用户从一个段落拖到表格或代码块，映射不连续。

处理：只允许同一连续 source range 内的选区；无法确认连续性时拒绝保存。

### 评论内容过长

风险：多轮批注后一次评论过长。

处理：限制批注数量和 quote 长度，备注正文不主动截断但发送前显示字符数提示。

### 包边界污染

风险：`packages/ui` 引入业务评论逻辑。

处理：`packages/ui` 只提供 source-mapped Markdown 渲染或通用 selection 回调；批注 state、评论提交、toast、i18n 放在 `packages/views`。

## 验收标准

- 在 issue 中打开 Markdown 附件预览，可以选择预览里的文本并添加备注。
- 可以连续添加多条备注。
- 每条备注都展示文件名、起止行、起止字符、原文摘录、备注正文。
- 点击“发送到评论区”后，issue 评论区新增一条包含全部批注的评论。
- 评论中的范围格式为 `filename:Lx:Cy-La:Cb`。
- 中文字符位置计算正确。
- 无 `issueId` 的 Markdown 预览不出现批注入口。
- 普通 Markdown 渲染、评论输入、附件预览现有能力不回退。
