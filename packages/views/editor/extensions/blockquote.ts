import Blockquote from "@tiptap/extension-blockquote";

// The serializer backslash-escapes every `[` and `]` in text, which turns a
// GitHub alert marker (`> [!NOTE]`) into `> \[!NOTE\]` on the first save —
// a plain quote to every other Markdown reader, agents included.
const ESCAPED_ALERT_MARKER_RE = /^> \\\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\\\]/i;

export function unescapeAlertMarker(markdown: string): string {
  return markdown.replace(ESCAPED_ALERT_MARKER_RE, "> [!$1]");
}

const renderBlockquote = Blockquote.config.renderMarkdown;

export const PatchedBlockquote = Blockquote.extend({
  renderMarkdown: (node, helpers, context) =>
    unescapeAlertMarker(renderBlockquote?.(node, helpers, context) ?? ""),
});
