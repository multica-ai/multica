/**
 * CompletedMessageFooter - Footer for completed assistant messages
 *
 * Shows duration and action buttons (copy, etc.)
 */
import { useState } from 'react'
import { Copy, Check } from 'lucide-react'
import { toast } from 'sonner'
import { cn, formatDuration, formatLocalizedDatetime } from '@/lib/utils'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { Button } from '@/components/ui/button'

/** Duration to show copy success feedback (in milliseconds) */
const COPY_FEEDBACK_DURATION_MS = 1500

export interface CompletedMessageFooterProps {
  /** Duration in milliseconds */
  durationMs: number
  /** Start time for tooltip (ISO 8601 string or timestamp) */
  startTime?: string | number
  /** Content to copy (for future implementation) */
  content?: string
  /** Model name for tooltip */
  modelName?: string
  /** Agent name for tooltip */
  agentName?: string
  /** Additional className */
  className?: string
}

/**
 * CompletedMessageFooter - Footer for completed assistant messages
 *
 * Shows:
 * - Duration with tooltip (agent/model info + start time)
 * - Copy button (ghost variant, small icon)
 */
export function CompletedMessageFooter({
  durationMs,
  startTime,
  content,
  modelName,
  agentName,
  className
}: CompletedMessageFooterProps): React.JSX.Element | null {
  // Track copy success state for visual feedback
  const [copied, setCopied] = useState(false)

  // Don't show if duration is less than 1 second
  if (durationMs < 1000) {
    return null
  }

  // Parse start time for tooltip
  const startMs = startTime
    ? typeof startTime === 'number'
      ? startTime
      : new Date(startTime).getTime()
    : null

  // Handle copy action with visual feedback
  const handleCopy = async (): Promise<void> => {
    if (!content || copied) return
    try {
      await navigator.clipboard.writeText(content)
      setCopied(true)
      setTimeout(() => setCopied(false), COPY_FEEDBACK_DURATION_MS)
    } catch (err) {
      console.error('[CompletedMessageFooter] Failed to copy:', err)
      toast.error('Failed to copy content')
    }
  }

  // Build tooltip content for duration
  const durationTooltip = (
    <div className="space-y-1">
      {(modelName || agentName) && (
        <div className="font-medium">
          {agentName && modelName ? `${agentName} · ${modelName}` : agentName || modelName}
        </div>
      )}
      {startMs && <div className="text-muted-foreground">{formatLocalizedDatetime(startMs)}</div>}
    </div>
  )

  const hasDurationTooltip = modelName || agentName || startMs

  return (
    <div className={cn('flex items-center gap-2', className)}>
      {/* Duration */}
      {hasDurationTooltip ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="text-xs text-muted-foreground/50 cursor-default">
              {formatDuration(durationMs)}
            </span>
          </TooltipTrigger>
          <TooltipContent side="top" className="max-w-xs">
            {durationTooltip}
          </TooltipContent>
        </Tooltip>
      ) : (
        <span className="text-xs text-muted-foreground/50">{formatDuration(durationMs)}</span>
      )}

      {/* Copy button */}
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant={copied ? 'secondary' : 'ghost'}
            size="icon-sm"
            className={cn(
              'h-6 w-6 transition-all',
              !copied && 'text-muted-foreground/50 hover:text-muted-foreground'
            )}
            onClick={handleCopy}
          >
            {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          </Button>
        </TooltipTrigger>
        <TooltipContent side="top">Copy content</TooltipContent>
      </Tooltip>
    </div>
  )
}
