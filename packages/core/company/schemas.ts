import { z } from "zod";
import { SquadListSchema } from "../api/schemas";
import type {
  AssignSquadProjectResponse,
  CompanyFloor,
  CompanySeatConflict,
  CompanySeatsResponse,
  SquadProjectStatusMapping,
} from "./types";

// Lenient on purpose (see parseWithFallback): optional fields default, unknown
// fields pass through, and a malformed body degrades to an empty value instead
// of breaking the company page.

export const CompanyFloorSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string().default(""),
    name: z.string(),
    position: z.number().default(0),
    created_at: z.string().default(""),
    updated_at: z.string().default(""),
  })
  .loose();

export const CompanyFloorListSchema = z.array(CompanyFloorSchema);

export const EMPTY_COMPANY_FLOOR_LIST: CompanyFloor[] = [];

export const EMPTY_COMPANY_FLOOR: CompanyFloor = {
  id: "",
  workspace_id: "",
  name: "",
  position: 0,
  created_at: "",
  updated_at: "",
};

export const CompanySeatsResponseSchema = z
  .object({
    squads: SquadListSchema.default([]),
  })
  .loose();

export const EMPTY_COMPANY_SEATS_RESPONSE: CompanySeatsResponse = { squads: [] };

/** The 409 body of `PUT /api/company/seats` when a requested seat is taken. */
export const CompanySeatConflictBodySchema = z
  .object({
    code: z.literal("seat_conflict"),
    conflict: z
      .object({
        floor_id: z.string(),
        slot: z.number(),
        squad_id: z.string(),
        squad_name: z.string().default(""),
      })
      .loose(),
  })
  .loose();

/**
 * Reads the holder out of a failed seat request, or null when the error is not
 * a seat conflict (or its body is not the documented shape).
 */
export function parseCompanySeatConflict(body: unknown): CompanySeatConflict | null {
  const parsed = CompanySeatConflictBodySchema.safeParse(body);
  if (!parsed.success) return null;
  const { floor_id, slot, squad_id, squad_name } = parsed.data.conflict;
  return { floor_id, slot, squad_id, squad_name };
}

export const AssignSquadProjectResponseSchema = z
  .object({
    project: z
      .object({
        id: z.string(),
        squad_id: z.string().nullable().default(null),
      })
      .loose(),
    updated_issues: z.number().default(0),
  })
  .loose();

export const EMPTY_ASSIGN_SQUAD_PROJECT_RESPONSE: AssignSquadProjectResponse = {
  project: { id: "", squad_id: null },
  updated_issues: 0,
};

const SquadProjectMappingBodySchema = z
  .object({
    code: z.literal("workflow_status_mapping_required"),
    plan: z.object({
      required: z.array(
        z.object({
          status_key: z.string(),
          issue_count: z.number(),
          suggested_status_key: z.string().default(""),
        }),
      ),
    }),
  })
  .loose();

/** The statuses a 409 `workflow_status_mapping_required` asks the caller to map, or null for any other error. */
export function parseSquadProjectMapping(body: unknown): SquadProjectStatusMapping[] | null {
  const parsed = SquadProjectMappingBodySchema.safeParse(body);
  return parsed.success ? parsed.data.plan.required : null;
}
