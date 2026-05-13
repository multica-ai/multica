import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
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

 
function removeImageBySrc(editor: any, src: string) {
  if (!editor) return;
  const { tr } = editor.state;
  let deleted = false;
  editor.state.doc.descendants((node: any, pos: number) => {
    if (deleted) return false;
    if (node.type.name === "image" && node.attrs.src === src) {
      tr.delete(pos, pos + node.nodeSize);
      deleted = true;
      return false;
    }
    return undefined;
  });
  if (deleted) editor.view.dispatch(tr);
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
    }

    try {
      const result = await handler(file);
      if (result) {
        const { tr } = editor.state;
        let found = false;
        editor.state.doc.descendants((node: { type: { name: string }; attrs: { src: string } }, nodePos: number) => {
          if (found) return false;
          if (node.type.name === "image" && node.attrs.src === blobUrl) {
            tr.setNodeMarkup(nodePos, undefined, {
              ...node.attrs,
              src: result.link,
              alt: result.filename,
              uploading: false,
            });
            found = true;
            return false;
          }
          return undefined;
        });
        if (found) editor.view.dispatch(tr);
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
        finalizeFileCard(editor, uploadId, result.link);
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
function dedupFiles(files: Iterable<File>): File[] {
  const seen = new Set<string>();
  return Array.from(files).filter((file) => {
    const key = `${file.name}\0${file.size}\0${file.type}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

/**
 * Extract files from clipboard data, checking both `.files` and `.items`.
 *
 * Some browsers (Safari, macOS screenshot paste) only expose pasted images
 * through `clipboardData.items` and leave `clipboardData.files` empty.
 * This helper unifies both sources and deduplicates the result.
 */
function getClipboardFiles(
  clipboard: Pick<DataTransfer, "files" | "items"> | null | undefined,
): File[] {
  if (!clipboard) return [];

  const directFiles = clipboard.files ? Array.from(clipboard.files) : [];
  const itemFiles =
    clipboard.items == null
      ? []
      : Array.from(clipboard.items)
          .filter((item) => item.kind === "file")
          .map((item) => item.getAsFile())
          .filter((file): file is File => file != null);

  return dedupFiles([...directFiles, ...itemFiles]);
}

export function createFileUploadExtension(
  onUploadFileRef: React.RefObject<((file: File) => Promise<UploadResult | null>) | undefined>,
) {
  return Extension.create({
    name: "fileUpload",
    addProseMirrorPlugins() {
      const { editor } = this;

      const handleFiles = async (files: Iterable<File>) => {
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
              const files = getClipboardFiles(event.clipboardData);
              if (!files.length) return false;
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
              const unique = dedupFiles(Array.from(files));
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

export { getClipboardFiles };
