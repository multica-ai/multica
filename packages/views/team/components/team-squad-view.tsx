"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Crown, Link2, Plus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { squadListOptions, squadMemberStatusOptions } from "@multica/core/workspace/queries";
import type { Squad, SquadMemberStatus } from "@multica/core/types";
import { AppLink, useNavigation } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { ActorIssuesPanel } from "../../common/actor-issues-panel";
import { SquadDetailPage } from "../../squads/components/squad-detail-page";
import { useT } from "../../i18n";
import { TEAM_TABS, teamHref, type TeamTab } from "../selection";
import { TeamDetailHeader } from "./team-detail-header";
import { TeamDetailSkeleton } from "./team-agent-view";
import { TeamNotFound } from "./team-not-found";
import { TeamDot, TeamPanel, TeamPanelRow, TeamPanelSection, TeamPill, type TeamTone, useTeamCopy } from "./team-ui";

const EMPTY_STATUS: SquadMemberStatus[] = [];

/**
 * A squad has no conversation of its own — talking to a squad is what a
 * channel is for. It takes issues (its leader routes them) and has setup.
 */
export function TeamSquadView({ squadId, tab }: { squadId: string; tab: TeamTab }) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { replace } = useNavigation();
  const { getActorName } = useActorName();
  const { copyLink } = useTeamCopy();
  const { data: squads = [], isLoading } = useQuery(squadListOptions(wsId));
  const squad = squads.find((s) => s.id === squadId) ?? null;
  const { data: statusData } = useQuery({
    ...squadMemberStatusOptions(wsId, squadId),
    enabled: !!squad,
  });
  const statuses = statusData?.members ?? EMPTY_STATUS;
  const [panelOpen, setPanelOpen] = useState(true);

  if (!squad) return isLoading ? <TeamDetailSkeleton /> : <TeamNotFound />;

  const working = statuses.filter((m) => m.status === "working").length;
  const leaderName = getActorName("agent", squad.leader_id);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <TeamDetailHeader
        kind="squad"
        id={squad.id}
        tabs={TEAM_TABS.squad}
        activeTab={tab}
        icon={<ActorAvatar actorType="squad" actorId={squad.id} size="md" />}
        name={squad.name}
        menu={
          <DropdownMenuItem onClick={() => copyLink(teamHref(wsPaths.team(), { kind: "squad", id: squad.id }))}>
            <Link2 className="size-3.5" />
            {t(($) => $.detail.copy_link)}
          </DropdownMenuItem>
        }
        status={
          squad.archived_at ? (
            <TeamPill tone="muted" dot={false}>{t(($) => $.status.archived)}</TeamPill>
          ) : working > 0 ? (
            <TeamPill tone="brand">{t(($) => $.squad.working, { count: working })}</TeamPill>
          ) : (
            <TeamPill tone="muted">{t(($) => $.status.idle)}</TeamPill>
          )
        }
        actions={
          !squad.archived_at && (
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                useModalStore.getState().open("create-issue", {
                  assignee_type: "squad",
                  assignee_id: squad.id,
                })
              }
            >
              <Plus className="size-3.5" />
              {t(($) => $.squad.assign_issue)}
            </Button>
          )
        }
        panelOpen={tab === "work" ? panelOpen : undefined}
        onTogglePanel={tab === "work" ? () => setPanelOpen((v) => !v) : undefined}
      />
      {tab === "work" && (
        <div className="flex min-h-0 flex-1">
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            <p className="shrink-0 border-b px-5 py-2 text-caption text-muted-foreground">
              {t(($) => $.squad.work_note, { name: leaderName })}
            </p>
            <ActorIssuesPanel actorType="squad" actorId={squad.id} />
          </div>
          {panelOpen && <SquadPanel squad={squad} statuses={statuses} leaderName={leaderName} />}
        </div>
      )}
      {tab === "setup" && (
        <SquadDetailPage
          squadId={squad.id}
          embedded
          onArchived={() => replace(teamHref(wsPaths.team(), { kind: "none" }))}
        />
      )}
    </div>
  );
}

const STATUS_TONE: Record<string, TeamTone> = { working: "brand", unstable: "warning" };

function SquadPanel({
  squad,
  statuses,
  leaderName,
}: {
  squad: Squad;
  statuses: SquadMemberStatus[];
  leaderName: string;
}) {
  const { t } = useT("team");
  const wsPaths = useWorkspacePaths();
  const { getActorName } = useActorName();
  const ordered = [...statuses].sort((a, b) => {
    const rank = (m: SquadMemberStatus) => (m.member_id === squad.leader_id ? 0 : m.member_type === "agent" ? 1 : 2);
    return rank(a) - rank(b);
  });

  return (
    <TeamPanel>
      <TeamPanelSection title={t(($) => $.squad.members)} trailing={ordered.length}>
        <ul className="-mx-2 flex flex-col gap-px">
          {ordered.map((m) => {
            const type = m.member_type === "agent" ? "agent" : "member";
            const issue = m.active_issues[0];
            return (
              <li key={`${m.member_type}:${m.member_id}`}>
                <AppLink
                  href={teamHref(wsPaths.team(), { kind: type, id: m.member_id })}
                  className="flex h-8 items-center gap-2 rounded-md px-2 text-caption hover:bg-surface-hover"
                >
                  <ActorAvatar actorType={type} actorId={m.member_id} size="sm" />
                  <span className="min-w-0 truncate">{getActorName(type, m.member_id)}</span>
                  {m.member_id === squad.leader_id && (
                    <Crown className="size-3 shrink-0 text-muted-foreground" aria-label={t(($) => $.panel.leader)} />
                  )}
                  <span className="flex-1" />
                  {m.status && (
                    <span className="flex shrink-0 items-center gap-1.5 text-micro text-muted-foreground">
                      <TeamDot tone={STATUS_TONE[m.status] ?? "muted"} />
                      {m.status === "working" && issue ? issue.identifier : t(($) => $.squad.member_status[m.status!])}
                    </span>
                  )}
                </AppLink>
              </li>
            );
          })}
        </ul>
      </TeamPanelSection>
      <TeamPanelSection title={t(($) => $.squad.how_it_works)}>
        <p className="text-caption text-muted-foreground">{t(($) => $.squad.how_it_works_body, { name: leaderName })}</p>
      </TeamPanelSection>
      <TeamPanelSection title={t(($) => $.panel.properties)}>
        <dl className="flex flex-col gap-1">
          <TeamPanelRow label={t(($) => $.squad.leader)}>
            <ActorAvatar actorType="agent" actorId={squad.leader_id} size="xs" />
            <span className="truncate">{leaderName}</span>
          </TeamPanelRow>
          <TeamPanelRow label={t(($) => $.squad.created_by)}>
            <ActorAvatar actorType="member" actorId={squad.creator_id} size="xs" />
            <span className="truncate">{getActorName("member", squad.creator_id)}</span>
          </TeamPanelRow>
        </dl>
      </TeamPanelSection>
    </TeamPanel>
  );
}
