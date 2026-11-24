// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { buildHeatmap, splitActors } from "./contribution-utils";

describe("contribution utils", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // Wednesday 2026-11-18, midday UTC.
    vi.setSystemTime(new Date("2026-11-18T12:00:00Z"));
  });
  afterEach(() => vi.useRealTimers());

  it("splits actors into people and agents and drops anything else", () => {
    const base = { created: 0, commented: 0, status_changed: 0, completed: 0, participated: 1 };
    const { members, agents } = splitActors([
      { ...base, actor_type: "member", actor_id: "m" },
      { ...base, actor_type: "agent", actor_id: "a" },
      { ...base, actor_type: "system", actor_id: "" },
    ]);
    expect(members.map((m) => m.actor_id)).toEqual(["m"]);
    expect(agents.map((a) => a.actor_id)).toEqual(["a"]);
  });

  it("lays the heatmap out Monday-first and marks days outside the window", () => {
    const grid = buildHeatmap(
      [
        { date: "2026-11-16", active: 4, collaborated: 0, delivered: 0 },
        { date: "2026-11-17", active: 1, collaborated: 0, delivered: 0 },
        { date: "2026-11-18", active: 0, collaborated: 0, delivered: 0 },
      ],
      "UTC",
      2,
    );
    expect(grid).toHaveLength(2);
    const thisWeek = grid[1]!;
    expect(thisWeek[0]).toMatchObject({ date: "2026-11-16", level: 4, inRange: true });
    expect(thisWeek[1]).toMatchObject({ date: "2026-11-17", level: 1 });
    expect(thisWeek[2]).toMatchObject({ date: "2026-11-18", level: 0, inRange: true });
    // Tomorrow and last week are not in the summary.
    expect(thisWeek[3]?.inRange).toBe(false);
    expect(grid[0]![0]?.inRange).toBe(false);
  });
});
