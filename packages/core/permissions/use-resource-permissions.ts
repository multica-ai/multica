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
} {
  const { userId, role } = useCurrentMember(wsId);
  const ctx = { userId, role };
  if (agent === null) {
    return { canEdit: PENDING, canAssign: PENDING };
  }
  return {
    canEdit: canEditAgent(agent, ctx),
    canAssign: canAssignAgentToIssue(agent, ctx),
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
