import Image from "@tiptap/extension-image";
import { ReactNodeViewRenderer, type ReactNodeViewProps } from "@tiptap/react";
import type { ComponentType } from "react";

/**
 * Dependencies injected from `@multica/views`, which owns the React node view
 * and the markdown-label escaper. Injecting them (rather than importing views
 * here) keeps this package a leaf of the dependency graph — views imports this
 * extension, not the other way round — so there is no import cycle.
 */
export interface ImageExtensionDeps {
  imageView: ComponentType<ReactNodeViewProps>;
  escapeMarkdownLabel: (label: string) => string;
}

// Minimal HTML-attribute escaping for the sized/aligned serialisation form.
// The value is read back with getAttribute() on parse, which decodes entities,
// so the round-trip stays clean.
function attr(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/"/g, "&quot;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

/**
 * The ContentEditor image node. Extends the upstream `@tiptap/extension-image`
 * with cerebro concerns: an upload placeholder flag, intrinsic pixel
 * dimensions (for zero-layout-shift), and inline resize/alignment
 * (`widthPct` / `align`, FIR-4699 Phase 5).
 *
 * Serialisation is size-dependent so every existing document stays byte-for-
 * byte identical: an untouched image is `![alt](url)`; a resized or aligned
 * image serialises to `<img src alt data-width-pct data-align>`. The width is
 * a percentage of the text column and rides in a `data-` attribute (not the
 * HTML `width` attribute, which is spec'd as integer pixels) so a single CSS
 * rule sizes it in both the editor and the read-only view.
 */
export function createImageExtension(deps: ImageExtensionDeps) {
  const { imageView, escapeMarkdownLabel } = deps;

  return Image.extend({
    addAttributes() {
      return {
        ...this.parent?.(),
        uploading: {
          default: false,
          renderHTML: (attrs: Record<string, unknown>) =>
            attrs.uploading ? { "data-uploading": "" } : {},
          parseHTML: (el: HTMLElement) => el.hasAttribute("data-uploading"),
        },
        // Intrinsic pixel dimensions, captured on upload (file-upload.ts). The
        // browser uses width/height on <img> to compute aspect-ratio and reserve
        // the box before the image decodes, so inserting an image causes no
        // layout shift (and the post-insert scrollIntoView stays correct). Not
        // serialized to markdown so round-trips stay clean.
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
        // Inline placement: width as a percentage of the text column, and
        // alignment. Both default to null so an untouched image serialises to
        // `![alt](url)` exactly as today. `data-width-pct` is rendered onto the
        // DOM img so a CSS rule (Phase 5 visual slice) can size it in both the
        // editor and the read-only view.
        widthPct: {
          default: null,
          renderHTML: (attrs: Record<string, unknown>) =>
            attrs.widthPct
              ? { "data-width-pct": attrs.widthPct as number }
              : {},
          parseHTML: (el: HTMLElement) => {
            const p = parseInt(el.getAttribute("data-width-pct") || "", 10);
            return Number.isFinite(p) ? p : null;
          },
        },
        align: {
          default: null,
          renderHTML: (attrs: Record<string, unknown>) =>
            attrs.align ? { "data-align": attrs.align as string } : {},
          parseHTML: (el: HTMLElement) => el.getAttribute("data-align") || null,
        },
      };
    },
    addNodeView() {
      return ReactNodeViewRenderer(imageView);
    },
    renderMarkdown: (node: { attrs?: Record<string, unknown> }) => {
      const src = (node.attrs?.src as string) || "";
      const rawAlt = (node.attrs?.alt as string) || "";
      const title = node.attrs?.title as string | undefined;
      const pct = node.attrs?.widthPct as number | undefined;
      const align = node.attrs?.align as string | undefined;
      if (pct || align) {
        const w = pct ? ` data-width-pct="${pct}"` : "";
        const a = align ? ` data-align="${attr(align)}"` : "";
        return `<img src="${attr(src)}" alt="${attr(rawAlt)}"${w}${a}>`;
      }
      const alt = escapeMarkdownLabel(rawAlt);
      if (title) {
        return `![${alt}](${src} "${title}")`;
      }
      return `![${alt}](${src})`;
    },
  }).configure({
    inline: false,
    allowBase64: false,
  });
}
