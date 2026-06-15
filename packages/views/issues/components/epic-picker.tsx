"use client";

import { Check, Diamond, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { epicListOptions } from "@multica/core/epics/queries";
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

export function EpicPicker({
  epicId,
  onUpdate,
  align = "start",
  defaultOpen = false,
}: {
  epicId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  align?: "start" | "center" | "end";
  defaultOpen?: boolean;
}) {
  const { t } = useT("epics");
  const wsId = useWorkspaceId();
  const { data: epics = [] } = useQuery(epicListOptions(wsId));
  const current = epics.find((e) => e.id === epicId);

  return (
    <DropdownMenu defaultOpen={defaultOpen}>
      <DropdownMenuTrigger className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden">
        {current ? (
          <span
            className="inline-block size-3 shrink-0 rounded-sm"
            style={{ backgroundColor: current.color }}
          />
        ) : (
          <Diamond className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className="truncate">
          {current ? current.title : t(($) => $.picker.no_epic)}
        </span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-52">
        {epics.map((epic) => (
          <DropdownMenuItem key={epic.id} onClick={() => onUpdate({ epic_id: epic.id })}>
            <span
              className="inline-block size-3 shrink-0 rounded-sm mr-1.5"
              style={{ backgroundColor: epic.color }}
            />
            <span className="truncate">{epic.title}</span>
            {epic.id === epicId && <Check className="ml-auto h-3.5 w-3.5 shrink-0" />}
          </DropdownMenuItem>
        ))}
        {epics.length > 0 && epicId && <DropdownMenuSeparator />}
        {epicId && (
          <DropdownMenuItem onClick={() => onUpdate({ epic_id: null })}>
            <X className="h-3.5 w-3.5 text-muted-foreground" />
            {t(($) => $.picker.remove_epic)}
          </DropdownMenuItem>
        )}
        {epics.length === 0 && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">
            {t(($) => $.picker.no_epics)}
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
