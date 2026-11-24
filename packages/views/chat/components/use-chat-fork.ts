"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import { runtimeListOptions } from "@multica/core/runtimes";
import type { Agent, ChatSession } from "@multica/core/types";
import { toast } from "sonner";
import { useT } from "../../i18n";

export function useChatFork(
  workspaceId: string,
  session: ChatSession | null,
  agent: Agent | null,
  enabled: boolean,
  onOpen: (chatId: string) => void,
) {
  const { t } = useT("chat");
  const queryClient = useQueryClient();
  const [pending, setPending] = useState<{
    sessionId: string;
    taskId: string;
    workspaceId: string;
    toastId: ReturnType<typeof toast.info>;
  } | null>(null);
  const { data: runtimes } = useQuery({
    ...runtimeListOptions(workspaceId),
    enabled: enabled && !!workspaceId && !!agent?.runtime_id && session?.status === "active",
  });
  const runtime = runtimes?.find((candidate) => candidate.id === agent?.runtime_id);
  const capabilities = runtime?.metadata.capabilities;
  const canFork = enabled && session?.status === "active" && runtime?.provider === "codex" &&
    runtime.runtime_mode === "local" && Array.isArray(capabilities) && capabilities.includes("codex-chat-fork-v1");

  const create = useMutation({
    mutationFn: async (sessionId: string) => {
      const fork = await api.createChatFork(sessionId);
      if (!fork || fork.status !== "queued") throw new Error("invalid chat fork acceptance");
      return fork;
    },
    onSuccess: ({ task_id }, sessionId) => {
      const toastId = toast.info(t(($) => $.header.fork_pending));
      setPending({ sessionId, taskId: task_id, workspaceId, toastId });
    },
    onError: () => toast.error(t(($) => $.header.fork_failed)),
  });

  const current = pending?.workspaceId === workspaceId ? pending : null;
  const status = useQuery({
    queryKey: [...chatKeys.all(workspaceId), "fork", current?.sessionId, current?.taskId],
    enabled: enabled && current !== null,
    queryFn: async () => {
      if (!current) throw new Error("missing chat fork request");
      const fork = await api.getChatFork(current.sessionId, current.taskId);
      if (!fork || fork.task_id !== current.taskId) throw new Error("invalid chat fork status");
      return fork;
    },
    refetchInterval: enabled && current !== null ? 1000 : false,
  });

  useEffect(() => {
    if (!current || !enabled) return;
    const fork = status.data;
    if (status.isError || fork?.status === "failed" || fork?.status === "cancelled" || fork?.status === "unknown") {
      setPending(null);
      toast.error(t(($) => $.header.fork_failed), { id: current.toastId });
    } else if (fork?.status === "completed") {
      setPending(null);
      if (!fork.chat_id) {
        toast.error(t(($) => $.header.fork_failed), { id: current.toastId });
        return;
      }
      const childId = fork.chat_id;
      void queryClient.invalidateQueries({ queryKey: chatKeys.sessions(workspaceId) });
      toast.success(t(($) => $.header.fork_ready), {
        id: current.toastId,
        action: {
          label: t(($) => $.header.open_fork),
          onClick: () => onOpen(childId),
        },
      });
    }
  }, [current, enabled, onOpen, queryClient, status.data, status.isError, t, workspaceId]);

  return {
    canFork,
    pending: create.isPending || current !== null,
    start: () => {
      if (canFork && session && !create.isPending && !current) create.mutate(session.id);
    },
  };
}
