/**
 * Chat refresh fallback for a degraded realtime connection.
 *
 * The chat screen's message/pending-task caches use `staleTime: Infinity`
 * and are normally refreshed only by `useChatSessionRealtime`. That is
 * correct while the workspace socket is authenticated, but a socket that
 * never connects (proxy blocks the upgrade, Cloudflare Access intercepts
 * it, flaky cellular) leaves the cache frozen until the screen remounts.
 *
 * This hook keeps an actively viewed conversation correct without a
 * socket:
 *   - refetch immediately whenever the screen gains focus, the app returns
 *     to the foreground, or socket health changes;
 *   - while focused + foregrounded + disconnected, poll messages, the
 *     pending task, and the in-flight task transcript every 4s;
 *   - stop polling when the screen loses focus or the app backgrounds.
 *
 * Reconnect refreshes stay owned by `useChatSessionRealtime`'s
 * `onReconnect` handler; this hook only adds a one-shot refetch when the
 * connected flag flips so an initial connect also catches up.
 */
import { useEffect, useRef, useState } from "react";
import { AppState, type AppStateStatus } from "react-native";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { chatKeys } from "@/data/queries/chat";
import { useWSConnected } from "./realtime-provider";

export const CHAT_FALLBACK_POLL_INTERVAL_MS = 4_000;

/** Refetch the caches a visible chat conversation reads. Partial keys are
 *  intentional: each invalidate targets exactly one cache entry. */
export function refreshChatSessionCaches(
  qc: QueryClient,
  sessionId: string,
  pendingTaskId?: string | null,
) {
  void qc.invalidateQueries({
    queryKey: chatKeys.messages(sessionId),
    refetchType: "active",
  });
  void qc.invalidateQueries({
    queryKey: chatKeys.pendingTask(sessionId),
    refetchType: "active",
  });
  if (pendingTaskId) {
    void qc.invalidateQueries({
      queryKey: chatKeys.taskMessages(pendingTaskId),
      refetchType: "active",
    });
  }
}

function isForeground(status: AppStateStatus): boolean {
  return status !== "background" && status !== "inactive";
}

export function useChatRefreshFallback({
  sessionId,
  focused,
  pendingTaskId,
}: {
  sessionId: string | null;
  focused: boolean;
  /** Active task whose live transcript should follow the polling cadence. */
  pendingTaskId?: string | null;
}) {
  const qc = useQueryClient();
  const connected = useWSConnected();
  const [appActive, setAppActive] = useState(() =>
    isForeground(AppState.currentState),
  );
  const pendingTaskIdRef = useRef(pendingTaskId);
  pendingTaskIdRef.current = pendingTaskId;

  useEffect(() => {
    const sub = AppState.addEventListener("change", (status) => {
      setAppActive(isForeground(status));
    });
    return () => sub.remove();
  }, []);

  useEffect(() => {
    if (!focused || !sessionId) return;

    // One refresh per focus/session/connectivity/foreground transition.
    // `invalidateQueries` on an inactive query is a no-op, so a background
    // session cannot be refreshed by a stale timer.
    refreshChatSessionCaches(qc, sessionId, pendingTaskIdRef.current);

    if (!appActive || connected) return;

    const timer = setInterval(() => {
      refreshChatSessionCaches(qc, sessionId, pendingTaskIdRef.current);
    }, CHAT_FALLBACK_POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [focused, sessionId, appActive, connected, qc]);
}
