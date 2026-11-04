import LinkifyIt from 'linkify-it'

/**
 * Linkify - URL and file path detection for markdown preprocessing
 *
 * Uses linkify-it (12M downloads/week) for battle-tested URL detection,
 * plus custom regex for local file paths.
 */

// Keep fuzzy detection enabled so linkify-it can recognize `www.` URLs.
// collectLinkifyMatches applies the stricter shared product policy below.
const linkify = new LinkifyIt()

// Common source/config file extensions used by the file-path detector.
const FILE_EXTENSIONS =
  'ts|tsx|js|jsx|mjs|cjs|md|json|yaml|yml|py|go|rs|css|scss|less|html|htm|txt|log|sh|bash|zsh|swift|kt|java|c|cpp|h|hpp|rb|php|xml|toml|ini|cfg|conf|env|sql|graphql|vue|svelte|astro|prisma|dockerfile|makefile|gitignore'

// File path regex - detects /path, ~/path, ./path with common extensions
// Matches paths that start with /, ~/, or ./ followed by path chars and a file extension
const FILE_PATH_REGEX = new RegExp(
  `(?:^|[\\s([{<])((\\/|~\\/|\\.\\/)[\\w\\-./@]+\\.(?:${FILE_EXTENSIONS}))(?=[\\s)\\]}.,;:!?>]|$)`,
  'gi'
)

// CJK full-width punctuation that should terminate a URL.
// linkify-it only treats ASCII punctuation as URL boundaries, so in Chinese /
// Japanese text a URL followed by e.g. "。" gets the punctuation and every
// character up to the next whitespace swallowed into the href. We truncate the
// detected URL at the first occurrence of any of these characters. Character
// set mirrors the fix applied in mattermost/marked#22.
const CJK_URL_TERMINATOR_REGEX =
  /[！-／：-＠［-｀｛-～、。「-】]/

// Markdown inline-formatting delimiters that linkify-it counts as URL characters
// but which, at the very end of a bare URL, are almost always the author closing
// an emphasis / strikethrough span (`**url**`, `*url*`, `~~url~~`) rather than
// part of the URL. We drop a trailing run of them so the surrounding markdown
// still parses — mirroring GFM's own autolink trailing-punctuation trim. A URL
// that genuinely ends in `*` / `~` loses that character from the link, exactly
// as it does on GitHub (MUL-4242).
const TRAILING_MD_DELIMITER = /[*~]+$/

interface DetectedLink {
  type: 'url' | 'email' | 'file'
  text: string
  url: string
  start: number
  end: number
}

export interface CodeRange {
  start: number
  end: number
}

/**
 * Product policy for automatic external links.
 *
 * Detectors call this only after validating a candidate. This function decides
 * which validated text forms are safe to turn into a link without an explicit
 * user action: HTTP(S) URLs, `www.` URLs, and email addresses. In particular,
 * all other scheme-less domains stay plain text, which also prevents filenames
 * such as `plan.md` from being mistaken for websites.
 *
 * Keep this predicate shared by the editor and markdown renderer so typing,
 * paste, and read-only display cannot drift again.
 */
export function shouldAutoLink(value: string): boolean {
  return (
    /^https?:\/\//i.test(value) ||
    /^www\./i.test(value) ||
    /^[^\s@/:]+@[^\s@/:]+\.[^\s@/:]+$/.test(value)
  )
}

/**
 * A block container written before a fence on its opening line. `indent` is
 * the list item's content column, measured from where its parent's content
 * starts.
 */
type FenceContainer = { kind: 'quote' } | { kind: 'list'; indent: number }

interface FenceRun {
  marker: string
  length: number
  suffix: string
}

interface FenceLine extends FenceRun {
  containers: FenceContainer[]
}

function isSpace(char: string | undefined): boolean {
  return char === ' ' || char === '\t'
}

function isDigit(char: string | undefined): boolean {
  return char !== undefined && char >= '0' && char <= '9'
}

function parseFenceRun(line: string, cursor: number): FenceRun | undefined {
  while (isSpace(line[cursor])) cursor += 1
  const marker = line[cursor]
  if (marker !== '`' && marker !== '~') return undefined
  const start = cursor
  while (line[cursor] === marker) cursor += 1
  const length = cursor - start
  if (length < 3) return undefined
  return { marker, length, suffix: line.slice(cursor) }
}

