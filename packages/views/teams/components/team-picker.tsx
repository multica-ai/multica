"use client";

import type { ReactElement } from "react";
import { Check, Users, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { activeTeamListOptions } from "@multica/core/teams/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import type { Team } from "@multica/core/types";
import { useT } from "../../i18n";

function TeamKey({ team }: { team: Team }) {
  return (
    <span className="inline-flex h-5 min-w-7 items-center justify-center rounded bg-muted px-1.5 text-[10px] font-medium text-muted-foreground">
      {team.key}
    </span>
  );
}

export function TeamPicker({
  teamId,
  onChange,
  triggerRender,
  align = "start",
  allowClear = false,
  allowedTeamIds,
}: {
  teamId: string | null;
  onChange: (teamId: string | null) => void;
  triggerRender?: ReactElement;
  align?: "start" | "center" | "end";
  allowClear?: boolean;
  // When set, restricts the offered teams to this id set (e.g. the selected
  // project's teams). Undefined means no constraint.
  allowedTeamIds?: string[];
}) {
  const { t } = useT("teams");
  const wsId = useWorkspaceId();
  const { data: allTeams = [] } = useQuery(activeTeamListOptions(wsId));
  const teams = allowedTeamIds
    ? allTeams.filter((team) => allowedTeamIds.includes(team.id))
    : allTeams;
  const current = teams.find((team) => team.id === teamId);

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
        {current ? (
          <TeamKey team={current} />
        ) : (
          <Users className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className="truncate">
          {current ? current.name : t(($) => $.picker.placeholder)}
        </span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-56">
        {teams.map((team) => (
          <DropdownMenuItem key={team.id} onClick={() => onChange(team.id)}>
            <TeamKey team={team} />
            <span className="truncate">{team.name}</span>
            {team.id === teamId && (
              <Check className="ml-auto h-3.5 w-3.5 shrink-0" />
            )}
          </DropdownMenuItem>
        ))}
        {allowClear && teamId && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => onChange(null)}>
              <X className="h-3.5 w-3.5 text-muted-foreground" />
              {t(($) => $.picker.clear)}
            </DropdownMenuItem>
          </>
        )}
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
        <Users className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate">
          {selected.length === 0
            ? t(($) => $.picker.placeholder)
            : t(($) => $.picker.selected_count, { count: selected.length })}
        </span>
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
              <TeamKey team={team} />
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
