import * as React from 'react'
import ReactMarkdown, { type Components, defaultUrlTransform } from 'react-markdown'
import rehypeKatex from 'rehype-katex'
import rehypeRaw from 'rehype-raw'
import rehypeSanitize, { defaultSchema } from 'rehype-sanitize'
import remarkBreaks from 'remark-breaks'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'
import { createLowlight, common } from 'lowlight'
import { toJsxRuntime } from 'hast-util-to-jsx-runtime'
import { jsx, jsxs, Fragment } from 'react/jsx-runtime'
import { FileText, Download, Check, Copy, Trash2 } from 'lucide-react'
import { isValidElement, useState } from 'react'
import { cn } from '@multica/ui/lib/utils'
import { CODE_LIGATURE_CLASS } from '@multica/ui/lib/code-style'
import { CodeBlock, InlineCode } from './CodeBlock'
import { isAllowedFileCardHref, preprocessFileCards } from './file-cards'
import { preprocessLinks } from './linkify'
import { preprocessMentionShortcodes } from './mentions'
import { preprocessJsonLiterals } from './preprocess-json'
import { highlightToHtml } from './highlight-markdown'
import 'katex/dist/katex.min.css'
import './markdown.css'

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
 *
 * - 'editor-parity': Rendering that matches the Tiptap editor's readonly output
 *   Best for: Wiki/Issue readonly content, comment cards
 *   Uses lowlight (same engine as Tiptap), CodeBlockHeader, tableWrapper div,
 *   .rich-text-editor.readonly CSS scope, and full preprocessing pipeline
 *   (including JSON literals + ==mark== highlight syntax).
 */
export type RenderMode = 'terminal' | 'minimal' | 'full' | 'editor-parity'

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
  /**
   * Custom renderer for mention links (e.g. mention://issue/UUID).
   * When not provided, mentions render as a simple styled span.
   * The `label` parameter carries the link text (e.g. "MUL-123" from
   * `[MUL-123](mention://issue/uuid)`), useful for fallback display.
   */
  renderMention?: (props: { type: string; id: string; label?: string }) => React.ReactNode
  /**
   * CDN hostname for file card detection (e.g. "multica-static.copilothub.ai").
   * When provided, enables file card preprocessing and rendering.
   */
  cdnDomain?: string
  /**
   * Optional override for the image renderer. When provided, replaces the
   * default `<img>` with constrained sizing. The views-package wrapper uses
   * this to inject the unified `<Attachment>` component so chat messages get
   * the same hover toolbar / lightbox / preview-modal treatment as comments.
   */
  renderImage?: (props: { src: string; alt: string }) => React.ReactNode
  /**
   * Optional override for the file-card renderer. When provided, replaces
   * the simplified card chrome (filename + download button) with whatever
   * the caller supplies. Used the same way as `renderImage` to bridge into
   * the views-package `<Attachment>` component.
   */
  renderFileCard?: (props: { href: string; filename: string }) => React.ReactNode
  /**
   * Optional renderer for Mermaid diagrams. When provided and mode is
   * 'editor-parity', replaces the default code block for ```mermaid fences
   * with the caller's component (e.g. <MermaidDiagram>).
   */
  renderMermaid?: (props: { chart: string }) => React.ReactNode
  /**
   * Optional renderer for HTML block previews. When provided and mode is
   * 'editor-parity', replaces the default code block for ```html fences
   * with the caller's component (e.g. <HtmlBlockPreview>).
   */
  renderHtmlBlock?: (props: { html: string }) => React.ReactNode
  /**
   * Optional callback for link hover events. When provided and mode is
   * 'editor-parity', the outer wrapper ref is passed so the caller can
   * attach hover listeners for LinkHoverCard behavior.
   */
  onLinkHover?: (wrapperRef: React.RefObject<HTMLDivElement | null>) => void
}

// ---------------------------------------------------------------------------
// Lowlight — same engine + language set as Tiptap's CodeBlockLowlight.
// Used only in editor-parity mode.
// ---------------------------------------------------------------------------

const lowlight = createLowlight(common)

// Code fences that the `code` renderer returns as a non-<code> React element
// (Mermaid diagram, HTML preview iframe). The `pre` renderer below unwraps
// these so the default <pre><code> envelope doesn't clamp their styles.
const PRE_UNWRAP_RE = /(^|\s)language-(html|mermaid)(\s|$)/

// ---------------------------------------------------------------------------
// Sanitization schema — extends GitHub defaults to allow code highlighting classes,
// Multica's internal mention/slash protocols, and <mark> (text highlight emitted
// by highlightToHtml from `==text==`).
// ---------------------------------------------------------------------------
const sanitizeSchema = {
  ...defaultSchema,
  tagNames: [...(defaultSchema.tagNames ?? []), 'mark'],
  protocols: {
    ...defaultSchema.protocols,
    href: [...(defaultSchema.protocols?.href ?? []), 'mention', 'slash'],
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
      ...(defaultSchema.attributes?.img ?? []),
      'alt',
    ],
  },
}

