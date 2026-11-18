"use client";

import {
  useEffect,
  useRef,
  useState,
  type DragEvent as ReactDragEvent,
  type RefObject,
} from "react";
import type { Editor } from "@tiptap/core";
import type { EditorView } from "@tiptap/pm/view";
import { NodeSelection } from "@tiptap/pm/state";
import {
  Copy,
  GripVertical,
  Plus,
  Trash2,
} from "@multica/ui/icons";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import {
  TEXT_BLOCK_KINDS,
  canTurnInto,
  currentTextBlockKind,
  deleteBlock,
  duplicateBlock,
  openInsertMenuBelow,
  resolveBlockTarget,
  turnBlockInto,
  type BlockTarget,
  type TextBlockKind,
} from "./extensions/block-commands";
import { BLOCK_MENU_ITEMS } from "./extensions/block-insert-suggestion";

/** Height of the handle row; it is centered on the block's first line. */
const HANDLE_HEIGHT = 24;
/** Width of "+" and grip together, plus the gap to the block. */
const HANDLE_OFFSET = 48;
/** A bullet or number sits left of its list item's box; clear it. */
const LIST_MARKER_GAP = 20;

interface HandlePosition {
  target: BlockTarget;
  /** Hover area: from the handle to the block's left edge, the block's full height. */
  top: number;
  left: number;
  width: number;
  height: number;
  /** Offset of the buttons inside the area, centered on the first line. */
  buttonTop: number;
}

/** How long the handle survives the pointer leaving the editor on its way over. */
const HIDE_DELAY_MS = 200;

/** Vertical center of the block's first line, so the handle lines up with text. */
function firstLineCenter(view: EditorView, target: BlockTarget): number | null {
  const { node, pos } = target;
  let textStart: number | null = node.isTextblock ? pos + 1 : null;
  if (textStart === null) {
    node.descendants((child, offset) => {
      if (textStart !== null) return false;
      if (child.isTextblock) {
        textStart = pos + 1 + offset + 1;
        return false;
      }
      return true;
    });
  }
  if (textStart === null) return null;
  try {
    const coords = view.coordsAtPos(textStart);
    return (coords.top + coords.bottom) / 2;
  } catch {
    return null;
  }
}

/** Which block sits under the pointer's row, and where its handle goes. */
export function measureBlockHandle(
  editor: Editor,
  container: HTMLElement,
  clientX: number,
  clientY: number,
): HandlePosition | null {
  if (editor.isDestroyed || !editor.isEditable) return null;
  const { view } = editor;
  const editorRect = view.dom.getBoundingClientRect();
  if (clientY < editorRect.top || clientY > editorRect.bottom) return null;
  // Probe inside the text column so a pointer in the margin still finds its row.
  const left = Math.min(Math.max(clientX, editorRect.left + 1), editorRect.right - 1);
  const hit = view.posAtCoords({ left, top: clientY });
  if (!hit) return null;
  const target = resolveBlockTarget(view.state.doc, hit.inside >= 0 ? hit.inside : hit.pos);
  if (!target) return null;
  const dom = view.nodeDOM(target.pos);
  if (!(dom instanceof HTMLElement)) return null;
  const containerRect = container.getBoundingClientRect();
  const blockRect = dom.getBoundingClientRect();
  const center = firstLineCenter(view, target) ?? blockRect.top + HANDLE_HEIGHT / 2;
  const markerGap = target.node.type.name === "listItem" ? LIST_MARKER_GAP : 0;
  // The area runs right up to the block and down its whole height, so moving
  // the pointer left from any line of the block lands on the handle instead of
  // crossing empty margin, which would leave the editor and hide it.
  return {
    target,
    top: blockRect.top - containerRect.top,
    left: blockRect.left - containerRect.left - HANDLE_OFFSET - markerGap,
    width: HANDLE_OFFSET + markerGap,
    height: Math.max(blockRect.height, HANDLE_HEIGHT),
    buttonTop: Math.max(0, center - blockRect.top - HANDLE_HEIGHT / 2),
  };
}

/** The kind a list item reads as is its list's kind. */
function targetKind(editor: Editor, target: BlockTarget): TextBlockKind | null {
  if (target.node.type.name === "listItem") {
    return currentTextBlockKind(editor.state.doc.resolve(target.pos).parent);
  }
  return currentTextBlockKind(target.node);
}

const KIND_ICONS = new Map(BLOCK_MENU_ITEMS.map((item) => [item.kind, item.icon]));

/**
 * Hover handle for document blocks. "+" opens the `/` menu on a new line
 * below; the grip drags the block (ProseMirror moves it on drop) or opens
 * turn-into, duplicate and delete.
 */
