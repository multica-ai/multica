import { Extension } from "@tiptap/core";
import type { RefObject } from "react";
/**
 * `onSubmit` must return true when it actually handled the event and false
 * when there's no submit handler wired up. That lets us fall through to the
 * default Enter behaviour — inserting a newline — when appropriate.
 *
 * `submitOnEnterRef` — ref whose current value controls Enter behavior.
 * When true, bare Enter submits (chat-style). When false, only Mod-Enter
 * submits and bare Enter keeps its default (newline).
 */
export function createSubmitExtension(
  onSubmit: () => boolean,
  { submitOnEnterRef }: { submitOnEnterRef: RefObject<boolean> },
) {
  return Extension.create({
    name: "submitShortcut",
    addKeyboardShortcuts() {
      return {
        "Mod-Enter": () => onSubmit(),
        Enter: () => {
          if (!submitOnEnterRef.current) return false;

          const editor = this.editor;
          if (editor.view.composing) return false;

          if (
            editor.isActive("codeBlock") ||
            editor.isActive("listItem") ||
            editor.isActive("blockquote")
          ) return false;

          return onSubmit();
        },
      };
    },
  });
}
