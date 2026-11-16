import { stripChannelMediaMarkers } from "@multica/core/types";

// A preview surface shows one or two clamped lines, but the clamp is CSS:
// the full description still mounts into the DOM and the accessibility
// tree of every card on the board. This ceiling is a DOM-weight bound, not
// a line-width promise — it sits far above what any preview surface can
// display, so the visible text is still decided by the clamp. (#6633)
const PREVIEW_MAX_LENGTH = 300;

/**
 * Flatten description Markdown into a one-line plain-text preview. Shared by
 * every surface that shows a description snippet next to an issue.
 *
 * Channel-media provenance is server-owned merge metadata, not authored
 * content, so it is stripped first: the image Markdown it annotates is removed
 * a line below, and without this the bare HTML comment survives every
 * remaining pass and becomes visible preview text.
 */
export function descriptionPreview(markdown: string): string {
  const preview = (
    stripChannelMediaMarkers(markdown)
      // Backslash-escaped punctuation is unescaped first, and the order is
      // load-bearing. The editor serializes a link as `\[label\](url)`, so the
      // link rule below sees `\[label\]` and its `[^\]]+` capture swallows the
      // trailing backslash while the leading one survives — the preview then
      // reads `\label\`. Unescaping after any structural rule is too late,
      // because the escapes are what stop those rules from matching.
      // The character class is CommonMark's escapable set: all ASCII
      // punctuation. A backslash before anything else (a letter, a space) is a
      // literal backslash in prose and is left alone.
      .replace(/\\([!"#$%&'()*+,\-./:;<=>?@[\\\]^_`{|}~])/g, "$1")
      // The destination group tolerates one level of balanced parentheses,
      // e.g. `(https://en.wikipedia.org/wiki/Foo_(bar))` or a nested link
      // produced when a URL inside a link label gets autolinked, which
      // serializes as `[label]([https://x](https://x))`. A naive `[^)]+`
      // stops at the first `)` and leaves the outer one stranded in the
      // preview text — do not simplify this back to `[^)]+`.
      .replace(/!file\[[^\]]*\]\((?:[^()]|\([^()]*\))*\)/g, "")
      .replace(/!\[[^\]]*\]\((?:[^()]|\([^()]*\))*\)/g, "")
      .replace(/\[([^\]]+)\]\((?:[^()]|\([^()]*\))*\)/g, "$1")
      .replace(/[*_`~]+/g, "")
      .replace(/^[\s>#]+/gm, "")
      .replace(/\s+/g, " ")
      .trim()
  );

  if (preview.length <= PREVIEW_MAX_LENGTH) return preview;

  // The ellipsis occupies the last code unit of the budget. Slicing by code
  // unit can land between the two halves of a surrogate pair, which renders as
  // a lone replacement glyph, so an emoji straddling the cut is dropped whole.
  const maxContentLength = PREVIEW_MAX_LENGTH - 1;
  const lastCodeUnit = preview.charCodeAt(maxContentLength - 1);
  const endsOnHighSurrogate = lastCodeUnit >= 0xd800 && lastCodeUnit <= 0xdbff;
  const safeEnd = endsOnHighSurrogate ? maxContentLength - 1 : maxContentLength;

  return `${preview.slice(0, safeEnd).trimEnd()}…`;
}
