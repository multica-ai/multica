/**
 * Pure geometry helpers for review annotation shapes.
 *
 * Shapes are stored with NORMALIZED coordinates (0..1 fractions of the
 * rendered media box) — same convention as web's review canvas, so a shape
 * drawn on either client renders identically on the other.
 *
 * Rendering converts to px against the current media layout. Everything
 * emits px NUMBERS, never "%" strings: SVG Path `d` and Polygon `points`
 * have no percent form, so a mixed-unit overlay renders pen strokes and
 * arrow heads at the wrong scale (the original mobile bug).
 */

import type { AnnotationShape } from "@multica/core/types";

export interface ShapePoint {
  x: number;
  y: number;
}

// Mobile renders exactly the shape type web persists — no parallel local
// shape model (data-identity rule in apps/mobile/CLAUDE.md). `type` is a
// plain string in core; renderers must keep a fallback branch for values
// this client doesn't know yet.
export type ReviewShape = AnnotationShape;

/** Minimum normalized size below which a drag is treated as an accidental tap. */
const MIN_SHAPE_SIZE = 0.01;

/**
 * Canonicalize a just-drawn shape for persistence: flip negative drag
 * direction so x,y is the top-left, drop degenerate shapes. Returns null
 * when the shape is too small to mean anything.
 */
export function normalizeShape(shape: ReviewShape): ReviewShape | null {
  if (shape.type === "rectangle" || shape.type === "ellipse") {
    let { x = 0, y = 0, width = 0, height = 0 } = shape;
    if (width < 0) {
      x += width;
      width = Math.abs(width);
    }
    if (height < 0) {
      y += height;
      height = Math.abs(height);
    }
    if (width < MIN_SHAPE_SIZE && height < MIN_SHAPE_SIZE) return null;
    return { ...shape, x, y, width, height };
  }
  if ((shape.points?.length ?? 0) < 2) return null;
  return shape;
}

export function rectToSvgProps(shape: ReviewShape, w: number, h: number) {
  const x = shape.x ?? 0;
  const y = shape.y ?? 0;
  const width = shape.width ?? 0;
  const height = shape.height ?? 0;
  return {
    x: Math.min(x, x + width) * w,
    y: Math.min(y, y + height) * h,
    width: Math.abs(width) * w,
    height: Math.abs(height) * h,
  };
}

export function ellipseToSvgProps(shape: ReviewShape, w: number, h: number) {
  const x = shape.x ?? 0;
  const y = shape.y ?? 0;
  const width = shape.width ?? 0;
  const height = shape.height ?? 0;
  return {
    cx: (x + width / 2) * w,
    cy: (y + height / 2) * h,
    rx: Math.abs(width / 2) * w,
    ry: Math.abs(height / 2) * h,
  };
}

export function pointsToSvgPath(points: ShapePoint[], w: number, h: number): string {
  return points
    .map((p, i) => `${i === 0 ? "M" : "L"} ${p.x * w} ${p.y * h}`)
    .join(" ");
}

/** Normalized length of the arrow head relative to the media box. */
const ARROW_HEAD_LEN = 0.05;

/**
 * Three-point polygon for an arrow head, tip first, as an SVG `points`
 * string in px.
 */
export function arrowHeadPoints(start: ShapePoint, end: ShapePoint, w: number, h: number): string {
  const angle = Math.atan2(end.y - start.y, end.x - start.x);
  const p1 = {
    x: end.x - ARROW_HEAD_LEN * Math.cos(angle - Math.PI / 6),
    y: end.y - ARROW_HEAD_LEN * Math.sin(angle - Math.PI / 6),
  };
  const p2 = {
    x: end.x - ARROW_HEAD_LEN * Math.cos(angle + Math.PI / 6),
    y: end.y - ARROW_HEAD_LEN * Math.sin(angle + Math.PI / 6),
  };
  return [end, p1, p2].map((p) => `${p.x * w},${p.y * h}`).join(" ");
}
