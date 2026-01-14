/**
 * Chat view component - displays messages and tool calls
 */
import { useEffect, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import type { StoredSessionUpdate } from '../../../shared/types'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { ChevronDown } from 'lucide-react'

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
            <Button onClick={onNewSession}>
              New Session
            </Button>
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

  // User message - bubble style
  if (isUser) {
    return (
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-lg bg-[var(--color-surface)] px-4 py-3 text-sm">
          {message.content}
        </div>
      </div>
    )
  }

  // Assistant message - flat list style
  return (
    <div className="space-y-3">
      {/* Thought - expanded, collapsible */}
      {message.thought && (
        <ThoughtBlock text={message.thought} />
      )}

      {/* Tool calls */}
      {message.toolCalls.map((tc) => (
        <ToolCallLine key={tc.id} toolCall={tc} />
      ))}

      {/* Text content with markdown */}
      {message.content && (
        <div className="prose prose-invert prose-sm max-w-none">
          <ReactMarkdown
            components={{
              // Custom styling for markdown elements
              p: ({ children }) => <p className="mb-3 last:mb-0 leading-relaxed">{children}</p>,
              h1: ({ children }) => <h1 className="text-xl font-bold mb-3 mt-4">{children}</h1>,
              h2: ({ children }) => <h2 className="text-lg font-bold mb-2 mt-3">{children}</h2>,
              h3: ({ children }) => <h3 className="text-base font-semibold mb-2 mt-3">{children}</h3>,
              ul: ({ children }) => <ul className="list-disc pl-4 mb-3 space-y-1">{children}</ul>,
              ol: ({ children }) => <ol className="list-decimal pl-4 mb-3 space-y-1">{children}</ol>,
              li: ({ children }) => <li className="leading-relaxed">{children}</li>,
              code: ({ className, children }) => {
                const isBlock = className?.includes('language-')
                if (isBlock) {
                  return (
                    <code className="block bg-[var(--color-surface)] rounded-lg p-3 text-xs font-mono overflow-x-auto">
                      {children}
                    </code>
                  )
                }
                return (
                  <code className="bg-[var(--color-surface)] rounded px-1.5 py-0.5 text-xs font-mono">
                    {children}
                  </code>
                )
              },
              pre: ({ children }) => (
                <pre className="bg-[var(--color-surface)] rounded-lg p-3 mb-3 overflow-x-auto text-xs">
                  {children}
                </pre>
              ),
              a: ({ href, children }) => (
                <a href={href} className="text-[var(--color-accent)] hover:underline" target="_blank" rel="noopener noreferrer">
                  {children}
                </a>
              ),
              blockquote: ({ children }) => (
                <blockquote className="border-l-2 border-[var(--color-border)] pl-3 italic text-[var(--color-text-muted)]">
                  {children}
                </blockquote>
              ),
              hr: () => <hr className="border-[var(--color-border)] my-4" />,
              strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
              em: ({ children }) => <em className="italic">{children}</em>,
            }}
          >
            {message.content}
          </ReactMarkdown>
        </div>
      )}
    </div>
  )
}

// Thought block - expanded, collapsible
function ThoughtBlock({ text }: { text: string }) {
  const [isExpanded, setIsExpanded] = useState(false)
  const isLong = text.length > 200

  return (
    <Collapsible open={isExpanded} onOpenChange={setIsExpanded}>
      <div className="rounded-lg border bg-card/50 p-3">
        <CollapsibleTrigger className="flex w-full items-center gap-2 text-left text-sm text-muted-foreground">
          <span className="opacity-60">⊛</span>
          <span className="font-medium">Thinking</span>
          {isLong && (
            <ChevronDown className={`ml-auto h-4 w-4 transition-transform ${isExpanded ? 'rotate-180' : ''}`} />
          )}
        </CollapsibleTrigger>

        <CollapsibleContent>
          <div className="mt-2 text-sm text-muted-foreground">
            <ReactMarkdown
              components={{
                p: ({ children }) => <p className="mb-2 last:mb-0">{children}</p>,
                code: ({ children }) => (
                  <code className="bg-background rounded px-1 py-0.5 text-xs font-mono">
                    {children}
                  </code>
                ),
              }}
            >
              {text}
            </ReactMarkdown>
          </div>
        </CollapsibleContent>

        {/* Preview when collapsed */}
        {!isExpanded && isLong && (
          <div className="mt-2 text-sm text-muted-foreground line-clamp-3">
            <ReactMarkdown
              components={{
                p: ({ children }) => <p className="mb-2 last:mb-0">{children}</p>,
                code: ({ children }) => (
                  <code className="bg-background rounded px-1 py-0.5 text-xs font-mono">
                    {children}
                  </code>
                ),
              }}
            >
              {text}
            </ReactMarkdown>
          </div>
        )}

        {/* Show full content when not long */}
        {!isLong && (
          <div className="mt-2 text-sm text-muted-foreground">
            <ReactMarkdown
              components={{
                p: ({ children }) => <p className="mb-2 last:mb-0">{children}</p>,
                code: ({ children }) => (
                  <code className="bg-background rounded px-1 py-0.5 text-xs font-mono">
                    {children}
                  </code>
                ),
              }}
            >
              {text}
            </ReactMarkdown>
          </div>
        )}
      </div>
    </Collapsible>
  )
}

