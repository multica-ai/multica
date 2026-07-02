import type { QueryClient } from "@tanstack/react-query";
import { useQuery } from "@tanstack/react-query";
import { memberListOptions } from "@multica/core/workspace/queries";
import { checkMentionAccess } from "../core/comments";

// FIR-2595 point 3 — scope the @mention picker inside a note to the people the
// note is actually shared with (and their agents), instead of the full
// workspace directory. This prevents even offering a colleague who cannot open
// the note — the same leak the give-access prompt catches after the fact, but
// caught earlier by never suggesting the person at all.
//
// "Who can open this note" is resolved server-side by the mention-access
// endpoint (folder grants + note shares + visibility), so we do NOT re-derive
// that logic on the client. `useEnsureNoteMentionScope` keeps the answer warm
// in the TanStack Query cache; `getNoteMentionScope` reads it back
// synchronously from the TipTap factory, which runs outside React render.

const SCOPE_ROOT = "note-mention-scope";

export function noteMentionScopeKey(wsId: string, noteId: string) {
  return ["notes", wsId, SCOPE_ROOT, noteId] as const;
}

interface NoteMentionScopeData {
  // Member ids the server reports as NOT able to open the note.
  noAccess: string[];
}

/**
 * Resolved scope for a note's @mention picker.
 *
 * `active` is false when scoping must not apply — no note in context, the
 * feature is off, or the answer has not loaded yet. In that case the picker
 * keeps its full upstream behaviour (fail open, never over-hide).
 *
 * When `active` is true, `allows(id)` returns whether the given member id may
 * be suggested. Agents are scoped by owner: an agent owned by someone without
 * note access is hidden, while an ownerless (workspace-wide) agent is kept —
 * it is not a private colleague's personal agent, and hiding it would strip
 * shared automation from every restricted note.
 */
export interface NoteMentionScope {
  active: boolean;
  allows: (memberId: string | null | undefined) => boolean;
}

const INACTIVE_SCOPE: NoteMentionScope = { active: false, allows: () => true };

/**
 * Keep the note's mention-scope answer warm in the query cache. Fetches "which
 * of the workspace's members cannot open this note" once the note is known and
 * scoping is enabled. Returns nothing — its only job is to feed the
 * synchronous `getNoteMentionScope` read from the editor factory.
 */
export function useEnsureNoteMentionScope(
  wsId: string,
  noteId: string | null | undefined,
  enabled: boolean,
): void {
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: enabled && Boolean(wsId),
  });
  const memberIds = members.map((m) => m.user_id);

  useQuery({
    queryKey: noteMentionScopeKey(wsId, noteId ?? ""),
    queryFn: async (): Promise<NoteMentionScopeData> => ({
      noAccess: await checkMentionAccess(noteId!, memberIds),
    }),
    enabled: enabled && Boolean(wsId && noteId) && memberIds.length > 0,
    staleTime: 30_000,
  });
}

/**
 * Read the note's mention scope synchronously from the query cache. Used by the
 * @mention factory, which runs outside React render and cannot fetch on demand.
 */
export function getNoteMentionScope(
  qc: QueryClient,
  wsId: string,
  noteId: string | null | undefined,
): NoteMentionScope {
  if (!noteId) return INACTIVE_SCOPE;
  const data = qc.getQueryData<NoteMentionScopeData>(
    noteMentionScopeKey(wsId, noteId),
  );
  if (!data) return INACTIVE_SCOPE;

  const noAccess = new Set(data.noAccess);
  return {
    active: true,
    // Allowed when the server did not flag the member as no-access. Ownerless
    // agents (memberId == null) are kept — see NoteMentionScope docs.
    allows: (memberId) => (memberId ? !noAccess.has(memberId) : true),
  };
}
