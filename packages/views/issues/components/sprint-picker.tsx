"use client";

import { Check, Timer, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { sprintListOptions } from "@multica/core/sprints/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { UpdateIssueRequest } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";

export function SprintPicker({
  sprintId,
  onUpdate,
  align = "start",
  defaultOpen = false,
}: {
  sprintId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  align?: "start" | "center" | "end";
  defaultOpen?: boolean;
}) {
  const { t } = useT("sprints");
  const wsId = useWorkspaceId();
  const { data: sprints = [] } = useQuery(sprintListOptions(wsId));
  const current = sprints.find((s) => s.id === sprintId);

  return (
    <DropdownMenu defaultOpen={defaultOpen}>
      <DropdownMenuTrigger className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden">
        <Timer className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate">
          {current
            ? `${current.name} · ${current.start_date}–${current.end_date}`
            : t(($) => $.picker.no_sprint)}
        </span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-56">
        {sprints.map((sprint) => (
          <DropdownMenuItem key={sprint.id} onClick={() => onUpdate({ sprint_id: sprint.id })}>
            <span className="truncate">{sprint.name}</span>
            <span className="ml-1 text-xs text-muted-foreground shrink-0">
              {sprint.start_date}–{sprint.end_date}
            </span>
            {sprint.id === sprintId && <Check className="ml-auto h-3.5 w-3.5 shrink-0" />}
          </DropdownMenuItem>
        ))}
        {sprints.length > 0 && sprintId && <DropdownMenuSeparator />}
        {sprintId && (
          <DropdownMenuItem onClick={() => onUpdate({ sprint_id: null })}>
            <X className="h-3.5 w-3.5 text-muted-foreground" />
            {t(($) => $.picker.remove_sprint)}
          </DropdownMenuItem>
        )}
        {sprints.length === 0 && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">
            {t(($) => $.picker.no_sprints)}
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
