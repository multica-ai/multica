/**
 * The mounted comment composers, by issue, so a shortcut can reach the one
 * for the issue in front of the user. The newest mount answers: a peek's
 * composer over the page's, for the same issue.
 */
const composers = new Map<string, (() => void)[]>();

export function registerComposerFocus(issueId: string, focus: () => void): () => void {
  const list = composers.get(issueId) ?? [];
  composers.set(issueId, [...list, focus]);
  return () => {
    const remaining = (composers.get(issueId) ?? []).filter((entry) => entry !== focus);
    if (remaining.length > 0) composers.set(issueId, remaining);
    else composers.delete(issueId);
  };
}

/** Focuses `issueId`'s comment composer, or null when none is mounted. */
export function composerFocusFor(issueId: string): (() => void) | null {
  return composers.get(issueId)?.at(-1) ?? null;
}
