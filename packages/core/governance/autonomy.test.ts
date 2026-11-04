import { describe, expect, it } from "vitest";
import { autonomyLevelName, parseSquadAutonomy, FALLBACK_SQUAD_AUTONOMY } from "./autonomy";

describe("parseSquadAutonomy", () => {
  it("reads a level with its name, reason and time", () => {
    const view = parseSquadAutonomy(
      { squad_id: "s1", level: 2, level_name: "Trusted", reason: "Two clean weeks.", changed_at: "2026-11-01T10:00:00Z" },
      "GET /api/squads/:id/autonomy",
    );
    expect(view).toMatchObject({ squad_id: "s1", level: 2, level_name: "Trusted", reason: "Two clean weeks." });
    expect(view.changed_at).toBe("2026-11-01T10:00:00Z");
  });

  it("defaults a squad that never changed to L1 with no reason", () => {
    const view = parseSquadAutonomy({ squad_id: "s1", level: 1, reason: null, changed_at: null }, "x");
    expect(view.level).toBe(1);
    expect(view.level_name).toBe("Work");
    expect(view.reason).toBe("");
    expect(view.changed_at).toBeNull();
  });

  it("falls back to the default when the body is malformed", () => {
    expect(parseSquadAutonomy("not an object", "x")).toEqual(FALLBACK_SQUAD_AUTONOMY);
  });

  it("keeps a level from a newer server by number, with no name", () => {
    const view = parseSquadAutonomy({ squad_id: "s1", level: 7 }, "x");
    expect(view.level).toBe(7);
    expect(view.level_name).toBe("");
  });

  it("names the four levels of spec 4.3", () => {
    expect([0, 1, 2, 3].map(autonomyLevelName)).toEqual(["Suggest", "Work", "Trusted", "Autonomous"]);
  });
});
