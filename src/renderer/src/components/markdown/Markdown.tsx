import * as React from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import rehypeRaw from 'rehype-raw'
import remarkGfm from 'remark-gfm'
import { cn } from '@/lib/utils'
import { CodeBlock, InlineCode } from './CodeBlock'
import { preprocessLinks } from './linkify'

/**
 * Render modes for markdown content:
 *
 * - 'terminal': Raw output with minimal formatting, control chars visible
 *   Best for: Debug output, raw logs, when you want to see exactly what's there
 *
 * - 'minimal': Clean rendering with syntax highlighting but no extra chrome
 *   Best for: Chat messages, inline content, when you want readability without clutter
 *
 * - 'full': Rich rendering with beautiful tables, styled code blocks, proper typography
 *   Best for: Documentation, long-form content, when presentation matters
 */
export type RenderMode = 'terminal' | 'minimal' | 'full'

export interface MarkdownProps {
  children: string
  /**
   * Render mode controlling formatting level
   * @default 'minimal'
   */
  mode?: RenderMode
  className?: string
  /**
   * Message ID for memoization (optional)
   * When provided, memoizes parsed blocks to avoid re-parsing during streaming
   */
  id?: string
  /**
   * Callback when a URL is clicked
   */
  onUrlClick?: (url: string) => void
  /**
   * Callback when a file path is clicked
   */
  onFileClick?: (path: string) => void
}

// File path detection regex - matches paths starting with /, ~/, or ./
const FILE_PATH_REGEX =
  /^(?:\/|~\/|\.\/)[\w\-./@]+\.(?:ts|tsx|js|jsx|mjs|cjs|md|json|yaml|yml|py|go|rs|css|scss|less|html|htm|txt|log|sh|bash|zsh|swift|kt|java|c|cpp|h|hpp|rb|php|xml|toml|ini|cfg|conf|env|sql|graphql|vue|svelte|astro|prisma)$/i

/**
 * Create custom components based on render mode
 */
