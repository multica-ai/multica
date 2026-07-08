import LinkifyIt from 'linkify-it'

/**
 * Linkify - URL and file path detection for markdown preprocessing
 *
 * Uses linkify-it (12M downloads/week) for battle-tested URL detection,
 * plus custom regex for local file paths.
 */

// Initialize linkify-it with default settings (fuzzy URLs, emails enabled)
const linkify = new LinkifyIt()

// Common source/config file extensions. Shared between the file-path detector
// and the bare-filename guard below so the two never drift.
const FILE_EXTENSIONS =
  'ts|tsx|js|jsx|mjs|cjs|md|json|yaml|yml|py|go|rs|css|scss|less|html|htm|txt|log|sh|bash|zsh|swift|kt|java|c|cpp|h|hpp|rb|php|xml|toml|ini|cfg|conf|env|sql|graphql|vue|svelte|astro|prisma|dockerfile|makefile|gitignore'

// File path regex - detects /path, ~/path, ./path with common extensions
// Matches paths that start with /, ~/, or ./ followed by path chars and a file extension
const FILE_PATH_REGEX = new RegExp(
  `(?:^|[\\s([{<])((\\/|~\\/|\\.\\/)[\\w\\-./@]+\\.(?:${FILE_EXTENSIONS}))(?=[\\s)\\]}.,;:!?>]|$)`,
  'gi'
)

// A bare filename token like "plan.md" or "vite.config.ts": a single path
// segment ending in a known file extension, with no slash, scheme, or port.
// linkify-it fuzzy-matches these as domains because several of the extensions
// (md, sh, rs, py, …) are also valid TLDs. We use this to stop bare
// filenames from being auto-linked to dead external sites like https://plan.md.
const BARE_FILENAME_REGEX = new RegExp(`^[\\w.-]+\\.(?:${FILE_EXTENSIONS})$`, 'i')

// CJK full-width punctuation that should terminate a URL.
// linkify-it only treats ASCII punctuation as URL boundaries, so in Chinese /
// Japanese text a URL followed by e.g. "。" gets the punctuation and every
// character up to the next whitespace swallowed into the href. We truncate the
// detected URL at the first occurrence of any of these characters. Character
// set mirrors the fix applied in mattermost/marked#22.
//
// Exported so the read-only render pipeline can apply the same boundary to URLs
// that remark-gfm autolinks in the parse tree (see remark-cjk-autolink.ts).
export const CJK_URL_TERMINATOR_REGEX =
  /[！-／：-＠［-｀｛-～、。「-】]/

interface DetectedLink {
  type: 'url' | 'email' | 'file'
  text: string
  url: string
  start: number
  end: number
}

interface CodeRange {
  start: number
  end: number
}

/**
 * Find all code block and inline code ranges in text
 * These ranges should be excluded from link detection
 */
function findCodeRanges(text: string): CodeRange[] {
  const ranges: CodeRange[] = []

  // Find fenced code blocks (```...```)
  const fencedRegex = /```[\s\S]*?```/g
  let match
  while ((match = fencedRegex.exec(text)) !== null) {
    ranges.push({ start: match.index, end: match.index + match[0].length })
  }

  // Find display math blocks ($$...$$)
  const displayMathRegex = /\$\$[\s\S]*?\$\$/g
  while ((match = displayMathRegex.exec(text)) !== null) {
    const pos = match.index
    const insideOther = ranges.some((r) => pos >= r.start && pos < r.end)
    if (!insideOther) {
      ranges.push({ start: pos, end: pos + match[0].length })
    }
  }

  // Find inline math ($...$)
  const inlineMathRegex = /(?<!\$)\$(?!\$)([^$\n]+)\$(?!\$)/g
  while ((match = inlineMathRegex.exec(text)) !== null) {
    const pos = match.index
    const insideOther = ranges.some((r) => pos >= r.start && pos < r.end)
    if (!insideOther) {
      ranges.push({ start: pos, end: pos + match[0].length })
    }
  }

  // Find inline code (`...`)
  // But skip escaped backticks and code inside fenced blocks
  const inlineRegex = /(?<!`)`(?!`)([^`\n]+)`(?!`)/g
  while ((match = inlineRegex.exec(text)) !== null) {
    const pos = match.index
    // Check if this is inside a fenced block or math block
    const insideOther = ranges.some((r) => pos >= r.start && pos < r.end)
    if (!insideOther) {
      ranges.push({ start: pos, end: pos + match[0].length })
    }
  }

  return ranges
}

/**
 * Check if a position is inside any code range
 */
