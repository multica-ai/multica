import type { Editor } from "@tiptap/core";

export const MOVE_IMAGE_TO_TRAY_EVENT = "cerebro:move-image-to-tray";

export interface MoveImageToTrayDetail {
  src: string;
  filename: string;
  caption?: string;
}

interface ImageNodeLike {
  attrs: Record<string, unknown>;
  nodeSize: number;
}

/** Delete an image together with the caption block it owns, when present. */
export function deleteInlineImageAndCaption(
  editor: Editor,
  node: ImageNodeLike,
  getPos: () => number | undefined,
) {
  const pos = getPos();
  if (typeof pos !== "number") return;
  const caption = editor.state.doc.nodeAt(pos + node.nodeSize);
  const to =
    pos +
    node.nodeSize +
    (caption?.type.name === "imageCaption" ? caption.nodeSize : 0);
  editor.view.dispatch(editor.state.tr.delete(pos, to));
}

/**
 * Offer an inline image to the nearest tray. The tray accepts by cancelling the
 * event; only then do we remove the image and its immediately-following caption.
 */
export function moveInlineImageToTray(
  target: EventTarget,
  editor: Editor,
  node: ImageNodeLike,
  getPos: () => number | undefined,
) {
  const pos = getPos();
  if (typeof pos !== "number") return;
  const caption = editor.state.doc.nodeAt(pos + node.nodeSize);
  const detail: MoveImageToTrayDetail = {
    src: String(node.attrs.src ?? ""),
    filename: String(node.attrs.alt || "image"),
    ...(caption?.type.name === "imageCaption" && caption.textContent
      ? { caption: caption.textContent }
      : {}),
  };
  const accepted = !target.dispatchEvent(
    new CustomEvent<MoveImageToTrayDetail>(MOVE_IMAGE_TO_TRAY_EVENT, {
      bubbles: true,
      cancelable: true,
      detail,
    }),
  );
  if (!accepted) return;

  deleteInlineImageAndCaption(editor, node, getPos);
}
