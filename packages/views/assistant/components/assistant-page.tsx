"use client";

import React, { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useChatStore } from "@multica/core/chat";
import { useAuthStore } from "@multica/core/auth";
import { chatSessionsOptions, chatMessagesOptions, pendingChatTaskOptions } from "@multica/core/chat/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useAgentPresenceDetail } from "@multica/core/agents";
import { api } from "@multica/core/api";
import { ChatMessageList } from "../../chat/components/chat-message-list";
import { ChatInput } from "../../chat/components/chat-input";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { SessionList } from "./session-list";
import { createLogger } from "@multica/core/logger";

const logger = createLogger("assistant.page");

export function AssistantPage() {
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const setActiveSession = useChatStore((s) => s.setActiveSession);

  // 获取所有会话
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));

  // 获取当前会话的消息
  const { data: rawMessages, isLoading: messagesLoading } = useQuery(
    chatMessagesOptions(activeSessionId ?? ""),
  );
  const messages = activeSessionId ? rawMessages ?? [] : [];

  // 获取当前会话的运行状态
  const { data: pendingTask } = useQuery(
    pendingChatTaskOptions(activeSessionId ?? ""),
  );
  const pendingTaskId = pendingTask?.task_id ?? null;

  // 获取所有 agents
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  // 当前会话对应的 agent
  const currentSession = sessions.find((s) => s.id === activeSessionId);
  const currentAgent = agents.find((a) => a.id === currentSession?.agent_id);

  // Agent 可用性状态
  const presenceDetail = useAgentPresenceDetail(wsId, currentAgent?.id);
  const availability = presenceDetail === "loading" ? undefined : presenceDetail.availability;

  const { uploadWithToast } = useFileUpload(api);

  // 发送消息
  const handleSend = useCallback(
    async (content: string, attachmentIds?: string[]) => {
      if (!activeSessionId) {
        logger.warn("handleSend: no active session");
        return;
      }

      logger.info("sendMessage", { sessionId: activeSessionId, contentLength: content.length });

      try {
        await api.sendChatMessage(activeSessionId, content, attachmentIds);
      } catch (error) {
        logger.error("sendMessage failed", { error });
      }
    },
    [activeSessionId],
  );

  // 上传文件
  const handleUploadFile = useCallback(
    async (file: File) => {
      if (!activeSessionId) {
        logger.warn("handleUploadFile: no active session");
        return null;
      }

      return uploadWithToast(file, { chatSessionId: activeSessionId });
    },
    [activeSessionId, uploadWithToast],
  );

  // 停止任务
  const handleStop = useCallback(() => {
    if (!pendingTaskId) {
      logger.debug("handleStop: no pending task");
      return;
    }

    logger.info("cancelTask", { taskId: pendingTaskId });
    api.cancelTaskById(pendingTaskId).catch((err) => {
      logger.warn("cancelTask failed", { error: err });
    });
  }, [pendingTaskId]);

  // 选择会话
  const handleSelectSession = useCallback(
    (sessionId: string) => {
      setActiveSession(sessionId);
    },
    [setActiveSession],
  );

  const showSkeleton = !!activeSessionId && messagesLoading;
  const hasMessages = messages.length > 0 || !!pendingTaskId;

  return (
    <div className="flex h-screen">
      {/* 左侧会话列表 */}
      <SessionList
        sessions={sessions}
        agents={agents}
        activeSessionId={activeSessionId}
        onSelectSession={handleSelectSession}
      />

      {/* 右侧消息区域 */}
      <div className="flex-1 flex flex-col border-l">
        {activeSessionId ? (
          <>
            {/* 消息列表 - 复用现有组件 */}
            <div className="flex-1 overflow-hidden">
              <ChatMessageList
                messages={messages}
                pendingTask={pendingTask}
                availability={availability}
              />
            </div>

            {/* 输入区域 - 复用现有组件 */}
            <ChatInput
              onSend={handleSend}
              onUploadFile={handleUploadFile}
              onStop={handleStop}
              isRunning={!!pendingTaskId}
              disabled={false}
              noAgent={!currentAgent}
              agentName={currentAgent?.name}
            />
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center space-y-2">
              <h3 className="text-lg font-semibold text-muted-foreground">选择一个会话</h3>
              <p className="text-sm text-muted-foreground">从左侧列表选择或创建新会话</p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
