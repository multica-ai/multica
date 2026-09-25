import { Extension } from "@tiptap/core";
import { Plugin, PluginKey, TextSelection } from "@tiptap/pm/state";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { createSafeId } from "@multica/core/utils";

/**
 * Locate this upload's placeholder node, whichever shape it took.
 *
 * `uploadId` is the SAME value the draft store knows as `clientUploadId` —
 * minted once in {@link uploadAndInsertFile} (or by the rebuild path) and
 * carried by both records. One identity is what lets a settle find its own
 * node in a document that a different mount rebuilt.
 */

function findUploadNode(editor: any, uploadId: string): { pos: number; node: any } | null {
  let found: { pos: number; node: any } | null = null;
  editor.state.doc.descendants((node: any, pos: number) => {
    if (found) return false;
    if (
      (node.type.name === "fileCard" || node.type.name === "image") &&
      node.attrs.uploadId === uploadId
    ) {
      found = { pos, node };
      return false;
    }
    return undefined;
  });
  return found;
}

/** Drop this upload's placeholder node, whichever shape it took. */

export function removeUploadNode(editor: any, uploadId: string): boolean {
  const hit = findUploadNode(editor, uploadId);
  if (!hit) return false;
  editor.view.dispatch(editor.state.tr.delete(hit.pos, hit.pos + hit.node.nodeSize));
  return true;
}

/**
 * Turn this upload's placeholder into the finished attachment, in place.
 *
 * Returns false when no node carries the id — the caller then knows the
 * document is not showing this upload and can fall back to appending.
 *
 * A fileCard settling into an image swaps node TYPE, not just attrs: the
 * rebuild path cannot know an upload will turn out to be an image (it has no
 * bytes, only a filename), so it always writes a card and this is where the
 * document catches up with what actually arrived.
 */

export function settleUploadNode(editor: any, uploadId: string, result: UploadResult): boolean {
  const hit = findUploadNode(editor, uploadId);
  if (!hit) return false;
  // Persist the stable per-attachment URL, never the short-lived signed one
  // (MUL-3130). `link` is the fallback for the no-workspace avatar branch.
  const href = result.markdownLink || result.link;
  const isImage = (result.content_type ?? "").startsWith("image/");
  const tr = editor.state.tr;

  if (hit.node.type.name === "image") {
    tr.setNodeMarkup(hit.pos, undefined, {
      ...hit.node.attrs,
      src: href,
      alt: result.filename,
      uploading: false,
      uploadId: null,
    });
  } else if (isImage) {
    tr.replaceWith(
      hit.pos,
      hit.pos + hit.node.nodeSize,
      editor.schema.nodes.image.create({ src: href, alt: result.filename, uploading: false }),
    );
  } else {
    tr.setNodeMarkup(hit.pos, undefined, {
      ...hit.node.attrs,
      href,
      uploading: false,
      uploadId: null,
    });
  }
  editor.view.dispatch(tr);
  return true;
}

/**
 * Write a placeholder for an upload this document is not showing yet.
 *
 * Used when a composer reopens over an upload a previous mount started: the
 * bytes are gone with that mount, so there is no preview to render and the
 * card is all we can honestly draw — {@link settleUploadNode} promotes it to
 * an image if that is what arrives.
 */

export function insertUploadPlaceholder(
  editor: any,
  upload: { uploadId: string; filename: string; size?: number },
): boolean {
  // Idempotent: already drawn counts as success, so a caller retrying until
  // it lands cannot be fooled into retrying forever by its own first insert.
  if (findUploadNode(editor, upload.uploadId)) return true;
  const endPos = editor.state.doc.content.size;
  editor
    .chain()
    .insertContentAt(endPos, {
      type: "fileCard",
      attrs: {
        filename: upload.filename,
        href: "",
        fileSize: upload.size ?? 0,
        uploading: true,
        uploadId: upload.uploadId,
      },
    })
    .run();
  return true;
}

export function findImagePosBySrc(editor: any, src: string): number | null {
  if (!editor) return null;
  let imagePos: number | null = null;
  editor.state.doc.descendants((node: any, pos: number) => {
    if (imagePos !== null) return false;
    if (node.type.name === "image" && node.attrs.src === src) {
      imagePos = pos;
      return false;
    }
    return undefined;
  });
  return imagePos;
}

/**
 * Read an image's intrinsic pixel dimensions off-thread. Returns null when the
 * decode fails or the API is unavailable (e.g. jsdom in tests, where
 * `createImageBitmap` is undefined) — callers degrade to no reserved box.
 */
