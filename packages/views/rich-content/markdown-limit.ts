/**
 * Content longer than this renders as plain text instead of Markdown (#11547).
 *
 * Formatting time is not linear in the length of one paragraph: GFM's
 * autolink-literal extension rescans the paragraph as it goes, so a pasted log
 * or JSON export of a megabyte takes several seconds to parse and freezes
 * whatever surface shows it, even with our patch for its worst case
 * (`patches/micromark-extension-gfm-autolink-literal@2.1.0.patch`). Below this
 * size every shape we measured (prose, logs, single-line JSON) parses in about
 * 100 ms. Bodies past it are dumps, which read no worse as text.
 *
 * Kept apart from the renderer so data-only code (timeline height estimates)
 * can apply the same rule without importing it.
 */
export const MAX_MARKDOWN_CHARS = 200_000;

export function rendersAsPlainText(content: string): boolean {
  return content.length > MAX_MARKDOWN_CHARS;
}
