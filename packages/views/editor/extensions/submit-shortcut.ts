import { Extension } from "@tiptap/core";

/**
 * `onSubmit` must return true when it actually handled the event and false
 * when there's no submit handler wired up. That lets us fall through to the
 * default Enter behaviour — inserting a newline — when appropriate.
 *
 * `submitOnEnter` — when true, bare Enter also submits (chat-style). When
 * false, only Mod-Enter submits and bare Enter keeps its default (newline).
 */
export function createSubmitExtension(
  onSubmit: () => boolean,
  { submitOnEnter }: { submitOnEnter: boolean },
) {
  return Extension.create({
    name: "submitShortcut",
    addKeyboardShortcuts() {
      const shortcuts: Record<string, () => boolean> = {
        "Mod-Enter": () => onSubmit(),
      };
      if (submitOnEnter) {
        shortcuts.Enter = () => {
          const editor = this.editor;
          // IME guard — never submit while composing a multi-key input
          // (Chinese pinyin, Japanese kana, etc). `view.composing` is set
          // by ProseMirror between compositionstart and compositionend.
          if (editor.view.composing) return false;
          // Let Enter insert a newline inside a code block.
          if (editor.isActive("codeBlock")) return false;
          return onSubmit();
        };
      }
      return shortcuts;
    },
  });
}
