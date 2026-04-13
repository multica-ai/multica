import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { chatKeys } from "./queries";
import type { ChatSession } from "../types";

export function useCreateChatSession() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (data: { agent_id: string; title?: string }) =>
      api.createChatSession(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
      qc.invalidateQueries({ queryKey: chatKeys.allSessions(wsId) });
    },
  });
}

export function useArchiveChatSession() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (sessionId: string) => api.archiveChatSession(sessionId),
    onMutate: async (sessionId) => {
      await qc.cancelQueries({ queryKey: chatKeys.sessions(wsId) });
      await qc.cancelQueries({ queryKey: chatKeys.allSessions(wsId) });

      const prevSessions = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));
      const prevAll = qc.getQueryData<ChatSession[]>(chatKeys.allSessions(wsId));

      // Optimistic: remove from active, mark as archived in allSessions
      qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), (old) =>
        old ? old.filter((s) => s.id !== sessionId) : old,
      );
      qc.setQueryData<ChatSession[]>(chatKeys.allSessions(wsId), (old) =>
        old?.map((s) =>
          s.id === sessionId ? { ...s, status: "archived" as const } : s,
        ),
      );

      return { prevSessions, prevAll };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevSessions) qc.setQueryData(chatKeys.sessions(wsId), ctx.prevSessions);
      if (ctx?.prevAll) qc.setQueryData(chatKeys.allSessions(wsId), ctx.prevAll);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
      qc.invalidateQueries({ queryKey: chatKeys.allSessions(wsId) });
    },
  });
}
