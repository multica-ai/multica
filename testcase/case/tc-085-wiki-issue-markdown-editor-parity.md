# TC-085: Wiki/Issue 正文渲染统一为 editor-parity mode（OPE-2836）

## 关联信息

- **OPE 编号**: OPE-2836
- **Gitee PR**: !392
- **Commit SHA**:
  - 429dcc88c — feat: 迁移预处理函数到 ui/markdown 并添加 mark 标签白名单
  - 2a1ef900c — feat: 新增 editor-parity mode 并将 ReadonlyContent 改为薄封装
  - 511be4fd1 — fix: 修复内部链接导航回归并清理重复测试块
  - c6f2e5486 — perf: lowlight 按需加载并清理 editor-parity 渲染器
- **特性摘要**: 消除三套 react-markdown 渲染器分裂（`ReadonlyContent` / `ui/Markdown` / `views/common/Markdown`），将 `ReadonlyContent` 的增强能力下沉到 `ui/Markdown` 的新 `editor-parity` mode，`ReadonlyContent` 退化为薄封装；预处理函数迁入 `ui/markdown/`，原路径保留 re-export；lowlight 改为按需动态加载，使 chat 静态包不再背负 lowlight + common 语言集

## 涉及源文件

- `packages/ui/markdown/Markdown.tsx` — 新增 `editor-parity` render mode、`CodeBlockHighlighted` 组件、lowlight 懒加载（`getHighlightFn` + `useCodeHighlighter`）
- `packages/ui/markdown/preprocess-json.ts` — 新增（从 views/editor 迁入）
- `packages/ui/markdown/highlight-markdown.ts` — 新增（从 views/editor 迁入）
- `packages/ui/markdown/highlight-match.ts` — 新增（从 views/editor 迁入）
- `packages/ui/markdown/index.ts` — 导出迁移后的函数
- `packages/views/common/markdown.tsx` — 新增 editor-parity preset（注入 Mermaid/HtmlBlockPreview/LinkHoverCard/Mention/Attachment + openLink 客户端导航）
- `packages/views/editor/readonly-content.tsx` — 改为 `ViewsMarkdown mode="editor-parity"` 薄封装
- `packages/views/editor/utils/preprocess-json.ts` / `highlight-markdown.ts` / `highlight-match.ts` — 改为 re-export
- 测试：`packages/ui/markdown/Markdown.test.tsx`、`packages/views/editor/readonly-content.test.tsx` 及迁移函数对应测试

## 验证要点

1. **Wiki/Issue 只读渲染无回归**：代码块高亮（lowlight 加载前为纯文本、加载后升级为高亮 span）、`==mark==` 语法、Mermaid 图表、HTML 块预览、表格 `.tableWrapper`、mention 链接、图片/附件渲染
2. **内部链接导航**：ReadonlyContent 中的内部工作区链接走客户端导航（`openLink`），外部链接新标签页打开
3. **Chat 不受影响**：`minimal`/`full`/`terminal` mode 行为不变，不触发 lowlight 加载
4. **Bundle 隔离**：`createLowlight` 仅出现在独立 async chunk（764K），共享/框架 chunk 均不含 lowlight
5. **预处理器单一真源**：editor 扩展与只读 lowering 共用 `highlight-match`，`==text==` 边界规则不漂移
6. **单元测试**：`ui/markdown` 56 通过、`readonly-content` 31 通过；`preprocessJsonLiterals` 不在 minimal mode 运行
