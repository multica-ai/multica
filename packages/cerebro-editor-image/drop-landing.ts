// Drop landing geometry (FIR-4699 Phase 5). While a file is dragged over the
// writing pane, a 2px `--brand` line shows the block the image will land above
// or below. This is pure geometry: from the pointer's vertical position and the
// blocks' rects it names the nearest block, the edge and the line's Y. It never
// decides the tray — "onto the tray" is a real hit-test on the thumbnail strip
// that the DOM layer already has, not something to guess from a Y coordinate.
// The DOM wiring (measuring rects, drawing the line, resolving the insert
// position from the block's node, inserting on drop) lives in the editor plugin.

/** A block's viewport rect, in document order. */
export interface DropBlock {
  /** Viewport top edge, px. */
  top: number;
  /** Viewport bottom edge, px. */
  bottom: number;
}

/** Where a dragged image lands: the nearest block, the edge and the line Y. */
export interface DropLanding {
  /** Index of the nearest block in the input array. */
  blockIndex: number;
  /** Which edge of that block the image lands on. */
  side: "above" | "below";
  /** Viewport Y for the 2px landing line. */
  lineY: number;
}

/**
 * Resolve where a dragged file lands over the writing pane, from the pointer's
 * vertical position and the blocks' viewport rects (document order).
 *
 * Always returns a landing when there is a block to land against — including
 * the bottom half of the last block, which lands below it (the common "type
 * something, drop the image under it" gesture). Routing to the tray is the DOM
 * layer's call, made by hit-testing the thumbnail strip, not by this geometry.
 * Returns null only for an empty pane, where there is no block to land against.
 */
export function dropLanding(
  pointerY: number,
  blocks: DropBlock[],
): DropLanding | null {
  if (blocks.length === 0) return null;

  // Nearest block by edge distance — 0 when the pointer is inside the span.
  let nearest = 0;
  let bestDist = Infinity;
  for (let i = 0; i < blocks.length; i++) {
    const b = blocks[i]!;
    const dist =
      pointerY < b.top
        ? b.top - pointerY
        : pointerY > b.bottom
          ? pointerY - b.bottom
          : 0;
    if (dist < bestDist) {
      bestDist = dist;
      nearest = i;
    }
  }

  const b = blocks[nearest]!;
  const side = pointerY < (b.top + b.bottom) / 2 ? "above" : "below";
  return { blockIndex: nearest, side, lineY: side === "above" ? b.top : b.bottom };
}
