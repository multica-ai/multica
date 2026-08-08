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
 * Density follows the reader's "issue mention style" preference, and every mode
 * gets the hover card. The card carries a description snippet, the assignee and
 * sub-issue progress — detail no inline chip shows at any density — so `full`
 * benefits from it just as the title-hiding modes do.
 *
 * Two details are mode-dependent because `plain` renders bare prose, not a box:
 *   - `align-middle` centers the boxed chip in the line box. Bare text of the
 *     surrounding size must sit on the sentence baseline instead, so plain mode
 *     leaves vertical-align alone.
 *   - `hover:bg-accent` needs padding and a radius to look like anything but a
 *     rectangle hugging the glyphs. Plain mode's hover affordance is the
 *     underline that `IssueChip` already carries.
 */
export function IssueMentionCard({ issueId, fallbackLabel }: IssueMentionCardProps) {
  const p = useWorkspacePaths();
  const openInNewTab = useIssueLinkStore((s) => s.openInNewTab);
  const mode = useIssueMentionDisplayStore((s) => s.mode);
  const isPlain = mode === "plain";

  const link = (
    <AppLink
      href={p.issueDetail(issueId)}
      target={openInNewTab ? "_blank" : undefined}
      newTabTitle={fallbackLabel}
      className={isPlain ? "issue-mention" : "issue-mention align-middle"}
    >
      <IssueChip
        issueId={issueId}
        fallbackLabel={fallbackLabel}
        variant={mode}
        className={
          isPlain ? "cursor-pointer" : "cursor-pointer hover:bg-accent transition-colors"
        }
      />
    </AppLink>
  );

  return <IssueHoverCard issueId={issueId}>{link}</IssueHoverCard>;
}
