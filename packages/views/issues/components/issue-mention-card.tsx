"use client";

import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { IssueChip } from "./issue-chip";

interface IssueMentionCardProps {
  issueId: string;
  /** Fallback text when issue is not in store (e.g. "MUL-7") */
  fallbackLabel?: string;
  openInModal?: boolean;
}

/**
 * Navigable chip — wraps IssueChip in an AppLink pointing at the issue's
 * detail page. Hover/cursor affordance is layered onto the chip itself so
 * the visual target matches the clickable target.
 */
export function IssueMentionCard({
  issueId,
  fallbackLabel,
  openInModal = false,
}: IssueMentionCardProps) {
  const p = useWorkspacePaths();
  const openModal = useModalStore((s) => s.open);
  const chip = (
    <IssueChip
      issueId={issueId}
      fallbackLabel={fallbackLabel}
      className="cursor-pointer hover:bg-accent transition-colors"
    />
  );
  if (openInModal) {
    return (
      <button
        type="button"
        className="issue-mention not-prose inline-flex"
        onClick={() => openModal("issue-detail", { issueId })}
      >
        {chip}
      </button>
    );
  }
  return (
    <AppLink href={p.issueDetail(issueId)} className="issue-mention not-prose inline-flex">
      {chip}
    </AppLink>
  );
}
