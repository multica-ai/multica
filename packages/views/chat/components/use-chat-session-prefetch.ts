"use client";

import { useCallback, useEffect, useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { prefetchChatSession, sortChatSessions } from "@multica/core/chat/queries";
import { chatAppRequestsOptions } from "@multica/core/connected-apps";
import { useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import type { ChatSession } from "@multica/core/types";

/** Sessions warmed when idle and nothing is selected yet. */
export const CHAT_IDLE_PREFETCH_LIMIT = 2;

/**
 * Pick the adjacent sessions to warm around `activeSessionId` in priority order:
 *
 *  - When an active session is open: `next` first (the primary target of
 *    `advanceSelectionAfterArchive`), then `prev` (the fallback when archiving
 *    the last session, and an immediately adjacent click target), then `next+1`
 *    for consecutive archiving.
 *  - Works across both the active history list and the archived view (whichever
 *    list contains `activeSessionId`).
 *  - When no session is open: the top rows in history order.
 */
export function pickAdjacentChatSessions(
  sessions: readonly ChatSession[],
  activeSessionId: string | null,
): ChatSession[] {
  const history = sortChatSessions(sessions.filter((s) => s.status !== "archived"));
  const picked: ChatSession[] = [];
  const seen = new Set<string>();
  const add = (session: ChatSession | null | undefined) => {
    if (!session?.id || session.id === activeSessionId || seen.has(session.id)) return;
    seen.add(session.id);
    picked.push(session);
  };

  if (activeSessionId) {
    const historyIdx = history.findIndex((s) => s.id === activeSessionId);
    if (historyIdx >= 0) {
      add(history[historyIdx + 1]);
      add(history[historyIdx - 1]);
      add(history[historyIdx + 2]);
      return picked;
    }

    const archived = sortChatSessions(sessions.filter((s) => s.status === "archived"));
    const archivedIdx = archived.findIndex((s) => s.id === activeSessionId);
    if (archivedIdx >= 0) {
      add(archived[archivedIdx + 1]);
      add(archived[archivedIdx - 1]);
      add(archived[archivedIdx + 2]);
      return picked;
    }
  }

  for (const session of history) {
    if (picked.length >= CHAT_IDLE_PREFETCH_LIMIT) break;
    add(session);
  }
  return picked;
}

/**
 * Warms neighbouring chat sessions in the background while the Chat page is
 * active so archiving and auto-advancing (or clicking an adjacent thread) hits
 * a warm cache without flashing the skeleton or asynchronously popping in
 * bottom strips. Returns an on-demand `prefetchSession` callback for row hover
 * and focus.
 */
export function useChatSessionPrefetch({
  wsId,
  sessions,
  activeSessionId,
  isActive = true,
}: {
  wsId: string;
  sessions: readonly ChatSession[];
  activeSessionId: string | null;
  isActive?: boolean;
}) {
  const qc = useQueryClient();
  const appRequestsEnabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);

  const prefetchSession = useCallback(
    (sessionId: string) => {
      if (!wsId || !sessionId) return Promise.resolve();
      return Promise.all([
        prefetchChatSession(qc, sessionId),
        appRequestsEnabled
          ? qc.prefetchQuery(chatAppRequestsOptions(wsId, sessionId))
          : undefined,
      ])
        .then(() => undefined)
        .catch(() => {
          // A failed warm-up falls back cleanly to the session's own query on open.
        });
    },
    [qc, wsId, appRequestsEnabled],
  );

  const idleTargets = useMemo(
    () => (isActive ? pickAdjacentChatSessions(sessions, activeSessionId) : []),
    [isActive, sessions, activeSessionId],
  );
  // Effect identity follows the target session IDs, not the session object
  // references that realtime patches replace on every chat event.
  const idleKey = idleTargets.map((s) => s.id).join(",");

  useEffect(() => {
    if (!isActive || !wsId || !idleKey) return;
    let cancelled = false;
    const run = async () => {
      // Sequential: each session fans out across messages, pending task, and wakeups.
      for (const session of idleTargets) {
        if (cancelled) return;
        await prefetchSession(session.id);
      }
    };
    const timeout = activeSessionId ? 150 : 400;
    if (typeof window.requestIdleCallback === "function") {
      const id = window.requestIdleCallback(() => void run(), { timeout });
      return () => {
        cancelled = true;
        window.cancelIdleCallback(id);
      };
    }
    const timer = window.setTimeout(() => void run(), activeSessionId ? 100 : 300);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed by target session IDs
  }, [idleKey, wsId, isActive, appRequestsEnabled]);

  return prefetchSession;
}
