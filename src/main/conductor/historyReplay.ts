/**
 * History replay utilities for session resumption
 * Formats conversation history for prepending to prompts when agent restarts
 */
import type { StoredSessionUpdate } from '../../shared/types'
import type { MessageContent } from '../../shared/types/message'

// Token estimation: ~4 bytes per token (same heuristic as Codex)
const APPROX_BYTES_PER_TOKEN = 4

/**
 * Extract text from user message content
 * Supports both new format (MessageContent[]) and old format ({ text: string })
 */
function extractUserMessageText(content: unknown): string {
  if (!content) return ''

  // New format: MessageContent[] (array of content items)
  if (Array.isArray(content)) {
    const textItem = (content as MessageContent).find((item) => item.type === 'text')
    return textItem?.type === 'text' ? textItem.text : ''
  }

  // Old format: { type: 'text', text: string } or { text: string }
  if (typeof content === 'object') {
    const obj = content as { text?: string }
    if (typeof obj.text === 'string') {
      return obj.text
    }
  }

  return ''
}

// Default max tokens for history (leaves room for user prompt and response)
const DEFAULT_MAX_HISTORY_TOKENS = 20000

/**
 * Estimate token count from text
 */
function estimateTokens(text: string): number {
  return Math.ceil(text.length / APPROX_BYTES_PER_TOKEN)
}

/**
 * Message extracted from session updates
 */
interface ExtractedMessage {
  role: 'user' | 'assistant'
  content: string
  toolSummary?: string // e.g., "[Used: Read file.ts, Edit file.ts]"
}

/**
 * Extract messages from session updates
 * Combines streaming chunks and summarizes tool calls
 */
function extractMessages(updates: StoredSessionUpdate[]): ExtractedMessage[] {
  const messages: ExtractedMessage[] = []
  let currentAssistantContent = ''
  let currentToolCalls: string[] = []

  for (const update of updates) {
    const inner = update.update?.update
    if (!inner || !('sessionUpdate' in inner)) continue

    // Cast to string for comparison - 'user_message' is a custom internal type not in ACP schema
    const updateType = inner.sessionUpdate as string

    if (updateType === 'user_message') {
      // Flush any pending assistant message
      if (currentAssistantContent || currentToolCalls.length > 0) {
        messages.push({
          role: 'assistant',
          content: currentAssistantContent,
          toolSummary:
            currentToolCalls.length > 0 ? `[Used: ${currentToolCalls.join(', ')}]` : undefined
        })
        currentAssistantContent = ''
        currentToolCalls = []
      }

      // Add user message (custom internal type)
      const rawContent = (inner as { content?: unknown }).content
      const content = extractUserMessageText(rawContent)
      if (content) {
        messages.push({ role: 'user', content })
      }
    } else if (updateType === 'agent_message_chunk') {
      // Accumulate assistant content (only text type)
      // Note: Streaming chunks contain cumulative content, so we use the latest value
      const chunk = inner as { content?: { type?: string; text?: string } }
      if (chunk.content?.type === 'text' && chunk.content?.text) {
        currentAssistantContent = chunk.content.text
      }
    } else if (updateType === 'tool_call') {
      // Record tool call with context (e.g., "Read src/file.ts")
      const toolCall = inner as { title?: string; name?: string }
      const toolName = toolCall.title || toolCall.name || 'Unknown tool'
      // Always add to preserve context when same tool is used multiple times
      currentToolCalls.push(toolName)
    }
  }

  // Flush final assistant message
  if (currentAssistantContent || currentToolCalls.length > 0) {
    messages.push({
      role: 'assistant',
      content: currentAssistantContent,
      toolSummary:
        currentToolCalls.length > 0 ? `[Used: ${currentToolCalls.join(', ')}]` : undefined
    })
  }

  return messages
}

/**
 * Format a single message to string
 */
function formatMessage(msg: ExtractedMessage): string {
  if (msg.role === 'user') {
    return `USER: ${msg.content}\n`
  } else {
    let line = `ASSISTANT: ${msg.content}`
    if (msg.toolSummary) {
      line += `\n${msg.toolSummary}`
    }
    return line + '\n'
  }
}

/**
 * Format messages into history string
 */
function formatMessages(messages: ExtractedMessage[]): string {
  return messages.map(formatMessage).join('\n')
}

/**
 * Format session history for replay
 * Returns formatted history string to prepend to first prompt
 *
 * @param updates - Session updates from SessionStore
 * @param maxTokens - Maximum tokens for history (default 20000)
 * @returns Formatted history string, or null if no meaningful history
 */
export function formatHistoryForReplay(
  updates: StoredSessionUpdate[],
  maxTokens: number = DEFAULT_MAX_HISTORY_TOKENS
): string | null {
  // Extract messages from updates
  const messages = extractMessages(updates)

  if (messages.length === 0) {
    return null
  }

  // Pre-compute token estimates for each message (O(n) instead of O(n²))
  const messageTokens = messages.map((msg) => {
    const formatted = formatMessage(msg)
    // Add ~1 token for the newline separator between messages
    return estimateTokens(formatted) + 1
  })
  const totalTokens = messageTokens.reduce((sum, t) => sum + t, 0)

  // If within budget, return as-is
  if (totalTokens <= maxTokens) {
    return wrapHistory(formatMessages(messages), messages.length)
  }

  // Find truncation point using cumulative sums (O(n) instead of O(n²))
  let startIndex = 0
  let currentTokens = totalTokens
  while (startIndex < messages.length - 1 && currentTokens > maxTokens) {
    currentTokens -= messageTokens[startIndex]
    startIndex++
  }

  // Format remaining messages
  const truncatedMessages = messages.slice(startIndex)
  let formatted = formatMessages(truncatedMessages)

  // Add truncation notice
  if (startIndex > 0) {
    formatted = `[${startIndex} earlier messages truncated...]\n\n${formatted}`
  }

  return wrapHistory(formatted, messages.length, startIndex)
}

/**
 * Wrap history with header/footer markers
 */
function wrapHistory(content: string, totalMessages: number, truncatedCount: number = 0): string {
  const header =
    truncatedCount > 0
      ? `[Session History - ${totalMessages} messages, ${truncatedCount} truncated]`
      : `[Session History - ${totalMessages} messages]`

  return `${header}

${content}
[End of History]

Continue the conversation. The user's new message follows:

`
}

/**
 * Check if history is meaningful enough to replay
 * (e.g., has at least one complete exchange)
 * Uses lightweight scan without full message extraction
 */
export function hasReplayableHistory(updates: StoredSessionUpdate[]): boolean {
  let hasUser = false
  let hasAssistant = false

  for (const update of updates) {
    const inner = update.update?.update
    if (!inner || !('sessionUpdate' in inner)) continue

    // Cast to string - 'user_message' is a custom internal type not in ACP schema
    const updateType = inner.sessionUpdate as string

    if (updateType === 'user_message') {
      const rawContent = (inner as { content?: unknown }).content
      const content = extractUserMessageText(rawContent)
      if (content) {
        hasUser = true
      }
    } else if (updateType === 'agent_message_chunk') {
      const chunk = inner as { content?: { type?: string; text?: string } }
      if (chunk.content?.type === 'text' && chunk.content?.text) {
        hasAssistant = true
      }
    }

    // Early exit once we've found both
    if (hasUser && hasAssistant) {
      return true
    }
  }

  return hasUser && hasAssistant
}
