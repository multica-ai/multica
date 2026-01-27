/**
 * Message grouping utility - transforms session updates into displayable messages
 * Extracted from ChatView for better maintainability and testability
 */
import type { StoredSessionUpdate } from '../../../shared/types'
import type { ToolCall, AnsweredResponse } from '../components/ToolCallItem'

// Content block types for preserving time order
export interface TextBlock {
  type: 'text'
  content: string
}

export interface ImageBlock {
  type: 'image'
  data: string
  mimeType: string
}

export interface ThoughtBlock {
  type: 'thought'
  content: string
}

export interface ToolCallBlock {
  type: 'tool_call'
  toolCall: ToolCall
}

// Plan entry from ACP protocol (TodoWrite tool)
export interface PlanEntry {
  content: string
  status: 'pending' | 'in_progress' | 'completed'
  priority?: 'high' | 'medium' | 'low'
}

export interface PlanBlock {
  type: 'plan'
  entries: PlanEntry[]
}

export interface ErrorBlock {
  type: 'error'
  errorType: 'auth' | 'general'
  agentId?: string
  authCommand?: string
  message: string
}

export type ContentBlock =
  | TextBlock
  | ImageBlock
  | ThoughtBlock
  | ToolCallBlock
  | PlanBlock
  | ErrorBlock

// Current action for dynamic status label (only meaningful for last message during processing)
export interface CurrentAction {
  type: 'thinking' | 'writing' | 'tool'
  toolName?: string
  toolStatus?: string
}

export interface Message {
  role: 'user' | 'assistant'
  blocks: ContentBlock[]
  // Time tracking for assistant messages
  startTime?: string // ISO 8601, first event timestamp
  endTime?: string // ISO 8601, last event timestamp
  // Timestamp when the last event was received (Date.now() value, for LiveTimer projection)
  lastEventTimestamp?: number
  // Current action for dynamic status label (only meaningful for last message during processing)
  currentAction?: CurrentAction
}

export function groupUpdatesIntoMessages(updates: StoredSessionUpdate[]): Message[] {
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
  // Track when the last event was received (Date.now() value, for LiveTimer projection)
  let lastEventTimestamp: number | undefined
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
        lastEventTimestamp,
        currentAction
      })
      currentBlocks = []
      toolCallMap.clear()
      addedToolCallIds.clear()
      // Reset tracking for next message
      assistantStartTime = undefined
      assistantEndTime = undefined
      lastEventTimestamp = undefined
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
        // Track when the frontend received this event (for LiveTimer projection)
        lastEventTimestamp = new Date(stored.timestamp).getTime()
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
        // Track when the frontend received this event (for LiveTimer projection)
        lastEventTimestamp = new Date(stored.timestamp).getTime()
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
        // Track when the frontend received this event (for LiveTimer projection)
        lastEventTimestamp = new Date(stored.timestamp).getTime()
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
        // Track when the frontend received this event (for LiveTimer projection)
        lastEventTimestamp = new Date(stored.timestamp).getTime()
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
