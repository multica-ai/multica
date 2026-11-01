// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Agent, Squad } from "../types";
import { OFFICE_ROOM_DESKS, OFFICE_SLOTS } from "./constants";
import { layoutFloor } from "./layout";

const FLOOR = { id: "f1" };

function squad(id: string, over: Partial<Squad> = {}): Squad {
  return {
    id,
    workspace_id: "ws",
    name: id,
    description: "",
    instructions: "",
    avatar_url: null,
    leader_id: `${id}-lead`,
    creator_id: "u",
    created_at: "",
    updated_at: "",
    archived_at: null,
    archived_by: null,
    floor_id: "f1",
    office_slot: 0,
    ...over,
  };
}

function agent(id: string, home: string | null, over: Partial<Agent> = {}): Agent {
  return { id, home_squad_id: home, archived_at: null, ...over } as Agent;
}

describe("layoutFloor", () => {
  it("always returns one room per slot, empty when nothing is seated", () => {
    const layout = layoutFloor(FLOOR, [], []);
    expect(layout.rooms).toHaveLength(OFFICE_SLOTS);
    expect(layout.rooms.map((r) => r.slot)).toEqual([0, 1, 2, 3, 4, 5]);
    expect(layout.rooms.every((r) => r.squad === null && r.agentIds.length === 0)).toBe(true);
    expect(layout.sessions).toEqual([]);
    expect(layout.unseatedSquads).toEqual([]);
    expect(layout.unassignedAgents).toEqual([]);
  });

  it("places squads in their slots, slots 0 to 5", () => {
    const squads = [0, 1, 2, 3, 4, 5].map((slot) => squad(`s${slot}`, { office_slot: 5 - slot }));
    const layout = layoutFloor(FLOOR, squads, []);
    expect(layout.rooms.map((r) => r.squad?.id)).toEqual(["s5", "s4", "s3", "s2", "s1", "s0"]);
  });

  it("seats an agent in its home squad only", () => {
    const squads = [squad("a", { office_slot: 0 }), squad("b", { office_slot: 1 })];
    const agents = [agent("x", "a"), agent("y", "b"), agent("z", "a")];
    const layout = layoutFloor(FLOOR, squads, agents);
    expect(layout.rooms[0]!.agentIds).toEqual(["x", "z"]);
    expect(layout.rooms[1]!.agentIds).toEqual(["y"]);
    expect(layout.sessions).toEqual([
      { agentId: "x", squadId: "a", slot: 0, desk: 0 },
      { agentId: "z", squadId: "a", slot: 0, desk: 1 },
      { agentId: "y", squadId: "b", slot: 1, desk: 0 },
    ]);
  });

  it("puts sessions in slot order, whatever the squad order", () => {
    const squads = [squad("late", { office_slot: 4 }), squad("early", { office_slot: 1 })];
    const layout = layoutFloor(FLOOR, squads, [agent("l", "late"), agent("e", "early")]);
    expect(layout.sessions.map((s) => s.agentId)).toEqual(["e", "l"]);
  });

  it("seats the leader first, then members in the order they joined", () => {
    const squads = [squad("a", { leader_id: "boss" })];
    const agents = [agent("m1", "a"), agent("boss", "a"), agent("m2", "a"), agent("m0", "a")];
    const members = {
      a: [
        { member_type: "agent", member_id: "m0" },
        { member_type: "agent", member_id: "boss" },
        { member_type: "agent", member_id: "m2" },
        { member_type: "agent", member_id: "m1" },
      ] as const,
    };
    const layout = layoutFloor(FLOOR, squads, agents, members);
    expect(layout.rooms[0]!.agentIds).toEqual(["boss", "m0", "m2", "m1"]);
  });

  it("falls back to the agent order when member order is unknown or partial", () => {
    const squads = [squad("a", { leader_id: "boss" })];
    const agents = [agent("c", "a"), agent("b", "a"), agent("boss", "a"), agent("a1", "a")];
    expect(layoutFloor(FLOOR, squads, agents).rooms[0]!.agentIds).toEqual(["boss", "c", "b", "a1"]);

    const partial = { a: [{ member_type: "agent", member_id: "a1" }] as const };
    expect(layoutFloor(FLOOR, squads, agents, partial).rooms[0]!.agentIds).toEqual(["boss", "a1", "c", "b"]);
  });

  it("skips human members when ordering", () => {
    const squads = [squad("a", { leader_id: "boss" })];
    const agents = [agent("w2", "a"), agent("w1", "a"), agent("boss", "a")];
    const members = {
      a: [
        { member_type: "member", member_id: "w2" }, // a person who shares an id with nothing
        { member_type: "agent", member_id: "w1" },
        { member_type: "agent", member_id: "w2" },
      ] as const,
    };
    const layout = layoutFloor(FLOOR, squads, agents, members);
    expect(layout.rooms[0]!.agentIds).toEqual(["boss", "w1", "w2"]);
  });

  it("never draws a human: members are not agents and have no desk", () => {
    const squads = [squad("a")];
    const layout = layoutFloor(FLOOR, squads, [], { a: [{ member_type: "member", member_id: "u1" }] });
    expect(layout.sessions).toEqual([]);
    expect(layout.rooms[0]!.agentIds).toEqual([]);
  });

  it("keeps the leader first even when the leader joined last", () => {
    const squads = [squad("a", { leader_id: "boss" })];
    const members = {
      a: [
        { member_type: "agent", member_id: "w" },
        { member_type: "agent", member_id: "boss" },
      ] as const,
    };
    const layout = layoutFloor(FLOOR, squads, [agent("w", "a"), agent("boss", "a")], members);
    expect(layout.rooms[0]!.agentIds).toEqual(["boss", "w"]);
  });

  it("does not seat a leader whose home is another squad", () => {
    const squads = [squad("a", { leader_id: "boss", office_slot: 0 }), squad("b", { office_slot: 1 })];
    const layout = layoutFloor(FLOOR, squads, [agent("boss", "b"), agent("w", "a")]);
    expect(layout.rooms[0]!.agentIds).toEqual(["w"]);
    expect(layout.rooms[1]!.agentIds).toEqual(["boss"]);
  });

  describe("overflow past the desks", () => {
    const many = (n: number) => Array.from({ length: n }, (_, i) => agent(`w${i}`, "a"));

    it("fills exactly the desks without overflow", () => {
      const layout = layoutFloor(FLOOR, [squad("a")], many(OFFICE_ROOM_DESKS));
      expect(layout.rooms[0]!.agentIds).toHaveLength(OFFICE_ROOM_DESKS);
      expect(layout.rooms[0]!.overflow).toBe(0);
      expect(layout.overflowTotal).toBe(0);
    });

    it("seats the first ten and counts the rest", () => {
      const layout = layoutFloor(FLOOR, [squad("a")], many(OFFICE_ROOM_DESKS + 3));
      expect(layout.rooms[0]!.agentIds).toEqual(many(OFFICE_ROOM_DESKS).map((a) => a.id));
      expect(layout.rooms[0]!.overflow).toBe(3);
      expect(layout.sessions).toHaveLength(OFFICE_ROOM_DESKS);
      expect(layout.overflowTotal).toBe(3);
    });

    it("keeps the leader on a desk when the room overflows", () => {
      const agents = [...many(OFFICE_ROOM_DESKS + 2), agent("boss", "a")];
      const layout = layoutFloor(FLOOR, [squad("a", { leader_id: "boss" })], agents);
      expect(layout.rooms[0]!.agentIds[0]).toBe("boss");
      expect(layout.rooms[0]!.overflow).toBe(3);
    });

    it("sums overflow across rooms", () => {
      const squads = [squad("a", { office_slot: 0 }), squad("b", { office_slot: 1 })];
      const agents = [...many(OFFICE_ROOM_DESKS + 1), ...Array.from({ length: OFFICE_ROOM_DESKS + 2 }, (_, i) => agent(`v${i}`, "b"))];
      expect(layoutFloor(FLOOR, squads, agents).overflowTotal).toBe(3);
    });
  });

  describe("unassigned agents", () => {
    it("lists agents with no home", () => {
      const layout = layoutFloor(FLOOR, [squad("a")], [agent("x", null), agent("y", "a"), { id: "old" } as Agent]);
      expect(layout.unassignedAgents.map((a) => a.id)).toEqual(["x", "old"]);
    });

    it("treats a home that is archived or unknown as unassigned", () => {
      const squads = [squad("gone", { archived_at: "2026-01-01T00:00:00Z" })];
      const layout = layoutFloor(FLOOR, squads, [agent("x", "gone"), agent("y", "nope")]);
      expect(layout.unassignedAgents.map((a) => a.id)).toEqual(["x", "y"]);
    });

    it("skips archived agents entirely", () => {
      const layout = layoutFloor(FLOOR, [squad("a")], [agent("x", "a", { archived_at: "t" }), agent("y", null, { archived_at: "t" })]);
      expect(layout.sessions).toEqual([]);
      expect(layout.unassignedAgents).toEqual([]);
    });

    it("does not call an agent homed in a squad on another floor unassigned or draw it here", () => {
      const squads = [squad("a", { floor_id: "f2" })];
      const layout = layoutFloor(FLOOR, squads, [agent("x", "a")]);
      expect(layout.unassignedAgents).toEqual([]);
      expect(layout.sessions).toEqual([]);
    });

    it("counts an agent whose home squad is unseated as assigned but not drawn", () => {
      const squads = [squad("a", { floor_id: null, office_slot: null })];
      const layout = layoutFloor(FLOOR, squads, [agent("x", "a")]);
      expect(layout.unassignedAgents).toEqual([]);
      expect(layout.sessions).toEqual([]);
    });
  });

  describe("squads", () => {
    it("skips archived squads: not in a room and not unseated", () => {
      const squads = [squad("a", { archived_at: "t" }), squad("b", { archived_at: "t", floor_id: null, office_slot: null })];
      const layout = layoutFloor(FLOOR, squads, [agent("x", "a")]);
      expect(layout.rooms[0]!.squad).toBeNull();
      expect(layout.unseatedSquads).toEqual([]);
    });

    it("lists squads with no seat as unseated, including older servers that omit the fields", () => {
      const legacy = squad("legacy");
      delete legacy.floor_id;
      delete legacy.office_slot;
      const squads = [legacy, squad("none", { floor_id: null, office_slot: null }), squad("seated")];
      const layout = layoutFloor(FLOOR, squads, []);
      expect(layout.unseatedSquads.map((s) => s.id)).toEqual(["legacy", "none"]);
    });

    it("ignores squads seated on another floor", () => {
      const layout = layoutFloor(FLOOR, [squad("other", { floor_id: "f2" })], []);
      expect(layout.rooms.every((r) => r.squad === null)).toBe(true);
      expect(layout.unseatedSquads).toEqual([]);
    });

    it("treats a slot outside 0 to 5 as unseated", () => {
      for (const slot of [6, -1, 1.5, null]) {
        const layout = layoutFloor(FLOOR, [squad("a", { office_slot: slot })], []);
        expect(layout.unseatedSquads.map((s) => s.id)).toEqual(["a"]);
      }
    });

    it("never drops a squad that claims an occupied seat", () => {
      const layout = layoutFloor(FLOOR, [squad("first"), squad("second")], []);
      expect(layout.rooms[0]!.squad?.id).toBe("first");
      expect(layout.unseatedSquads.map((s) => s.id)).toEqual(["second"]);
    });
  });

  it("lists an agent once when the input repeats it", () => {
    const layout = layoutFloor(FLOOR, [squad("a")], [agent("x", "a"), agent("x", "a")]);
    expect(layout.rooms[0]!.agentIds).toEqual(["x"]);
  });

  it("does not mutate its inputs", () => {
    const squads = [squad("a")];
    const agents = [agent("b", "a"), agent("a1", "a")];
    const snapshot = JSON.stringify({ squads, agents });
    layoutFloor(FLOOR, squads, agents);
    expect(JSON.stringify({ squads, agents })).toBe(snapshot);
  });
});