async function readImageDimensions(
  file: File,
): Promise<{ width: number; height: number } | null> {
  if (typeof createImageBitmap !== "function") return null;
  try {
    const bitmap = await createImageBitmap(file);
    const dims = { width: bitmap.width, height: bitmap.height };
    bitmap.close();
    return dims.width > 0 && dims.height > 0 ? dims : null;
  } catch {
    return null;
  }
}

/**
 * Measure the file's intrinsic size and write it onto the freshly-inserted
 * image node so the browser reserves the box before decode (no layout shift).
 * Fire-and-forget after insert: keyed on the blob `src`, so if the upload swap
 * already replaced it we simply skip — the swap preserves any width/height we
 * managed to set via `...imageNode.attrs`.
 */
async function applyImageDimensions(editor: any, file: File, src: string) {
  const dims = await readImageDimensions(file);
  if (!dims) return;

  const imagePos = findImagePosBySrc(editor, src);
  if (imagePos === null) return;

  const imageNode = editor.state.doc.nodeAt(imagePos);
  if (!imageNode || imageNode.attrs.width) return;

  const tr = editor.state.tr.setNodeMarkup(imagePos, undefined, {
    ...imageNode.attrs,
    width: dims.width,
    height: dims.height,
  });
  editor.view.dispatch(tr);
}

function moveSelectionToParagraphAfterImage(editor: any, src: string) {
  const imagePos = findImagePosBySrc(editor, src);
  if (imagePos === null) return;

  const imageNode = editor.state.doc.nodeAt(imagePos);
  if (!imageNode) return;

  const afterImagePos = imagePos + imageNode.nodeSize;
  const $afterImage = editor.state.doc.resolve(afterImagePos);
  if ($afterImage.nodeAfter?.type.name !== "paragraph") return;

  const paragraphStart = afterImagePos + 1;
  const tr = editor.state.tr
    .setSelection(TextSelection.create(editor.state.doc, paragraphStart))
    .scrollIntoView();
  editor.view.dispatch(tr);
}

/**
 * Shared upload flow: insert blob preview → upload → replace with real URL.
 * Used by both paste/drop (at cursor) and button upload (at end of doc).
 */
export async function uploadAndInsertFile(

  editor: any,
  file: File,
  handler: (file: File, uploadId: string) => Promise<UploadResult | null>,
  pos?: number,
) {
  const isImage = file.type.startsWith("image/");
  // One id for both records. The handler adopts it as the draft's
  // `clientUploadId`, so a settle arriving at a different mount can still find
  // the node this one drew — see findUploadNode.
  const uploadId = createSafeId();

  if (isImage) {
    const blobUrl = URL.createObjectURL(file);
    const imgAttrs = { src: blobUrl, alt: file.name, uploading: true, uploadId };
    if (pos !== undefined) {
      editor.chain().focus().insertContentAt(pos, { type: "image", attrs: imgAttrs }).run();
    } else {
      editor.chain().focus().setImage(imgAttrs).run();
      moveSelectionToParagraphAfterImage(editor, blobUrl);
    }

    // Reserve the image box ASAP so the async decode doesn't shift layout.
    // Fire-and-forget: must not delay the handler() call below, which the
    // synchronous-insert contract (instant preview) depends on.
    void applyImageDimensions(editor, file, blobUrl);

    try {
      const result = await handler(file, uploadId);
      // The upload outlives the mount (coordinator-owned, MUL-5181): by the
      // time it settles this editor may be destroyed. Dispatching against a
      // destroyed EditorView throws, and the catch would dispatch again —
      // the write-back path owns delivery for dead editors, not this swap.
      if (editor.isDestroyed) return;
      if (result) settleUploadNode(editor, uploadId, result);
      else removeUploadNode(editor, uploadId);
    } catch {
      if (!editor.isDestroyed) removeUploadNode(editor, uploadId);
    } finally {
      URL.revokeObjectURL(blobUrl);
    }
  } else {
    // Non-image: insert skeleton fileCard → upload → finalize with real URL
    const cardAttrs = { filename: file.name, href: "", fileSize: file.size, uploading: true, uploadId };
    const insertContent = { type: "fileCard", attrs: cardAttrs };
    if (pos !== undefined) {
      editor.chain().focus().insertContentAt(pos, insertContent).run();
    } else {
      editor.chain().focus().insertContent(insertContent).run();
    }

    try {
      const result = await handler(file, uploadId);
      // See the image branch: a settle after this editor's destroy must not
      // dispatch against the dead EditorView.
      if (editor.isDestroyed) return;
      if (result) settleUploadNode(editor, uploadId, result);
      else removeUploadNode(editor, uploadId);
    } catch {
      if (!editor.isDestroyed) removeUploadNode(editor, uploadId);
    }
  }
}

