/**
 * Chat view component - displays messages and tool calls
 */
import { useEffect, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { StoredSessionUpdate } from '../../../shared/types'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { ChevronDown, CheckCircle2, Circle, Loader2, Folder } from 'lucide-react'
import { ToolCallItem, type ToolCall, type AnsweredResponse } from './ToolCallItem'
import { PermissionRequestItem } from './permission'
import { usePermissionStore } from '../stores/permissionStore'
import { cn } from '@/lib/utils'

interface ChatViewProps {
  updates: StoredSessionUpdate[]
  isProcessing: boolean
  hasSession: boolean
  isInitializing: boolean
  currentSessionId: string | null
  onSelectFolder?: () => void
}

export function ChatView({
  updates,
  isProcessing,
  hasSession,
  isInitializing,
  currentSessionId,
  onSelectFolder
}: ChatViewProps) {
  const bottomRef = useRef<HTMLDivElement>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const isNearBottomRef = useRef(true)
  const pendingPermission = usePermissionStore((s) => s.pendingRequests[0] ?? null)

  // Only show permission request if it belongs to the current session
  const currentPermission =
    pendingPermission?.multicaSessionId === currentSessionId ? pendingPermission : null

  // Track scroll position to determine if user is near bottom
  const handleScroll = () => {
    const container = scrollContainerRef.current
    if (!container) return
    // Consider "near bottom" if within 100px of the bottom
    const threshold = 100
    const isNear = container.scrollHeight - container.scrollTop - container.clientHeight < threshold
    isNearBottomRef.current = isNear
  }

  // Auto-scroll to bottom only if user is already near bottom
  useEffect(() => {
    if (isNearBottomRef.current) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
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
          <p className="text-muted-foreground mb-6">
            {hasSession
              ? 'Start a conversation with your coding agent'
              : 'Select a folder to start'}
          </p>
          {!hasSession && onSelectFolder && (
            <button
              onClick={onSelectFolder}
              className="inline-flex items-center gap-2 bg-card hover:bg-accent transition-colors duration-200 rounded-xl px-4 py-2.5 border border-border cursor-pointer text-sm text-muted-foreground hover:text-foreground"
            >
              <Folder className="h-4 w-4" />
              <span>Browse folder</span>
            </button>
          )}
        </div>
      </div>
    )
  }

  return (
    <div ref={scrollContainerRef} onScroll={handleScroll} className="flex-1 overflow-y-auto">
      <div className="mx-auto max-w-3xl space-y-5 px-8 py-6">
        {messages.map((msg, idx) => (
          <MessageBubble key={idx} message={msg} />
        ))}

        {/* Permission request - show in feed (only for current session) */}
        {/* Completed state is shown inline with the tool call, like other tools */}
        {currentPermission && <PermissionRequestItem request={currentPermission} />}

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

// Plan entry from ACP protocol (TodoWrite tool)
interface PlanEntry {
  content: string
  status: 'pending' | 'in_progress' | 'completed'
  priority?: 'high' | 'medium' | 'low'
}

interface PlanBlock {
  type: 'plan'
  entries: PlanEntry[]
}

type ContentBlock = TextBlock | ImageBlock | ThoughtBlock | ToolCallBlock | PlanBlock

interface Message {
  role: 'user' | 'assistant'
  blocks: ContentBlock[]
}

function groupUpdatesIntoMessages(updates: StoredSessionUpdate[]): Message[] {
  // Sort updates by sequence number to ensure correct ordering despite async delivery
  // Updates without sequence numbers (e.g., user messages, legacy data) keep their relative position
  const sortedUpdates = [...updates].sort((a, b) => {
    // If both have sequence numbers, sort by sequence
    if (a.sequenceNumber !== undefined && b.sequenceNumber !== undefined) {
      return a.sequenceNumber - b.sequenceNumber
    }
    // If only one has sequence number, keep relative order (stable sort)
    return 0
  })

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
        blocks: currentBlocks
      })
      currentBlocks = []
      toolCallMap.clear()
      addedToolCallIds.clear()
    }
  }

  for (const stored of sortedUpdates) {
    const notification = stored.update
    const update = notification?.update
    if (!update || !('sessionUpdate' in update)) {
      continue
    }

    switch (update.sessionUpdate) {
      case 'user_message' as string:
        // Skip internal messages (used by G-3 mechanism for AskUserQuestion answers)
        // These are sent to agent but should not be displayed in UI
        {
          const userUpdate = update as { content?: unknown; _internal?: boolean }
          if (userUpdate._internal) {
            break // Skip internal messages - not displayed in UI
          }

          // Flush any pending assistant message
          flushAssistantMessage()
          // Add user message - supports multiple formats for backward compatibility
          const userBlocks: ContentBlock[] = []
          const content = userUpdate.content

          if (Array.isArray(content)) {
            // New format: MessageContent array (e.g., [{ type: 'text', text: '...' }, { type: 'image', ... }])
            for (const item of content) {
              if (item && typeof item === 'object') {
                if (item.type === 'text' && typeof item.text === 'string') {
                  userBlocks.push({ type: 'text', content: item.text })
                } else if (item.type === 'image' && typeof item.data === 'string') {
                  userBlocks.push({
                    type: 'image',
                    data: item.data,
                    mimeType: item.mimeType || 'image/png'
                  })
                }
              }
            }
          } else if (
            content &&
            typeof content === 'object' &&
            'type' in content &&
            'text' in content
          ) {
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
              blocks: userBlocks
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
          // Extract _meta.claudeCode.toolName (most reliable tool name source)
          const meta = update._meta as { claudeCode?: { toolName?: string } } | undefined

          // Check if toolCall already exists
          let toolCall = toolCallMap.get(update.toolCallId)
          if (toolCall) {
            // Update existing toolCall (keep reference unchanged so currentBlocks updates too)
            if (update.status) toolCall.status = update.status
            if (update.title) toolCall.title = update.title
            if (meta?.claudeCode?.toolName) toolCall.toolName = meta.claudeCode.toolName
            if (update.kind) toolCall.kind = update.kind
            if (update.rawInput && Object.keys(update.rawInput).length > 0) {
              toolCall.rawInput = update.rawInput as Record<string, unknown>
              toolCall.input =
                typeof update.rawInput === 'string'
                  ? update.rawInput
                  : JSON.stringify(update.rawInput, null, 2)
            }
            if (update.rawOutput) {
              toolCall.output =
                typeof update.rawOutput === 'string'
                  ? update.rawOutput
                  : JSON.stringify(update.rawOutput, null, 2)
            }
          } else {
            // Create new toolCall
            // Flush pending text/thought before tool call to preserve order
            flushPendingThought()
            flushPendingText()

            toolCall = {
              id: update.toolCallId,
              title: update.title || 'Tool Call',
              status: update.status || 'pending',
              kind: update.kind,
              toolName: meta?.claudeCode?.toolName,
              rawInput: update.rawInput as Record<string, unknown> | undefined,
              input:
                typeof update.rawInput === 'string'
                  ? update.rawInput
                  : update.rawInput && Object.keys(update.rawInput).length > 0
                    ? JSON.stringify(update.rawInput, null, 2)
                    : undefined
            }
            if (update.rawOutput) {
              toolCall.output =
                typeof update.rawOutput === 'string'
                  ? update.rawOutput
                  : JSON.stringify(update.rawOutput, null, 2)
            }
            toolCallMap.set(update.toolCallId, toolCall)

            // Add tool call block
            currentBlocks.push({ type: 'tool_call', toolCall })
            addedToolCallIds.add(update.toolCallId)
          }
        }
        break

      case 'tool_call_update':
        if ('toolCallId' in update) {
          // Extract _meta.claudeCode.toolName
          const updateMeta = update._meta as { claudeCode?: { toolName?: string } } | undefined

          // Get or create the tool call entry
          let toolCall = toolCallMap.get(update.toolCallId)
          if (toolCall) {
            // Update existing tool call in place
            if (update.status) toolCall.status = update.status
            if (update.title) toolCall.title = update.title
            if (updateMeta?.claudeCode?.toolName) toolCall.toolName = updateMeta.claudeCode.toolName
            if (update.rawInput && Object.keys(update.rawInput).length > 0) {
              toolCall.rawInput = update.rawInput as Record<string, unknown>
              toolCall.input =
                typeof update.rawInput === 'string'
                  ? update.rawInput
                  : JSON.stringify(update.rawInput, null, 2)
            }
            if (update.rawOutput) {
              toolCall.output =
                typeof update.rawOutput === 'string'
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
              toolName: updateMeta?.claudeCode?.toolName,
              rawInput: update.rawInput as Record<string, unknown> | undefined
            }
            if (update.rawInput && Object.keys(update.rawInput).length > 0) {
              toolCall.input =
                typeof update.rawInput === 'string'
                  ? update.rawInput
                  : JSON.stringify(update.rawInput, null, 2)
            }
            if (update.rawOutput) {
              toolCall.output =
                typeof update.rawOutput === 'string'
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

      case 'plan':
        // Handle plan updates from TodoWrite tool
        if ('entries' in update && Array.isArray(update.entries)) {
          flushPendingThought()
          flushPendingText()
          const entries: PlanEntry[] = update.entries.map(
            (entry: { content?: string; status?: string; priority?: string }) => ({
              content: entry.content || '',
              status: (entry.status as PlanEntry['status']) || 'pending',
              priority: entry.priority as PlanEntry['priority']
            })
          )
          if (entries.length > 0) {
            // Find existing plan block and update it instead of creating new one
            const existingPlanIndex = currentBlocks.findIndex((b) => b.type === 'plan')
            if (existingPlanIndex >= 0) {
              ;(
                currentBlocks[existingPlanIndex] as { type: 'plan'; entries: PlanEntry[] }
              ).entries = entries
            } else {
              currentBlocks.push({ type: 'plan', entries })
            }
          }
        }
        break

      case 'askuserquestion_response':
        // Handle persisted AskUserQuestion response (for state restoration after restart)
        if ('toolCallId' in update && 'response' in update) {
          const responseUpdate = update as { toolCallId: string; response: AnsweredResponse }
          const toolCall = toolCallMap.get(responseUpdate.toolCallId)
          if (toolCall) {
            // Mark tool call as completed and attach the persisted response
            toolCall.status = 'completed'
            toolCall.answeredResponse = responseUpdate.response
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
        <div className="max-w-[85%] rounded-lg bg-[#f9f7f5] dark:bg-muted px-4 py-3 text-[15px]">
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
          {textBlock && <div className="whitespace-pre-wrap">{textBlock.content}</div>}
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
          case 'plan':
            return <PlanBlockView key={`plan-${idx}`} entries={block.entries} />
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
    <div className="prose prose-invert max-w-none text-[15px] leading-[1.7]">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // Paragraphs: consistent spacing, tighter line height for readability
          p: ({ children }) => <p className="mb-4 last:mb-0">{children}</p>,
          // Headings: more space above (1.5x) than below (0.5x) for visual grouping
          h1: ({ children }) => (
            <h1 className="text-xl font-bold mt-6 mb-3 first:mt-0">{children}</h1>
          ),
          h2: ({ children }) => (
            <h2 className="text-lg font-bold mt-5 mb-2.5 first:mt-0">{children}</h2>
          ),
          h3: ({ children }) => (
            <h3 className="text-base font-semibold mt-4 mb-2 first:mt-0">{children}</h3>
          ),
          // Lists: consistent spacing with content
          ul: ({ children }) => (
            <ul className="list-disc pl-5 mb-4 last:mb-0 space-y-1.5">{children}</ul>
          ),
          ol: ({ children }) => (
            <ol className="list-decimal pl-5 mb-4 last:mb-0 space-y-1.5">{children}</ol>
          ),
          li: ({ children }) => <li>{children}</li>,
          // Code: pre handles container, code is transparent for blocks
          code: ({ className, children }) => {
            const isBlock = className?.includes('language-')
            if (isBlock) {
              // Block code inside pre - no extra styling, pre handles it
              return <code>{children}</code>
            }
            // Inline code
            return (
              <code className="bg-muted/70 rounded px-1.5 py-0.5 text-[13px] font-mono">
                {children}
              </code>
            )
          },
          pre: ({ children }) => (
            <pre className="bg-muted rounded-lg px-4 py-3 mb-4 last:mb-0 overflow-x-auto text-[13px] font-mono leading-relaxed">
              {children}
            </pre>
          ),
          // Links
          a: ({ href, children }) => (
            <a
              href={href}
              className="text-primary hover:underline"
              target="_blank"
              rel="noopener noreferrer"
            >
              {children}
            </a>
          ),
          // Blockquote: subtle styling
          blockquote: ({ children }) => (
            <blockquote className="border-l-2 border-border pl-4 my-4 text-muted-foreground">
              {children}
            </blockquote>
          ),
          hr: () => <hr className="border-border my-6" />,
          strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
          em: ({ children }) => <em className="italic">{children}</em>,
          // Table components for GFM table support
          table: ({ children }) => (
            <div className="overflow-x-auto mb-4 last:mb-0">
              <table className="min-w-full border-collapse border border-border text-sm">
                {children}
              </table>
            </div>
          ),
          thead: ({ children }) => <thead className="bg-muted/50">{children}</thead>,
          tbody: ({ children }) => <tbody>{children}</tbody>,
          tr: ({ children }) => <tr className="border-b border-border">{children}</tr>,
          th: ({ children }) => (
            <th className="border border-border px-3 py-2 text-left font-semibold">{children}</th>
          ),
          td: ({ children }) => <td className="border border-border px-3 py-2">{children}</td>
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
            <ChevronDown
              className={`ml-auto h-4 w-4 transition-transform ${isExpanded ? 'rotate-180' : ''}`}
            />
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
                )
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
                )
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
                )
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
      <span
        className="h-1.5 w-1.5 animate-bounce rounded-full bg-current"
        style={{ animationDelay: '0ms' }}
      />
      <span
        className="h-1.5 w-1.5 animate-bounce rounded-full bg-current"
        style={{ animationDelay: '150ms' }}
      />
      <span
        className="h-1.5 w-1.5 animate-bounce rounded-full bg-current"
        style={{ animationDelay: '300ms' }}
      />
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

// Plan block view - displays todo list from TodoWrite tool (collapsible)
function PlanBlockView({ entries }: { entries: PlanEntry[] }) {
  const [isExpanded, setIsExpanded] = useState(false)

  if (!entries || entries.length === 0) return null

  const completedCount = entries.filter((e) => e.status === 'completed').length
  const inProgressEntry = entries.find((e) => e.status === 'in_progress')
  const totalCount = entries.length

  return (
    <Collapsible open={isExpanded} onOpenChange={setIsExpanded}>
      <CollapsibleTrigger className="flex w-full items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors">
        {/* Status indicator */}
        {inProgressEntry ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin text-[var(--tool-running)]" />
        ) : completedCount === totalCount ? (
          <CheckCircle2 className="h-3.5 w-3.5 text-[var(--tool-success)]" />
        ) : (
          <Circle className="h-3.5 w-3.5" />
        )}

        {/* Title with progress */}
        <span className="text-secondary-foreground">Tasks</span>
        <span className="text-xs text-muted-foreground">
          {completedCount}/{totalCount}
        </span>

        {/* Current task hint when collapsed */}
        {!isExpanded && inProgressEntry && (
          <span className="text-xs text-muted-foreground truncate max-w-[200px]">
            – {inProgressEntry.content}
          </span>
        )}

        {/* Chevron */}
        <ChevronDown
          className={cn('ml-auto h-3.5 w-3.5 transition-transform', isExpanded && 'rotate-180')}
        />
      </CollapsibleTrigger>

      <CollapsibleContent>
        <div className="mt-2 space-y-1 pl-5">
          {entries.map((entry, idx) => (
            <div key={idx} className="flex items-start gap-2 text-xs">
              {/* Status icon - smaller */}
              {entry.status === 'completed' ? (
                <CheckCircle2 className="h-3 w-3 text-[var(--tool-success)] flex-shrink-0 mt-0.5" />
              ) : entry.status === 'in_progress' ? (
                <Loader2 className="h-3 w-3 text-[var(--tool-running)] flex-shrink-0 mt-0.5 animate-spin" />
              ) : (
                <Circle className="h-3 w-3 text-muted-foreground/50 flex-shrink-0 mt-0.5" />
              )}
              {/* Content - smaller text */}
              <span
                className={cn(
                  'leading-relaxed',
                  entry.status === 'completed' && 'text-muted-foreground line-through',
                  entry.status === 'in_progress' && 'text-secondary-foreground',
                  entry.status === 'pending' && 'text-muted-foreground'
                )}
              >
                {entry.content}
              </span>
            </div>
          ))}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