function isInsideCode(pos: number, ranges: CodeRange[]): boolean {
  return ranges.some((r) => pos >= r.start && pos < r.end)
}

function isEscaped(text: string, index: number): boolean {
  let slashCount = 0
  for (let i = index - 1; i >= 0 && text[i] === '\\'; i--) {
    slashCount++
  }
  return slashCount % 2 === 1
}

function findMatchingBracket(text: string, openIndex: number): number {
  let depth = 0

  for (let i = openIndex; i < text.length; i++) {
    if (isEscaped(text, i)) continue

    const char = text[i]
    if (char === '[') {
      depth++
    } else if (char === ']') {
      depth--
      if (depth === 0) return i
    }
  }

  return -1
}

function findInlineLinkEnd(text: string, openParenIndex: number): number {
  let depth = 0

  for (let i = openParenIndex; i < text.length; i++) {
    if (isEscaped(text, i)) continue

    const char = text[i]
    if (char === '(') {
      depth++
    } else if (char === ')') {
      depth--
      if (depth === 0) return i + 1
    }
  }

  return -1
}

/**
 * Find existing markdown link/image spans so auto-linkification does not create
 * nested links inside their labels or destinations.
 */
function findMarkdownLinkRanges(text: string): CodeRange[] {
  const ranges: CodeRange[] = []

  for (let i = 0; i < text.length; i++) {
    if (text[i] !== '[' || isEscaped(text, i)) continue
    if (ranges.some((r) => i >= r.start && i < r.end)) continue

    const labelEnd = findMatchingBracket(text, i)
    if (labelEnd === -1) continue

    const start = i > 0 && text[i - 1] === '!' && !isEscaped(text, i - 1) ? i - 1 : i
    const nextChar = text[labelEnd + 1]

    if (nextChar === '(') {
      const end = findInlineLinkEnd(text, labelEnd + 1)
      if (end !== -1) {
        ranges.push({ start, end })
        i = end - 1
      }
      continue
    }

    if (nextChar === '[') {
      const referenceEnd = findMatchingBracket(text, labelEnd + 1)
      if (referenceEnd !== -1) {
        ranges.push({ start, end: referenceEnd + 1 })
        i = referenceEnd
      }
    }
  }

  return ranges
}

/**
 * Check if a link at given position is already a markdown link
 * Looks for patterns like [text](url) or [text][ref]
 */
function isAlreadyLinked(text: string, linkStart: number, linkEnd: number): boolean {
  // Check if preceded by ]( which indicates we're inside a markdown link href
  // Pattern: [text](URL) - we're checking if URL is our link
  const before = text.slice(Math.max(0, linkStart - 2), linkStart)
  if (before.endsWith('](')) return true

  // Check if preceded by ][ for reference links
  if (before.endsWith('][')) return true

  // Check if the link text is wrapped in []
  // Pattern: [URL](href) - URL is being used as link text
  const charBefore = text[linkStart - 1]
  const charAfter = text[linkEnd]
  if (charBefore === '[' && charAfter === ']') return true

  return false
}

/**
 * Check if ranges overlap
 */
function rangesOverlap(
  a: { start: number; end: number },
  b: { start: number; end: number }
): boolean {
  return a.start < b.end && b.start < a.end
}

/**
 * Run linkify-it on `text` and push normalized link records into `out`,
 * shifted by `offset`. When linkify-it merges multiple URLs into one match
 * because they are separated only by CJK punctuation (which it doesn't treat
 * as a URL boundary), we truncate at that punctuation and re-scan the tail.
 */
function collectLinkifyMatches(text: string, offset: number, out: DetectedLink[]): void {
  const matches = linkify.match(text)
  if (!matches) return

  for (const match of matches) {
    const cjkIdx = match.text.search(CJK_URL_TERMINATOR_REGEX)
    if (cjkIdx === 0) continue // match starts with CJK punct — skip

    const truncate = cjkIdx > 0
    const matchText = truncate ? match.text.slice(0, cjkIdx) : match.text
    const matchEnd = truncate ? match.index + cjkIdx : match.lastIndex

    // Bare filenames such as "plan.md" or "README.md" are fuzzy-matched as
    // domains because their extension is also a valid TLD. They are file
    // references, not URLs — leave them as plain text rather than link to a
    // dead external site. Only schemeless (fuzzy) matches are suppressed; an
    // explicit "https://plan.md" the author typed is still honored.
    if (!(match.schema === '' && BARE_FILENAME_REGEX.test(matchText))) {
      // linkify-it may prepend a scheme (e.g. "http://" or "mailto:") to url
      // while leaving text as the raw substring. Preserve that prefix.
      const schemePrefix = match.url.slice(0, match.url.length - match.text.length)
      const matchUrl = truncate ? schemePrefix + matchText : match.url

      out.push({
        type: match.schema === 'mailto:' ? 'email' : 'url',
        text: matchText,
        url: matchUrl,
        start: match.index + offset,
        end: matchEnd + offset
      })
    }

    if (truncate) {
      // Rescan the tail after the CJK punct — linkify-it had greedily swallowed
      // it, so any additional URLs after the punct were never emitted.
      const tailStart = matchEnd + 1
      collectLinkifyMatches(text.slice(tailStart), offset + tailStart, out)
      return
    }
  }
}

