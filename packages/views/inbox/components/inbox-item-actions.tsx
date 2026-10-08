"use client";

import { Archive, ArchiveRestore, Check, CircleDot, ExternalLink } from "lucide-react";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import type { RowActionItem } from "../../common/row-actions-menu";
import { useIntentNavigate } from "../../navigation";
import { useT } from "../../i18n";
import type { InboxView } from "./inbox-view";

/** Row-level actions the menus invoke, all keyed by inbox item id. */
export interface InboxRowActions {
  onMarkRead: (id: string) => void;
  onMarkUnread: (id: string) => void;
  /**
   * Archive in the main list, unarchive in the archived one — the same
   * reversal-of-the-current-view the row's inline button performs.
   */
  onAction: (id: string) => void;
}

/**
 * The actions an inbox row offers, grouped the way separators divide them.
 *
 * Shared by the desktop right-click menu and the compact per-row menu that
 * stands in for the hover button wherever the pointer cannot hover, so the two
 * can never drift: a pointer with no hover state gets exactly the actions a
 * right-click gives.
 *
 * `actions` may be null (no provider); the row then has nothing to offer.
 */
export function useInboxItemActions(
  item: InboxItem,
  view: InboxView,
  actions: InboxRowActions | null,
): RowActionItem[][] {
  const { t } = useT("inbox");
  // Null-safe slug (not useWorkspacePaths, which throws): keeps the menus
  // renderable outside a workspace route; the item just doesn't show.
  const slug = useWorkspaceSlug();
  const intentNavigate = useIntentNavigate();
  if (!actions) return [];

  const issueHref =
    slug && item.issue_id
      ? paths.workspace(slug).issueDetail(item.issue_id)
      : null;
  const isArchivedView = view === "archived";

  const groups: RowActionItem[][] = [];

  // An explicit "Open in new tab" CTA is a foreground open — focus follows,
  // per the navigation spec's right-click row. Only rows that reference an
  // issue have somewhere to open.
  if (issueHref) {
    groups.push([
      {
        key: "open-in-new-tab",
        label: t(($) => $.context_menu.open_in_new_tab),
        icon: <ExternalLink className="h-4 w-4" />,
        onSelect: () => intentNavigate(issueHref, "foreground-tab"),
      },
    ]);
  }

  // The read toggle is main-view only. Archived rows deliberately render as
  // read (archiving preserves `read` so a restore can bring the real state
  // back) and the unread count excludes archived items — so a toggle here
  // would report success and change nothing on screen.
  if (!isArchivedView) {
    groups.push([
      item.read === true
        ? {
            key: "mark-unread",
            label: t(($) => $.context_menu.mark_unread),
            icon: <CircleDot className="h-4 w-4" />,
            onSelect: () => actions.onMarkUnread(item.id),
          }
        : {
            key: "mark-read",
            label: t(($) => $.context_menu.mark_read),
            icon: <Check className="h-4 w-4" />,
            onSelect: () => actions.onMarkRead(item.id),
          },
    ]);
  }

  groups.push([
    {
      key: "archive-toggle",
      label: isArchivedView
        ? t(($) => $.context_menu.unarchive)
        : t(($) => $.context_menu.archive),
      icon: isArchivedView ? (
        <ArchiveRestore className="h-4 w-4" />
      ) : (
        <Archive className="h-4 w-4" />
      ),
      onSelect: () => actions.onAction(item.id),
    },
  ]);

  return groups;
}