export function BlockHandle({
  editor,
  containerRef,
}: {
  editor: Editor;
  containerRef: RefObject<HTMLDivElement | null>;
}) {
  const { t } = useT("editor");
  const [position, setPosition] = useState<HandlePosition | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const gripRef = useRef<HTMLButtonElement>(null);
  // While the menu is open or a drag is in flight the handle must stay on its
  // block; the listeners read these without re-subscribing.
  const pinnedRef = useRef(false);
  pinnedRef.current = menuOpen;
  const draggingRef = useRef(false);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    let frame = 0;
    let hideTimer: ReturnType<typeof setTimeout> | undefined;
    const cancelHide = () => {
      clearTimeout(hideTimer);
      hideTimer = undefined;
    };
    const hide = () => {
      cancelHide();
      if (pinnedRef.current || draggingRef.current) return;
      cancelAnimationFrame(frame);
      setPosition(null);
    };
    // Leaving the editor is often the pointer heading for the handle along a
    // path that grazes outside it; give it a moment to arrive.
    const hideSoon = () => {
      cancelHide();
      hideTimer = setTimeout(hide, HIDE_DELAY_MS);
    };
    const onMove = (event: MouseEvent) => {
      cancelHide();
      // A held button means a text selection or a drag is under way; moving
      // the handle then would detach the drag source.
      if (pinnedRef.current || draggingRef.current || event.buttons !== 0) return;
      if ((event.target as Element | null)?.closest?.("[data-block-handle]")) return;
      const { clientX, clientY } = event;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        setPosition(measureBlockHandle(editor, container, clientX, clientY));
      });
    };
    container.addEventListener("mousemove", onMove);
    container.addEventListener("mouseleave", hideSoon);
    // Typing and remote updates move blocks under a handle; hide until the
    // pointer moves again.
    editor.view.dom.addEventListener("keydown", hide);
    editor.on("update", hide);
    return () => {
      cancelAnimationFrame(frame);
      cancelHide();
      container.removeEventListener("mousemove", onMove);
      container.removeEventListener("mouseleave", hideSoon);
      editor.view.dom.removeEventListener("keydown", hide);
      editor.off("update", hide);
    };
  }, [editor, containerRef]);

  if (!position) return null;
  const { target } = position;
  const current = targetKind(editor, target);

  const run = (action: () => void) => {
    action();
    setMenuOpen(false);
    setPosition(null);
  };

  const onDragStart = (event: ReactDragEvent<HTMLButtonElement>) => {
    const { view } = editor;
    // Not dispatched as the editor selection: a selected block would raise the
    // formatting toolbar over the drop targets for the whole drag.
    const selection = NodeSelection.create(view.state.doc, target.pos);
    const slice = selection.content();
    const { dom, text } = view.serializeForClipboard(slice);
    event.dataTransfer.clearData();
    event.dataTransfer.setData("text/html", dom.innerHTML);
    event.dataTransfer.setData("text/plain", text);
    event.dataTransfer.effectAllowed = "copyMove";
    const blockDom = view.nodeDOM(target.pos);
    if (blockDom instanceof HTMLElement) event.dataTransfer.setDragImage(blockDom, 0, 0);
    // ProseMirror's own drop handler reads this: it inserts the slice at the
    // drop point and removes the original through `node`. `node` is part of
    // prosemirror-view's runtime Dragging but missing from its typings.
    view.dragging = { slice, move: true, node: selection } as EditorView["dragging"];
    draggingRef.current = true;
  };

  const onDragEnd = () => {
    draggingRef.current = false;
    setPosition(null);
  };

  return (
    <div
      data-block-handle=""
      className="absolute z-10"
      style={{
        top: position.top,
        left: position.left,
        width: position.width,
        height: position.height,
      }}
    >
      <div
        className="absolute left-0 flex items-center gap-0.5"
        style={{ top: position.buttonTop, height: HANDLE_HEIGHT }}
      >
        <button
          type="button"
          aria-label={t(($) => $.block_handle.add)}
          title={t(($) => $.block_handle.add)}
          className="flex size-6 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          // Keep the caret in the editor; the click places it on the new line.
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => run(() => openInsertMenuBelow(editor, target))}
        >
          <Plus className="size-4" />
        </button>
        <button
          ref={gripRef}
          type="button"
          draggable
          aria-label={t(($) => $.block_handle.options)}
          title={t(($) => $.block_handle.options)}
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          className="flex h-6 w-4 cursor-grab items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground active:cursor-grabbing"
          onClick={() => setMenuOpen(true)}
          onDragStart={onDragStart}
          onDragEnd={onDragEnd}
        >
          <GripVertical className="size-4" />
        </button>
      </div>
      <DropdownMenu
        open={menuOpen}
        onOpenChange={(open) => {
          setMenuOpen(open);
          if (!open) setPosition(null);
        }}
      >
        <DropdownMenuContent anchor={gripRef} align="start" className="min-w-44">
          {canTurnInto(target.node) && (
            <>
              <DropdownMenuGroup>
                <DropdownMenuLabel>{t(($) => $.block_handle.turn_into)}</DropdownMenuLabel>
                {/* A submenu would be closed as a "sibling" of this anchored,
                    trigger-less menu, so the block types sit inline as icons. */}
                <div className="grid grid-cols-5 gap-0.5 px-1 pb-1">
                  {TEXT_BLOCK_KINDS.map((kind) => {
                    const Icon = KIND_ICONS.get(kind);
                    const label = t(($) => $.block_menu.blocks[kind]);
                    return (
                      <DropdownMenuItem
                        key={kind}
                        aria-label={label}
                        title={label}
                        className={cn(
                          "size-8 justify-center p-0",
                          current === kind && "bg-accent text-accent-foreground",
                        )}
                        onClick={() => run(() => turnBlockInto(editor, target, kind))}
                      >
                        {Icon && <Icon />}
                      </DropdownMenuItem>
                    );
                  })}
                </div>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
            </>
          )}
          <DropdownMenuItem onClick={() => run(() => duplicateBlock(editor, target))}>
            <Copy />
            {t(($) => $.block_handle.duplicate)}
          </DropdownMenuItem>
          <DropdownMenuItem
            variant="destructive"
            onClick={() => run(() => deleteBlock(editor, target))}
          >
            <Trash2 />
            {t(($) => $.block_handle.delete)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
