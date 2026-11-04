import type { Goal, GoalScopeType } from "@multica/core/governance";

/**
 * The goal tree the Goals tab draws: company goals first, then floors, squads and
 * initiatives in the order the workspace lists them. A goal whose scope object is
 * not in those lists (an archived squad, say) stays under its scope with no name,
 * rather than vanishing.
 */

export interface GoalTreeSection {
  scope: GoalScopeType;
  scopeId: string | null;
  /** The scope object's name, or null when it is not in the lists given. */
  name: string | null;
  goals: Goal[];
}

export interface GoalTreeNames {
  floors: { id: string; name: string }[];
  squads: { id: string; name: string }[];
  initiatives: { id: string; identifier: string; title: string }[];
}

const SCOPE_ORDER: GoalScopeType[] = ["company", "floor", "squad", "initiative"];

/** Groups goals into tree sections. Empty sections are left out. */
export function buildGoalTree(goals: Goal[], names: GoalTreeNames): GoalTreeSection[] {
  const sections: GoalTreeSection[] = [];
  const byKey = new Map<string, GoalTreeSection>();

  const section = (scope: GoalScopeType, scopeId: string | null, name: string | null) => {
    const key = `${scope}:${scopeId ?? ""}`;
    let found = byKey.get(key);
    if (!found) {
      found = { scope, scopeId, name, goals: [] };
      byKey.set(key, found);
      sections.push(found);
    }
    return found;
  };

  // The lists fix the order and the names; a goal's own scope may add a section.
  for (const goal of goals) {
    if (goal.scope_type === "company") section("company", null, null).goals.push(goal);
  }
  for (const floor of names.floors) {
    const goalsHere = goals.filter((g) => g.scope_type === "floor" && g.scope_id === floor.id);
    if (goalsHere.length) section("floor", floor.id, floor.name).goals.push(...goalsHere);
  }
  for (const squad of names.squads) {
    const goalsHere = goals.filter((g) => g.scope_type === "squad" && g.scope_id === squad.id);
    if (goalsHere.length) section("squad", squad.id, squad.name).goals.push(...goalsHere);
  }
  for (const ini of names.initiatives) {
    const goalsHere = goals.filter((g) => g.scope_type === "initiative" && g.scope_id === ini.id);
    if (goalsHere.length) section("initiative", ini.id, `${ini.identifier} ${ini.title}`.trim()).goals.push(...goalsHere);
  }
  // Goals whose scope object is not in the lists stay under their scope, unnamed.
  const listed = new Set([
    ...names.floors.map((f) => `floor:${f.id}`),
    ...names.squads.map((s) => `squad:${s.id}`),
    ...names.initiatives.map((i) => `initiative:${i.id}`),
  ]);
  for (const goal of goals) {
    if (goal.scope_type === "company") continue;
    const key = `${goal.scope_type}:${goal.scope_id ?? ""}`;
    if (listed.has(key)) continue;
    section(goal.scope_type, goal.scope_id, null).goals.push(goal);
  }

  return SCOPE_ORDER.flatMap((scope) => sections.filter((s) => s.scope === scope));
}

/** Progress as a whole percent for a bar and its label, or null when there is no reading. */
export function progressPercent(progress: number | null): number | null {
  if (progress == null || !Number.isFinite(progress)) return null;
  return Math.round(Math.min(1, Math.max(0, progress)) * 100);
}
