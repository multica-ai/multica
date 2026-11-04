import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import type { LocalFolderProject } from "@multica/core/local-conversation/protocol";
import type { LocalV2Scope, LocalV2Transport } from "@multica/core/local-conversation/v2";
import { groupLocalConversations, sessionEntry, type LocalConversationProjectGroup, type LocalSessionEntry } from "./local-conversation-projects";
import { conversationStore } from "./conversation-store";

const PAGE = 8;

export interface ConversationPageGroup extends LocalConversationProjectGroup {
  /** Set while more conversations of the group are hidden. */
  nextCursor: string | null;
  pageCount: number;
}

/**
 * The local conversation list, grouped by folder project. The complete list
 * comes from the scope's v2 summaries and stays current with them; groups
 * show eight conversations per page, pinned first, then most recent.
 */
export function useLocalConversationPages(transport: LocalV2Transport, scope: LocalV2Scope, projects: LocalFolderProject[], hidden: string[], archived: boolean, search: string, enabled: boolean) {
  const client = conversationStore(transport, scope);
  const [status, setStatus] = useState<{ pending: boolean; error: Error | null; fetching: boolean }>({ pending: true, error: null, fetching: false });
  const [pages, setPages] = useState(new Map<string | null, number>());
  const version = useSyncExternalStore(useCallback((listener) => client.onSummaries(listener), [client]), () => client.summaries());
  const load = useCallback(() => {
    setStatus((current) => ({ ...current, fetching: true }));
    client.loadSummaries().then(() => setStatus({ pending: false, error: null, fetching: false }), (error: unknown) => setStatus({ pending: false, error: error instanceof Error ? error : new Error(String(error)), fetching: false }));
  }, [client]);
  useEffect(() => {
    if (!enabled) return;
    load();
    return transport.onConnection((value, connected) => {
      if (connected && value.userId === scope.userId && value.workspaceId === scope.workspaceId) load();
    });
  }, [enabled, load, transport, scope.userId, scope.workspaceId]);
  // Sides open beside their source; a conversation joins the list with its first message.
  const sessions: LocalSessionEntry[] = version.filter((summary) => !summary.side_source_id && summary.turn_count > 0).map(sessionEntry)
    .sort((a, b) => Number(b.pinned) - Number(a.pinned) || b.updatedAt.localeCompare(a.updatedAt));
  const groups: ConversationPageGroup[] = groupLocalConversations(sessions, projects.filter((project) => !hidden.includes(project.id)), archived, search).map((group) => {
    const pageCount = pages.get(group.id) ?? 1;
    const visible = group.sessions.slice(0, pageCount * PAGE);
    return { ...group, sessions: visible, pageCount, nextCursor: visible.length < group.sessions.length ? String(pageCount) : null };
  }).filter((group) => group.sessions.length || (!archived && !search.trim() && (group.id !== null || projects.length > 0)));
  const loadMore = async (group: ConversationPageGroup) => {
    setPages((current) => new Map(current).set(group.id, group.pageCount + 1));
  };
  return { data: sessions, groups, loading: new Set<string | null>(), loadMore, isPending: enabled && status.pending, isError: !!status.error, error: status.error, isFetching: status.fetching, refetch: load };
}
