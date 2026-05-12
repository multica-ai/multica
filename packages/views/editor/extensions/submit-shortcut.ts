import { Extension } from "@tiptap/core";

/**
 * `onSubmit` must return true when it actually handled the event and false
 * when there's no submit handler wired up. That lets us fall through to the
 * default Enter behaviour — inserting a newline — when appropriate.
 *
 * `submitOnEnter` — when true, bare Enter submits (chat-style) and
 * Mod-Enter / Shift-Enter fall through to Tiptap's hardBreak for a newline.
 * When false, only Mod-Enter submits and bare Enter keeps its default
 * (newline).
 */
export function createSubmitExtension(
  onSubmit: () => boolean,
  { submitOnEnter }: { submitOnEnter: boolean },
) {
  return Extension.create({
    name: "submitShortcut",
    addKeyboardShortcuts() {
      const shortcuts: Record<string, () => boolean> = {};
      // CEREBRO-PATCH(submit-shortcut-chat-mode-newline): JEH-1025 — in
      // chat-style mode, Mod-Enter (Cmd/Ctrl+Enter) must NOT submit; it
      // falls through to hardBreak so it inserts a newline alongside
      // Shift+Enter. Matches the Composer preference copy.
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
      } else {
        shortcuts["Mod-Enter"] = () => onSubmit();
      }
      return shortcuts;
    },
  });
}
