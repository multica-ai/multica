"use client";

import { useEffect, useRef } from "react";
import { useCanonicalIssue } from "@multica/core/issues/canonical-id";
import { useIssueDetailSplitStore } from "@multica/core/issues/stores";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import { useNavigation } from "../../navigation";
import { IssueDetail, IssueDetailSkeleton, IssueNotFound } from "./issue-detail";
import { IssueDetailListRail } from "./issue-detail-list-rail";

interface IssueDetailRouteProps {
  /**
   * Raw `/{ws}/issues/{segment}` parameter. Either a UUID (older links, and
   * anything the app itself linked before the issue row was in hand) or a
   * human-readable identifier such as `MUL-123`.
   */
  routeId: string;
  onDelete?: () => void;
}

/**
 * Rewrite `/{ws}/issues/{uuid}` to `/{ws}/issues/{identifier}` once the issue
 * is known, so the address bar and any copied URL read as `MUL-123`.
 *
 * A replace, not a push: the UUID URL is the same page, and a history entry
 * for it would make Back bounce the user between two spellings of one issue.
 */
export function useCanonicalIssueUrl(routeId: string, identifier: string | undefined) {
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const canonicalHref = identifier ? paths.issueDetail(identifier) : null;
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
export function IssueDetailRoute({ routeId, onDelete }: IssueDetailRouteProps) {
  const wsId = useWorkspaceId();
  const { canonicalId, issue, isResolving, notFound } = useCanonicalIssue(wsId, routeId);
  const isCompact = useIsCompact();
  const collapsed = useIssueDetailSplitStore((s) => s.collapsed);

  useCanonicalIssueUrl(routeId, issue?.identifier);

  if (isResolving) return <IssueDetailSkeleton />;

  // Render not-found here rather than handing the unresolved segment down.
  // `IssueDetail` would mount a second observer on the query that just failed,
  // refetch it, and restart this component's resolve/remount cycle — an
  // unbounded request loop that never settles. See `CanonicalIssue.notFound`.
  if (notFound || !canonicalId) return <IssueNotFound showBackLink={!onDelete} />;

  const detail = <IssueDetail issueId={canonicalId} onDelete={onDelete} />;

  // Below the desktop breakpoint keep the current full-width detail and skip
  // the left rail entirely.
  if (isCompact) return detail;

  // Collapsed: a fixed narrow rail (icon + count) beside the full detail,
  // matching the inbox/chat two-panel layout without a resize handle.
  if (collapsed) {
    return (
      <div className="flex flex-1 min-h-0">
        <div className="flex w-11 shrink-0 flex-col border-r">
          <IssueDetailListRail activeIssueId={canonicalId} />
        </div>
        <div className="flex min-h-0 flex-1 flex-col">{detail}</div>
      </div>
    );
  }

  return (
    <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0">
      <ResizablePanel
        id="list"
        defaultSize={320}
        minSize={240}
        maxSize={480}
        groupResizeBehavior="preserve-pixel-size"
      >
        <div className="flex h-full flex-col border-r">
          <IssueDetailListRail activeIssueId={canonicalId} />
        </div>
      </ResizablePanel>
      <ResizableHandle />
      <ResizablePanel id="detail" minSize="40%">
        <div className="flex min-h-0 h-full flex-col">{detail}</div>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
