import DOMPurify from "isomorphic-dompurify";

// Strict allow-list for HTML snippets authored by agents and rendered inline
// via dangerouslySetInnerHTML in document-view-page. Full HTML documents take
// the iframe + sandbox path and don't go through this util.
//
// Kept: typographic markup (headings, paragraphs, lists, tables, code,
// blockquotes, inline emphasis, links, images, figures, hr/br).
// Stripped: <script>, <style>, <iframe>, <object>, <embed>, every on*-handler,
// and any href/src that isn't http(s) or mailto (kills javascript: + data:).

const ALLOWED_TAGS = [
  "h1", "h2", "h3", "h4", "h5", "h6",
  "p", "br", "hr",
  "ul", "ol", "li",
  "table", "thead", "tbody", "tfoot", "tr", "td", "th",
  "code", "pre",
  "blockquote",
  "strong", "em",
  "a", "img",
  "figure", "figcaption",
];

const ALLOWED_ATTR = [
  "href", "src", "alt", "title",
  "colspan", "rowspan",
];

// DOMPurify special-cases data: URIs in <img>/<video>/<audio>/etc. so they
// bypass ALLOWED_URI_REGEXP. We don't want that — agent-authored data: URIs
// are an attack vector (data:text/html, etc.). The hook hard-strips them
// alongside javascript:/vbscript:/file: on href + src.
const UNSAFE_URI = /^\s*(?:javascript|data|vbscript|file):/i;

DOMPurify.addHook("uponSanitizeAttribute", (_node, hookEvent) => {
  if (hookEvent.attrName !== "href" && hookEvent.attrName !== "src") return;
  if (UNSAFE_URI.test(hookEvent.attrValue ?? "")) {
    hookEvent.keepAttr = false;
  }
});

export function sanitizeHtml(dirty: string): string {
  return DOMPurify.sanitize(dirty, {
    ALLOWED_TAGS,
    ALLOWED_ATTR,
    ALLOWED_URI_REGEXP: /^(?:https?:|mailto:)/i,
    FORBID_TAGS: ["script", "style", "iframe", "object", "embed", "link", "meta", "base", "form"],
    FORBID_ATTR: ["style"],
  }) as string;
}
