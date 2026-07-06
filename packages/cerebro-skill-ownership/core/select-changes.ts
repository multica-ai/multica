// FIR-2742 — cross-skill change-request review. The per-skill change-request
// queue (skill detail page) answers "changes on THIS skill". This module powers
// the workspace-wide view an owner/approver needs: "every pending change waiting
// on a skill I own or approve", filterable and sortable so they can be handled
// one at a time from the Skills page + inbox. Pure functions only, so the
// filtering/sorting is unit-tested without React.

import type { SkillChangeRequest, SkillSummary } from "@multica/core/types";

/**
 * A pending change request joined to the skill it targets. The pending queue
 * endpoint returns bare change requests (no skill name/owner), so we enrich
 * each one from the workspace skill list the page already has cached.
 */
export interface EnrichedSkillChange extends SkillChangeRequest {
  skill_name: string;
  skill_owner_id: string | null;
  /** The skill's live version — the "from" side of the proposed bump. */
  current_version: string;
  /** True when the current user owns or approves the target skill. */
  mine: boolean;
}

export type SkillChangeScope = "mine" | "all";
export type SkillChangeSort = "newest" | "oldest";

/**
 * Join each change request to its skill and mark whether the current user is a
 * reviewer (owner or approver) of that skill. Change requests whose skill is
 * not in `skills` (not visible to this user) are dropped.
 */
export function enrichSkillChanges(
  changes: SkillChangeRequest[],
  skills: SkillSummary[],
  userId: string | null,
): EnrichedSkillChange[] {
  const byId = new Map<string, SkillSummary>();
  for (const s of skills) byId.set(s.id, s);

  const out: EnrichedSkillChange[] = [];
  for (const cr of changes) {
    const skill = byId.get(cr.skill_id);
    if (!skill) continue;
    const mine = !!(
      userId &&
      (skill.owner_id === userId ||
        (skill.approver_ids ?? []).includes(userId))
    );
    out.push({
      ...cr,
      skill_name: skill.name,
      skill_owner_id: skill.owner_id,
      current_version: skill.current_version,
      mine,
    });
  }
  return out;
}

/** Filter by review scope (mine = skills I own/approve) and a free-text query. */
export function filterSkillChanges(
  changes: EnrichedSkillChange[],
  opts: { scope: SkillChangeScope; search: string },
): EnrichedSkillChange[] {
  const q = opts.search.trim().toLowerCase();
  return changes.filter((c) => {
    if (opts.scope === "mine" && !c.mine) return false;
    if (
      q &&
      !c.skill_name.toLowerCase().includes(q) &&
      !c.title.toLowerCase().includes(q)
    ) {
      return false;
    }
    return true;
  });
}

/** Sort by creation time. Newest first is the default review order. */
export function sortSkillChanges(
  changes: EnrichedSkillChange[],
  sort: SkillChangeSort,
): EnrichedSkillChange[] {
  const dir = sort === "newest" ? -1 : 1;
  return [...changes].sort(
    (a, b) =>
      dir *
      (new Date(a.created_at).getTime() - new Date(b.created_at).getTime()),
  );
}

/** Convenience: enrich, keep only the current user's, for the alert count. */
export function selectMyPendingChanges(
  changes: SkillChangeRequest[],
  skills: SkillSummary[],
  userId: string | null,
): EnrichedSkillChange[] {
  return enrichSkillChanges(changes, skills, userId).filter((c) => c.mine);
}