/**
 * Custom URL transform that allows Multica internal protocols while keeping
 * the default security for all other URLs.
 */
function urlTransform(url: string): string {
  if (url.startsWith('mention://')) return url
  if (url.startsWith('slash://skill/')) return url
  return defaultUrlTransform(url)
}


// File path detection regex - matches paths starting with /, ~/, or ./
const FILE_PATH_REGEX =
  /^(?:\/|~\/|\.\/)[\w\-./@]+\.(?:ts|tsx|js|jsx|mjs|cjs|md|json|yaml|yml|py|go|rs|css|scss|less|html|htm|txt|log|sh|bash|zsh|swift|kt|java|c|cpp|h|hpp|rb|php|xml|toml|ini|cfg|conf|env|sql|graphql|vue|svelte|astro|prisma)$/i

// ---------------------------------------------------------------------------
// CodeBlockHeader — used in editor-parity mode to match Tiptap's code block
// chrome (language label + copy button + disabled delete button).
// ---------------------------------------------------------------------------

function CodeBlockHeader({ language, code }: { language?: string; code: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    if (!code) return
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard access can be unavailable in readonly contexts.
    }
  }

  return (
    <div className="code-block-header flex select-none items-center justify-between rounded-t-md border-b border-border bg-muted/50 px-3 py-1.5 text-xs text-muted-foreground">
      <span>{language || 'text'}</span>
      <div className="flex items-center gap-1">
        <button
          type="button"
          onMouseDown={(e) => e.preventDefault()}
          onClick={handleCopy}
          className="pointer-events-auto flex items-center gap-1 rounded px-1.5 py-0.5 transition-colors hover:bg-muted hover:text-foreground"
          title="Copy code"
          aria-label="Copy code"
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
          <span>{copied ? 'Copied' : 'Copy'}</span>
        </button>
        <button
          type="button"
          disabled
          aria-disabled="true"
          className="flex h-6 w-6 cursor-default items-center justify-center rounded text-muted-foreground opacity-60"
          title="Delete"
          aria-label="Delete"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>
    </div>
  )
}

/**
 * Extract plain text from a react-markdown AST node's children.
 * Used by the editor-parity code component to ensure code block / inline code
 * content is always rendered as plain text.
 */
function extractTextFromAst(node: any): string {
  if (!node?.children) return ''
  return (node.children as any[])
    .map((n: any) => {
      if (n.type === 'text') return n.value as string
      if (n.children) return extractTextFromAst(n)
      return ''
    })
    .join('')
    .replace(/\n$/, '')
}

/**
 * Create custom components based on render mode
 */
