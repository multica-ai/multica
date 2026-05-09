"use client";

// Phase 7a — multi-select footer bar.
//
// Renders when at least one PR is selected on the Kanban. The bar
// shows the selection summary (count, projects spanned, highest
// risk) and the action affordances: create release, add to existing
// release (when an `assembling` release already exists), clear
// selection.
//
// Rendered inside `ShipPage`'s flex column root so the bar sticks to
// the bottom of the page chrome — the parent provides padding; the
// bar itself is a fixed-height row.

import { useMemo, useState } from "react";
import { ListChecks, Rocket, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useShipSelection, useShipSelectionCount } from "@multica/core/ship";
import type { PullRequest } from "@multica/core/types";
import { useT } from "../../i18n";
import { CreateReleaseDialog } from "./create-release-dialog";

interface ShipSelectionBarProps {
  /** All PRs visible on the page, used to derive the project count
   *  + highest-risk summary without keeping a parallel store. */
  pullRequests: PullRequest[];
}

/** Map from RiskLevel to a numeric rank for the "highest risk"
 *  summary. Higher number = scarier. The map is local to this
 *  component because the Kanban risk pill already has its own
 *  ordering and we don't want to share constants for what is a
 *  presentational summary line. */
const RISK_RANK: Record<string, number> = {
  low: 0,
  medium: 1,
  high: 2,
  critical: 3,
};

export function ShipSelectionBar({ pullRequests }: ShipSelectionBarProps) {
  const { t } = useT("ship");
  const count = useShipSelectionCount();
  const selected = useShipSelection((s) => s.selected);
  const clear = useShipSelection((s) => s.clear);
  const [dialogOpen, setDialogOpen] = useState(false);

  // Resolve the selected PRs from the visible set. Memoized on the
  // selection size + PR list length so we don't rebuild the array
  // on every render — selection is a Set, so reference identity
  // doesn't change unless the set changes.
  const selectedPRs = useMemo(
    () => pullRequests.filter((pr) => selected.has(pr.id)),
    [pullRequests, selected],
  );

  const projectCount = useMemo(() => {
    const set = new Set<string>();
    for (const pr of selectedPRs) {
      if (pr.project_id) set.add(pr.project_id);
    }
    return set.size;
  }, [selectedPRs]);

  const highestRisk = useMemo(() => {
    let best: string = "low";
    for (const pr of selectedPRs) {
      const candidate = pr.risk_level ?? "medium";
      if ((RISK_RANK[candidate] ?? 0) > (RISK_RANK[best] ?? 0)) {
        best = candidate;
      }
    }
    return best;
  }, [selectedPRs]);

  if (count === 0) return null;

  // Phase 7a only allows creating a release from PRs in the same
  // project (cross-project releases land in 7e). Disable submit
  // when the selection spans 2+ projects so the user gets a clear
  // signal rather than a runtime backend error.
  const canCreateRelease = projectCount === 1;

  return (
    <>
      <div
        // Sticky to the bottom of the parent flex column. Uses
        // semantic tokens so it adapts to dark mode without
        // hardcoded colors per CLAUDE.md.
        className="sticky bottom-0 left-0 right-0 z-20 border-t bg-background/95 backdrop-blur"
        data-testid="ship-selection-bar"
      >
        <div className="mx-auto flex items-center gap-3 px-5 py-2.5">
          <ListChecks className="size-4 text-muted-foreground" aria-hidden />
          <span className="text-sm font-medium">
            {t(($) => $.selection.count, { count })}
          </span>
          <span className="text-xs text-muted-foreground">·</span>
          <span className="text-xs text-muted-foreground">
            {t(($) => $.selection.across_projects, { count: projectCount })}
          </span>
          <span className="text-xs text-muted-foreground">·</span>
          <span className="text-xs text-muted-foreground">
            {t(($) => $.selection.highest_risk, { level: highestRisk })}
          </span>

          <div className="ml-auto flex items-center gap-2">
            <Button
              size="sm"
              onClick={() => setDialogOpen(true)}
              disabled={!canCreateRelease}
              data-testid="ship-selection-create-release"
            >
              <Rocket className="size-3.5" aria-hidden />
              {t(($) => $.selection.create_release)}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={clear}
              data-testid="ship-selection-clear"
            >
              <X className="size-3.5" aria-hidden />
              {t(($) => $.selection.clear)}
            </Button>
          </div>
        </div>
      </div>

      {/* Phase 7a — Create release dialog. Wired here so the bar
          owns its own dialog state; closing the dialog on success
          also clears the selection (handled inside the dialog). */}
      {selectedPRs.length > 0 && projectCount === 1 && (
        <CreateReleaseDialog
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          // We've asserted projectCount===1 above, so .project_id
          // on the first PR is the canonical project for this
          // dialog session.
          projectId={selectedPRs[0]?.project_id ?? ""}
          selectedPullRequests={selectedPRs}
        />
      )}
    </>
  );
}
