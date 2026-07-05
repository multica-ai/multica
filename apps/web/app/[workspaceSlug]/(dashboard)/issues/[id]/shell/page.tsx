"use client";

import Link from "next/link";
import { use, useMemo } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Loader2, Terminal } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { useAuthStore } from "@multica/core/auth";
import { useIssueTimeline } from "@multica/views/issues/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useAgentPresenceDetail } from "@multica/core/agents";
import type { ChatMessage, ChatPendingTask } from "@multica/core/types";
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
  const user = useAuthStore((s) => s.user);

  const { data: issue, isLoading: issueLoading } = useQuery(issueDetailOptions(wsId, issueId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const { timeline = [], loading: messagesLoading, submitComment } = useIssueTimeline(issueId, user?.id);

  const activeAgent = useMemo(
    () => agents.find((agent) => agent.id === (issue?.assignee_id ?? "")) ?? null,
    [agents, issue?.assignee_id],
  );

  const presenceDetail = useAgentPresenceDetail(wsId, activeAgent?.id);
  const availability = presenceDetail === "loading" ? undefined : presenceDetail.availability;

  const { data: activeTasksData } = useQuery({
    queryKey: ["issues", wsId, issueId, "active-tasks"],
    queryFn: () => api.getActiveTasksForIssue(issueId),
    enabled: !!issueId,
    refetchInterval: 2000,
  });

  const activeTask = activeTasksData?.tasks?.[0] ?? null;

  const pendingTask = useMemo<ChatPendingTask | undefined>(() => {
    if (!activeTask) return undefined;
    return {
      task_id: activeTask.id,
      status: activeTask.status,
      created_at: activeTask.started_at ?? activeTask.dispatched_at ?? "",
    };
  }, [activeTask]);

  const messages = useMemo<ChatMessage[]>(() => {
    return timeline
      .filter((entry) => entry.type === "comment")
      .map((entry) => ({
        id: entry.id,
        chat_session_id: "",
        role: entry.actor_type === "agent" ? "assistant" : "user",
        content: entry.content ?? "",
        task_id: entry.source_task_id ?? null,
        created_at: entry.created_at,
        attachments: entry.attachments,
      }));
  }, [timeline]);

  const { uploadWithToast } = useFileUpload(api);

  const handleSend = async (
    content: string,
    attachmentIds?: string[],
  ): Promise<boolean> => {
    try {
      const ok = await submitComment(content, attachmentIds);
      if (ok) {
        qc.invalidateQueries({ queryKey: ["issues", wsId, issueId, "active-tasks"] });
      }
      return ok;
    } catch {
      return false;
    }
  };

  const handleUploadFile = async (file: File) => {
    if (!issue) return null;
    return uploadWithToast(file, { issueId: issue.id });
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

      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-3xl border bg-background">
        <ChatMessageList
          messages={messages}
          pendingTask={pendingTask}
          availability={availability}
          hasOlderMessages={false}
          isFetchingOlderMessages={false}
          onLoadOlderMessages={() => {}}
        />
        {!messagesLoading && (
          <ChatInput
            onSend={handleSend}
            onUploadFile={handleUploadFile}
            isRunning={!!pendingTask?.task_id}
            disabled={issue.status === "cancelled"}
            agentName={activeAgent?.name}
          />
        )}
      </div>
    </div>
  );
}