function parseFenceLine(line: string): FenceLine | undefined {
  const containers: FenceContainer[] = []
  let cursor = 0
  let contentStart = 0

  // Consume each space and container marker once; ambiguous whitespace must
  // never be redistributed between nested quantifiers on a non-fence line.
  while (cursor < line.length) {
    while (isSpace(line[cursor])) cursor += 1
    const char = line[cursor]
    if (char === '>') {
      cursor += 1
      if (isSpace(line[cursor])) cursor += 1
      containers.push({ kind: 'quote' })
    } else if ((char === '-' || char === '+' || char === '*') && isSpace(line[cursor + 1])) {
      cursor += 1
      while (isSpace(line[cursor])) cursor += 1
      containers.push({ kind: 'list', indent: cursor - contentStart })
    } else if (isDigit(char)) {
      while (isDigit(line[cursor])) cursor += 1
      if ((line[cursor] !== '.' && line[cursor] !== ')') || !isSpace(line[cursor + 1])) {
        return undefined
      }
      cursor += 1
      while (isSpace(line[cursor])) cursor += 1
      containers.push({ kind: 'list', indent: cursor - contentStart })
    } else {
      break
    }
    contentStart = cursor
  }

  const run = parseFenceRun(line, cursor)
  return run && { ...run, containers }
}

/**
 * Consume the prefixes of the containers that hold an open fence. Returns the
 * offset where the fence content starts, or -1 when this line ends one of the
 * containers. Fenced code is never a lazy continuation, so a quote ends at the
 * first line without its `>` and a list item at the first non-blank line that
 * does not reach its content column.
 */
function consumeContainers(line: string, containers: FenceContainer[]): number {
  let cursor = 0
  for (const container of containers) {
    if (container.kind === 'quote') {
      while (isSpace(line[cursor])) cursor += 1
      if (line[cursor] !== '>') return -1
      cursor += 1
      if (isSpace(line[cursor])) cursor += 1
    } else {
      const start = cursor
      while (cursor - start < container.indent && isSpace(line[cursor])) cursor += 1
      if (cursor - start < container.indent && line.slice(cursor).trim() !== '') return -1
    }
  }
  return cursor
}

/** Find complete fenced blocks, retaining the opening marker and its length. */
export function findFencedCodeRanges(text: string): CodeRange[] {
  const ranges: CodeRange[] = []
  let opening: ({ start: number } & FenceLine) | undefined
  let offset = 0

  for (const line of text.split('\n')) {
    if (opening) {
      const content = consumeContainers(line, opening.containers)
      if (content === -1) {
        // The container closed the fence; this line may open another one.
        ranges.push({ start: opening.start, end: offset - 1 })
        opening = undefined
      } else {
        const fence = parseFenceRun(line, content)
        if (
          fence &&
          fence.marker === opening.marker &&
          fence.length >= opening.length &&
          fence.suffix.trim() === ''
        ) {
          ranges.push({ start: opening.start, end: offset + line.length })
          opening = undefined
        }
        offset += line.length + 1
        continue
      }
    }

    const fence = parseFenceLine(line)
    // Backtick fence info strings cannot themselves contain backticks.
    if (fence && (fence.marker === '~' || !fence.suffix.includes('`'))) {
      opening = { start: offset, ...fence }
    }
    offset += line.length + 1
  }

  // CommonMark keeps an unclosed fence literal through the end of its container.
  if (opening) ranges.push({ start: opening.start, end: text.length })
  return ranges
}

/**
 * Find all code block and inline code ranges in text
 * These ranges should be excluded from link detection
 */
