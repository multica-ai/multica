/**
 * Chat view component - displays messages and tool calls
 * Note: Scroll behavior is managed by parent (App.tsx) for unified scroll context
 */
import { useState, useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { StoredSessionUpdate } from '../../../shared/types'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { ChevronDown, ChevronRight, CheckCircle2, Circle, Loader2, Folder } from 'lucide-react'
import { ToolCallItem, type ToolCall, type AnsweredResponse } from './ToolCallItem'
import { PermissionRequestItem } from './permission'
import { usePermissionStore } from '../stores/permissionStore'
import { cn } from '@/lib/utils'
import { MessageTimer, StatusIndicator, Spinner, type CurrentAction } from './ui/LoadingIndicator'
import { CompletedMessageFooter } from './ui/CompletedMessageFooter'

// Hoisted ReactMarkdown components for better performance (avoids object recreation on each render)
const TEXT_MARKDOWN_COMPONENTS = {
  // Paragraphs: consistent spacing, tighter line height for readability
  p: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <p className="mb-4 last:mb-0">{children}</p>
  ),
  // Headings: more space above (1.5x) than below (0.5x) for visual grouping
  h1: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <h1 className="text-xl font-bold mt-6 mb-3 first:mt-0">{children}</h1>
  ),
  h2: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <h2 className="text-lg font-bold mt-5 mb-2.5 first:mt-0">{children}</h2>
  ),
  h3: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <h3 className="text-base font-semibold mt-4 mb-2 first:mt-0">{children}</h3>
  ),
  // Lists: consistent spacing with content
  ul: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <ul className="list-disc pl-5 mb-4 last:mb-0 space-y-1.5">{children}</ul>
  ),
  ol: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <ol className="list-decimal pl-5 mb-4 last:mb-0 space-y-1.5">{children}</ol>
  ),
  li: ({ children }: { children?: React.ReactNode }): React.JSX.Element => <li>{children}</li>,
  // Code: pre handles container, code is transparent for blocks
  code: ({
    className,
    children
  }: {
    className?: string
    children?: React.ReactNode
  }): React.JSX.Element => {
    const isBlock = className?.includes('language-')
    if (isBlock) {
      // Block code inside pre - no extra styling, pre handles it
      return <code>{children}</code>
    }
    // Inline code
    return (
      <code className="bg-muted/70 rounded px-1.5 py-0.5 text-[13px] font-mono">{children}</code>
    )
  },
  pre: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <pre className="bg-muted rounded-lg px-4 py-3 mb-4 last:mb-0 overflow-x-auto text-[13px] font-mono leading-relaxed">
      {children}
    </pre>
  ),
  // Links
  a: ({ href, children }: { href?: string; children?: React.ReactNode }): React.JSX.Element => (
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
  blockquote: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <blockquote className="border-l-2 border-border pl-4 my-4 text-muted-foreground">
      {children}
    </blockquote>
  ),
  hr: (): React.JSX.Element => <hr className="border-border my-6" />,
  strong: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <strong className="font-semibold">{children}</strong>
  ),
  em: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <em className="italic">{children}</em>
  ),
  // Table components for GFM table support
  table: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <div className="overflow-x-auto mb-4 last:mb-0">
      <table className="min-w-full border-collapse border border-border text-sm">{children}</table>
    </div>
  ),
  thead: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <thead className="bg-muted/50">{children}</thead>
  ),
  tbody: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <tbody>{children}</tbody>
  ),
  tr: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <tr className="border-b border-border">{children}</tr>
  ),
  th: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <th className="border border-border px-3 py-2 text-left font-semibold">{children}</th>
  ),
  td: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <td className="border border-border px-3 py-2">{children}</td>
  )
}

// Simpler markdown components for thought blocks
const THOUGHT_MARKDOWN_COMPONENTS = {
  p: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <p className="mb-2 last:mb-0">{children}</p>
  ),
  code: ({ children }: { children?: React.ReactNode }): React.JSX.Element => (
    <code className="bg-background rounded px-1 py-0.5 text-xs font-mono">{children}</code>
  )
}

interface ChatViewProps {
  updates: StoredSessionUpdate[]
  isProcessing: boolean
  hasSession: boolean
  isInitializing: boolean
  currentSessionId: string | null
  onSelectFolder?: () => void
  /** Ref for bottom anchor - passed from parent for scroll management */
  bottomRef?: React.RefObject<HTMLDivElement | null>
  /** Current model name for tooltip display */
  currentModelName?: string
  /** Current agent ID (claude-code, opencode, codex) */
  currentAgentId?: string
}

