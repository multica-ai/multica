"use client";

import { ListTodo } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { activeTeamListOptions } from "@multica/core/teams/queries";
import type { Team } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { IssueSurface } from "../../issues/surface/issue-surface";
import { IssuesSurfaceHeader } from "../../issues/components/issues-page";
import { ProjectsPage } from "../../projects/components/projects-page";
import { AutopilotsPage } from "../../autopilots/components/autopilots-page";
import { TeamIcon } from "./team-icon";
import { useT } from "../../i18n";

/**
 * Team surface pages — the sidebar's per-team children (/team/:key/...).
 * Teams are addressed by key (readable URLs; keys freeze once a team has
 * issues), resolved against the cached team list.
 */
function useTeamByKey(teamKey: string): { team: Team | undefined; resolved: boolean } {
  const wsId = useWorkspaceId();
  const { data: teams = [], isSuccess } = useQuery(activeTeamListOptions(wsId));
  const team = teams.find((t) => t.key.toLowerCase() === teamKey.toLowerCase());
  return { team, resolved: isSuccess };
}

function TeamNotFound() {
  const { t } = useT("teams");
  return (
    <div className="flex flex-1 min-h-0 items-center justify-center text-sm text-muted-foreground">
      {t(($) => $.surface.not_found)}
    </div>
  );
}

export function TeamIssuesPage({ teamKey }: { teamKey: string }) {
  const { t } = useT("issues");
  const p = useWorkspacePaths();
  const { team, resolved } = useTeamByKey(teamKey);
  if (!team) return resolved ? <TeamNotFound /> : null;

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="gap-2">
        {/* Team name links into the team's settings page. */}
        <AppLink
          href={p.teamSettings(team.key)}
          className="flex items-center gap-2 rounded px-1 -mx-1 transition-colors hover:bg-accent/40"
        >
          <TeamIcon team={team} />
          <h1 className="text-sm font-medium">{team.name}</h1>
        </AppLink>
        <span className="text-xs text-muted-foreground">
          {t(($) => $.page.breadcrumb_title)}
        </span>
      </PageHeader>

      <IssueSurface
        scope={{ type: "team", teamId: team.id }}
        modes={["board", "list", "swimlane"]}
        batchToolbar="list"
        renderHeader={({ controller }) => (
          <IssuesSurfaceHeader
            issues={controller.surfaceIssues}
            isRefreshing={controller.isRefreshing}
          />
        )}
        renderEmpty={() => (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">{t(($) => $.page.empty_title)}</p>
            <p className="text-xs">{t(($) => $.page.empty_hint)}</p>
          </div>
        )}
      />
    </div>
  );
}

export function TeamProjectsPage({ teamKey }: { teamKey: string }) {
  const { team, resolved } = useTeamByKey(teamKey);
  if (!team) return resolved ? <TeamNotFound /> : null;
  return <ProjectsPage teamId={team.id} />;
}

export function TeamAutopilotsPage({ teamKey }: { teamKey: string }) {
  const { team, resolved } = useTeamByKey(teamKey);
  if (!team) return resolved ? <TeamNotFound /> : null;
  return <AutopilotsPage teamId={team.id} />;
}
