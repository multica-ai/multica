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
 * HTML/text classification is intentionally conservative. Rich semantic HTML
 * should stay native so links, lists, emphasis, and inline code survive.
 * Syntax-highlight wrappers from editors (<pre>/<code>/<span>/<div>) are not
 * enough by themselves, because those should still paste as Markdown source.
 */
import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import {
  Fragment,
  Slice,
  type Node as ProseMirrorNode,
} from "@tiptap/pm/model";

const LARGE_PASTE_TEXT_THRESHOLD = 50_000;
const SEMANTIC_RICH_HTML_SELECTOR = [
  "a[href]",
  "b",
  "blockquote",
  "del",
  "details",
  "em",
  "figcaption",
  "figure",
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "hr",
  "i",
  "img",
  "li",
  "mark",
  "ol",
  "s",
  "strong",
  "sub",
  "summary",
  "sup",
  "table",
  "tbody",
  "td",
  "tfoot",
  "th",
  "thead",
  "tr",
  "u",
  "ul",
].join(",");
const RAW_HTML_TAG_RE = /<(\/?[a-zA-Z][a-zA-Z0-9-]*)(?:\s[^>]*)?\/?>/g;

// CommonMark treats <word> as raw HTML regardless of whether "word" is a real
// HTML element. For plain-text paste, the user's text is the source of truth, so
// escape tag-like runs before the Markdown lexer can classify them as HTML.
function escapeRawHtmlTagsInSegment(segment: string): string {
  return segment.replace(
    RAW_HTML_TAG_RE,
    (match) => match.replaceAll("<", "&lt;").replaceAll(">", "&gt;"),
  );
}

function collectRawHtmlTagsInSegment(segment: string): string[] {
  return segment.match(RAW_HTML_TAG_RE) ?? [];
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
        parts.push(escapeRawHtmlTagsInSegment(delimiter));
        i = afterOpener;
      }
      continue;
    }

    const nextBacktick = line.indexOf("`", i);
    const end = nextBacktick === -1 ? line.length : nextBacktick;
    parts.push(escapeRawHtmlTagsInSegment(line.slice(i, end)));
    i = end;
  }

  return parts.join("");
}

function collectTagsOutsideCodeSpans(line: string): string[] {
  const tags: string[] = [];
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
          i = idx + count;
          found = true;
          break;
        }
        closerIdx = idx + 1;
      }

      if (!found) {
        i = afterOpener;
      }
      continue;
    }

    const nextBacktick = line.indexOf("`", i);
    const end = nextBacktick === -1 ? line.length : nextBacktick;
    tags.push(...collectRawHtmlTagsInSegment(line.slice(i, end)));
    i = end;
  }

  return tags;
}

export function escapeRawHtmlTagsOutsideCode(text: string): string {
  const lines = text.split("\n");
  let inFencedBlock = false;
  let fenceChar = "";
  let fenceLen = 0;

  const processed = lines.map((line) => {
    const fenceMatch = line.match(/^ {0,3}(`{3,}|~{3,})/);
    const fence = fenceMatch?.[1];
    if (fence) {
      if (!inFencedBlock) {
        inFencedBlock = true;
        fenceChar = fence.charAt(0);
        fenceLen = fence.length;
        return line;
      }
      const isClosingFence =
        fence.charAt(0) === fenceChar &&
        fence.length >= fenceLen &&
        /^ {0,3}(`{3,}|~{3,})[ \t]*$/.test(line);
      if (isClosingFence) {
        inFencedBlock = false;
        return line;
      }
    }

    if (inFencedBlock) return line;
    return escapeTagsOutsideCodeSpans(line);
  });

  return processed.join("\n");
}

