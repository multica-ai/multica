"use client";

import { useQuery } from "@tanstack/react-query";
import type { MemberRole, SkillSummary } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";

/**
 * Whether the current user may edit/delete the given skill.
 *
 * Rule: workspace admins & owners can edit any skill; the skill's owner and its
 * approvers can edit directly; for legacy skills with no owner set, the creator
 * can still edit. Everyone else is read-only and proposes a change request
 * instead. Server enforces this independently (`isSkillManager`); the hook
 * mirrors it so the UI can hide/disable actions instead of waiting for a 403.
 *
 * `wsId` is explicit (not read from `WorkspaceIdProvider`) so this hook stays
 * usable in components that render before workspace context is wired, and so
 * the scope of the permission check is always obvious to the caller. Matches
 * the repo rule for workspace-aware hooks.
 */
export function useCanEditSkill(
  skill: SkillSummary | null | undefined,
  wsId: string,
): boolean {
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  if (!skill) return false;
  const myRole = members.find((m) => m.user_id === userId)?.role ?? null;
  return canEditSkill(skill, { userId, role: myRole });
}

/**
 * Non-hook variant for places that already have the role + userId at hand
 * (e.g. list rows that compute role once for the whole page).
 */
export function canEditSkill(
  skill: SkillSummary,
  opts: { userId: string | null; role: MemberRole | null },
): boolean {
  if (opts.role === "admin" || opts.role === "owner") return true;
  if (opts.userId === null) return false;
  // CEREBRO-PATCH(skill-ownership-edit-rights): JEH-216/FIR-2629 — owner + approvers edit directly; creator only when no owner is set yet. Mirrors backend isSkillManager.
  if (skill.owner_id && skill.owner_id === opts.userId) return true;
  if (skill.approver_ids.includes(opts.userId)) return true;
  if (!skill.owner_id && skill.created_by === opts.userId) return true;
  return false;
}
