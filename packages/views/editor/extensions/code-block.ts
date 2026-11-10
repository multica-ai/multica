import CodeBlockLowlight from "@tiptap/extension-code-block-lowlight";

/**
 * Splits a fence info string into its language (the first word) and the rest,
 * e.g. ```mermaid title="Request flow" → `mermaid` + `title="Request flow"`.
 * Everything that dispatches on the language (preview, highlighting, the
 * label) reads `language`; `meta` is only carried so a save writes the info
 * string back unchanged (#10255).
 */
export function splitFenceInfo(info: string | null | undefined): {
  language: string | null;
  meta: string | null;
} {
  const trimmed = (info ?? "").trim();
  if (!trimmed) return { language: null, meta: null };
  const space = trimmed.search(/\s/);
  if (space === -1) return { language: trimmed, meta: null };
  return {
    language: trimmed.slice(0, space),
    meta: trimmed.slice(space).trim() || null,
  };
}

export const CodeBlockExtension = CodeBlockLowlight.extend({
  addAttributes() {
    return {
      ...this.parent?.(),
      meta: {
        default: null,
        parseHTML: (element) => element.getAttribute("data-meta"),
        renderHTML: (attributes) =>
          attributes.meta ? { "data-meta": attributes.meta } : {},
      },
    };
  },
  parseMarkdown(token, helpers) {
    // Same acceptance rule as @tiptap/extension-code-block: fenced or indented.
    const raw = token.raw ?? "";
    if (!raw.startsWith("```") && !raw.startsWith("~~~") && token.codeBlockStyle !== "indented") {
      return [];
    }
    return helpers.createNode(
      "codeBlock",
      splitFenceInfo(token.lang),
      token.text ? [helpers.createTextNode(token.text)] : [],
    );
  },
  renderMarkdown(node, helpers) {
    const content = helpers.renderChildren(node.content ?? []);
    // A literal fence inside the code must not close the serialized block.
    let fenceLength = 3;
    for (const [run] of content.matchAll(/`+/g)) {
      fenceLength = Math.max(fenceLength, run.length + 1);
    }
    const fence = "`".repeat(fenceLength);
    const language = node.attrs?.language ?? "";
    const meta = node.attrs?.meta;
    const info = meta ? `${language} ${meta}` : language;
    return `${fence}${info}\n${content}\n${fence}`;
  },
});
