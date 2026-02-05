"use client";

import { memo, useMemo } from "react";
import { MemoizedMarkdown } from "@multica/ui/components/markdown";
import { StreamingMarkdown } from "@multica/ui/components/markdown/StreamingMarkdown";
import { ToolCallItem } from "@multica/ui/components/tool-call-item";
import { ThinkingItem } from "@multica/ui/components/thinking-item";
import { cn, getTextContent } from "@multica/ui/lib/utils";
import type { Message } from "@multica/store";
import type { ContentBlock, ToolCall, ThinkingContent } from "@multica/sdk";

/** Extract toolCall blocks from content */
function getToolCalls(blocks: ContentBlock[]): ToolCall[] {
  return blocks.filter((b): b is ToolCall => b.type === "toolCall")
}

/** Extract concatenated thinking text from content blocks */
function getThinkingText(blocks: ContentBlock[]): string {
  return blocks
    .filter((b): b is ThinkingContent => b.type === "thinking")
    .map((b) => b.thinking ?? "")
    .join("")
}

/** Build a synthetic "running" toolResult Message from a ToolCall block */
function toRunningMessage(tc: ToolCall, agentId: string): Message {
  return {
    id: tc.id,
    role: "toolResult",
    content: [],
    agentId,
    toolCallId: tc.id,
    toolName: tc.name,
    toolArgs: tc.arguments,
    toolStatus: "running",
  }
}

interface MessageListProps {
  messages: Message[]
  streamingIds: Set<string>
}

export const MessageList = memo(function MessageList({ messages, streamingIds }: MessageListProps) {
  // Build a set of toolCallIds that already have a toolResult message,
  // so we don't render duplicate items from the assistant's toolCall blocks
  const resolvedToolCallIds = useMemo(() => {
    const ids = new Set<string>()
    for (const msg of messages) {
      if (msg.role === "toolResult" && msg.toolCallId) {
        ids.add(msg.toolCallId)
      }
    }
    return ids
  }, [messages])

  return (
    <div className="relative p-6 px-10 max-w-4xl mx-auto">
      {messages.map((msg) => {
        // ToolResult messages → render as tool execution item
        if (msg.role === "toolResult") {
          return <ToolCallItem key={msg.id} message={msg} />
        }

        const text = getTextContent(msg.content)
        const toolCalls = msg.role === "assistant" ? getToolCalls(msg.content) : []
        const thinking = msg.role === "assistant" ? getThinkingText(msg.content) : ""
        const hasThinkingBlocks = msg.role === "assistant" && msg.content.some((b) => b.type === "thinking")
        const isStreaming = streamingIds.has(msg.id)

        // Find toolCall blocks that don't have a toolResult message yet —
        // these are tools the LLM decided to call but haven't started executing
        const unresolvedToolCalls = toolCalls.filter((tc) => !resolvedToolCallIds.has(tc.id))

        // Skip completely empty messages (no text, no unresolved tools, no thinking, not streaming)
        if (!text && unresolvedToolCalls.length === 0 && !hasThinkingBlocks && !isStreaming) return null

        return (
          <div key={msg.id}>
            {/* Render thinking content (before text, matching LLM output order) */}
            {hasThinkingBlocks && (
              <ThinkingItem thinking={thinking} isStreaming={isStreaming} />
            )}

            {/* Render text content (if any) */}
            {(text || isStreaming) && (
              <div
                className={cn(
                  "flex",
                  msg.role === "user" ? "justify-end" : "justify-start"
                )}
              >
                <div
                  className={cn(
                    msg.role === "user" ? "bg-muted rounded-md max-w-[60%] py-1 px-2.5 my-2" : "w-full py-1 px-2.5 my-1"
                  )}
                >
                  {isStreaming ? (
                    <StreamingMarkdown content={text} isStreaming={true} mode="minimal" />
                  ) : (
                    <MemoizedMarkdown mode="minimal" id={msg.id}>
                      {text}
                    </MemoizedMarkdown>
                  )}
                </div>
              </div>
            )}

            {/* Render unresolved toolCall blocks as "running" tool items */}
            {unresolvedToolCalls.map((tc) => (
              <ToolCallItem key={tc.id} message={toRunningMessage(tc, msg.agentId)} />
            ))}
          </div>
        )
      })}
    </div>
  )
})
