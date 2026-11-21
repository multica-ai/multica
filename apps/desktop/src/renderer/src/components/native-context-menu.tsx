import { Fragment, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { Code, Globe, ImageIcon, Link2, Table2 } from "@multica/ui/icons";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
} from "@multica/ui/components/ui/context-menu";
import { copyText } from "@multica/ui/lib/clipboard";
import { createShortcutChord, formatShortcut } from "@multica/core/shortcuts";
import { useT } from "@multica/views/i18n";
import {
  ContextMenuScopeSubmenu,
  findContextMenuScope,
  type ResolvedContextMenuScope,
  type RowActionItem,
} from "@multica/views/common/context-menu-scope";
import { contextMenuOriginRef } from "@multica/ui/lib/context-menu-origin";
import { contextPrimitives } from "@multica/views/common/menu-primitives";
import {
  contextMenuActions,
  hasContextMenuItems,
  type ContextMenuAction,
  type ContextMenuCommand,
  type ContextMenuRequest,
} from "../../../shared/context-menu";
import { useKeyboardContextMenu } from "../hooks/use-keyboard-context-menu";

/** A request older than this no longer belongs to the last DOM right-click. */
const POINT_MAX_AGE_MS = 1000;

/** What the page itself adds to a right-click outside a text field. */
interface PageItems {
  linkUrl: string | null;
  scope: ResolvedContextMenuScope;
  selection: RowActionItem[];
  code: string | null;
  table: string | null;
}

interface OpenMenu {
  request: ContextMenuRequest;
  point: { x: number; y: number };
  /** Focus and selection when the menu opened; an edit command needs both back. */
  focus: HTMLElement | null;
  ranges: Range[];
  /** The element the menu is for; overlays around it treat the menu as their own. */
  origin: Element | null;
  page: PageItems | null;
}

type Pending = { kind: "command"; command: ContextMenuCommand } | { kind: "local"; run: () => void };

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

function cellText(cell: HTMLTableCellElement): string {
  return (cell.textContent ?? "").trim().replace(/\s+/g, " ").replace(/\|/g, "\\|");
}

/** A rendered table as Markdown, its first row as the header. */
export function tableToMarkdown(table: HTMLTableElement): string {
  const rows = Array.from(table.rows, (row) => Array.from(row.cells, cellText));
  if (rows.length === 0) return "";
  const width = Math.max(...rows.map((row) => row.length));
  const line = (cells: string[]) =>
    `| ${Array.from({ length: width }, (_, i) => cells[i] ?? "").join(" | ")} |`;
  const [header, ...body] = rows;
  return [line(header!), line(Array(width).fill("---")), ...body.map(line)].join("\n");
}

function readPageItems(target: Element | null, request: ContextMenuRequest, selectedText: string): PageItems | null {
  // A text field only gets its edit menu.
  if (request.isEditable) return null;
  const scope = findContextMenuScope(target);
  const text = selectedText.trim();
  const pre = target?.closest("pre");
  const table = target?.closest("table");
  return {
    linkUrl: request.hasLink ? (target?.closest("a[href]") as HTMLAnchorElement | null)?.href ?? null : null,
    scope,
    selection: request.canCopy && text ? scope.selectionActions(text) : [],
    code: pre ? (pre.querySelector("code") ?? pre).textContent : null,
    table: table ? tableToMarkdown(table) : null,
  };
}

function hasPageItems(page: PageItems | null): boolean {
  if (!page) return false;
  return (
    !!page.scope.submenu ||
    page.scope.contentActions.length > 0 ||
    page.selection.length > 0 ||
    !!page.code ||
    !!page.table
  );
}

/**
 * Draws the menu for a right-click the page left unhandled — a text field,
 * selected text, a link, an image, code, a table, an attachment — which main
 * forwards instead of popping a native menu, so every menu in the app shares
 * one language and one look. Main's items act on its own copy of the click;
 * the page adds what only it knows: the attachment's own actions, the code to
 * copy, and the object around the content (a comment) as one submenu. Picking
 * an item runs it once the menu has closed and focus and selection are back
 * where the right-click found them, since cut, copy and paste act on whatever
 * holds them. Shift+F10 and the menu key open the focused element's menu.
 */
