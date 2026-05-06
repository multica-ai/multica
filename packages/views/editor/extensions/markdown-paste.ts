/**
 * Markdown paste extension — ensures pasted text is parsed as Markdown.
 *
 * Problem: The browser clipboard can contain BOTH text/plain and text/html.
 * ProseMirror always prefers text/html when present (hardcoded in
 * parseFromClipboard: `let asText = !html`). When copying from VS Code,
 * text editors, or .md files, the OS wraps text in <pre>/<div> HTML tags.
 * ProseMirror parses these as code blocks — wrong.
 *
 * Solution: Use `handlePaste` (the only ProseMirror prop that runs for ALL
 * paste events and has access to raw ClipboardEvent). We check for
 * `data-pm-slice` in the HTML — this attribute is added by ProseMirror's
 * own clipboard serializer. If present, the source is another ProseMirror
 * editor and its HTML is structurally correct — let ProseMirror handle it.
 * Otherwise, ignore the HTML and parse text/plain as Markdown.
 *
 * Why not clipboardTextParser? It only runs when there's NO text/html on
 * the clipboard (ProseMirror source: `let asText = !!text && !html`).
 *
 * Why not heuristic detection (looksLikeMarkdown / hasRichHtml)? Unreliable.
 * VS Code's HTML contains <code> tags that fool rich-content detectors.
 * Markdown pattern matching has too many edge cases. The data-pm-slice
 * check is deterministic — no false positives.
 */
import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Slice } from "@tiptap/pm/model";

export function createMarkdownPasteExtension() {
  return Extension.create({
    name: "markdownPaste",
    addProseMirrorPlugins() {
      const { editor } = this;
      return [
        new Plugin({
          key: new PluginKey("markdownPaste"),
          props: {
            handlePaste(view, event) {
              if (!editor.markdown) return false;
              const clipboard = event.clipboardData;
              if (!clipboard) return false;

              // If clipboard has files, defer to the fileUpload extension.
              if (clipboard.files?.length) return false;

              const text = clipboard.getData("text/plain");
              if (!text) return false;

              // If the caret is inside a code block, insert the text as-is.
              // Code blocks must keep newlines literal; running Markdown
              // parsing here would split a blank line (\n\n) into two
              // paragraphs and tear the code block open. (#1982)
              const { $from } = view.state.selection;
              if ($from.parent.type.name === "codeBlock") {
                view.dispatch(view.state.tr.insertText(text));
                return true;
              }

              const html = clipboard.getData("text/html");

              // If HTML contains data-pm-slice, the source is another
              // ProseMirror editor — let ProseMirror use its native HTML
              // clipboard path to preserve exact node structure.
              if (html && html.includes("data-pm-slice")) return false;

              // Everything else (VS Code, text editors, .md files, terminals,
              // web pages): parse text/plain as Markdown.
              const json = editor.markdown.parse(text);
              const node = editor.schema.nodeFromJSON(json);
              const slice = Slice.maxOpen(node.content);
              const tr = view.state.tr.replaceSelection(slice);
              view.dispatch(tr);
              return true;
            },
          },
        }),
      ];
    },
  });
}
