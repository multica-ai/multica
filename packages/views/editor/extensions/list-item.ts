import { ListItem } from "@tiptap/extension-list";

/**
 * Patched ListItem with proper "double-Enter exits list" behaviour.
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
 */
export const PatchedListItem = ListItem.extend({
  addKeyboardShortcuts() {
    return {
      Enter: () =>
        this.editor.commands.first(({ commands }) => [
          () => commands.splitListItem(this.name),
          () => commands.liftListItem(this.name),
        ]),
      Tab: () => this.editor.commands.sinkListItem(this.name),
      "Shift-Tab": () => this.editor.commands.liftListItem(this.name),
    };
  },
});
