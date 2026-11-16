"use client";

import { AppLink } from "../../navigation";
import { yieldsToTextMenu } from "../../common/point-menu";
import { useWorkspacePaths } from "@multica/core/paths";
import { IssueChip, useChipIssue } from "./issue-chip";
import { IssueHoverCard } from "./issue-hover-card";
import { useT } from "../../i18n";
import { useCurrentIssueRenderContext } from "../current-issue-render-context";
import { useIssuePeekActions } from "../surface/peek-context";
import {
  IssueContextMenuProvider,
  useOpenIssueContextMenu,
} from "../actions/issue-actions-context-menu";

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
 *
 * Right-click opens the sidebar's issue menu at the cursor once the issue has
 * loaded. Its provider sits outside the hover card, so the open menu does not
 * count as hovering the chip.
 */
export function IssueMentionCard({ issueId, fallbackLabel }: IssueMentionCardProps) {
  return (
    <IssueContextMenuProvider variant="sidebar">
      <IssueHoverCard issueId={issueId} fallbackLabel={fallbackLabel}>
        <IssueMentionLink issueId={issueId} fallbackLabel={fallbackLabel} />
      </IssueHoverCard>
    </IssueContextMenuProvider>
  );
}

function IssueMentionLink({ issueId, fallbackLabel }: IssueMentionCardProps) {
  const p = useWorkspacePaths();
  const currentIssue = useCurrentIssueRenderContext();
  const peek = useIssuePeekActions();
  const issue = useChipIssue(issueId);
  const openMenu = useOpenIssueContextMenu();
  const currentIdentifier =
    currentIssue && issueId === currentIssue.id
      ? currentIssue.identifier
      : null;
  return (
    <AppLink
      href={p.issueDetail(issueId)}
      newTabTitle={fallbackLabel}
      className="issue-mention align-middle"
      onContextMenu={
        issue
          ? (event) => {
              // The innermost object's menu wins; a row around the mention
              // must not open its own as well. Selected text and an editor
              // keep their text menu.
              event.stopPropagation();
              if (!yieldsToTextMenu(event.target as Element)) openMenu(issue, event);
            }
          : undefined
      }
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
  );
}
