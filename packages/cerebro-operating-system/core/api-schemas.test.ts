import { describe, expect, it } from "vitest";

import {
  EMPTY_ROCKS,
  EMPTY_CONNECTIONS,
  EMPTY_STRATEGY,
  operatingSystemSettingsSchema,
  objectConnectionListSchema,
  rocksListSchema,
  strategyListSchema,
} from "./api-schemas";

describe("operating system API schemas", () => {
  it("falls back to empty lists when list arrays are missing", () => {
    expect(strategyListSchema.safeParse({}).success).toBe(false);
    expect(EMPTY_STRATEGY).toEqual({ strategy_items: [] });
    expect(rocksListSchema.safeParse({}).success).toBe(false);
    expect(EMPTY_ROCKS).toEqual({ rocks: [] });
  });

  it("defaults malformed terminology fields independently", () => {
    const parsed = operatingSystemSettingsSchema.parse({
      workspace_id: "ws-1",
      terminology: { strategy: "Direction", rock: null, rocks: "" },
    });
    expect(parsed.terminology).toEqual({ strategy: "Direction", rock: "Rock", rocks: "Rocks" });
  });

  it("downgrades unknown strategy and health enum values", () => {
    const strategy = strategyListSchema.parse({
      strategy_items: [{ id: "s1", workspace_id: "w1", kind: "new_kind", title: "North", description: "", position: 0, state: "new_state", created_at: "", updated_at: "" }],
    });
    expect(strategy.strategy_items[0]).toMatchObject({ kind: "unknown", state: "unknown" });

    const rocks = rocksListSchema.parse({
      rocks: [{ project_id: "p1", workspace_id: "w1", project_title: "Project", project_status: "planned", period_start: "2026-07-01", period_end: "2026-09-30", confidence: 50, reported_health: "new", derived_health: { state: "new", reason: "", calculated_at: "" }, issue_count: 0, done_issue_count: 0, blocked_issue_count: 0, created_at: "", updated_at: "" }],
    });
    expect(rocks.rocks[0]).toMatchObject({ reported_health: "unset", derived_health: { state: "unknown" } });
  });

  it("rejects missing issue counts and wrong confidence types", () => {
    expect(rocksListSchema.safeParse({ rocks: [{ confidence: "50" }] }).success).toBe(false);
  });

  it("falls back safely when connection lists drift", () => {
    expect(objectConnectionListSchema.safeParse({ connections: null }).success).toBe(false);
    expect(objectConnectionListSchema.safeParse({ connections: [{ target_id: 4 }] }).success).toBe(false);
    expect(EMPTY_CONNECTIONS).toEqual({ connections: [] });
  });
});
