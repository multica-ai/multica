import { Extension } from "@tiptap/core";
import { Plugin, PluginKey, TextSelection } from "@tiptap/pm/state";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { createSafeId } from "@multica/core/utils";

/** Find and remove a fileCard node by uploadId. */
 
function removeUploadingFileCard(editor: any, uploadId: string) {
  const { tr } = editor.state;
  let deleted = false;
  editor.state.doc.descendants((node: any, pos: number) => {
    if (deleted) return false;
    if (node.type.name === "fileCard" && node.attrs.uploadId === uploadId) {
      tr.delete(pos, pos + node.nodeSize);
      deleted = true;
      return false;
    }
    return undefined;
  });
  if (deleted) editor.view.dispatch(tr);
}

/** Update a fileCard node from uploading state to final state with real URL. */
 
function finalizeFileCard(editor: any, uploadId: string, href: string) {
  const { tr } = editor.state;
  let updated = false;
  editor.state.doc.descendants((node: any, nodePos: number) => {
    if (updated) return false;
    if (node.type.name === "fileCard" && node.attrs.uploadId === uploadId) {
      tr.setNodeMarkup(nodePos, undefined, {
        ...node.attrs,
        href,
        uploading: false,
      });
      updated = true;
      return false;
    }
    return undefined;
  });
  if (updated) editor.view.dispatch(tr);
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

function removeImageBySrc(editor: any, src: string) {
  const imagePos = findImagePosBySrc(editor, src);
  if (imagePos === null) return;

  const imageNode = editor.state.doc.nodeAt(imagePos);
  if (!imageNode) return;

  const tr = editor.state.tr.delete(imagePos, imagePos + imageNode.nodeSize);
  editor.view.dispatch(tr);
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
  handler: (file: File) => Promise<UploadResult | null>,
  pos?: number,
) {
  const isImage = file.type.startsWith("image/");

  if (isImage) {
    const blobUrl = URL.createObjectURL(file);
    const imgAttrs = { src: blobUrl, alt: file.name, uploading: true };
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
      const result = await handler(file);
      if (result) {
        const imagePos = findImagePosBySrc(editor, blobUrl);
        const imageNode = imagePos === null ? null : editor.state.doc.nodeAt(imagePos);
        if (imagePos !== null && imageNode) {
          const tr = editor.state.tr.setNodeMarkup(imagePos, undefined, {
            ...imageNode.attrs,
            // Persist the stable per-attachment URL into markdown so
            // the comment doesn't capture a short-lived signed URL
            // (MUL-3130). Falls back to `link` for the no-workspace
            // avatar branch where there's no attachment-row id; that
            // path is unreachable from comment/issue editors but the
            // fallback keeps the contract consistent for any caller
            // that drops in without an issue context.
            src: result.markdownLink || result.link,
            alt: result.filename,
            uploading: false,
          });
          editor.view.dispatch(tr);
        }
      } else {
        removeImageBySrc(editor, blobUrl);
      }
    } catch {
      removeImageBySrc(editor, blobUrl);
    } finally {
      URL.revokeObjectURL(blobUrl);
    }
  } else {
    // Non-image: insert skeleton fileCard → upload → finalize with real URL
    const uploadId = createSafeId();
    const cardAttrs = { filename: file.name, href: "", fileSize: file.size, uploading: true, uploadId };
    const insertContent = { type: "fileCard", attrs: cardAttrs };
    if (pos !== undefined) {
      editor.chain().focus().insertContentAt(pos, insertContent).run();
    } else {
      editor.chain().focus().insertContent(insertContent).run();
    }

    try {
      const result = await handler(file);
      if (result) {
        finalizeFileCard(editor, uploadId, result.markdownLink || result.link);
      } else {
        removeUploadingFileCard(editor, uploadId);
      }
    } catch {
      removeUploadingFileCard(editor, uploadId);
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

export function createFileUploadExtension(
  onUploadFileRef: React.RefObject<((file: File) => Promise<UploadResult | null>) | undefined>,
) {
  return Extension.create({
    name: "fileUpload",
    addProseMirrorPlugins() {
      const { editor } = this;

      const handleFiles = async (files: FileList) => {
        const handler = onUploadFileRef.current;
        if (!handler) return false;
        for (const file of dedupFiles(files)) {
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
              if (!files?.length) return false;
              if (!onUploadFileRef.current) return false;
              handleFiles(files);
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
