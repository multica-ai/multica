"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { Focus } from "lucide-react";
import type { ContextAnchor } from "@multica/core/chat";
import { useChatStore } from "@multica/core/chat";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { inboxListOptions } from "@multica/core/inbox/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { IssueChip } from "../../issues/components/issue-chip";
import { ProjectChip } from "../../projects/components/project-chip";
import { AppLink, useNavigation } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";

/**
 * Format a derived ContextAnchor as the markdown prefix prepended to the
 * outgoing chat message. Uses the same `mention://issue/<uuid>` scheme as
 * the editor's mention extension, so the AI sees an identical token whether
 * the user typed `@MUL-1` in-line or focus-mode attached it.
 */
export function buildAnchorMarkdown(anchor: ContextAnchor): string {
  if (anchor.type === "issue") {
    const base = `Context: [${anchor.label}](mention://issue/${anchor.id})`;
    return anchor.subtitle ? `${base} — "${anchor.subtitle}"` : base;
  }
  return `Context: Project "${anchor.label}"`;
}

/**
 * Returns true when the given pathname can resolve to an anchor candidate
 * (issue detail, project detail, or inbox). Used by both the resolver and
 * the tracker so they agree on which routes are anchor-eligible.
 */
function isAnchorEligiblePath(pathname: string): boolean {
  if (/^\/[^/]+\/issues\/[^/]+$/.test(pathname)) return true;
  if (/^\/[^/]+\/projects\/[^/]+$/.test(pathname)) return true;
  if (/^\/[^/]+\/inbox$/.test(pathname)) return true;
  return false;
}

/**
 * Runs an effect that remembers the last anchor-eligible location the user
 * visited. Mount this in a component that's present on every page (the app
 * sidebar) so the chat page — which is its own route and therefore has no
 * anchor of its own — can still know what the user was just looking at.
 */
export function useAnchorTracker(): void {
  const { pathname, searchParams } = useNavigation();
  const setLastAnchorLocation = useChatStore((s) => s.setLastAnchorLocation);
  useEffect(() => {
    if (!isAnchorEligiblePath(pathname)) return;
    setLastAnchorLocation({ pathname, search: searchParams.toString() });
  }, [pathname, searchParams, setLastAnchorLocation]);
}

/**
 * Resolve the current page into an anchorable candidate, or null if the user
 * is somewhere without a natural focus object. Subscribes via react-query so
 * the result updates the instant the relevant cache fills.
 *
 * When the user is on the Chat route (no intrinsic anchor), falls back to
 * the last anchor-eligible location remembered by `useAnchorTracker`, so
 * "open Chat from an issue → focus mode still attaches that issue" works.
 *
 * `wsId` is passed in (per CLAUDE.md convention) so this hook works outside
 * a WorkspaceIdProvider if ever reused elsewhere.
 */
export function useRouteAnchorCandidate(wsId: string): {
  candidate: ContextAnchor | null;
  isResolving: boolean;
} {
  const { pathname, searchParams } = useNavigation();
  const lastAnchorLocation = useChatStore((s) => s.lastAnchorLocation);

  // On the Chat route there's no intrinsic anchor; substitute the last
  // anchor-eligible location the user visited. Anywhere else, use the
  // live route directly.
  const useFallback = !isAnchorEligiblePath(pathname) && !!lastAnchorLocation;
  const effectivePath = useFallback ? lastAnchorLocation!.pathname : pathname;
  const effectiveSearch = useFallback
    ? new URLSearchParams(lastAnchorLocation!.search)
    : searchParams;

  const issueMatch = effectivePath.match(/^\/[^/]+\/issues\/([^/]+)$/);
  const projectMatch = effectivePath.match(/^\/[^/]+\/projects\/([^/]+)$/);
  const isInbox = /^\/[^/]+\/inbox$/.test(effectivePath);

  const routeIssueId = issueMatch ? decodeURIComponent(issueMatch[1]!) : null;
  const routeProjectId = projectMatch
    ? decodeURIComponent(projectMatch[1]!)
    : null;

  // Inbox: the anchor is the issue behind the currently selected notification.
  const { data: inboxItems = [] } = useQuery({
    ...inboxListOptions(wsId),
    enabled: isInbox,
  });
  const inboxKey = isInbox ? effectiveSearch.get("issue") : null;
  const inboxSelectedIssueId =
    isInbox && inboxKey
      ? inboxItems.find((i) => (i.issue_id ?? i.id) === inboxKey)?.issue_id ??
        null
      : null;

  // One issue fetch covers both /issues/:id and inbox-derived anchors.
  const issueIdToFetch = routeIssueId ?? inboxSelectedIssueId;
  const { data: issue, isLoading: issueLoading } = useQuery({
    ...issueDetailOptions(wsId, issueIdToFetch ?? ""),
    enabled: !!issueIdToFetch,
  });

  const { data: project, isLoading: projectLoading } = useQuery({
    ...projectDetailOptions(wsId, routeProjectId ?? ""),
    enabled: !!routeProjectId,
  });

  if (issueIdToFetch) {
    if (!issue) return { candidate: null, isResolving: issueLoading };
    return {
      candidate: {
        type: "issue",
        id: issue.id,
        label: issue.identifier,
        subtitle: issue.title,
      },
      isResolving: false,
    };
  }

  if (routeProjectId) {
    if (!project) return { candidate: null, isResolving: projectLoading };
    return {
      candidate: {
        type: "project",
        id: project.id,
        label: project.title,
      },
      isResolving: false,
    };
  }

  return { candidate: null, isResolving: false };
}

