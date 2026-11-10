"use client";

import { useCallback, useEffect, useRef } from "react";
import { useCanonicalIssue } from "@multica/core/issues/canonical-id";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../navigation";
import { IssueDetail, IssueDetailSkeleton, IssueNotFound, IssueLoadError } from "./issue-detail";
import { IssuePeekHost } from "./issue-peek";

interface IssueDetailRouteProps {
  /**
   * Raw `/{ws}/issues/{segment}` parameter. Either a UUID (older links, and
   * anything the app itself linked before the issue row was in hand) or a
   * human-readable identifier such as `MUL-123`.
   */
  routeId: string;
  onDelete?: () => void;
  /**
   * The decoded file name of a `/{ws}/issues/{segment}/deliverables/{name}`
   * link: open the newest version of that deliverable.
   */
  deliverable?: string;
}

/**
 * Rewrite `/{ws}/issues/{uuid}` to `/{ws}/issues/{identifier}` once the issue
 * is known, so the address bar and any copied URL read as `MUL-123`.
 *
 * A replace, not a push: the UUID URL is the same page, and a history entry
 * for it would make Back bounce the user between two spellings of one issue.
 */
export function useCanonicalIssueUrl(
  routeId: string,
  identifier: string | undefined,
  hash = "",
  deliverable?: string,
) {
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  // A deliverable link keeps its `/deliverables/{name}` tail: dropping it
  // would turn the address into a plain issue link while the file is open.
  const canonicalHref = identifier
    ? `${deliverable ? paths.issueDeliverable(identifier, deliverable) : paths.issueDetail(identifier)}${hash}`
    : null;
  // `useWorkspacePaths()` and the navigation adapter are both rebuilt on
  // render, so this ref — not the dependency array — is what guarantees the
  // replace runs once per target instead of on every commit.
  const replacedRef = useRef<string | null>(null);

  useEffect(() => {
    if (!canonicalHref || routeId === identifier) return;
    if (replacedRef.current === canonicalHref) return;
    replacedRef.current = canonicalHref;
    navigation.replace(canonicalHref);
  }, [canonicalHref, identifier, routeId, navigation]);
}

export function parseCommentHighlightHash(hash: string): string | undefined {
  const match = /^#comment-([A-Za-z0-9_-]+)$/.exec(hash);
  return match?.[1];
}

function useCommentHighlightHash(): { hash: string; commentId?: string } {
  const { hash } = useNavigation();

  return { hash, commentId: parseCommentHighlightHash(hash) };
}

/**
 * Route-level wrapper around `IssueDetail` for `/{ws}/issues/{id}`.
 *
 * Two jobs the panel-embedded `IssueDetail` must not do:
 *  - resolve an identifier segment to the issue's UUID before rendering, so
 *    every cache key below stays UUID-keyed and realtime updates land (see
 *    `useCanonicalIssue`);
 *  - rewrite the address bar to the canonical identifier URL. That belongs to
 *    the route and only the route — the inbox renders `IssueDetail` in a side
 *    panel, where replacing the URL would navigate the user out of the inbox.
 */
export function IssueDetailRoute({ routeId, onDelete, deliverable }: IssueDetailRouteProps) {
  const wsId = useWorkspaceId();
  const {
    canonicalId, issue, isResolving, notFound, loadFailed, isRetrying, retry, hasLoaded,
  } = useCanonicalIssue(wsId, routeId);
  const highlight = useCommentHighlightHash();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  useCanonicalIssueUrl(routeId, issue?.identifier, highlight.hash, deliverable);

  // Once the link has opened its file and the viewer has closed (or the file
  // is missing), the address becomes the plain issue page. A replace: Back
  // should leave the issue, not re-open the file.
  const issueRef = issue?.identifier ?? routeId;
  const latestNav = useRef({ paths, navigation });
  latestNav.current = { paths, navigation };
  const onDeliverableLinkDone = useCallback(() => {
    const { paths, navigation } = latestNav.current;
    navigation.replace(paths.issueDetail(issueRef));
  }, [issueRef]);

  if (loadFailed) {
    return <IssueLoadError showBackLink={!onDelete} onRetry={retry} isRetrying={isRetrying} />;
  }

  if (isResolving || (!issue && !notFound)) return <IssueDetailSkeleton />;

  // Render not-found here rather than handing the unresolved segment down.
  // `IssueDetail` would mount a second observer on the query that just failed,
  // refetch it, and restart this component's resolve/remount cycle — an
  // unbounded request loop that never settles. See `CanonicalIssue.notFound`.
  if (notFound || !canonicalId) return <IssueNotFound showBackLink={!onDelete} />;

  // The page hosts a side peek so Shift+Click on an issue mention in the
  // description or comments opens it floating over the page. Keyed so moving
  // to another issue closes the peek.
  return (
    <IssuePeekHost key={canonicalId}>
      <IssueDetail
        issueId={canonicalId}
        skipInitialRefetch={hasLoaded}
        onDelete={onDelete}
        highlightCommentId={highlight.commentId}
        openDeliverable={deliverable}
        onDeliverableLinkDone={deliverable ? onDeliverableLinkDone : undefined}
      />
    </IssuePeekHost>
  );
}
