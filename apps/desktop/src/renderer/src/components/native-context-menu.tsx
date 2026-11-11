import { Fragment, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Globe, ImageIcon, Link2 } from "@multica/ui/icons";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
} from "@multica/ui/components/ui/context-menu";
import { createShortcutChord, formatShortcut } from "@multica/core/shortcuts";
import { useT } from "@multica/views/i18n";
import {
  contextMenuActions,
  type ContextMenuAction,
  type ContextMenuCommand,
  type ContextMenuRequest,
} from "../../../shared/context-menu";

/** A request older than this no longer belongs to the last DOM right-click. */
const POINT_MAX_AGE_MS = 1000;

interface OpenMenu {
  request: ContextMenuRequest;
  point: { x: number; y: number };
  /** Focus and selection when the menu opened; an edit command needs both back. */
  focus: HTMLElement | null;
  ranges: Range[];
}

function isTextField(el: HTMLElement | null): boolean {
  return el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement;
}

function restoreSelection(menu: OpenMenu) {
  // A text field keeps its own selection across focus changes; putting the
  // document ranges back would collapse it.
  if (isTextField(menu.focus)) return;
  const selection = window.getSelection();
  if (!selection || menu.ranges.length === 0) return;
  if (!menu.ranges.every((range) => range.startContainer.isConnected && range.endContainer.isConnected)) return;
  selection.removeAllRanges();
  for (const range of menu.ranges) selection.addRange(range);
}

/**
 * Draws the menu for a right-click the page left unhandled — a text field,
 * selected text, a link, an image — which main forwards instead of popping a
 * native menu, so every menu in the app shares one language and one look.
 * Picking an item sends main a command; it runs once the menu has closed and
 * focus and selection are back where the right-click found them, since cut,
 * copy and paste act on whatever holds them.
 */
export function NativeContextMenu() {
  const { t } = useT("layout");
  const [menu, setMenu] = useState<OpenMenu | null>(null);
  const [open, setOpen] = useState(false);
  const lastEvent = useRef<{ x: number; y: number; at: number } | null>(null);
  const pendingCommand = useRef<ContextMenuCommand | null>(null);

  useEffect(() => {
    // Capture phase, so a right-click is seen even when a handler stops it.
    // The DOM position is in the page's own coordinates; Electron's is not
    // once the page is zoomed.
    const record = (event: MouseEvent) => {
      lastEvent.current = { x: event.clientX, y: event.clientY, at: performance.now() };
    };
    window.addEventListener("contextmenu", record, true);
    const unsubscribe = window.desktopAPI.onContextMenu((request) => {
      const last = lastEvent.current;
      const point =
        last && performance.now() - last.at < POINT_MAX_AGE_MS
          ? { x: last.x, y: last.y }
          : { x: request.x, y: request.y };
      const active = document.activeElement;
      const selection = window.getSelection();
      const ranges = selection
        ? Array.from({ length: selection.rangeCount }, (_, i) => selection.getRangeAt(i).cloneRange())
        : [];
      pendingCommand.current = null;
      setMenu({
        request,
        point,
        focus: active instanceof HTMLElement && active !== document.body ? active : null,
        ranges,
      });
      setOpen(true);
    });
    return () => {
      window.removeEventListener("contextmenu", record, true);
      unsubscribe();
    };
  }, []);

  const point = menu?.point;
  const anchor = useMemo(
    () =>
      point
        ? {
            getBoundingClientRect: () =>
              DOMRect.fromRect({ x: point.x, y: point.y, width: 0, height: 0 }),
          }
        : undefined,
    [point],
  );

  if (!menu) return null;

  const { request } = menu;
  const actions = new Set(contextMenuActions(request));
  const pick = (command: ContextMenuCommand) => {
    pendingCommand.current = command;
  };
  const shortcut = (key: string) => formatShortcut(createShortcutChord(key, { primary: true }));

  const labels: Record<ContextMenuAction, string> = {
    cut: t(($) => $.context_menu.cut),
    copy: t(($) => $.context_menu.copy),
    paste: t(($) => $.context_menu.paste),
    selectAll: t(($) => $.context_menu.select_all),
    addToDictionary: t(($) => $.context_menu.add_to_dictionary),
    openLink: t(($) => $.context_menu.open_link),
    copyLink: t(($) => $.context_menu.copy_link_address),
    copyImage: t(($) => $.context_menu.copy_image),
  };
  const extras: Partial<Record<ContextMenuAction, ReactNode>> = {
    cut: <ContextMenuShortcut>{shortcut("X")}</ContextMenuShortcut>,
    copy: <ContextMenuShortcut>{shortcut("C")}</ContextMenuShortcut>,
    paste: <ContextMenuShortcut>{shortcut("V")}</ContextMenuShortcut>,
    selectAll: <ContextMenuShortcut>{shortcut("A")}</ContextMenuShortcut>,
  };
  const icons: Partial<Record<ContextMenuAction, ReactNode>> = {
    openLink: <Globe />,
    copyLink: <Link2 />,
    copyImage: <ImageIcon />,
  };
  const item = (type: ContextMenuAction) =>
    actions.has(type) ? (
      <ContextMenuItem key={type} onClick={() => pick({ id: request.id, type })}>
        {icons[type]}
        {labels[type]}
        {extras[type]}
      </ContextMenuItem>
    ) : null;

  const groups: ReactNode[][] = [
    [
      ...request.suggestions.map((suggestion, index) => (
        <ContextMenuItem
          key={`suggestion-${index}`}
          className="font-medium"
          onClick={() => pick({ id: request.id, type: "replaceMisspelling", index })}
        >
          {suggestion}
        </ContextMenuItem>
      )),
      item("addToDictionary"),
    ],
    [item("cut"), item("copy"), item("paste")],
    [item("selectAll")],
    [item("openLink"), item("copyLink")],
    [item("copyImage")],
  ]
    .map((group) => group.filter(Boolean))
    .filter((group) => group.length > 0);

  return (
    <ContextMenu
      open={open}
      onOpenChange={setOpen}
      onOpenChangeComplete={(isOpen) => {
        if (isOpen) return;
        const command = pendingCommand.current;
        pendingCommand.current = null;
        if (!command) return;
        restoreSelection(menu);
        window.desktopAPI.runContextMenuCommand(command);
      }}
    >
      <ContextMenuContent
        anchor={anchor}
        side="bottom"
        align="start"
        alignOffset={0}
        finalFocus={() => menu.focus ?? false}
        className="min-w-44"
      >
        {groups.map((group, index) => (
          <Fragment key={index}>
            {index > 0 && <ContextMenuSeparator />}
            {group}
          </Fragment>
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
}
