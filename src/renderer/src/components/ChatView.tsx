/**
 * Chat view component - displays messages and tool calls
 */
import { useEffect, useRef } from 'react'
import type { StoredSessionUpdate } from '../../../shared/types'

interface ChatViewProps {
  updates: StoredSessionUpdate[]
  isProcessing: boolean
}

export function ChatView({ updates, isProcessing }: ChatViewProps) {
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
          <p className="text-[var(--color-text-muted)]">
            Start a conversation with your coding agent
          </p>
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
  let currentToolCalls: ToolCall[] = []
  const toolCallMap = new Map<string, ToolCall>()

  for (const stored of updates) {
    const update = stored.update.update
    if (!update || !('sessionUpdate' in update)) continue

    switch (update.sessionUpdate) {
      case 'user_message' as string:
        // Flush any pending assistant message
        if (currentAssistantContent || currentToolCalls.length > 0) {
          messages.push({
            role: 'assistant',
            content: currentAssistantContent,
            toolCalls: currentToolCalls,
          })
          currentAssistantContent = ''
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
              toolCalls: [],
            })
          }
        }
        break

      case 'agent_message_chunk':
        if ('content' in update && update.content?.type === 'text') {
          currentAssistantContent = update.content.text
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
              : update.rawInput
                ? JSON.stringify(update.rawInput, null, 2)
                : undefined,
          }
          toolCallMap.set(update.toolCallId, toolCall)
          currentToolCalls = Array.from(toolCallMap.values())
        }
        break

      case 'tool_call_update':
        if ('toolCallId' in update && toolCallMap.has(update.toolCallId)) {
          const existing = toolCallMap.get(update.toolCallId)!
          if (update.status) existing.status = update.status
          if (update.rawOutput) {
            existing.output = typeof update.rawOutput === 'string'
              ? update.rawOutput
              : JSON.stringify(update.rawOutput, null, 2)
          }
          currentToolCalls = Array.from(toolCallMap.values())
        }
        break
    }
  }

  // Flush any remaining assistant content
  if (currentAssistantContent || currentToolCalls.length > 0) {
    messages.push({
      role: 'assistant',
      content: currentAssistantContent,
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
  const statusIcon =
    toolCall.status === 'completed' ? (
      <span className="text-green-500">✓</span>
    ) : toolCall.status === 'running' ? (
      <LoadingDots />
    ) : (
      <span className="text-yellow-500">●</span>
    )

  return (
    <div className="rounded border border-[var(--color-border)] bg-[var(--color-background)] p-2 text-xs">
      <div className="flex items-center gap-2">
        {statusIcon}
        <span className="font-medium">{toolCall.title}</span>
        {toolCall.kind && (
          <span className="text-[var(--color-text-muted)]">({toolCall.kind})</span>
        )}
      </div>
      {toolCall.input && (
        <pre className="mt-1 max-h-24 overflow-auto rounded bg-black/20 p-1 text-[var(--color-text-muted)]">
          {toolCall.input.slice(0, 200)}
          {toolCall.input.length > 200 && '...'}
        </pre>
      )}
    </div>
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
