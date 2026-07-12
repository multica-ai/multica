"use client";

// FIR-3114 — after replying on an issue that sits in a batch Round, the reply
// is held until the Round runs. This bar makes that state visible on the issue
// itself, styled like the scheduled-run (wakeup) banner: same warning-tinted
// container, so "your reply is waiting for the Round" reads like "a run is
// scheduled". Renders nothing for issues outside a batch Round or with no held
// replies, and only for the Round's owner (round statuses are owner-scoped).

import { Pause } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { useRoundStatuses } from "./queries";

function nextRunLabel(value: string | null | undefined): string | null {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return new Intl.DateTimeFormat(undefined, { weekday: "short", hour: "2-digit", minute: "2-digit" }).format(date);
}

export function RoundHeldReplyBar({ issueId }: { issueId: string }) {
  const enabled = useFeatureFlag("cerebro_inbox_rounds");
  const wsId = useWorkspaceId();
  const { data: statuses = [] } = useRoundStatuses(wsId, enabled);
  if (!enabled) return null;
  const status = statuses.find((candidate) => candidate.members.some((member) => member.issue_id === issueId));
  const member = status?.members.find((candidate) => candidate.issue_id === issueId);
  if (!status || !member || status.round.mode !== "batch" || member.held_trigger_count === 0) return null;
  const next = nextRunLabel(status.round.next_run_at);
  const held = member.held_trigger_count;
  return (
    <div className="mt-4 overflow-hidden rounded-lg border border-warning/30 bg-warning/10" role="status" aria-label={`Reply held for ${status.round.name}`}>
      <div className="flex items-center gap-2 px-3 py-2 text-xs">
        <Pause className="h-3.5 w-3.5 shrink-0 text-warning" />
        <span className="min-w-0 truncate font-medium text-foreground">
          {held === 1 ? "Reply held" : `${held} replies held`} for “{status.round.name}”
        </span>
        <span className="ml-auto shrink-0 text-muted-foreground">
          {next ? `Runs ${next}` : "Runs when you press Run"}
        </span>
      </div>
    </div>
  );
}