// Tool call line - inline display
function ToolCallLine({ toolCall }: { toolCall: ToolCall }) {
  const { icon, action, detail } = parseToolCall(toolCall)

  const isRunning = toolCall.status === 'running' || toolCall.status === 'in_progress' || toolCall.status === 'pending'
  const isFailed = toolCall.status === 'failed'

  return (
    <div className={`flex items-center gap-2 text-sm ${isFailed ? 'text-red-400' : 'text-[var(--color-text-muted)]'}`}>
      {/* Icon */}
      <span className="w-4 text-center font-mono opacity-60">{icon}</span>

      {/* Action */}
      <span>{action}</span>

      {/* Detail in code pill */}
      {detail && (
        <span className="rounded bg-[var(--color-surface)] px-2 py-0.5 font-mono text-xs truncate max-w-[300px]">
          {detail}
        </span>
      )}

      {/* Running indicator */}
      {isRunning && <LoadingDots />}
    </div>
  )
}

interface ParsedToolCall {
  icon: string
  action: string
  detail?: string
}

function parseToolCall(toolCall: ToolCall): ParsedToolCall {
  const title = toolCall.title?.toLowerCase() || ''
  const kind = toolCall.kind?.toLowerCase() || ''

  // Try to extract file path from input
  let filePath: string | undefined
  if (toolCall.input) {
    try {
      const parsed = JSON.parse(toolCall.input)
      filePath = parsed.file_path || parsed.path || parsed.filePath || parsed.pattern
    } catch {
      // Not JSON, might be a direct path
      if (toolCall.input.startsWith('/') || toolCall.input.includes('.')) {
        filePath = toolCall.input.split('\n')[0].trim()
      }
    }
  }

  // Determine icon and action based on kind/title
  if (kind === 'search' || title.includes('glob') || title.includes('search') || title.includes('grep')) {
    return {
      icon: '◎',
      action: title.includes('glob') ? 'Search files' : 'Search',
      detail: filePath || extractPathFromTitle(toolCall.title),
    }
  }

  if (title.includes('list') || title.startsWith('ls')) {
    return {
      icon: '▤',
      action: 'List',
      detail: extractPathFromTitle(toolCall.title),
    }
  }

  if (title.includes('read')) {
    return {
      icon: '◔',
      action: 'Read',
      detail: filePath || extractPathFromTitle(toolCall.title),
    }
  }

  if (title.includes('write') || title.includes('create')) {
    // Try to get line count from output
    let lineCount = ''
    if (toolCall.output) {
      const lines = toolCall.output.split('\n').length
      if (lines > 1) lineCount = `${lines} lines`
    }
    return {
      icon: '▤',
      action: lineCount ? `Write ${lineCount}` : 'Write',
      detail: filePath || extractPathFromTitle(toolCall.title),
    }
  }

  if (title.includes('edit') || title.includes('replace')) {
    return {
      icon: '✎',
      action: 'Edit',
      detail: filePath || extractPathFromTitle(toolCall.title),
    }
  }

  if (title.startsWith('run') || kind === 'bash' || kind === 'shell') {
    const cmd = extractCommandFromTitle(toolCall.title)
    return {
      icon: '>_',
      action: getCommandDescription(toolCall.title),
      detail: cmd,
    }
  }

  // Default
  return {
    icon: '◇',
    action: toolCall.title || toolCall.kind || 'Tool',
    detail: filePath,
  }
}

function extractPathFromTitle(title?: string): string | undefined {
  if (!title) return undefined
  // Look for path-like strings
  const match = title.match(/\/[\w\-./]+/)
  return match ? match[0] : undefined
}

function extractCommandFromTitle(title?: string): string | undefined {
  if (!title) return undefined
  // Remove "Run " prefix and get the command
  if (title.toLowerCase().startsWith('run ')) {
    const cmd = title.slice(4).trim()
    // Truncate if too long
    return cmd.length > 50 ? cmd.slice(0, 47) + '...' : cmd
  }
  return undefined
}

function getCommandDescription(title?: string): string {
  if (!title) return 'Run command'
  const lower = title.toLowerCase()

  if (lower.includes('mkdir')) return 'Create directory'
  if (lower.includes('rm ')) return 'Remove'
  if (lower.includes('mv ')) return 'Move'
  if (lower.includes('cp ')) return 'Copy'
  if (lower.includes('npm') || lower.includes('pnpm') || lower.includes('yarn')) return 'Package manager'
  if (lower.includes('git')) return 'Git'
  if (lower.includes('python')) return 'Run Python'
  if (lower.includes('node')) return 'Run Node'
  if (lower.includes('perl')) return 'Run Perl'

  return 'Run command'
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
