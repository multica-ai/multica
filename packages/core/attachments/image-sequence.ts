/**
 * Attachment sequence — the ordered list of attachments one surface exposes
 * to the preview viewer's prev / next navigation (MUL-5752, MUL-7642).
 *
 * `collectAttachmentSequence` takes the inclusion rule from the caller: web /
 * desktop page through every previewable kind (images, PDFs, video, Markdown,
 * HTML, text), so an issue reads as one run of files. `collectImageSequence`
 * is the images-only rule mobile's lightbox is built on.
 *
 * The sequence is built from DATA, not from the DOM: both the issue timeline
 * and the chat message list are virtualized, so a registry of mounted <img>
 * nodes would silently drop every image scrolled out of the window. Feeding it
 * the same `{ content, attachments }` blocks the renderers consume keeps the
 * order identical to what the reader sees without depending on what is
 * currently painted.
 *
 * Pure — no React, no DOM, no platform APIs. Mobile shares it (see
 * apps/mobile/CLAUDE.md: pure functions from @multica/core are importable).
 */

import type { Attachment } from "../types/attachment";
import {
  attachmentIdFromDownloadURL,
  contentReferencesAttachment,
} from "../types/attachment-url";

// Extension fallback for the image test. Mirrors IMAGE_EXTS in
// packages/views/editor/utils/preview.ts, which now delegates here so the
// "is this an image?" answer cannot drift between the preview dispatch table
// and the sequence builder.
const IMAGE_EXTENSIONS = new Set<string>([
  "png",
  "jpg",
  "jpeg",
  "gif",
  "webp",
  "avif",
  "bmp",
  "ico",
  "svg",
]);

