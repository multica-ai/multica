// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { SkillListFilters } from "@multica/core/skills/stores";
import type { Agent, Label, SkillSummary } from "@multica/core/types";
import { rowMatchesFilters, type SkillRow } from "./skill-list-filter";

function makeSkill(overrides: Partial<SkillSummary> = {}): SkillSummary {
  return {
    id: "skill-1",
    workspace_id: "ws-1",
    name: "review-helper",
    description: "",
    config: {},
    created_by: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeLabel(id: string, name = id): Label {
  return {
    id,
    workspace_id: "ws-1",
    resource_type: "skill",
    name,
    color: "#3b82f6",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function makeRow(
  skillOverrides: Partial<SkillSummary> = {},
  rowOverrides: Partial<SkillRow> = {},
): SkillRow {
  return {
    skill: makeSkill(skillOverrides),
    agents: [],
    creator: null,
    runtime: null,
    originType: "manual",
    canEdit: true,
    ...rowOverrides,
  };
}

const EMPTY_FILTERS: SkillListFilters = {
  usage: [],
  origins: [],
  agents: [],
  creators: [],
  labels: [],
};

function withLabels(filters: SkillListFilters, ...ids: string[]): SkillListFilters {
  return { ...filters, labels: ids };
}

describe("rowMatchesFilters — labels dimension", () => {
  const noFilters = EMPTY_FILTERS;

  it("empty labels filter is inactive (all rows pass)", () => {
    const unlabeled = makeRow();
    const labeled = makeRow({ labels: [makeLabel("lab-1")] });
    const empty = makeRow({ labels: [] });
    expect(rowMatchesFilters(unlabeled, noFilters, "")).toBe(true);
    expect(rowMatchesFilters(labeled, noFilters, "")).toBe(true);
    expect(rowMatchesFilters(empty, noFilters, "")).toBe(true);
  });

  it("missing labels fail when a labels filter is on", () => {
    const unlabeled = makeRow();
    expect(rowMatchesFilters(unlabeled, withLabels(noFilters, "lab-1"), "")).toBe(
      false,
    );
  });

  it("empty labels fail when a labels filter is on", () => {
    const empty = makeRow({ labels: [] });
    expect(rowMatchesFilters(empty, withLabels(noFilters, "lab-1"), "")).toBe(
      false,
    );
  });

  it("multi-select labels filter is OR-combined", () => {
    const a = makeRow({ labels: [makeLabel("lab-a")] });
    const b = makeRow({ labels: [makeLabel("lab-b")] });
    const neither = makeRow({ labels: [makeLabel("lab-c")] });
    const filters = withLabels(noFilters, "lab-a", "lab-b");
    expect(rowMatchesFilters(a, filters, "")).toBe(true);
    expect(rowMatchesFilters(b, filters, "")).toBe(true);
    expect(rowMatchesFilters(neither, filters, "")).toBe(false);
  });

  it("labels + usage filter: both must pass (AND)", () => {
    const used = makeRow(
      { labels: [makeLabel("lab-1")] },
      { agents: [{ id: "agent-1" } as Agent] },
    );
    const unused = makeRow({ labels: [makeLabel("lab-1")] }, { agents: [] });
    const filters: SkillListFilters = {
      ...noFilters,
      labels: ["lab-1"],
      usage: ["used"],
    };
    expect(rowMatchesFilters(used, filters, "")).toBe(true);
    expect(rowMatchesFilters(unused, filters, "")).toBe(false);
  });

  it("labels + name search: both must pass (AND)", () => {
    const match = makeRow({
      name: "review-helper",
      labels: [makeLabel("lab-1")],
    });
    const miss = makeRow({
      name: "deploy-notes",
      labels: [makeLabel("lab-1")],
    });
    const filters = withLabels(noFilters, "lab-1");
    expect(rowMatchesFilters(match, filters, "review")).toBe(true);
    expect(rowMatchesFilters(miss, filters, "review")).toBe(false);
  });
});
