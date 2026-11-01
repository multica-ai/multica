/**
 * The least the office measures in layout units. Fewer units draw each desk larger on the page; 416 by 171
 * is the smallest floor that holds the studio's 3 by 2 desk rooms (`STUDIO_MIN_W/H` in the engine patch, which
 * change together with these), and a larger page grows the floor past it rather than leaving bars.
 */
const MIN_UNITS_W = 416;
const MIN_UNITS_H = 171;

/** The office's size in layout units. */
export interface OfficeUnits {
  w: number;
  h: number;
}

/** The units that fill a `width` by `height` page with the same pixels per unit the minimum floor gets. */
export function unitsFor(width: number, height: number): OfficeUnits {
  if (width <= 0 || height <= 0) return { w: MIN_UNITS_W, h: MIN_UNITS_H };
  const pixelsPerUnit = Math.min(width / MIN_UNITS_W, height / MIN_UNITS_H);
  return { w: Math.round(width / pixelsPerUnit), h: Math.round(height / pixelsPerUnit) };
}
/** Picks the office's floor layout: the closest of the pack's variants to the reference. */
export const LAYOUT_SEED = 2;
/** The engine steps and paints this often; pixel art does not need more, and it keeps the worker light. */
export const FRAME_MS = 33;

/**
 * A worker on the floor: one per agent, seated at a desk of its home squad's room.
 * `room` is the room's slot on the floor, 0 to `OFFICE_SLOTS - 1` (see `@multica/core/office`).
 */
export interface WorkerSession {
  key: string;
  working: boolean;
  room: number;
}

/** A tag over a worker's head, in layout units: its anchor is the top of the head. */
export interface OfficeLabel {
  key: string;
  x: number;
  y: number;
}

/** Page to worker. The canvas moves to the worker once, at `init`. */
export type ToWorker =
  | { type: "init"; canvas: OffscreenCanvas }
  | { type: "sessions"; sessions: WorkerSession[] }
  | { type: "size"; units: OfficeUnits }
  | { type: "paused"; paused: boolean };

/** A point on the floor, in layout units. */
export interface OfficePoint {
  x: number;
  y: number;
}

/** A walled desk room, or a whiteboard's face, in layout units. */
export interface OfficeRoom {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Worker to page: where the tags go, and where the rooms and the whiteboards are. Each is sent only when it changes. */
export type FromWorker =
  | { type: "labels"; labels: OfficeLabel[] }
  | { type: "rooms"; rooms: OfficeRoom[]; tank: OfficePoint | null; boards: OfficeRoom[] }
  | { type: "failed" };
