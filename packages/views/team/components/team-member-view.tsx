"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { ActorAvatar } from "../../common/actor-avatar";
import { ActorIssuesPanel } from "../../common/actor-issues-panel";
import { useT } from "../../i18n";
import { TEAM_TABS, type TeamTab } from "../selection";
import { TeamDetailHeader } from "./team-detail-header";
import { TeamDetailSkeleton } from "./team-agent-view";
import { TeamNotFound } from "./team-not-found";

export function TeamMemberView({ userId, tab }: { userId: string; tab: TeamTab }) {
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
      {tab === "work" && <ActorIssuesPanel actorType="member" actorId={member.user_id} />}
    </div>
  );
}