export function findCodeRanges(text: string): CodeRange[] {
  const fencedRanges = findFencedCodeRanges(text)
  const ranges = [...fencedRanges]
  let match

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

  // Find inline code spans with matching backtick runs.
  // Ignore spans inside fenced code or math blocks.
  // Inline code can span soft line breaks, but not a blank paragraph boundary
  // (LF or CRLF).
  const inlineRegex = /(?<!`)(`+)(?!`)((?:[^\n]|\n(?![ \t]*\r?\n))*?)(?<!`)\1(?!`)/g
  while ((match = inlineRegex.exec(text)) !== null) {
    const pos = match.index
    const end = pos + match[0].length
    // Never let a candidate consume an opener after a fenced block's closer.
    const crossedFence = fencedRanges.find((r) => pos < r.end && end > r.start)
    if (crossedFence) {
      inlineRegex.lastIndex = crossedFence.end
      continue
    }
    const insideOther = ranges.find((r) => pos >= r.start && pos < r.end)
    if (insideOther) {
      inlineRegex.lastIndex = insideOther.end
      continue
    }
    ranges.push({ start: pos, end })
  }

  return ranges
}

/**
 * Check if a position is inside any code range
 */
export function isInsideCode(pos: number, ranges: CodeRange[]): boolean {
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
export function findMarkdownLinkRanges(text: string): CodeRange[] {
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
export function rangesOverlap(
  a: { start: number; end: number },
  b: { start: number; end: number }
): boolean {
  return a.start < b.end && b.start < a.end
}

/**
 * Run linkify-it on `text` and push normalized link records into `out`,
 * shifted by `offset`. Two boundary corrections linkify-it doesn't make itself:
 * - CJK punctuation ends a URL (linkify-it only breaks on ASCII); when it merged
 *   several URLs across CJK punctuation we truncate and re-scan the tail; and
 * - a trailing run of markdown delimiters (`*`, `~`) is dropped from the URL so
 *   `**url**` keeps its closing emphasis marker outside the link.
 */
function collectLinkifyMatches(text: string, offset: number, out: DetectedLink[]): void {
  const matches = linkify.match(text)
  if (!matches) return

  for (const match of matches) {
    const cjkIdx = match.text.search(CJK_URL_TERMINATOR_REGEX)
    if (cjkIdx === 0) continue // match starts with CJK punct — skip

    const truncate = cjkIdx > 0
    // URL text up to the first CJK terminator, then minus a trailing markdown
    // delimiter run (e.g. the closing `**` of `**url**`). matchText stays a
    // prefix of match.text, so the link end is just its length past match.index.
    const matchText = (truncate ? match.text.slice(0, cjkIdx) : match.text).replace(
      TRAILING_MD_DELIMITER,
      ''
    )

    if (matchText.length > 0 && shouldAutoLink(matchText)) {
      // When we trimmed the raw match, rebuild the href from the scheme prefix
      // linkify-it added (e.g. "http://" / "mailto:") plus the trimmed text;
      // otherwise keep linkify-it's normalized url as-is.
      const trimmed = matchText.length !== match.text.length
      const schemePrefix = match.url.slice(0, match.url.length - match.text.length)
      const matchUrl =
        match.schema === '' && /^www\./i.test(matchText)
          ? `https://${matchText}`
          : trimmed
            ? schemePrefix + matchText
            : match.url

      out.push({
        type: match.schema === 'mailto:' ? 'email' : 'url',
        text: matchText,
        url: matchUrl,
        start: match.index + offset,
        end: match.index + matchText.length + offset
      })
    }

    if (truncate) {
      // Rescan the tail after the CJK punct — linkify-it had greedily swallowed
      // it, so any additional URLs after the punct were never emitted.
      const tailStart = match.index + cjkIdx + 1
      collectLinkifyMatches(text.slice(tailStart), offset + tailStart, out)
      return
    }
  }
}

/**
 * Detect all links (URLs, emails, file paths) in text.
 */
export function detectLinks(text: string): DetectedLink[] {
  const links: DetectedLink[] = []

  // 1. Detect URLs and emails with linkify-it, applying boundary corrections.
  collectLinkifyMatches(text, 0, links)

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
 * Shared by the Tiptap editor and the read-only react-markdown renderers, so
 * both surfaces linkify identically. Trailing markdown delimiters are excluded
 * from the URL (see collectLinkifyMatches), which keeps `**url**` bold and its
 * link clean (MUL-4242).
 */
export function preprocessLinks(text: string): string {
  // Quick check - if no potential links, return early
  if (!linkify.pretest(text) && !/[~/.]\//.test(text)) {
    return text
  }

  const codeRanges = findCodeRanges(text)
  const markdownLinkRanges = findMarkdownLinkRanges(text)
  const links = detectLinks(text)

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
