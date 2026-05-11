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
 * Otherwise, classify text/plain into one of three paths:
 * - native: let ProseMirror or another extension handle it
 * - literal: insert exact text without Markdown parsing
 * - markdown: parse text/plain as Markdown
 *
 * Why not clipboardTextParser? It only runs when there's NO text/html on
 * the clipboard (ProseMirror source: `let asText = !!text && !html`).
 *
 * Why not heuristic detection (looksLikeMarkdown / hasRichHtml)? Unreliable.
 * VS Code's HTML contains <code> tags that fool rich-content detectors.
 * Markdown pattern matching has too many edge cases. Instead, the classifier
 * only keeps narrow deterministic exits for editor-owned slices, code block
 * context, structured plain text, and large payloads.
 */
import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Slice } from "@tiptap/pm/model";

const LARGE_PASTE_TEXT_THRESHOLD = 50_000;

type PasteMode = "native" | "literal" | "markdown";

interface PasteClassificationInput {
  text: string;
  html: string;
  hasFiles: boolean;
  isInsideCodeBlock: boolean;
}

function isJsonDocumentText(text: string): boolean {
  const trimmed = text.trim();
  if (!trimmed) return false;

  const startsLikeJson =
    (trimmed.startsWith("{") && trimmed.endsWith("}")) ||
    (trimmed.startsWith("[") && trimmed.endsWith("]"));
  if (!startsLikeJson) return false;

  try {
    JSON.parse(trimmed);
    return true;
  } catch {
    return false;
  }
}

function isStructuredPlainText(text: string): boolean {
  return isJsonDocumentText(text);
}

function classifyPaste({
  text,
  html,
  hasFiles,
  isInsideCodeBlock,
}: PasteClassificationInput): PasteMode {
  if (hasFiles) return "native";
  if (!text) return "native";
  if (isInsideCodeBlock) return "literal";
  if (html && html.includes("data-pm-slice")) return "native";
  if (text.length > LARGE_PASTE_TEXT_THRESHOLD) return "literal";
  if (isStructuredPlainText(text)) return "literal";
  return "markdown";
}

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

              const text = clipboard.getData("text/plain");
              const html = clipboard.getData("text/html");
              const { $from } = view.state.selection;
              const mode = classifyPaste({
                text,
                html,
                hasFiles: Boolean(clipboard.files?.length),
                isInsideCodeBlock: $from.parent.type.name === "codeBlock",
              });

              if (mode === "native") return false;

              if (mode === "literal") {
                view.dispatch(view.state.tr.insertText(text));
                return true;
              }

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
