/**
 * Best-effort cleanup of a markdown string for compact preview UI — e.g.
 * the "Replying to X" chip above the inline comment composer. NOT a full
 * markdown parser; handles the few patterns that actually appear in
 * multica comments and would otherwise show as visible syntax garbage in a
 * one- or two-line preview:
 *
 *   - mention links   `[@Alice](mention://member/uuid)` → `@Alice`
 *   - issue mentions  `[MUL-123](mention://issue/uuid)` → `MUL-123`
 *   - images          `![filename](url)` / `![](url)`  → `📷`
 *   - plain links     `[label](https://...)`           → `label`
 *
 * Collapses runs of blank lines so a deeply-spaced comment doesn't waste
 * the preview's two-line budget. Single `\n` is preserved so multi-line
 * comments still show two distinct lines.
 *
 * Everything else passes through unchanged. The output is meant for
 * display via `<Text numberOfLines={N}>` — no further escaping needed.
 */
export function stripMarkdown(md: string): string {
  if (!md) return "";
  return md
    // Images first (the leading `!` distinguishes from a plain link).
    .replace(/!\[[^\]]*\]\([^)]*\)/g, "📷")
    // Mention links — keep the label (already includes `@` or the
    // issue identifier verbatim per `mention-extension.ts`).
    .replace(/\[([^\]]+)\]\(mention:\/\/[^)]+\)/g, "$1")
    // Plain links last — strip the URL, keep the label.
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    // Collapse multi-blank-line runs to single \n so the preview budget
    // isn't spent on whitespace.
    .replace(/\n{2,}/g, "\n")
    .trim();
}
