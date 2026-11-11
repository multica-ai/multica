import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import type { ChatSession, ChatSessionsPage, ChatSessionSummary } from "../types";
import { chatKeys } from "./queries";

export function patchChatSessionPages(qc: QueryClient, wsId: string, id: string, patch: Partial<ChatSession>) {
  const pages = qc.getQueriesData<InfiniteData<ChatSessionsPage>>({ queryKey: chatKeys.sessionPages(wsId) });
  const detailKey = chatKeys.session(wsId, id);
  const detail = qc.getQueryData<ChatSession>(detailKey);
  const summaryKey = chatKeys.sessionSummary(wsId);
  const summary = qc.getQueryData<ChatSessionSummary>(summaryKey);
  const previous = detail ?? pages.flatMap(([, data]) => data?.pages.flatMap((page) => page.sessions) ?? []).find((session) => session.id === id);

  for (const [key, data] of pages) {
    if (!data) continue;
    qc.setQueryData(key, {
      ...data,
      pages: data.pages.map((page) => ({
        ...page,
        sessions: page.sessions.map((session) => session.id === id ? { ...session, ...patch } : session),
      })),
    });
  }
  if (detail) qc.setQueryData(detailKey, { ...detail, ...patch });
  if (summary) {
    const unread = { ...summary.unread_by_session };
    if (patch.status === "archived" || patch.has_unread === false) delete unread[id];
    const statusChanged = previous && patch.status && patch.status !== previous.status;
    const delta = statusChanged ? patch.status === "archived" ? -1 : 1 : 0;
    qc.setQueryData(summaryKey, {
      ...summary,
      active_count: Math.max(0, summary.active_count + delta),
      archived_count: Math.max(0, summary.archived_count - delta),
      unread_by_session: unread,
    });
  }
  return () => {
    for (const [key, data] of pages) if (data) qc.setQueryData(key, data);
    if (detail) qc.setQueryData(detailKey, detail);
    if (summary) qc.setQueryData(summaryKey, summary);
  };
}
