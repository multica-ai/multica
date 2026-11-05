import { useCallback } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useChatRunDraftStore, forgetChatRunDraft } from "./run-draft-store";
import { getCurrentSlug } from "../platform/workspace-storage";
import { chatKeys } from "./queries";

export function useAddToChatRun(sessionId: string | null, taskId: string | undefined) {
  const qc = useQueryClient();
  const hasRetry = useChatRunDraftStore((s) => s.draft.attempts.some((a) => a.session === sessionId));
  const { mutateAsync } = useMutation({
    mutationFn: ({ session, task, content, id }: { session: string; task: string; content: string; id: string }) =>
      api.addToChatRun(session, task, content, id),
    retry: false,
    onSettled: (_data, _error, { session }) => {
      qc.invalidateQueries({ queryKey: chatKeys.messages(session) });
      qc.invalidateQueries({ queryKey: chatKeys.messagesPage(session) });
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(session) });
    },
  });
  const send = useCallback(async (content: string) => {
    if (!sessionId) throw new Error("run unavailable");
    const workspaceSlug = getCurrentSlug();
    const store = useChatRunDraftStore.getState();
    let current = store.draft.attempts.find((a) => a.session === sessionId && a.content === content);
    if (!current) {
      if (!taskId) throw new Error("run unavailable");
      current = { session: sessionId, task: taskId, content, id: crypto.randomUUID() };
      store.setDraft({ attempts: [...store.draft.attempts, current] });
    }
    await mutateAsync(current);
    forgetChatRunDraft(current.id, workspaceSlug);
  }, [sessionId, taskId, mutateAsync]);
  return sessionId && (taskId || hasRetry) ? send : undefined;
}
