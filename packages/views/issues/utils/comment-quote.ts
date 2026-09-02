/**
 * Match GitHub's Quote reply behavior: copy the original Markdown into the
 * composer as a blockquote. The quote stays editable and is submitted as part
 * of the new comment without introducing a separate reply relationship.
 */
export function buildCommentQuoteMarkdown(content: string): string {
  return content
    .trim()
    .split(/\r?\n/)
    .map((line) => `> ${line}`)
    .join("\n");
}
