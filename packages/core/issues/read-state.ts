import { useRef, useState } from "react";
import type { UseQueryResult } from "@tanstack/react-query";
import { ApiError } from "../api/client";
import type { Issue } from "../types";

/** Keep opaque client refusals on the existing unavailable surface. */
function isUnavailable(error: unknown): boolean {
  return error instanceof ApiError && error.status >= 400 && error.status < 500;
}

/** Presentation and manual retry for an uncached primary issue read only. */
export function useIssueReadState(
  wsId: string,
  id: string,
  query: Pick<UseQueryResult<Issue>, "data" | "error" | "isError" | "isFetching" | "refetch">,
) {
  const scope = JSON.stringify([wsId, id]);
  const [attempt, setAttempt] = useState<{ scope: string } | null>(null);
  const pending = useRef<{ scope: string } | null>(null);
  const isRetrying = attempt?.scope === scope;
  const failed = query.data === undefined && query.isError;
  const notFound = failed && isUnavailable(query.error);
  const loadFailed = query.data === undefined && ((failed && !notFound) || isRetrying);

  const retry = async () => {
    if (!loadFailed || query.isFetching || pending.current?.scope === scope) return;
    const current = { scope };
    pending.current = current;
    setAttempt(current);
    try {
      // Reuse the existing observer; do not cancel or mount another failed read.
      await query.refetch({ cancelRefetch: false });
    } catch {
      // The query owns the error. Never expose a raw server response here.
    } finally {
      if (pending.current === current) pending.current = null;
      setAttempt((value) => value === current ? null : value);
    }
  };

  return { notFound, loadFailed, isRetrying, retry };
}
