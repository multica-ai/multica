import { Extension, type Editor } from "@tiptap/core";
import { Plugin, PluginKey, Selection } from "@tiptap/pm/state";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import type { EditorView } from "@tiptap/pm/view";

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
 * `handleTextInput` is ProseMirror's primary hook for real keyboard and IME
 * input — paste goes through `handlePaste`/`doPaste` and drop through
 * `handleDrop`, neither of which reaches it. Chrome has one exception: after a
 * handled select-all/delete in a contenteditable, the next printable key can be
 * reconciled from the DOM without calling `handleTextInput`. A native capture
 * listener on the editor DOM supplies that missing provenance even when
 * ProseMirror skips its own event-routing chain; the following transaction
 * must still prove that the trigger character was actually inserted before it
 * is armed.
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

function selectsWholeDocument(view: EditorView): boolean {
  const { selection, doc } = view.state;
  return (
    !selection.empty &&
    selection.from === 0 &&
    selection.to === doc.content.size
  );
}

/**
 * Position where a printable key will replace/insert text according to the
 * browser's live selection.
 *
 * After Chrome handles select-all + Backspace, the DOM caret is already back
 * inside the empty paragraph while ProseMirror can still expose an
 * `AllSelection` at position 0 until its DOM observer reconciles. The next key
 * is inserted at position 1, so arming from `view.state.selection.from` would
 * record the wrong position and reject a genuine trigger.
 */
function domInsertionPosition(view: EditorView): number {
  const fallback = view.state.selection.from;
  // Chrome leaves the ProseMirror selection stale at AllSelection after the
  // browser empties the contenteditable. Its next printable key is placed in
  // the schema-preserved first paragraph, not at AllSelection.from (0).
  if (selectsWholeDocument(view)) {
    return Selection.atStart(view.state.doc).from;
  }

  const selection = view.dom.ownerDocument.getSelection();
  if (!selection?.anchorNode || !selection.focusNode) return fallback;
  if (
    !view.dom.contains(selection.anchorNode) ||
    !view.dom.contains(selection.focusNode)
  ) {
    return fallback;
  }

  try {
    const anchor = view.posAtDOM(selection.anchorNode, selection.anchorOffset, -1);
    const focus = view.posAtDOM(selection.focusNode, selection.focusOffset, -1);
    return Math.min(anchor, focus);
  } catch {
    // A transient detached DOM node must not break typing. The following
    // document-change verification still rejects a mismatched fallback.
    return fallback;
  }
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
    const recordKeyDown = (view: EditorView, event: KeyboardEvent) => {
      // `handleTextInput` remains authoritative for IME and modified-key
      // layouts. This raw fallback is only for an unmodified printable trigger
      // that Chrome may commit through DOM reconciliation.
      if (
        !event.isComposing &&
        !event.ctrlKey &&
        !event.metaKey &&
        !event.altKey &&
        TRIGGER_CHARS.has(event.key)
      ) {
        pendingArm = domInsertionPosition(view);
      } else {
        pendingArm = null;
      }
    };

    return [
      new Plugin<number | null>({
        key: new PluginKey("suggestionTriggerArming"),

        view(view) {
          // ProseMirror can decline a keydown when its state selection is
          // temporarily behind the browser DOM selection. Capture on the DOM
          // itself so provenance survives that exact reconciliation gap.
          const onKeyDown = (event: KeyboardEvent) => recordKeyDown(view, event);
          view.dom.addEventListener("keydown", onKeyDown, true);
          return {
            destroy() {
              view.dom.removeEventListener("keydown", onKeyDown, true);
            },
          };
        },

        state: {
          init() {
            armedPositions.set(editor, null);
            return null;
          },

          apply(tr, prev, _oldState, newState) {
            const armCandidate = pendingArm;
            // `handleTextInput` is followed immediately by a document change.
            // The keydown fallback may first be followed by an internal
            // selection/meta transaction, so keep that candidate only while
            // the caret remains at its insertion point. A document change
            // always consumes it, whether or not it proves valid.
            if (armCandidate !== null) {
              if (tr.docChanged || (tr.selectionSet && newState.selection.from !== armCandidate)) {
                pendingArm = null;
              }
            }

            const next = ((): number | null => {
              // A trigger character was just typed. The keydown fallback can
              // run even when a later plugin consumes the key, so only accept
              // the candidate on a document change that really inserted a
              // trigger at that position.
              if (
                armCandidate !== null &&
                tr.docChanged &&
                stillHoldsTrigger(newState.doc, armCandidate)
              ) {
                return armCandidate;
              }
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

            armedPositions.set(editor, next);
            return next;
          },
        },

        props: {
          handleTextInput(view, from, _to, text) {
            const index = lastTriggerIndex(text);
            const base = selectsWholeDocument(view)
              ? Selection.atStart(view.state.doc).from
              : from;
            pendingArm = index === -1 ? null : base + index;
            // Never handle the input — every other plugin must still see it.
            return false;
          },
          handlePaste() {
            pendingArm = null;
            return false;
          },
          handleDrop() {
            pendingArm = null;
            return false;
          },
          handleClick() {
            pendingArm = null;
            return false;
          },
        },
      }),
    ];
  },
});