function createComponents(
  mode: RenderMode,
  onUrlClick?: (url: string) => void,
  onFileClick?: (path: string) => void
): Partial<Components> {
  const baseComponents: Partial<Components> = {
    // Links: Make clickable with callbacks
    a: ({ href, children }) => {
      const handleClick = (e: React.MouseEvent): void => {
        e.preventDefault()
        if (href) {
          // Check if it's a file path
          if (FILE_PATH_REGEX.test(href) && onFileClick) {
            onFileClick(href)
          } else if (onUrlClick) {
            onUrlClick(href)
          } else {
            // Default: open in new window
            window.open(href, '_blank', 'noopener,noreferrer')
          }
        }
      }

      return (
        <a
          href={href}
          onClick={handleClick}
          className="text-primary hover:underline cursor-pointer"
        >
          {children}
        </a>
      )
    }
  }

  // Terminal mode: minimal formatting
  if (mode === 'terminal') {
    return {
      ...baseComponents,
      // No special code handling - just monospace
      code: ({ children }) => <code className="font-mono">{children}</code>,
      pre: ({ children }) => <pre className="font-mono whitespace-pre-wrap my-2">{children}</pre>,
      // Minimal paragraph spacing
      p: ({ children }) => <p className="my-1">{children}</p>,
      // Simple lists
      ul: ({ children }) => <ul className="list-disc list-inside my-1">{children}</ul>,
      ol: ({ children }) => <ol className="list-decimal list-inside my-1">{children}</ol>,
      li: ({ children }) => <li className="my-0.5">{children}</li>,
      // Plain tables
      table: ({ children }) => <table className="my-2 font-mono text-sm">{children}</table>,
      th: ({ children }) => <th className="text-left pr-4">{children}</th>,
      td: ({ children }) => <td className="pr-4">{children}</td>
    }
  }

  // Minimal mode: clean with syntax highlighting
  if (mode === 'minimal') {
    return {
      ...baseComponents,
      // Inline code
      code: ({ className, children, ...props }) => {
        const match = /language-(\w+)/.exec(className || '')
        const isBlock =
          'node' in props && props.node?.position?.start.line !== props.node?.position?.end.line

        // Block code - use CodeBlock with full mode
        if (match || isBlock) {
          const code = String(children).replace(/\n$/, '')
          return <CodeBlock code={code} language={match?.[1]} mode="full" className="my-1" />
        }

        // Inline code
        return <InlineCode>{children}</InlineCode>
      },
      pre: ({ children }) => <>{children}</>,
      // Comfortable paragraph spacing
      p: ({ children }) => <p className="my-2 leading-relaxed">{children}</p>,
      // Styled lists
      ul: ({ children }) => (
        <ul className="my-2 space-y-1 ps-[16px] pe-2 list-disc marker:text-muted-foreground">
          {children}
        </ul>
      ),
      ol: ({ children }) => <ol className="my-2 space-y-1 pl-6 list-decimal">{children}</ol>,
      li: ({ children }) => <li>{children}</li>,
      // Clean tables
      table: ({ children }) => (
        <div className="my-3 overflow-x-auto">
          <table className="min-w-full text-sm">{children}</table>
        </div>
      ),
      thead: ({ children }) => <thead className="border-b">{children}</thead>,
      th: ({ children }) => (
        <th className="text-left py-2 px-3 font-semibold text-muted-foreground">{children}</th>
      ),
      td: ({ children }) => <td className="py-2 px-3 border-b border-border/50">{children}</td>,
      // Headings - H1/H2 same size, differentiated by weight
      h1: ({ children }) => <h1 className="font-sans text-base font-bold mt-5 mb-3">{children}</h1>,
      h2: ({ children }) => (
        <h2 className="font-sans text-base font-semibold mt-4 mb-3">{children}</h2>
      ),
      h3: ({ children }) => (
        <h3 className="font-sans text-sm font-semibold mt-4 mb-2">{children}</h3>
      ),
      // Blockquotes
      blockquote: ({ children }) => (
        <blockquote className="border-l-2 border-muted-foreground/30 pl-3 my-2 text-muted-foreground italic">
          {children}
        </blockquote>
      ),
      // Horizontal rules
      hr: () => <hr className="my-4 border-border" />,
      // Strong/emphasis
      strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
      em: ({ children }) => <em className="italic">{children}</em>
    }
  }

  // Full mode: rich styling
  return {
    ...baseComponents,
    // Full code blocks with copy button
    code: ({ className, children, ...props }) => {
      const match = /language-(\w+)/.exec(className || '')
      const isBlock =
        'node' in props && props.node?.position?.start.line !== props.node?.position?.end.line

      if (match || isBlock) {
        const code = String(children).replace(/\n$/, '')
        return <CodeBlock code={code} language={match?.[1]} mode="full" className="my-1" />
      }

      return <InlineCode>{children}</InlineCode>
    },
    pre: ({ children }) => <>{children}</>,
    // Rich paragraph spacing
    p: ({ children }) => <p className="my-3 leading-relaxed">{children}</p>,
    // Styled lists
    ul: ({ children }) => (
      <ul className="my-3 space-y-1.5 ps-[16px] pe-2 list-disc marker:text-muted-foreground">
        {children}
      </ul>
    ),
    ol: ({ children }) => <ol className="my-3 space-y-1.5 pl-6 list-decimal">{children}</ol>,
    li: ({ children }) => <li className="leading-relaxed">{children}</li>,
    // Beautiful tables
    table: ({ children }) => (
      <div className="my-4 overflow-x-auto rounded-md border">
        <table className="min-w-full divide-y divide-border">{children}</table>
      </div>
    ),
    thead: ({ children }) => <thead className="bg-muted/50">{children}</thead>,
    tbody: ({ children }) => <tbody className="divide-y divide-border">{children}</tbody>,
    th: ({ children }) => <th className="text-left py-3 px-4 font-semibold text-sm">{children}</th>,
    td: ({ children }) => <td className="py-3 px-4 text-sm">{children}</td>,
    tr: ({ children }) => <tr className="hover:bg-muted/30 transition-colors">{children}</tr>,
    // Rich headings
    h1: ({ children }) => <h1 className="font-sans text-base font-bold mt-7 mb-4">{children}</h1>,
    h2: ({ children }) => (
      <h2 className="font-sans text-base font-semibold mt-6 mb-3">{children}</h2>
    ),
    h3: ({ children }) => <h3 className="font-sans text-sm font-semibold mt-5 mb-3">{children}</h3>,
    h4: ({ children }) => <h4 className="text-sm font-semibold mt-3 mb-1">{children}</h4>,
    // Styled blockquotes
    blockquote: ({ children }) => (
      <blockquote className="border-l-4 border-foreground/30 bg-muted/30 pl-4 pr-3 py-2 my-3 rounded-r-md">
        {children}
      </blockquote>
    ),
    // Task lists (GFM)
    input: ({ type, checked }) => {
      if (type === 'checkbox') {
        return (
          <input
            type="checkbox"
            checked={checked}
            readOnly
            className="mr-2 rounded border-muted-foreground"
          />
        )
      }
      return <input type={type} />
    },
    // Horizontal rules
    hr: () => <hr className="my-6 border-border" />,
    // Strong/emphasis
    strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
    em: ({ children }) => <em className="italic">{children}</em>,
    del: ({ children }) => <del className="line-through text-muted-foreground">{children}</del>
  }
}

/**
 * Markdown - Customizable markdown renderer with multiple render modes
 *
 * Features:
 * - Three render modes: terminal, minimal, full
 * - Syntax highlighting via Shiki
 * - GFM support (tables, task lists, strikethrough)
 * - Clickable links and file paths
 * - Memoization for streaming performance
 */
export function Markdown({
  children,
  mode = 'minimal',
  className,
  onUrlClick,
  onFileClick
}: MarkdownProps): React.JSX.Element {
  const components = React.useMemo(
    () => createComponents(mode, onUrlClick, onFileClick),
    [mode, onUrlClick, onFileClick]
  )

  // Preprocess to convert raw URLs and file paths to markdown links
  const processedContent = React.useMemo(() => preprocessLinks(children), [children])

  return (
    <div className={cn('markdown-content text-sm', className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeRaw]}
        components={components}
      >
        {processedContent}
      </ReactMarkdown>
    </div>
  )
}

/**
 * MemoizedMarkdown - Optimized for streaming scenarios
 *
 * Splits content into blocks and memoizes each block separately,
 * so only new/changed blocks re-render during streaming.
 */
export const MemoizedMarkdown = React.memo(Markdown, (prevProps, nextProps) => {
  // If id is provided, use it for memoization
  if (prevProps.id && nextProps.id) {
    return (
      prevProps.id === nextProps.id &&
      prevProps.children === nextProps.children &&
      prevProps.mode === nextProps.mode
    )
  }
  // Otherwise compare content and mode
  return prevProps.children === nextProps.children && prevProps.mode === nextProps.mode
})
MemoizedMarkdown.displayName = 'MemoizedMarkdown'
