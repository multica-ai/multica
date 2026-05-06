import type { QueryClient } from "@tanstack/react-query";
import { chatKeys } from "../chat/queries";

export function invalidateChatQueriesOnReconnect(
  qc: Pick<QueryClient, "invalidateQueries">,
  wsId: string | null | undefined,
) {
  if (wsId) {
    qc.invalidateQueries({ queryKey: chatKeys.all(wsId) });
  }

  // Session messages, per-session pending tasks, and streamed task messages
  // are keyed outside the workspace-scoped chat root, so reconnect recovery
  // must invalidate those active caches explicitly.
  qc.invalidateQueries({ queryKey: ["chat", "messages"] });
  qc.invalidateQueries({ queryKey: ["chat", "pending-task"] });
  qc.invalidateQueries({ queryKey: ["task-messages"] });
}
