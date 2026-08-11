"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type MouseEvent,
  type ReactNode,
} from "react";
import { Archive, ArchiveRestore, Check, CircleDot, ExternalLink } from "lucide-react";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { useIntentNavigate } from "../../navigation";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
} from "@multica/ui/components/ui/context-menu";
import type { InboxView } from "./inbox-view";
import { useT } from "../../i18n";

/**
 * Right-click actions for inbox rows.
 *
 * One shared menu for the whole list rather than a Base UI ContextMenu root
 * per row — the same shape `IssueContextMenuProvider` uses, for the same two
 * reasons. Only one menu is ever open, so N roots is N times the machinery for
 * nothing; and the list is virtualized, so a per-row root would unmount (taking
 * its open menu with it) the moment the row scrolls out of the viewport.
 *
 * Rows delegate: the row only reports (item, cursor position) up to this
 * singleton, which anchors the menu at the cursor via a virtual anchor element.
 */

/** Row-level actions the menu invokes, all keyed by inbox item id. */
export interface InboxRowActions {
  onMarkRead: (id: string) => void;
  onMarkUnread: (id: string) => void;
  /**
   * Archive in the main list, unarchive in the archived one — the same
   * reversal-of-the-current-view the row's hover button performs.
   */
  onAction: (id: string) => void;
}

interface ActiveMenu {
  item: InboxItem;
  position: { x: number; y: number };
}

type OpenInboxContextMenu = (item: InboxItem, event: MouseEvent) => void;

const InboxContextMenuContext = createContext<OpenInboxContextMenu | null>(null);

export function InboxContextMenuProvider({
  view,
  actions,
  children,
}: {
  view: InboxView;
  actions: InboxRowActions;
  children: ReactNode;
}) {
  const [active, setActive] = useState<ActiveMenu | null>(null);
  const [open, setOpen] = useState(false);
  // The row the user right-clicked. Rows style their "menu is open for me"
  // state off `data-popup-open`; setting it imperatively keeps that affordance
  // without re-rendering every row on open/close.
  const triggerElRef = useRef<HTMLElement | null>(null);

  const openMenu = useCallback<OpenInboxContextMenu>((item, event) => {
    event.preventDefault();
    triggerElRef.current?.removeAttribute("data-popup-open");
    const el = event.currentTarget as HTMLElement;
    el.setAttribute("data-popup-open", "");
    triggerElRef.current = el;
    setActive({ item, position: { x: event.clientX, y: event.clientY } });
    setOpen(true);
  }, []);

  const handleOpenChange = useCallback((v: boolean) => {
    if (!v) {
      triggerElRef.current?.removeAttribute("data-popup-open");
      triggerElRef.current = null;
    }
    setOpen(v);
  }, []);

  return (
    <InboxContextMenuContext.Provider value={openMenu}>
      {children}
      {/* Mounted on first use, kept mounted after — the popup itself unmounts
          while closed. */}
      {active && (
        <InboxContextMenuSingleton
          item={active.item}
          position={active.position}
          view={view}
          actions={actions}
          open={open}
          onOpenChange={handleOpenChange}
        />
      )}
    </InboxContextMenuContext.Provider>
  );
}

/**
 * Opens the list's shared context menu for a row. Returns `null` when no
 * provider is above — the row then simply has no right-click menu instead of
 * crashing, which keeps `InboxListItem` renderable on its own.
 */
export function useInboxContextMenu(): OpenInboxContextMenu | null {
  return useContext(InboxContextMenuContext);
}

function InboxContextMenuSingleton({
  item,
  position,
  view,
  actions,
  open,
  onOpenChange,
}: ActiveMenu & {
  view: InboxView;
  actions: InboxRowActions;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const { t } = useT("inbox");
  // Null-safe slug (not useWorkspacePaths, which throws): keeps the menu
  // renderable outside a workspace route; the item just doesn't show.
  const slug = useWorkspaceSlug();
  const issueHref =
    slug && item.issue_id
      ? paths.workspace(slug).issueDetail(item.issue_id)
      : null;
  const intentNavigate = useIntentNavigate();
  const isArchivedView = view === "archived";

  // Point-sized virtual anchor at the right-click position, so the menu opens
  // at the cursor rather than at the row's top-left corner.
  const anchor = useMemo(
    () => ({
      getBoundingClientRect: () =>
        DOMRect.fromRect({ x: position.x, y: position.y, width: 0, height: 0 }),
    }),
    [position.x, position.y],
  );

  return (
    <ContextMenu open={open} onOpenChange={onOpenChange}>
      <ContextMenuContent anchor={anchor}>
        {/* An explicit "Open in new tab" CTA is a foreground open — focus
            follows, per the navigation spec's right-click row. Only rows that
            reference an issue have somewhere to open. */}
        {issueHref && (
          <>
            <ContextMenuItem
              onClick={() => intentNavigate(issueHref, "foreground-tab")}
            >
              <ExternalLink className="h-4 w-4" />
              {t(($) => $.context_menu.open_in_new_tab)}
            </ContextMenuItem>
            <ContextMenuSeparator />
          </>
        )}
        {/* The read toggle is main-view only. Archived rows deliberately render
            as read (archiving preserves `read` so a restore can bring the real
            state back) and the unread count excludes archived items — so a
            toggle here would report success and change nothing on screen. */}
        {!isArchivedView && (
          <>
            {item.read === true ? (
              <ContextMenuItem onClick={() => actions.onMarkUnread(item.id)}>
                <CircleDot className="h-4 w-4" />
                {t(($) => $.context_menu.mark_unread)}
              </ContextMenuItem>
            ) : (
              <ContextMenuItem onClick={() => actions.onMarkRead(item.id)}>
                <Check className="h-4 w-4" />
                {t(($) => $.context_menu.mark_read)}
              </ContextMenuItem>
            )}
            <ContextMenuSeparator />
          </>
        )}
        <ContextMenuItem onClick={() => actions.onAction(item.id)}>
          {isArchivedView ? (
            <>
              <ArchiveRestore className="h-4 w-4" />
              {t(($) => $.context_menu.unarchive)}
            </>
          ) : (
            <>
              <Archive className="h-4 w-4" />
              {t(($) => $.context_menu.archive)}
            </>
          )}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}
