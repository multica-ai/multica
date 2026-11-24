"use client";

import { useQuery } from "@tanstack/react-query";
import { useChatStore } from "@multica/core/chat";
import { useSetChatSessionArchived } from "@multica/core/chat/mutations";
import { useChatSessionList } from "@multica/core/chat/use-session-list";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Agent } from "@multica/core/types";
import { agentListOptions } from "@multica/core/workspace/queries";
import { ChatThreadList } from "../../chat/components/chat-thread-list";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { PanelHeader } from "../panel-nav";

const EMPTY_AGENTS: Agent[] = [];

/** Chat previewed from another space. Opening a thread lands on it in Chat. */
export function ChatPeek() {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const sessionList = useChatSessionList(wsId);
  const { data: sessions } = sessionList;
  const { data: agents = EMPTY_AGENTS } = useQuery(agentListOptions(wsId));
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const setArchived = useSetChatSessionArchived();

  return (
    <>
      <PanelHeader title={t(($) => $.page.title)} />
      <div className="min-h-0 flex-1 overflow-y-auto px-2 py-1">
        <ChatThreadList
          sessions={sessions}
          sessionList={sessionList}
          agents={agents}
          activeSessionId={activeSessionId}
          onSelectSession={(session) => push(`${p.chat()}?session=${session.id}`)}
          onArchive={(session) => setArchived.mutate({ sessionId: session.id, archived: true })}
        />
      </div>
    </>
  );
}
