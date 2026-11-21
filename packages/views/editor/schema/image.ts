/**
 * image node schema and Markdown codec, without the React NodeView, so the
 * collaboration server can parse and serialize documents in Node.
 */
import Image from "@tiptap/extension-image";
import { escapeMarkdownLabel } from "../utils/escape-markdown-label";

export const ImageSchema = Image.extend({
  addAttributes() {
    return {
      ...this.parent?.(),
      uploading: {
        default: false,
        renderHTML: (attrs: Record<string, unknown>) =>
          attrs.uploading ? { "data-uploading": "" } : {},
        parseHTML: (el: HTMLElement) => el.hasAttribute("data-uploading"),
      },
      // The upload this placeholder belongs to — the same value the draft
      // store knows as `clientUploadId`. Lets a settle arriving at a mount
      // that did not start the upload find the node to replace. Render-only,
      // and cleared once the node holds a real URL.
      uploadId: {
        default: null,
        rendered: false,
      },
      // Intrinsic pixel dimensions, captured on upload (file-upload.ts). The
      // browser uses width/height on <img> to compute aspect-ratio and reserve
      // the box before the image decodes, so inserting an image causes no
      // layout shift (and the post-insert scrollIntoView stays correct). Not
      // serialized to markdown — `renderMarkdown` only emits src/alt/title — so
      // round-trips stay clean.
      width: {
        default: null,
        renderHTML: (attrs: Record<string, unknown>) =>
          attrs.width ? { width: attrs.width as number } : {},
        parseHTML: (el: HTMLElement) => {
          const w = parseInt(el.getAttribute("width") || "", 10);
          return Number.isFinite(w) ? w : null;
        },
      },
      height: {
        default: null,
        renderHTML: (attrs: Record<string, unknown>) =>
          attrs.height ? { height: attrs.height as number } : {},
        parseHTML: (el: HTMLElement) => {
          const h = parseInt(el.getAttribute("height") || "", 10);
          return Number.isFinite(h) ? h : null;
        },
      },
    };
  },
  renderMarkdown: (node: any) => {
    const src = node.attrs?.src || "";
    // Same rule as fileCard: an in-flight placeholder is not content. Its src
    // is a process-local `blob:` URL that expires on reload, so emitting it
    // would put a dead link in the persisted draft. This replaces the
    // after-the-fact regex scrub ContentEditor used to run on every serialise.
    if (node.attrs?.uploading === true || !src) return "";
    const alt = escapeMarkdownLabel(node.attrs?.alt || "");
    const title = node.attrs?.title;
    if (title) {
      return `![${alt}](${src} "${title}")`;
    }
    return `![${alt}](${src})`;
  },
}).configure({
  inline: false,
  allowBase64: false,
});
