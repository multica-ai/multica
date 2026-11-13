import { Table } from "@tiptap/extension-table";

// GFM table rows cannot contain physical newlines. Preserve them as inline
// breaks before the stock table serializer collapses cell whitespace.
export const TableWithLineBreaks = Table.extend({
  renderMarkdown(node, helpers, context) {
    const markdown = Table.config.renderMarkdown!.call(
      this,
      node,
      {
        ...helpers,
        renderChildren: (...args: Parameters<typeof helpers.renderChildren>) =>
          helpers.renderChildren(...args).replace(/\r?\n/g, "<br>"),
      },
      context,
    );
    // The stock serializer separates paragraphs inside a cell with U+001F.
    return markdown.replaceAll("\u001f", "<br>");
  },
});
