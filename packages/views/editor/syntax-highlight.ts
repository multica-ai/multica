import { toHtml } from "hast-util-to-html";
import { common, createLowlight } from "lowlight";

const baseLowlight = createLowlight(common);

/**
 * Tiptap calls `highlightAuto` whenever a code fence has no registered
 * language. Auto-detection runs every registered grammar over the full block,
 * which makes large documents expensive to mount. Preserve the interface but
 * make that fallback deterministic and cheap: explicit known languages are
 * still highlighted, while unlabelled or unknown blocks stay plaintext.
 */
export const codeLowlight: ReturnType<typeof createLowlight> = {
  ...baseLowlight,
  highlightAuto(value) {
    return baseLowlight.highlight("plaintext", value);
  },
};

export function highlightCode(value: string, language?: string) {
  const normalizedLanguage = language?.trim().toLowerCase();

  return normalizedLanguage && baseLowlight.registered(normalizedLanguage)
    ? baseLowlight.highlight(normalizedLanguage, value)
    : baseLowlight.highlight("plaintext", value);
}

const HIGHLIGHTED_HTML_LIMIT = 300;
const highlightedHtml = new Map<string, string>();

/**
 * `highlightCode` serialized to HTML, cached by language and source. A code
 * block highlights again every time its comment remounts (scrolled back into a
 * virtualized timeline, the issue reopened), for an identical result.
 */
export function highlightCodeHtml(value: string, language?: string): string {
  const key = `${language?.trim().toLowerCase() ?? ""}\u0000${value}`;
  const hit = highlightedHtml.get(key);
  if (hit !== undefined) {
    // Least recently used goes first.
    highlightedHtml.delete(key);
    highlightedHtml.set(key, hit);
    return hit;
  }
  const html = toHtml(highlightCode(value, language));
  highlightedHtml.set(key, html);
  if (highlightedHtml.size > HIGHLIGHTED_HTML_LIMIT) {
    highlightedHtml.delete(highlightedHtml.keys().next().value!);
  }
  return html;
}
