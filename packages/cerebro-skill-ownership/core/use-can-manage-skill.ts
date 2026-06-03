"use client";

import type { MemberRole, SkillSummary } from "@multica/core/types";
import { useCurrentMember } from "@multica/core/permissions";

/**
 * Whether the current user may manage ownership / review change requests on the
 * given skill. Mirrors the backend `isSkillManager` (server/internal/handler/
 * skill.go) so the UI hides manage actions instead of waiting for a 403:
 *
 *   - workspace owner or admin → always
 *   - the skill's owner → yes
 *   - any of the skill's approvers → yes
 *   - legacy fallback: the creator when no owner has been set yet
 *
 * Anyone else may still open a change request, but cannot review or reassign
 * ownership. `wsId` is explicit per the repo rule for workspace-aware hooks.
 */
export function useCanManageSkill(
  skill: SkillSummary | null | undefined,
  wsId: string,
): boolean {
  const { userId, role } = useCurrentMember(wsId);
  return canManageSkill(skill, { userId, role });
}

export function canManageSkill(
  skill: SkillSummary | null | undefined,
  opts: { userId: string | null; role: MemberRole | null },
): boolean {
  if (!skill) return false;
  if (opts.role === "owner" || opts.role === "admin") return true;
  if (opts.userId == null) return false;
  if (skill.owner_id && skill.owner_id === opts.userId) return true;
  if (skill.approver_ids.includes(opts.userId)) return true;
  if (!skill.owner_id && skill.created_by === opts.userId) return true;
  return false;
}
