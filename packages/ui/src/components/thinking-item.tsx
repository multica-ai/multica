"use client"

import { memo, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  AiBrain01Icon,
  ArrowRight01Icon,
} from "@hugeicons/core-free-icons"
import { cn } from "@multica/ui/lib/utils"

interface ThinkingItemProps {
  thinking: string
  isStreaming?: boolean
}

export const ThinkingItem = memo(function ThinkingItem({ thinking, isStreaming }: ThinkingItemProps) {
  const [expanded, setExpanded] = useState(false)

  const hasContent = !!thinking
  const isThinking = isStreaming && !hasContent

  return (
    <div className="py-0.5 px-2.5 text-sm text-muted-foreground">
      <div className={cn("rounded transition-colors", expanded && "bg-muted/30")}>
      <button
        type="button"
        aria-label={isThinking ? "Thinking" : "Thought"}
        aria-expanded={hasContent ? expanded : undefined}
        onClick={() => hasContent && setExpanded(!expanded)}
        className={cn(
          "group flex w-full items-center gap-1.5 rounded px-2.5 py-1",
          "text-left transition-[color,background-color]",
          "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 outline-none",
          hasContent && !expanded && "hover:bg-muted/30 cursor-pointer",
          hasContent && expanded && "cursor-pointer",
          !hasContent && "cursor-default",
        )}
      >
        {/* Status dot */}
        <span
          className={cn(
            "size-1.5 rounded-full shrink-0",
            isThinking
              ? "bg-[var(--tool-running)] motion-safe:animate-[glow-pulse_2s_ease-in-out_infinite]"
              : "bg-[var(--tool-success)]",
          )}
        />

        {/* Icon */}
        <HugeiconsIcon
          icon={AiBrain01Icon}
          strokeWidth={2}
          className="size-3.5 shrink-0"
        />

        {/* Label */}
        <span className="font-medium shrink-0">
          {isThinking ? "Thinking" : "Thought"}
        </span>

        {/* Running indicator */}
        {isThinking && (
          <span className="ml-auto text-xs text-muted-foreground/60 shrink-0 font-[tabular-nums] motion-safe:animate-pulse">
            thinking…
          </span>
        )}

        {/* Chevron — visible on hover when expandable */}
        {hasContent && (
          <HugeiconsIcon
            icon={ArrowRight01Icon}
            strokeWidth={2}
            className={cn(
              "size-3 text-muted-foreground/40 shrink-0",
              "transition-[transform,opacity] duration-150",
              !isThinking && "ml-auto",
              "opacity-0 group-hover:opacity-100",
              expanded && "rotate-90 opacity-100",
            )}
          />
        )}
      </button>

      {/* Expanded thinking content */}
      {expanded && thinking && (
        <div
          role="region"
          aria-label="Thinking content"
          tabIndex={0}
          className="px-2.5 pt-1 pb-2 text-xs max-h-48 overflow-y-auto whitespace-pre-wrap break-words"
        >
          {thinking}
        </div>
      )}
      </div>
    </div>
  )
})
