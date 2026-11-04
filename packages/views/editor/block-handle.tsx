"use client";

/**
 * The ⋮⋮ handle in the left gutter of document editors. It follows the
 * top-level block under the pointer: drag it to move the block, click it for
 * turn-into, duplicate and delete.
 */

import { useEffect, useState, type DragEvent as ReactDragEvent, type RefObject } from "react";
import type { Editor } from "@tiptap/core";
import { NodeSelection, TextSelection } from "@tiptap/pm/state";
import { Copy, GripVertical, Trash2 } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../i18n";
import { BLOCK_TYPE_ITEMS, applyBlockCommand, blockMenuLabel, type BlockTypeId } from "./extensions/block-menu";

const HANDLE_HEIGHT = 24;
const TEXTBLOCKS = new Set(["paragraph", "heading"]);

interface HoveredBlock {
  pos: number;
  /** Handle offset from the container's top, centred on the first line. */
  top: number;
}

/** The top-level block whose row is at `clientY`, or null. */
export function topLevelBlockAt(editor: Editor, clientY: number): number | null {
  const { view } = editor;
  const box = view.dom.getBoundingClientRect();
  const found = view.posAtCoords({ left: box.left + Math.min(16, box.width / 2), top: clientY });
  if (!found) return null;
  const $pos = view.state.doc.resolve(found.pos);
  if ($pos.depth > 0) return $pos.before(1);
  return $pos.nodeAfter ? found.pos : null;
}

function firstLineHeight(element: HTMLElement): number {
  const lineHeight = parseFloat(getComputedStyle(element).lineHeight);
  return Number.isFinite(lineHeight) ? lineHeight : HANDLE_HEIGHT;
}

export function BlockHandle({ editor, containerRef }: { editor: Editor; containerRef: RefObject<HTMLDivElement | null> }) {
  const { t } = useT("editor");
  const [block, setBlock] = useState<HoveredBlock | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const onMove = (event: MouseEvent) => {
      if (menuOpen || !editor.isEditable || editor.view.dragging) return;
      if (event.target instanceof Element && event.target.closest("[data-block-handle]")) return;
      const pos = topLevelBlockAt(editor, event.clientY);
      const dom = pos === null ? null : editor.view.nodeDOM(pos);
      if (pos === null || !(dom instanceof HTMLElement)) {
        setBlock(null);
        return;
      }
      const rect = dom.getBoundingClientRect();
      const origin = container.getBoundingClientRect();
      const lineHeight = firstLineHeight(dom);
      const top = Math.round(rect.top - origin.top + Math.max(0, (lineHeight - HANDLE_HEIGHT) / 2));
      setBlock((prev) => (prev?.pos === pos && prev.top === top ? prev : { pos, top }));
    };
    const onLeave = () => {
      if (!menuOpen) setBlock(null);
    };
    container.addEventListener("mousemove", onMove);
    container.addEventListener("mouseleave", onLeave);
    return () => {
      container.removeEventListener("mousemove", onMove);
      container.removeEventListener("mouseleave", onLeave);
    };
  }, [containerRef, editor, menuOpen]);

  // Typing moves blocks under a resting pointer; the handle waits for the
  // next mouse move instead of pointing at a stale block.
  useEffect(() => {
    const hide = ({ transaction }: { transaction: { docChanged: boolean } }) => {
      if (transaction.docChanged && !menuOpen) setBlock(null);
    };
    editor.on("transaction", hide);
    return () => {
      editor.off("transaction", hide);
    };
  }, [editor, menuOpen]);

  if (!block || !editor.isEditable) return null;
  const node = editor.state.doc.nodeAt(block.pos);
  if (!node) return null;
  const isTextblock = TEXTBLOCKS.has(node.type.name);

  const turnInto = (id: BlockTypeId) => {
    applyBlockCommand(
      editor.chain().command(({ tr }) => {
        tr.setSelection(TextSelection.near(tr.doc.resolve(block.pos + 1)));
        return true;
      }),
      id,
    ).run();
  };
  const duplicate = () => {
    editor
      .chain()
      .command(({ tr }) => {
        tr.insert(block.pos + node.nodeSize, node.copy(node.content));
        return true;
      })
      .run();
  };
  const remove = () => {
    editor
      .chain()
      .command(({ tr }) => {
        tr.delete(block.pos, block.pos + node.nodeSize);
        return true;
      })
      .run();
    setBlock(null);
  };

  const onDragStart = (event: ReactDragEvent<HTMLButtonElement>) => {
    const { view } = editor;
    const selection = NodeSelection.create(view.state.doc, block.pos);
    view.dispatch(view.state.tr.setSelection(selection));
    const slice = selection.content();
    const { dom, text } = view.serializeForClipboard(slice);
    event.dataTransfer.clearData();
    event.dataTransfer.setData("text/html", dom.innerHTML);
    event.dataTransfer.setData("text/plain", text);
    event.dataTransfer.effectAllowed = "copyMove";
    const blockDom = view.nodeDOM(block.pos);
    if (blockDom instanceof HTMLElement) event.dataTransfer.setDragImage(blockDom, 0, 0);
    // ProseMirror's own drop handler moves the block: it deletes the dragged
    // node selection and inserts the slice where it lands.
    view.dragging = { slice, move: true, node: selection } as typeof view.dragging;
    setMenuOpen(false);
  };
  const onDragEnd = () => {
    // A drop outside the editor never reaches ProseMirror's drop handler,
    // which is what clears this.
    editor.view.dragging = null;
    setBlock(null);
  };

  const label = t(($) => $.block_handle.label);

  return (
    // 18px outside the content: dialog editors leave a 20px gutter that clips.
    <div data-block-handle="" className="absolute left-0 z-10 -translate-x-[calc(100%+0.125rem)]" style={{ top: block.top }}>
      <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              draggable
              aria-label={label}
              title={label}
              className="flex h-6 w-4 cursor-grab items-center justify-center rounded-sm text-muted-foreground/70 transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing"
              onDragStart={onDragStart}
              onDragEnd={onDragEnd}
            >
              <GripVertical className="size-4" />
            </button>
          }
        />
        <DropdownMenuContent
          align="start"
          className="w-auto min-w-44"
          finalFocus={() => (editor.isDestroyed ? false : (editor.view.dom as HTMLElement))}
        >
          {isTextblock && (
            <>
              <DropdownMenuGroup>
                <DropdownMenuLabel>{t(($) => $.block_menu.turn_into)}</DropdownMenuLabel>
                {BLOCK_TYPE_ITEMS.map((item) => {
                  const Icon = item.icon;
                  return (
                    <DropdownMenuItem key={item.id} onClick={() => turnInto(item.id as BlockTypeId)}>
                      <Icon className="text-muted-foreground" />
                      {blockMenuLabel(t, item.id)}
                    </DropdownMenuItem>
                  );
                })}
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
            </>
          )}
          <DropdownMenuItem onClick={duplicate}>
            <Copy className="text-muted-foreground" />
            {t(($) => $.block_handle.duplicate)}
          </DropdownMenuItem>
          <DropdownMenuItem variant="destructive" onClick={remove}>
            <Trash2 />
            {t(($) => $.block_handle.delete)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
