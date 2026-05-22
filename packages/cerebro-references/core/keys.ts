// TanStack Query cache keys for the cerebro references domain.
//
// Two access patterns:
//   - byIssue(wsId, issueId)   — "show me everything attached to this issue"
//   - byObject(wsId, object, refId) — reverse lookup "which issues link to X?"
// Both are scoped on workspace id so workspace switching invalidates them
// automatically.

export const referenceKeys = {
  all: (wsId: string) => ["cerebro", "references", wsId] as const,
  byIssue: (wsId: string, issueId: string) =>
    [...referenceKeys.all(wsId), "by-issue", issueId] as const,
  byObject: (wsId: string, object: string, refId: string) =>
    [...referenceKeys.all(wsId), "by-object", object, refId] as const,
};
