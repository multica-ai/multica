/**
 * CEREBRO-PATCH(list-editing): FIR-4707 Phase 6 slice 13 — touch gestures for
 * list items on a phone. A horizontal swipe past the threshold indents (right)
 * or outdents (left) the item; a swipe is ignored while the user is selecting
 * text, so drag-to-select is never hijacked. Long-press-and-drag to move is
 * handled by the drag handle's own pointer path (@tiptap/extension-drag-handle-
 * react), verified by track C.
 *
 * `resolveHorizontalSwipe` is the pure decision the touch handler makes, kept
 * separate so the threshold and the selection guard are unit-tested; the DOM
 * touch wiring in useCerebroListSwipe is verified by the track-C browser run.
 */
import { useEffect } from "react";
import type { Editor } from "@tiptap/react";
import { isListEditingEnabled } from "./cerebro-list-behaviour";

/** Minimum horizontal travel (px) before a swipe indents or outdents. */
export const SWIPE_THRESHOLD_PX = 40;

export type SwipeAction = "indent" | "outdent" | null;

/**
 * Decide what a horizontal touch travel of `dx` px means for a list item.
 * Right (dx > 0) indents, left (dx < 0) outdents; travel shorter than the
 * threshold, or a gesture made while selecting text, does nothing.
 */
export function resolveHorizontalSwipe(
  dx: number,
  opts: { selecting: boolean; threshold?: number },
): SwipeAction {
  if (opts.selecting) return null;
  const threshold = opts.threshold ?? SWIPE_THRESHOLD_PX;
  if (Math.abs(dx) < threshold) return null;
  return dx > 0 ? "indent" : "outdent";
}

function indentAt(editor: Editor) {
  editor
    .chain()
    .focus()
    .command(({ commands }) => {
      commands.sinkListItem("taskItem");
      commands.sinkListItem("listItem");
      return true;
    })
    .run();
}

function outdentAt(editor: Editor) {
  editor
    .chain()
    .focus()
    .command(({ commands }) => {
      commands.liftListItem("taskItem");
      commands.liftListItem("listItem");
      return true;
    })
    .run();
}

/**
 * Attach the swipe-to-indent/outdent touch handler to a touch device's editor.
 * A no-op on non-touch pointers. The pointer plumbing is verified by track C;
 * the indent/outdent decision is `resolveHorizontalSwipe`, unit-tested.
 */
export function useCerebroListSwipe(editor: Editor | null) {
  useEffect(() => {
    if (!editor || !isListEditingEnabled()) return;
    const dom = editor.view.dom;
    let startX = 0;
    let startY = 0;
    let tracking = false;

    const onStart = (e: TouchEvent) => {
      if (e.touches.length !== 1) return;
      startX = e.touches[0]!.clientX;
      startY = e.touches[0]!.clientY;
      tracking = true;
    };
    const onEnd = (e: TouchEvent) => {
      if (!tracking) return;
      tracking = false;
      const t = e.changedTouches[0];
      if (!t) return;
      const dx = t.clientX - startX;
      const dy = t.clientY - startY;
      // Vertical-dominant travel is a scroll, not a swipe.
      if (Math.abs(dy) > Math.abs(dx)) return;
      const selecting = !editor.state.selection.empty;
      const action = resolveHorizontalSwipe(dx, { selecting });
      if (action === "indent") indentAt(editor);
      else if (action === "outdent") outdentAt(editor);
    };

    dom.addEventListener("touchstart", onStart, { passive: true });
    dom.addEventListener("touchend", onEnd, { passive: true });
    return () => {
      dom.removeEventListener("touchstart", onStart);
      dom.removeEventListener("touchend", onEnd);
    };
  }, [editor]);
}
