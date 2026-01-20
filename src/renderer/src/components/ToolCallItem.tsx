/**
 * Tool call item component - displays tool calls with expandable details
 */
import { useState, type ReactNode } from 'react'
import {
  ChevronRight,
  FileText,
  FilePen,
  Terminal,
  Search,
  Globe,
  Bot,
  ListTodo,
  Circle,
  MessageSquare
} from 'lucide-react'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'
import { usePermissionStore } from '../stores/permissionStore'
import { isQuestionTool } from '../../../shared/tool-names'

/**
 * Persisted response from AskUserQuestion (restored from session updates)
 */
export interface AnsweredResponse {
  optionId?: string // Optional: permission optionId (not needed for display)
  selectedOption?: string
  selectedOptions?: string[]
  customText?: string
  answers?: Array<{ question: string; answer: string }>
}

export interface ToolCall {
  id: string
  title: string
  status: string
  kind?: string
  toolName?: string // From _meta.claudeCode.toolName, most reliable tool name
  rawInput?: Record<string, unknown> // Raw input for subtitle display
  input?: string
  output?: string
  /** Persisted response for AskUserQuestion (restored from disk on restart) */
  answeredResponse?: AnsweredResponse
}

// Icon styles
const iconClass = 'h-3.5 w-3.5 text-muted-foreground'

// Calculate line count from output
function countLines(output: string | undefined): number {
  if (!output) return 0
  return output.split('\n').length
}

// Extract match count from output (for grep/search tools)
function extractMatchCount(output: string | undefined): number | undefined {
  if (!output) return undefined
  // Count non-empty lines as match count
  const lines = output.split('\n').filter((line) => line.trim())
  return lines.length > 0 ? lines.length : undefined
}

// Format file path (show filename only)
function formatPath(path: string | undefined): string | undefined {
  if (!path) return undefined
  const parts = path.split('/')
  return parts[parts.length - 1]
}

// Format command (handle array format)
function formatCommand(command: unknown): string {
  if (Array.isArray(command)) {
    return command.join(' ')
  }
  return String(command || '')
}

// Get tool display info (icon + name + subtitle + stats)
function getDisplayInfo(toolCall: ToolCall): {
  icon: ReactNode
  name: string
  subtitle?: string
  stats?: string // Right-side stats, e.g. "1027 lines" or "51 matches"
} {
  const { toolName, kind, title, rawInput, output, status } = toolCall
  const input = rawInput || {}
  const name = toolName || ''
  const isCompleted = status === 'completed'
  const isPending = status === 'pending' || status === 'running' || status === 'in_progress'

  // Map display info based on toolName or kind
  switch (name.toLowerCase() || kind) {
    case 'read': {
      const lines = isCompleted ? countLines(output) : 0
      const stats =
        isCompleted && lines > 0 ? `${lines} lines` : isPending ? 'reading...' : undefined
      return {
        icon: <FileText className={iconClass} />,
        name: 'Read',
        subtitle: formatPath(input.file_path as string),
        stats
      }
    }
    case 'write': {
      const content = input.content as string | undefined
      const lines = content ? content.split('\n').length : 0
      const stats = lines > 0 ? `${lines} lines` : undefined
      return {
        icon: <FilePen className={iconClass} />,
        name: 'Write',
        subtitle: formatPath(input.file_path as string),
        stats
      }
    }
    case 'edit': {
      return {
        icon: <FilePen className={iconClass} />,
        name: 'Edit',
        subtitle: formatPath(input.file_path as string)
      }
    }
    case 'bash':
    case 'execute': {
      return {
        icon: <Terminal className={iconClass} />,
        name: 'Terminal',
        subtitle: (input.description as string) || formatCommand(input.command).slice(0, 50),
        stats: isPending ? 'running...' : undefined
      }
    }
    case 'grep': {
      const matches = isCompleted ? extractMatchCount(output) : undefined
      const stats =
        matches !== undefined ? `${matches} matches` : isPending ? 'searching...' : undefined
      return {
        icon: <Search className={iconClass} />,
        name: `grep '${(input.pattern as string)?.slice(0, 20) || '...'}'`,
        subtitle: formatPath(input.path as string),
        stats
      }
    }
    case 'glob': {
      const matches = isCompleted ? extractMatchCount(output) : undefined
      const stats =
        matches !== undefined ? `${matches} files` : isPending ? 'searching...' : undefined
      return {
        icon: <Search className={iconClass} />,
        name: 'Glob',
        subtitle: input.pattern as string,
        stats
      }
    }
    case 'search': {
      const matches = isCompleted ? extractMatchCount(output) : undefined
      const stats =
        matches !== undefined ? `${matches} matches` : isPending ? 'searching...' : undefined
      return {
        icon: <Search className={iconClass} />,
        name: 'Search',
        subtitle: input.pattern as string,
        stats
      }
    }
    case 'websearch': {
      return {
        icon: <Globe className={iconClass} />,
        name: 'Web Search',
        subtitle: input.query as string,
        stats: isPending ? 'searching...' : undefined
      }
    }
    case 'webfetch': {
      return {
        icon: <Globe className={iconClass} />,
        name: 'Web Fetch',
        subtitle: input.url as string,
        stats: isPending ? 'fetching...' : undefined
      }
    }
    case 'fetch': {
      return {
        icon: <Globe className={iconClass} />,
        name: 'Web Search',
        subtitle: (input.query as string) || (input.url as string),
        stats: isPending ? 'searching...' : undefined
      }
    }
    case 'task': {
      return {
        icon: <Bot className={iconClass} />,
        name: `${input.subagent_type || 'Task'} Agent`,
        subtitle: input.description as string,
        stats: isPending ? 'working...' : undefined
      }
    }
    case 'todowrite': {
      return { icon: <ListTodo className={iconClass} />, name: 'TodoWrite' }
    }
    case 'askuserquestion':
    case 'mcp__conductor__askuserquestion':
    case 'question': {
      // Extract question header from rawInput
      // Supports both Claude Code's 'AskUserQuestion' and OpenCode's 'question' tool names
      const questions = (input.questions as Array<{ header?: string; question?: string }>) || []
      const firstQuestion = questions[0]
      const header = firstQuestion?.header || 'Question'
      return {
        icon: <MessageSquare className={iconClass} />,
        name: header,
        stats: isPending ? 'waiting...' : undefined
      }
    }
    default: {
      // fallback: use toolName or title or kind
      return { icon: <Circle className={iconClass} />, name: toolName || title || kind || 'Tool' }
    }
  }
}

