import type { SquadMember } from "@multica/core/types";

/**
 * The runtime id every live agent shares, `""` when none is set, or `null` when
 * the squad's agents run on different runtimes.
 */
export function sharedSquadRuntimeId(
  members: readonly SquadMember[],
  agentRuntimeIdById: ReadonlyMap<string, string>,
  archivedAgentIds: ReadonlySet<string>,
): string | null {
  const ids = new Set(
    members
      .filter((m) => m.member_type === "agent" && !archivedAgentIds.has(m.member_id))
      .map((m) => agentRuntimeIdById.get(m.member_id) ?? ""),
  );
  if (ids.size > 1) return null;
  const [only = ""] = ids;
  return only;
}
