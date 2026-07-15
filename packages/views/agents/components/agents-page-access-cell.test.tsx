// @vitest-environment jsdom

import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import { AccessCell, type AgentListRow } from "./agents-page";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

function makeRow(
  overrides: Partial<AgentListRow["agent"]> = {},
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
  };
}

function renderCell(row: AgentListRow) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <table>
        <tbody>
          <tr>
            <AccessCell row={row} />
          </tr>
        </tbody>
      </table>
    </I18nProvider>,
  );
}

describe("AccessCell", () => {
  it("renders 'Owner only' for a private agent", () => {
    renderCell(makeRow({ permission_mode: "private", invocation_targets: [] }));
    expect(screen.getByText("Owner only")).toBeInTheDocument();
  });

  it("renders 'Workspace' for a public_to agent with a workspace target", () => {
    renderCell(
      makeRow({
        permission_mode: "public_to",
        invocation_targets: [{ target_type: "workspace", target_id: "" }],
      }),
    );
    expect(screen.getByText("Workspace")).toBeInTheDocument();
  });

  it("renders 'Specific people' for a public_to agent with a member target", () => {
    renderCell(
      makeRow({
        permission_mode: "public_to",
        invocation_targets: [
          { target_type: "member", target_id: "user-1" },
        ],
      }),
    );
    expect(screen.getByText("Specific people")).toBeInTheDocument();
    expect(screen.queryByText("Workspace")).not.toBeInTheDocument();
  });

  it("prefers 'Workspace' when a workspace target sits alongside member targets", () => {
    renderCell(
      makeRow({
        permission_mode: "public_to",
        invocation_targets: [
          { target_type: "member", target_id: "user-1" },
          { target_type: "workspace", target_id: "" },
        ],
      }),
    );
    expect(screen.getByText("Workspace")).toBeInTheDocument();
  });

  it("renders 'Specific people' for a public_to agent with no targets", () => {
    renderCell(
      makeRow({ permission_mode: "public_to", invocation_targets: [] }),
    );
    expect(screen.getByText("Specific people")).toBeInTheDocument();
  });

  it("fails safe to 'Owner only' when permission_mode is missing", () => {
    const row = makeRow({ permission_mode: undefined as unknown as never, invocation_targets: [] });
    renderCell(row);
    expect(screen.getByText("Owner only")).toBeInTheDocument();
  });
});
