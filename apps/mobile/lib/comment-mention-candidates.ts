/**
 * The people-style rows of the comment @mention picker (`@all`, people,
 * agents, squads), filtered by the search query and sorted by name.
 *
 * A guest (MUL-9420) mentions people only: the server refuses its agent and
 * squad mentions, and it does not page the whole workspace with `@all`.
 * Mirrors `packages/views/editor/extensions/mention-suggestion.tsx`.
 */
import type { Agent, MemberWithUser, Squad } from "@multica/core/types";

export interface CommentMentionCandidates {
  all: boolean;
  members: MemberWithUser[];
  agents: Agent[];
  squads: Squad[];
}

export function commentMentionCandidates({
  query,
  members,
  agents,
  squads,
  guest,
}: {
  query: string;
  members: readonly MemberWithUser[];
  agents: readonly Agent[];
  squads: readonly Squad[];
  guest: boolean;
}): CommentMentionCandidates {
  const q = query.trim().toLowerCase();
  const matchName = (name: string) => !q || name.toLowerCase().includes(q);
  const byName = (a: { name: string }, b: { name: string }) =>
    a.name.localeCompare(b.name);
  return {
    all: !guest && (!q || "all".includes(q)),
    members: members.filter((m) => matchName(m.name)).sort(byName),
    agents: guest ? [] : agents.filter((a) => matchName(a.name)).sort(byName),
    squads: guest
      ? []
      : squads.filter((s) => !s.archived_at && matchName(s.name)).sort(byName),
  };
}
