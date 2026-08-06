"use client";

// CEREBRO-PATCH(autopilot-permissions): consume the existing effective Permissions response for Autopilot controls (FIR-4359).

import { useQuery } from "@tanstack/react-query";
import { useCurrentMember } from "@multica/core/permissions";
import { toolPolicyTableOptions } from "@multica/cerebro-tool-policy/core";
import type { ToolPolicyRow } from "@multica/cerebro-tool-policy/core";

export function resolveAutopilotControlPermissions(
  role: string | null,
  rows: Pick<ToolPolicyRow, "tool_key" | "resource_pattern" | "effective">[] | undefined,
) {
  const isAdmin = role === "owner" || role === "admin";
  const effective = (key: string) =>
    rows?.find((row) => row.tool_key === key && !row.resource_pattern)?.effective.setting ===
    "allow";
  return {
    canManage: isAdmin || effective("create_autopilot"),
    canTrigger: isAdmin || effective("trigger_autopilot"),
  };
}

export function useAutopilotControlPermissions(wsId: string) {
  const member = useCurrentMember(wsId);
  const policy = useQuery(
    toolPolicyTableOptions(wsId, { userId: member.userId }),
  );
  return {
    ...resolveAutopilotControlPermissions(member.role, policy.data),
    isLoading: member.isLoading || policy.isLoading,
  };
}
