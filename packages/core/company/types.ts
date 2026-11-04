import type { Squad } from "../types";

/** A named, ordered group of `COMPANY_SLOTS` squad rooms. Floors are the company's tabs. */
export interface CompanyFloor {
  id: string;
  workspace_id: string;
  name: string;
  /** 0-based order of the floor among the workspace's floors. */
  position: number;
  created_at: string;
  updated_at: string;
}

export interface CreateCompanyFloorRequest {
  name: string;
}

export interface UpdateCompanyFloorRequest {
  name?: string;
  /** Moves the floor to this 0-based index; the other floors are renumbered. */
  position?: number;
}

/**
 * One squad's seat in a `PUT /api/company/seats` request. `floor_id` and `slot`
 * are both set (seat the squad) or both null (unseat it).
 */
export interface CompanySeatAssignment {
  squad_id: string;
  floor_id: string | null;
  slot: number | null;
}

/** The squads named by a seat request, in request order, as they stand afterwards. */
export interface CompanySeatsResponse {
  squads: Squad[];
}

/** The squad that already holds a seat a request asked for (HTTP 409, code `seat_conflict`). */
export interface CompanySeatConflict {
  floor_id: string;
  slot: number;
  squad_id: string;
  squad_name: string;
}

/** Body of `POST /api/squads/:id/project`: the project the squad takes, and the status moves a workflow switch needs. */
export interface AssignSquadProjectRequest {
  project_id: string;
  /** Maps each status the SDLC workflow lacks to a status it has. Sent after a 409 asks for it. */
  status_mapping?: Record<string, string>;
}

/** A status some issues are on that the SDLC workflow does not list, and the status the server suggests for them. */
export interface SquadProjectStatusMapping {
  status_key: string;
  issue_count: number;
  suggested_status_key: string;
}

export interface AssignSquadProjectResponse {
  project: { id: string; squad_id: string | null };
  updated_issues: number;
}
