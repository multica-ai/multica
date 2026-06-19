"use client";

// FIR-1597. Renders next to the Start date / Due date pickers in the issue side
// panel. Lets the user attach an OPTIONAL wall-clock time to a date that is
// already set. A start time auto-starts an agent-assigned issue at that moment;
// a due time makes the "due" reminder fire at that moment instead of at the
// start of the day. Empty = no time, the date behaves exactly as before.
import { useState } from "react";
import { Clock, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";

import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";

import {
  EMPTY_ISSUE_DATE_TIMES,
  type IssueTimeKind,
} from "../core/types";
import {
  issueDateTimesOptions,
  useSetIssueDateTimes,
} from "../core/queries";

interface Props {
  workspaceId: string;
  issueId: string;
  kind: IssueTimeKind;
  className?: string;
}

export function IssueTimePicker({ workspaceId, issueId, kind, className }: Props) {
  const [open, setOpen] = useState(false);
  const query = useQuery(issueDateTimesOptions(workspaceId, issueId));
  const save = useSetIssueDateTimes(workspaceId, issueId);

  const data = query.data ?? EMPTY_ISSUE_DATE_TIMES;
  const value = kind === "start" ? data.start_time : data.due_time;

  const commit = (next: string | null) => {
    save.mutate(
      {
        start_time: kind === "start" ? next : data.start_time,
        due_time: kind === "due" ? next : data.due_time,
      },
      { onError: () => toast.error("Failed to update time") },
    );
  };

  return (
    <div className={className}>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger className="flex cursor-pointer items-center gap-1 rounded -mx-1 px-1 text-xs text-muted-foreground transition-colors hover:bg-accent/30">
          <Clock className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate">{value ?? "Add time"}</span>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-44 gap-2 p-2">
          <input
            type="time"
            value={value ?? ""}
            onChange={(e) => commit(e.target.value || null)}
            className="w-full rounded border bg-background px-2 py-1 text-sm"
          />
          {value && (
            <button
              type="button"
              onClick={() => {
                commit(null);
                setOpen(false);
              }}
              className="flex w-full cursor-pointer items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-xs text-muted-foreground transition-colors hover:bg-accent"
            >
              <X className="h-3.5 w-3.5 shrink-0" />
              Clear time
            </button>
          )}
        </PopoverContent>
      </Popover>
    </div>
  );
}
