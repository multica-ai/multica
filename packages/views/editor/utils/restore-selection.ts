import { TextSelection, type Selection } from "@tiptap/pm/state";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";

/**
 * Map a pre-replace caret range onto the nearest VALID text positions of a
 * freshly rebuilt document.
 *
 * The content-sync effect used to restore the caret with a bare
 * `setTextSelection({ from: Math.min(from, docSize), to: Math.min(to, docSize) })`.
 * A clamped offset can land on a *structural* (non-text) boundary — e.g. the gap
 * between an `orderedList` and its first `listItem` — where ProseMirror has no
 * cursor and the browser paints the caret on the following line. That was the
 * visible half of the "type `1.`, switch issues, come back, caret jumps to the
 * second line" bug.
 *
 * `TextSelection.between` resolves each end and searches outward for a real
 * text position, so the returned selection is always a placeable cursor inside
 * a textblock — never a structural gap.
 */
export function clampSelectionToText(
  doc: ProseMirrorNode,
  from: number,
  to: number,
): Selection {
  const size = doc.content.size;
  const clamp = (pos: number) => Math.min(Math.max(pos, 0), size);
  return TextSelection.between(doc.resolve(clamp(from)), doc.resolve(clamp(to)));
}
