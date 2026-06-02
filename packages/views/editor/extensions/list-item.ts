import type { Editor } from "@tiptap/core";
import { ListItem, TaskItem } from "@tiptap/extension-list";

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
 * Tab / Shift-Tab indent / dedent the item.
 */
function listItemKeymap(editor: Editor, name: string) {
  return {
    Enter: () =>
      editor.commands.first(({ commands }) => [
        () => commands.splitListItem(name),
        () => commands.liftListItem(name),
      ]),
    Tab: () => editor.commands.sinkListItem(name),
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
 */
export const PatchedTaskItem = TaskItem.extend({
  addKeyboardShortcuts() {
    return listItemKeymap(this.editor, this.name);
  },
}).configure({ nested: true });