/**
 * Focus-mode toggle. Disabled whenever the current page has no anchor
 * (nothing to share) — focusMode persists across such pages, so returning
 * to an anchorable page restores the user's prior on/off choice.
 *
 *   no candidate          →  disabled
 *   off + candidate       →  ghost + muted, clickable (→ turns on)
 *   on  + candidate       →  secondary (bright), clickable (→ turns off)
 */
export function ContextAnchorButton() {
  const wsId = useWorkspaceId();
  const { candidate, isResolving } = useRouteAnchorCandidate(wsId);
  const focusMode = useChatStore((s) => s.focusMode);
  const setFocusMode = useChatStore((s) => s.setFocusMode);

  const hasAnchor = !!candidate;
  const isDisabled = !hasAnchor && !isResolving;
  const isBright = focusMode && hasAnchor;

  const tooltipText = isDisabled
    ? "Nothing to share with Multica on this page"
    : focusMode && candidate
      ? candidate.type === "issue"
        ? `Multica knows you're viewing ${candidate.label} · Click to turn off`
        : `Multica knows you're viewing project "${candidate.label}" · Click to turn off`
      : "Let Multica know what you're viewing";

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant={isBright ? "secondary" : "ghost"}
            size="icon-sm"
            className={isBright ? undefined : "text-muted-foreground"}
            onClick={() => setFocusMode(!focusMode)}
            disabled={isDisabled}
            aria-label={
              focusMode ? "Stop sharing current page" : "Share current page with Multica"
            }
            aria-pressed={focusMode}
          />
        }
      >
        <Focus />
      </TooltipTrigger>
      <TooltipContent side="top">{tooltipText}</TooltipContent>
    </Tooltip>
  );
}

/**
 * Renders the derived focus target above the input. Shows only when focus
 * mode is on *and* the current route resolves to an anchorable object.
 * No dismiss affordance — use the button to leave focus mode.
 */
export function ContextAnchorCard() {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { candidate } = useRouteAnchorCandidate(wsId);
  const focusMode = useChatStore((s) => s.focusMode);

  if (!focusMode || !candidate) return null;

  const href =
    candidate.type === "issue"
      ? paths.issueDetail(candidate.id)
      : paths.projectDetail(candidate.id);

  const tooltipText =
    candidate.type === "issue"
      ? `Multica knows you're viewing ${candidate.label}${candidate.subtitle ? ` — ${candidate.subtitle}` : ""}`
      : `Multica knows you're viewing project "${candidate.label}"`;

  // Same pattern as IssueMentionCard: wrap the pure chip in an AppLink and
  // layer cursor + hover affordance onto the chip. Makes the anchor feel
  // alive (text-cursor → pointer, hover background) and behave consistently
  // with @mentions — clicking jumps to the entity.
  return (
    <div className="mx-2 mt-2 flex items-center">
      <Tooltip>
        <TooltipTrigger
          render={
            <AppLink href={href} className="inline-flex">
              {candidate.type === "issue" ? (
                <IssueChip
                  issueId={candidate.id}
                  fallbackLabel={candidate.label}
                  className="cursor-pointer hover:bg-accent transition-colors"
                />
              ) : (
                <ProjectChip
                  projectId={candidate.id}
                  fallbackLabel={candidate.label}
                  className="cursor-pointer hover:bg-accent transition-colors"
                />
              )}
            </AppLink>
          }
        />
        <TooltipContent side="top">{tooltipText}</TooltipContent>
      </Tooltip>
    </div>
  );
}
