import { CJK_URL_TERMINATOR_REGEX } from './linkify'

/**
 * remark-cjk-autolink — trim CJK punctuation that remark-gfm's autolink literal
 * swallowed into a URL.
 *
 * Read-only renderers let remark-gfm autolink bare URLs in the parse tree, so an
 * adjacent markdown delimiter (e.g. a closing `**`) is never absorbed into the
 * href — that was MUL-4242. gfm's autolink literal, however, shares linkify-it's
 * CJK weakness: `https://x/a。后面` extends the link across the ideographic full
 * stop and the run after it. preprocessLinks used to trim this before parsing;
 * since URLs are no longer preprocessed in read-only mode, we re-apply the same
 * boundary on the parsed tree.
 *
 * Only autolink *literals* are touched — links whose href was derived from the
 * visible text (`https://…`, `www.…` → `http://…`, `a@b` → `mailto:a@b`).
 * Explicit `[label](url)` links keep whatever destination the author wrote, even
 * when it contains CJK punctuation.
 */

interface MdNode {
  type: string
  url?: string
  value?: string
  children?: MdNode[]
}

// The scheme prefix remark-gfm prepends to an autolink literal's href. Returns
// null when `url` was not derived from `text`, i.e. an explicit link — leave it.
function autolinkSchemePrefix(url: string, text: string): string | null {
  if (url === text) return ''
  for (const prefix of ['http://', 'https://', 'mailto:']) {
    if (url === prefix + text) return prefix
  }
  return null
}

// If `node` is an autolink literal whose text runs past a CJK terminator, return
// [trimmed link, trailing text] to splice in its place; otherwise null.
function splitCjkAutolink(node: MdNode): MdNode[] | null {
  if (node.type !== 'link' || !node.url || node.children?.length !== 1) return null
  const child = node.children[0]
  if (!child || child.type !== 'text' || typeof child.value !== 'string') return null

  const text = child.value
  if (autolinkSchemePrefix(node.url, text) === null) return null

  const cut = text.search(CJK_URL_TERMINATOR_REGEX)
  if (cut <= 0) return null // no CJK punctuation, or the text starts with it

  const rest = text.slice(cut)
  const trimmed: MdNode = {
    ...node,
    url: node.url.slice(0, node.url.length - rest.length),
    children: [{ type: 'text', value: text.slice(0, cut) }],
  }
  return [trimmed, { type: 'text', value: rest }]
}

function transform(node: MdNode): void {
  const children = node.children
  if (!children) return
  for (let i = 0; i < children.length; i++) {
    const split = splitCjkAutolink(children[i]!)
    if (split) {
      children.splice(i, 1, ...split)
      i += split.length - 1 // skip the appended trailing-text node
    } else {
      transform(children[i]!)
    }
  }
}

/** unified/remark plugin. Attach after remark-gfm. */
export function remarkCjkAutolink() {
  return (tree: unknown): void => {
    transform(tree as MdNode)
  }
}
