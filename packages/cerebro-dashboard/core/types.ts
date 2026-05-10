export type ActorScope = "all" | "members" | "agents";

export type { TimeRange } from "./api";
import type { TimeRange } from "./api";

export interface DashboardFilter {
  scope: ActorScope;
  /** Optional specific actor id (member or agent) within the scope. */
  actorId: string | null;
  actorName: string | null;
  range: TimeRange;
}

export const DEFAULT_FILTER: DashboardFilter = {
  scope: "all",
  actorId: null,
  actorName: null,
  range: "7d",
};

export function timeRangeToDays(range: TimeRange): number {
  switch (range) {
    case "24h":
      return 1;
    case "7d":
      return 7;
    case "30d":
      return 30;
  }
}
