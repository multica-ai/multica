import { type Editor, InputRule } from "@tiptap/core";
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
