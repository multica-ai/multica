/**
 * CEREBRO-PATCH(list-editing): FIR-4707 Phase 6 slice 12 — the block-menu
 * commands behind the hover drag handle (cerebro-block-handle.tsx). These are
 * the actions that are testable against a headless editor: Duplicate, Delete
 * and Move a block, plus the multi-item variants when a selection spans several
 * list items. "Turn into", Indent and Outdent reuse the editor's own built-in
 * commands (setParagraph / toggleHeading / toggleList / liftListItem /
 * sinkListItem), and Comment / Create issue from item are host callbacks wired
 * in the React handle — none of those need new commands here.
 *
 * The drag handle's own pointer drag-to-move and drag-to-indent is provided by
 * @tiptap/extension-drag-handle-react and is verified by the track-C browser
 * run, not by these headless tests; `moveBlock` is the reorder the handle
 * ultimately performs, exposed as a command so the reorder is unit-covered.
 *
 * Registered from extensions/index.ts, gated on the same flag as the rest of
 * Phase 6 (isListEditingEnabled), so it ships dark until Phase 9.
 */
import { Extension } from "@tiptap/core";
import { TextSelection } from "@tiptap/pm/state";
import type { ResolvedPos, Node as PMNode } from "@tiptap/pm/model";

const ITEM_TYPES = ["listItem", "taskItem"];

/**
 * Depth of the node the block handle targets at `$pos`: the nearest enclosing
 * list item, or — outside a list — the top-level block (a direct child of the
 * doc). Returns -1 only for an empty document with no block at all.
 */
function handleTargetDepth($pos: ResolvedPos): number {
  for (let d = $pos.depth; d > 0; d--) {
    if (ITEM_TYPES.includes($pos.node(d).type.name)) return d;
  }
  return $pos.depth >= 1 ? 1 : -1;
}

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    cerebroBlockActions: {
      /** Insert a copy of the targeted block (with children) right after it. */
      duplicateBlock: () => ReturnType;
      /** Delete the targeted block, or every block a selection spans. */
      deleteBlock: () => ReturnType;
      /** Move the targeted block (with children) up (-1) or down (1). */
      moveBlock: (dir: -1 | 1) => ReturnType;
    };
  }
}

export const CerebroBlockActions = Extension.create({
  name: "cerebroBlockActions",

  addCommands() {
    return {
      duplicateBlock:
        () =>
        ({ state, tr, dispatch }) => {
          const { $from } = state.selection;
          const d = handleTargetDepth($from);
          if (d < 0) return false;
          const node = $from.node(d);
          const to = $from.after(d);
          if (!dispatch) return true;
          tr.insert(to, node);
          dispatch(tr);
          return true;
        },

      deleteBlock:
        () =>
        ({ state, tr, dispatch }) => {
          const { $from, $to } = state.selection;
          const dFrom = handleTargetDepth($from);
          const dTo = handleTargetDepth($to);
          if (dFrom < 0 || dTo < 0) return false;
          // A selection spanning several blocks deletes all of them.
          const from = $from.before(dFrom);
          const to = $to.after(dTo);
          if (!dispatch) return true;
          tr.delete(from, to);
          dispatch(tr);
          return true;
        },

      moveBlock:
        (dir) =>
        ({ state, tr, dispatch }) => {
          const { $from } = state.selection;
          const d = handleTargetDepth($from);
          if (d < 0) return false;

          const parent: PMNode = $from.node(d - 1);
          const index = $from.index(d - 1);
          const target = index + dir;
          if (target < 0 || target >= parent.childCount) return false;
          if (!dispatch) return true;

          const node = $from.node(d);
          const from = $from.before(d);
          const to = $from.after(d);
          const offset = $from.pos - from; // keep the cursor inside the block
          const insertPos =
            dir === -1
              ? from - parent.child(index - 1).nodeSize
              : from + parent.child(index + 1).nodeSize;

          tr.delete(from, to).insert(insertPos, node);
          tr.setSelection(TextSelection.near(tr.doc.resolve(insertPos + offset)));
          tr.scrollIntoView();
          dispatch(tr);
          return true;
        },
    };
  },
});
