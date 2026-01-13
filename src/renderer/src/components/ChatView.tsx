/**
 * Chat view component - displays messages and tool calls
 */
import { useEffect, useRef, useState } from 'react'
import type { StoredSessionUpdate } from '../../../shared/types'

interface ChatViewProps {
  updates: StoredSessionUpdate[]
  isProcessing: boolean
  hasSession: boolean
  onNewSession?: () => void
}

export function ChatView({ updates, isProcessing, hasSession, onNewSession }: ChatViewProps) {
  const bottomRef = useRef<HTMLDivElement>(null)

  // Auto-scroll to bottom when new messages arrive
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [updates])

  // Group updates into messages
  const messages = groupUpdatesIntoMessages(updates)

  if (messages.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <div className="text-center">
          <h1 className="mb-2 text-3xl font-bold">Multica</h1>
          <p className="mb-4 text-[var(--color-text-muted)]">
            {hasSession
              ? 'Start a conversation with your coding agent'
              : 'Create a session to start chatting'}
          </p>
          {!hasSession && onNewSession && (
            <button
              onClick={onNewSession}
              className="rounded-lg bg-[var(--color-primary)] px-4 py-2 font-medium text-white transition-colors hover:bg-[var(--color-primary-dark)]"
            >
              New Session
            </button>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto max-w-3xl space-y-4 p-4">
        {messages.map((msg, idx) => (
          <MessageBubble key={idx} message={msg} />
        ))}

        {isProcessing && (
          <div className="flex items-center gap-2 text-[var(--color-text-muted)]">
            <LoadingDots />
            <span className="text-sm">Agent is thinking...</span>
          </div>
        )}

        <div ref={bottomRef} />
      </div>
    </div>
  )
}

interface Message {
  role: 'user' | 'assistant'
  content: string
  thought: string
  toolCalls: ToolCall[]
}

interface ToolCall {
  id: string
  title: string
  status: string
  kind?: string
  input?: string
  output?: string
}

function groupUpdatesIntoMessages(updates: StoredSessionUpdate[]): Message[] {
  const messages: Message[] = []
  let currentAssistantContent = ''
  let currentThought = ''
  let currentToolCalls: ToolCall[] = []
  const toolCallMap = new Map<string, ToolCall>()

  for (const stored of updates) {
    // The stored.update is SessionNotification which has { sessionId, update }
    const notification = stored.update
    const update = notification?.update
    if (!update || !('sessionUpdate' in update)) {
      continue
    }

    switch (update.sessionUpdate) {
      case 'user_message' as string:
        // Flush any pending assistant message
        if (currentAssistantContent || currentThought || currentToolCalls.length > 0) {
          messages.push({
            role: 'assistant',
            content: currentAssistantContent,
            thought: currentThought,
            toolCalls: currentToolCalls,
          })
          currentAssistantContent = ''
          currentThought = ''
          currentToolCalls = []
          toolCallMap.clear()
        }
        // Add user message
        {
          const userUpdate = update as { content?: { type: string; text: string } }
          if (userUpdate.content?.type === 'text') {
            messages.push({
              role: 'user',
              content: userUpdate.content.text,
              thought: '',
              toolCalls: [],
            })
          }
        }
        break

      case 'agent_message_chunk':
        // APPEND chunks instead of replacing
        if ('content' in update && update.content?.type === 'text') {
          currentAssistantContent += update.content.text
        }
        break

      case 'agent_thought_chunk':
        // Accumulate thought chunks
        if ('content' in update && update.content?.type === 'text') {
          currentThought += update.content.text
        }
        break

      case 'tool_call':
        if ('toolCallId' in update) {
          const toolCall: ToolCall = {
            id: update.toolCallId,
            title: update.title || 'Tool Call',
            status: update.status || 'pending',
            kind: update.kind,
            input: typeof update.rawInput === 'string'
              ? update.rawInput
              : update.rawInput && Object.keys(update.rawInput).length > 0
                ? JSON.stringify(update.rawInput, null, 2)
                : undefined,
          }
          toolCallMap.set(update.toolCallId, toolCall)
          currentToolCalls = Array.from(toolCallMap.values())
        }
        break

      case 'tool_call_update':
        if ('toolCallId' in update) {
          // Get or create the tool call entry
          const existingTool = toolCallMap.get(update.toolCallId)
          if (existingTool) {
            if (update.status) existingTool.status = update.status
            if (update.title) existingTool.title = update.title
            if (update.rawInput && Object.keys(update.rawInput).length > 0) {
              existingTool.input = typeof update.rawInput === 'string'
                ? update.rawInput
                : JSON.stringify(update.rawInput, null, 2)
            }
            if (update.rawOutput) {
              existingTool.output = typeof update.rawOutput === 'string'
                ? update.rawOutput
                : JSON.stringify(update.rawOutput, null, 2)
            }
          } else {
            // Create new entry if we see update before the initial tool_call
            const newTool: ToolCall = {
              id: update.toolCallId,
              title: update.title || 'Tool Call',
              status: update.status || 'pending',
              kind: update.kind ?? undefined,
            }
            if (update.rawInput && Object.keys(update.rawInput).length > 0) {
              newTool.input = typeof update.rawInput === 'string'
                ? update.rawInput
                : JSON.stringify(update.rawInput, null, 2)
            }
            toolCallMap.set(update.toolCallId, newTool)
          }
          currentToolCalls = Array.from(toolCallMap.values())
        }
        break
    }
  }

  // Flush any remaining assistant content
  if (currentAssistantContent || currentThought || currentToolCalls.length > 0) {
    messages.push({
      role: 'assistant',
      content: currentAssistantContent.trim(),
      thought: currentThought.trim(),
      toolCalls: currentToolCalls,
    })
  }

  return messages
}

interface MessageBubbleProps {
  message: Message
}

function MessageBubble({ message }: MessageBubbleProps) {
  const isUser = message.role === 'user'

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div
        className={`max-w-[85%] rounded-lg px-4 py-2 ${
          isUser
            ? 'bg-[var(--color-primary)] text-white'
            : 'bg-[var(--color-surface)]'
        }`}
      >
        {/* Thought (agent reasoning) */}
        {message.thought && (
          <div className="mb-2 rounded border-l-2 border-purple-500/50 bg-purple-500/10 py-1 pl-2 text-xs text-[var(--color-text-muted)] italic">
            {message.thought}
          </div>
        )}

        {/* Tool calls */}
        {message.toolCalls.length > 0 && (
          <div className="mb-2 space-y-2">
            {message.toolCalls.map((tc) => (
              <ToolCallCard key={tc.id} toolCall={tc} />
            ))}
          </div>
        )}

        {/* Text content */}
        {message.content && (
          <div className="whitespace-pre-wrap text-sm">{message.content}</div>
        )}
      </div>
    </div>
  )
}

