"use client";

import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { IssueChip } from "./issue-chip";
import { IssueHoverCard } from "./issue-hover-card";
import { useT } from "../../i18n";
import { useCurrentIssueRenderContext } from "../current-issue-render-context";
import { useIssuePeekActions } from "../surface/peek-context";

/** Shift+Click with the primary button and no other modifier. */
function isPeekClick(event: React.MouseEvent) {
  return (
    event.button === 0 &&
    event.shiftKey &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.altKey
  );
}

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
 * per-preference override. The one exception is Shift+Click inside a peek
 * host (an issue list surface, the issue page, the inbox detail): it opens
 * the mentioned issue in the floating side peek, the same gesture board cards
 * and list rows use. Outside a host, Shift+Click keeps AppLink's behavior.
 *
 * Hovering opens IssueHoverCard, which shows the detail the chip has no room
 * for. No `delay` is passed, so it opens on Base UI's default dwell. The same
 * `fallbackLabel` the chip degrades to names the card when the detail fetch
 * fails.
 */
export function IssueMentionCard({ issueId, fallbackLabel }: IssueMentionCardProps) {
  const p = useWorkspacePaths();
  const currentIssue = useCurrentIssueRenderContext();
  const peek = useIssuePeekActions();
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
        onMouseDown={
          peek
            ? (event) => {
                // Keep the shift-click from extending the text selection.
                if (isPeekClick(event)) event.preventDefault();
              }
            : undefined
        }
        onClick={
          peek
            ? (event) => {
                if (!isPeekClick(event)) return;
                event.preventDefault();
                peek.toggle(issueId);
              }
            : undefined
        }
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
