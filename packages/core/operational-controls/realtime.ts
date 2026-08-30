import type { QueryClient } from "@tanstack/react-query";
import { operationalControlKeys } from "./queries";
import { OperationalControlsChangedPayloadSchema } from "./schemas";

export function handleOperationalControlsChanged(
  queryClient: QueryClient,
  payload: unknown,
  expectedWorkspaceId?: string,
): boolean {
  const parsed = OperationalControlsChangedPayloadSchema.safeParse(payload);
  if (!parsed.success) return false;
  if (
    expectedWorkspaceId !== undefined &&
    parsed.data.workspace_id !== expectedWorkspaceId
  ) {
    return false;
  }

  void queryClient.invalidateQueries({
    queryKey: operationalControlKeys.all(parsed.data.workspace_id),
  });
  return true;
}

export function evictProtectedOperationalControlCaches(
  queryClient: QueryClient,
  workspaceId: string,
): void {
  queryClient.removeQueries({
    queryKey: operationalControlKeys.all(workspaceId),
  });
}

export function handleCurrentMemberRoleUpdated(
  queryClient: QueryClient,
  update: {
    workspaceId: string;
    currentUserId: string | undefined;
    member: { user_id: string; role: string };
  },
): boolean {
  if (!update.currentUserId || update.member.user_id !== update.currentUserId) {
    return false;
  }
  if (update.member.role === "owner" || update.member.role === "admin") {
    return false;
  }

  evictProtectedOperationalControlCaches(queryClient, update.workspaceId);
  return true;
}
