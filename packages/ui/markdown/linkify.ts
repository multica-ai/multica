import LinkifyIt from 'linkify-it'

/**
 * Linkify - URL and file path detection for markdown preprocessing
 *
 * Uses linkify-it (12M downloads/week) for battle-tested URL detection,
 * plus custom regex for local file paths.
 */

// Initialize linkify-it with default settings (fuzzy URLs, emails enabled)
const linkify = new LinkifyIt()

// File path regex - detects /path, ~/path, ./path with common extensions
// Matches paths that start with /, ~/, or ./ followed by path chars and a file extension
const FILE_PATH_REGEX =
  /(?:^|[\s([{<])((\/|~\/|\.\/)[\w\-./@]+\.(?:ts|tsx|js|jsx|mjs|cjs|md|json|yaml|yml|py|go|rs|css|scss|less|html|htm|txt|log|sh|bash|zsh|swift|kt|java|c|cpp|h|hpp|rb|php|xml|toml|ini|cfg|conf|env|sql|graphql|vue|svelte|astro|prisma|dockerfile|makefile|gitignore))(?=[\s)\]}.,;:!?>]|$)/gi

// CJK full-width punctuation that should terminate a URL.
// linkify-it only treats ASCII punctuation as URL boundaries, so in Chinese /
// Japanese text a URL followed by e.g. "。" gets the punctuation and every
// character up to the next whitespace swallowed into the href. We truncate the
// detected URL at the first occurrence of any of these characters. Character
// set mirrors the fix applied in mattermost/marked#22.
const CJK_URL_TERMINATOR_REGEX =
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
    // linkify-it may prepend a scheme (e.g. "http://" or "mailto:") to url
    // while leaving text as the raw substring. Preserve that prefix.
    const schemePrefix = match.url.slice(0, match.url.length - match.text.length)
    const matchUrl = truncate ? schemePrefix + matchText : match.url
    const matchEnd = truncate ? match.index + cjkIdx : match.lastIndex

    out.push({
      type: match.schema === 'mailto:' ? 'email' : 'url',
      text: matchText,
      url: matchUrl,
      start: match.index + offset,
      end: matchEnd + offset
    })

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
 * Detect all links (URLs, emails, file paths) in text
 */
export function detectLinks(text: string): DetectedLink[] {
  const links: DetectedLink[] = []

  // 1. Detect URLs and emails with linkify-it, applying CJK boundary handling.
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
 * Preprocess text to convert raw URLs and file paths into markdown links
 * Skips code blocks and already-linked content
 */
export function preprocessLinks(text: string): string {
  // Quick check - if no potential links, return early
  if (!linkify.pretest(text) && !/[~/.]\//.test(text)) {
    return text
  }

  const codeRanges = findCodeRanges(text)
  const links = detectLinks(text)

  if (links.length === 0) return text

  // Build result, converting raw links to markdown links
  let result = ''
  let lastIndex = 0

  for (const link of links) {
    // Skip if inside code block
    if (isInsideCode(link.start, codeRanges)) continue

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
