import { type Editor, InputRule } from "@tiptap/core";
import { ListItem, TaskItem } from "@tiptap/extension-list";
import { sinkListItem as pmSinkListItem } from "@tiptap/pm/schema-list";
import { type Command, TextSelection } from "@tiptap/pm/state";
import type { NodeType } from "@tiptap/pm/model";

/**
 * Tab indent that also works for a multi-item selection whose first item is the
 * first child of its (sub)list.
 *
 * Stock `sinkListItem` (prosemirror-schema-list) bails — returns false without
 * dispatching — whenever `range.startIndex === 0`, because the first item has no
 * preceding sibling to nest under. That is correct for a collapsed cursor in the
 * first item, but it also kills the natural "select the whole list from the top
 * and press Tab" gesture: the command sees the first item at index 0 and does
 * nothing (MUL-3697).
 *
 * The structurally-correct behaviour in a nested-list model (matching Notion /
 * GitHub) is: keep the first selected item as an anchor and sink the rest under
 * it. We get that by re-running the *stock* command on a selection narrowed to
 * start inside the SECOND selected item (so its `startIndex` becomes 1) while
 * keeping the original `$to`. The narrowed selection is computed on a derived
 * state and never dispatched on its own, so the whole operation is a single
 * dispatch / single undo step.
 *
 * Shift-Tab / `liftListItem` has no equivalent limitation (it handles ranges and
 * the first-item case correctly), so only Tab needs this wrapper.
 */
function sinkListItemRange(itemType: NodeType): Command {
  return (state, dispatch) => {
    // Normal path — cursor or range not starting at the first item. This also
    // covers the genuine no-op for a collapsed cursor in the first item: stock
    // returns false and the fallback guards below also return false.
    if (pmSinkListItem(itemType)(state, dispatch)) return true;

    const { $from, $to } = state.selection;
    const range = $from.blockRange(
      $to,
      (node) => node.childCount > 0 && node.firstChild?.type === itemType,
    );
    // Clean false (no dispatch) when the fallback does not apply: no list range,
    // the range does not start at the first item, fewer than two items are
    // selected, or the item type does not match (C2).
    if (!range) return false;
    if (range.startIndex !== 0) return false;
    if (range.endIndex - range.startIndex < 2) return false;
    if (range.parent.child(range.startIndex).type !== itemType) return false;

    // Move $from into the second selected item, keep $to in the last selected
    // item (C1 — do not collapse onto the second item). +2 steps over the
    // <listItem> + <paragraph> open tokens into inline content; `between` snaps
    // to a valid text position if the item does not start with a paragraph.
    const secondItemStart =
      range.start + range.parent.child(range.startIndex).nodeSize + 2;
    const narrowed = state.apply(
      state.tr.setSelection(
        TextSelection.between(state.doc.resolve(secondItemStart), $to),
      ),
    );
    return pmSinkListItem(itemType)(narrowed, dispatch);
  };
}

/**
 * Shared list keymap with proper "double-Enter exits list" behaviour.
 *
 * Tiptap's stock `Enter: splitListItem` is incomplete. `splitListItem` itself
 * returns false (without dispatching) when the cursor sits in an empty
 * TOP-LEVEL list item, with a code comment saying "bail out and let next
 * command handle lifting" — but the stock keymap has no next command.
 * The empty Enter then falls through to ProseMirror's baseKeymap (`splitBlock`),
 * which just inserts another empty paragraph inside the list item, trapping
 * the user.
 *
 * Fix: chain `splitListItem` → `liftListItem` via `commands.first`. The lift
 * fallback only runs when `splitListItem` returns false (top-level empty
 * item), matching the universal editor behaviour where a second Enter on an
 * empty bullet exits the list as a plain paragraph. Non-empty and nested
 * empty items are unaffected because `splitListItem` handles them correctly
 * and returns true.
 *
 * Tab indents the item(s) — see `sinkListItemRange` for the multi-item
 * first-item handling. Shift-Tab dedents via the stock command.
 */
function listItemKeymap(editor: Editor, name: string) {
  return {
    Enter: () =>
      editor.commands.first(({ commands }) => [
        () => commands.splitListItem(name),
        () => commands.liftListItem(name),
      ]),
    Tab: () => {
      const itemType = editor.schema.nodes[name];
      if (!itemType) return false;
      return sinkListItemRange(itemType)(editor.state, (tr) =>
        editor.view.dispatch(tr),
      );
    },
    "Shift-Tab": () => editor.commands.liftListItem(name),
  };
}

export const PatchedListItem = ListItem.extend({
  addKeyboardShortcuts() {
    return listItemKeymap(this.editor, this.name);
  },
});

/**
 * Patched TaskItem — same "double-Enter exits list" fix as PatchedListItem,
 * applied to checkbox task items so they behave identically to bullet/ordered
 * lists. `nested: true` lets a task item hold nested lists (so Tab indents into
 * a sub-task and nested markdown round-trips), matching GitHub / Notion.
 *
 * It also adds the GitHub-style `- [ ] ` typing flow. TaskItem's built-in input
 * rule only converts `[ ] ` / `[x] ` typed at the start of a PLAIN paragraph;
 * once a leading `- ` (or `1. `) has turned the line into a bullet/ordered
 * item, that rule no longer fires and `[ ]` stays as literal text. The extra
 * rule below catches the checkbox token when it is the entire content of a
 * freshly-typed list item and converts just that item into a task item —
 * sibling items in the same list are left untouched (lift then re-wrap).
 */
export const PatchedTaskItem = TaskItem.extend({
  addKeyboardShortcuts() {
    return listItemKeymap(this.editor, this.name);
  },
  addInputRules() {
    return [
      ...(this.parent?.() ?? []),
      new InputRule({
        find: /^\[([ xX])?\]\s$/,
        handler: ({ state, range, match, chain }) => {
          // Only when the checkbox token is the whole content of a list item.
          // Plain-paragraph typing is handled by the inherited rule above; the
          // anchored regex guarantees there is no other text before it. When
          // the guard fails the handler leaves the transaction untouched, so
          // the rule is a no-op and ProseMirror falls through.
          if (state.selection.$from.node(-1)?.type.name === "listItem") {
            const checked = (match[1] ?? "").toLowerCase() === "x";
            chain()
              .deleteRange(range)
              .liftListItem("listItem")
              .toggleList("taskList", "taskItem")
              .updateAttributes("taskItem", { checked })
              .run();
          }
        },
      }),
    ];
  },
}).configure({ nested: true });
