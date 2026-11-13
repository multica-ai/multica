// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { chatKeys } from "./queries";
import { refetchChatSessionsAfterCommit } from "./session-list";
import { applyChatSessionUpdatedToCache } from "../realtime/use-realtime-sync";
import type { ChatSession } from "../types";

describe("refetchChatSessionsAfterCommit", () => {
  it.each([
    { name: "cold cache", hasCache: false },
    { name: "warm cache", hasCache: true },
  ])("keeps a title committed while a list request is in flight ($name)", async ({ hasCache }) => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const key = chatKeys.sessions("ws-1");
    if (hasCache) qc.setQueryData(key, []);
    const session = { id: "session-1", title: "Release checklist" } as ChatSession;
    let finishFirst!: (rows: ChatSession[]) => void;
    const first = new Promise<ChatSession[]>((resolve) => {
      finishFirst = resolve;
    });
    let fetches = 0;
    const observer = new QueryObserver(qc, {
      queryKey: key,
      staleTime: Infinity,
      queryFn: () => (++fetches === 1 ? first : Promise.resolve([session])),
    });
    const unsubscribe = observer.subscribe(() => {});
    try {
      // The list GET read the newly created, still-untitled row; the send
      // commits the title and its realtime event lands before that GET returns.
      const inFlight = observer.refetch();
      applyChatSessionUpdatedToCache(qc, "ws-1", {
        chat_session_id: session.id,
        title: session.title,
      });
      const refresh = refetchChatSessionsAfterCommit(qc, key);
      finishFirst([{ ...session, title: "" }]);
      await Promise.all([inFlight, refresh]);

      expect(qc.getQueryData<ChatSession[]>(key)?.[0]?.title).toBe(session.title);
    } finally {
      unsubscribe();
      qc.clear();
    }
  });
});
