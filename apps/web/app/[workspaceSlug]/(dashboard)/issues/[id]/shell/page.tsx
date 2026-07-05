"use client";

import Link from "next/link";
import { use, useEffect, useMemo, useRef, useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Loader2, Terminal } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { useChatStore } from "@multica/core/chat";
import { chatKeys, chatMessagesPageOptions, pendingChatTaskOptions } from "@multica/core/chat/queries";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useAgentPresenceDetail } from "@multica/core/agents";
import type { ChatPendingTask, ChatSession } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { ChatInput } from "@multica/views/chat/components/chat-input";
import { ChatMessageList } from "@multica/views/chat/components/chat-message-list";

export default function IssueShellPage({
  params,
}: {
  params: Promise<{ workspaceSlug: string; id: string }>;
}) {
  const { workspaceSlug, id } = use(params);
  return (
    <ErrorBoundary resetKeys={[id]}>
      <IssueShellPageInner issueId={id} workspaceSlug={workspaceSlug} />
    </ErrorBoundary>
  );
}

function IssueShellPageInner({
  issueId,
  workspaceSlug,
}: {
  issueId: string;
  workspaceSlug: string;
}) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: issue, isLoading: issueLoading } = useQuery(issueDetailOptions(wsId, issueId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const [session, setSession] = useState<ChatSession | null>(null);
  const [sessionError, setSessionError] = useState<string | null>(null);
  const createStartedRef = useRef(false);
  const { uploadWithToast } = useFileUpload(api);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);

  useEffect(() => {
    if (!issue || createStartedRef.current) return;
    createStartedRef.current = true;
    let cancelled = false;
    void api.createIssueShellSession(issue.id)
      .then((created) => {
        if (cancelled) return;
        setSession(created);
        qc.setQueryData(chatKeys.session(wsId, created.id), created);
        setActiveSession(created.id);
        setSelectedAgentId(created.agent_id);
      })
      .catch((err) => {
        if (cancelled) return;
        setSessionError(err instanceof Error ? err.message : "Failed to open issue shell");
      });
    return () => {
      cancelled = true;
    };
  }, [issue, qc, setActiveSession, setSelectedAgentId, wsId]);

  const activeAgent = useMemo(
    () => agents.find((agent) => agent.id === (session?.agent_id ?? issue?.assignee_id ?? "")) ?? null,
    [agents, issue?.assignee_id, session?.agent_id],
  );
  const presenceDetail = useAgentPresenceDetail(wsId, activeAgent?.id);
  const availability = presenceDetail === "loading" ? undefined : presenceDetail.availability;
  const sessionId = session?.id ?? "";

  const {
    data: rawMessagePages,
    isLoading: messagesLoading,
    fetchNextPage: fetchOlderMessages,
    hasNextPage: hasOlderMessages,
    isFetchingNextPage: isFetchingOlderMessages,
  } = useInfiniteQuery(chatMessagesPageOptions(sessionId));
  const messagePages = sessionId ? rawMessagePages?.pages ?? [] : [];
  const messages = useMemo(
    () => [...messagePages].reverse().flatMap((page) => page.messages),
    [messagePages],
  );
  const { data: pendingTask } = useQuery(pendingChatTaskOptions(sessionId));

  useEffect(() => {
    if (!sessionId) return;
    void api.markChatSessionRead(sessionId).catch(() => {});
  }, [sessionId, messages.length]);

  const handleSend = async (
    content: string,
    attachmentIds?: string[],
  ): Promise<boolean> => {
    if (!sessionId) return false;
    try {
      const result = await api.sendChatMessage(sessionId, content, attachmentIds);
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), {
        task_id: result.task_id,
        status: "queued",
        created_at: result.created_at,
      });
      qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
      return true;
    } catch {
      return false;
    }
  };

  const handleUploadFile = async (file: File) => {
    if (!sessionId) return null;
    return uploadWithToast(file, { chatSessionId: sessionId });
  };

  if (issueLoading || !issue) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="mx-auto flex h-[calc(100vh-7rem)] w-full max-w-6xl flex-col gap-4 px-4 py-4 md:px-6">
      <div className="flex items-center justify-between gap-3 rounded-2xl border bg-card px-4 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Terminal className="h-4 w-4" />
            <span>Issue shell</span>
          </div>
          <div className="truncate text-lg font-semibold">
            {issue.identifier}: {issue.title}
          </div>
          <div className="text-sm text-muted-foreground">
            {activeAgent ? `Attached to ${activeAgent.name}` : "Waiting for assigned agent"}
          </div>
        </div>
        <Button asChild variant="outline" size="sm">
          <Link href={`/${workspaceSlug}/issues/${issue.id}`}>
            <ArrowLeft className="h-4 w-4" />
            Back to issue
          </Link>
        </Button>
      </div>

      {sessionError ? (
        <div className="rounded-2xl border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
          {sessionError}
        </div>
      ) : null}

      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-3xl border bg-background">
        {!session ? (
          <div className="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            Opening issue shell...
          </div>
        ) : (
          <>
            <ChatMessageList
              messages={messages}
              pendingTask={pendingTask}
              availability={availability}
              hasOlderMessages={!!hasOlderMessages}
              isFetchingOlderMessages={isFetchingOlderMessages}
              onLoadOlderMessages={() => {
                void fetchOlderMessages();
              }}
            />
            {!messagesLoading && (
              <ChatInput
                onSend={handleSend}
                onUploadFile={handleUploadFile}
                isRunning={!!pendingTask?.task_id}
                disabled={session.status === "archived"}
                agentName={activeAgent?.name}
              />
            )}
          </>
        )}
      </div>
    </div>
  );
}