/** Deduplicate files from the same paste/drop event.
 *  macOS/Chrome can put the same file in the FileList twice. */
function dedupFiles(files: FileList): File[] {
  const seen = new Set<string>();
  return Array.from(files).filter((file) => {
    const key = `${file.name}\0${file.size}\0${file.type}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

/** Filename given to the .txt synthesised from an over-threshold paste. */
export const PASTED_TEXT_FILENAME = "pasted-text.txt";

/**
 * Source text of every file synthesised by the paste-as-file path, keyed by
 * the File instance that carries it downstream.
 *
 * Unlike a dropped file, this one has no copy anywhere else: the text was
 * never written into the document and its source may be a tab the user has
 * already closed. So whoever owns the draft must be able to put it back when
 * the upload fails. A WeakMap rather than a property on the File keeps the
 * File exactly as the upload layer expects it, and lets the entry die with the
 * upload. Read it with {@link pastedTextSource}; a File that came from disk
 * returns undefined and needs no recovery.
 */
const pastedTextSources = new WeakMap<File, string>();

/** Record the text a synthesised paste file was built from. */
export function markPastedTextFile(file: File, text: string): File {
  pastedTextSources.set(file, text);
  return file;
}

/** The text an over-threshold paste was made from, or undefined for real files. */
export function pastedTextSource(file: File): string | undefined {
  return pastedTextSources.get(file);
}

export function createFileUploadExtension(
  onUploadFileRef: React.RefObject<
    ((file: File, uploadId: string) => Promise<UploadResult | null>) | undefined
  >,
  /**
   * Character count above which a plain-text paste is uploaded as a .txt
   * attachment instead of being inserted into the document. A ref because the
   * extension array is built once at mount while the prop that feeds it can
   * change. Undefined / 0 keeps every paste as text — the default, so an
   * editor that never opts in behaves exactly as before.
   */
  pasteAsFileThresholdRef?: React.RefObject<number | undefined>,
) {
  return Extension.create({
    name: "fileUpload",
    addProseMirrorPlugins() {
      const { editor } = this;

      const handleFiles = async (files: File[]) => {
        const handler = onUploadFileRef.current;
        if (!handler) return false;
        for (const file of files) {
          await uploadAndInsertFile(editor, file, handler);
        }
        return true;
      };

      return [
        new Plugin({
          key: new PluginKey("fileUpload"),
          props: {
            handlePaste(_view, event) {
              const files = event.clipboardData?.files;
              if (!files?.length) {
                // No file on the clipboard: this may still be a paste large
                // enough that the host wants it as an attachment rather than
                // thousands of characters of body text (turn-based composers
                // only — document editors never pass a threshold).
                const threshold = pasteAsFileThresholdRef?.current;
                if (!threshold || threshold <= 0) return false;
                const text = event.clipboardData?.getData("text/plain") ?? "";
                if (text.length <= threshold) return false;
                if (!onUploadFileRef.current) return false;
                // A paste INTO a code block is the one long paste that is
                // deliberately inline — the user opened a fence to show the
                // thing. Converting it to an attachment would take away what
                // they just asked for.
                if (editor.isActive("codeBlock")) return false;
                const file = new File([text], PASTED_TEXT_FILENAME, { type: "text/plain" });
                handleFiles([markPastedTextFile(file, text)]);
                return true;
              }
              if (!onUploadFileRef.current) return false;
              handleFiles(dedupFiles(files));
              return true;
            },
            handleDrop(view, event) {
              const dragEvent = event as DragEvent;
              const files = dragEvent.dataTransfer?.files;
              if (!files?.length) return false;
              const handler = onUploadFileRef.current;
              if (!handler) return false;
              // Resolve drop position from mouse coordinates.
              // Only the first file uses the drop position; subsequent files
              // append to the end to avoid stale position issues.
              const dropPos = view.posAtCoords({ left: dragEvent.clientX, top: dragEvent.clientY });
              const unique = dedupFiles(files);
              for (let i = 0; i < unique.length; i++) {
                const insertPos = i === 0 ? dropPos?.pos : undefined;
                uploadAndInsertFile(editor, unique[i]!, handler, insertPos);
              }
              return true;
            },
          },
        }),
      ];
    },
  });
}
