import * as React from 'react'
import { codeToHtml, bundledLanguages, type BundledLanguage } from 'shiki'
import { cn } from '@/lib/utils'

/**
 * Hook to detect dark mode changes via MutationObserver
 * Returns true if 'dark' class is present on document.documentElement
 */
function useIsDarkMode(): boolean {
  const [isDark, setIsDark] = React.useState(() =>
    document.documentElement.classList.contains('dark')
  )

  React.useEffect(() => {
    const observer = new MutationObserver((mutations) => {
      for (const mutation of mutations) {
        if (mutation.attributeName === 'class') {
          setIsDark(document.documentElement.classList.contains('dark'))
        }
      }
    })

    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class']
    })

    return () => observer.disconnect()
  }, [])

  return isDark
}

export interface CodeBlockProps {
  code: string
  language?: string
  className?: string
  /**
   * Render mode affects code block styling:
   * - 'terminal': Minimal, keeps control chars visible
   * - 'minimal': Clean code, basic styling
   * - 'full': Rich styling with background, copy button, etc.
   */
  mode?: 'terminal' | 'minimal' | 'full'
  /**
   * Force a specific theme. If not provided, detects from document.documentElement.classList
   */
  forcedTheme?: 'light' | 'dark'
}

// Map common aliases to Shiki language names
const LANGUAGE_ALIASES: Record<string, BundledLanguage> = {
  js: 'javascript',
  ts: 'typescript',
  py: 'python',
  sh: 'bash',
  zsh: 'bash',
  yml: 'yaml',
  rb: 'ruby',
  rs: 'rust',
  kt: 'kotlin',
  'objective-c': 'objc',
  objc: 'objc'
}

// Simple LRU cache for highlighted code
const highlightCache = new Map<string, string>()
const CACHE_MAX_SIZE = 200

function getCacheKey(code: string, lang: string, theme: string): string {
  return `${theme}:${lang}:${code}`
}

function isValidLanguage(lang: string): lang is BundledLanguage {
  const normalized = LANGUAGE_ALIASES[lang] || lang
  return normalized in bundledLanguages
}

/**
 * CodeBlock - Syntax highlighted code block using Shiki
 *
 * Uses VS Code's syntax highlighting engine for accurate highlighting.
 * Lazy-loads highlighting and caches results for performance.
 */
export function CodeBlock({
  code,
  language = 'text',
  className,
  mode = 'full',
  forcedTheme
}: CodeBlockProps): React.JSX.Element {
  const [highlighted, setHighlighted] = React.useState<string | null>(null)
  const [isLoading, setIsLoading] = React.useState(true)
  const [copied, setCopied] = React.useState(false)

  // Subscribe to dark mode changes via MutationObserver
  const isDarkMode = useIsDarkMode()

  // Resolve language alias - keep as string to allow 'text' fallback
  const langLower = language.toLowerCase()
  const resolvedLang: string = LANGUAGE_ALIASES[langLower] || langLower

  React.useEffect(() => {
    let cancelled = false

    async function highlight(): Promise<void> {
      // Theme priority:
      // 1. forcedTheme prop - explicit override
      // 2. isDarkMode from hook - reactive to theme changes
      let theme: string
      if (forcedTheme) {
        theme = forcedTheme === 'dark' ? 'github-dark' : 'github-light'
      } else {
        theme = isDarkMode ? 'github-dark' : 'github-light'
      }
      const cacheKey = getCacheKey(code, resolvedLang, theme)

      const cached = highlightCache.get(cacheKey)
      if (cached) {
        if (!cancelled) {
          setHighlighted(cached)
          setIsLoading(false)
        }
        return
      }

      try {
        // Use valid language or fallback to plaintext
        const lang = isValidLanguage(resolvedLang) ? resolvedLang : 'text'

        const html = await codeToHtml(code, {
          lang,
          theme
        })

        // Cache the result
        if (highlightCache.size >= CACHE_MAX_SIZE) {
          const firstKey = highlightCache.keys().next().value
          if (firstKey) highlightCache.delete(firstKey)
        }
        highlightCache.set(cacheKey, html)

        if (!cancelled) {
          setHighlighted(html)
          setIsLoading(false)
        }
      } catch (error) {
        // Fallback to plain text on error
        console.warn(`Shiki highlighting failed for language "${resolvedLang}":`, error)
        if (!cancelled) {
          setHighlighted(null)
          setIsLoading(false)
        }
      }
    }

    highlight()

    return () => {
      cancelled = true
    }
  }, [code, resolvedLang, forcedTheme, isDarkMode])

  const handleCopy = React.useCallback(async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (err) {
      console.error('Failed to copy code:', err)
    }
  }, [code])

  // Terminal mode: raw monospace with minimal styling
  if (mode === 'terminal') {
    return (
      <pre className={cn('font-mono text-sm whitespace-pre-wrap', className)}>
        <code>{code}</code>
      </pre>
    )
  }

  // Minimal mode: just syntax highlighting, no chrome
  if (mode === 'minimal') {
    if (isLoading || !highlighted) {
      return (
        <pre className={cn('font-mono text-sm whitespace-pre-wrap', className)}>
          <code>{code}</code>
        </pre>
      )
    }

    return (
      <div
        className={cn(
          'font-mono text-sm [&_pre]:!bg-transparent [&_pre]:!p-0 [&_pre]:whitespace-pre-wrap [&_pre]:break-all [&_code]:!bg-transparent',
          className
        )}
        dangerouslySetInnerHTML={{ __html: highlighted }}
      />
    )
  }

  // Full mode: rich styling with header and copy button
  return (
    <div
      className={cn(
        'relative group rounded-lg overflow-hidden border bg-muted/30 mb-4 last:mb-0',
        className
      )}
    >
      {/* Language label + copy button */}
      <div className="flex items-center justify-between px-3 py-1.5 bg-muted/50 border-b text-xs">
        <span className="text-muted-foreground font-medium uppercase tracking-wide">
          {resolvedLang !== 'text' ? resolvedLang : 'plain text'}
        </span>
        <button
          onClick={handleCopy}
          className="opacity-0 group-hover:opacity-100 transition-opacity text-muted-foreground hover:text-foreground"
          aria-label="Copy code"
        >
          {copied ? (
            <svg
              className="w-4 h-4 text-green-500"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M5 13l4 4L19 7"
              />
            </svg>
          ) : (
            <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
              />
            </svg>
          )}
        </button>
      </div>

      {/* Code content */}
      <div className="p-3 overflow-x-auto">
        {isLoading || !highlighted ? (
          <pre className="font-mono text-sm whitespace-pre-wrap break-all">
            <code>{code}</code>
          </pre>
        ) : (
          <div
            className="font-mono text-sm [&_pre]:!bg-transparent [&_pre]:!m-0 [&_pre]:!p-0 [&_pre]:whitespace-pre-wrap [&_pre]:break-all [&_code]:!bg-transparent"
            dangerouslySetInnerHTML={{ __html: highlighted }}
          />
        )}
      </div>
    </div>
  )
}

/**
 * InlineCode - Styled inline code span
 * Features: subtle background (3%), subtle border (5%), 75% opacity text
 */
export function InlineCode({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}): React.JSX.Element {
  return (
    <code
      className={cn(
        'px-1.5 py-0.5 rounded bg-foreground/[0.03] border border-foreground/[0.05] font-mono text-sm text-foreground/75',
        className
      )}
    >
      {children}
    </code>
  )
}
