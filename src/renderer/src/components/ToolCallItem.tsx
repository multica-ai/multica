/**
 * Tool call item component - displays tool calls with expandable details
 */
import { useState } from 'react'
import { ChevronRight } from 'lucide-react'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'

export interface ToolCall {
  id: string
  title: string
  status: string
  kind?: string
  input?: string
  output?: string
}

// Status dot component - displays tool call status with color and animation
function StatusDot({ status }: { status: string }) {
  const statusStyles: Record<string, string> = {
    pending: 'bg-[var(--tool-pending)]',
    running: 'bg-[var(--tool-running)] animate-[glow-pulse_2s_ease-in-out_infinite]',
    in_progress: 'bg-[var(--tool-running)] animate-[glow-pulse_2s_ease-in-out_infinite]',
    completed: 'bg-[var(--tool-success)]',
    failed: 'bg-[var(--tool-error)]',
  }

  return (
    <span
      className={cn(
        'h-1.5 w-1.5 rounded-full flex-shrink-0',
        statusStyles[status] || statusStyles.pending
      )}
    />
  )
}

// Tool call details - shows input and output separated by a line
function ToolCallDetails({ toolCall }: { toolCall: ToolCall }) {
  return (
    <div className="ml-4 mt-1 mb-2 bg-muted/50 rounded-md p-2">
      {/* Input */}
      {toolCall.input && (
        <div className="overflow-auto max-h-[120px]">
          <pre className="text-xs font-mono text-muted-foreground whitespace-pre-wrap break-all">
            {formatJson(toolCall.input)}
          </pre>
        </div>
      )}

      {/* Separator */}
      {toolCall.input && toolCall.output && (
        <div className="my-1.5 border-t border-border/40" />
      )}

      {/* Output */}
      {toolCall.output && (
        <div className="overflow-auto max-h-[160px]">
          <pre className="text-xs font-mono text-muted-foreground/70 whitespace-pre-wrap break-all">
            {toolCall.output}
          </pre>
        </div>
      )}
    </div>
  )
}

// Format JSON string for display
function formatJson(input: string): string {
  try {
    const parsed = JSON.parse(input)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return input
  }
}

// Tool call item - expandable display with input/output details
export function ToolCallItem({ toolCall }: { toolCall: ToolCall }) {
  const [isOpen, setIsOpen] = useState(false)

  const hasDetails = toolCall.input || toolCall.output
  const isFailed = toolCall.status === 'failed'
  const kind = toolCall.kind || 'tool'

  return (
    <Collapsible open={isOpen} onOpenChange={setIsOpen}>
      <CollapsibleTrigger
        className={cn(
          'group flex w-full items-center gap-2 rounded px-1.5 py-0.5',
          'text-sm transition-colors duration-100',
          'hover:bg-muted/20',
          hasDetails && 'cursor-pointer',
          !hasDetails && 'cursor-default'
        )}
        disabled={!hasDetails}
      >
        {/* Status dot */}
        <StatusDot status={toolCall.status} />

        {/* Kind */}
        <span className={cn(
          'text-secondary-foreground',
          isFailed && 'text-[var(--tool-error)]'
        )}>
          {kind}
        </span>

        {/* Title (file path etc) - truncate */}
        {toolCall.title && (
          <span className="truncate max-w-[400px] text-muted-foreground text-xs">
            {toolCall.title}
          </span>
        )}

        {/* Expand indicator */}
        {hasDetails && (
          <ChevronRight
            className={cn(
              'ml-auto h-3 w-3 text-muted-foreground/40 transition-all duration-150',
              'opacity-0 group-hover:opacity-100',
              isOpen && 'rotate-90 opacity-100'
            )}
          />
        )}
      </CollapsibleTrigger>

      {hasDetails && (
        <CollapsibleContent className="overflow-hidden">
          <ToolCallDetails toolCall={toolCall} />
        </CollapsibleContent>
      )}
    </Collapsible>
  )
}