export function NativeContextMenu() {
  const { t } = useT("layout");
  useKeyboardContextMenu();
  const [menu, setMenu] = useState<OpenMenu | null>(null);
  const [open, setOpen] = useState(false);
  const lastEvent = useRef<{
    x: number;
    y: number;
    at: number;
    target: Element | null;
    selectedText: string;
  } | null>(null);
  const pending = useRef<Pending | null>(null);

  useEffect(() => {
    // Capture phase, so a right-click is seen even when a handler stops it.
    // The DOM position is in the page's own coordinates; Electron's is not
    // once the page is zoomed. The selection is read before the event's
    // default action, which on macOS selects the word under the cursor: only
    // text the user selected gets the selection entries.
    const record = (event: MouseEvent) => {
      lastEvent.current = {
        x: event.clientX,
        y: event.clientY,
        at: performance.now(),
        target: event.target instanceof Element ? event.target : null,
        selectedText: window.getSelection()?.toString() ?? "",
      };
    };
    window.addEventListener("contextmenu", record, true);
    const unsubscribe = window.desktopAPI.onContextMenu((request) => {
      const last = lastEvent.current;
      const fresh = !!last && performance.now() - last.at < POINT_MAX_AGE_MS;
      const point = fresh ? { x: last.x, y: last.y } : { x: request.x, y: request.y };
      const active = document.activeElement;
      const selection = window.getSelection();
      const ranges = selection
        ? Array.from({ length: selection.rangeCount }, (_, i) => selection.getRangeAt(i).cloneRange())
        : [];
      const page = readPageItems(
        fresh ? last.target : null,
        request,
        fresh ? last.selectedText : ranges.map((range) => range.toString()).join(""),
      );
      if (!hasContextMenuItems(request) && !hasPageItems(page)) return;
      pending.current = null;
      const focus = active instanceof HTMLElement && active !== document.body ? active : null;
      setMenu({
        request,
        point,
        focus,
        ranges,
        origin: (fresh ? last.target : null) ?? focus,
        page,
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
  const origin = menu?.origin ?? null;
  const originRef = useMemo(() => contextMenuOriginRef(origin), [origin]);

  if (!menu) return null;

  const { request, page } = menu;
  const contentActions = page?.scope.contentActions ?? [];
  // An attachment's own Copy link replaces the browser's link entries, which
  // would hand out the raw signed URL.
  const actions = new Set(
    contextMenuActions(request).filter(
      (type) => contentActions.length === 0 || (type !== "openLink" && type !== "copyLink"),
    ),
  );
  const pick = (command: ContextMenuCommand) => {
    pending.current = { kind: "command", command };
  };
  const pickLocal = (run: () => void) => {
    pending.current = { kind: "local", run };
  };
  const copy = (text: string) => {
    void copyText(text).then((ok) =>
      ok ? toast.success(t(($) => $.context_menu.copied)) : toast.error(t(($) => $.context_menu.copy_failed)),
    );
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
  const localItem = (action: RowActionItem) => (
    <ContextMenuItem
      key={action.key}
      variant={action.danger ? "destructive" : "default"}
      onClick={() => pickLocal(action.onSelect)}
    >
      {action.icon}
      {action.label}
    </ContextMenuItem>
  );
  const submenu = page?.scope.submenu
    ? {
        ...page.scope.submenu,
        groups: page.scope.submenu.groups.map((group) =>
          group.map((action) => ({ ...action, onSelect: () => pickLocal(action.onSelect) })),
        ),
      }
    : null;

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
    contentActions.map(localItem),
    [item("cut"), item("copy"), item("paste"), ...(page?.selection ?? []).map(localItem)],
    [item("selectAll")],
    [item("openLink"), item("copyLink")],
    [item("copyImage")],
    [
      page?.code
        ? localItem({
            key: "copy-code",
            icon: <Code />,
            label: t(($) => $.context_menu.copy_code),
            onSelect: () => copy(page.code!),
          })
        : null,
      page?.table
        ? localItem({
            key: "copy-table",
            icon: <Table2 />,
            label: t(($) => $.context_menu.copy_table),
            onSelect: () => copy(page.table!),
          })
        : null,
    ],
  ]
    .map((group) => group.filter(Boolean))
    .filter((group) => group.length > 0);

  return (
    <ContextMenu
      open={open}
      onOpenChange={setOpen}
      onOpenChangeComplete={(isOpen) => {
        if (isOpen) return;
        const picked = pending.current;
        pending.current = null;
        if (!picked) return;
        restoreSelection(menu);
        if (picked.kind === "command") window.desktopAPI.runContextMenuCommand(picked.command);
        else picked.run();
      }}
    >
      <ContextMenuContent
        ref={originRef}
        anchor={anchor}
        side="bottom"
        align="start"
        alignOffset={0}
        // A page action moves focus itself — into a reply box, an emoji picker.
        finalFocus={() => (pending.current?.kind === "local" ? false : menu.focus ?? false)}
        className="w-auto min-w-44 max-w-80"
      >
        {page?.linkUrl && (
          <div className="truncate px-2 py-1.5 text-xs text-muted-foreground" title={page.linkUrl}>
            {page.linkUrl}
          </div>
        )}
        {groups.map((group, index) => (
          <Fragment key={index}>
            {(index > 0 || !!page?.linkUrl) && <ContextMenuSeparator />}
            {group}
          </Fragment>
        ))}
        <ContextMenuScopeSubmenu
          submenu={submenu}
          primitives={contextPrimitives}
          separated={groups.length > 0 || !!page?.linkUrl}
        />
      </ContextMenuContent>
    </ContextMenu>
  );
}
