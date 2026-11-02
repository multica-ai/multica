import { useQueryClient } from "@tanstack/react-query";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import { appendTaskMessage, refreshTaskMessages } from "./chat-ws-updaters";

/** Own the selected run's stream for the lifetime of its detail screen. */
export function useRunTranscriptRealtime(issueId: string, taskId: string | undefined) {
  const qc = useQueryClient();

  useWSSubscriptions((ws, wsId) => {
    if (!issueId || !taskId) return;

    const isMine = (payload: { issue_id?: string; task_id: string }) =>
      payload.issue_id === issueId && payload.task_id === taskId;
    const refresh = () => {
      void refreshTaskMessages(qc, taskId);
      void qc.invalidateQueries({ queryKey: issueKeys.tasks(wsId, issueId) });
    };
    const onTerminal = (payload: { issue_id?: string; task_id: string }) => {
      if (isMine(payload)) refresh();
    };

    return [
      ws.on("task:message", (payload) => {
        if (isMine(payload)) appendTaskMessage(qc, payload);
      }),
      ws.on("task:completed", onTerminal),
      ws.on("task:failed", onTerminal),
      // Cancellation precedes the final flush. Continue accepting messages
      // afterward, and backfill again on the daemon's cancellation ack.
      ws.on("task:cancelled", onTerminal),
      ws.onReconnect(refresh),
    ];
  }, [issueId, taskId, qc]);
}
