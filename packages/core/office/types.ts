import type { Squad } from "../types";

/** A named, ordered group of `OFFICE_SLOTS` squad rooms. Floors are the office's tabs. */
export interface OfficeFloor {
  id: string;
  workspace_id: string;
  name: string;
  /** 0-based order of the floor among the workspace's floors. */
  position: number;
  created_at: string;
  updated_at: string;
}

export interface CreateOfficeFloorRequest {
  name: string;
}

export interface UpdateOfficeFloorRequest {
  name?: string;
  /** Moves the floor to this 0-based index; the other floors are renumbered. */
  position?: number;
}

/**
 * One squad's seat in a `PUT /api/office/seats` request. `floor_id` and `slot`
 * are both set (seat the squad) or both null (unseat it).
 */
export interface OfficeSeatAssignment {
  squad_id: string;
  floor_id: string | null;
  slot: number | null;
}

/** The squads named by a seat request, in request order, as they stand afterwards. */
export interface OfficeSeatsResponse {
  squads: Squad[];
}

/** The squad that already holds a seat a request asked for (HTTP 409, code `seat_conflict`). */
export interface OfficeSeatConflict {
  floor_id: string;
  slot: number;
  squad_id: string;
  squad_name: string;
}