function extensionOf(filename: string): string {
  const base = (filename ?? "").toLowerCase().split(/[\\/]/).pop() ?? "";
  const withoutQuery = base.split(/[?#]/, 1)[0] ?? "";
  const dot = withoutQuery.lastIndexOf(".");
  if (dot <= 0) return "";
  return withoutQuery.slice(dot + 1);
}

function normalizeContentType(contentType: string): string {
  const ct = (contentType ?? "").toLowerCase().trim();
  const semi = ct.indexOf(";");
  return (semi >= 0 ? ct.slice(0, semi) : ct).trim();
}

/**
 * True when the file renders as an image. Content-type wins; the extension is
 * the fallback for URL-only sources that carry no server metadata.
 *
 * NOTE for callers implementing a full kind dispatch: this is only the image
 * branch. `application/pdf`, `video/*` and `audio/*` must be tested BEFORE
 * this (an `.svg` is text-like XML, a `.pdf` named `chart.pdf.png` is not a
 * real case) — see `getPreviewKind`.
 */
export function isImageAttachment(
  contentType: string,
  filename: string,
): boolean {
  if (normalizeContentType(contentType).startsWith("image/")) return true;
  const ext = extensionOf(filename);
  return ext !== "" && IMAGE_EXTENSIONS.has(ext);
}

// ---------------------------------------------------------------------------
// URL → attachment matching
// ---------------------------------------------------------------------------

function stripQueryAndFragment(url: string): string {
  return url.split(/[?#]/, 1)[0] ?? "";
}

function matchesAttachmentURL(
  embeddedURL: string,
  attachmentURL?: string,
): boolean {
  if (!embeddedURL || !attachmentURL) return false;
  if (embeddedURL === attachmentURL) return true;
  const embeddedStable = stripQueryAndFragment(embeddedURL);
  const attachmentStable = stripQueryAndFragment(attachmentURL);
  return embeddedStable !== "" && embeddedStable === attachmentStable;
}

/**
 * Resolve a URL embedded in a markdown body back to its attachment record.
 *
 * Two fallbacks, in order — the same pair `AttachmentDownloadProvider` uses
 * (and now shares with this module so a URL can never resolve to one record
 * for the download click and another for the sequence):
 *
 *   1. id extracted from the stable `/api/attachments/<id>/download` shape
 *      that every post-MUL-3130 body persists. Survives a host swap and any
 *      incidental query / fragment.
 *   2. full-URL equality against `url` / `download_url` / `markdown_url`, for
 *      legacy bodies and CDN markdown that never got the stable shape.
 */
export function matchAttachmentByURL(
  url: string,
  attachments: ReadonlyArray<Attachment> | null | undefined,
): Attachment | undefined {
  if (!url || !attachments?.length) return undefined;
  const idFromUrl = attachmentIdFromDownloadURL(url);
  if (idFromUrl) {
    const byId = attachments.find((a) => a.id === idFromUrl);
    if (byId) return byId;
  }
  return attachments.find(
    (a) =>
      matchesAttachmentURL(url, a.url) ||
      matchesAttachmentURL(url, a.download_url) ||
      matchesAttachmentURL(url, a.markdown_url),
  );
}

/**
 * The attachments a surface renders as cards BELOW the body — i.e. the ones
 * the markdown does not already reference inline.
 *
 * Also drops a re-upload of a file that is already inline under a different
 * attachment row (same name + type + size): the reader sees one image, so the
 * sequence must count one.
 */
export function selectStandaloneAttachments(
  content: string | null | undefined,
  attachments: ReadonlyArray<Attachment> | null | undefined,
): Attachment[] {
  if (!attachments?.length) return [];
  if (!content) return [...attachments];
  return attachments.filter((a) => {
    if (contentReferencesAttachment(content, a)) return false;
    const hasSiblingInContent = attachments.some(
      (other) =>
        other.id !== a.id &&
        other.filename === a.filename &&
        other.content_type === a.content_type &&
        other.size_bytes === a.size_bytes &&
        contentReferencesAttachment(content, other),
    );
    return !hasSiblingInContent;
  });
}

// ---------------------------------------------------------------------------
// Inline image references
// ---------------------------------------------------------------------------

function blankOut(text: string): string {
  // Newlines survive so line-anchored patterns below keep their line numbers.
  return text.replace(/[^\n]/g, " ");
}

/**
 * Blank every code span and fenced block, preserving offsets and line breaks.
 *
 * Without this, an `![shot](url)` shown as an EXAMPLE inside a fenced block —
 * common in agent replies explaining markdown — would be counted as a real
 * image and the reader would page onto something that was never rendered.
 *
 * Line scanner rather than one multiline regex: a backreferenced
 * `(`{3,})[\s\S]*?\1` over agent output is a ReDoS shape, and this runs on
 * every issue/chat render.
 */
function maskCode(content: string): string {
  const lines = content.split("\n");
  let fence: string | null = null;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    const fenceMatch = /^[ \t]{0,3}(`{3,}|~{3,})/.exec(line);
    if (fence !== null) {
      lines[i] = blankOut(line);
      const closes =
        fenceMatch !== null &&
        fenceMatch[1]![0] === fence[0] &&
        fenceMatch[1]!.length >= fence.length;
      if (closes) fence = null;
      continue;
    }
    if (fenceMatch !== null) {
      fence = fenceMatch[1]!;
      lines[i] = blankOut(line);
      continue;
    }
    lines[i] = line.replace(/(`+)[^`\n]*?\1/g, blankOut);
  }
  return lines.join("\n");
}

interface InlineRef {
  index: number;
  url: string;
  /** Best filename hint available at the reference site. */
  filename: string;
  /**
   * `!file[name](url)` card rather than an image reference. Markdown `![]()`
   * and `<img>` are images by construction (the renderers pass
   * `forceKind: "image"`); a card is whatever its name/type says it is, so it
   * still has to pass the caller's inclusion rule.
   */
  isFileCard: boolean;
}

// `![alt](url "title")`. The URL char class stops at whitespace and `)` so a
// title never leaks into it; the optional <> wrapper is CommonMark's escape
// for URLs containing spaces.
const MARKDOWN_IMAGE_RE =
  /!\[([^\]\n]*)\]\(\s*<([^>\n]*)>[^)]*\)|!\[([^\]\n]*)\]\(\s*([^)\s]+)[^)]*\)/g;

// Raw `<img src=...>` — reachable because the readonly renderer runs
// rehypeRaw + rehypeSanitize, and the sanitize schema keeps img.
const HTML_IMAGE_RE =
  /<img\b[^>]*?\ssrc\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s">']+))[^>]*>/gi;

// `!file[name](url)` on its own line — `preprocessFileCards` turns it into a
// fileCard div, which the renderer hands to <Attachment>: an image filename
// renders as an inline image, anything else as a file card.
const FILE_CARD_LINE_RE = /^[ \t]*!file\[((?:\\.|[^\]\\\n])*)\]\(([^)\s]+)\)[ \t]*$/gm;

function unescapeLabel(label: string): string {
  return label.replace(/\\([[\]\\()])/g, "$1");
}

function extractInlineRefs(rawContent: string): InlineRef[] {
  if (!rawContent) return [];
  const content = maskCode(rawContent);
  const refs: InlineRef[] = [];

  for (const m of content.matchAll(MARKDOWN_IMAGE_RE)) {
    // Two alternations: `<...>`-wrapped URL (groups 1/2) or bare (groups 3/4).
    const alt = m[1] ?? m[3] ?? "";
    const url = (m[2] ?? m[4] ?? "").trim();
    if (!url) continue;
    refs.push({
      index: m.index ?? 0,
      url,
      filename: unescapeLabel(alt),
      isFileCard: false,
    });
  }

  for (const m of content.matchAll(HTML_IMAGE_RE)) {
    const url = (m[1] ?? m[2] ?? m[3] ?? "").trim();
    if (!url) continue;
    refs.push({
      index: m.index ?? 0,
      url,
      filename: "",
      isFileCard: false,
    });
  }

  for (const m of content.matchAll(FILE_CARD_LINE_RE)) {
    const url = (m[2] ?? "").trim();
    if (!url) continue;
    refs.push({
      index: m.index ?? 0,
      url,
      filename: unescapeLabel(m[1] ?? ""),
      isFileCard: true,
    });
  }

  return refs.sort((a, b) => a.index - b.index);
}

// ---------------------------------------------------------------------------
// Sequence
// ---------------------------------------------------------------------------

/** One attachment the viewer can page to. */
export interface ImageSequenceItem {
  /**
   * Identity used to open the sequence at the clicked attachment. The
   * attachment id when the reference resolves to a record, otherwise the URL
   * as written in the body — matching what the renderer knows at click time.
   */
  key: string;
  /** Best-known URL. Callers holding `attachment` should prefer re-resolving. */
  url: string;
  filename: string;
  /** Present when the reference resolved to a workspace attachment record. */
  attachment?: Attachment;
  /**
   * The body references this as an image (`![]()` / `<img>`), so it renders
   * as one whatever `filename` says — for such a reference `filename` is the
   * markdown caption, which is prose with no extension to read (MUL-7518).
   */
  imageByConstruction: boolean;
}

/** One renderable unit: an issue description, a comment, a chat message. */
export interface ImageSequenceBlock {
  content?: string | null;
  attachments?: ReadonlyArray<Attachment> | null;
  /**
   * Whether the surface renders this block's unreferenced attachments as cards
   * under it. Defaults to true. An issue description renders none, and its
   * `attachments` is the whole issue's list (comment uploads keep `issue_id`),
   * so there it only resolves the body's references.
   */
  standalone?: boolean;
}

/** What an inclusion rule gets to decide on. */
export interface SequenceCandidate {
  contentType: string;
  filename: string;
  /** False for a body reference that did not resolve to an attachment record. */
  hasRecord: boolean;
}

/**
 * Flatten blocks into the ordered sequence a viewer walks.
 *
 * Order is render order: for each block, references inline in the body in
 * text order, then the standalone attachment cards rendered under it. Repeats
 * of the same attachment collapse to their first position, so the counter
 * matches the number of distinct files rather than the number of references.
 *
 * Image references (`![]()` / `<img>`) are always kept — they render as
 * images by construction. File cards and standalone attachments are kept
 * when `include` accepts them.
 */
export function collectAttachmentSequence(
  blocks: ReadonlyArray<ImageSequenceBlock | null | undefined>,
  include: (candidate: SequenceCandidate) => boolean,
): ImageSequenceItem[] {
  const items: ImageSequenceItem[] = [];
  const seen = new Set<string>();

  const push = (item: ImageSequenceItem) => {
    if (!item.key || seen.has(item.key)) return;
    seen.add(item.key);
    items.push(item);
  };

  for (const block of blocks) {
    if (!block) continue;
    const content = block.content ?? "";
    const attachments = block.attachments ?? [];

    for (const ref of extractInlineRefs(content)) {
      const attachment = matchAttachmentByURL(ref.url, attachments);
      if (
        ref.isFileCard &&
        !include({
          contentType: attachment?.content_type ?? "",
          filename: attachment?.filename || ref.filename || ref.url,
          hasRecord: !!attachment,
        })
      ) {
        continue;
      }
      push({
        key: attachment?.id ?? ref.url,
        url:
          attachment?.download_url ||
          attachment?.markdown_url ||
          attachment?.url ||
          ref.url,
        filename: attachment?.filename || ref.filename,
        attachment,
        imageByConstruction: !ref.isFileCard,
      });
    }

    if (block.standalone === false) continue;
    for (const attachment of selectStandaloneAttachments(content, attachments)) {
      if (
        !include({
          contentType: attachment.content_type,
          filename: attachment.filename,
          hasRecord: true,
        })
      ) {
        continue;
      }
      push({
        key: attachment.id,
        url:
          attachment.download_url || attachment.markdown_url || attachment.url,
        filename: attachment.filename,
        attachment,
        imageByConstruction: false,
      });
    }
  }

  return items;
}

/** The images-only sequence (mobile's lightbox). */
export function collectImageSequence(
  blocks: ReadonlyArray<ImageSequenceBlock | null | undefined>,
): ImageSequenceItem[] {
  return collectAttachmentSequence(blocks, ({ contentType, filename }) =>
    isImageAttachment(contentType, filename),
  );
}

/** Index of `key` in the sequence, or -1 when it is not part of it. */
export function indexOfImageKey(
  items: ReadonlyArray<ImageSequenceItem>,
  key: string,
): number {
  if (!key) return -1;
  return items.findIndex((item) => item.key === key);
}
