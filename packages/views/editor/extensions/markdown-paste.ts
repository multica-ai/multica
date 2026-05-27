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

// Standard HTML elements — CommonMark §6.6 treats any <word> as inline HTML.
// Non-standard tags get silently dropped by ProseMirror's schema. We escape
// only tags NOT in this set so legitimate HTML passes through normally.
const STANDARD_HTML_ELEMENTS = new Set([
  "a", "abbr", "address", "area", "article", "aside", "audio",
  "b", "base", "bdi", "bdo", "blockquote", "body", "br", "button",
  "canvas", "caption", "cite", "code", "col", "colgroup",
  "data", "datalist", "dd", "del", "details", "dfn", "dialog", "div", "dl", "dt",
  "em", "embed",
  "fieldset", "figcaption", "figure", "footer", "form",
  "h1", "h2", "h3", "h4", "h5", "h6", "head", "header", "hgroup", "hr", "html",
  "i", "iframe", "img", "input", "ins",
  "kbd",
  "label", "legend", "li", "link",
  "main", "map", "mark", "math", "menu", "meta", "meter",
  "nav", "noscript",
  "object", "ol", "optgroup", "option", "output",
  "p", "param", "picture", "pre", "progress",
  "q",
  "rp", "rt", "ruby",
  "s", "samp", "script", "search", "section", "select", "slot", "small", "source", "span", "strong", "style", "sub", "summary", "sup", "svg",
  "table", "tbody", "td", "template", "textarea", "tfoot", "th", "thead", "time", "title", "tr", "track",
  "u", "ul",
  "var", "video",
  "wbr",
]);

function escapeTagsInSegment(segment: string): string {
  return segment.replace(
    /<(\/?[a-zA-Z][a-zA-Z0-9-]*)(?:\s[^>]*)?\/?>/g,
    (match, tagPart: string) => {
      const tagName = tagPart.replace(/^\//, "").toLowerCase();
      if (STANDARD_HTML_ELEMENTS.has(tagName)) return match;
      return match.replaceAll("<", "&lt;").replaceAll(">", "&gt;");
    },
  );
}

function escapeTagsOutsideCodeSpans(line: string): string {
  const parts: string[] = [];
  let i = 0;

  while (i < line.length) {
    if (line[i] === "`") {
      let count = 0;
      while (i + count < line.length && line[i + count] === "`") count++;
      const delimiter = "`".repeat(count);
      const afterOpener = i + count;

      let closerIdx = afterOpener;
      let found = false;
      while (closerIdx <= line.length - count) {
        const idx = line.indexOf(delimiter, closerIdx);
        if (idx === -1) break;
        if (
          (idx + count >= line.length || line[idx + count] !== "`") &&
          (idx === 0 || line[idx - 1] !== "`")
        ) {
          parts.push(line.slice(i, idx + count));
          i = idx + count;
          found = true;
          break;
        }
        closerIdx = idx + 1;
      }

      if (!found) {
        parts.push(escapeTagsInSegment(delimiter));
        i = afterOpener;
      }
      continue;
    }

    const nextBacktick = line.indexOf("`", i);
    const end = nextBacktick === -1 ? line.length : nextBacktick;
    parts.push(escapeTagsInSegment(line.slice(i, end)));
    i = end;
  }

  return parts.join("");
}

export function escapeNonStandardHtmlTags(text: string): string {
  const lines = text.split("\n");
  let inFencedBlock = false;
  let fenceChar = "";
  let fenceLen = 0;

  const processed = lines.map((line) => {
    const fenceMatch = line.match(/^(`{3,}|~{3,})/);
    const fence = fenceMatch?.[1];
    if (fence) {
      if (!inFencedBlock) {
        inFencedBlock = true;
        fenceChar = fence.charAt(0);
        fenceLen = fence.length;
        return line;
      }
      if (fence.charAt(0) === fenceChar && fence.length >= fenceLen) {
        inFencedBlock = false;
        return line;
      }
    }

    if (inFencedBlock) return line;
    return escapeTagsOutsideCodeSpans(line);
  });

  return processed.join("\n");
}

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
              const preprocessed = escapeNonStandardHtmlTags(text);
              const json = editor.markdown.parse(preprocessed);
              const node = editor.schema.nodeFromJSON(json);

              // Safety net: if parsing still produces an empty doc despite
              // non-empty input, fall back to literal insertion.
              const first = node.content.firstChild;
              const parsedEmpty =
                node.content.childCount === 0 ||
                (node.content.childCount === 1 &&
                  first?.type.name === "paragraph" &&
                  first.content.size === 0);
              if (text.trim() && parsedEmpty) {
                view.dispatch(view.state.tr.insertText(text));
                return true;
              }

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
