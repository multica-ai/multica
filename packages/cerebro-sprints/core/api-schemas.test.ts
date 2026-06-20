import { describe, expect, it } from "vitest";

import {
  selectableSprintSchema,
  selectableSprintsListSchema,
  sprintSettingsSchema,
} from "./api-schemas";

// FIR-1657: the settings + selectable-sprints contracts must survive an older
// backend that doesn't yet send the new fields (CLAUDE.md → API Response
// Compatibility). These tests feed minimal/malformed payloads and assert the
// schema degrades to safe defaults instead of throwing.

describe("sprintSettingsSchema", () => {
  const base = {
    project_id: "p1",
    workspace_id: "w1",
    enabled: true,
    duration_unit: "week",
    duration_count: 2,
    start_weekday: 1,
    name_template: "Sprint {n}",
    auto_create_enabled: true,
    auto_create_lead_days: 2,
    move_incomplete_enabled: true,
    move_incomplete_target_status: "todo",
    timezone: "UTC",
    created_at: "2026-06-20T00:00:00Z",
    updated_at: "2026-06-20T00:00:00Z",
  };

  it("defaults accepts_external_issues to false when the backend omits it", () => {
    const parsed = sprintSettingsSchema.parse(base);
    expect(parsed.accepts_external_issues).toBe(false);
  });

  it("preserves accepts_external_issues when present", () => {
    const parsed = sprintSettingsSchema.parse({ ...base, accepts_external_issues: true });
    expect(parsed.accepts_external_issues).toBe(true);
  });
});

describe("selectableSprintSchema", () => {
  const sprint = {
    id: "s1",
    workspace_id: "w1",
    project_id: "p2",
    name: "Sprint 1",
    sequence_no: 1,
    status: "planned",
    start_date: "2026-06-01",
    end_date: "2026-06-14",
    created_at: "2026-06-20T00:00:00Z",
    updated_at: "2026-06-20T00:00:00Z",
  };

  it("defaults project_title and is_own_project when missing", () => {
    const parsed = selectableSprintSchema.parse(sprint);
    expect(parsed.project_title).toBe("");
    expect(parsed.is_own_project).toBe(false);
  });

  it("parses a full row with grouping fields", () => {
    const parsed = selectableSprintSchema.parse({
      ...sprint,
      project_title: "Firtal Enable",
      is_own_project: true,
    });
    expect(parsed.project_title).toBe("Firtal Enable");
    expect(parsed.is_own_project).toBe(true);
  });

  it("list schema accepts an empty sprints array", () => {
    const parsed = selectableSprintsListSchema.parse({ sprints: [] });
    expect(parsed.sprints).toEqual([]);
  });
});
