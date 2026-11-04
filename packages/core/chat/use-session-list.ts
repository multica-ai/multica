import { useMemo } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { ApiError } from "../api";
import { chatSessionPagesOptions, chatSessionOptions, sortChatSessions } from "./queries";
import type { ChatSession } from "../types";

export function useChatSessionList(wsId: string, status: "active" | "archived" = "active", enabled = true) {
  const query = useInfiniteQuery({ ...chatSessionPagesOptions(wsId, status), enabled });
  const data = useMemo(() => {
    const seen = new Set<string>();
    return sortChatSessions((query.data?.pages ?? []).flatMap((page) => page.sessions).filter((session) => {
      if (seen.has(session.id)) return false;
      seen.add(session.id);
      return session.status === status;
    }));
  }, [query.data, status]);
  return { ...query, data };
}

export type ChatSessionList = ReturnType<typeof useChatSessionList>;

export function useCurrentChatSession(wsId: string, id: string | null, sessions: ChatSession[]) {
  const query = useQuery(chatSessionOptions(wsId, id ?? ""));
  // A partial list cannot prove that a deep link or restored selection is gone.
  const unavailable = query.error instanceof ApiError && [403, 404].includes(query.error.status);
  return { ...query, data: id && !unavailable ? query.data ?? sessions.find((s) => s.id === id) ?? null : null, unavailable };
}
