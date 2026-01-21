/**
 * LoadingIndicator - Spinner with optional label and elapsed time
 * Inspired by craft-agents-oss SpinKit Grid design
 */
import { useState, useEffect, useRef } from 'react'
import { cn, formatDuration, formatLocalizedDatetime } from '@/lib/utils'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'

// Re-export Spinner from dedicated component file
export { Spinner, type SpinnerProps } from './Spinner'
import { Spinner } from './Spinner'

export interface StatusIndicatorProps {
  /** Optional elapsed time in milliseconds */
  time?: number
  /** Status label text */
  label: string
  /** Additional className */
  className?: string
}

/**
 * StatusIndicator - Reusable status display with optional time
 * Order: [Spinner] [time] [label]
 * - Spinner always first (uses theme color via currentColor)
 * - Time second (if provided)
 * - Label third
 * - Uses smallest font size (text-xs) for subtle appearance
 */
export function StatusIndicator({
  time,
  label,
  className
}: StatusIndicatorProps): React.JSX.Element {
  return (
    <div className={cn('flex items-center gap-2 text-xs text-muted-foreground', className)}>
      <Spinner className="text-xs" />
      {time !== undefined && (
        <span className="text-muted-foreground/60">{formatDuration(time)}</span>
      )}
      <span className="text-muted-foreground/80">{label}</span>
    </div>
  )
}

export interface LoadingIndicatorProps {
  /** Optional label to show next to spinner */
  label?: string
  /** Whether to animate the spinner */
  animated?: boolean
  /** Show elapsed time (pass start timestamp or true to auto-track) */
  showElapsed?: boolean | number
  /** Additional className for the container */
  className?: string
  /** Additional className for the spinner */
  spinnerClassName?: string
}

/**
 * LoadingIndicator - Spinner with optional label and elapsed time
 *
 * Inherits text color and size from parent element.
 *
 * Features:
 * - Animated 3x3 dot grid spinner (CSS-only)
 * - Optional label text
 * - Optional elapsed time display (shows after 1 second)
 */
export function LoadingIndicator({
  label,
  animated = true,
  showElapsed = false,
  className,
  spinnerClassName
}: LoadingIndicatorProps): React.JSX.Element {
  const [elapsed, setElapsed] = useState(0)
  const startTimeRef = useRef<number | null>(null)

  // Elapsed time tracking
  useEffect(() => {
    if (!showElapsed) return

    // Initialize start time
    if (typeof showElapsed === 'number') {
      startTimeRef.current = showElapsed
    } else if (!startTimeRef.current) {
      startTimeRef.current = Date.now()
    }

    const interval = setInterval(() => {
      if (startTimeRef.current) {
        setElapsed(Date.now() - startTimeRef.current)
      }
    }, 1000)

    return () => clearInterval(interval)
  }, [showElapsed])

  return (
    <span className={cn('inline-flex items-center gap-2', className)}>
      {/* Spinner */}
      {animated ? (
        <Spinner className={spinnerClassName} />
      ) : (
        <span className="inline-flex items-center justify-center w-[1em] h-[1em]">●</span>
      )}

      {/* Label */}
      {label && <span className="text-muted-foreground">{label}</span>}

      {/* Elapsed time - only show after 1 second to avoid flicker */}
      {showElapsed && elapsed >= 1000 && (
        <span className="text-muted-foreground/60">{formatDuration(elapsed)}</span>
      )}
    </span>
  )
}

/** Current agent action for dynamic status label */
export interface CurrentAction {
  type: 'thinking' | 'writing' | 'tool'
  toolName?: string // e.g., "Bash", "Read", "Search"
  toolStatus?: string // e.g., "pending", "running", "in_progress", "completed"
}

export interface MessageTimerProps {
  /** Start time (ISO 8601 string or timestamp) */
  startTime?: string | number
  /** End time (ISO 8601 string or timestamp) - if provided, shows final duration */
  endTime?: string | number
  /** Whether the message is still processing */
  isProcessing?: boolean
  /** Label to show (overrides dynamic label) */
  label?: string
  /** Additional className */
  className?: string
  /** Model name for tooltip display (e.g., "Opus 4.5") */
  modelName?: string
  /** Agent name for tooltip display (e.g., "Claude Code") */
  agentName?: string
  /** Current agent action for dynamic label */
  currentAction?: CurrentAction
}

