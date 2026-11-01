import type { Agent } from "../types";

/**
 * The office rooms, one per department, in the order the floor lays them out:
 * a 2x2 grid, product and research on top, planning and general below. The
 * keys are the server's `office_room` values; the page names them.
 */
export const OFFICE_ROOMS = ["product", "research", "planning", "general"] as const;

export type OfficeRoom = (typeof OFFICE_ROOMS)[number];

/** The desks in each office room: the engine seats at most this many workers per room. */
export const OFFICE_ROOM_CAPACITY = 4;

/** The room a new agent starts in, and the one an unknown value falls back to. */
export const DEFAULT_OFFICE_ROOM: OfficeRoom = "general";

/**
 * The room an agent sits in. An agent from a backend that predates the field,
 * or one carrying a room this client does not know, sits in the default room
 * rather than dropping off the floor.
 */
export function officeRoomOf(agent: Pick<Agent, "office_room">): OfficeRoom {
  return isOfficeRoom(agent.office_room) ? agent.office_room : DEFAULT_OFFICE_ROOM;
}

export function isOfficeRoom(value: unknown): value is OfficeRoom {
  return typeof value === "string" && (OFFICE_ROOMS as readonly string[]).includes(value);
}
