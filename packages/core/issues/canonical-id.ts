import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { Issue } from "../types";
import { issueDetailOptions, issueKeys } from "./queries";
import { useIssueReadState } from "./read-state";

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** True for a canonical issue UUID; false for an identifier like `MUL-123`. */
export function isIssueUuid(value: string): boolean {
  return UUID_RE.test(value);
}

export interface CanonicalIssue {
  /**
   * The issue's UUID, or `null` while an identifier is still resolving or if
   * it resolved to nothing.
   */
  canonicalId: string | null;
  /** The issue itself, read from the UUID-keyed entry. */
  issue: Issue | undefined;
  isResolving: boolean;
  /**
   * Terminal: the primary read returned an opaque unavailable response.
   *
   * Callers MUST branch on this and render a not-found state rather than
   * handing the unresolved segment to a view that will query it again. A
   * second observer mounting on the failed query refetches it
   * (`retryOnMount`), which flips this hook back to resolving, unmounts that
   * view, and remounts it when the refetch fails — an unbounded request loop
   * that never settles on "not found".
   */
  notFound: boolean;
  loadFailed: boolean;
  /** A successful primary read completed during this mount. */
  hasLoaded: boolean;
  isRetrying: boolean;
  retry: () => Promise<void>;
}

/**
 * Resolve a raw `/{ws}/issues/{segment}` route parameter — a UUID or a
 * human-readable identifier (`MUL-123`) — to the issue and its UUID.
 *
 * Why callers must key on the UUID rather than the raw segment: the realtime
 * updaters patch `issueKeys.detail(wsId, issue.id)` with the UUID carried by
 * the websocket payload (`ws-updaters.ts`). A view keyed on the identifier
 * would sit on a cache entry no realtime event can ever reach, so the detail
 * page would render fine and then silently stop updating. The identifier stays
 * a presentation concern: it lives in the URL, never in a cache key.
 *
 * Resolution reuses `GET /api/issues/{id}`, which accepts either form, so it is
 * the same request the detail view would have made anyway. Handing that
 * response to the canonical query as `initialData` is what holds it to ONE
 * request: `initialData` is applied while the observer is created, so the
 * canonical query never observes an empty cache and never fires a fetch of its
 * own. Seeding the cache from an effect instead would run after that decision
 * and leave the single-request property resting on hook ordering.
 */
export function useCanonicalIssue(wsId: string, routeId: string): CanonicalIssue {
  const qc = useQueryClient();
  const isUuid = isIssueUuid(routeId);
  const resolveEnabled = !isUuid && !!wsId && !!routeId;

  const resolve = useQuery({
    ...issueDetailOptions(wsId, routeId),
    enabled: resolveEnabled,
  });

  const canonicalId = isUuid ? routeId : (resolve.data?.id ?? null);

  const detail = useQuery({
    ...issueDetailOptions(wsId, canonicalId ?? ""),
    enabled: !!canonicalId && !!wsId,
    // Ignored once the entry holds data, so a row already patched by a realtime
    // event is never overwritten by this older resolution response.
    initialData: isUuid ? undefined : resolve.data,
  });

  // Mirror the loaded row into its identifier-keyed entry, the reverse of the
  // `initialData` hop above.
  //
  // In-app links still carry the UUID, so opening one lands on the UUID URL and
  // the route then rewrites the address bar to the identifier. That rewrite is
  // a navigation: the route re-renders with the identifier as its segment, and
  // without this seed the resolution query above would miss on a cache entry
  // nothing has filled and re-fetch an issue already in hand — a second request
  // for one issue open. The `??` keeps a realtime-patched entry intact, and
  // only `.id` is ever read back out, which never changes.
  const loadedIdentifier = detail.data?.identifier;
  const loadedIssue = detail.data;
  useEffect(() => {
    if (!loadedIssue || !loadedIdentifier || loadedIdentifier === routeId) return;
    qc.setQueryData<Issue>(
      issueKeys.detail(wsId, loadedIdentifier),
      (old) => old ?? loadedIssue,
    );
  }, [qc, wsId, loadedIdentifier, loadedIssue, routeId]);

  // A failed identifier must settle here without mounting a second observer.
  // Once resolved, the UUID entry owns both the data and background failures.
  const readState = useIssueReadState(
    wsId,
    canonicalId ?? routeId,
    canonicalId ? detail : resolve,
  );

  return {
    canonicalId,
    hasLoaded:
      (resolve.isSuccess && resolve.isFetchedAfterMount) ||
      (detail.isSuccess && detail.isFetchedAfterMount),
    issue: detail.data,
    isResolving: resolveEnabled && !canonicalId && !readState.notFound && !readState.loadFailed,
    ...readState,
  };
}
