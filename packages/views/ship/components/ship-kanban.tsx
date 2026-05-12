"use client";

import { useMemo } from "react";
import { AlertTriangle } from "lucide-react";
import { useDeployEnvironments, useRecentDeploys } from "@multica/core/ship";
import type { PullRequest } from "@multica/core/types";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useT } from "../../i18n";
import {
  bucketPullRequests,
  type ShipDeploySnapshot,
  type ShipKanbanColumn,
} from "../hooks/use-pr-state";
import { ShipPRCard } from "./ship-pr-card";

interface ShipKanbanProps {
  /** All PRs for the project (any state). The component buckets locally
   *  via deriveShipKanbanColumn — no server-side state filtering required.
   *  Pass an empty array while loading; the empty-state messaging only
   *  fires once `isLoading` is false. */
  pullRequests: PullRequest[];
  isLoading?: boolean;
  /** Project id — required so the kanban can resolve the per-project deploy
   *  environments + recent deploys, which are needed for the merged → in
   *  staging → promoting → in production columns. Phase 1 didn't need this
   *  because there were only review-state columns. */
  projectId: string;
}

const COLUMN_ACCENT: Record<ShipKanbanColumn, string> = {
  // Semantic accent strips on top of each column for quick visual scanning.
  // Use semantic tokens where available; the saturated accent pulls from
  // the existing chart palette so dark mode still reads. Eight colors
  // chosen to remain distinguishable across the row (no two adjacent
  // columns share a hue).
  drafted: "bg-violet-500/40",
  in_review: "bg-blue-500/40",
  ready_to_land: "bg-emerald-500/40",
  merged_pre_staging: "bg-amber-500/40",
  in_staging: "bg-orange-500/40",
  promoting: "bg-rose-500/40",
  in_production: "bg-teal-500/40",
  done: "bg-zinc-500/40",
};

function ColumnHeader({
  column,
  count,
}: {
  column: ShipKanbanColumn;
  count: number;
}) {
  const { t } = useT("ship");
  return (
    <div className="flex items-center justify-between gap-2 px-2 py-1.5">
      <div className="flex items-center gap-2">
        <span className={`size-2 rounded-full ${COLUMN_ACCENT[column]}`} />
        <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {t(($) => $.kanban.column[column])}
        </span>
      </div>
      <span className="text-xs tabular-nums text-muted-foreground">
        {count}
      </span>
    </div>
  );
}

/** Computes the deploy snapshot the kanban needs to bucket merged PRs into
 *  the correct deploy column.
 *
 *  Phase 2 trade-off — we need recent deploys for both staging AND
 *  production to spot in-flight production deploys. We deliberately call
 *  the hook with both env ids in a stable order so React's hook rules
 *  aren't violated (the env IDs are stable per project; the hook order
 *  doesn't change between renders even when the IDs are missing).
 *
 *  Empty/missing environments fall back to "" so the hook receives a
 *  consistent string; the underlying query is gated by `enabled: !!id`
 *  so an empty string is a no-op.
 */
function useDeploySnapshot(projectId: string): ShipDeploySnapshot {
  const { data: envData } = useDeployEnvironments(projectId);
  const envs = envData?.environments ?? [];
  // Pull the staging + production rows. We only support one of each per
  // project today; future preview environments would need extending here.
  const staging = envs.find((e) => e.kind === "staging") ?? null;
  const production = envs.find((e) => e.kind === "production") ?? null;

  // The "Promoting" column needs to know which SHAs are currently being
  // deployed to production. Use the recent deploys query (already cached
  // by the swimlanes) so we don't double-fetch.
  const { data: prodDeploys } = useRecentDeploys(production?.id ?? "", 20);

  const productionInFlightShas = useMemo(() => {
    const set = new Set<string>();
    for (const d of prodDeploys?.deploys ?? []) {
      if (d.status === "pending" || d.status === "in_progress") {
        if (d.sha) set.add(d.sha);
      }
    }
    return set;
  }, [prodDeploys]);

  return useMemo<ShipDeploySnapshot>(
    () => ({
      staging,
      production,
      productionInFlightShas,
    }),
    [staging, production, productionInFlightShas],
  );
}

export function ShipKanban({ pullRequests, isLoading, projectId }: ShipKanbanProps) {
  const { t } = useT("ship");
  const isMobile = useIsMobile();
  const snapshot = useDeploySnapshot(projectId);
  const visibleColumns = useMemo<ShipKanbanColumn[]>(() => {
    const cols: ShipKanbanColumn[] = [
      "drafted",
      "in_review",
      "ready_to_land",
      "merged_pre_staging",
    ];
    if (snapshot.staging) cols.push("in_staging");
    if (snapshot.production) {
      cols.push("promoting");
      cols.push("in_production");
    }
    cols.push("done");
    return cols;
  }, [snapshot.staging, snapshot.production]);
  const buckets = useMemo(
    () => bucketPullRequests(pullRequests, snapshot),
    [pullRequests, snapshot],
  );

  return (
    <div className="space-y-3">
      {/* Failing / blocked rail. Surfaces the same PRs that ALSO show up in
          their column below — this is a parallel "hey look at this" rail,
          not an exclusive bucket. We render it only when non-empty so we
          don't burn vertical space on a healthy project. */}
      {buckets.failing_blocked.length > 0 && (
        <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3">
          <div className="mb-2 flex items-center gap-2 text-xs font-medium text-destructive">
            <AlertTriangle className="size-3.5" />
            <span>
              {t(($) => $.kanban.failing_blocked)} ·{" "}
              <span className="tabular-nums">{buckets.failing_blocked.length}</span>
            </span>
          </div>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {buckets.failing_blocked.map((pr) => (
              <ShipPRCard
                key={`fail-${pr.id}`}
                pr={pr}
                stagingEnv={snapshot.staging}
              />
            ))}
          </div>
        </div>
      )}

      {isMobile ? (
        <div className="space-y-2">
          {visibleColumns.map((col) => (
            <details
              key={col}
              open={buckets[col].length > 0}
              className="group rounded-md border bg-muted/20"
            >
              <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
                <ColumnHeader column={col} count={buckets[col].length} />
              </summary>
              <div className="flex flex-col gap-2 p-2 pt-0">
                {buckets[col].length === 0 ? (
                  <div className="px-2 py-3 text-center text-xs text-muted-foreground">
                    {isLoading ? "" : t(($) => $.kanban.empty_column)}
                  </div>
                ) : (
                  buckets[col].map((pr) => (
                    <ShipPRCard key={pr.id} pr={pr} stagingEnv={snapshot.staging} />
                  ))
                )}
              </div>
            </details>
          ))}
        </div>
      ) : (
        <div className="overflow-x-auto pb-2">
          <div className="flex gap-3 min-w-max">
            {visibleColumns.map((col) => (
              <div
                key={col}
                className="flex w-64 shrink-0 flex-col rounded-md border bg-muted/20"
              >
                <ColumnHeader column={col} count={buckets[col].length} />
                <div className="flex flex-col gap-2 p-2 pt-0">
                  {buckets[col].length === 0 ? (
                    <div className="px-2 py-4 text-center text-xs text-muted-foreground">
                      {isLoading ? "" : t(($) => $.kanban.empty_column)}
                    </div>
                  ) : (
                    buckets[col].map((pr) => (
                      <ShipPRCard key={pr.id} pr={pr} stagingEnv={snapshot.staging} />
                    ))
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
