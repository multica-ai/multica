import * as React from 'react'
import { codeToHtml, bundledLanguages, type BundledLanguage } from 'shiki'
import { Copy, Check } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Button } from "@multica/ui/components/ui/button"
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip"
import { cn } from '@multica/ui/lib/utils'

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

function getCacheKey(code: string, lang: string): string {
  return `${lang}:${code}`
}

function isValidLanguage(lang: string): lang is BundledLanguage {
  const normalized = LANGUAGE_ALIASES[lang] || lang
  return normalized in bundledLanguages
}

/**
 * CodeBlock - Syntax highlighted code block using Shiki
 *
 * Uses Shiki dual themes with CSS variables for light/dark switching.
 * No JS-based dark mode detection needed — theme switching is handled
 * entirely via CSS (see globals.css for .shiki/.dark .shiki rules).
 *
 * @see https://shiki.style/guide/dual-themes
 */
export function CodeBlock({
  code,
  language = 'text',
  className,
  mode = 'full'
}: CodeBlockProps): React.JSX.Element {
  const { t } = useTranslation("ui")
  const [highlighted, setHighlighted] = React.useState<string | null>(null)
  const [isLoading, setIsLoading] = React.useState(true)
  const [copied, setCopied] = React.useState(false)

  // Resolve language alias - keep as string to allow 'text' fallback
  const langLower = language.toLowerCase()
  const resolvedLang: string = LANGUAGE_ALIASES[langLower] || langLower

  React.useEffect(() => {
    let cancelled = false

    async function highlight(): Promise<void> {
      const cacheKey = getCacheKey(code, resolvedLang)

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

        // Dual themes: Shiki outputs CSS variables for both themes in one pass.
        // CSS handles switching via .dark selector (see globals.css).
        const html = await codeToHtml(code, {
          lang,
          themes: {
            light: 'github-light',
            dark: 'github-dark',
          },
          defaultColor: false,
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
  }, [code, resolvedLang])

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
        <code className="font-mono">{code}</code>
      </pre>
    )
  }

  // Minimal mode: just syntax highlighting, no chrome
  if (mode === 'minimal') {
    if (isLoading || !highlighted) {
      return (
        <pre className={cn('font-mono text-sm whitespace-pre-wrap', className)}>
          <code className="font-mono">{code}</code>
        </pre>
      )
    }

    return (
      <div
        className={cn(
          'font-mono text-sm [&_pre]:!bg-transparent [&_pre]:!p-0 [&_pre]:whitespace-pre-wrap [&_pre]:break-all [&_code]:!bg-transparent [&_code]:font-mono [&_pre]:font-mono',
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
          {resolvedLang !== 'text' ? resolvedLang : t(($) => $.plain_text)}
        </span>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={handleCopy}
                className="opacity-0 group-hover:opacity-100 transition-opacity text-muted-foreground hover:text-foreground"
                aria-label={t(($) => $.copy_code)}
              >
                {copied ? (
                  <Check className="size-3.5 text-success" />
                ) : (
                  <Copy className="size-3.5" />
                )}
              </Button>
            }
          />
          <TooltipContent>{t(($) => $.copy_code)}</TooltipContent>
        </Tooltip>
      </div>

      {/* Code content */}
      <div className="p-3 overflow-x-auto">
        {isLoading || !highlighted ? (
          <pre className="font-mono text-sm whitespace-pre-wrap break-all">
            <code className="font-mono">{code}</code>
          </pre>
        ) : (
          <div
            className="font-mono text-sm [&_pre]:!bg-transparent [&_pre]:!m-0 [&_pre]:!p-0 [&_pre]:whitespace-pre-wrap [&_pre]:break-all [&_code]:!bg-transparent [&_code]:font-mono [&_pre]:font-mono"
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
