"use client"

import { memo, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  File01Icon,
  FloppyDiskIcon,
  FileEditIcon,
  CommandLineIcon,
  Search01Icon,
  FolderOpenIcon,
  GlobeIcon,
  DatabaseIcon,
  GitBranchIcon,
  ArrowRight01Icon,
} from "@hugeicons/core-free-icons"
import { cn, getTextContent } from "@multica/ui/lib/utils"
import type { Message } from "@multica/store"

// ---------------------------------------------------------------------------
// Tool display config
// ---------------------------------------------------------------------------

const TOOL_DISPLAY: Record<string, { label: string; icon: typeof File01Icon }> = {
  read:            { label: "Read",          icon: File01Icon },
  write:           { label: "Write",         icon: FloppyDiskIcon },
  edit:            { label: "Edit",          icon: FileEditIcon },
  exec:            { label: "Exec",          icon: CommandLineIcon },
  bash:            { label: "Exec",          icon: CommandLineIcon },
  process:         { label: "Process",       icon: CommandLineIcon },
  grep:            { label: "Grep",          icon: Search01Icon },
  find:            { label: "Find",          icon: Search01Icon },
  ls:              { label: "ListDir",       icon: FolderOpenIcon },
  glob:            { label: "Glob",          icon: Search01Icon },
  web_search:      { label: "WebSearch",     icon: GlobeIcon },
  web_fetch:       { label: "WebFetch",      icon: GlobeIcon },
  memory_get:      { label: "MemoryGet",     icon: DatabaseIcon },
  memory_set:      { label: "MemorySet",     icon: DatabaseIcon },
  memory_delete:   { label: "MemoryDelete",  icon: DatabaseIcon },
  memory_list:     { label: "MemoryList",    icon: DatabaseIcon },
  sessions_spawn:  { label: "SpawnSession",  icon: GitBranchIcon },
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Extract a short basename from a file path */
function basename(path: string): string {
  return path.split("/").pop() ?? path
}

/** Smart subtitle based on tool type and args */
function getSubtitle(toolName: string, args?: Record<string, unknown>): string {
  if (!args) return ""
  switch (toolName) {
    case "read":
    case "write":
    case "edit":
      return args.path ? basename(String(args.path)) : ""
    case "exec":
    case "bash":
    case "process": {
      const cmd = String(args.command ?? args.cmd ?? "")
      return cmd.length > 60 ? cmd.slice(0, 57) + "…" : cmd
    }
    case "grep":
    case "find":
      return args.pattern ? String(args.pattern) : ""
    case "glob":
      return args.pattern ? String(args.pattern) : ""
    case "web_search":
      return args.query ? String(args.query) : ""
    case "web_fetch":
      try { return new URL(String(args.url)).hostname } catch { return String(args.url ?? "") }
    default:
      return ""
  }
}

/** Running-state label per tool */
const RUNNING_LABELS: Record<string, string> = {
  read: "reading…",
  write: "writing…",
  edit: "editing…",
  exec: "running…",
  bash: "running…",
  process: "running…",
  grep: "searching…",
  find: "searching…",
  glob: "searching…",
  web_search: "searching…",
  web_fetch: "fetching…",
}

/** Stats derived from tool result content */
function getStats(toolName: string, toolStatus: string, resultText: string): string {
  if (toolStatus === "running") return RUNNING_LABELS[toolName] ?? "working…"
  if (toolStatus === "error" || toolStatus === "interrupted" || !resultText) return ""

  switch (toolName) {
    case "read": {
      const lines = resultText.split("\n").length
      return `${lines} lines`
    }
    case "grep": {
      const matches = resultText.split("\n").filter((l) => l.trim()).length
      return matches > 0 ? `${matches} matches` : ""
    }
    case "glob":
    case "find": {
      const files = resultText.split("\n").filter((l) => l.trim()).length
      return files > 0 ? `${files} files` : ""
    }
    default:
      return ""
  }
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export const ToolCallItem = memo(function ToolCallItem({ message }: { message: Message }) {
  const [expanded, setExpanded] = useState(false)
  const { toolName = "", toolStatus = "running", toolArgs, content } = message

  const display = TOOL_DISPLAY[toolName] ?? { label: toolName, icon: CommandLineIcon }
  const isFinished = toolStatus !== "running"
  const resultText = getTextContent(content)
  const hasDetails = isFinished && !!resultText
  const subtitle = getSubtitle(toolName, toolArgs)
  const stats = getStats(toolName, toolStatus, resultText)

  return (
    <div className="py-0.5 px-2.5 text-sm text-muted-foreground">
      <div className={cn("rounded transition-colors", expanded && "bg-muted/30")}>
      <button
        type="button"
        aria-label={`${display.label}${subtitle ? ` ${subtitle}` : ""} — ${toolStatus}`}
        aria-expanded={hasDetails ? expanded : undefined}
        onClick={() => hasDetails && setExpanded(!expanded)}
        className={cn(
          "group flex w-full items-center gap-1.5 rounded px-2.5 py-1",
          "text-left transition-[color,background-color]",
          "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 outline-none",
          hasDetails && !expanded && "hover:bg-muted/30 cursor-pointer",
          hasDetails && expanded && "cursor-pointer",
          !hasDetails && "cursor-default",
        )}
      >
        {/* Status dot */}
        <span
          className={cn(
            "size-1.5 rounded-full shrink-0",
            toolStatus === "running" && "bg-[var(--tool-running)] motion-safe:animate-[glow-pulse_2s_ease-in-out_infinite]",
            toolStatus === "success" && "bg-[var(--tool-success)]",
            toolStatus === "error" && "bg-[var(--tool-error)]",
            toolStatus === "interrupted" && "bg-[var(--tool-error)]",
          )}
        />

        {/* Tool icon */}
        <HugeiconsIcon
          icon={display.icon}
          strokeWidth={2}
          className={cn("size-3.5 shrink-0", toolStatus === "error" && "text-[var(--tool-error)]")}
        />

        {/* Tool label */}
        <span className={cn(
          "font-medium shrink-0",
          toolStatus === "error" && "text-[var(--tool-error)]",
          toolStatus === "interrupted" && "text-[var(--tool-error)]",
        )}>
          {display.label}
        </span>

        {/* Smart subtitle */}
        {subtitle && (
          <span className="text-muted-foreground/60 truncate min-w-0">
            {subtitle}
          </span>
        )}

        {/* Right-aligned stats */}
        {stats && (
          <span className={cn(
            "ml-auto text-xs text-muted-foreground/60 shrink-0",
            "font-[tabular-nums]",
            toolStatus === "running" && "motion-safe:animate-pulse",
          )}>
            {stats}
          </span>
        )}

        {/* Chevron — visible on hover when expandable */}
        {hasDetails && (
          <HugeiconsIcon
            icon={ArrowRight01Icon}
            strokeWidth={2}
            className={cn(
              "size-3 text-muted-foreground/40 shrink-0",
              "transition-[transform,opacity] duration-150",
              !stats && "ml-auto",
              "opacity-0 group-hover:opacity-100",
              expanded && "rotate-90 opacity-100",
            )}
          />
        )}
      </button>

      {/* Expanded result */}
      {expanded && resultText && (
        <div
          role="region"
          aria-label={`${display.label} result`}
          tabIndex={0}
          className="px-2.5 pt-1 pb-2 text-xs max-h-48 overflow-y-auto whitespace-pre-wrap break-all"
        >
          {resultText}
        </div>
      )}
      </div>
    </div>
  )
})
