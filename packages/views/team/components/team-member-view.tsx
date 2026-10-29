"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  conversationListOptions,
  findDirectConversation,
  useOpenConversationThread,
  useOpenDirectConversation,
} from "@multica/core/conversations";
import { ActorAvatar } from "../../common/actor-avatar";
import { ActorIssuesPanel } from "../../common/actor-issues-panel";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { TEAM_TABS, teamHref, type TeamTab } from "../selection";
import { TeamDetailHeader } from "./team-detail-header";
import { TeamDetailSkeleton } from "./team-agent-view";
import { TeamNotFound } from "./team-not-found";
import { TeamConversation } from "./team-conversation";
import { TeamConversationThread } from "./team-channel-view";

export function TeamMemberView({
  userId,
  tab,
  threadId,
}: {
  userId: string;
  tab: TeamTab;
  threadId: string | null;
}) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const { data: members = [], isLoading } = useQuery(memberListOptions(wsId));
  const member = members.find((m) => m.user_id === userId) ?? null;

  if (!member) return isLoading ? <TeamDetailSkeleton /> : <TeamNotFound />;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <TeamDetailHeader
        kind="member"
        id={member.user_id}
        tabs={TEAM_TABS.member}
        activeTab={tab}
        identity={
          <>
            <ActorAvatar actorType="member" actorId={member.user_id} size="md" />
            <h2 className="truncate text-body font-medium">{member.name}</h2>
            <span className="truncate text-caption text-muted-foreground">
              {t(($) => $.member.role[member.role])}
            </span>
          </>
        }
      />
      {tab === "chat" && <TeamMemberChat userId={member.user_id} name={member.name} threadId={threadId} />}
      {tab === "work" && <ActorIssuesPanel actorType="member" actorId={member.user_id} />}
    </div>
  );
}

/**
 * The viewer's direct conversation with a member — or their notes to self on
 * their own row. It is created by the first message, so opening a member
 * never leaves an empty conversation behind.
 */
function TeamMemberChat({ userId, name, threadId }: { userId: string; name: string; threadId: string | null }) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { push } = useNavigation();
  const viewerId = useAuthStore((s) => s.user?.id ?? "");
  const { data: conversations = [], isLoading } = useQuery(conversationListOptions(wsId));
  const openDirect = useOpenDirectConversation();
  const openThread = useOpenConversationThread();
  const direct = findDirectConversation(conversations, viewerId, userId);
  const self = viewerId === userId;
  const memberHref = (thread?: string) => teamHref(wsPaths.team(), { kind: "member", id: userId, threadId: thread });

  if (isLoading) return <TeamDetailSkeleton />;
  if (threadId) return <TeamConversationThread key={threadId} threadId={threadId} backHref={memberHref()} />;

  return (
    <TeamConversation
      key={direct?.id ?? "new"}
      conversationId={direct?.id ?? null}
      ensureConversation={async () => (await openDirect.mutateAsync(userId)).id}
      placeholder={self ? t(($) => $.member.notes_placeholder) : t(($) => $.member.dm_placeholder, { name })}
      empty={self ? t(($) => $.member.notes_empty) : t(($) => $.member.dm_empty, { name })}
      threadHref={(id) => memberHref(id)}
      onReplyInThread={(message) =>
        openThread.mutate(
          { conversationId: message.conversation_id, rootMessageId: message.id },
          {
            onSuccess: (thread) => push(memberHref(thread.id)),
            onError: () => toast.error(t(($) => $.chat.thread.open_failed)),
          },
        )
      }
    />
  );
}
