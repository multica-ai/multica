"use client";

import { Check } from "lucide-react";
import type { Team } from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import { TeamIcon } from "./team-icon";
import { useT } from "../../i18n";

/**
 * Resolution dialog for "the issue's team is not part of the selected
 * project" (Linear-style). The first option mirrors the server-side default
 * for headless callers — adding the team to the project — so confirming it
 * simply proceeds; picking a project team instead retargets the issue.
 */
export function TeamProjectConflictDialog({
  open,
  teamName,
  projectName,
  projectTeams,
  onAddTeam,
  onMoveToTeam,
  onCancel,
}: {
  open: boolean;
  teamName: string;
  projectName: string;
  /** The project's current teams, offered as "move the issue there" targets. */
  projectTeams: Team[];
  onAddTeam: () => void;
  onMoveToTeam: (teamId: string) => void;
  onCancel: () => void;
}) {
  const { t } = useT("teams");
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
    >
      <DialogContent showCloseButton={false} className="sm:max-w-md">
        <DialogTitle>{t(($) => $.conflict.title, { team: teamName })}</DialogTitle>
        <DialogDescription>
          {t(($) => $.conflict.body, { team: teamName, project: projectName })}
        </DialogDescription>
        <div className="flex flex-col gap-1">
          <button
            type="button"
            autoFocus
            onClick={onAddTeam}
            className="flex items-center gap-2 rounded-md border border-input bg-accent/40 px-3 py-2 text-left text-sm transition-colors hover:bg-accent"
          >
            <span className="min-w-0 flex-1 truncate">
              {t(($) => $.conflict.add_team, { team: teamName })}
            </span>
            <Check className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          </button>
          {projectTeams.length > 0 && (
            <>
              <div className="px-1 pt-1 text-xs text-muted-foreground">
                {t(($) => $.conflict.project_teams)}
              </div>
              {projectTeams.map((team) => (
                <button
                  key={team.id}
                  type="button"
                  onClick={() => onMoveToTeam(team.id)}
                  className="flex items-center gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors hover:bg-accent/60"
                >
                  <TeamIcon team={team} />
                  <span className="min-w-0 flex-1 truncate">
                    {t(($) => $.conflict.move_issue, { team: team.name })}
                  </span>
                </button>
              ))}
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
