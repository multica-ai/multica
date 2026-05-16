import type { Release } from "@multica/core/types";

/** Returns the most significant deployment timestamp for a release:
 *  production deploy (promoted_at) > staging deploy (staged_at) > creation. */
export function releaseDeployedAt(
  r: Pick<Release, "promoted_at" | "staged_at" | "created_at">,
): string {
  return r.promoted_at ?? r.staged_at ?? r.created_at;
}

/** Compact absolute timestamp: "May 9, 3:42 PM". Drops the year for the
 *  current year since active releases are always recent. */
export function formatDeployedAt(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (!Number.isFinite(d.getTime())) return "";
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    year: d.getFullYear() !== new Date().getFullYear() ? "numeric" : undefined,
    hour: "numeric",
    minute: "2-digit",
  });
}

/** Returns Tailwind classes for the stage badge background + text.
 *  Mirrors the progress-bar icon colors on the release detail page. */
export function releaseStageColorClass(stage: string): string {
  switch (stage) {
    case "merging":
      return "bg-amber-500/20 text-amber-700 dark:text-amber-400";
    case "in_staging":
      return "bg-blue-500/20 text-blue-700 dark:text-blue-400";
    case "verifying":
      return "bg-purple-500/20 text-purple-700 dark:text-purple-400";
    case "promoting":
      return "bg-orange-500/20 text-orange-700 dark:text-orange-400";
    case "in_production":
      return "bg-emerald-500/20 text-emerald-700 dark:text-emerald-400";
    case "done":
      return "bg-emerald-500/20 text-emerald-700 dark:text-emerald-400";
    case "rolled_back":
      return "bg-destructive/20 text-destructive";
    default:
      return "bg-muted text-muted-foreground";
  }
}