export function ChatView({
  updates,
  isProcessing,
  hasSession,
  isInitializing,
  currentSessionId,
  onSelectFolder,
  bottomRef,
  currentModelName,
  currentAgentId
}: ChatViewProps): React.JSX.Element {
  const pendingPermission = usePermissionStore((s) => s.pendingRequests[0] ?? null)

  // Only show permission request if it belongs to the current session
  const currentPermission =
    pendingPermission?.multicaSessionId === currentSessionId ? pendingPermission : null

  // Group updates into messages (memoized to avoid expensive recomputation)
  const messages = useMemo(() => groupUpdatesIntoMessages(updates), [updates])

  // Show initializing state
  if (isInitializing) {
    return <SessionInitializing />
  }

  if (messages.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center py-20">
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

  // Get agent display name for tooltip
  const agentDisplayName = currentAgentId
    ? AGENT_DISPLAY_NAMES[currentAgentId] || currentAgentId
    : undefined

  // Determine if we need a standalone processing indicator
  // Shown when isProcessing=true and the last message is a user message (no assistant response yet)
  const showStandaloneProcessing =
    isProcessing && (messages.length === 0 || messages[messages.length - 1].role === 'user')

  return (
    <div className="space-y-5 py-5">
      {messages.map((msg, idx) => (
        <MessageBubble
          key={idx}
          message={msg}
          isLastMessage={idx === messages.length - 1}
          isProcessing={isProcessing}
          modelName={currentModelName}
          agentName={agentDisplayName}
          currentAction={msg.currentAction}
        />
      ))}

      {/* Standalone processing indicator - shown when processing but no assistant content yet */}
      {showStandaloneProcessing && <StatusIndicator label="Preparing..." />}

      {/* Permission request - show in feed (only for current session) */}
      {currentPermission && <PermissionRequestItem request={currentPermission} />}

      {/* Bottom anchor for scroll */}
      <div ref={bottomRef} />
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

interface ErrorBlock {
  type: 'error'
  errorType: 'auth' | 'general'
  agentId?: string
  authCommand?: string
  message: string
}

type ContentBlock = TextBlock | ImageBlock | ThoughtBlock | ToolCallBlock | PlanBlock | ErrorBlock

interface Message {
  role: 'user' | 'assistant'
  blocks: ContentBlock[]
  // Time tracking for assistant messages
  startTime?: string // ISO 8601, first event timestamp
  endTime?: string // ISO 8601, last event timestamp
  // Current action for dynamic status label (only meaningful for last message during processing)
  currentAction?: CurrentAction
}

function groupUpdatesIntoMessages(updates: StoredSessionUpdate[]): Message[] {
  // Sort updates by sequence number to ensure correct ordering despite async delivery
  // Updates without sequence numbers (e.g., user messages, legacy data) keep their relative position
  const sortedUpdates = [...updates].sort((a, b): number => {
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
  // Track time for assistant messages
  let assistantStartTime: string | undefined
  let assistantEndTime: string | undefined
  // Track current action for dynamic status label
  let currentAction: CurrentAction | undefined

  const flushPendingText = (): void => {
    if (pendingText) {
      currentBlocks.push({ type: 'text', content: pendingText })
      pendingText = ''
    }
  }

  const flushPendingThought = (): void => {
    if (pendingThought) {
      currentBlocks.push({ type: 'thought', content: pendingThought })
      pendingThought = ''
    }
  }

  const flushAssistantMessage = (): void => {
    flushPendingThought()
    flushPendingText()
    if (currentBlocks.length > 0) {
      messages.push({
        role: 'assistant',
        blocks: currentBlocks,
        startTime: assistantStartTime,
        endTime: assistantEndTime,
        currentAction
      })
      currentBlocks = []
      toolCallMap.clear()
      addedToolCallIds.clear()
      // Reset tracking for next message
      assistantStartTime = undefined
      assistantEndTime = undefined
      currentAction = undefined
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
        // Track time - first agent event sets start time
        if (!assistantStartTime) {
          assistantStartTime = stored.timestamp
        }
        assistantEndTime = stored.timestamp
        // Track current action for status label
        currentAction = { type: 'thinking' }
        // Accumulate thought chunks
        if ('content' in update && update.content?.type === 'text') {
          pendingThought += update.content.text
        }
        break

      case 'agent_message_chunk':
        // Track time - first agent event sets start time
        if (!assistantStartTime) {
          assistantStartTime = stored.timestamp
        }
        assistantEndTime = stored.timestamp
        // Track current action for status label
        currentAction = { type: 'writing' }
        // Flush thought before text (thought usually comes first)
        flushPendingThought()
        // Accumulate text chunks
        if ('content' in update && update.content?.type === 'text') {
          pendingText += update.content.text
        }
        break

      case 'tool_call':
        // Track time
        if (!assistantStartTime) {
          assistantStartTime = stored.timestamp
        }
        assistantEndTime = stored.timestamp
        if ('toolCallId' in update) {
          // Extract _meta.claudeCode.toolName (most reliable tool name source)
          const meta = update._meta as { claudeCode?: { toolName?: string } } | undefined
          // Track current action for status label
          currentAction = {
            type: 'tool',
            toolName: meta?.claudeCode?.toolName || update.title || undefined,
            toolStatus: update.status || undefined
          }

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
        // Track time
        assistantEndTime = stored.timestamp
        if ('toolCallId' in update) {
          // Extract _meta.claudeCode.toolName
          const updateMeta = update._meta as { claudeCode?: { toolName?: string } } | undefined
          // Track current action for status label
          currentAction = {
            type: 'tool',
            toolName: updateMeta?.claudeCode?.toolName || update.title || undefined,
            toolStatus: update.status || undefined
          }

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

      case 'error_message' as string:
        // Handle error messages (shown in chat instead of toast)
        {
          const errorUpdate = update as {
            errorType?: string
            agentId?: string
            authCommand?: string
            message?: string
          }
          flushAssistantMessage()
          const errorBlock: ErrorBlock = {
            type: 'error',
            errorType: (errorUpdate.errorType as 'auth' | 'general') || 'general',
            agentId: errorUpdate.agentId,
            authCommand: errorUpdate.authCommand,
            message: errorUpdate.message || 'An error occurred'
          }
          // Add error as a standalone "assistant" message
          messages.push({
            role: 'assistant',
            blocks: [errorBlock]
          })
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
  isLastMessage: boolean
  isProcessing: boolean
  modelName?: string
  agentName?: string
  currentAction?: CurrentAction
}

function MessageBubble({
  message,
  isLastMessage,
  isProcessing,
  modelName,
  agentName,
  currentAction
}: MessageBubbleProps): React.JSX.Element {
  const isUser = message.role === 'user'
  const isComplete = !isLastMessage || !isProcessing

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

  // Assistant message - use collapsible wrapper for completed messages
  return (
    <CollapsibleAssistantMessage
      blocks={message.blocks}
      isComplete={isComplete}
      startTime={message.startTime}
      endTime={message.endTime}
      isProcessing={isLastMessage && isProcessing}
      modelName={modelName}
      agentName={agentName}
      currentAction={currentAction}
    />
  )
}

// Render a single content block
function renderContentBlock(block: ContentBlock, idx: number): React.JSX.Element | null {
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
    case 'error':
      return (
        <AuthErrorBlockView
          key={`error-${idx}`}
          errorType={block.errorType}
          agentId={block.agentId}
          authCommand={block.authCommand}
          message={block.message}
        />
      )
    default:
      return null
  }
}

// Collapsible assistant message - collapses tool calls and thoughts when message is complete
// Collapse condition: tool + thought >= 2
// Collapse range: from first tool/thought to last tool/thought (inclusive), with all content in between
function CollapsibleAssistantMessage({
  blocks,
  isComplete,
  startTime,
  endTime,
  isProcessing,
  modelName,
  agentName,
  currentAction
}: {
  blocks: ContentBlock[]
  isComplete: boolean
  startTime?: string
  endTime?: string
  isProcessing?: boolean
  modelName?: string
  agentName?: string
  currentAction?: CurrentAction
}): React.JSX.Element {
  const [isExpanded, setIsExpanded] = useState(false)

  // Count tool calls and thoughts (used for collapse condition)
  const toolCallCount = blocks.filter((b) => b.type === 'tool_call').length
  const thoughtCount = blocks.filter((b) => b.type === 'thought').length

  // Collapse condition: tool + thought >= 2
  const shouldCollapse = isComplete && toolCallCount + thoughtCount >= 2

  // Calculate duration for completed state
  const durationMs =
    startTime && endTime ? new Date(endTime).getTime() - new Date(startTime).getTime() : 0

  // Extract text content for copying (only text blocks, skip thoughts/tools)
  const textContent = blocks
    .filter((b): b is TextBlock => b.type === 'text')
    .map((b) => b.content)
    .join('\n\n')

  // Not collapsible - render all blocks normally
  if (!shouldCollapse) {
    return (
      <div className="space-y-3">
        {blocks.map((block, idx) => renderContentBlock(block, idx))}
        {/* Processing: show spinner + time + label */}
        {isProcessing && (
          <MessageTimer
            startTime={startTime}
            endTime={endTime}
            isProcessing={true}
            modelName={modelName}
            agentName={agentName}
            currentAction={currentAction}
          />
        )}
        {/* Completed: show duration + copy button */}
        {isComplete && (
          <CompletedMessageFooter
            durationMs={durationMs}
            startTime={startTime}
            content={textContent}
            modelName={modelName}
            agentName={agentName}
          />
        )}
      </div>
    )
  }

  // Find first and last tool/thought indices (needed for both expanded and collapsed states)
  const firstCollapsibleIdx = blocks.findIndex(
    (b) => b.type === 'tool_call' || b.type === 'thought'
  )
  const lastCollapsibleIdx = blocks.findLastIndex(
    (b) => b.type === 'tool_call' || b.type === 'thought'
  )

  // Split blocks:
  // - beforeBlocks: content before first tool/thought (always visible)
  // - collapsedBlocks: from first to last tool/thought inclusive (collapsible)
  // - afterBlocks: content after last tool/thought (always visible)
  const beforeBlocks = blocks.slice(0, firstCollapsibleIdx)
  const collapsedBlocks = blocks.slice(firstCollapsibleIdx, lastCollapsibleIdx + 1)
  const afterBlocks = blocks.slice(lastCollapsibleIdx + 1)

  // Count items within the collapsed region for summary
  const collapsedToolCount = collapsedBlocks.filter((b) => b.type === 'tool_call').length
  const collapsedThoughtCount = collapsedBlocks.filter((b) => b.type === 'thought').length
  const collapsedMessageCount = collapsedBlocks.filter((b) => b.type === 'text').length

  // Build summary text
  const summaryParts: string[] = []
  if (collapsedToolCount > 0) {
    summaryParts.push(`${collapsedToolCount} tool call${collapsedToolCount > 1 ? 's' : ''}`)
  }
  if (collapsedThoughtCount > 0) {
    summaryParts.push(`${collapsedThoughtCount} thought${collapsedThoughtCount > 1 ? 's' : ''}`)
  }
  if (collapsedMessageCount > 0) {
    summaryParts.push(`${collapsedMessageCount} message${collapsedMessageCount > 1 ? 's' : ''}`)
  }

  return (
    <div className="space-y-3">
      {/* Content before first tool/thought (always visible) */}
      {beforeBlocks.map((block, idx) => renderContentBlock(block, idx))}

      {/* Collapsible section using Collapsible component */}
      <Collapsible open={isExpanded} onOpenChange={setIsExpanded}>
        <CollapsibleTrigger
          className={cn(
            'flex w-full items-center gap-2 rounded px-1.5 py-0.5',
            'text-sm text-muted-foreground transition-colors duration-100',
            'hover:bg-muted/20 hover:text-foreground cursor-pointer'
          )}
        >
          <ChevronRight
            className={cn('h-3.5 w-3.5 transition-transform', isExpanded && 'rotate-90')}
          />
          <span>{summaryParts.join(', ')}</span>
        </CollapsibleTrigger>

        <CollapsibleContent className="space-y-3 mt-3">
          {collapsedBlocks.map((block, idx) =>
            renderContentBlock(block, firstCollapsibleIdx + idx)
          )}
        </CollapsibleContent>
      </Collapsible>

      {/* Content after last tool/thought (always visible) */}
      {afterBlocks.map((block, idx) =>
        renderContentBlock(block, firstCollapsibleIdx + collapsedBlocks.length + idx)
      )}

      {/* Completed: show duration + copy button */}
      {isComplete && (
        <CompletedMessageFooter
          durationMs={durationMs}
          startTime={startTime}
          content={textContent}
          modelName={modelName}
          agentName={agentName}
        />
      )}
    </div>
  )
}

// Text content block with markdown rendering
function TextContentBlock({ content }: { content: string }): React.JSX.Element | null {
  if (!content) return null

  return (
    <div className="prose prose-invert max-w-none text-[15px] leading-[1.7]">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={TEXT_MARKDOWN_COMPONENTS}>
        {content}
      </ReactMarkdown>
    </div>
  )
}

// Thought block view - expanded, collapsible
function ThoughtBlockView({ text }: { text: string }): React.JSX.Element | null {
  // Hooks must be called before any conditional returns (Rules of Hooks)
  const [isExpanded, setIsExpanded] = useState(false)
  const isLong = text.length > 200

  // Skip rendering if content is empty or only whitespace
  if (!text || !text.trim()) return null

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
            <ReactMarkdown components={THOUGHT_MARKDOWN_COMPONENTS}>{text}</ReactMarkdown>
          </div>
        </CollapsibleContent>

        {/* Preview when collapsed */}
        {!isExpanded && isLong && (
          <div className="mt-2 text-sm text-muted-foreground line-clamp-3">
            <ReactMarkdown components={THOUGHT_MARKDOWN_COMPONENTS}>{text}</ReactMarkdown>
          </div>
        )}

        {/* Show full content when not long */}
        {!isLong && (
          <div className="mt-2 text-sm text-muted-foreground">
            <ReactMarkdown components={THOUGHT_MARKDOWN_COMPONENTS}>{text}</ReactMarkdown>
          </div>
        )}
      </div>
    </Collapsible>
  )
}

function SessionInitializing(): React.JSX.Element {
  return (
    <div className="flex flex-1 items-center justify-center">
      <div className="flex items-center gap-2 text-muted-foreground">
        <Spinner className="text-sm" />
        <span className="text-sm">Starting agent...</span>
      </div>
    </div>
  )
}

// Plan block view - displays todo list from TodoWrite tool (collapsible)
function PlanBlockView({ entries }: { entries: PlanEntry[] }): React.JSX.Element | null {
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

// Agent name mapping for display
const AGENT_DISPLAY_NAMES: Record<string, string> = {
  'claude-code': 'Claude Code',
  opencode: 'OpenCode',
  codex: 'Codex'
}

// Auth error block view - displays authentication required message with command
function AuthErrorBlockView({
  errorType,
  agentId,
  authCommand,
  message
}: {
  errorType: 'auth' | 'general'
  agentId?: string
  authCommand?: string
  message: string
}): React.JSX.Element {
  const agentName = agentId ? AGENT_DISPLAY_NAMES[agentId] || agentId : 'Agent'

  const handleRunInTerminal = async (): Promise<void> => {
    if (authCommand) {
      try {
        await window.electronAPI.runInTerminal(authCommand)
      } catch (err) {
        // Fallback to copying if terminal open fails
        await navigator.clipboard.writeText(authCommand)
        console.error('Failed to open terminal:', err)
      }
    }
  }

  if (errorType !== 'auth') {
    // General error - simple display
    return (
      <div className="rounded-lg border border-destructive/50 bg-destructive/5 p-4">
        <div className="flex items-center gap-2 mb-2">
          <span className="text-xs font-mono px-2 py-0.5 rounded border border-destructive/50 text-destructive">
            ERROR
          </span>
        </div>
        <p className="text-sm text-muted-foreground">{message}</p>
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-destructive/30 bg-background p-4 space-y-4">
      {/* Error badges */}
      <div className="flex flex-wrap gap-2">
        <span className="text-xs font-mono px-2 py-1 rounded border border-destructive/50 text-destructive">
          SESSION ERROR
        </span>
        <span className="text-xs font-mono px-2 py-1 rounded border border-destructive/50 text-destructive">
          AUTHENTICATION REQUIRED
        </span>
      </div>

      {/* Resolution steps */}
      <div className="space-y-3">
        <p className="text-sm text-foreground">To resolve, please:</p>

        <ol className="space-y-3 text-sm list-none">
          {/* Step 1: Run command */}
          <li className="flex items-center gap-3">
            <span className="text-muted-foreground w-4 shrink-0">1.</span>
            <span>Run</span>
            <button
              onClick={handleRunInTerminal}
              className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-muted hover:bg-muted/80 transition-colors border border-border"
              title="Click to run in terminal"
            >
              <svg className="h-3.5 w-3.5" fill="currentColor" viewBox="0 0 24 24">
                <path d="M8 5v14l11-7z" />
              </svg>
              <code className="text-sm font-mono">{authCommand}</code>
            </button>
          </li>

          {/* Step 2: Send message again */}
          <li className="flex items-center gap-3">
            <span className="text-muted-foreground w-4 shrink-0">2.</span>
            <span>Send your last message again</span>
          </li>
        </ol>
      </div>

      {/* Additional info */}
      <p className="text-xs text-muted-foreground">
        This will authenticate {agentName}. Follow the prompts in your terminal to complete the
        login process.
      </p>
    </div>
  )
}