/**
 * MessageTimer - Timer display for message duration
 *
 * Shows:
 * - Spinner + label + elapsed time when processing
 * - Final duration when complete
 *
 * Time calculation: Uses endTime - startTime directly.
 * Since endTime updates with each sessionUpdate, no interval needed.
 */
export function MessageTimer({
  startTime,
  endTime,
  isProcessing = false,
  label,
  className,
  modelName,
  agentName,
  currentAction
}: MessageTimerProps): React.JSX.Element | null {
  // Get dynamic status label based on current action
  const getStatusLabel = (): string => {
    if (label) return label // Allow override
    if (!currentAction) return 'Thinking...'

    switch (currentAction.type) {
      case 'thinking':
        return 'Thinking...'
      case 'writing':
        return 'Writing...'
      case 'tool': {
        // Show contextual label based on tool name (matching ToolCallItem patterns)
        const name = currentAction.toolName?.toLowerCase() || ''
        switch (name) {
          case 'read':
            return 'Reading...'
          case 'write':
          case 'edit':
            return 'Writing...'
          case 'bash':
          case 'execute':
            return 'Running...'
          case 'grep':
          case 'glob':
          case 'search':
          case 'websearch':
          case 'fetch':
            return 'Searching...'
          case 'webfetch':
            return 'Fetching...'
          case 'task':
            return 'Working...'
          default:
            return currentAction.toolName ? `Running ${currentAction.toolName}...` : 'Working...'
        }
      }
      default:
        return 'Working...'
    }
  }

  // Parse timestamps
  const startMs = startTime
    ? typeof startTime === 'number'
      ? startTime
      : new Date(startTime).getTime()
    : null
  const endMs = endTime
    ? typeof endTime === 'number'
      ? endTime
      : new Date(endTime).getTime()
    : null

  // Calculate elapsed directly from props (no useState/useEffect needed)
  // endTime updates with each sessionUpdate, triggering re-render automatically
  const elapsedMs = startMs && endMs ? endMs - startMs : 0

  // Don't show anything if no start time
  if (!startMs) return null

  // Processing state: show spinner + time + label
  // Order: [Spinner] [time] [label] - uses smallest font (text-xs)
  // Time only shown when endTime is available (elapsedMs > 0)
  if (isProcessing) {
    return (
      <div className={cn('flex items-center gap-2 text-xs text-muted-foreground', className)}>
        <Spinner className="text-xs" />
        {elapsedMs > 0 && (
          <span className="text-muted-foreground/60">{formatDuration(elapsedMs)}</span>
        )}
        <span className="text-muted-foreground/80">{getStatusLabel()}</span>
      </div>
    )
  }

  // Completed state: show final duration (only if > 1 second)
  if (elapsedMs < 1000) {
    return null
  }

  // State 3 edge case: isProcessing=false but no endTime
  // Show fallback with "Summarizing..." label
  if (!endMs) {
    return <StatusIndicator time={elapsedMs} label="Summarizing..." className={className} />
  }

  // Check if we have tooltip content
  const hasTooltipContent = modelName || agentName || startMs

  // Build tooltip content
  const tooltipContent = (
    <div className="space-y-1">
      {/* Agent (left) · Model (right) */}
      {(modelName || agentName) && (
        <div className="font-medium">
          {agentName && modelName ? `${agentName} · ${modelName}` : agentName || modelName}
        </div>
      )}
      {/* Start time only */}
      {startMs && <div className="text-muted-foreground">{formatLocalizedDatetime(startMs)}</div>}
    </div>
  )

  // If we have tooltip info, wrap in tooltip
  if (hasTooltipContent) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span className={cn('text-xs text-muted-foreground/50 cursor-default', className)}>
            {formatDuration(elapsedMs)}
          </span>
        </TooltipTrigger>
        <TooltipContent side="top" className="max-w-xs">
          {tooltipContent}
        </TooltipContent>
      </Tooltip>
    )
  }

  return (
    <span className={cn('text-xs text-muted-foreground/50', className)}>
      {formatDuration(elapsedMs)}
    </span>
  )
}
