// CEREBRO-PATCH(cerebro-wakeup-sidebar): TECH-3144 — show pending wakeups in the issue sidebar with cancel action.
"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlarmClock, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";

const WAKEUP_QUERY_KEY = (issueId: string) => ["cerebro-wakeups", issueId];

function formatWakeupTime(isoString: string): string {
  try {
    return new Intl.DateTimeFormat("da-DK", {
      timeZone: "Europe/Copenhagen",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    }).format(new Date(isoString));
  } catch {
    return isoString;
  }
}

function triggerLabel(triggerType: string, fireAt?: string, watchStatus?: string): string {
  if (triggerType === "time" && fireAt) return formatWakeupTime(fireAt);
  if (triggerType === "issue_status" && watchStatus) return `Ved status: ${watchStatus}`;
  if (triggerType === "github_ci") return "Ved CI-opdatering";
  return triggerType;
}

export function CerebroWakeupSection({ issueId }: { issueId: string }) {
  const queryClient = useQueryClient();

  // Always load pending wakeups — no toggle needed to see whether any exist.
  const { data } = useQuery({
    queryKey: WAKEUP_QUERY_KEY(issueId),
    queryFn: () => api.listIssueWakeups(issueId),
    staleTime: 30_000,
  });

  const wakeups = data?.wakeups ?? [];

  async function handleCancel(id: string) {
    try {
      await api.cancelWakeup(id);
      await queryClient.invalidateQueries({ queryKey: WAKEUP_QUERY_KEY(issueId) });
    } catch {
      toast.error("Kunne ikke annullere wakeup");
    }
  }

  // Hide the section entirely when there is nothing pending, so it never adds
  // empty clutter — but when wakeups exist they are visible at a glance.
  if (wakeups.length === 0) return null;

  return (
    <div>
      <div className="flex items-center gap-1 px-2 py-1 text-xs font-medium text-muted-foreground mb-1">
        <AlarmClock className="!size-3 shrink-0" />
        <span className="flex-1 text-left">Planlagte wakeups</span>
        <span className="tabular-nums">{wakeups.length}</span>
      </div>

      <div className="pl-2 space-y-1">
        {wakeups.map((w) => (
          <div
            key={w.id}
            className="flex items-start gap-1 rounded-md px-1 py-1 text-xs hover:bg-accent/50 group"
          >
            <div className="flex-1 min-w-0">
              <p className="font-medium text-foreground truncate">
                {triggerLabel(w.trigger_type, w.fire_at, w.watch_status)}
              </p>
              <p className="text-muted-foreground truncate">{w.prompt}</p>
            </div>
            <Tooltip>
              {/* CEREBRO-PATCH(wakeup-cancel-tooltip-render): Use Base UI render prop; TooltipTrigger does not accept button props directly. */}
              <TooltipTrigger
                render={
                  <button
                    type="button"
                    className="shrink-0 mt-0.5 rounded p-0.5 opacity-0 group-hover:opacity-100 hover:bg-destructive/10 hover:text-destructive transition-all"
                    onClick={() => handleCancel(w.id)}
                    aria-label="Annuller wakeup"
                  />
                }
              >
                <X className="!size-3" />
              </TooltipTrigger>
              <TooltipContent side="left">Annuller</TooltipContent>
            </Tooltip>
          </div>
        ))}
      </div>
    </div>
  );
}
