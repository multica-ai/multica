"use client";

import type { ReactElement } from "react";
import { Check, Users } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { activeTeamListOptions } from "@multica/core/teams/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import type { Team } from "@multica/core/types";
import { TeamIcon } from "./team-icon";
import { useT } from "../../i18n";

// icon + key: how a team is identified everywhere (picker rows, triggers).
function TeamBadge({ team }: { team: Team }) {
  return (
    <span className="flex items-center gap-1.5 overflow-hidden">
      <TeamIcon team={team} />
      <span className="shrink-0 text-[10px] font-medium text-muted-foreground">
        {team.key}
      </span>
    </span>
  );
}

// Single-select and non-clearable by design: every issue belongs to exactly
// one team, so the picker never offers an empty state — `teamId` is only
// null while the team list is still loading.
export function TeamPicker({
  teamId,
  onChange,
  triggerRender,
  align = "start",
  allowedTeamIds,
  disabled = false,
}: {
  teamId: string | null;
  onChange: (teamId: string) => void;
  triggerRender?: ReactElement;
  align?: "start" | "center" | "end";
  // When set, restricts the offered teams to this id set (e.g. the selected
  // project's teams). Undefined means no constraint.
  allowedTeamIds?: string[];
  // Locked display (e.g. sub-issues inherit the parent's team server-side).
  disabled?: boolean;
}) {
  const { t } = useT("teams");
  const wsId = useWorkspaceId();
  const { data: allTeams = [] } = useQuery(activeTeamListOptions(wsId));
  const teams = allowedTeamIds
    ? allTeams.filter((team) => allowedTeamIds.includes(team.id))
    : allTeams;
  // Resolve the display label against the unfiltered list so a selection
  // outside `allowedTeamIds` (e.g. before the caller converges it) still
  // renders instead of flashing the placeholder.
  const current = allTeams.find((team) => team.id === teamId);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        className={
          triggerRender
            ? undefined
            : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden disabled:cursor-default disabled:hover:bg-transparent"
        }
        render={triggerRender}
      >
        {/* Trigger shows icon + name — the key alone reads as an opaque
            identifier out of context (e.g. an auto-normalized "T2323");
            key + name live together in the menu items. */}
        {current ? (
          <>
            <TeamIcon team={current} />
            <span className="truncate">{current.name}</span>
          </>
        ) : (
          <>
            <Users className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{t(($) => $.picker.placeholder)}</span>
          </>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-56">
        {teams.map((team) => (
          <DropdownMenuItem key={team.id} onClick={() => onChange(team.id)}>
            <TeamBadge team={team} />
            <span className="truncate">{team.name}</span>
            {team.id === teamId && (
              <Check className="ml-auto h-3.5 w-3.5 shrink-0" />
            )}
          </DropdownMenuItem>
        ))}
        {teams.length === 0 && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">
            {t(($) => $.picker.empty)}
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function TeamMultiPicker({
  teamIds,
  onChange,
  triggerRender,
  align = "start",
}: {
  teamIds: string[];
  onChange: (teamIds: string[]) => void;
  triggerRender?: ReactElement;
  align?: "start" | "center" | "end";
}) {
  const { t } = useT("teams");
  const wsId = useWorkspaceId();
  const { data: teams = [] } = useQuery(activeTeamListOptions(wsId));
  const selected = teams.filter((team) => teamIds.includes(team.id));

  const toggle = (teamId: string) => {
    onChange(
      teamIds.includes(teamId)
        ? teamIds.filter((id) => id !== teamId)
        : [...teamIds, teamId],
    );
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className={
          triggerRender
            ? undefined
            : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden"
        }
        render={triggerRender}
      >
        {/* Mirror the single picker's icon+name trigger when exactly one team
            is selected; degrade to icons+count for multiple. */}
        {selected.length === 0 ? (
          <>
            <Users className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{t(($) => $.picker.placeholder)}</span>
          </>
        ) : selected.length === 1 ? (
          <>
            <TeamIcon team={selected[0]!} />
            <span className="truncate">{selected[0]!.name}</span>
          </>
        ) : (
          <>
            <span className="flex shrink-0 items-center gap-0.5">
              {selected.slice(0, 3).map((team) => (
                <TeamIcon key={team.id} team={team} />
              ))}
            </span>
            <span className="truncate">
              {t(($) => $.picker.selected_count, { count: selected.length })}
            </span>
          </>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-56">
        {teams.map((team) => {
          const checked = teamIds.includes(team.id);
          return (
            <DropdownMenuCheckboxItem
              key={team.id}
              checked={checked}
              onCheckedChange={() => toggle(team.id)}
            >
              <TeamBadge team={team} />
              <span className="truncate">{team.name}</span>
            </DropdownMenuCheckboxItem>
          );
        })}
        {teams.length === 0 && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">
            {t(($) => $.picker.empty)}
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
