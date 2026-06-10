// CEREBRO: TECH-3298 — per-workspace self-wakeup limits, rendered inline under
// the wakeup feature in the Cerebro features settings tab. Two numbers: how
// many times an agent may wake itself on one issue, and the minimum gap between
// two time-based wakeups. Read by any member; only admins/owners can change them.
"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";

const queryKey = (wsId: string) => ["cerebro-wakeup-settings", wsId] as const;

const MAX_PER_ISSUE_BOUNDS = { min: 1, max: 100 };
const MIN_INTERVAL_BOUNDS = { min: 0, max: 1440 };

function clamp(value: number, min: number, max: number): number {
  if (Number.isNaN(value)) return min;
  return Math.min(max, Math.max(min, Math.round(value)));
}

export function WakeupLimitsSettings() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { role } = useCurrentMember(wsId);
  const isAdmin = role === "owner" || role === "admin";

  const { data, isError } = useQuery({
    queryKey: queryKey(wsId),
    queryFn: () => api.getWakeupSettings(wsId),
  });

  // Local draft so typing is smooth; seeded whenever server data arrives.
  const [maxPerIssue, setMaxPerIssue] = useState<string>("");
  const [minInterval, setMinInterval] = useState<string>("");
  useEffect(() => {
    if (!data) return;
    setMaxPerIssue(String(data.max_self_per_issue));
    setMinInterval(String(data.min_interval_minutes));
  }, [data]);

  const mutation = useMutation({
    mutationFn: ({ max, min }: { max: number; min: number }) =>
      api.setWakeupSettings(wsId, max, min),
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to update wakeup limits");
      // Re-seed from the server so the inputs don't keep a rejected value.
      void qc.invalidateQueries({ queryKey: queryKey(wsId) });
    },
    onSuccess: (res) => {
      qc.setQueryData(queryKey(wsId), res);
    },
  });

  const commit = (rawMax: string, rawMin: string) => {
    if (!isAdmin || !data) return;
    const max = clamp(Number(rawMax), MAX_PER_ISSUE_BOUNDS.min, MAX_PER_ISSUE_BOUNDS.max);
    const min = clamp(Number(rawMin), MIN_INTERVAL_BOUNDS.min, MIN_INTERVAL_BOUNDS.max);
    setMaxPerIssue(String(max));
    setMinInterval(String(min));
    if (max === data.max_self_per_issue && min === data.min_interval_minutes) return;
    mutation.mutate({ max, min });
  };

  if (isError) {
    return (
      <p className="text-xs text-destructive">Failed to load wakeup limits.</p>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-4">
        <div className="space-y-0.5">
          <Label className="text-xs font-medium">Max self-wakeups per issue</Label>
          <p className="text-xs text-muted-foreground">
            How many times an agent may schedule itself to wake up on the same issue.
          </p>
        </div>
        <Input
          type="number"
          inputMode="numeric"
          min={MAX_PER_ISSUE_BOUNDS.min}
          max={MAX_PER_ISSUE_BOUNDS.max}
          value={maxPerIssue}
          disabled={!isAdmin || mutation.isPending}
          onChange={(e) => setMaxPerIssue(e.target.value)}
          onBlur={() => commit(maxPerIssue, minInterval)}
          className="w-20 text-right"
          aria-label="Max self-wakeups per issue"
        />
      </div>

      <div className="flex items-center justify-between gap-4">
        <div className="space-y-0.5">
          <Label className="text-xs font-medium">Minimum minutes between wakeups</Label>
          <p className="text-xs text-muted-foreground">
            The shortest gap allowed between two time-based wakeups on the same issue.
          </p>
        </div>
        <Input
          type="number"
          inputMode="numeric"
          min={MIN_INTERVAL_BOUNDS.min}
          max={MIN_INTERVAL_BOUNDS.max}
          value={minInterval}
          disabled={!isAdmin || mutation.isPending}
          onChange={(e) => setMinInterval(e.target.value)}
          onBlur={() => commit(maxPerIssue, minInterval)}
          className="w-20 text-right"
          aria-label="Minimum minutes between wakeups"
        />
      </div>

      {!isAdmin && (
        <p className="text-xs text-muted-foreground">
          Only workspace admins can change these limits.
        </p>
      )}
    </div>
  );
}
