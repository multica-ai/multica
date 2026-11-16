"use client";

import { useState } from "react";

/**
 * `live` as the open issue sees it. A click, or the issue header moving on,
 * names the next issue before the route reaches it, which on web waits for
 * the next page to load. While `namesNext` says `live` has named another
 * issue, the open issue keeps the value it had; once the route reaches
 * another issue, or `live` stops naming one, `live` takes over.
 */
export function useHeldUntilArrival<T>(openIssueId: string | null, live: T, namesNext: boolean) {
  const [settled, setSettled] = useState({ issueId: openIssueId, value: live });
  const movingOn = settled.issueId === openIssueId && live !== settled.value && namesNext;
  if (!movingOn && (settled.issueId !== openIssueId || settled.value !== live)) {
    setSettled({ issueId: openIssueId, value: live });
  }
  return { value: movingOn ? settled.value : live, movingOn };
}
