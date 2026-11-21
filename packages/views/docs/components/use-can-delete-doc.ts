import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { canDeleteDoc } from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";

type Deletable = Parameters<typeof canDeleteDoc>[0];

// Whether the signed-in member may delete a doc or folder. Hides the delete
// action only; the server enforces the same rule.
export function useCanDeleteDoc(): (item: Deletable) => boolean {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const role = members.find((m) => m.user_id === userId)?.role ?? null;
  return (item) => canDeleteDoc(item, userId, role, agents);
}
