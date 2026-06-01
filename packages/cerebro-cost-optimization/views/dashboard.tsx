"use client";

import { Badge } from "@multica/ui/components/ui/badge";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  COST_SAVINGS,
  type CostSavingDefinition,
  runtimeScopeBadge,
} from "../registry";
import {
  appliedValue,
  estimatedValue,
  formatUsd,
  type DashboardSaving,
} from "../dashboard";
import { useCostOptimizationDashboardQuery } from "./api";

/**
 * One stat: a value with a label underneath. Mirrors the compact stat layout
 * used across the settings cards so the dashboard reads like the rest of the UI.
 */
function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="space-y-0.5">
      <p className="text-base font-semibold tabular-nums">{value}</p>
      <p className="text-xs text-muted-foreground">{label}</p>
    </div>
  );
}

function SavingCard({
  def,
  saving,
}: {
  def: CostSavingDefinition;
  saving: DashboardSaving | undefined;
}) {
  const measuredRuns = saving
    ? saving.treatmentRunCount + saving.controlRunCount
    : 0;
  const totalRuns = saving ? measuredRuns + saving.shadowRunCount : 0;
  // "Estimated" only means something when there are non-applied runs to estimate
  // from (measure-only or held-out control). At 100% On with no holdout there are
  // none, so show "—" there instead of a misleading $0.00 next to the real saving.
  const hasEstimate = saving
    ? saving.shadowRunCount > 0 || saving.controlRunCount > 0
    : false;
  const scopeBadge = runtimeScopeBadge(def.runtimeScope);

  return (
    <Card>
      <CardContent className="space-y-3">
        <div className="space-y-0.5">
          <div className="flex items-center gap-2">
            <p className="text-sm font-medium">{def.label}</p>
            {scopeBadge && <Badge variant="secondary">{scopeBadge}</Badge>}
          </div>
          <p className="text-xs text-muted-foreground">{def.description}</p>
        </div>

        {!saving || totalRuns === 0 ? (
          <p className="text-xs text-muted-foreground italic">
            No measured runs yet — turn this saving to Measure only or On to start
            collecting data.
          </p>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-4">
              <Stat label="Measured saved (On)" value={appliedValue(saving)} />
              <Stat
                label="Estimated would-save"
                value={hasEstimate ? estimatedValue(saving) : "—"}
              />
            </div>

            {saving.measured && (
              <p className="text-xs text-muted-foreground">
                Holdout A/B: {formatUsd(saving.measured.totalSavedCents)} total ·{" "}
                {formatUsd(saving.measured.savedPerRunCents)} per run ·{" "}
                {formatUsd(saving.measured.treatmentAvgCostCents)} with saving vs{" "}
                {formatUsd(saving.measured.controlAvgCostCents)} held out
              </p>
            )}

            <p className="text-xs text-muted-foreground">
              {saving.treatmentRunCount} on · {saving.controlRunCount} held out ·{" "}
              {saving.shadowRunCount} measure-only
            </p>
          </>
        )}
      </CardContent>
    </Card>
  );
}

/**
 * Per-saving dashboard. "Measured saved (On)" is the real saving accrued on runs
 * that applied the saving, calibrated per run from each run's own usage — so it
 * shows a real number even at 100% rollout with no control arm. "Estimated
 * would-save" is the hypothetical for runs where the saving was not applied
 * (measure-only or held-out). When a holdout fraction is configured, the more
 * rigorous control-vs-treatment A/B is shown underneath.
 */
export function CostOptimizationDashboard() {
  const query = useCostOptimizationDashboardQuery();
  const byKey = new Map(query.data?.map((s) => [s.savingKey, s]) ?? []);

  return (
    <section className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-sm font-semibold">Savings dashboard</h2>
        <p className="text-xs text-muted-foreground">
          Measured saved (On) is what runs actually saved by applying the saving —
          a real figure even at 100% rollout. Estimated would-save is the
          hypothetical for runs that did not apply it. When a holdout is
          configured, a control-vs-treatment A/B is shown too. All amounts in US
          dollars.
        </p>
      </div>

      {query.isError && (
        <p className="text-xs text-destructive">
          Failed to load savings dashboard:{" "}
          {query.error instanceof Error ? query.error.message : "unknown error"}
        </p>
      )}

      <div className="space-y-3">
        {COST_SAVINGS.map((def) => (
          <SavingCard key={def.key} def={def} saving={byKey.get(def.key)} />
        ))}
      </div>
    </section>
  );
}
