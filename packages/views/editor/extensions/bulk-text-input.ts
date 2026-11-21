import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";

/**
 * Feeds a multi-character text insertion to the input rules as plain text
 * followed by one typed character.
 *
 * Tiptap's input-rule runner assumes every `handleTextInput` call carries a
 * single keystroke: it places a rule's match at
 * `from - (match.length - text.length)`. Some keyboards insert a whole phrase in
 * one call — voice keyboards that record first and then insert the
 * transcription, predictive-text picks, text replacements. When a rule matches
 * only the tail of such a phrase (`3x4`, `...`, `--`, a closing backtick), that
 * range starts past its own end, and the rule either throws, leaving the phrase
 * out of the document, or writes only its replacement, dropping everything
 * before the match (MUL-9305).
 *
 * So the phrase minus its last character goes in as plain text, and the last
 * character re-enters `handleTextInput`, where the input rules see an ordinary
 * keystroke. IME composition is left alone: the runner already skips it.
 */
export const BulkTextInputExtension = Extension.create({
  name: "bulkTextInput",
  // Ahead of every extension that owns input rules (Link sits at 1000), and
  // behind SuggestionTriggerArming, which still has to see the whole phrase.
  priority: 1001,

  addProseMirrorPlugins() {
    return [
      new Plugin({
        key: new PluginKey("bulkTextInput"),
        props: {
          handleTextInput(view, from, to, text) {
            if (view.composing) return false;
            const chars = Array.from(text);
            if (chars.length < 2) return false;
            const last = chars.pop()!;
            const head = chars.join("");
            view.dispatch(view.state.tr.insertText(head, from, to));
            const at = from + head.length;
            const deflt = () => view.state.tr.insertText(last, at, at).scrollIntoView();
            if (!view.someProp("handleTextInput", (f) => f(view, at, at, last, deflt))) {
              view.dispatch(deflt());
            }
            return true;
          },
        },
      }),
    ];
  },
});
