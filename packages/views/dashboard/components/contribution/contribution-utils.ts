import type { ContributionActorStats, ContributionBucket } from "@multica/core/types";
import { addDaysIso, todayIso, weekStartIso } from "../../../runtimes/utils";

export const STATUS_GROUPS = [
  "todo",
  "in_progress",
  "in_review",
  "done",
  "cancelled",
] as const;
export type StatusGroup = (typeof STATUS_GROUPS)[number];

export function isStatusGroup(value: string): value is StatusGroup {
  return (STATUS_GROUPS as readonly string[]).includes(value);
}

/** Series colours. People and agents differ in lightness as well as hue so the
 *  two stay apart for colour-blind readers. */
export const STATUS_COLOR: Record<StatusGroup, string> = {
  todo: "var(--muted-foreground)",
  in_progress: "var(--warning)",
  in_review: "var(--success)",
  done: "var(--brand)",
  cancelled: "var(--border)",
};

export const ACTOR_COLOR = {
  member: "var(--chart-1)",
  agent: "var(--warning)",
} as const;

export type ActorKind = "member" | "agent";

/** Splits the leaderboard rows into the two separately ranked groups. */
export function splitActors(actors: readonly ContributionActorStats[]): {
  members: ContributionActorStats[];
  agents: ContributionActorStats[];
} {
  const members: ContributionActorStats[] = [];
  const agents: ContributionActorStats[] = [];
  for (const a of actors) {
    if (a.actor_type === "member") members.push(a);
    else if (a.actor_type === "agent") agents.push(a);
  }
  return { members, agents };
}

export function actorKey(type: string, id: string): string {
  return `${type}:${id}`;
}

export interface HeatmapCell {
  date: string;
  count: number;
  /** 0 = no activity, 1–4 = quartiles of the busiest day. */
  level: number;
  /** False for days outside the fetched window (before it, or after today). */
  inRange: boolean;
}

/**
 * A Monday-first calendar of `weeks` columns ending with the current week.
 * Days the summary does not cover are marked out of range rather than zero, so
 * a 30-day filter does not paint five months of empty-looking days.
 */
export function buildHeatmap(
  daily: readonly ContributionBucket[],
  tz: string,
  weeks = 26,
): HeatmapCell[][] {
  const byDate = new Map(daily.map((d) => [d.date, d.active]));
  const max = daily.reduce((m, d) => Math.max(m, d.active), 0);
  const today = todayIso(tz);
  const firstMonday = addDaysIso(weekStartIso(today), -(weeks - 1) * 7);
  const columns: HeatmapCell[][] = [];
  for (let w = 0; w < weeks; w++) {
    const column: HeatmapCell[] = [];
    for (let d = 0; d < 7; d++) {
      const date = addDaysIso(firstMonday, w * 7 + d);
      const count = byDate.get(date) ?? 0;
      const inRange = byDate.has(date) && date <= today;
      const level =
        !inRange || count === 0 || max === 0
          ? 0
          : Math.min(4, Math.max(1, Math.ceil((count / max) * 4)));
      column.push({ date, count, level, inRange });
    }
    columns.push(column);
  }
  return columns;
}

/** Percent of `part` in `total`, safe for an empty total. */
export function pct(part: number, total: number): number {
  return total > 0 ? (part / total) * 100 : 0;
}
