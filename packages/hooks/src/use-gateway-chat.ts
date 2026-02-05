"use client";

import { useState, useEffect, useCallback } from "react";
import {
  type GatewayClient,
  type StreamPayload,
  type GetAgentMessagesResult,
  type ExecApprovalRequestPayload,
  type ApprovalDecision,
  StreamAction,
  ExecApprovalRequestAction,
} from "@multica/sdk";
import { useChat } from "./use-chat";

interface UseGatewayChatOptions {
  client: GatewayClient;
  hubId: string;
  agentId: string;
}

export function useGatewayChat({ client, hubId, agentId }: UseGatewayChatOptions) {
  const chat = useChat();
  const [isLoading, setIsLoading] = useState(false);
  const [isLoadingHistory, setIsLoadingHistory] = useState(true);

  // Fetch history
  useEffect(() => {
    client
      .request<GetAgentMessagesResult>(hubId, "getAgentMessages", { agentId, limit: 200 })
      .then((result) => chat.setHistory(result.messages, agentId))
      .catch(() => {})
      .finally(() => setIsLoadingHistory(false));
  }, [client, hubId, agentId]);

  // Subscribe to events
  useEffect(() => {
    client.onMessage((msg) => {
      if (msg.action === StreamAction) {
        const payload = msg.payload as StreamPayload;
        chat.handleStream(payload);
        if (payload.event.type === "message_start") setIsLoading(true);
        if (payload.event.type === "message_end") setIsLoading(false);
        return;
      }
      if (msg.action === ExecApprovalRequestAction) {
        chat.addApproval(msg.payload as ExecApprovalRequestPayload);
        return;
      }
      if (msg.action === "error") {
        chat.setError(msg.payload as { code: string; message: string });
        return;
      }
    });
    return () => { client.onMessage(() => {}); };
  }, [client]);

  const sendMessage = useCallback(
    (text: string) => {
      const trimmed = text.trim();
      if (!trimmed) return;
      chat.addUserMessage(trimmed, agentId);
      chat.setError(null);
      client.send(hubId, "message", { agentId, content: trimmed });
      setIsLoading(true);
    },
    [client, hubId, agentId],
  );

  const resolveApproval = useCallback(
    (approvalId: string, decision: ApprovalDecision) => {
      chat.removeApproval(approvalId);
      client.request(hubId, "resolveExecApproval", { approvalId, decision }).catch(() => {});
    },
    [client, hubId],
  );

  return {
    messages: chat.messages,
    streamingIds: chat.streamingIds,
    isLoading,
    isLoadingHistory,
    error: chat.error,
    pendingApprovals: chat.pendingApprovals,
    sendMessage,
    resolveApproval,
  };
}
