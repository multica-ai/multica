/**
 * Chat view component - displays messages and tool calls
 */
import { useEffect, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { StoredSessionUpdate } from '../../../shared/types'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { ChevronDown } from 'lucide-react'
import { ToolCallItem, type ToolCall } from './ToolCallItem'
import { PermissionRequestItem } from './PermissionRequestItem'
import { usePermissionStore } from '../stores/permissionStore'

interface ChatViewProps {
  updates: StoredSessionUpdate[]
  isProcessing: boolean
  hasSession: boolean
  onNewSession?: () => void
}

export function ChatView({ updates, isProcessing, hasSession, onNewSession }: ChatViewProps) {
  const bottomRef = useRef<HTMLDivElement>(null)
  const pendingPermission = usePermissionStore((s) => s.pendingRequest)

  // Auto-scroll to bottom when new messages arrive or permission request changes
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [updates, pendingPermission])

  // Group updates into messages
  const messages = groupUpdatesIntoMessages(updates)

  if (messages.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <div className="text-center">
          <h1 className="mb-2 text-3xl font-bold">Multica</h1>
          <p className="mb-4 text-muted-foreground">
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

        {/* Permission request - show in feed */}
        {pendingPermission && (
          <PermissionRequestItem request={pendingPermission} />
        )}

        {isProcessing && !pendingPermission && (
          <div className="flex items-center gap-2 text-muted-foreground">
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
        <div className="max-w-[85%] rounded-lg bg-muted px-4 py-3 text-sm">
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
        <ToolCallItem key={tc.id} toolCall={tc} />
      ))}

      {/* Text content with markdown */}
      {message.content && (
        <div className="prose prose-invert prose-sm max-w-none">
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
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
                    <code className="block bg-muted rounded-lg p-3 text-xs font-mono overflow-x-auto">
                      {children}
                    </code>
                  )
                }
                return (
                  <code className="bg-muted rounded px-1.5 py-0.5 text-xs font-mono">
                    {children}
                  </code>
                )
              },
              pre: ({ children }) => (
                <pre className="bg-muted rounded-lg p-3 mb-3 overflow-x-auto text-xs">
                  {children}
                </pre>
              ),
              a: ({ href, children }) => (
                <a href={href} className="text-primary hover:underline" target="_blank" rel="noopener noreferrer">
                  {children}
                </a>
              ),
              blockquote: ({ children }) => (
                <blockquote className="border-l-2 border-border pl-3 italic text-muted-foreground">
                  {children}
                </blockquote>
              ),
              hr: () => <hr className="border-border my-4" />,
              strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
              em: ({ children }) => <em className="italic">{children}</em>,
              // Table components for GFM table support
              table: ({ children }) => (
                <div className="overflow-x-auto mb-3">
                  <table className="min-w-full border-collapse border border-border text-sm">
                    {children}
                  </table>
                </div>
              ),
              thead: ({ children }) => (
                <thead className="bg-muted/50">{children}</thead>
              ),
              tbody: ({ children }) => <tbody>{children}</tbody>,
              tr: ({ children }) => (
                <tr className="border-b border-border">{children}</tr>
              ),
              th: ({ children }) => (
                <th className="border border-border px-3 py-2 text-left font-semibold">
                  {children}
                </th>
              ),
              td: ({ children }) => (
                <td className="border border-border px-3 py-2">{children}</td>
              ),
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

function LoadingDots() {
  return (
    <span className="inline-flex gap-1">
      <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-current" style={{ animationDelay: '0ms' }} />
      <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-current" style={{ animationDelay: '150ms' }} />
      <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-current" style={{ animationDelay: '300ms' }} />
    </span>
  )
}
