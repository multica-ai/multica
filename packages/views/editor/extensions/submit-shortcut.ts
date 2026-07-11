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
        "Mod-Enter": () => {
          // IME guard — same as Enter below. While a composition is open the
          // composed text is not in the document yet, so submitting here
          // would send the doc WITHOUT what the user just typed (e.g. paste
          // a screenshot, type a pinyin sentence, hit ⌘↵ before the buffer
          // commits — the submission carries only the screenshot).
          if (this.editor.view.composing) return false;
          return onSubmit();
        },
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