function findRawHtmlTagsOutsideCode(text: string): string[] {
  const lines = text.split("\n");
  const tags: string[] = [];
  let inFencedBlock = false;
  let fenceChar = "";
  let fenceLen = 0;

  for (const line of lines) {
    const fenceMatch = line.match(/^ {0,3}(`{3,}|~{3,})/);
    const fence = fenceMatch?.[1];
    if (fence) {
      if (!inFencedBlock) {
        inFencedBlock = true;
        fenceChar = fence.charAt(0);
        fenceLen = fence.length;
        continue;
      }
      const isClosingFence =
        fence.charAt(0) === fenceChar &&
        fence.length >= fenceLen &&
        /^ {0,3}(`{3,}|~{3,})[ \t]*$/.test(line);
      if (isClosingFence) {
        inFencedBlock = false;
        continue;
      }
    }

    if (!inFencedBlock) {
      tags.push(...collectTagsOutsideCodeSpans(line));
    }
  }

  return tags;
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

function hasRichStyle(style: string): boolean {
  const normalized = style.toLowerCase();
  return (
    /font-weight\s*:\s*(bold|[6-9]00)\b/.test(normalized) ||
    /font-style\s*:\s*italic\b/.test(normalized) ||
    /text-decoration[^;]*(line-through|underline)/.test(normalized)
  );
}

function countOccurrences(text: string, needle: string): number {
  if (!needle) return 0;
  let count = 0;
  let index = text.indexOf(needle);
  while (index !== -1) {
    count++;
    index = text.indexOf(needle, index + needle.length);
  }
  return count;
}

function htmlPreservesRawTagsFromPlainText(html: string, text: string): boolean {
  const tags = findRawHtmlTagsOutsideCode(text);
  if (tags.length === 0) return true;
  if (typeof DOMParser === "undefined") return false;

  const doc = new DOMParser().parseFromString(html, "text/html");
  const htmlText = doc.body?.textContent ?? "";
  const expectedCounts = new Map<string, number>();
  for (const tag of tags) {
    expectedCounts.set(tag, (expectedCounts.get(tag) ?? 0) + 1);
  }

  for (const [tag, expectedCount] of expectedCounts) {
    if (countOccurrences(htmlText, tag) < expectedCount) return false;
  }

  return true;
}

function hasSemanticRichHtml(html: string, text: string): boolean {
  if (!html.trim()) return false;
  if (typeof DOMParser === "undefined") return false;

  if (!htmlPreservesRawTagsFromPlainText(html, text)) return false;

  const doc = new DOMParser().parseFromString(html, "text/html");
  const { body } = doc;
  if (!body) return false;

  if (body.querySelector(SEMANTIC_RICH_HTML_SELECTOR)) return true;

  // Inline <code> carries meaningful rich-text semantics. A <pre><code> pair
  // alone is often just a syntax-highlight wrapper from editors, so keep that
  // path available for Markdown parsing.
  for (const code of Array.from(body.querySelectorAll("code"))) {
    if (!code.closest("pre")) return true;
  }

  for (const el of Array.from(body.querySelectorAll<HTMLElement>("[style]"))) {
    if (hasRichStyle(el.getAttribute("style") ?? "")) return true;
  }

  return false;
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
  if (html && hasSemanticRichHtml(html, text)) return "native";
  if (text.length > LARGE_PASTE_TEXT_THRESHOLD) return "literal";
  if (isStructuredPlainText(text)) return "literal";
  return "markdown";
}

function canJoinOrderedLists(
  left: ProseMirrorNode,
  right: ProseMirrorNode,
): boolean {
  const leftType = left.attrs.type ?? "1";
  const rightType = right.attrs.type ?? "1";
  if (
    left.type.name !== "orderedList" ||
    right.type !== left.type ||
    leftType !== rightType
  ) {
    return false;
  }

  const parsedLeftStart = Number(left.attrs.start);
  const parsedRightStart = Number(right.attrs.start);
  const leftStart = Number.isFinite(parsedLeftStart) ? parsedLeftStart : 1;
  const rightStart = Number.isFinite(parsedRightStart) ? parsedRightStart : 1;
  const expectedContinuation = leftStart + left.childCount;

  // Some rich-text sources put every visual item in its own <ol> without a
  // start value, so the parsed slice becomes adjacent one-item lists that all
  // restart at 1. An explicit continuation value is the same structure with a
  // better HTML hint. Both forms should become one list in our document model.
  return rightStart === 1 || rightStart === expectedContinuation;
}

function repairOrderedListsInFragment(fragment: Fragment): Fragment {
  const repaired: ProseMirrorNode[] = [];

  fragment.forEach((node) => {
    const content = node.isLeaf
      ? node.content
      : repairOrderedListsInFragment(node.content);
    const repairedNode = content.eq(node.content) ? node : node.copy(content);
    const previous = repaired.at(-1);

    if (previous && canJoinOrderedLists(previous, repairedNode)) {
      repaired[repaired.length - 1] = previous.copy(
        previous.content.append(repairedNode.content),
      );
      return;
    }

    repaired.push(repairedNode);
  });

  return Fragment.fromArray(repaired);
}

function repairFragmentedOrderedLists(slice: Slice): Slice {
  const content = repairOrderedListsInFragment(slice.content);
  if (content.eq(slice.content)) return slice;
  return new Slice(content, slice.openStart, slice.openEnd);
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
            handlePaste(view, event, slice) {
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

              if (mode === "native") {
                // ProseMirror-owned clipboard HTML already represents our exact
                // document structure. Only repair external rich HTML, where
                // adjacent <ol> fragments commonly stand for one visual list.
                if (html && !html.includes("data-pm-slice")) {
                  const repaired = repairFragmentedOrderedLists(slice);
                  if (repaired !== slice) {
                    const tr = view.state.tr
                      .replaceSelection(repaired)
                      .scrollIntoView()
                      .setMeta("paste", true)
                      .setMeta("uiEvent", "paste");
                    view.dispatch(tr);
                    return true;
                  }
                }
                return false;
              }

              if (mode === "literal") {
                view.dispatch(view.state.tr.insertText(text));
                return true;
              }

              // Everything else (VS Code, text editors, .md files, terminals,
              // web pages): parse text/plain as Markdown.
              const preprocessed = escapeRawHtmlTagsOutsideCode(text);
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

              const parsedSlice = Slice.maxOpen(node.content);
              const tr = view.state.tr.replaceSelection(parsedSlice);
              view.dispatch(tr);
              return true;
            },
          },
        }),
      ];
    },
  });
}
