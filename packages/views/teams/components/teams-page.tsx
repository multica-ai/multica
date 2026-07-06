"use client";

import { useMemo } from "react";
import { Plus, Trash2, Users } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { teamListOptions } from "@multica/core/teams/queries";
import { useArchiveTeam } from "@multica/core/teams/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import type { Team } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  ListGrid,
  ListGridBody,
  ListGridCell,
  ListGridHeader,
  ListGridHeaderCell,
  ListGridRow,
} from "@multica/ui/components/ui/list-grid";
import { useRowLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { TeamIcon } from "./team-icon";
import { useT } from "../../i18n";

/**
 * Teams directory — rows link into each team's settings page, where editing
 * lives. Creation goes through the shared "create-team" modal (also opened
 * from the sidebar's + action); this page only lists and archives.
 */
export function TeamsPage() {
  const { t } = useT("teams");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { data: teams = [], isLoading } = useQuery(teamListOptions(wsId));
  const archiveTeam = useArchiveTeam();
  const rowLink = useRowLink();
  const openCreate = () => useModalStore.getState().open("create-team");

  const sortedTeams = useMemo(
    () =>
      [...teams].sort((a, b) => {
        if (a.is_default !== b.is_default) return a.is_default ? -1 : 1;
        if (!!a.archived_at !== !!b.archived_at) return a.archived_at ? 1 : -1;
        return a.name.localeCompare(b.name);
      }),
    [teams],
  );

  const handleArchive = async (team: Team) => {
    try {
      await archiveTeam.mutateAsync(team.id);
      toast.success(t(($) => $.toast_archived));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.toast_archive_failed),
      );
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="gap-2">
        <Users className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
        <Button size="sm" className="ml-auto" onClick={openCreate}>
          <Plus className="h-3.5 w-3.5" />
          {t(($) => $.page.new_team)}
        </Button>
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto @container">
        {isLoading ? (
          <div className="rounded-md border p-4 text-sm text-muted-foreground">
            {t(($) => $.page.loading)}
          </div>
        ) : sortedTeams.length === 0 ? (
          <div className="flex h-full min-h-72 flex-col items-center justify-center gap-2 text-muted-foreground">
            <Users className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">{t(($) => $.page.empty_title)}</p>
            <Button size="sm" onClick={openCreate}>
              {t(($) => $.page.create_first)}
            </Button>
          </div>
        ) : (
          <ListGrid className="grid-cols-[0.75rem_minmax(10rem,1fr)_8rem_2.5rem_0.75rem] @2xl:grid-cols-[0.75rem_minmax(12rem,1fr)_6rem_9rem_2.5rem_0.75rem]">
            <ListGridHeader>
              <ListGridHeaderCell>{t(($) => $.table.name)}</ListGridHeaderCell>
              <ListGridHeaderCell className="hidden @2xl:flex">
                {t(($) => $.table.issues)}
              </ListGridHeaderCell>
              <ListGridHeaderCell>{t(($) => $.table.state)}</ListGridHeaderCell>
              <ListGridHeaderCell align="right">
                {t(($) => $.table.actions)}
              </ListGridHeaderCell>
            </ListGridHeader>
            <ListGridBody>
              {sortedTeams.map((team) => (
                <ListGridRow
                  key={team.id}
                  className="cursor-pointer"
                  {...rowLink(p.teamDetail(team.key))}
                >
                  <ListGridCell className="gap-2">
                    <TeamIcon team={team} />
                    <span className="shrink-0 text-[10px] font-medium text-muted-foreground">
                      {team.key}
                    </span>
                    <span className="min-w-0 truncate text-sm font-medium">{team.name}</span>
                  </ListGridCell>
                  <ListGridCell className="hidden text-sm tabular-nums text-muted-foreground @2xl:flex">
                    {team.issue_counter}
                  </ListGridCell>
                  <ListGridCell className="gap-1.5">
                    {team.is_default && (
                      <Badge variant="secondary">{t(($) => $.state.default)}</Badge>
                    )}
                    {team.archived_at && (
                      <Badge variant="outline">{t(($) => $.state.archived)}</Badge>
                    )}
                    {!team.is_default && !team.archived_at && (
                      <Badge variant="outline">{t(($) => $.state.active)}</Badge>
                    )}
                  </ListGridCell>
                  <ListGridCell className="justify-end px-0">
                    <Button
                      size="icon"
                      variant="ghost"
                      className="h-7 w-7 text-muted-foreground hover:text-destructive"
                      disabled={team.is_default || !!team.archived_at}
                      onClick={(event) => {
                        event.stopPropagation();
                        handleArchive(team);
                      }}
                      aria-label={t(($) => $.actions.archive)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </ListGridCell>
                </ListGridRow>
              ))}
            </ListGridBody>
          </ListGrid>
        )}
      </div>
    </div>
  );
}
