"use client";

import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { useIssueLinkStore } from "@multica/core/issues/stores";
import { IssueChip } from "./issue-chip";

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
 */
export function IssueMentionCard({ issueId, fallbackLabel }: IssueMentionCardProps) {
  const p = useWorkspacePaths();
  const openInNewTab = useIssueLinkStore((s) => s.openInNewTab);
  return (
    <AppLink
      href={p.issueDetail(issueId)}
      target={openInNewTab ? "_blank" : undefined}
      newTabTitle={fallbackLabel}
      className="issue-mention not-prose align-middle"
    >
      <IssueChip
        issueId={issueId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </AppLink>
  );
}
