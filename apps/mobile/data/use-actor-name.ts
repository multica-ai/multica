import { useQuery } from "@tanstack/react-query";
import { useWorkspaceStore } from "@/data/workspace-store";
import { memberListOptions } from "@/data/queries/members";
import { agentListOptions } from "@/data/queries/agents";
import { squadListOptions } from "@/data/queries/squads";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { resolveAvatarUrl } from "@multica/core/gravatar";
import { deriveGravatarSettings } from "@multica/core/gravatar/settings";

/**
 * Resolve actor (member / agent / squad) name + avatar URL from the
 * workspace lists. Mirrors packages/core/workspace/hooks.ts useActorName.
 *
 * Returns synchronous lookup helpers — they read whatever is in the TQ
 * cache. If the lists haven't loaded yet, lookups return null/initials
 * fallback; the row will re-render once data arrives.
 */
export function useActorLookup() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  // Use select to derive only the gravatar setting — avoids re-renders
  // when unrelated workspace data changes.
  const gravatarEnabled = useQuery({
    ...workspaceListOptions(),
    select: (data) => deriveGravatarSettings(data.find((w) => w.id === wsId) ?? null).enabled,
  }).data ?? false;
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));

  const getName = (
    type: "member" | "agent" | "squad" | null | undefined,
    id: string | null | undefined,
  ): string => {
    if (!type || !id) return "System";
    if (type === "member") {
      const m = members.find((m) => m.user_id === id);
      return m?.name ?? "Unknown";
    }
    if (type === "agent") {
      const a = agents.find((a) => a.id === id);
      return a?.name ?? "Unknown Agent";
    }
    return squads.find((s) => s.id === id)?.name ?? "Squad";
  };

  const getAvatarUrl = (
    type: "member" | "agent" | "squad" | null | undefined,
    id: string | null | undefined,
  ): string | null => {
    if (!type || !id) return null;
    if (type === "member") {
      const m = members.find((m) => m.user_id === id);
      return resolveAvatarUrl({
        avatarUrl: m?.avatar_url,
        email: m?.email,
        gravatarEnabled,
      });
    }
    if (type === "agent") {
      return agents.find((a) => a.id === id)?.avatar_url ?? null;
    }
    return squads.find((s) => s.id === id)?.avatar_url ?? null;
  };

  return { getName, getAvatarUrl };
}

export function getInitials(name: string): string {
  return name
    .split(" ")
    .map((w) => w[0])
    .filter(Boolean)
    .join("")
    .toUpperCase()
    .slice(0, 2);
}
