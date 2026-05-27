"use client";

import { Badge } from "@multica/ui/components/ui/badge";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  COST_SAVINGS,
  type CostSavingDefinition,
  runtimeScopeBadge,
} from "../registry";
import {
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
              <Stat label="Estimated would-save" value={estimatedValue(saving)} />
              <Stat
                label="Measured (holdout A/B)"
                value={
                  saving.measured
                    ? formatUsd(saving.measured.totalSavedCents)
                    : "—"
                }
              />
            </div>

            {saving.measured && (
              <p className="text-xs text-muted-foreground">
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
 * Per-saving dashboard: estimated would-save vs. measured actually-saved. The
 * measured number is the random-holdout A/B (control runs with the saving
 * withheld vs. treatment runs with it applied) and only appears for model
 * routing, where a run's real cost reflects the saving. The estimate is the
 * shadow/hypothetical figure and is always shown.
 */
export function CostOptimizationDashboard() {
  const query = useCostOptimizationDashboardQuery();
  const byKey = new Map(query.data?.map((s) => [s.savingKey, s]) ?? []);

  return (
    <section className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-sm font-semibold">Savings dashboard</h2>
        <p className="text-xs text-muted-foreground">
          Estimated would-save is the per-run hypothetical. Measured (holdout A/B)
          compares a random share of On runs that ran without the saving (control)
          against the runs that applied it. All amounts in US dollars.
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