interface ToolCallCardProps {
  toolCall: ToolCall
}

function ToolCallCard({ toolCall }: ToolCallCardProps) {
  const [expanded, setExpanded] = useState(false)

  // Get icon based on tool kind
  const icon = getToolIcon(toolCall.kind, toolCall.title)

  // Get clean title
  const cleanTitle = getCleanTitle(toolCall.title, toolCall.kind)

  // Status styling
  const statusConfig = {
    completed: { icon: '✓', color: 'text-green-500', bg: 'bg-green-500/10' },
    failed: { icon: '✗', color: 'text-red-500', bg: 'bg-red-500/10' },
    running: { icon: null, color: 'text-blue-500', bg: 'bg-blue-500/10' },
    in_progress: { icon: null, color: 'text-blue-500', bg: 'bg-blue-500/10' },
    pending: { icon: '○', color: 'text-yellow-500', bg: 'bg-yellow-500/10' },
  }
  const status = statusConfig[toolCall.status as keyof typeof statusConfig] || statusConfig.pending

  const hasDetails = toolCall.input || toolCall.output

  return (
    <div
      className={`rounded-lg border border-[var(--color-border)] ${status.bg} overflow-hidden text-xs`}
    >
      {/* Header - always visible */}
      <div
        className={`flex items-center gap-2 px-3 py-2 ${hasDetails ? 'cursor-pointer hover:bg-black/5' : ''}`}
        onClick={() => hasDetails && setExpanded(!expanded)}
      >
        {/* Tool icon */}
        <span className="text-base">{icon}</span>

        {/* Title */}
        <span className="flex-1 font-medium truncate">{cleanTitle}</span>

        {/* Status */}
        <span className={`flex items-center gap-1 ${status.color}`}>
          {status.icon ? (
            <span>{status.icon}</span>
          ) : (
            <LoadingDots />
          )}
          <span className="text-[10px] uppercase">{toolCall.status}</span>
        </span>

        {/* Expand icon */}
        {hasDetails && (
          <span className={`text-[var(--color-text-muted)] transition-transform ${expanded ? 'rotate-180' : ''}`}>
            ▼
          </span>
        )}
      </div>

      {/* Details - collapsible */}
      {expanded && hasDetails && (
        <div className="border-t border-[var(--color-border)] bg-black/10 px-3 py-2 space-y-2">
          {toolCall.input && (
            <div>
              <div className="text-[10px] uppercase text-[var(--color-text-muted)] mb-1">Input</div>
              <pre className="max-h-32 overflow-auto rounded bg-black/20 p-2 text-[var(--color-text-muted)] whitespace-pre-wrap break-all">
                {toolCall.input}
              </pre>
            </div>
          )}
          {toolCall.output && (
            <div>
              <div className="text-[10px] uppercase text-[var(--color-text-muted)] mb-1">Output</div>
              <pre className="max-h-32 overflow-auto rounded bg-black/20 p-2 text-[var(--color-text-muted)] whitespace-pre-wrap break-all">
                {toolCall.output.slice(0, 500)}
                {toolCall.output.length > 500 && '...'}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function getToolIcon(kind?: string, title?: string): string {
  const lowerKind = kind?.toLowerCase() || ''
  const lowerTitle = title?.toLowerCase() || ''

  // Match by kind first
  if (lowerKind.includes('search') || lowerKind.includes('grep') || lowerKind.includes('glob')) {
    return '🔍'
  }
  if (lowerKind.includes('file') || lowerKind.includes('read') || lowerKind.includes('write')) {
    return '📄'
  }
  if (lowerKind.includes('edit')) {
    return '✏️'
  }
  if (lowerKind.includes('bash') || lowerKind.includes('shell') || lowerKind.includes('command')) {
    return '⌨️'
  }

  // Match by title
  if (lowerTitle.includes('list') || lowerTitle.includes('ls')) {
    return '📁'
  }
  if (lowerTitle.includes('read')) {
    return '📖'
  }
  if (lowerTitle.includes('write') || lowerTitle.includes('create')) {
    return '📝'
  }
  if (lowerTitle.includes('edit') || lowerTitle.includes('replace')) {
    return '✏️'
  }
  if (lowerTitle.includes('run') || lowerTitle.includes('exec') || lowerTitle.includes('bash')) {
    return '⌨️'
  }
  if (lowerTitle.includes('search') || lowerTitle.includes('find') || lowerTitle.includes('grep')) {
    return '🔍'
  }
  if (lowerTitle.includes('glob')) {
    return '🔍'
  }

  return '🔧'
}

function getCleanTitle(title?: string, kind?: string): string {
  if (!title) return kind || 'Tool Call'

  // If title is too long or looks like a command, try to extract meaningful part
  const cleanTitle = title.trim()

  // If it starts with "Run " and has a long command, shorten it
  if (cleanTitle.startsWith('Run ') && cleanTitle.length > 50) {
    const cmd = cleanTitle.slice(4).split(' ')[0]
    return `Run ${cmd}`
  }

  // If it's a file path, show just the filename
  if (cleanTitle.includes('/') && !cleanTitle.includes(' ')) {
    const parts = cleanTitle.split('/')
    return parts[parts.length - 1] || cleanTitle
  }

  // Truncate if too long
  if (cleanTitle.length > 60) {
    return cleanTitle.slice(0, 57) + '...'
  }

  return cleanTitle
}

function LoadingDots() {
  return (
    <span className="inline-flex gap-1">
      <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-current" style={{ animationDelay: '0ms' }} />
      <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-current" style={{ animationDelay: '150ms' }} />
      <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-current" style={{ animationDelay: '300ms' }} />
    </span>
  )
}
