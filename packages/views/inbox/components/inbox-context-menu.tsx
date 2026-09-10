"use client";

import {
  Fragment,
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type MouseEvent,
  type ReactNode,
} from "react";
import type { InboxItem } from "@multica/core/types";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
} from "@multica/ui/components/ui/context-menu";
import type { InboxView } from "./inbox-view";
import { useInboxItemActions, type InboxRowActions } from "./inbox-item-actions";

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
 *
 * The provider also hands the same action set back down (`useInboxRowActions`)
 * so the row's own compact menu — the touch replacement for right-click and
 * hover — offers exactly these actions.
 */

interface ActiveMenu {
  item: InboxItem;
  position: { x: number; y: number };
}

type OpenInboxContextMenu = (item: InboxItem, event: MouseEvent) => void;

interface InboxContextMenuValue {
  open: OpenInboxContextMenu;
  actions: InboxRowActions;
}

const InboxContextMenuContext = createContext<InboxContextMenuValue | null>(null);

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

  const value = useMemo<InboxContextMenuValue>(
    () => ({ open: openMenu, actions }),
    [openMenu, actions],
  );

  return (
    <InboxContextMenuContext.Provider value={value}>
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
  return useContext(InboxContextMenuContext)?.open ?? null;
}

/**
 * The list's row actions, for the row's own compact menu. `null` without a
 * provider, same graceful degradation as `useInboxContextMenu`.
 */
export function useInboxRowActions(): InboxRowActions | null {
  return useContext(InboxContextMenuContext)?.actions ?? null;
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
  const groups = useInboxItemActions(item, view, actions);

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
        {groups.map((group, index) => (
          <Fragment key={group[0]?.key ?? index}>
            {index > 0 && <ContextMenuSeparator />}
            {group.map((action) => (
              <ContextMenuItem key={action.key} onClick={action.onSelect}>
                {action.icon}
                {action.label}
              </ContextMenuItem>
            ))}
          </Fragment>
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
}
