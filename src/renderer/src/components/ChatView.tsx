/**
 * Chat view component - displays messages and tool calls
 * Note: Scroll behavior is managed by parent (App.tsx) for unified scroll context
 */
import { useState, useMemo } from 'react'
import type { StoredSessionUpdate } from '../../../shared/types'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import {
  ChevronDown,
  ChevronRight,
  CheckCircle2,
  CircleDashed,
  Loader2,
  Folder
} from 'lucide-react'
import { ToolCallItem } from './ToolCallItem'
import { PermissionRequestItem } from './permission'
import { usePermissionStore } from '../stores/permissionStore'
import { cn } from '@/lib/utils'
import { MessageTimer, StatusIndicator, Spinner } from './ui/LoadingIndicator'
import { CompletedMessageFooter } from './ui/CompletedMessageFooter'
import { Markdown, StreamingMarkdown } from './markdown'
import {
  groupUpdatesIntoMessages,
  type Message,
  type ContentBlock,
  type TextBlock,
  type ImageBlock,
  type PlanEntry,
  type CurrentAction
} from '../utils/messageGrouping'

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
        <div className="max-w-[85%] rounded-lg bg-[#f9f7f5] dark:bg-muted px-4 py-3 text-sm break-words overflow-hidden">
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
      lastEventTimestamp={message.lastEventTimestamp}
      isProcessing={isLastMessage && isProcessing}
      modelName={modelName}
      agentName={agentName}
      currentAction={currentAction}
    />
  )
}

// Render a single content block
function renderContentBlock(
  block: ContentBlock,
  idx: number,
  isStreaming: boolean = false
): React.JSX.Element | null {
  switch (block.type) {
    case 'thought':
      return <ThoughtBlockView key={`thought-${idx}`} text={block.content} />
    case 'tool_call':
      return <ToolCallItem key={block.toolCall.id} toolCall={block.toolCall} />
    case 'text':
      return (
        <TextContentBlock key={`text-${idx}`} content={block.content} isStreaming={isStreaming} />
      )
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
  lastEventTimestamp,
  isProcessing,
  modelName,
  agentName,
  currentAction
}: {
  blocks: ContentBlock[]
  isComplete: boolean
  startTime?: string
  endTime?: string
  lastEventTimestamp?: number
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

  // Streaming state: only the last text block is streaming when message is not complete
  const isStreaming = !isComplete

  // Not collapsible - render all blocks normally
  if (!shouldCollapse) {
    return (
      <div className="space-y-3">
        {blocks.map((block, idx) => {
          // Only the last text block gets streaming optimization
          const isLastTextBlock =
            block.type === 'text' && blocks.slice(idx + 1).every((b) => b.type !== 'text')
          return renderContentBlock(block, idx, isStreaming && isLastTextBlock)
        })}
        {/* Processing: show spinner + time + label */}
        {isProcessing && (
          <MessageTimer
            startTime={startTime}
            endTime={endTime}
            lastEventTimestamp={lastEventTimestamp}
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
function TextContentBlock({
  content,
  isStreaming = false
}: {
  content: string
  isStreaming?: boolean
}): React.JSX.Element | null {
  if (!content) return null

  return (
    <div className="max-w-none leading-[1.7] break-words overflow-hidden [&_*]:break-words">
      {isStreaming ? (
        <StreamingMarkdown content={content} isStreaming={true} mode="minimal" />
      ) : (
        <Markdown mode="minimal">{content}</Markdown>
      )}
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
            <Markdown mode="terminal">{text}</Markdown>
          </div>
        </CollapsibleContent>

        {/* Preview when collapsed */}
        {!isExpanded && isLong && (
          <div className="mt-2 text-sm text-muted-foreground line-clamp-3">
            <Markdown mode="terminal">{text}</Markdown>
          </div>
        )}

        {/* Show full content when not long */}
        {!isLong && (
          <div className="mt-2 text-sm text-muted-foreground">
            <Markdown mode="terminal">{text}</Markdown>
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
          <CircleDashed className="h-3.5 w-3.5 text-muted-foreground" />
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
                <CircleDashed className="h-3 w-3 text-muted-foreground/50 flex-shrink-0 mt-0.5" />
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
