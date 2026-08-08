"use client";

import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { useIssueLinkStore, useIssueMentionDisplayStore } from "@multica/core/issues/stores";
import { IssueChip } from "./issue-chip";
import { IssueHoverCard } from "./issue-hover-card";

interface IssueMentionCardProps {
  issueId: string;
  /** Fallback text when issue is not in store (e.g. "MUL-7") */
  fallbackLabel?: string;
}

/**
 * Navigable chip — wraps IssueChip in an AppLink pointing at the issue's
 * detail page. Hover/cursor affordance is layered onto the chip itself so
 * the visual target matches the clickable target.
 *
 * Plain click honors the "open issue links in new tab" preference
 * (Settings → Preferences): new browser tab on web, new app tab on desktop.
 *
 * Density follows the reader's "issue mention style" preference. The narrower
 * modes hide the title, so those get a hover card that brings it back; `full`
 * already shows it and stays exactly as it was.
 */
export function IssueMentionCard({ issueId, fallbackLabel }: IssueMentionCardProps) {
  const p = useWorkspacePaths();
  const openInNewTab = useIssueLinkStore((s) => s.openInNewTab);
  const mode = useIssueMentionDisplayStore((s) => s.mode);

  const link = (
    <AppLink
      href={p.issueDetail(issueId)}
      target={openInNewTab ? "_blank" : undefined}
      newTabTitle={fallbackLabel}
      className="issue-mention align-middle"
    >
      <IssueChip
        issueId={issueId}
        fallbackLabel={fallbackLabel}
        variant={mode}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </AppLink>
  );

  if (mode === "full") return link;

  return <IssueHoverCard issueId={issueId}>{link}</IssueHoverCard>;
}
