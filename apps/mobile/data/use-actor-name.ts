import { useQuery } from "@tanstack/react-query";
import { useWorkspaceStore } from "@/data/workspace-store";
import { memberListOptions } from "@/data/queries/members";
import { agentListOptions } from "@/data/queries/agents";
import { squadListOptions } from "@/data/queries/squads";

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
      return members.find((m) => m.user_id === id)?.avatar_url ?? null;
    }
    if (type === "agent") {
      return agents.find((a) => a.id === id)?.avatar_url ?? null;
    }
    return squads.find((s) => s.id === id)?.avatar_url ?? null;
  };

  return { getName, getAvatarUrl };
}

// Multi-word names (e.g. "John Doe") keep the standard word-initial
// convention, sliced to 2 letters. Single-word names (common for agent
// names, e.g. "Codex" or "客服助手") have too little material for that
// convention, so we take more characters straight from the word: 2 for
// CJK scripts (each character carries more visual weight), 4 for Latin
// scripts (individual letters are narrower).
const CJK_PATTERN = /[一-鿿㐀-䶿]/;

export function getInitials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return "";
  if (words.length > 1) {
    return words
      .map((w) => w[0])
      .join("")
      .toUpperCase()
      .slice(0, 2);
  }
  const [word] = words;
  return CJK_PATTERN.test(word)
    ? word.slice(0, 2)
    : word.toUpperCase().slice(0, 4);
}
