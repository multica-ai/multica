import type { Agent, MemberRole, Space } from "../types";

/**
 * Concrete Space contexts the current member may use with an Agent in Chat.
 * Mirrors the backend intersection: member collaboration × active Space ×
 * Agent Availability. An empty result means the Agent cannot be chatted with.
 */
export function chatSpacesForAgent(
  agent: Agent,
  spaces: readonly Space[],
  userId: string | null | undefined,
  memberRole: MemberRole | null | undefined,
): Space[] {
  const workspaceAdmin = memberRole === "owner" || memberRole === "admin";
  const collaborative = spaces.filter(
    (space) =>
      !space.archived_at &&
      (workspaceAdmin ||
        (space.is_member &&
          (space.member_role === "lead" ||
            space.member_role === "admin" ||
            space.member_role === "member"))),
  );

  switch (agent.availability_mode) {
    case "selected_spaces": {
      const selected = new Set(agent.availability_space_ids ?? []);
      return collaborative.filter((space) => selected.has(space.id));
    }
    case "private":
      return agent.owner_id === userId ? collaborative : [];
    case "workspace":
      return collaborative;
    default:
      // Older servers omitted Availability. Preserve their workspace-wide
      // behavior during the API compatibility window.
      return collaborative;
  }
}

/** One available Space is clearer by name; two or more default to All spaces. */
export function defaultChatSpaceId(spaces: readonly Space[]): string | null {
  return spaces.length === 1 ? spaces[0]!.id : null;
}
