# TC-085: Wiki/Issue 正文渲染统一为 editor-parity mode（OPE-2836）

## 关联信息

- **OPE 编号**: OPE-2836
- **Gitee PR**: !392
- **Commit SHA**:
  - 429dcc88c — feat: 迁移预处理函数到 ui/markdown 并添加 mark 标签白名单
  - 2a1ef900c — feat: 新增 editor-parity mode 并将 ReadonlyContent 改为薄封装
  - 511be4fd1 — fix: 修复内部链接导航回归并清理重复测试块
  - c6f2e5486 — perf: lowlight 按需加载并清理 editor-parity 渲染器
  - b57f552f1 — refactor: core+postprocess-hook 收口，上游文件回归原位
- **特性摘要**: 消除三套 react-markdown 渲染器分裂（`ReadonlyContent` / `ui/Markdown` / `views/common/Markdown`），将 `ReadonlyContent` 退化为薄封装；`ui/Markdown` 新增 `editor-parity` mode + `postprocess` hook，views wrapper 通过 hook 注入 editor-parity 专属预处理（`preprocessJsonLiterals` + `highlightToHtml`）；上游文件保持原位，零 fork 偏离；lowlight 改为按需动态加载

## 架构设计

```
views/common/markdown.tsx（wrapper）
  ├─ 注入 Mention/Attachment/Mermaid/HtmlBlock/LinkHover 渲染器
  ├─ 注入 openLink 客户端导航
  └─ 传入 postprocess: (c) => highlightToHtml(preprocessJsonLiterals(c))

ui/markdown/Markdown.tsx（core engine）
  ├─ 预处理管线：mention → link → filecard → postprocess(json+mark)
  ├─ editor-parity mode：lowlight 代码高亮（CodeBlockHighlighted 组件自订阅）
  ├─ sanitize schema 白名单 <mark> 标签
  └─ 所有 mode 共享 baseComponents（mention/link/filecard/image/fileCard）

views/editor/utils/（上游原位，零 fork 偏离）
  ├─ highlight-markdown.ts — ==text== → <mark> lowering
  ├─ highlight-match.ts — 边界规则（editor tokenizer + 只读 lowering 共用）
  └─ preprocess-json.ts — 裸 JSON → ```json 代码块包裹
```

## 涉及源文件

- `packages/ui/markdown/Markdown.tsx` — 新增 `editor-parity` render mode、`postprocess` hook prop、`CodeBlockHighlighted` 组件、lowlight 懒加载
- `packages/ui/markdown/Markdown.test.tsx` — postprocess hook 集成测试（mock）、lowlight 异步加载测试
- `packages/views/common/markdown.tsx` — editor-parity preset（注入渲染器 + postprocess hook）
- `packages/views/editor/readonly-content.tsx` — 薄封装（`ViewsMarkdown mode="editor-parity"`）
- `packages/views/editor/utils/highlight-markdown.ts` — 上游原位，完整实现（非 re-export）
- `packages/views/editor/utils/highlight-match.ts` — 上游原位，完整实现（非 re-export）
- `packages/views/editor/utils/preprocess-json.ts` — 上游原位，完整实现（非 re-export）

## 验证要点

1. **Wiki/Issue 只读渲染无回归**：代码块高亮（lowlight 加载前为纯文本、加载后升级为高亮 span）、`==mark==` 语法、Mermaid 图表、HTML 块预览、表格 `.tableWrapper`、mention 链接、图片/附件渲染
2. **内部链接导航**：ReadonlyContent 中的内部工作区链接走客户端导航（`openLink`），外部链接新标签页打开
3. **Chat 不受影响**：`minimal`/`full`/`terminal` mode 行为不变，不触发 lowlight 加载
4. **Bundle 隔离**：`createLowlight` 仅出现在独立 async chunk（764K），共享/框架 chunk 均不含 lowlight
5. **预处理器单一真源**：editor 扩展与只读 lowering 共用 `highlight-match`，`==text==` 边界规则不漂移
6. **管线顺序**：`mention → link → filecard → postprocess(json+mark)`，与上游 ReadonlyContent 逐位一致
7. **上游零 fork 偏离**：`highlight-markdown.ts` / `highlight-match.ts` / `preprocess-json.ts` 与 main 无 diff

## 上游同步注意

上游的 `views/editor/utils/highlight-markdown.ts`、`highlight-match.ts`、`preprocess-json.ts` 保持原位。上游改动可直接合入，无需同步到其他位置。`readonly-content.tsx` 的薄封装与上游独立渲染器冲突时，保留薄封装（底层通过 `Markdown.tsx` + `views/common/markdown.tsx` 反映上游改动）。
