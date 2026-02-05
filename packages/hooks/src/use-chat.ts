"use client";

import { useState, useCallback } from "react";
import { v7 as uuidv7 } from "uuid";
import {
  type ContentBlock,
  type AgentEvent,
  type StreamPayload,
  type AgentMessageItem,
  type ExecApprovalRequestPayload,
  type ApprovalDecision,
} from "@multica/sdk";

export type ToolStatus = "running" | "success" | "error" | "interrupted";

export interface Message {
  id: string;
  role: "user" | "assistant" | "toolResult";
  content: ContentBlock[];
  agentId: string;
  stopReason?: string;
  toolCallId?: string;
  toolName?: string;
  toolArgs?: Record<string, unknown>;
  toolStatus?: ToolStatus;
  isError?: boolean;
}

export interface ChatError {
  code: string;
  message: string;
}

export interface PendingApproval extends ExecApprovalRequestPayload {
  receivedAt: number;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function toContentBlocks(content: string | ContentBlock[]): ContentBlock[] {
  if (typeof content === "string") {
    return content ? [{ type: "text", text: content }] : [];
  }
  if (Array.isArray(content)) return content;
  return [];
}

function extractContent(event: AgentEvent): ContentBlock[] {
  if (!("message" in event)) return [];
  const msg = event.message;
  if (!msg || !("content" in msg)) return [];
  const content = msg.content;
  return Array.isArray(content) ? (content as ContentBlock[]) : [];
}

// ---------------------------------------------------------------------------
// useChat — pure state hook, no IO, no side effects
// ---------------------------------------------------------------------------

export function useChat() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [streamingIds, setStreamingIds] = useState<Set<string>>(new Set());
  const [pendingApprovals, setPendingApprovals] = useState<PendingApproval[]>([]);
  const [error, setError] = useState<ChatError | null>(null);

  const isStreaming = streamingIds.size > 0;

  /** Load history: convert raw AgentMessageItem[] → Message[] */
  const setHistory = useCallback((raw: AgentMessageItem[], agentId: string) => {
    const toolCallArgsMap = new Map<string, { name: string; args: Record<string, unknown> }>();
    for (const m of raw) {
      if (m.role === "assistant") {
        for (const block of m.content) {
          if (block.type === "toolCall") {
            toolCallArgsMap.set(block.id, { name: block.name, args: block.arguments });
          }
        }
      }
    }

    const loaded: Message[] = [];
    for (const m of raw) {
      if (m.role === "user") {
        loaded.push({ id: uuidv7(), role: "user", content: toContentBlocks(m.content), agentId });
      } else if (m.role === "assistant") {
        loaded.push({ id: uuidv7(), role: "assistant", content: toContentBlocks(m.content), agentId, stopReason: m.stopReason });
      } else if (m.role === "toolResult") {
        const callInfo = toolCallArgsMap.get(m.toolCallId);
        loaded.push({
          id: uuidv7(),
          role: "toolResult",
          content: toContentBlocks(m.content),
          agentId,
          toolCallId: m.toolCallId,
          toolName: m.toolName,
          toolArgs: callInfo?.args,
          toolStatus: m.isError ? "error" : "success",
          isError: m.isError,
        });
      }
    }

    setMessages(loaded);
  }, []);

  /** Add a user message */
  const addUserMessage = useCallback((text: string, agentId: string) => {
    setMessages((prev) => [
      ...prev,
      { id: uuidv7(), role: "user", content: [{ type: "text", text }], agentId },
    ]);
  }, []);

  /** Process a StreamPayload → update messages + streamingIds */
  const handleStream = useCallback((payload: StreamPayload) => {
    const { event } = payload;

    switch (event.type) {
      case "message_start": {
        const newMsg: Message = {
          id: payload.streamId,
          role: "assistant",
          content: [],
          agentId: payload.agentId,
        };
        const content = extractContent(event);
        if (content.length) newMsg.content = content;

        setMessages((prev) => [...prev, newMsg]);
        setStreamingIds((prev) => new Set(prev).add(payload.streamId));
        break;
      }
      case "message_update": {
        const content = extractContent(event);
        setMessages((prev) =>
          prev.map((m) => (m.id === payload.streamId ? { ...m, content } : m)),
        );
        break;
      }
      case "message_end": {
        const content = extractContent(event);
        const stopReason =
          "message" in event
            ? (event.message as { stopReason?: string })?.stopReason
            : undefined;

        setMessages((prev) =>
          prev.map((m) => {
            if (m.id === payload.streamId) return { ...m, content, stopReason };
            if (m.role === "toolResult" && m.toolStatus === "running" && m.agentId === payload.agentId) {
              return { ...m, toolStatus: "interrupted" as ToolStatus };
            }
            return m;
          }),
        );
        setStreamingIds((prev) => {
          const next = new Set(prev);
          next.delete(payload.streamId);
          return next;
        });
        break;
      }
      case "tool_execution_start": {
        setMessages((prev) => [
          ...prev,
          {
            id: uuidv7(),
            role: "toolResult",
            content: [],
            agentId: payload.agentId,
            toolCallId: event.toolCallId,
            toolName: event.toolName,
            toolArgs: event.args as Record<string, unknown> | undefined,
            toolStatus: "running",
            isError: false,
          },
        ]);
        break;
      }
      case "tool_execution_end": {
        setMessages((prev) =>
          prev.map((m) =>
            m.role === "toolResult" && m.toolCallId === event.toolCallId
              ? {
                  ...m,
                  toolStatus: (event.isError ? "error" : "success") as ToolStatus,
                  isError: event.isError ?? false,
                  content:
                    event.result != null
                      ? [{ type: "text" as const, text: typeof event.result === "string" ? event.result : JSON.stringify(event.result) }]
                      : [],
                }
              : m,
          ),
        );
        break;
      }
      case "tool_execution_update":
        break;
    }
  }, []);

  /** Add pending approval */
  const addApproval = useCallback((payload: ExecApprovalRequestPayload) => {
    setPendingApprovals((prev) => [...prev, { ...payload, receivedAt: Date.now() }]);
  }, []);

  /** Remove pending approval */
  const removeApproval = useCallback((approvalId: string) => {
    setPendingApprovals((prev) => prev.filter((a) => a.approvalId !== approvalId));
  }, []);

  return {
    // Rendering state
    messages,
    streamingIds,
    isStreaming,
    pendingApprovals,
    error,
    // State control (for transport layer to call)
    setError,
    setHistory,
    addUserMessage,
    handleStream,
    addApproval,
    removeApproval,
  };
}

export type UseChatReturn = ReturnType<typeof useChat>;
