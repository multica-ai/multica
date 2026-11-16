"use client";

import type { Agent, Skill } from "../types";
import { useCurrentMember } from "./use-current-member";
import {
  canAssignAgentToIssue,
  canDeleteSkill,
  canEditAgent,
  canEditSkill,
} from "./rules";
import { deny, type Decision } from "./types";

const PENDING: Decision = deny("unknown", "");

/**
 * Per-resource hook that returns a `Decision` for every relevant capability.
 * Each hook calls `useCurrentMember()` once and threads the context into the
 * pure rules in `rules.ts`.
 *
 * `wsId` is explicit (not read from `WorkspaceIdProvider`) so the hook stays
 * usable outside a workspace context — matches the repo rule for
 * workspace-aware hooks.
 *
 * Resource = `null` collapses every Decision to a denied "unknown" — keeps
 * callers branch-free during loading.
 *
 * `canArchive` / `canRestore` / `canManage` are deliberately not exposed:
 * the backend gates them identically to `canEdit`, so callers can use
 * `canEdit` everywhere and read better at the call site.
 */
export function useAgentPermissions(
  agent: Agent | null,
  wsId: string,
): {
  canEdit: Decision;
  canAssign: Decision;
  isLoading: boolean;
} {
  const { userId, role, isLoading } = useCurrentMember(wsId);
  const ctx = { userId, role };
  // While the member query is in flight, `role` is null and the rules below
  // would misread a legitimate member as denied (e.g. `not_member` for a
  // public_to+workspace agent). Callers with always-clickable affordances
  // must treat `isLoading` as "undetermined", not as a deny.
  if (agent === null) {
    return { canEdit: PENDING, canAssign: PENDING, isLoading };
  }
  return {
    canEdit: canEditAgent(agent, ctx),
    canAssign: canAssignAgentToIssue(agent, ctx),
    isLoading,
  };
}

export function useSkillPermissions(
  skill: Skill | null,
  wsId: string,
): {
  canEdit: Decision;
  canDelete: Decision;
} {
  const { userId, role } = useCurrentMember(wsId);
  const ctx = { userId, role };
  if (skill === null) {
    return { canEdit: PENDING, canDelete: PENDING };
  }
  return {
    canEdit: canEditSkill(skill, ctx),
    canDelete: canDeleteSkill(skill, ctx),
  };
}
