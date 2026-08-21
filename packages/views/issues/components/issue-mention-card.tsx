"use client";

import { AppLink } from "../../navigation";
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
 * per-preference override.
 *
 * Hovering opens IssueHoverCard, which shows the detail the chip has no room
 * for. No `delay` is passed, so it opens on Base UI's default dwell. The same
 * `fallbackLabel` the chip degrades to names the card when the detail fetch
 * fails.
 */
export function IssueMentionCard({ issueId, fallbackLabel }: IssueMentionCardProps) {
  const p = useWorkspacePaths();
  const currentIssue = useCurrentIssueRenderContext();
  const currentIdentifier =
    currentIssue && issueId === currentIssue.id
      ? currentIssue.identifier
      : null;
  return (
    <IssueHoverCard issueId={issueId} fallbackLabel={fallbackLabel}>
      <AppLink
        href={p.issueDetail(issueId)}
        newTabTitle={fallbackLabel}
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
