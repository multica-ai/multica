import { Extension } from "@tiptap/core";

/**
 * Escape → blur the editor. Without this, pressing ESC inside the
 * contenteditable does nothing (browsers don't blur contenteditables by
 * default), leaving users stuck in the editor with no keyboard escape hatch.
 */
export function createBlurShortcutExtension() {
  return Extension.create({
    name: "blurShortcut",
    addKeyboardShortcuts() {
      return {
        Escape: ({ editor }) => {
          editor.commands.blur();
          return true;
        },
      };
    },
  });
}
