import { Extension, type Editor } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";

/**
 * Records that the user *typed* a suggestion trigger character, so a picker can
 * refuse to open over a `@` or `/` that merely happens to sit in the document.
 *
 * ## Why this exists
 *
 * Tiptap's Suggestion plugin is state-derived, not event-driven: it re-runs
 * `findSuggestionMatch` on EVERY transaction and asks "does the text before the
 * cursor look like a trigger?" — never "did the user just type one?". Pasted,
 * dropped, undone and server-loaded text produce a document identical to typed
 * text, so the plugin cannot tell them apart. With `allowSpaces` the match then
 * runs from the `@` to the end of the line, so pasting `npx @scope/pkg --token=…`
 * opens an empty picker that swallows Enter (MUL-5429).
 *
 * That is upstream's known, unfixed design: ueberdosis/tiptap#4183 ("Suggestion
 * opens even if current user didn't type the trigger", open since 2023, labelled
 * `complexity: hard`) and #7371 ("Suggestion should only start when user
 * explicitly types suggestion char"). Upstream's answer was to expose
 * `shouldShow` and let applications supply the missing provenance themselves —
 * but `shouldShow` receives the transaction without `prev.active`, while `allow`
 * receives `prev.active` without the transaction, so no transaction-inspecting
 * predicate can express "only on the transaction that opened it". Provenance has
 * to come from outside the transaction stream.
 *
 * ## How it works
 *
 * `handleTextInput` is the one ProseMirror hook that fires for real keyboard and
 * IME input only — paste goes through `handlePaste`/`doPaste` and drop through
 * `handleDrop`, neither of which reaches it. This is the same signal Tiptap's own
 * InputRules run on, which is why typing `- ` makes a list but pasting it does
 * not. Suggestion is the one Tiptap feature that does not use it; this plugin
 * supplies it.
 *
 * Typing a trigger character arms its document position. The pickers' `shouldShow`
 * then opens only for a match anchored at the armed position. Anything the user
 * did not type is never armed, so it never opens a picker.
 */

/** Trigger characters any suggestion in this editor listens for. */
const TRIGGER_CHARS = new Set(["@", "/"]);

/**
 * Latest armed position per editor.
 *
 * `shouldShow` runs inside the Suggestion plugin's `apply`, where `editor.state`
 * is still the PREVIOUS state — so the armed position for the transaction being
 * applied is not yet readable through a PluginKey. This map mirrors it as the
 * arming plugin's `apply` computes it. Extension priority (below) puts that
 * `apply` ahead of every suggestion plugin, so the mirror is always current by
 * the time `shouldShow` reads it.
 */
const armedPositions = new WeakMap<Editor, number | null>();

/** True when `from` is where the user typed a trigger character. */
export function isTriggerArmedAt(editor: Editor, from: number): boolean {
  const armed = armedPositions.get(editor);
  return armed !== null && armed !== undefined && armed === from;
}

/** Offset of the last trigger character in typed text, or -1. */
function lastTriggerIndex(text: string): number {
  for (let i = text.length - 1; i >= 0; i--) {
    if (TRIGGER_CHARS.has(text[i]!)) return i;
  }
  return -1;
}

/** True when the armed position still holds its trigger character. */
function stillHoldsTrigger(doc: ProseMirrorNode, pos: number): boolean {
  if (pos < 0 || pos + 1 > doc.content.size) return false;
  return TRIGGER_CHARS.has(doc.textBetween(pos, pos + 1));
}

export const SuggestionTriggerArmingExtension = Extension.create({
  name: "suggestionTriggerArming",
  // Ahead of every suggestion plugin so both `handleTextInput` (which must see
  // the keystroke before an input rule can consume it) and `apply` (which must
  // refresh the mirror before `shouldShow` reads it) run first.
  priority: 1000,

  addProseMirrorPlugins() {
    const editor = this.editor;
    // Set by `handleTextInput` just before ProseMirror dispatches the insertion,
    // consumed by the very next `apply`. Holds a position in the NEW document:
    // text typed at `from` puts its i-th character at `from + i`.
    let pendingArm: number | null = null;

    return [
      new Plugin<number | null>({
        key: new PluginKey("suggestionTriggerArming"),

        state: {
          init() {
            armedPositions.set(editor, null);
            return null;
          },

          apply(tr, prev, _oldState, newState) {
            const next = ((): number | null => {
              // A trigger character was just typed — arm it unconditionally.
              if (pendingArm !== null) return pendingArm;
              if (prev === null) return null;

              // A deliberate caret move (click, arrow key) abandons the trigger.
              // Typing never sets the selection explicitly, so it survives.
              // IME excluded: composition dispatches selection transactions of
              // its own mid-word.
              if (tr.selectionSet && !tr.docChanged && !editor.view.composing) {
                return null;
              }

              const mapped = tr.docChanged ? tr.mapping.map(prev, -1) : prev;
              if (!stillHoldsTrigger(newState.doc, mapped)) return null;
              // Cursor no longer sits after the trigger inside the same query.
              if (!newState.selection.empty || newState.selection.from <= mapped) {
                return null;
              }
              return mapped;
            })();

            pendingArm = null;
            armedPositions.set(editor, next);
            return next;
          },
        },

        props: {
          handleTextInput(_view, from, _to, text) {
            const index = lastTriggerIndex(text);
            if (index !== -1) pendingArm = from + index;
            // Never handle the input — every other plugin must still see it.
            return false;
          },
        },
      }),
    ];
  },
});
