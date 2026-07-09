import { CJK_URL_TERMINATOR_REGEX, detectLinks } from './linkify'

/**
 * remark-cjk-autolink — trim CJK punctuation that remark-gfm's autolink literal
 * swallowed into a URL.
 *
 * Read-only renderers let remark-gfm autolink bare URLs in the parse tree, so an
 * adjacent markdown delimiter (e.g. a closing `**`) is never absorbed into the
 * href — that was MUL-4242. gfm's autolink literal, however, shares linkify-it's
 * CJK weakness: `https://x/a。后面` extends the link across the ideographic full
 * stop and the run after it, and `url1、url2` gets glued into one link.
 * preprocessLinks used to trim this before parsing; since URLs are no longer
 * preprocessed in read-only mode, we re-derive the real segments on the parsed
 * tree with the same CJK-aware detector, which rescans the tail so every URL in
 * a CJK-separated run stays linked.
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

// If `node` is an autolink literal whose text ran past a CJK terminator, rebuild
// it: gfm glued everything up to the next whitespace into one link, so re-derive
// the real segments and return the [link, text, link, …] sequence to splice in.
// Returns null (leave the node alone) when it is not an autolink literal or its
// boundary was already correct.
function splitCjkAutolink(node: MdNode): MdNode[] | null {
  if (node.type !== 'link' || !node.url || node.children?.length !== 1) return null
  const child = node.children[0]
  if (!child || child.type !== 'text' || typeof child.value !== 'string') return null

  const text = child.value
  if (autolinkSchemePrefix(node.url, text) === null) return null // not an autolink literal
  if (!CJK_URL_TERMINATOR_REGEX.test(text)) return null // gfm's boundary was already correct

  // detectLinks reuses collectLinkifyMatches, which truncates each URL at the
  // first CJK terminator AND rescans the tail — so multiple URLs separated by
  // CJK punctuation (`url1、url2`) each come back as their own segment instead
  // of only the first staying linked.
  const links = detectLinks(text, true).filter((link) => link.type !== 'file')
  if (links.length === 0) return null

  const out: MdNode[] = []
  let pos = 0
  for (const link of links) {
    if (link.start > pos) out.push({ type: 'text', value: text.slice(pos, link.start) })
    out.push({ ...node, url: link.url, children: [{ type: 'text', value: link.text }] })
    pos = link.end
  }
  if (pos < text.length) out.push({ type: 'text', value: text.slice(pos) })
  return out
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
