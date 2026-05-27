import type { Agent } from "@multica/core/types";
import type { Decision, PermissionContext } from "@multica/core/permissions";

/**
 * canTriggerPrivateAgentMention — whether an @mention of `agent` by the current
 * user will actually WAKE it. Mirrors the Go gate `canTriggerPrivateAgent` in
 * `server/internal/handler/agent_access.go` (FIR-2349): a private agent is
 * triggered only by its owning member's sphere. Unlike `canAssignAgentToIssue`,
 * workspace admins get NO override here — a personal agent answers only to its
 * owner. Workspace-visibility agents are always triggerable by any member.
 *
 * The @mention picker and the rendered mention chip use this to tell a
 * non-owner that their tag will not notify the agent.
 */
export function canTriggerPrivateAgentMention(
  agent: Agent,
  ctx: PermissionContext,
): Decision {
  if (ctx.userId === null) {
    return {
      allowed: false,
      reason: "not_authenticated",
      message: "Sign in to mention agents.",
    };
  }
  if (agent.visibility !== "private") {
    if (ctx.role === null) {
      return {
        allowed: false,
        reason: "not_member",
        message: "Join this workspace to mention agents.",
      };
    }
    return { allowed: true, reason: "allowed", message: "" };
  }
  // visibility === "private" — owner only, no admin override.
  if (agent.owner_id !== null && agent.owner_id === ctx.userId) {
    return { allowed: true, reason: "allowed", message: "" };
  }
  return {
    allowed: false,
    reason: "private_visibility",
    message:
      "Personal agent — only the owner can trigger it. This mention won't notify it.",
  };
}
