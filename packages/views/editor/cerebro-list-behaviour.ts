/**
 * CEREBRO-PATCH(list-editing): Phase 6 slice 11 — keyboard, and paste rules for
 * list editing, on top of the existing `PatchedListItem` / `PatchedTaskItem`
 * (extensions/list-item.ts). Those already ship Tab / Shift-Tab indent-outdent,
 * the "Enter on an empty item exits the list" fallback, and the `[] ` / `[x] `
 * input rules; `- `, `* ` and `1. ` are StarterKit's own input rules. This file
 * adds only what is still missing, so nothing is duplicated:
 *
 *   - Backspace at the very start of a list item lifts it (repeated presses walk
 *     it out to a plain paragraph), instead of merging into the previous item.
 *   - ⌥↑ / ⌥↓ move the whole item — nested children included — past its sibling.
 *   - A multi-line plain-text paste dropped inside a list item becomes one
 *     sibling item per line, rather than one item full of line breaks. Pasted
 *     markdown list text is left to `markdownPaste`, which preserves its indent.
 *
 * Registered once from extensions/index.ts. Priority sits above markdownPaste so
 * the paste handler here gets first refusal on a paste landing inside a list.
 */
import { Extension } from "@tiptap/core";
import { Plugin, PluginKey, TextSelection } from "@tiptap/pm/state";
import type { ResolvedPos } from "@tiptap/pm/model";
import { useCerebroFeatureFlagsStore } from "@multica/cerebro-feature-flags";

const ITEM_TYPES = ["listItem", "taskItem"];

/**
 * Whether the Phase 6 list-editing behaviour is switched on.
 *
 * The editor-toolbar build (track A, Phase 4) owns the `cerebro_editor_toolbar`
 * flag and its registry default; track B must not touch that registry. So the
 * key is read defensively here — while it is unregistered or off the editor
 * keeps its stock list handling (default off preserves the existing editor per
 * cerebro rule 4), and this behaviour lights up once track A registers the flag
 * and a user turns it on. `createEditorExtensions` gates the extension on this.
 */
export function isListEditingEnabled(): boolean {
  const s = useCerebroFeatureFlagsStore.getState();
  const overrides = s.overrides as Record<string, boolean | undefined>;
  const workspace = s.workspaceOverrides as Record<string, boolean | undefined>;
  return (
    overrides.cerebro_editor_toolbar ??
    workspace.cerebro_editor_toolbar ??
    false
  );
}

/** Depth of the nearest enclosing list item, or -1 if the cursor is not in one. */
function enclosingItemDepth($pos: ResolvedPos): number {
  for (let d = $pos.depth; d > 0; d--) {
    if (ITEM_TYPES.includes($pos.node(d).type.name)) return d;
  }
  return -1;
}

// A line that is itself markdown block syntax (list marker, checkbox, heading,
// blockquote) belongs to markdownPaste — it round-trips the structure and indent.
const MARKDOWN_BLOCK_LINE = /^\s*([-*+]\s|\d+[.)]\s|\[[ xX]?\]\s|#{1,6}\s|>\s)/;

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    cerebroListBehaviour: {
      /** Move the enclosing list item (with children) up (-1) or down (1). */
      moveListItem: (dir: -1 | 1) => ReturnType;
      /** Lift the enclosing list item when the cursor is at its very start. */
      outdentListItemAtStart: () => ReturnType;
    };
  }
}

export const CerebroListBehaviour = Extension.create({
  name: "cerebroListBehaviour",
  // Above markdownPaste (default 100) so its handlePaste runs first.
  priority: 1000,

  addCommands() {
    return {
      moveListItem:
        (dir) =>
        ({ state, tr, dispatch }) => {
          const { $from } = state.selection;
          const d = enclosingItemDepth($from);
          if (d < 0) return false;

          const list = $from.node(d - 1);
          const index = $from.index(d - 1);
          const target = index + dir;
          if (target < 0 || target >= list.childCount) return false;
          if (!dispatch) return true;

          const item = $from.node(d);
          const from = $from.before(d);
          const to = $from.after(d);
          const offset = $from.pos - from; // keep the cursor inside the item

          if (dir === -1) {
            const insertPos = from - list.child(index - 1).nodeSize;
            tr.delete(from, to).insert(insertPos, item);
            tr.setSelection(
              TextSelection.near(tr.doc.resolve(insertPos + offset)),
            );
          } else {
            // After deleting the item, the next sibling shifts left to `from`.
            const insertPos = from + list.child(index + 1).nodeSize;
            tr.delete(from, to).insert(insertPos, item);
            tr.setSelection(
              TextSelection.near(tr.doc.resolve(insertPos + offset)),
            );
          }
          tr.scrollIntoView();
          return true;
        },

      outdentListItemAtStart:
        () =>
        ({ state, commands }) => {
          const { $from, empty } = state.selection;
          if (!empty) return false;
          const d = enclosingItemDepth($from);
          if (d < 0) return false;
          // Only at the very start of the item's first text block.
          if ($from.parentOffset !== 0 || $from.index(d) !== 0) return false;
          return commands.liftListItem($from.node(d).type.name);
        },
    };
  },

  addKeyboardShortcuts() {
    return {
      "Alt-ArrowUp": () => this.editor.commands.moveListItem(-1),
      "Alt-ArrowDown": () => this.editor.commands.moveListItem(1),
      Backspace: () => this.editor.commands.outdentListItemAtStart(),
    };
  },

  addProseMirrorPlugins() {
    const { editor } = this;
    return [
      new Plugin({
        key: new PluginKey("cerebroListPaste"),
        props: {
          handlePaste(view, event) {
            const clipboard = (event as ClipboardEvent).clipboardData;
            if (!clipboard) return false;
            const text = clipboard.getData("text/plain");
            if (!text) return false;
            // Internal ProseMirror copies carry real structure — leave them.
            const html = clipboard.getData("text/html");
            if (html && html.includes("data-pm-slice")) return false;

            const { $from, empty } = view.state.selection;
            if (!empty) return false;
            if (enclosingItemDepth($from) < 0) return false;

            const lines = text
              .replace(/\r\n?/g, "\n")
              .split("\n")
              .map((l) => l.trimEnd());
            const items = lines.filter((l) => l.trim() !== "");
            if (items.length < 2) return false; // single line: let others handle
            if (lines.some((l) => MARKDOWN_BLOCK_LINE.test(l))) return false;

            const itemDepth = enclosingItemDepth($from);
            const itemName = $from.node(itemDepth).type.name;
            // A non-empty item keeps its own text: the first pasted line becomes
            // a fresh sibling instead of being appended to that text. An empty
            // item is filled by the first line, then split for the rest.
            const itemHasText = $from.node(itemDepth).textContent.length > 0;
            const chain = editor.chain();
            items.forEach((line, i) => {
              if (i > 0 || itemHasText) chain.splitListItem(itemName);
              chain.insertContent(line);
            });
            chain.run();
            return true;
          },
        },
      }),
    ];
  },
});
