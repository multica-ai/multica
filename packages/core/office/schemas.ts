import { z } from "zod";
import { SquadListSchema } from "../api/schemas";
import type { OfficeFloor, OfficeSeatConflict, OfficeSeatsResponse } from "./types";

// Lenient on purpose (see parseWithFallback): optional fields default, unknown
// fields pass through, and a malformed body degrades to an empty value instead
// of breaking the office page.

export const OfficeFloorSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string().default(""),
    name: z.string(),
    position: z.number().default(0),
    created_at: z.string().default(""),
    updated_at: z.string().default(""),
  })
  .loose();

export const OfficeFloorListSchema = z.array(OfficeFloorSchema);

export const EMPTY_OFFICE_FLOOR_LIST: OfficeFloor[] = [];

export const EMPTY_OFFICE_FLOOR: OfficeFloor = {
  id: "",
  workspace_id: "",
  name: "",
  position: 0,
  created_at: "",
  updated_at: "",
};

export const OfficeSeatsResponseSchema = z
  .object({
    squads: SquadListSchema.default([]),
  })
  .loose();

export const EMPTY_OFFICE_SEATS_RESPONSE: OfficeSeatsResponse = { squads: [] };

/** The 409 body of `PUT /api/office/seats` when a requested seat is taken. */
export const OfficeSeatConflictBodySchema = z
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
export function parseOfficeSeatConflict(body: unknown): OfficeSeatConflict | null {
  const parsed = OfficeSeatConflictBodySchema.safeParse(body);
  if (!parsed.success) return null;
  const { floor_id, slot, squad_id, squad_name } = parsed.data.conflict;
  return { floor_id, slot, squad_id, squad_name };
}
