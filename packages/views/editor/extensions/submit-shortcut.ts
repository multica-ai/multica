import { Extension } from "@tiptap/core";
import { Plugin } from "@tiptap/pm/state";
import {
  getShortcut,
  isPlainShortcut,
  shortcutMatchesEvent,
  type ShortcutChord,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";

export function shouldHandleSubmitShortcut(
  event: KeyboardEvent,
  options: {
    configuredShortcut: ShortcutChord | null;
    composing: boolean;
  },
): boolean {
  return (
    !event.repeat &&
    !options.composing &&
    !isImeComposing(event) &&
    shortcutMatchesEvent(options.configuredShortcut, event)
  );
}

/**
 * When plain Enter becomes Send, Shift+Enter must inherit the old Enter
 * behavior instead of Tiptap's normal hard-break behavior. Replaying the
 * editor's Enter keymap preserves paragraphs, patched list exit/continuation,
 * code-block newlines, and open suggestion pickers.
 */
export function shouldReplayNativeEnter(
  event: KeyboardEvent,
  configuredShortcut: ShortcutChord | null,
  composing: boolean,
): boolean {
  return (
    !composing &&
    !isImeComposing(event) &&
    isPlainShortcut(configuredShortcut, "Enter") &&
    event.key === "Enter" &&
    event.shiftKey &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.altKey
  );
}

/**
 * Tiered-priority dynamic key handler. Suggestion and slash-command plugins
 * get first refusal, while submit still runs before the base editor keymap.
 */
export function createSubmitShortcutExtension(
  onSubmit: () => boolean,
) {
  return Extension.create({
    name: "submitShortcut",
    // Mention/slash extensions use 101, so open pickers get first refusal.
    // Default editor keymaps use 100; reverse extension order places this
    // late-added handler ahead of them while keeping picker priority intact.
    priority: 100,
    addProseMirrorPlugins() {
      const editor = this.editor;
      let replayingEnter = false;
      return [
        new Plugin({
          props: {
            handleKeyDown(view, event) {
              if (replayingEnter) return false;
              const shortcut = getShortcut("send");

              if (
                shouldReplayNativeEnter(event, shortcut, view.composing)
              ) {
                replayingEnter = true;
                try {
                  return editor.commands.keyboardShortcut("Enter");
                } finally {
                  replayingEnter = false;
                }
              }

              if (
                !shouldHandleSubmitShortcut(event, {
                  configuredShortcut: shortcut,
                  composing: view.composing,
                })
              ) {
                return false;
              }
              return onSubmit();
            },
          },
        }),
      ];
    },
  });
}
