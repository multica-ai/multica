"use client";

import React, { useCallback, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useChatStore } from "@multica/core/chat";
import { useAuthStore } from "@multica/core/auth";
import { chatSessionsOptions, chatMessagesOptions, pendingChatTaskOptions, chatKeys } from "@multica/core/chat/queries";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useAgentPresenceDetail } from "@multica/core/agents";
import { useCreateChatSession } from "@multica/core/chat/mutations";
import { api } from "@multica/core/api";
import { ChatMessageList } from "../../chat/components/chat-message-list";
import { ChatInput } from "../../chat/components/chat-input";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { SessionList } from "./session-list";
import { NewSessionDialog } from "./new-session-dialog";
import { createLogger } from "@multica/core/logger";
import type { ChatMessage } from "@multica/core/types";

const logger = createLogger("assistant.page");

export function AssistantPage() {
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const qc = useQueryClient();

  // 新建会话对话框状态
  const [showNewSessionDialog, setShowNewSessionDialog] = useState(false);

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

  // 获取所有 members
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  // 获取所有运行时
  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(runtimeListOptions(wsId));

  // 当前会话对应的 agent
  const currentSession = sessions.find((s) => s.id === activeSessionId);
  const currentAgent = agents.find((a) => a.id === currentSession?.agent_id);

  // Agent 可用性状态
  const presenceDetail = useAgentPresenceDetail(wsId, currentAgent?.id);
  const availability = presenceDetail === "loading" ? undefined : presenceDetail.availability;

  const { uploadWithToast } = useFileUpload(api);
  const createSession = useCreateChatSession();

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

  // 创建新会话
  const handleCreateSession = useCallback(
    async (agentId: string, runtimeId: string) => {
      logger.info("createSession", { agentId, runtimeId });

      try {
        const session = await createSession.mutateAsync({
          agent_id: agentId,
          title: "",
        });

        // 预先设置空消息列表，避免加载闪烁
        qc.setQueryData<ChatMessage[]>(chatKeys.messages(session.id), []);

        // 切换到新会话
        setActiveSession(session.id);

        // 关闭对话框
        setShowNewSessionDialog(false);

        logger.info("createSession success", { sessionId: session.id });
      } catch (error) {
        logger.error("createSession failed", { error });
      }
    },
    [createSession, qc, setActiveSession],
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
        onNewSession={() => setShowNewSessionDialog(true)}
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

      {/* 新建会话对话框 */}
      <NewSessionDialog
        open={showNewSessionDialog}
        onOpenChange={setShowNewSessionDialog}
        agents={agents}
        runtimes={runtimes}
        runtimesLoading={runtimesLoading}
        members={members}
        currentUserId={user?.id ?? null}
        onCreateSession={handleCreateSession}
      />
    </div>
  );
}
