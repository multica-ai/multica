/**
 * File card preprocessing for markdown content.
 *
 * Converts file-card syntax into HTML divs that can be rendered by
 * react-markdown with a custom `div` component.
 *
 * Two syntaxes are supported:
 * 1. `!file[name](url)` — new unambiguous syntax (no hostname check needed)
 * 2. `[name](cdnUrl)` — legacy syntax, matched by CDN hostname on own line
 *
 * Output: `<div data-type="fileCard" data-href="url" data-filename="name"></div>`
 *
 * All functions are pure — no global state, no imports from core/ or views/.
 */

const IMAGE_EXTS = /\.(png|jpe?g|gif|webp|svg|ico|bmp|tiff?)$/i

/** New syntax: !file[name](url) — unambiguous, no hostname matching needed. */
const NEW_FILE_CARD_RE = /^!file\[([^\]]*)\]\((https?:\/\/[^)]+)\)$/

/** Legacy syntax: [name](cdnUrl) on its own line — matched by CDN hostname. */
const FILE_LINK_LINE = /^\[([^\]]+)\]\((https?:\/\/[^)]+)\)$/

function escapeAttr(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;')
}

function toFileCardHtml(filename: string, url: string): string {
  return `<div data-type="fileCard" data-href="${escapeAttr(url)}" data-filename="${escapeAttr(filename)}"></div>`
}

/**
 * Check if a URL points to our upload CDN.
 *
 * Uses exact hostname match against `cdnDomain` (e.g. "multica-static.copilothub.ai"),
 * and also matches any `.amazonaws.com` subdomain as a fallback for direct S3 URLs.
 */
export function isCdnUrl(url: string, cdnDomain: string): boolean {
  try {
    const u = new URL(url)
    return u.hostname === cdnDomain || u.hostname.endsWith('.amazonaws.com')
  } catch {
    return false
  }
}

/**
 * Check if a CDN URL is a non-image file that should render as a file card.
 * Image URLs (png, jpg, etc.) are excluded — they render as inline images.
 */
export function isFileCardUrl(url: string, cdnDomain: string): boolean {
  try {
    return isCdnUrl(url, cdnDomain) && !IMAGE_EXTS.test(new URL(url).pathname)
  } catch {
    return false
  }
}

/**
 * Preprocess markdown to convert file-card syntax into HTML divs.
 *
 * Handles both `!file[name](url)` (new syntax) and legacy `[name](cdnUrl)`
 * lines. Only standalone lines are matched — inline links are left untouched.
 *
 * @param markdown  Raw markdown string
 * @param cdnDomain CDN hostname for legacy link detection (e.g. "multica-static.copilothub.ai")
 */
export function preprocessFileCards(markdown: string, cdnDomain: string): string {
  return markdown
    .split('\n')
    .map((line) => {
      const trimmed = line.trim()

      // New syntax: !file[name](url) — always a file card, no hostname check needed.
      const newMatch = trimmed.match(NEW_FILE_CARD_RE)
      if (newMatch) {
        return toFileCardHtml(newMatch[1]!, newMatch[2]!)
      }

      // Legacy: [name](cdnUrl) on its own line — CDN hostname matching.
      const match = trimmed.match(FILE_LINK_LINE)
      if (!match) return line
      const filename = match[1]!
      const url = match[2]!
      if (!isFileCardUrl(url, cdnDomain)) return line
      return toFileCardHtml(filename, url)
    })
    .join('\n')
}
