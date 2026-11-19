"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "../auth";
import type { MemberRole, MemberWithUser } from "../types";
import { memberListOptions } from "../workspace/queries";

/**
 * Resolves the current user's membership in the given workspace. Single source
 * of truth for "what role am I" — replaces ad-hoc `members.find(...)` lookups
 * scattered across the views.
 *
 * `wsId` is explicit (not via `useWorkspaceId()` Context) so this hook stays
 * usable in components that may render before workspace context is wired,
 * matching the repo rule for workspace-aware hooks.
 */
export function useCurrentMember(wsId: string): {
  userId: string | null;
  role: MemberRole | null;
  member: MemberWithUser | null;
  isLoading: boolean;
} {
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members, isLoading } = useQuery(memberListOptions(wsId));
  const member = members?.find((m) => m.user_id === userId) ?? null;
  return {
    userId,
    role: member?.role ?? null,
    member,
    isLoading,
  };
}

/**
 * Whether the current user is a guest of the workspace (MUL-9420). A guest
 * reaches only its shared projects, so the shell hides everything else and
 * skips the queries the server would refuse. False while the member list
 * loads.
 */
export function useIsGuest(wsId: string | null | undefined): boolean {
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members } = useQuery({
    ...memberListOptions(wsId ?? ""),
    enabled: !!wsId && !!userId,
  });
  return !!members?.some((m) => m.user_id === userId && m.role === "guest");
}
