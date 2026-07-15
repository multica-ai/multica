import { describe, expect, it, vi } from "vitest";
import { createStore } from "zustand/vanilla";
import {
  issueViewDefinitionFromState,
  issueViewDefinitionsEqual,
  issueViewStateFromDefinition,
  type IssueViewState,
  viewStoreSlice,
} from "../issues/stores/view-store";
import { applyIssueViewTemplate } from "./templates";

function baseDefinition() {
  const state = createStore<IssueViewState>()((set) =>
    viewStoreSlice(set),
  ).getState();
  return {
    ...issueViewDefinitionFromState(state),
    statusFilters: ["todo" as const],
    priorityFilters: ["high" as const],
    includeNoAssignee: true,
  };
}

describe("applyIssueViewTemplate", () => {
  it.each([
    ["blocked", "statusFilters", ["blocked"]],
    ["needs_review", "statusFilters", ["in_review"]],
    ["urgent", "priorityFilters", ["urgent"]],
  ] as const)("applies the %s preset", (template, key, expected) => {
    const result = applyIssueViewTemplate(baseDefinition(), template);
    expect(result[key]).toEqual(expected);
    expect(result.includeNoAssignee).toBe(false);
  });

  it("applies the unassigned preset", () => {
    const result = applyIssueViewTemplate(baseDefinition(), "unassigned");
    expect(result.includeNoAssignee).toBe(true);
    expect(result.statusFilters).toEqual([]);
    expect(result.priorityFilters).toEqual([]);
  });
});

describe("relative saved-view dates", () => {
  it("resolves presets when opened without becoming dirty as bounds age", () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(new Date(2026, 6, 15, 12));
      const definition = {
        ...baseDefinition(),
        dateFilter: {
          field: "created_at" as const,
          from: "2020-01-01",
          to: "2020-01-07",
          preset: "last_7_days" as const,
        },
      };

      expect(issueViewStateFromDefinition(definition).dateFilter).toEqual({
        field: "created_at",
        from: "2026-07-09",
        to: "2026-07-15",
        preset: "last_7_days",
      });
      expect(issueViewDefinitionsEqual(definition, {
        ...definition,
        dateFilter: { ...definition.dateFilter, from: "2026-07-09", to: "2026-07-15" },
      })).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });
});
