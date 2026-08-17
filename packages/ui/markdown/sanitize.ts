import { defaultUrlTransform } from 'react-markdown'
import { defaultSchema, type Options } from 'rehype-sanitize'

/**
 * Canonical sanitize schema + URL transform for every product-level Markdown
 * renderer (Chat via ui/markdown/Markdown.tsx, Issue/Comment via
 * views/editor/readonly-content.tsx).
 *
 * This is the ONLY copy. Both renderers previously carried a verbatim fork of
 * this schema, which had already drifted — the readonly fork whitelisted <mark>
 * for `==highlight==` and the chat fork did not. A security-relevant allow-list
 * maintained in two places means any future XSS fix has to land twice, and
 * missing one is a hole. Adding a surface means importing this, never copying
 * it (MUL-4922).
 *
 * The two gates below must agree on what a valid inline image is:
 * `protocols.src` + `attributes.img` (sanitize) and `markdownUrlTransform`
 * (react-markdown). Change them together.
 */
export const markdownSanitizeSchema: Options = {
  ...defaultSchema,
  // Allow <mark> (text highlight) — emitted by highlightToHtml from `==text==`.
  // It carries no attributes, so only the tag name needs whitelisting.
  tagNames: [...(defaultSchema.tagNames ?? []), 'mark'],
  protocols: {
    ...defaultSchema.protocols,
    href: [...(defaultSchema.protocols?.href ?? []), 'mention', 'slash'],
    // Permit inline data-URI images (QR codes, charts, base64 screenshots).
    // The scheme gate only allows `data:` through here; attributes.img below
    // narrows it to image/* so non-image data URIs are still rejected.
    src: [...(defaultSchema.protocols?.src ?? []), 'data'],
  },
  attributes: {
    ...defaultSchema.attributes,
    div: [
      ...(defaultSchema.attributes?.div ?? []),
      'dataType',
      'dataHref',
      'dataFilename',
    ],
    code: [
      ...(defaultSchema.attributes?.code ?? []),
      ['className', /^language-/],
      ['className', /^math-/],
      ['className', /^hljs/],
    ],
    img: [
      // Drop the default plain `src` entry so the value allow-list below is the
      // one findDefinition resolves — it returns the first match by name, so a
      // bare `src` string would otherwise shadow (and disable) the allow-list.
      ...(defaultSchema.attributes?.img ?? []).filter(
        (attr) => (typeof attr === 'string' ? attr : attr[0]) !== 'src',
      ),
      'alt',
      // Allow inline data:image/* URIs while leaving every other src form
      // (http/https/site-relative) exactly as before: the negative lookahead
      // keeps all non-data values, and data: is narrowed to images only.
      ['src', /^data:image\//i, /^(?!data:)/i],
    ],
  },
}

/**
 * Allows Multica internal protocols and inline images through react-markdown's
 * URL gate while keeping default security for everything else.
 */
export function markdownUrlTransform(url: string): string {
  if (url.startsWith('mention://')) return url
  if (url.startsWith('slash://skill/')) return url
  // defaultUrlTransform strips every data: URL to '', which would blank the src
  // even after rehype-sanitize keeps it. Kept in sync with the image/* narrowing
  // in markdownSanitizeSchema so both gates agree on what a valid inline image is.
  if (/^data:image\//i.test(url)) return url
  return defaultUrlTransform(url)
}