// Status dot component - displays tool call status with color and animation
function StatusDot({ status }: { status: string }): React.JSX.Element {
  const statusStyles: Record<string, string> = {
    pending: 'bg-[var(--tool-pending)]',
    running: 'bg-[var(--tool-running)] animate-[glow-pulse_2s_ease-in-out_infinite]',
    in_progress: 'bg-[var(--tool-running)] animate-[glow-pulse_2s_ease-in-out_infinite]',
    completed: 'bg-[var(--tool-success)]',
    failed: 'bg-[var(--tool-error)]'
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
function ToolCallDetails({ toolCall }: { toolCall: ToolCall }): React.JSX.Element {
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
      {toolCall.input && toolCall.output && <div className="my-1.5 border-t border-border/40" />}

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
export function ToolCallItem({ toolCall }: { toolCall: ToolCall }): React.JSX.Element {
  const [isOpen, setIsOpen] = useState(false)

  // Check if this is an AskUserQuestion/question that has been responded to
  // Supports both Claude Code's 'AskUserQuestion' and OpenCode's 'question' tool names
  const isAskUserQuestion = isQuestionTool(toolCall.toolName) || isQuestionTool(toolCall.title)

  // Check for response in memory store (current session)
  const respondedData = usePermissionStore((s) =>
    isAskUserQuestion ? s.getRespondedByToolCallId(toolCall.id) : undefined
  )

  // Use persisted response (from disk) if memory response not available
  // This handles app restart where memory is cleared but session data persists
  const persistedResponse = toolCall.answeredResponse

  // For AskUserQuestion that has been answered, show completed state with selection
  const hasResponse = respondedData || persistedResponse
  if (isAskUserQuestion && hasResponse) {
    // Prefer memory response, fallback to persisted response
    const response = respondedData?.response || persistedResponse
    // Handle both single and multi-select answers
    const selectedAnswer =
      response?.selectedOptions?.join(', ') ||
      response?.selectedOption ||
      response?.customText ||
      response?.answers?.[0]?.answer ||
      'Answered'
    const questions = (toolCall.rawInput?.questions as Array<{ header?: string }>) || []
    const header = questions[0]?.header || 'Question'

    return (
      <div className="flex items-center gap-2 px-1.5 py-0.5 text-sm">
        <span className="h-1.5 w-1.5 rounded-full bg-[var(--tool-success)] flex-shrink-0" />
        <MessageSquare className="h-3.5 w-3.5 text-muted-foreground" />
        <span className="text-secondary-foreground">{header}</span>
        <span className="text-muted-foreground">– {selectedAnswer}</span>
      </div>
    )
  }

  const hasDetails = toolCall.input || toolCall.output
  const isFailed = toolCall.status === 'failed'

  // Use getDisplayInfo to get icon, name, subtitle, and stats
  const { icon, name, subtitle, stats } = getDisplayInfo(toolCall)
  const isPending =
    toolCall.status === 'pending' ||
    toolCall.status === 'running' ||
    toolCall.status === 'in_progress'

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

        {/* Icon */}
        {icon}

        {/* Tool name */}
        <span
          className={cn(
            'text-secondary-foreground flex-shrink-0',
            isFailed && 'text-[var(--tool-error)]'
          )}
        >
          {name}
        </span>

        {/* Subtitle (path, query, etc.) */}
        {subtitle && (
          <span className="text-muted-foreground truncate max-w-[300px]">{subtitle}</span>
        )}

        {/* Stats (line count, match count, etc.) - shown on the right */}
        {stats && (
          <span
            className={cn(
              'ml-auto text-xs text-muted-foreground/70 flex-shrink-0',
              isPending && 'animate-pulse'
            )}
          >
            {stats}
          </span>
        )}

        {/* Expand indicator */}
        {hasDetails && (
          <ChevronRight
            className={cn(
              'h-3 w-3 text-muted-foreground/40 transition-all duration-150',
              !stats && 'ml-auto',
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
