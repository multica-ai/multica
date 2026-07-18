import { describe, it, expect } from "vitest";
import {
  effectiveAccessScope,
  type AccessScope,
} from "@multica/core/agents";
import {
  EMPTY_AGENT_FILTERS,
  type AgentListFilters,
} from "@multica/core/agents/stores";
import { rowMatchesFilters, type AgentListRow } from "./agents-page";

function makeRow(
  overrides: Partial<AgentListRow["agent"]> = {},
  rowOverrides: Partial<AgentListRow> = {},
): AgentListRow {
  return {
    agent: {
      id: "agent-1",
      workspace_id: "ws-1",
      runtime_id: "rt-1",
      name: "Test",
      description: "",
      instructions: "",
      avatar_url: null,
      runtime_mode: "local",
      runtime_config: {},
      max_concurrent_tasks: 1,
      owner_id: "user-1",
      archived_at: null,
      custom_args: [],
      visibility: "private",
      permission_mode: "private",
      invocation_targets: [],
      model: "claude",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      status: "idle",
      skills: [],
      archived_by: null,
      ...overrides,
    },
    runtime: null,
    presence: null,
    activity: null,
    runCount: 0,
    lastActiveDays: null,
    owner: null,
    isOwnedByMe: false,
    canManage: false,
    ...rowOverrides,
  };
}

function withAccess(
  filters: AgentListFilters,
  value: AccessScope,
): AgentListFilters {
  return { ...filters, access: [value] };
}

describe("rowMatchesFilters — access dimension", () => {
  const noFilters = EMPTY_AGENT_FILTERS;

  it("empty access filter is inactive (all rows pass)", () => {
    const ownerOnly = makeRow({ permission_mode: "private" });
    const workspace = makeRow({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "workspace", target_id: null }],
    });
    const specific = makeRow({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "member", target_id: "u" }],
    });
    expect(rowMatchesFilters(ownerOnly, noFilters, "")).toBe(true);
    expect(rowMatchesFilters(workspace, noFilters, "")).toBe(true);
    expect(rowMatchesFilters(specific, noFilters, "")).toBe(true);
  });

  it("filters to owner-only", () => {
    const ownerOnly = makeRow({ permission_mode: "private" });
    const workspace = makeRow({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "workspace", target_id: null }],
    });
    expect(rowMatchesFilters(ownerOnly, withAccess(noFilters, "owner-only"), "")).toBe(true);
    expect(rowMatchesFilters(workspace, withAccess(noFilters, "owner-only"), "")).toBe(false);
  });

  it("filters to workspace", () => {
    const ownerOnly = makeRow({ permission_mode: "private" });
    const workspace = makeRow({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "workspace", target_id: "" }],
    });
    const specific = makeRow({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "member", target_id: "u" }],
    });
    expect(rowMatchesFilters(workspace, withAccess(noFilters, "workspace"), "")).toBe(true);
    expect(rowMatchesFilters(ownerOnly, withAccess(noFilters, "workspace"), "")).toBe(false);
    expect(rowMatchesFilters(specific, withAccess(noFilters, "workspace"), "")).toBe(false);
  });

  it("filters to specific-people", () => {
    const ownerOnly = makeRow({ permission_mode: "private" });
    const workspace = makeRow({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "workspace", target_id: null }],
    });
    const specific = makeRow({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "member", target_id: "u" }],
    });
    expect(rowMatchesFilters(specific, withAccess(noFilters, "specific-people"), "")).toBe(true);
    expect(rowMatchesFilters(workspace, withAccess(noFilters, "specific-people"), "")).toBe(false);
    expect(rowMatchesFilters(ownerOnly, withAccess(noFilters, "specific-people"), "")).toBe(false);
  });

  it("multi-select access filter is OR-combined", () => {
    const ownerOnly = makeRow({ permission_mode: "private" });
    const workspace = makeRow({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "workspace", target_id: null }],
    });
    const filters: AgentListFilters = { ...noFilters, access: ["owner-only", "workspace"] };
    expect(rowMatchesFilters(ownerOnly, filters, "")).toBe(true);
    expect(rowMatchesFilters(workspace, filters, "")).toBe(true);
  });

  it("access filter combines with other dimensions (AND)", () => {
    const ownerUser1 = makeRow(
      { owner_id: "user-1" },
      { isOwnedByMe: true },
    );
    const ownerUser2 = makeRow(
      { owner_id: "user-2" },
      { isOwnedByMe: false },
    );
    const filters: AgentListFilters = {
      ...noFilters,
      access: ["owner-only"],
      owners: ["user-1"],
    };
    expect(rowMatchesFilters(ownerUser1, filters, "")).toBe(true);
    expect(rowMatchesFilters(ownerUser2, filters, "")).toBe(false);
  });

  it("access + availability filter: both must pass (AND)", () => {
    const onlineAgent = makeRow(
      { permission_mode: "public_to", invocation_targets: [{ target_type: 'workspace', target_id: '' }] },
      { presence: { availability: "online" } as AgentListRow["presence"] },
    );
    const offlineAgent = makeRow(
      { permission_mode: "public_to", invocation_targets: [{ target_type: 'workspace', target_id: '' }] },
      { presence: { availability: "offline" } as AgentListRow["presence"] },
    );
    const filters: AgentListFilters = {
      ...noFilters,
      access: ["workspace"],
      availability: ["online"],
    };
    expect(rowMatchesFilters(onlineAgent, filters, "")).toBe(true);
    expect(rowMatchesFilters(offlineAgent, filters, "")).toBe(false);
  });

  it("access + runtimes filter: both must pass (AND)", () => {
    const localAgent = makeRow(
      { permission_mode: "private", runtime_id: "rt-local" },
    );
    const cloudAgent = makeRow(
      { permission_mode: "private", runtime_id: "rt-cloud" },
    );
    const filters: AgentListFilters = {
      ...noFilters,
      access: ["owner-only"],
      runtimes: ["rt-local"],
    };
    expect(rowMatchesFilters(localAgent, filters, "")).toBe(true);
    expect(rowMatchesFilters(cloudAgent, filters, "")).toBe(false);
  });

  it("access filter value matches the same derivation as effectiveAccessScope", () => {
    // The column and the filter share one derivation — guard against drift.
    const cases: Array<{
      label: string;
      row: AgentListRow;
      expected: "workspace" | "specific-people" | "owner-only";
    }> = [
      {
        label: "private",
        row: makeRow({ permission_mode: "private" }),
        expected: "owner-only",
      },
      {
        label: "public_to + workspace",
        row: makeRow({
          permission_mode: "public_to",
          invocation_targets: [{ target_type: "workspace", target_id: "" }],
        }),
        expected: "workspace",
      },
      {
        label: "public_to + member",
        row: makeRow({
          permission_mode: "public_to",
          invocation_targets: [{ target_type: "member", target_id: "u" }],
        }),
        expected: "specific-people",
      },
      {
        label: "public_to + no targets",
        row: makeRow({ permission_mode: "public_to", invocation_targets: [] }),
        expected: "specific-people",
      },
    ];
    for (const { label, row, expected } of cases) {
      expect(
        effectiveAccessScope(row.agent.permission_mode, row.agent.invocation_targets),
        label,
      ).toBe(expected);
    }
  });
});
