# TC-078: 代码块框选复制与渲染加固（OPE-1678）

## 关联信息

- **OPE 编号**: OPE-1678
- **Gitee PR**: !363
- **Commit SHA**: 6a5a27ccf, 0826c3202, 7520fef80, fb6a7a4c1, 890a14733, fd81cedc1
- **特性摘要**: 修复评论/Markdown 代码块框选复制漂移，部分选区按纯文本复制，代码高亮改用 React 元素渲染，Mermaid 全屏灯箱补充关闭按钮

## 涉及源文件

- `packages/ui/markdown/CodeBlock.tsx`
- `packages/ui/markdown/linkify.ts`
- `packages/ui/markdown/Markdown.tsx`
- `packages/views/editor/code-block-static.tsx`
- `packages/views/editor/extensions/code-block-view.tsx`
- `packages/views/editor/extensions/markdown-copy.ts`
- `packages/views/editor/html-block-preview.tsx`
- `packages/views/editor/mermaid-diagram.tsx`
- `packages/views/issues/components/comment-card.tsx`

## 验证要点

1. 在代码块内框选并复制，粘贴内容与所选文本一致，无漂移
2. 部分选区复制时以纯文本形式输出
3. 代码高亮通过 React 元素渲染，不再使用 dangerouslySetInnerHTML
4. linkify 跳过波浪号/缩进代码块内部，不误伤代码内容
5. Mermaid 全屏灯箱可通过关闭按钮退出
