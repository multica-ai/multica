import type { Agent, MemberRole } from "../types";

// Mirrors the server rule (requireDocDeletion). The server stays
// authoritative; this only decides whether delete is offered. Docs and
// folders share the rule: owners and admins delete anything, members delete
// what they or an agent they own created. Guests have no docs at all.
export function canDeleteDoc(
  item: { createdByType: "member" | "agent"; createdById: string },
  userId: string | null,
  role: MemberRole | null,
  agents: readonly Pick<Agent, "id" | "owner_id">[],
): boolean {
  if (!userId || !role || role === "guest") return false;
  if (role === "owner" || role === "admin") return true;
  if (item.createdByType === "member") return item.createdById === userId;
  return agents.some(
    (agent) => agent.id === item.createdById && agent.owner_id === userId,
  );
}
