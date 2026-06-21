"use client";

// FIR-1597. Renders next to the Start date / Due date pickers in the issue side
// panel once a date is set. Two things in one place:
//   1. an OPTIONAL wall-clock time attached to the date, and
//   2. a switch to auto-start the assigned agent at that date+time.
// Auto-start is opt-in and requires a time chosen in this same popover (the
// switch stays disabled until a time is set). Turning it on for one date turns
// it off for the other — an issue auto-starts on at most one date. Empty time +
// off = the date behaves exactly as before. The on/off state reflects the
// per-issue choice, falling back to the workspace default.
import { useState } from "react";
import { Clock, Play, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";

import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Switch } from "@multica/ui/components/ui/switch";

import {
  EMPTY_ISSUE_DATE_TIMES,
  type AgentStartKind,
  type IssueDateTimes,
  type IssueTimeKind,
} from "../core/types";
import {
  issueDateTimesOptions,
  useSetIssueDateTimes,
  workspaceDateTimeSettingsOptions,
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
  const settings = useQuery(workspaceDateTimeSettingsOptions(workspaceId));
  const save = useSetIssueDateTimes(workspaceId, issueId);

  const data = query.data ?? EMPTY_ISSUE_DATE_TIMES;
  const time = kind === "start" ? data.start_time : data.due_time;

  // Effective auto-start choice: the per-issue value, else the workspace default.
  const workspaceDefault: AgentStartKind =
    settings.data?.default_agent_start_kind ?? "none";
  const effectiveKind: AgentStartKind = data.agent_start_kind ?? workspaceDefault;

  // Auto-start only actually fires when this date is the chosen one AND it has a
  // time, so the switch reflects that real outcome.
  const autoStartOn = effectiveKind === kind && !!time;

  const commit = (nextTime: string | null, nextKind: AgentStartKind | null) => {
    const payload: IssueDateTimes = {
      start_time: kind === "start" ? nextTime : data.start_time,
      due_time: kind === "due" ? nextTime : data.due_time,
      agent_start_kind: nextKind,
    };
    save.mutate(payload, { onError: () => toast.error("Failed to update time") });
  };

  const onTimeChange = (nextTime: string | null) => {
    // Clearing the time can't leave an explicit opt-in standing (no time = no
    // auto-start), so drop this date's opt-in back to "none" in that case.
    const nextKind =
      nextTime === null && data.agent_start_kind === kind
        ? "none"
        : data.agent_start_kind;
    commit(nextTime, nextKind);
  };

  const onToggleAutoStart = (next: boolean) => {
    // On => this date becomes the single auto-start date. Off => explicit "none"
    // for the whole issue (also overrides a workspace default).
    commit(time, next ? kind : "none");
  };

  const dateLabel = kind === "start" ? "start date" : "due date";

  return (
    <div className={className}>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger className="flex cursor-pointer items-center gap-1 rounded -mx-1 px-1 text-xs text-muted-foreground transition-colors hover:bg-accent/30">
          <Clock className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate">{time ?? "Add time"}</span>
          {autoStartOn && (
            <Play
              className="h-3 w-3 shrink-0 fill-current text-primary"
              aria-label="Agent auto-starts at this time"
            />
          )}
        </PopoverTrigger>
        <PopoverContent align="start" className="w-60 gap-3 p-3">
          <input
            type="time"
            value={time ?? ""}
            onChange={(e) => onTimeChange(e.target.value || null)}
            className="w-full rounded border bg-background px-2 py-1 text-sm"
            aria-label={`Time of day for the ${dateLabel}`}
          />

          <div className="flex items-start justify-between gap-3 border-t pt-3">
            <div className="space-y-0.5">
              <p className="text-xs font-medium">Start agent at this time</p>
              <p className="text-xs text-muted-foreground">
                {time
                  ? `The assigned agent starts on the ${dateLabel} at this time.`
                  : "Add a time above to enable auto-start."}
              </p>
            </div>
            <Switch
              checked={autoStartOn}
              disabled={!time || save.isPending}
              onCheckedChange={onToggleAutoStart}
              aria-label={`Start agent on the ${dateLabel}`}
            />
          </div>

          {time && (
            <button
              type="button"
              onClick={() => {
                onTimeChange(null);
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
