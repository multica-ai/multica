import type { Agent, Squad, SquadMember } from "../types";
import { COMPANY_ROOM_DESKS, COMPANY_SLOTS } from "./constants";
import type { CompanyFloor } from "./types";

/** What `layoutFloor` needs to know about a squad's members. */
export type FloorMemberRef = Pick<SquadMember, "member_type" | "member_id">;

/** One squad room of the floor. */
export interface FloorRoom {
  /** Room slot, 0 to `COMPANY_SLOTS - 1`; equals the room's index in `rooms`. */
  slot: number;
  /** The live squad seated here, or null for an empty slot. */
  squad: Squad | null;
  /** Agents at the desks, desk 0 first: the leader, then in the order they joined. At most `COMPANY_ROOM_DESKS`. */
  agentIds: string[];
  /** Agents homed here beyond the desks. */
  overflow: number;
}

/** An agent at a desk, in the shape the engine seats. */
export interface FloorSession {
  agentId: string;
  squadId: string;
  slot: number;
  desk: number;
}

export interface FloorLayout {
  floorId: string;
  /** Always `COMPANY_SLOTS` rooms, indexed by slot. */
  rooms: FloorRoom[];
  /** Every seated agent of the floor: slot order, then desk order. */
  sessions: FloorSession[];
  /** Agents past the desks, summed over the rooms. */
  overflowTotal: number;
  /** Live squads that hold no usable seat (not drawn anywhere). */
  unseatedSquads: Squad[];
  /** Live agents with no home squad, or a home that is not a live squad (not drawn anywhere). */
  unassignedAgents: Agent[];
}

function validSlot(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0 && value < COMPANY_SLOTS;
}

/**
 * Lays one floor out: which squad sits in which slot, which agents sit at
 * which desks, who did not fit, and who is not drawn at all.
 *
 * - An agent is drawn only in its home squad's room. Archived agents are
 *   ignored. An agent whose home is missing, archived or unknown is unassigned.
 * - Desks go to the leader first, then agents in the order they were added to
 *   the squad. `membersBySquad` (a squad's members as the server lists them)
 *   supplies that order, and human members in it are skipped; without it, or
 *   for agents it does not list, the order of `agents` decides.
 * - Archived squads are ignored. A live squad with no seat, a slot outside
 *   0..5, or a seat another squad already holds, is unseated.
 * - Squads seated on other floors are not part of this floor, and neither are
 *   their agents.
 *
 * Pure: no ordering or identity is taken from anything but the arguments.
 */
export function layoutFloor(
  floor: Pick<CompanyFloor, "id">,
  squads: readonly Squad[],
  agents: readonly Agent[],
  membersBySquad: Readonly<Record<string, readonly FloorMemberRef[]>> = {},
): FloorLayout {
  const rooms: FloorRoom[] = Array.from({ length: COMPANY_SLOTS }, (_, slot) => ({
    slot,
    squad: null,
    agentIds: [],
    overflow: 0,
  }));
  const unseatedSquads: Squad[] = [];

  const liveSquads = squads.filter((squad) => !squad.archived_at);
  const liveSquadIds = new Set(liveSquads.map((squad) => squad.id));

  for (const squad of liveSquads) {
    if (!squad.floor_id || !validSlot(squad.company_slot)) {
      unseatedSquads.push(squad);
      continue;
    }
    if (squad.floor_id !== floor.id) continue;
    const room = rooms[squad.company_slot]!;
    if (room.squad) {
      // Two squads on one seat cannot happen on the server; never drop one silently.
      unseatedSquads.push(squad);
      continue;
    }
    room.squad = squad;
  }

  // Live agents, first occurrence wins, remembering the input order.
  const seen = new Set<string>();
  const liveAgents: Agent[] = [];
  for (const agent of agents) {
    if (agent.archived_at || seen.has(agent.id)) continue;
    seen.add(agent.id);
    liveAgents.push(agent);
  }
  const inputOrder = new Map(liveAgents.map((agent, index) => [agent.id, index]));

  const unassignedAgents = liveAgents.filter(
    (agent) => !agent.home_squad_id || !liveSquadIds.has(agent.home_squad_id),
  );

  const sessions: FloorSession[] = [];
  let overflowTotal = 0;
  for (const room of rooms) {
    const squad = room.squad;
    if (!squad) continue;

    const joinRank = new Map<string, number>();
    (membersBySquad[squad.id] ?? []).forEach((member) => {
      if (member.member_type !== "agent" || joinRank.has(member.member_id)) return;
      joinRank.set(member.member_id, joinRank.size);
    });
    const rank = (agent: Agent): [number, number] => [
      agent.id === squad.leader_id ? 0 : 1,
      // Listed members by join order; unlisted ones after them, by input order.
      joinRank.has(agent.id) ? joinRank.get(agent.id)! : joinRank.size + inputOrder.get(agent.id)!,
    ];

    const homed = liveAgents
      .filter((agent) => agent.home_squad_id === squad.id)
      .sort((a, b) => {
        const [aLeader, aOrder] = rank(a);
        const [bLeader, bOrder] = rank(b);
        return aLeader - bLeader || aOrder - bOrder;
      });

    room.agentIds = homed.slice(0, COMPANY_ROOM_DESKS).map((agent) => agent.id);
    room.overflow = Math.max(0, homed.length - COMPANY_ROOM_DESKS);
    overflowTotal += room.overflow;
    room.agentIds.forEach((agentId, desk) => {
      sessions.push({ agentId, squadId: squad.id, slot: room.slot, desk });
    });
  }

  return { floorId: floor.id, rooms, sessions, overflowTotal, unseatedSquads, unassignedAgents };
}