function createComponents(
  mode: RenderMode,
  onUrlClick?: (url: string) => void,
  onFileClick?: (path: string) => void,
  renderMention?: (props: { type: string; id: string; label?: string }) => React.ReactNode,
  renderImage?: (props: { src: string; alt: string }) => React.ReactNode,
  renderFileCard?: (props: { href: string; filename: string }) => React.ReactNode,
  renderMermaid?: (props: { chart: string }) => React.ReactNode,
  renderHtmlBlock?: (props: { html: string }) => React.ReactNode,
): Partial<Components> {
  const baseComponents: Partial<Components> = {
    // FileCard: intercept <div data-type="fileCard"> from preprocessFileCards
    div: ({ node, children, ...props }) => {
      const dataType = node?.properties?.dataType as string | undefined
      if (dataType === 'fileCard') {
        const rawHref = (node?.properties?.dataHref as string) || ''
        const href = isAllowedFileCardHref(rawHref) ? rawHref : ''
        const filename = (node?.properties?.dataFilename as string) || ''
        if (renderFileCard) {
          return <>{renderFileCard({ href, filename })}</>
        }
        return (
          <div className="my-1 flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1 transition-colors hover:bg-muted">
            <FileText className="size-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm">{filename}</p>
            </div>
            {href && (
              <button
                type="button"
                className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                onClick={() => window.open(href, '_blank', 'noopener,noreferrer')}
              >
                <Download className="size-3.5" />
              </button>
            )}
          </div>
        )
      }
      return <div {...props}>{children}</div>
    },
    // Images: render uploaded images with constrained sizing
    img: ({ src, alt }) => {
      if (renderImage) {
        return <>{renderImage({ src: typeof src === 'string' ? src : '', alt: alt ?? '' })}</>
      }
      return (
        <img
          src={src}
          alt={alt ?? ""}
          className="max-w-full h-auto rounded-md my-2"
          loading="lazy"
        />
      )
    },
    // Links: Make clickable with callbacks, or render as mention
    a: ({ href, children }) => {
      // Mention links: mention://member/id, mention://agent/id, mention://issue/id, mention://project/id, mention://all/all
      if (href?.startsWith('mention://')) {
        const mentionMatch = href.match(/^mention:\/\/(member|agent|issue|project|all)\/(.+)$/)
        if (mentionMatch?.[1] && mentionMatch[2]) {
          const type = mentionMatch[1]
          const id = mentionMatch[2]
          // Extract label text from children for fallback display
          const label =
            typeof children === 'string'
              ? children
              : Array.isArray(children)
                ? children.join('')
                : undefined

          if (renderMention) {
            // Let the custom renderer opt out for types it doesn't handle
            // by returning null/undefined — we then fall through to the
            // default styled span so nothing ever disappears silently.
            const rendered = renderMention({ type, id, label })
            if (rendered) return <>{rendered}</>
          }

          // Fallback: render as a simple styled span
          return (
            <span className="text-primary font-semibold mx-0.5">
              {children}
            </span>
          )
        }
        return (
          <span className="text-primary font-semibold mx-0.5">
            {children}
          </span>
        )
      }

      if (href?.startsWith('slash://skill/')) {
        return (
          <span className="slash-command text-primary font-semibold mx-0.5">
            {children}
          </span>
        )
      }

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
      code: ({ children }) => <code className={cn('font-mono', CODE_LIGATURE_CLASS)}>{children}</code>,
      pre: ({ children }) => (
        <pre className={cn('font-mono whitespace-pre-wrap my-2', CODE_LIGATURE_CLASS)}>
          {children}
        </pre>
      ),
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
      code: ({ className, children, node }) => {
        const match = /language-(\w+)/.exec(className || '')
        const isBlock =
          node?.position && node.position.start.line !== node.position.end.line

        // Block code - use CodeBlock with full mode
        if (match || isBlock) {
          const code = String(children).replace(/\n$/, '')
          return <CodeBlock code={code} language={match?.[1]} mode="full" className="my-1" />
        }

        // Inline code — force string to avoid rendering interactive elements
        return <InlineCode>{String(children)}</InlineCode>
      },
      pre: ({ children }) => <>{children}</>,
      // Comfortable paragraph spacing
      p: ({ children }) => <p className="my-2 leading-relaxed">{children}</p>,
      // Styled lists
      ul: ({ children }) => (
        <ul className="my-2 space-y-1 ps-4 pe-2 list-disc marker:text-muted-foreground">
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

  // Editor-parity mode: matches Tiptap's readonly output (lowlight, CodeBlockHeader,
  // tableWrapper, Mermaid/HTML callbacks). Used by ReadonlyContent via views wrapper.
  if (mode === 'editor-parity') {
    return {
      ...baseComponents,
      // Code — lowlight highlighting for blocks, plain render for inline.
      // Mermaid and HTML blocks delegate to callback renderers.
      code: ({ className, children, node, ...props }) => {
        const lang = /language-(\w+)/.exec(className || '')?.[1]
        const isBlock =
          node?.position &&
          node.position.start.line !== node.position.end.line

        // Extract plain text from AST node to avoid rendering interactive
        // elements inside code blocks/inline code.
        const codeText = extractTextFromAst(node)

        if (isBlock && lang === 'mermaid') {
          if (renderMermaid) {
            return <>{renderMermaid({ chart: codeText })}</>
          }
          // Fallback: plain code block
          return <code className={cn('hljs', 'language-mermaid')}>{codeText}</code>
        }
        if (isBlock && lang === 'html') {
          if (renderHtmlBlock) {
            return <>{renderHtmlBlock({ html: codeText })}</>
          }
          // Fallback: plain code block
          return <code className={cn('hljs', 'language-html')}>{codeText}</code>
        }

        if (!isBlock && !lang) {
          // Inline code — always render as plain text
          return <code {...props}>{codeText || children}</code>
        }

        const code = codeText || String(children).replace(/\n$/, '')

        // Block code — highlight with lowlight (same engine as Tiptap)
        try {
          const tree = lang
            ? lowlight.highlight(lang, code)
            : lowlight.highlightAuto(code)
          if (tree.children.length > 0) {
            const highlighted = toJsxRuntime(tree, { jsx, jsxs, Fragment })
            return (
              <code className={cn('hljs', lang && `language-${lang}`)}>
                {highlighted}
              </code>
            )
          }
        } catch {
          // fall through to plain render
        }
        return (
          <code className={cn('hljs', className)} {...props}>
            {code}
          </code>
        )
      },

      // Pre — pass through for CSS styling. Special-case Mermaid / HtmlBlockPreview
      // so the outer <pre> does not wrap them.
      pre: ({ node, children }) => {
        if (isValidElement(children)) {
          const childProps = children.props as { className?: string }
          if (PRE_UNWRAP_RE.test(childProps.className ?? '')) {
            return <>{children}</>
          }
        }
        // Extract text content and language for header bar
        const codeEl = (node?.children ?? []).find(
          (child: any) => child.type === 'element' && child.tagName === 'code'
        ) as any
        const codeText = codeEl
          ? (codeEl.children as any[])
              .filter((n: any) => n.type === 'text')
              .map((n: any) => n.value as string)
              .join('')
              .replace(/\n$/, '')
          : ''
        const classNames: string[] = codeEl?.properties?.className ?? []
        const langClass = classNames.find((cls: string) => cls.startsWith('language-'))
        const language = langClass?.replace('language-', '')
        return (
          <div className="code-block-wrapper my-2 overflow-hidden rounded-md border border-border select-text">
            {codeText && <CodeBlockHeader language={language} code={codeText} />}
            <pre className="!mt-0 !rounded-t-none !border-0 select-text">{children}</pre>
          </div>
        )
      },

      // Tables — wrap in tableWrapper div for border/radius/scroll (matches Tiptap)
      table: ({ children }) => (
        <div className="tableWrapper">
          <table>{children}</table>
        </div>
      ),
    }
  }

  // Full mode: rich styling
  return {
    ...baseComponents,
    // Full code blocks with copy button
    code: ({ className, children, node }) => {
      const match = /language-(\w+)/.exec(className || '')
      const isBlock =
        node?.position && node.position.start.line !== node.position.end.line

      if (match || isBlock) {
        const code = String(children).replace(/\n$/, '')
        return <CodeBlock code={code} language={match?.[1]} mode="full" className="my-1" />
      }

      return <InlineCode>{String(children)}</InlineCode>
    },
    pre: ({ children }) => <>{children}</>,
    // Rich paragraph spacing
    p: ({ children }) => <p className="my-3 leading-relaxed">{children}</p>,
    // Styled lists
    ul: ({ children }) => (
      <ul className="my-3 space-y-1.5 ps-4 pe-2 list-disc marker:text-muted-foreground">
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
 * - Four render modes: terminal, minimal, full, editor-parity
 * - Syntax highlighting via Shiki (minimal/full) or lowlight (editor-parity)
 * - GFM support (tables, task lists, strikethrough)
 * - Clickable links and file paths
 * - Memoization for streaming performance
 * - Pluggable mention rendering via renderMention prop
 * - editor-parity mode: matches Tiptap editor readonly output with
 *   lowlight, CodeBlockHeader, ==mark== syntax, and callback props
 *   for Mermaid/HTML/link-hover injection
 */
export function Markdown({
  children,
  mode = 'minimal',
  className,
  onUrlClick,
  onFileClick,
  renderMention,
  renderImage,
  renderFileCard,
  cdnDomain,
  renderMermaid,
  renderHtmlBlock,
  onLinkHover,
}: MarkdownProps): React.JSX.Element {
  const components = React.useMemo(
    () => createComponents(mode, onUrlClick, onFileClick, renderMention, renderImage, renderFileCard, renderMermaid, renderHtmlBlock),
    [mode, onUrlClick, onFileClick, renderMention, renderImage, renderFileCard, renderMermaid, renderHtmlBlock]
  )

  const wrapperRef = React.useRef<HTMLDivElement>(null)

  // Notify caller of wrapper ref for link-hover-card attachment
  React.useEffect(() => {
    if (onLinkHover && mode === 'editor-parity') {
      onLinkHover(wrapperRef)
    }
  }, [onLinkHover, mode])

  // Preprocess: convert mention shortcodes, raw URLs, and file cards to renderable content.
  // editor-parity mode additionally runs preprocessJsonLiterals + highlightToHtml.
  const processedContent = React.useMemo(
    () => {
      let result = preprocessMentionShortcodes(children)
      result = preprocessLinks(result)
      result = preprocessFileCards(result, cdnDomain ?? '')
      if (mode === 'editor-parity') {
        result = preprocessJsonLiterals(result)
        result = highlightToHtml(result)
      }
      return result
    },
    [children, cdnDomain, mode]
  )

  // editor-parity uses .rich-text-editor.readonly CSS scope (matches Tiptap styles)
  const wrapperClass = mode === 'editor-parity'
    ? cn('rich-text-editor readonly text-sm break-words', className)
    : cn('markdown-content break-words', className)

  return (
    <div ref={wrapperRef} className={wrapperClass}>
      <ReactMarkdown
        remarkPlugins={[remarkMath, remarkBreaks, [remarkGfm, { singleTilde: false }]]}
        rehypePlugins={[rehypeRaw, [rehypeSanitize, sanitizeSchema], rehypeKatex]}
        urlTransform={urlTransform}
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
