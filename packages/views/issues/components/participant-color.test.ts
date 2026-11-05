import { describe, expect, it } from "vitest";
import { PARTICIPANT_COLOR_COUNT, assignParticipantColors, participantColor, participantColorIndex } from "./participant-color";

describe("participantColorIndex", () => {
  it("is stable for the same actor", () => {
    const id = "01a19b67-3481-7501-a876-52413704bce3";
    expect(participantColorIndex("agent", id)).toBe(participantColorIndex("agent", id));
  });

  it("stays inside the palette", () => {
    for (let i = 0; i < 200; i++) {
      const index = participantColorIndex("member", `user-${i}`);
      expect(index).toBeGreaterThanOrEqual(0);
      expect(index).toBeLessThan(PARTICIPANT_COLOR_COUNT);
    }
  });

  it("spreads ids across the palette", () => {
    const used = new Set<number>();
    for (let i = 0; i < 64; i++) used.add(participantColorIndex("agent", `agent-${i}`));
    expect(used.size).toBe(PARTICIPANT_COLOR_COUNT);
  });
});

describe("assignParticipantColors", () => {
  const roster = Array.from({ length: 8 }, (_, i) => ({ type: i % 2 ? "agent" : "member", id: `p-${i}` }));

  it("gives a small roster distinct colours, in roster order", () => {
    const slots = assignParticipantColors(roster);
    expect([...slots.values()]).toEqual([0, 1, 2, 3, 4, 5, 6, 7]);
  });

  it("keeps existing colours when someone joins later", () => {
    const before = assignParticipantColors(roster.slice(0, 5));
    const after = assignParticipantColors(roster);
    for (const [key, slot] of before) expect(after.get(key)).toBe(slot);
  });

  it("falls back to the hash past the palette size", () => {
    const big = [...roster, { type: "member", id: "extra" }];
    const slots = assignParticipantColors(big);
    expect(slots.get("member:extra")).toBe(participantColorIndex("member", "extra"));
  });

  it("is used by participantColor when given", () => {
    const slots = new Map([["agent:a1", 6]]);
    expect(participantColor("agent", "a1", slots)).toBe("var(--participant-6)");
  });
});

describe("participantColor", () => {
  it("maps members and agents to a palette token", () => {
    expect(participantColor("member", "u1")).toMatch(/^var\(--participant-[0-7]\)$/);
    expect(participantColor("agent", "a1")).toMatch(/^var\(--participant-[0-7]\)$/);
  });

  it("leaves system and unknown actors uncoloured", () => {
    expect(participantColor("system", "x")).toBeUndefined();
    expect(participantColor("agent", "")).toBeUndefined();
    expect(participantColor(undefined, undefined)).toBeUndefined();
  });
});
