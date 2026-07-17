import { describe, expect, it } from "vitest";

import {
  DEFAULT_TERMINOLOGY,
  EMPTY_ROCKS,
  EMPTY_CONNECTIONS,
  EMPTY_STRATEGY,
  goalTypeListSchema,
  operatingSystemSettingsSchema,
  operatingPeriodListSchema,
  objectConnectionListSchema,
  osElementListSchema,
  rocksListSchema,
  strategyListSchema,
  strategyHistoryListSchema,
  visionPlanSchema,
  EMPTY_VISION_PLAN,
} from "./api-schemas";

describe("operating system API schemas", () => {
  it("falls back to empty lists when list arrays are missing", () => {
    expect(strategyListSchema.safeParse({}).success).toBe(false);
    expect(EMPTY_STRATEGY).toEqual({ strategy_items: [] });
    expect(rocksListSchema.safeParse({}).success).toBe(false);
    expect(EMPTY_ROCKS).toEqual({ rocks: [] });
  });

  it("defaults malformed terminology fields independently to neutral labels", () => {
    const parsed = operatingSystemSettingsSchema.parse({
      workspace_id: "ws-1",
      terminology: { strategy: "Direction", rock: null, rocks: "" },
    });
    expect(parsed.terminology).toEqual({ ...DEFAULT_TERMINOLOGY, strategy: "Direction" });
    expect(parsed.terminology.rock).toBe("Goal");
  });

  it("keeps stored legacy labels while filling missing element labels", () => {
    const parsed = operatingSystemSettingsSchema.parse({
      workspace_id: "ws-1",
      terminology: { strategy: "Strategy", rock: "Rock", rocks: "Rocks" },
    });
    expect(parsed.terminology).toMatchObject({ rock: "Rock", rocks: "Rocks", vision_plan: "Vision Plan", strategy_map: "Strategy Map" });
  });

  it("parses element settings and downgrades missing defaults", () => {
    const parsed = osElementListSchema.parse({ elements: [{ key: "goals", enabled: true, default_enabled: true }, { key: "scorecard", enabled: false }] });
    expect(parsed.elements).toEqual([
      { key: "goals", enabled: true, default_enabled: true },
      { key: "scorecard", enabled: false, default_enabled: false },
    ]);
  });

  it("parses goal types and rejects malformed entries", () => {
    const parsed = goalTypeListSchema.parse({ goal_types: [{ id: "t1", workspace_id: "w1", name: "Company", color: "#22C55E", scope_label: "company-wide", position: 0, created_at: "", updated_at: "" }] });
    expect(parsed.goal_types[0]).toMatchObject({ name: "Company", color: "#22C55E" });
    expect(goalTypeListSchema.safeParse({ goal_types: [{ id: 1 }] }).success).toBe(false);
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

  it("parses shared operating periods and defaults missing units", () => {
    const parsed = operatingPeriodListSchema.parse({ periods: [{ id: "q1", workspace_id: "w1", name: "Q3 2026", starts_on: "2026-07-01", ends_on: "2026-09-30" }] });
    expect(parsed.periods[0]).toMatchObject({ name: "Q3 2026", unit: "quarter" });
    const monthly = operatingPeriodListSchema.parse({ periods: [{ id: "m1", workspace_id: "w1", name: "August 2026", unit: "month", starts_on: "2026-08-01", ends_on: "2026-08-31" }] });
    expect(monthly.periods[0]?.unit).toBe("month");
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

  it("parses Vision Plan sections, structured parts, owners and Goal connections", () => {
    const parsed = visionPlanSchema.parse({ sections: [{
      id: "s1", workspace_id: "w1", key: "marketing-strategy", name: "Marketing Strategy",
      section_type: "structured", position: 3, created_at: "", updated_at: "", items: [{
        id: "i1", workspace_id: "w1", section_id: "s1", title: "Nordic operators", description: "",
        part_label: "Target market", owner_type: "agent", owner_id: "a1", owner_name: "Lone",
        position: 0, state: "active", goal_connections: [{ connection_id: "c1", goal_id: "g1" }], created_at: "", updated_at: "",
      }],
    }] });
    expect(parsed.sections[0]?.items[0]).toMatchObject({ part_label: "Target market", owner_name: "Lone", goal_connections: [{ goal_id: "g1" }] });
    expect(visionPlanSchema.safeParse({ sections: null }).success).toBe(false);
    expect(EMPTY_VISION_PLAN).toEqual({ sections: [] });
  });
});
