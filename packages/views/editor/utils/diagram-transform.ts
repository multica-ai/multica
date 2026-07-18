/**
 * Pan/zoom math for the Mermaid diagram viewer.
 *
 * The diagram itself lives inside an empty-sandbox iframe, which cannot run
 * scripts and therefore cannot implement its own pan/zoom. All interaction is
 * driven from the host document by transforming the wrapper around that
 * iframe, so this module stays pure DOM-free math: it is the single place
 * where "where is the diagram and how big is it" is decided.
 *
 * Coordinate model: the content wrapper has `transform-origin: 0 0` and is
 * positioned at the viewport's top-left, so a transform is applied as
 * `translate(x, y) scale(scale)`. `x`/`y` are viewport-space pixels of the
 * content's top-left corner; `scale` is the natural-size multiplier.
 */

export interface Size {
  width: number;
  height: number;
}

export interface Point {
  x: number;
  y: number;
}

export interface DiagramTransform {
  scale: number;
  x: number;
  y: number;
}

export const MIN_SCALE = 0.25;
export const MAX_SCALE = 4;

// How much of the diagram must stay inside the viewport. Panning is clamped so
// the user can never fling the canvas out of sight and be left staring at an
// empty viewport with no way back other than Reset.
const MIN_VISIBLE_PX = 48;

// Keyboard/button zoom step. 1.2 gives ~4 presses per doubling, which feels
// responsive without skipping past the scale the user wanted.
export const ZOOM_STEP = 1.2;

// Keyboard pan step, in viewport pixels per arrow-key press.
export const PAN_STEP_PX = 48;

export function clampScale(scale: number): number {
  if (!Number.isFinite(scale)) return 1;
  return Math.min(MAX_SCALE, Math.max(MIN_SCALE, scale));
}

function hasArea(size: Size): boolean {
  return (
    Number.isFinite(size.width) &&
    Number.isFinite(size.height) &&
    size.width > 0 &&
    size.height > 0
  );
}

/**
 * Scale that makes the content fit the viewport, never magnifying past natural
 * size. A small diagram opened in a large viewer should read at 100%, not be
 * blown up until its strokes look broken — same convention as macOS Preview.
 */
export function computeFitScale(content: Size, viewport: Size): number {
  if (!hasArea(content) || !hasArea(viewport)) return 1;

  return clampScale(
    Math.min(1, viewport.width / content.width, viewport.height / content.height),
  );
}

/** Centers `content` at `scale` inside `viewport`. */
export function centerTransform(
  content: Size,
  viewport: Size,
  scale: number,
): DiagramTransform {
  return {
    scale,
    x: (viewport.width - content.width * scale) / 2,
    y: (viewport.height - content.height * scale) / 2,
  };
}

/** Default view: fit to viewport, centered. */
export function computeFitTransform(content: Size, viewport: Size): DiagramTransform {
  return centerTransform(content, viewport, computeFitScale(content, viewport));
}

/**
 * Keeps at least `MIN_VISIBLE_PX` of the diagram inside the viewport (or all of
 * it, when the diagram is smaller than that margin), so the canvas can never be
 * panned into nothing.
 */
export function clampTransform(
  transform: DiagramTransform,
  content: Size,
  viewport: Size,
): DiagramTransform {
  if (!hasArea(content) || !hasArea(viewport)) return transform;

  const scale = clampScale(transform.scale);
  const scaledWidth = content.width * scale;
  const scaledHeight = content.height * scale;
  const marginX = Math.min(MIN_VISIBLE_PX, scaledWidth);
  const marginY = Math.min(MIN_VISIBLE_PX, scaledHeight);

  return {
    scale,
    x: Math.min(
      Math.max(transform.x, marginX - scaledWidth),
      viewport.width - marginX,
    ),
    y: Math.min(
      Math.max(transform.y, marginY - scaledHeight),
      viewport.height - marginY,
    ),
  };
}

/**
 * Zooms to `nextScale` while pinning the content point under `anchor` (in
 * viewport coordinates) to that same spot. This is what makes wheel and pinch
 * zoom track the cursor/fingers instead of drifting toward a corner.
 */
export function zoomToAt(
  transform: DiagramTransform,
  nextScale: number,
  anchor: Point,
  content: Size,
  viewport: Size,
): DiagramTransform {
  const scale = clampScale(nextScale);
  if (scale === transform.scale) return transform;

  const ratio = scale / transform.scale;

  return clampTransform(
    {
      scale,
      x: anchor.x - (anchor.x - transform.x) * ratio,
      y: anchor.y - (anchor.y - transform.y) * ratio,
    },
    content,
    viewport,
  );
}

/** Multiplicative zoom (wheel notch, +/- key, toolbar button) about `anchor`. */
export function zoomByAt(
  transform: DiagramTransform,
  factor: number,
  anchor: Point,
  content: Size,
  viewport: Size,
): DiagramTransform {
  return zoomToAt(transform, transform.scale * factor, anchor, content, viewport);
}

/** Zoom about the viewport center — the anchor to use for keyboard/buttons. */
export function zoomByAtCenter(
  transform: DiagramTransform,
  factor: number,
  content: Size,
  viewport: Size,
): DiagramTransform {
  return zoomByAt(
    transform,
    factor,
    { x: viewport.width / 2, y: viewport.height / 2 },
    content,
    viewport,
  );
}

export function panBy(
  transform: DiagramTransform,
  deltaX: number,
  deltaY: number,
  content: Size,
  viewport: Size,
): DiagramTransform {
  return clampTransform(
    { scale: transform.scale, x: transform.x + deltaX, y: transform.y + deltaY },
    content,
    viewport,
  );
}

/** Distance between two active pointers — the pinch gesture's scale signal. */
export function distanceBetween(a: Point, b: Point): number {
  return Math.hypot(a.x - b.x, a.y - b.y);
}

export function midpointOf(a: Point, b: Point): Point {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
}

/**
 * Converts a wheel notch into a zoom factor. `deltaMode` matters: mice report
 * lines (1) or pages (2) while trackpads report pixels (0), so a raw `deltaY`
 * comparison would make one of the two unusable.
 */
export function wheelZoomFactor(deltaY: number, deltaMode: number): number {
  const pixels = deltaMode === 1 ? deltaY * 16 : deltaMode === 2 ? deltaY * 100 : deltaY;
  // Exponential mapping keeps zooming symmetric: equal and opposite scrolls
  // return to the original scale. The 400 divisor tunes trackpad sensitivity.
  return Math.exp(-pixels / 400);
}