/**
 * Detect all links (URLs, emails, file paths) in text.
 *
 * `includeUrls` gates the URL/email pass. Read-only markdown renderers pass
 * `false` and let remark-gfm autolink URLs in the parse tree instead, which
 * cannot corrupt adjacent markdown (e.g. a trailing `**`). File paths, which
 * remark-gfm does not linkify, are always detected. See preprocessLinks.
 */
export function detectLinks(text: string, includeUrls = true): DetectedLink[] {
  const links: DetectedLink[] = []

  // 1. Detect URLs and emails with linkify-it, applying CJK boundary handling.
  if (includeUrls) collectLinkifyMatches(text, 0, links)

  // 2. Detect file paths with custom regex
  // Reset regex state
  FILE_PATH_REGEX.lastIndex = 0
  let fileMatch
  while ((fileMatch = FILE_PATH_REGEX.exec(text)) !== null) {
    const path = fileMatch[1]
    if (!path) continue // Skip if no capture group

    // Calculate actual start position (after any leading whitespace/punctuation)
    const fullMatch = fileMatch[0]
    const pathOffset = fullMatch.indexOf(path)
    const start = fileMatch.index + pathOffset

    // Check for overlaps with URL matches (URLs take precedence)
    const pathRange = { start, end: start + path.length }
    const overlapsUrl = links.some((link) => rangesOverlap(pathRange, link))
    if (overlapsUrl) continue

    links.push({
      type: 'file',
      text: path,
      url: path, // File paths are passed as-is to onFileClick handler
      start,
      end: start + path.length
    })
  }

  // Sort by position
  return links.sort((a, b) => a.start - b.start)
}

/**
 * Preprocess text to convert raw URLs and file paths into markdown links.
 * Skips code blocks and already-linked content.
 *
 * `opts.urls` (default `true`) controls the URL/email pass. The Tiptap editor
 * keeps it on because @tiptap/markdown does not autolink bare URLs. Read-only
 * react-markdown renderers pass `false`: they let remark-gfm autolink URLs in
 * the parse tree, where a bare URL can no longer swallow an adjacent markdown
 * delimiter — the string pass here can't tell `https://x**` (URL + bold close)
 * from a URL that legitimately ends in `*`, so it corrupted both (MUL-4242).
 * File paths (which remark-gfm never linkifies) are converted in both modes.
 */
export function preprocessLinks(text: string, opts?: { urls?: boolean }): string {
  const includeUrls = opts?.urls ?? true

  // Quick check - if no potential links, return early
  if (!linkify.pretest(text) && !/[~/.]\//.test(text)) {
    return text
  }

  const codeRanges = findCodeRanges(text)
  const markdownLinkRanges = findMarkdownLinkRanges(text)
  const links = detectLinks(text, includeUrls)

  if (links.length === 0) return text

  // Build result, converting raw links to markdown links
  let result = ''
  let lastIndex = 0

  for (const link of links) {
    // Skip if inside code block
    if (isInsideCode(link.start, codeRanges)) continue

    // Skip if this match is inside an existing markdown link or image.
    if (markdownLinkRanges.some((range) => rangesOverlap(link, range))) continue

    // Skip if already a markdown link
    if (isAlreadyLinked(text, link.start, link.end)) continue

    // Add text before this link
    result += text.slice(lastIndex, link.start)

    // Convert to markdown link
    result += `[${link.text}](${link.url})`

    lastIndex = link.end
  }

  // Add remaining text
  result += text.slice(lastIndex)

  return result
}

/**
 * Test if text contains any detectable links
 * Useful for optimization - skip preprocessing if no links present
 */
export function hasLinks(text: string): boolean {
  return linkify.pretest(text) || /[~/.]\/[\w]/.test(text)
}
