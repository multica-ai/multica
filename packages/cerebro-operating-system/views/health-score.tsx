import type { HealthState } from "../core/types";

const styles: Record<HealthState, string> = {
  on_track: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  at_risk: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
  off_track: "border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300",
  unset: "border-muted-foreground/20 bg-muted text-muted-foreground",
  unknown: "border-muted-foreground/20 bg-muted text-muted-foreground",
};

const labels: Record<HealthState, string> = { on_track: "On track", at_risk: "At risk", off_track: "Off track", unset: "Not set", unknown: "Unknown" };

export function HealthBadge({ state }: { state: HealthState }) {
  return <span className={`inline-flex rounded-full border px-2.5 py-1 text-xs font-medium ${styles[state]}`}>{labels[state]}</span>;
}

export function HealthScore({ score, state }: { score: number; state: HealthState }) {
  return (
    <div className={`grid size-14 shrink-0 place-items-center rounded-full border-4 text-sm font-semibold ${styles[state]}`} aria-label={`Health score ${score}`}>
      {score}
    </div>
  );
}
