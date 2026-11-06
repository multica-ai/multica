const EXCLUDED = 'button, input, textarea, [aria-hidden="true"], [hidden], script, style';
const BLOCKS = new Set(["P", "DIV", "LI", "PRE", "BLOCKQUOTE", "H1", "H2", "H3", "H4", "H5", "H6", "TR"]);

/** 选区捕获和原文定位共用同一份可见文本索引。
 * 保留段落边界和代码换行，排除渲染器的操作控件。
 */
export function indexRenderedText(root: HTMLElement, editable = false) {
  let text = "";
  const nodes: { node: Text; start: number; end: number }[] = [];
  const newline = () => { if (text && !text.endsWith("\n")) text += "\n"; };
  function visit(node: Node) {
    if (node.nodeType === Node.TEXT_NODE) {
      const value = node.textContent ?? "";
      nodes.push({ node: node as Text, start: text.length, end: text.length + value.length });
      text += value;
    } else if (node instanceof HTMLElement) {
      if (node.matches(EXCLUDED) || (!editable && node.matches('[contenteditable="true"]'))) return;
      if (node.tagName === "BR") { text += "\n"; return; }
      const block = BLOCKS.has(node.tagName);
      if (block) newline();
      node.childNodes.forEach(visit);
      if (block) newline();
    }
  }
  root.childNodes.forEach(visit);
  return { text, nodes };
}

function contentRoot(node: Node, contentAttribute: string): HTMLElement | null {
  return (node instanceof Element ? node : node.parentElement)?.closest<HTMLElement>(`[${contentAttribute}]`) ?? null;
}

export function captureTextSelection(card: HTMLElement, selection: Selection | null, {
  contentAttribute, scopeAttribute, editable = false,
}: { contentAttribute: string; scopeAttribute: string; editable?: boolean }) {
  if (!selection || selection.isCollapsed || selection.rangeCount !== 1) return null;
  const range = selection.getRangeAt(0);
  const root = contentRoot(range.startContainer, contentAttribute);
  if (!root || root !== contentRoot(range.endContainer, contentAttribute) || !card.contains(root) ||
    root.closest(`[${scopeAttribute}]`) !== card) return null;
  const { text, nodes } = indexRenderedText(root, editable);
  const selected = nodes.filter(({ node }) => range.intersectsNode(node));
  const first = selected[0];
  const last = selected.at(-1);
  if (!first || !last) return null;
  const start = first.start + (range.startContainer === first.node ? range.startOffset : 0);
  const end = last.start + (range.endContainer === last.node ? range.endOffset : last.node.length);
  if (!text.slice(start, end).trim()) return null;
  return {
    sourceId: root.getAttribute(contentAttribute)!,
    quote: text.slice(start, end), start,
    prefix: text.slice(Math.max(0, start - 32), start),
    suffix: text.slice(end, end + 32),
    range: range.cloneRange(), root,
  };
}
