import { useCallback, useRef } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { chatKeys } from "./queries";

export function useAddToChatRun(sessionId: string | null, taskId: string | undefined) {
  const qc = useQueryClient();
  const request = useRef<{ key: string; id: string } | null>(null);
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
    if (!sessionId || !taskId) throw new Error("run unavailable");
    const key = JSON.stringify([sessionId, taskId, content]);
    if (request.current?.key !== key) request.current = { key, id: crypto.randomUUID() };
    const current = request.current;
    await mutateAsync({ session: sessionId, task: taskId, content, id: current.id });
    if (request.current === current) request.current = null;
  }, [sessionId, taskId, mutateAsync]);
  return taskId && sessionId ? send : undefined;
}
