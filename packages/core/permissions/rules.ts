import type {
  Agent,
  Comment,
  Member,
  MemberRole,
  RuntimeDevice,
  Skill,
} from "../types";
import { ALLOW, deny, type Decision, type PermissionContext } from "./types";
import type {
  GovernanceAction,
  GovernanceDecision,
  GovernanceDomain,
} from "./types";

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

// ---- Governance ------------------------------------------------------------

export const governanceRoleTemplates = [
  {
    id: "management_team",
    name: "Management Team",
    description:
      "Coordinates cross-project governance, organization rules, and operational experiments.",
    domains: [
      "workspace",
      "project",
      "agent",
      "autopilot",
      "skill_squad",
      "issue_metadata",
    ] satisfies GovernanceDomain[],
  },
  {
    id: "hr_executive",
    name: "HR Executive",
    description:
      "Creates, updates, archives, restores, and evaluates agent roles within approved boundaries.",
    domains: ["agent", "skill_squad", "issue_metadata"] satisfies GovernanceDomain[],
  },
  {
    id: "process_expert",
    name: "Process Expert",
    description:
      "Improves workflow, issue hygiene, project rules, and autopilot operating cadence.",
    domains: ["project", "autopilot", "issue_metadata"] satisfies GovernanceDomain[],
  },
  {
    id: "prompt_engineer",
    name: "Prompt Engineer",
    description:
      "Updates non-sensitive agent descriptions, instructions, and workflow prompts.",
    domains: ["agent", "skill_squad", "issue_metadata"] satisfies GovernanceDomain[],
  },
  {
    id: "supervisor_audit",
    name: "Supervisor / Audit",
    description:
      "Reviews governance decisions, approvals, and audit trails without expanding permissions.",
    domains: [
      "workspace",
      "project",
      "agent",
      "autopilot",
      "skill_squad",
      "issue_metadata",
    ] satisfies GovernanceDomain[],
  },
] as const;

export const governanceActions: GovernanceAction[] = [
  {
    id: "issue.status.remediate",
    domain: "issue_metadata",
    strategy: "automatic",
    description:
      "Repair issue status, labels, assignee, or metadata when the requested change is reversible and non-sensitive.",
    audit: false,
  },
  {
    id: "issue.metadata.update_non_sensitive",
    domain: "issue_metadata",
    strategy: "automatic",
    description:
      "Update non-sensitive issue metadata that documents PRs, blockers, deploy URLs, or current decisions.",
    audit: false,
  },
  {
    id: "project.description.update",
    domain: "project",
    strategy: "automatic",
    description:
      "Update non-sensitive project descriptions, resources, or operating notes.",
    audit: false,
  },
  {
    id: "autopilot.pause_resume",
    domain: "autopilot",
    strategy: "automatic",
    description:
      "Pause or resume an existing autopilot without changing triggers, webhooks, or secrets.",
    audit: true,
  },
  {
    id: "agent.description.update",
    domain: "agent",
    strategy: "automatic",
    description:
      "Update non-sensitive agent name, description, instructions, or prompt guidance.",
    audit: true,
  },
  {
    id: "agent.create",
    domain: "agent",
    strategy: "approval_required",
    description: "Create a new agent or digital employee.",
    audit: true,
  },
  {
    id: "agent.archive_restore",
    domain: "agent",
    strategy: "approval_required",
    description: "Archive or restore an agent.",
    audit: true,
  },
  {
    id: "agent.env.update",
    domain: "agent",
    strategy: "approval_required",
    description:
      "Update custom environment variables or secret-like agent configuration.",
    audit: true,
  },
  {
    id: "agent.permissions.update",
    domain: "agent",
    strategy: "approval_required",
    description: "Change an agent's workspace/project authorization boundary.",
    audit: true,
  },
  {
    id: "autopilot.delete",
    domain: "autopilot",
    strategy: "approval_required",
    description: "Delete an autopilot or remove a trigger.",
    audit: true,
  },
  {
    id: "autopilot.webhook.rotate",
    domain: "autopilot",
    strategy: "approval_required",
    description: "Rotate webhook URLs, tokens, or signing secrets.",
    audit: true,
  },
  {
    id: "workspace.rules.update_cross_project",
    domain: "workspace",
    strategy: "approval_required",
    description:
      "Update workspace-level rules, prompt runtime context, or cross-project governance defaults.",
    audit: true,
  },
  {
    id: "workspace.metadata.update",
    domain: "workspace",
    strategy: "approval_required",
    description:
      "Update workspace metadata, context, or organization-wide settings.",
    audit: true,
  },
  {
    id: "project.delete",
    domain: "project",
    strategy: "approval_required",
    description: "Delete a project or destructive project-scoped configuration.",
    audit: true,
  },
  {
    id: "squad.delete",
    domain: "skill_squad",
    strategy: "approval_required",
    description: "Delete a squad or remove its governance ownership.",
    audit: true,
  },
  {
    id: "skill.delete",
    domain: "skill_squad",
    strategy: "approval_required",
    description: "Delete a skill used by agents or workflow automation.",
    audit: true,
  },
  {
    id: "workspace.owner_grant",
    domain: "workspace",
    strategy: "denied",
    description:
      "Grant workspace owner/admin authority to the actor itself or bypass owner approval.",
    audit: true,
  },
  {
    id: "secret.reveal",
    domain: "workspace",
    strategy: "denied",
    description:
      "Reveal, post, or exfiltrate secret values, tokens, private keys, or credentials.",
    audit: true,
  },
  {
    id: "production.deploy",
    domain: "workspace",
    strategy: "denied",
    description:
      "Deploy, publish, upload firmware, charge billing, or mutate production external systems without human control.",
    audit: true,
  },
];

export function evaluateGovernanceAction(
  action: GovernanceAction,
  ctx: PermissionContext & { approved?: boolean },
): GovernanceDecision {
  const decision: GovernanceDecision = {
    action_id: action.id,
    domain: action.domain,
    strategy: action.strategy,
    allowed: false,
    requires_approval: false,
    reason: "",
    audit: action.audit,
  };

  if (action.strategy === "automatic") {
    if (ctx.role !== null) {
      return {
        ...decision,
        allowed: true,
        reason: "automatic_action_allowed",
      };
    }
    return { ...decision, strategy: "denied", reason: "not_workspace_member" };
  }

  if (action.strategy === "approval_required") {
    if (!isAdminLike(ctx.role)) {
      return {
        ...decision,
        requires_approval: true,
        reason: "requires_workspace_admin_or_owner",
      };
    }
    if (ctx.approved !== true) {
      return {
        ...decision,
        requires_approval: true,
        reason: "approval_required",
      };
    }
    return {
      ...decision,
      allowed: true,
      reason: "approved_action_allowed",
    };
  }

  return { ...decision, reason: "hard_boundary" };
}

export function evaluateGovernanceActions(
  ctx: PermissionContext & { approved?: boolean },
): GovernanceDecision[] {
  return governanceActions
    .map((action) => evaluateGovernanceAction(action, ctx))
    .sort((a, b) =>
      a.domain === b.domain
        ? a.action_id.localeCompare(b.action_id)
        : a.domain.localeCompare(b.domain),
    );
}
