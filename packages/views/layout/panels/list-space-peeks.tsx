"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useChatStore } from "@multica/core/chat";
import { useSetChatSessionArchived } from "@multica/core/chat/mutations";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  filterInboxItems,
  inboxFiltersForPrioritySupport,
  inboxPriorityFilterSupport,
  useInboxFilters,
} from "@multica/core/inbox/filter-store";
import { useArchiveInbox } from "@multica/core/inbox/mutations";
import {
  deduplicateInboxItems,
  inboxListOptions,
  useInboxUnreadCount,
} from "@multica/core/inbox/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Agent, ChatSession, InboxItem } from "@multica/core/types";
import { agentListOptions } from "@multica/core/workspace/queries";
import { ChatThreadList } from "../../chat/components/chat-thread-list";
import { useT } from "../../i18n";
import { InboxList } from "../../inbox/components/inbox-list";
import { useIssueSurfaceActivity } from "../../issues/surface/activity";
import { useNavigation } from "../../navigation";
import { PanelCount, PanelHeader } from "../panel-nav";

const EMPTY_INBOX: InboxItem[] = [];
const EMPTY_SESSIONS: ChatSession[] = [];
const EMPTY_AGENTS: Agent[] = [];

/**
 * Inbox previewed from another space: the list the Inbox page shows, under
 * the same saved filters. Opening a row lands on it in the Inbox.
 */
export function InboxPeek() {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const { data: rawItems = EMPTY_INBOX } = useQuery(inboxListOptions(wsId));
  const filters = useInboxFilters(wsId);
  const { activityByIssueId } = useIssueSurfaceActivity();
  const unreadCount = useInboxUnreadCount(wsId);
  const archive = useArchiveInbox();

  const items = useMemo(() => {
    const effective = inboxFiltersForPrioritySupport(filters, inboxPriorityFilterSupport(rawItems));
    return filterInboxItems(deduplicateInboxItems(rawItems), effective, {
      activeIssueIds: effective.idleOnly ? new Set(activityByIssueId.keys()) : undefined,
    });
  }, [rawItems, filters, activityByIssueId]);

  const open = (params: Record<string, string>) =>
    push(`${p.inbox()}?${new URLSearchParams(params).toString()}`);

  return (
    <>
      <PanelHeader
        title={t(($) => $.page.title)}
        meta={<PanelCount value={unreadCount} className="text-muted-foreground" />}
      />
      <InboxList
        items={items}
        view="inbox"
        selectedKey=""
        scrollRestoration={false}
        onSelect={(item) => open({ issue: item.issue_id ?? item.id })}
        onAction={(id) => archive.mutate(id)}
      />
    </>
  );
}

/** Chat previewed from another space. Opening a thread lands on it in Chat. */
export function ChatPeek() {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const { data: sessions = EMPTY_SESSIONS } = useQuery(chatSessionsOptions(wsId));
  const { data: agents = EMPTY_AGENTS } = useQuery(agentListOptions(wsId));
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const setArchived = useSetChatSessionArchived();

  return (
    <>
      <PanelHeader title={t(($) => $.page.title)} />
      <div className="min-h-0 flex-1 overflow-y-auto px-2 py-1">
        <ChatThreadList
          sessions={sessions}
          agents={agents}
          activeSessionId={activeSessionId}
          onSelectSession={(session) => push(`${p.chat()}?session=${session.id}`)}
          onArchive={(session) => setArchived.mutate({ sessionId: session.id, archived: true })}
        />
      </div>
    </>
  );
}
