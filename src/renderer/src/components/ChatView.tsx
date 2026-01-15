/**
 * Chat view component - displays messages and tool calls
 */
import { useEffect, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { StoredSessionUpdate } from '../../../shared/types'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { ChevronDown } from 'lucide-react'
import { ToolCallItem, type ToolCall } from './ToolCallItem'
import { PermissionRequestItem } from './PermissionRequestItem'
import { usePermissionStore } from '../stores/permissionStore'

interface ChatViewProps {
  updates: StoredSessionUpdate[]
  isProcessing: boolean
  hasSession: boolean
  isInitializing: boolean
  currentSessionId: string | null
}

export function ChatView({ updates, isProcessing, hasSession, isInitializing, currentSessionId }: ChatViewProps) {
  const bottomRef = useRef<HTMLDivElement>(null)
  const pendingPermission = usePermissionStore((s) => s.pendingRequest)

  // Only show permission request if it belongs to the current session
  const currentPermission = pendingPermission?.multicaSessionId === currentSessionId
    ? pendingPermission
    : null

  // Auto-scroll to bottom when new messages arrive or permission request changes
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [updates, currentPermission])

  // Group updates into messages
  const messages = groupUpdatesIntoMessages(updates)

  // Show initializing state
  if (isInitializing) {
    return <SessionInitializing />
  }

  if (messages.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <div className="text-center">
          <h1 className="mb-2 text-3xl font-bold">Multica</h1>
          <p className="text-muted-foreground">
            {hasSession
              ? 'Start a conversation with your coding agent'
              : 'Select a folder below to start'}
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto max-w-3xl space-y-4 px-6 py-4">
        {messages.map((msg, idx) => (
          <MessageBubble key={idx} message={msg} />
        ))}

        {/* Permission request - show in feed (only for current session) */}
        {currentPermission && (
          <PermissionRequestItem request={currentPermission} />
        )}

        {isProcessing && !currentPermission && (
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

// Content block types for preserving time order
interface TextBlock {
  type: 'text'
  content: string
}

interface ImageBlock {
  type: 'image'
  data: string
  mimeType: string
}

interface ThoughtBlock {
  type: 'thought'
  content: string
}

interface ToolCallBlock {
  type: 'tool_call'
  toolCall: ToolCall
}

type ContentBlock = TextBlock | ImageBlock | ThoughtBlock | ToolCallBlock

interface Message {
  role: 'user' | 'assistant'
  blocks: ContentBlock[]
}

function groupUpdatesIntoMessages(updates: StoredSessionUpdate[]): Message[] {
  const messages: Message[] = []
  let currentBlocks: ContentBlock[] = []
  // Track tool calls by ID to update them in place
  const toolCallMap = new Map<string, ToolCall>()
  // Track which tool call IDs we've added as blocks (to avoid duplicates)
  const addedToolCallIds = new Set<string>()
  // Track accumulated text and thought for merging consecutive chunks
  let pendingText = ''
  let pendingThought = ''

  const flushPendingText = () => {
    if (pendingText) {
      currentBlocks.push({ type: 'text', content: pendingText })
      pendingText = ''
    }
  }

  const flushPendingThought = () => {
    if (pendingThought) {
      currentBlocks.push({ type: 'thought', content: pendingThought })
      pendingThought = ''
    }
  }

  const flushAssistantMessage = () => {
    flushPendingThought()
    flushPendingText()
    if (currentBlocks.length > 0) {
      messages.push({
        role: 'assistant',
        blocks: currentBlocks,
      })
      currentBlocks = []
      toolCallMap.clear()
      addedToolCallIds.clear()
    }
  }

  for (const stored of updates) {
    const notification = stored.update
    const update = notification?.update
    if (!update || !('sessionUpdate' in update)) {
      continue
    }

    // Debug: log unhandled update types
    const handledTypes = ['user_message', 'agent_thought_chunk', 'agent_message_chunk', 'tool_call', 'tool_call_update']
    if (!handledTypes.includes(update.sessionUpdate as string)) {
      console.log('[ChatView] Unhandled sessionUpdate type:', update.sessionUpdate, update)
    }

    switch (update.sessionUpdate) {
      case 'user_message' as string:
        // Flush any pending assistant message
        flushAssistantMessage()
        // Add user message - supports multiple formats for backward compatibility
        {
          const userUpdate = update as { content?: unknown }
          const userBlocks: ContentBlock[] = []
          const content = userUpdate.content

          if (Array.isArray(content)) {
            // New format: MessageContent array (e.g., [{ type: 'text', text: '...' }, { type: 'image', ... }])
            for (const item of content) {
              if (item && typeof item === 'object') {
                if (item.type === 'text' && typeof item.text === 'string') {
                  userBlocks.push({ type: 'text', content: item.text })
                } else if (item.type === 'image' && typeof item.data === 'string') {
                  userBlocks.push({ type: 'image', data: item.data, mimeType: item.mimeType || 'image/png' })
                }
              }
            }
          } else if (content && typeof content === 'object' && 'type' in content && 'text' in content) {
            // Old format: single text content object { type: 'text', text: '...' }
            const textContent = content as { type: string; text: unknown }
            if (textContent.type === 'text' && typeof textContent.text === 'string') {
              userBlocks.push({ type: 'text', content: textContent.text })
            }
          } else if (typeof content === 'string') {
            // Fallback: plain string content
            userBlocks.push({ type: 'text', content: content })
          }

          if (userBlocks.length > 0) {
            messages.push({
              role: 'user',
              blocks: userBlocks,
            })
          }
        }
        break

      case 'agent_thought_chunk':
        // Accumulate thought chunks
        if ('content' in update && update.content?.type === 'text') {
          pendingThought += update.content.text
        }
        break

      case 'agent_message_chunk':
        // Flush thought before text (thought usually comes first)
        flushPendingThought()
        // Accumulate text chunks
        if ('content' in update && update.content?.type === 'text') {
          pendingText += update.content.text
        }
        break

      case 'tool_call':
        if ('toolCallId' in update) {
          // Flush pending text/thought before tool call to preserve order
          flushPendingThought()
          flushPendingText()

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

          // Add tool call block if not already added
          if (!addedToolCallIds.has(update.toolCallId)) {
            currentBlocks.push({ type: 'tool_call', toolCall })
            addedToolCallIds.add(update.toolCallId)
          }
        }
        break

      case 'tool_call_update':
        if ('toolCallId' in update) {
          // Get or create the tool call entry
          let toolCall = toolCallMap.get(update.toolCallId)
          if (toolCall) {
            // Update existing tool call in place
            if (update.status) toolCall.status = update.status
            if (update.title) toolCall.title = update.title
            if (update.rawInput && Object.keys(update.rawInput).length > 0) {
              toolCall.input = typeof update.rawInput === 'string'
                ? update.rawInput
                : JSON.stringify(update.rawInput, null, 2)
            }
            if (update.rawOutput) {
              toolCall.output = typeof update.rawOutput === 'string'
                ? update.rawOutput
                : JSON.stringify(update.rawOutput, null, 2)
            }
          } else {
            // Create new entry if we see update before the initial tool_call
            // Flush pending text/thought first to preserve order
            flushPendingThought()
            flushPendingText()

            toolCall = {
              id: update.toolCallId,
              title: update.title || 'Tool Call',
              status: update.status || 'pending',
              kind: update.kind ?? undefined,
            }
            if (update.rawInput && Object.keys(update.rawInput).length > 0) {
              toolCall.input = typeof update.rawInput === 'string'
                ? update.rawInput
                : JSON.stringify(update.rawInput, null, 2)
            }
            if (update.rawOutput) {
              toolCall.output = typeof update.rawOutput === 'string'
                ? update.rawOutput
                : JSON.stringify(update.rawOutput, null, 2)
            }
            toolCallMap.set(update.toolCallId, toolCall)

            // Add tool call block if not already added
            if (!addedToolCallIds.has(update.toolCallId)) {
              currentBlocks.push({ type: 'tool_call', toolCall })
              addedToolCallIds.add(update.toolCallId)
            }
          }
        }
        break
    }
  }

  // Flush any remaining assistant content
  flushAssistantMessage()

  return messages
}

interface MessageBubbleProps {
  message: Message
}

function MessageBubble({ message }: MessageBubbleProps) {
  const isUser = message.role === 'user'

  // User message - bubble style with support for images
  if (isUser) {
    const imageBlocks = message.blocks.filter((b): b is ImageBlock => b.type === 'image')
    const textBlock = message.blocks.find((b): b is TextBlock => b.type === 'text')

    return (
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-lg bg-muted px-4 py-3 text-[15px]">
          {/* Render images first */}
          {imageBlocks.length > 0 && (
            <div className="flex flex-wrap gap-2 mb-2">
              {imageBlocks.map((img, idx) => (
                <img
                  key={idx}
                  src={`data:${img.mimeType};base64,${img.data}`}
                  alt={`Uploaded image ${idx + 1}`}
                  className="max-w-[200px] max-h-[200px] rounded-md object-cover"
                />
              ))}
            </div>
          )}
          {/* Render text content */}
          {textBlock && (
            <div className="whitespace-pre-wrap">{textBlock.content}</div>
          )}
        </div>
      </div>
    )
  }

  // Assistant message - render blocks in order to preserve time sequence
  return (
    <div className="space-y-3">
      {message.blocks.map((block, idx) => {
        switch (block.type) {
          case 'thought':
            return <ThoughtBlockView key={`thought-${idx}`} text={block.content} />
          case 'tool_call':
            return <ToolCallItem key={block.toolCall.id} toolCall={block.toolCall} />
          case 'text':
            return <TextContentBlock key={`text-${idx}`} content={block.content} />
          case 'image':
            return (
              <img
                key={`image-${idx}`}
                src={`data:${block.mimeType};base64,${block.data}`}
                alt={`Image ${idx + 1}`}
                className="max-w-[300px] max-h-[300px] rounded-md object-cover"
              />
            )
          default:
            return null
        }
      })}
    </div>
  )
}

// Text content block with markdown rendering
function TextContentBlock({ content }: { content: string }) {
  if (!content) return null

  return (
    <div className="prose prose-invert max-w-none text-[15px]">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
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
        {content}
      </ReactMarkdown>
    </div>
  )
}

// Thought block view - expanded, collapsible
function ThoughtBlockView({ text }: { text: string }) {
  // Skip rendering if content is empty or only whitespace
  if (!text || !text.trim()) return null

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

function SessionInitializing() {
  return (
    <div className="flex flex-1 items-center justify-center">
      <div className="flex flex-col items-center gap-6">
        {/* Shimmer progress bar */}
        <div className="relative h-1 w-48 overflow-hidden rounded-full bg-muted">
          <div className="absolute inset-0 h-full w-1/2 rounded-full bg-gradient-to-r from-transparent via-primary/60 to-transparent animate-shimmer" />
        </div>
        {/* Text */}
        <p className="text-sm text-muted-foreground">Initializing agent...</p>
      </div>
    </div>
  )
}
