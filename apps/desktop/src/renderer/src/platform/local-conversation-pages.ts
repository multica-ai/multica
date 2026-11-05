import { useCallback, useEffect, useMemo, useState } from "react";
import type { LocalFolderProject } from "@multica/core/local-conversation/protocol";
import type { LocalV2Scope, LocalV2Transport } from "@multica/core/local-conversation/v2";
import { groupLocalConversations, type LocalConversationProjectGroup } from "./local-conversation-projects";
import { LocalConversationListModel } from "./local-conversation-list-model";
import { conversationService } from "./conversation-services";
import type { DirectLocalV2Transport } from "./local-v2-transport";

const PAGE = 8;

export interface ConversationPageGroup extends LocalConversationProjectGroup {
  /** Set while more conversations of the group are hidden. */
  nextCursor: string | null;
  pageCount: number;
}

/**
 * The local conversation list, grouped by folder project. The complete list
 * comes from the scope's v2 summaries and stays current with them; groups
 * show eight conversations per page, pinned first, then most recent. Call this
 * from an observer component: it reads the list model.
 */
export function useLocalConversationPages(transport: LocalV2Transport & Pick<DirectLocalV2Transport, "availability" | "onAvailability">, scope: LocalV2Scope, projects: LocalFolderProject[], hidden: string[], archived: boolean, search: string, enabled: boolean) {
  const client = conversationService(transport, scope);
  const model = useMemo(() => new LocalConversationListModel(client.list), [client]);
  const [status, setStatus] = useState<{ pending: boolean; error: Error | null; fetching: boolean }>({ pending: true, error: null, fetching: false });
  const [pages, setPages] = useState(new Map<string | null, number>());
  const load = useCallback(() => {
    setStatus((current) => ({ ...current, fetching: true }));
    client.loadSummaries().then(() => setStatus({ pending: false, error: null, fetching: false }), (error: unknown) => setStatus({ pending: false, error: error instanceof Error ? error : new Error(String(error)), fetching: false }));
  }, [client]);
  useEffect(() => {
    if (!enabled) return;
    load();
    let available = transport.availability()?.state === "connected";
    const stopAvailability = transport.onAvailability((value) => {
      const next = value.state === "connected";
      if (next && !available) load();
      available = next;
    });
    const stopConnection = transport.onConnection((value, connected) => {
      if (connected && value.userId === scope.userId && value.workspaceId === scope.workspaceId) load();
    });
    return () => { stopAvailability(); stopConnection(); };
  }, [enabled, load, transport, scope.userId, scope.workspaceId]);
  // Sides open beside their source; a conversation joins the list with its first message.
  const sessions = model.sessions;
  const groups: ConversationPageGroup[] = groupLocalConversations(sessions, projects.filter((project) => !hidden.includes(project.id)), archived, search).map((group) => {
    const pageCount = pages.get(group.id) ?? 1;
    const visible = group.sessions.slice(0, pageCount * PAGE);
    return { ...group, sessions: visible, pageCount, nextCursor: visible.length < group.sessions.length ? String(pageCount) : null };
  }).filter((group) => group.sessions.length || (!archived && !search.trim() && (group.id !== null || projects.length > 0)));
  const loadMore = async (group: ConversationPageGroup) => {
    setPages((current) => new Map(current).set(group.id, group.pageCount + 1));
  };
  return { model, data: sessions, groups, loading: new Set<string | null>(), loadMore, isPending: enabled && status.pending, isError: !!status.error, error: status.error, isFetching: status.fetching, refetch: load };
}
