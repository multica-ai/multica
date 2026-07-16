import { describe, expect, it } from "vitest";

import {
  EMPTY_ROCKS,
  EMPTY_CONNECTIONS,
  EMPTY_STRATEGY,
  operatingSystemSettingsSchema,
  operatingPeriodListSchema,
  objectConnectionListSchema,
  rocksListSchema,
  strategyListSchema,
  strategyHistoryListSchema,
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

  it("parses first-class Rocks with optional connections and check-ins", () => {
    const parsed = rocksListSchema.parse({ rocks: [{
      id: "r1", workspace_id: "w1", title: "Independent Rock", description: "",
      owner_type: "agent", owner_id: "a1", owner_name: "Sara", period_id: "q1", period_name: "Q3 2026",
      period_start: "2026-07-01", period_end: "2026-09-30", confidence: 58,
      reported_health: "at_risk", derived_health: { state: "at_risk", reason: "work remains", calculated_at: "" },
      issue_count: 0, done_issue_count: 0, blocked_issue_count: 0, project_count: 0, health_score: 0,
      projects: [], issues: [], check_ins: [{ id: "c1", confidence: 58, reported_health: "at_risk", note: "Blocked", created_by_type: "member", created_by_id: "m1", created_at: "" }],
      created_at: "", updated_at: "",
    }] });
    expect(parsed.rocks[0]).toMatchObject({ id: "r1", title: "Independent Rock", projects: [], check_ins: [{ note: "Blocked" }] });
  });

  it("parses shared operating periods", () => {
    expect(operatingPeriodListSchema.parse({ periods: [{ id: "q1", workspace_id: "w1", name: "Q3 2026", starts_on: "2026-07-01", ends_on: "2026-09-30" }] }).periods[0]?.name).toBe("Q3 2026");
  });

  it("parses durable Strategy history", () => {
    const parsed = strategyHistoryListSchema.parse({ history: [{ id: "h1", strategy_item_id: "s1", action: "updated", title: "Nordic leader", snapshot: { title: "Nordic leader" }, changed_at: "2026-07-16T12:00:00Z" }] });
    expect(parsed.history[0]).toMatchObject({ action: "updated", title: "Nordic leader" });
  });

  it("falls back safely when connection lists drift", () => {
    expect(objectConnectionListSchema.safeParse({ connections: null }).success).toBe(false);
    expect(objectConnectionListSchema.safeParse({ connections: [{ target_id: 4 }] }).success).toBe(false);
    expect(EMPTY_CONNECTIONS).toEqual({ connections: [] });
  });
});
