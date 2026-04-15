import * as React from 'react'
import { Markdown, type RenderMode } from './Markdown'

export interface StreamingMarkdownProps {
  content: string
  isStreaming: boolean
  mode?: RenderMode
  className?: string
  onUrlClick?: (url: string) => void
  onFileClick?: (path: string) => void
  renderMention?: (props: { type: string; id: string }) => React.ReactNode
  cdnDomain?: string
}

interface Block {
  content: string
  isCodeBlock: boolean
}

/**
 * djb2 hash (XOR variant) by Daniel J. Bernstein.
 * Used to generate stable React keys for completed content blocks.
 *
 * - 5381: empirically chosen initial value that produces fewer collisions
 * - (hash << 5) + hash: equivalent to hash * 33
 * - ^ charCode: XOR variant, favored by Bernstein over additive version
 * - >>> 0: convert to unsigned 32-bit integer
 *
 * Not cryptographic — just fast with good distribution for short strings.
 * @see http://www.cse.yorku.ca/~oz/hash.html
 */
function simpleHash(str: string): string {
  let hash = 5381
  for (let i = 0; i < str.length; i++) {
    hash = ((hash << 5) + hash) ^ str.charCodeAt(i)
  }
  return (hash >>> 0).toString(36)
}

/**
 * Split content into blocks (paragraphs and code blocks)
 *
 * Block boundaries:
 * - Double newlines (paragraph separators)
 * - Code fences (```)
 *
 * This is intentionally simple - just string scanning, no regex per line.
 */
function splitIntoBlocks(content: string): Block[] {
  const blocks: Block[] = []
  const lines = content.split('\n')
  let currentBlock = ''
  let inCodeBlock = false
  let inMathBlock = false

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] ?? ''

    // Check for code fence (``` at start of line, optionally followed by language)
    if (line.startsWith('```')) {
      if (!inCodeBlock) {
        // Starting a code block - flush current paragraph first
        if (currentBlock.trim()) {
          blocks.push({ content: currentBlock.trim(), isCodeBlock: false })
          currentBlock = ''
        }
        inCodeBlock = true
        currentBlock = line + '\n'
      } else {
        // Ending a code block
        currentBlock += line
        blocks.push({ content: currentBlock, isCodeBlock: true })
        currentBlock = ''
        inCodeBlock = false
      }
    } else if (inCodeBlock) {
      // Inside code block - append line
      currentBlock += line + '\n'
    // Check for display math fence ($$)
    } else if (line.trim() === '$$') {
      if (!inMathBlock) {
        // Starting a math block - flush current paragraph first
        if (currentBlock.trim()) {
          blocks.push({ content: currentBlock.trim(), isCodeBlock: false })
          currentBlock = ''
        }
        inMathBlock = true
        currentBlock = line + '\n'
      } else {
        // Ending a math block
        currentBlock += line
        blocks.push({ content: currentBlock, isCodeBlock: false })
        currentBlock = ''
        inMathBlock = false
      }
    } else if (inMathBlock) {
      // Inside math block - append line (don't split on blank lines)
      currentBlock += line + '\n'
    } else if (line === '') {
      // Empty line outside code block = paragraph boundary
      if (currentBlock.trim()) {
        blocks.push({ content: currentBlock.trim(), isCodeBlock: false })
        currentBlock = ''
      }
    } else {
      // Regular text line
      if (currentBlock) {
        currentBlock += '\n' + line
      } else {
        currentBlock = line
      }
    }
  }

  // Flush remaining content
  if (currentBlock) {
    blocks.push({
      content: inCodeBlock || inMathBlock ? currentBlock : currentBlock.trim(),
      isCodeBlock: inCodeBlock
    })
  }

  return blocks
}

/**
 * Memoized block component
 *
 * Only re-renders if content or mode changes.
 * The key is assigned by the parent based on content hash,
 * so identical content won't even attempt to render.
 */
const MemoizedBlock = React.memo(
  function Block({
    content,
    mode,
    className,
    onUrlClick,
    onFileClick,
    renderMention,
    cdnDomain
  }: {
    content: string
    mode: RenderMode
    className?: string
    onUrlClick?: (url: string) => void
    onFileClick?: (path: string) => void
    renderMention?: (props: { type: string; id: string }) => React.ReactNode
    cdnDomain?: string
  }) {
    return (
      <Markdown mode={mode} className={className} onUrlClick={onUrlClick} onFileClick={onFileClick} renderMention={renderMention} cdnDomain={cdnDomain}>
        {content}
      </Markdown>
    )
  },
  (prev, next) => {
    // Only re-render if content actually changed
    return prev.content === next.content && prev.mode === next.mode && prev.className === next.className
  }
)
MemoizedBlock.displayName = 'MemoizedBlock'

/**
 * StreamingMarkdown - Optimized markdown renderer for streaming content
 *
 * Splits content into blocks (paragraphs, code blocks) and memoizes each block
 * independently. Only the last (active) block re-renders during streaming.
 *
 * Key insight: Completed blocks get a content-hash as their React key.
 * Same content = same key = React skips re-render entirely.
 *
 * @example
 * Content: "Hello\n\n```js\ncode\n```\n\nMore..."
 *
 * Block 1: "Hello"           -> key="block-abc123" -> memoized
 * Block 2: "```js\ncode\n```" -> key="block-xyz789" -> memoized
 * Block 3: "More..."         -> key="active-2"     -> re-renders
 */
export function StreamingMarkdown({
  content,
  isStreaming,
  mode = 'minimal',
  className,
  onUrlClick,
  onFileClick,
  renderMention,
  cdnDomain
}: StreamingMarkdownProps): React.JSX.Element {
  // Split into blocks - memoized to avoid recomputation
  // Must be called unconditionally to satisfy Rules of Hooks
  const blocks = React.useMemo(
    () => (isStreaming ? splitIntoBlocks(content) : []),
    [content, isStreaming]
  )

  // Not streaming - use simple Markdown (no block splitting needed)
  if (!isStreaming) {
    return (
      <Markdown mode={mode} className={className} onUrlClick={onUrlClick} onFileClick={onFileClick} renderMention={renderMention} cdnDomain={cdnDomain}>
        {content}
      </Markdown>
    )
  }

  // Empty content - return null, let parent handle loading indicator
  if (blocks.length === 0) {
    return <></>
  }

  return (
    <>
      {blocks.map((block, i) => {
        const isLastBlock = i === blocks.length - 1

        // Complete blocks use content hash as key -> stable identity -> memoized
        // Last block uses "active" prefix -> always re-renders on content change
        const key = isLastBlock ? `active-${i}` : `block-${i}-${simpleHash(block.content)}`

        return (
          <MemoizedBlock
            key={key}
            content={block.content}
            mode={mode}
            className={className}
            onUrlClick={onUrlClick}
            onFileClick={onFileClick}
            renderMention={renderMention}
            cdnDomain={cdnDomain}
          />
        )
      })}
    </>
  )
}
