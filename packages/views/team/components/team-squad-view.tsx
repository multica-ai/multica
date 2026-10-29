"use client";

import { useQuery } from "@tanstack/react-query";
import { Plus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { squadListOptions } from "@multica/core/workspace/queries";
import { useNavigation } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { ActorIssuesPanel } from "../../common/actor-issues-panel";
import { SquadDetailPage } from "../../squads/components/squad-detail-page";
import { useT } from "../../i18n";
import { TEAM_TABS, teamHref, type TeamTab } from "../selection";
import { TeamDetailHeader } from "./team-detail-header";
import { TeamDetailSkeleton } from "./team-agent-view";
import { TeamNotFound } from "./team-not-found";

/**
 * A squad has no conversation of its own — talking to a squad is what a
 * channel is for. It takes issues (its leader routes them) and has setup.
 */
export function TeamSquadView({ squadId, tab }: { squadId: string; tab: TeamTab }) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { replace } = useNavigation();
  const { data: squads = [], isLoading } = useQuery(squadListOptions(wsId));
  const squad = squads.find((s) => s.id === squadId) ?? null;

  if (!squad) return isLoading ? <TeamDetailSkeleton /> : <TeamNotFound />;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <TeamDetailHeader
        kind="squad"
        id={squad.id}
        tabs={TEAM_TABS.squad}
        activeTab={tab}
        identity={
          <>
            <ActorAvatar actorType="squad" actorId={squad.id} size="md" />
            <h2 className="truncate text-body font-medium">{squad.name}</h2>
          </>
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
      />
      {tab === "work" && <ActorIssuesPanel actorType="squad" actorId={squad.id} />}
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
