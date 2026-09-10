"use client";

import { AppLink, resolveClickIntent, useOptionalNavigation } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { IssueChip } from "./issue-chip";
import { IssueHoverCard } from "./issue-hover-card";
import { useT } from "../../i18n";
import { useCurrentIssueRenderContext } from "../current-issue-render-context";

interface IssueMentionCardProps {
  issueId: string;
  /** Fallback text when issue is not in store (e.g. "MUL-7") */
  fallbackLabel?: string;
}

function CurrentIssueChipContent({ identifier }: { identifier: string }) {
  const { t } = useT("issues");

  return (
    <span className="font-medium text-muted-foreground shrink-0">
      <span>{t(($) => $.detail.current_issue)}</span>{" "}
      <span className="text-muted-foreground">·</span>{" "}
      <span translate="no">{identifier}</span>
    </span>
  );
}

/**
 * Navigable chip — wraps IssueChip in an AppLink pointing at the issue's
 * detail page. Hover/cursor affordance is layered onto the chip itself so
 * the visual target matches the clickable target.
 *
 * AppLink owns the click semantics: plain click navigates in place, modifier
 * and middle clicks open tabs. There is deliberately no per-surface or
 * per-preference override — with one exception: a plain click on a mention of
 * the CURRENT issue (the one whose page is rendering this content) navigates
 * nowhere and flashes the active tab instead, since the destination is the
 * page already on screen.
 *
 * Hovering opens IssueHoverCard, which shows the detail the chip has no room
 * for. No `delay` is passed, so it opens on Base UI's default dwell. The same
 * `fallbackLabel` the chip degrades to names the card when the detail fetch
 * fails.
 */
export function IssueMentionCard({ issueId, fallbackLabel }: IssueMentionCardProps) {
  const p = useWorkspacePaths();
  const nav = useOptionalNavigation();
  const currentIssue = useCurrentIssueRenderContext();
  const isCurrentIssue = !!currentIssue && issueId === currentIssue.id;
  const currentIdentifier = isCurrentIssue ? currentIssue.identifier : null;

  // A plain click on a mention of the issue the reader is already viewing
  // would re-navigate to the page it is already on: a no-op that still tears
  // the detail view down and rebuilds it. Swallow that click and flash the
  // active tab instead, so the affordance still acknowledges the interaction.
  // Modifier / middle clicks keep their meaning — opening the same issue in a
  // NEW tab is a real destination — so only the "push" intent is intercepted.
  const handleClick = isCurrentIssue
    ? (e: React.MouseEvent<HTMLAnchorElement>) => {
        if (resolveClickIntent(e) !== "push") return;
        e.preventDefault();
        nav?.flashActiveTab?.();
      }
    : undefined;

  return (
    <IssueHoverCard issueId={issueId} fallbackLabel={fallbackLabel}>
      <AppLink
        href={p.issueDetail(issueId)}
        newTabTitle={fallbackLabel}
        onClick={handleClick}
        className="issue-mention align-middle"
      >
        <IssueChip
          issueId={issueId}
          fallbackLabel={fallbackLabel}
          className="cursor-pointer hover:bg-accent transition-colors"
        >
          {currentIdentifier ? (
            <CurrentIssueChipContent identifier={currentIdentifier} />
          ) : undefined}
        </IssueChip>
      </AppLink>
    </IssueHoverCard>
  );
}
