import type { Agent, AgentRuntime, MemberWithUser, SkillSummary } from "@multica/core/types";
import type { SkillListFilters } from "@multica/core/skills/stores";
import type { OriginInfo } from "../lib/origin";

export interface SkillRow {
  skill: SkillSummary;
  agents: Agent[];
  creator: MemberWithUser | null;
  runtime: AgentRuntime | null;
  originType: OriginInfo["type"];
  canEdit: boolean;
}

/**
 * Pure row-filter predicate: returns true if the row matches all active
 * filter dimensions plus name search. Empty arrays are inactive. Label
 * matching is OR-within-labels, matching Issue list semantics.
 */
export function rowMatchesFilters(
  row: SkillRow,
  filters: SkillListFilters,
  query: string,
): boolean {
  const q = query.trim().toLowerCase();
  if (q && !row.skill.name.toLowerCase().includes(q)) return false;
  if (filters.usage.length > 0) {
    const usage = row.agents.length > 0 ? "used" : "unused";
    if (!filters.usage.includes(usage)) return false;
  }
  if (
    filters.origins.length > 0 &&
    !filters.origins.includes(row.originType)
  ) {
    return false;
  }
  if (
    filters.agents.length > 0 &&
    !row.agents.some((a) => filters.agents.includes(a.id))
  ) {
    return false;
  }
  if (
    filters.creators.length > 0 &&
    (!row.skill.created_by ||
      !filters.creators.includes(row.skill.created_by))
  ) {
    return false;
  }
  if (filters.labels.length > 0) {
    const labels = row.skill.labels;
    if (!labels || labels.length === 0) return false;
    if (!labels.some((l) => filters.labels.includes(l.id))) return false;
  }
  return true;
}
