/**
 * A DOM for the Markdown codec. @tiptap/markdown parses HTML found in
 * Markdown (raw HTML, and the file-card divs preprocessMarkdown emits) only
 * when `window.DOMParser` exists, and otherwise keeps it as literal text — so
 * without this the server would build a different document than a browser
 * does from the same Markdown. Import before anything that touches Tiptap.
 */
import { Window } from "happy-dom";

const win = new Window();
const globals = globalThis as Record<string, unknown>;
for (const key of [
  "DOMParser",
  "Node",
  "Element",
  "HTMLElement",
  "Text",
  "DocumentFragment",
  "XMLSerializer",
] as const) {
  globals[key] ??= win[key];
}
globals.window ??= win;
globals.document ??= win.document;
