import type { AgentRuntime, MemberWithUser } from "@multica/core/types";

export interface RuntimeOwnerFallbackLabels {
  unknownOwner: string;
  workspaceRuntime: string;
}

export const DEFAULT_RUNTIME_OWNER_FALLBACK_LABELS: RuntimeOwnerFallbackLabels =
  {
    unknownOwner: "Unknown member",
    workspaceRuntime: "Workspace runtime",
  };

export function runtimeOwnerDisplayName(
  runtime: Pick<AgentRuntime, "owner_id">,
  ownerMember: Pick<MemberWithUser, "name"> | null,
  fallbackLabels: RuntimeOwnerFallbackLabels = DEFAULT_RUNTIME_OWNER_FALLBACK_LABELS,
): string {
  const memberName = ownerMember?.name.trim();
  if (memberName) return memberName;
  return runtime.owner_id
    ? fallbackLabels.unknownOwner
    : fallbackLabels.workspaceRuntime;
}
