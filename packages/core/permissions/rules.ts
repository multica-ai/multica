import type {
  Agent,
  Comment,
  Member,
  MemberRole,
  RuntimeDevice,
  Skill,
} from "../types";
import { ALLOW, deny, type Decision, type PermissionContext } from "./types";

/**
 * Pure permission rules — single source of truth that mirrors the Go backend
 * gates in `server/internal/handler/`. Hooks in `use-resource-permissions.ts`
 * are thin wrappers that pull `PermissionContext` from auth + member queries
 * and forward to these.
 *
 * Returning a `Decision` (not a boolean) lets every surface — disabled state,
 * tooltip, banner copy — read the same `reason` and stay consistent without
 * sprinkling copy through the view layer.
 */

const isAdminLike = (role: MemberRole | null) =>
  role === "owner" || role === "admin";

// ---- Agents ----------------------------------------------------------------

/**
 * Update / archive / restore agent fields. The backend gates archive and
 * restore identically to edit (`server/internal/handler/agent.go:519-535`),
 * so callers can use `canEditAgent` for all three.
 */
export function canEditAgent(agent: Agent, ctx: PermissionContext): Decision {
  if (ctx.userId === null) {
    return deny("not_authenticated", "Sign in to edit this agent.");
  }
  if (isAdminLike(ctx.role)) return ALLOW;
  if (agent.owner_id !== null && agent.owner_id === ctx.userId) return ALLOW;
  return deny(
    "not_resource_owner",
    "Only the agent owner and workspace admins can edit this agent.",
  );
}

/**
 * Assign an agent to an issue. Workspace-visibility agents are assignable by
 * any workspace member; private agents are restricted to their owner plus
 * workspace admins/owners. Mirrors `issue.go:1471-1490`.
 */
export function canAssignAgentToIssue(
  agent: Agent,
  ctx: PermissionContext,
): Decision {
  if (ctx.userId === null) {
    return deny("not_authenticated", "Sign in to assign agents.");
  }
  if (agent.visibility === "workspace") {
    if (ctx.role === null) {
      return deny("not_member", "Join this workspace to assign agents.");
    }
    return ALLOW;
  }
  // visibility === "private"
  if (isAdminLike(ctx.role)) return ALLOW;
  if (agent.owner_id !== null && agent.owner_id === ctx.userId) return ALLOW;
  return deny(
    "private_visibility",
    "Personal agent — only the owner and workspace admins can assign work.",
  );
}

// ---- Skills ----------------------------------------------------------------

export function canEditSkill(skill: Skill, ctx: PermissionContext): Decision {
  if (ctx.userId === null) {
    return deny("not_authenticated", "Sign in to edit this skill.");
  }
  if (isAdminLike(ctx.role)) return ALLOW;
  if (skill.created_by !== null && skill.created_by === ctx.userId) {
    return ALLOW;
  }
  return deny(
    "not_resource_owner",
    "Only the creator and workspace admins can edit this skill.",
  );
}

export function canDeleteSkill(skill: Skill, ctx: PermissionContext): Decision {
  return canEditSkill(skill, ctx);
}

// ---- Comments --------------------------------------------------------------

export function canEditComment(
  comment: Comment,
  ctx: PermissionContext,
): Decision {
  if (ctx.userId === null) {
    return deny("not_authenticated", "Sign in to edit comments.");
  }
  // Only member-authored comments can be edited; agent-authored comments are
  // immutable from any human's perspective.
  if (comment.author_type !== "member") {
    return deny(
      "not_resource_owner",
      "Agent-authored comments cannot be edited.",
    );
  }
  if (comment.author_id === ctx.userId) return ALLOW;
  if (isAdminLike(ctx.role)) return ALLOW;
  return deny(
    "not_resource_owner",
    "Only the author and workspace admins can edit this comment.",
  );
}

export function canDeleteComment(
  comment: Comment,
  ctx: PermissionContext,
): Decision {
  if (ctx.userId === null) {
    return deny("not_authenticated", "Sign in to delete comments.");
  }
  if (comment.author_type === "member" && comment.author_id === ctx.userId) {
    return ALLOW;
  }
  if (isAdminLike(ctx.role)) return ALLOW;
  return deny(
    "not_resource_owner",
    "Only the author and workspace admins can delete this comment.",
  );
}

// ---- Runtimes --------------------------------------------------------------

export function canDeleteRuntime(
  runtime: RuntimeDevice,
  ctx: PermissionContext,
): Decision {
  if (ctx.userId === null) {
    return deny("not_authenticated", "Sign in to delete runtimes.");
  }
  if (isAdminLike(ctx.role)) return ALLOW;
  if (runtime.owner_id !== null && runtime.owner_id === ctx.userId) {
    return ALLOW;
  }
  return deny(
    "not_resource_owner",
    "Only the runtime owner and workspace admins can delete this runtime.",
  );
}

// ---- Workspace -------------------------------------------------------------

export function canUpdateWorkspaceSettings(ctx: PermissionContext): Decision {
  if (isAdminLike(ctx.role)) return ALLOW;
  return deny(
    "not_admin_role",
    "Only workspace owners and admins can update workspace settings.",
  );
}

export function canDeleteWorkspace(ctx: PermissionContext): Decision {
  if (ctx.role === "owner") return ALLOW;
  return deny(
    "not_owner_role",
    "Only the workspace owner can delete this workspace.",
  );
}

export function canManageMembers(ctx: PermissionContext): Decision {
  if (isAdminLike(ctx.role)) return ALLOW;
  return deny(
    "not_admin_role",
    "Only workspace owners and admins can manage members.",
  );
}

/**
 * Encodes the role-change matrix from `workspace.go:458-530`:
 *   - admins cannot touch the owner role (neither demote owners nor promote)
 *   - the last owner cannot be demoted
 *   - non-managers cannot change roles at all
 *
 * `ownerCount` is the number of workspace members currently with role=owner.
 * Caller derives it locally from the cached member list.
 */
export function canChangeMemberRole(
  target: Pick<Member, "role">,
  ownerCount: number,
  ctx: PermissionContext,
): Decision {
  const manage = canManageMembers(ctx);
  if (!manage.allowed) return manage;

  if (target.role === "owner") {
    if (ctx.role !== "owner") {
      return deny(
        "not_owner_role",
        "Only the workspace owner can change another owner's role.",
      );
    }
    if (ownerCount <= 1) {
      return deny(
        "last_owner",
        "Promote another member to owner first — a workspace must keep at least one owner.",
      );
    }
  }
  return ALLOW;
}
