"use client";

import { useMemo, useState } from "react";
import { Users, Clock } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { useWorkspaceStore } from "@/features/workspace";
import { useProjectsQuery } from "@/features/projects/queries";
import { useTeamTimeStatsQuery } from "../hooks/use-time-tracking";
import { formatDuration } from "../components/LiveDuration";

// ── Range helpers ─────────────────────────────────────────────────────────────

type Range = "this-week" | "this-month" | "last-month";

function getRange(range: Range): { since: string; until: string; label: string } {
  const now = new Date();

  if (range === "this-week") {
    // Week starts on Monday.
    const day = now.getDay(); // 0=Sun, 1=Mon, ...
    const diff = (day === 0 ? -6 : 1 - day); // days back to Monday
    const monday = new Date(now);
    monday.setDate(now.getDate() + diff);
    monday.setHours(0, 0, 0, 0);

    const nextMonday = new Date(monday);
    nextMonday.setDate(monday.getDate() + 7);

    return {
      since: monday.toISOString(),
      until: nextMonday.toISOString(),
      label: "This Week",
    };
  }

  if (range === "this-month") {
    const since = new Date(now.getFullYear(), now.getMonth(), 1);
    const until = new Date(now.getFullYear(), now.getMonth() + 1, 1);
    return {
      since: since.toISOString(),
      until: until.toISOString(),
      label: "This Month",
    };
  }

  // last-month
  const since = new Date(now.getFullYear(), now.getMonth() - 1, 1);
  const until = new Date(now.getFullYear(), now.getMonth(), 1);
  return {
    since: since.toISOString(),
    until: until.toISOString(),
    label: "Last Month",
  };
}

// ── Sub-components ─────────────────────────────────────────────────────────────

/** Displays a labeled row with a time value in a summary table. */
function StatRow({ label, seconds }: { label: string; seconds: number }) {
  const hours = Math.floor(seconds / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  const formatted = hours > 0 ? `${hours}h ${mins}m` : `${mins}m`;

  return (
    <div className="flex items-center justify-between py-2 text-sm border-b last:border-0">
      <span className="truncate text-foreground">{label}</span>
      <span className="shrink-0 ml-4 font-mono text-muted-foreground">{formatted}</span>
    </div>
  );
}

// ── TeamTimePage ───────────────────────────────────────────────────────────────

/**
 * Workspace-level team time review page.
 *
 * Shows two tables for the selected time range:
 * - By member: each member's total logged hours
 * - By project: each project's total logged hours
 */
export function TeamTimePage() {
  const [activeRange, setActiveRange] = useState<Range>("this-week");
  const { since, until, label } = useMemo(() => getRange(activeRange), [activeRange]);

  const members = useWorkspaceStore((s) => s.members);
  const { data: projects } = useProjectsQuery();
  const { data: stats, isLoading } = useTeamTimeStatsQuery({ since, until });

  // Build a map of user_id → display name from workspace members.
  const memberNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const m of members) {
      const name = m.name?.trim() || m.email;
      map.set(m.user_id, name);
    }
    return map;
  }, [members]);

  // Build a map of project_id → title.
  const projectTitleById = useMemo(() => {
    const map = new Map<string, string>();
    for (const p of (projects ?? [])) {
      map.set(p.id, `${p.icon ?? "📁"} ${p.title}`);
    }
    return map;
  }, [projects]);

  // Total seconds across all users in range.
  const totalSeconds = useMemo(
    () => (stats?.by_user ?? []).reduce((acc, r) => acc + r.total_seconds, 0),
    [stats],
  );

  const RANGES: { id: Range; label: string }[] = [
    { id: "this-week", label: "This Week" },
    { id: "this-month", label: "This Month" },
    { id: "last-month", label: "Last Month" },
  ];

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* Page header */}
      <div className="flex items-center justify-between border-b px-6 py-4">
        <div className="flex items-center gap-2">
          <Users className="size-5 text-muted-foreground" />
          <h1 className="text-lg font-semibold">Team Time</h1>
        </div>

        {/* Range selector */}
        <div className="flex items-center gap-1 rounded-lg border p-1">
          {RANGES.map((r) => (
            <Button
              key={r.id}
              variant={activeRange === r.id ? "secondary" : "ghost"}
              size="sm"
              className="h-7 px-3 text-xs"
              onClick={() => setActiveRange(r.id)}
            >
              {r.label}
            </Button>
          ))}
        </div>
      </div>

      <div className="flex-1 overflow-y-auto px-6 py-6">
        <div className="mx-auto max-w-2xl space-y-8">

          {/* Summary bar */}
          {!isLoading && stats && (
            <div className="flex items-center gap-3 rounded-lg border bg-muted/40 px-4 py-3">
              <Clock className="size-4 shrink-0 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">
                <span className="font-semibold text-foreground">
                  {formatDuration(totalSeconds)}
                </span>
                {" "}total tracked by the team · {label}
              </p>
            </div>
          )}

          {isLoading ? (
            <div className="space-y-3">
              <Skeleton className="h-5 w-32" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : (
            <>
              {/* By member */}
              <section>
                <h2 className="mb-3 text-sm font-semibold">By Member</h2>
                <div className="rounded-lg border px-4">
                  {(stats?.by_user ?? []).length === 0 ? (
                    <p className="py-6 text-center text-sm text-muted-foreground">
                      No time logged by any member in this period.
                    </p>
                  ) : (
                    (stats?.by_user ?? []).map((row) => (
                      <StatRow
                        key={row.user_id}
                        label={memberNameById.get(row.user_id) ?? row.user_id}
                        seconds={row.total_seconds}
                      />
                    ))
                  )}
                </div>
              </section>

              {/* By project */}
              <section>
                <h2 className="mb-3 text-sm font-semibold">By Project</h2>
                <div className="rounded-lg border px-4">
                  {(stats?.by_project ?? []).length === 0 ? (
                    <p className="py-6 text-center text-sm text-muted-foreground">
                      No project time logged in this period.
                    </p>
                  ) : (
                    (stats?.by_project ?? []).map((row, idx) => (
                      <StatRow
                        key={row.project_id ?? `unlinked-${idx}`}
                        label={
                          row.project_id
                            ? (projectTitleById.get(row.project_id) ?? "Unknown project")
                            : "Unlinked (no project)"
                        }
                        seconds={row.total_seconds}
                      />
                    ))
                  )}
                </div>
              </section>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
