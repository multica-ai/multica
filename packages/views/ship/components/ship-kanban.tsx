"use client";

import { AlertTriangle } from "lucide-react";
import type { PullRequest } from "@multica/core/types";
import { useT } from "../../i18n";
import { bucketPullRequests, type ShipKanbanColumn } from "../hooks/use-pr-state";
import { ShipPRCard } from "./ship-pr-card";

interface ShipKanbanProps {
  /** All PRs for the project (any state). The component buckets locally
   *  via deriveShipKanbanColumn — no server-side state filtering required.
   *  Pass an empty array while loading; the empty-state messaging only
   *  fires once `isLoading` is false. */
  pullRequests: PullRequest[];
  isLoading?: boolean;
}

const COLUMNS: ShipKanbanColumn[] = [
  "drafted",
  "in_review",
  "ready_to_land",
  "recently_merged",
];

const COLUMN_ACCENT: Record<ShipKanbanColumn, string> = {
  // Semantic accent strips on top of each column for quick visual scanning.
  // Use semantic tokens where available; the saturated accent pulls from
  // the existing chart palette so dark mode still reads.
  drafted: "bg-violet-500/40",
  in_review: "bg-blue-500/40",
  ready_to_land: "bg-emerald-500/40",
  recently_merged: "bg-orange-500/40",
};

function ColumnHeader({
  column,
  count,
}: {
  column: ShipKanbanColumn;
  count: number;
}) {
  const { t } = useT("ship");
  const labelKey =
    column === "drafted"
      ? "drafted"
      : column === "in_review"
        ? "in_review"
        : column === "ready_to_land"
          ? "ready_to_land"
          : "recently_merged";
  return (
    <div className="flex items-center justify-between gap-2 px-2 py-1.5">
      <div className="flex items-center gap-2">
        <span className={`size-2 rounded-full ${COLUMN_ACCENT[column]}`} />
        <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {t(($) => $.kanban[labelKey])}
        </span>
      </div>
      <span className="text-xs tabular-nums text-muted-foreground">
        {count}
      </span>
    </div>
  );
}

export function ShipKanban({ pullRequests, isLoading }: ShipKanbanProps) {
  const { t } = useT("ship");
  const buckets = bucketPullRequests(pullRequests);

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
              <ShipPRCard key={`fail-${pr.id}`} pr={pr} />
            ))}
          </div>
        </div>
      )}

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        {COLUMNS.map((col) => (
          <div
            key={col}
            className="flex min-h-[8rem] flex-col rounded-md border bg-muted/20"
          >
            <ColumnHeader column={col} count={buckets[col].length} />
            <div className="flex flex-col gap-2 p-2 pt-0">
              {buckets[col].length === 0 ? (
                <div className="px-2 py-4 text-center text-xs text-muted-foreground">
                  {isLoading ? "" : t(($) => $.kanban.empty_column)}
                </div>
              ) : (
                buckets[col].map((pr) => <ShipPRCard key={pr.id} pr={pr} />)
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
