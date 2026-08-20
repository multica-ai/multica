import type { LiteLlmDayResult } from "./litellm-schema";
import type { LiteLlmUsage } from "./litellm";

/**
 * Pure aggregation logic, split out of litellm.ts's getTeamUsage so it's
 * testable without pulling in `server-only`. Each model entry may carry its
 * numbers under `metrics` (the real /team/daily/activity shape) or at the
 * top level — fall back the same way ai-spend-dashboard's fetchRows does
 * (`info?.metrics ?? info`), since LiteLLM has shipped both shapes.
 */
export function aggregateTeamUsage(days: LiteLlmDayResult[], todayISO: string): LiteLlmUsage | null {
  let cost24h = 0;
  let cost30d = 0;
  let tokens24h = 0;
  let sawAny = false;

  for (const day of days) {
    const date = day.date || day.group_by_day;
    const models = day.breakdown?.models ?? {};
    for (const info of Object.values(models)) {
      const metrics = info?.metrics ?? info ?? {};
      const spend = Number(metrics.spend ?? 0);
      const tokens = Number(metrics.total_tokens ?? 0);
      if (!spend && !tokens) continue;
      sawAny = true;
      cost30d += spend;
      if (date === todayISO) {
        cost24h += spend;
        tokens24h += tokens;
      }
    }
  }

  if (!sawAny) return null;
  return { cost24h, cost30d, tokens24h };
}
