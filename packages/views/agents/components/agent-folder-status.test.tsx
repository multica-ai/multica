// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { AgentPresenceDetail } from "@multica/core/agents";
import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../locales/en/agents.json";
import enAgentFolders from "../../locales/en/agent-folders.json";
import type { AgentListRow } from "./agents-page";
import {
  AgentFolderStatusCell,
  aggregateAgentFolderPresence,
} from "./agent-folder-status";

const TEST_RESOURCES = { en: { agents: enAgents, "agent-folders": enAgentFolders } };

function presence(
  availability: AgentPresenceDetail["availability"],
  workload: AgentPresenceDetail["workload"],
  runningCount = 0,
  queuedCount = 0,
): AgentPresenceDetail {
  return { availability, workload, runningCount, queuedCount, capacity: 2 };
}

function row(id: string, detail: AgentPresenceDetail | null): AgentListRow {
  return {
    agent: { id, name: id },
    presence: detail,
  } as AgentListRow;
}

function renderCell(rows: AgentListRow[]) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AgentFolderStatusCell rows={rows} />
    </I18nProvider>,
  );
}

describe("agent folder presence aggregation", () => {
  it("counts deduplicated member rows by workload and online availability", () => {
    const rows = [
      row("working", presence("online", "working", 2, 1)),
      row("queued", presence("offline", "queued", 0, 3)),
      row("online", presence("online", "idle")),
      row("offline", presence("offline", "idle")),
      row("unknown", null),
    ];
    const result = aggregateAgentFolderPresence(rows);
    expect(result.workingCount).toBe(1);
    expect(result.queuedCount).toBe(1);
    expect(result.onlineCount).toBe(2);
    expect(result.active.map(({ row: member }) => member.agent.id)).toEqual(["working", "queued"]);
  });

  it("shows an empty-folder dash and all four non-empty states", () => {
    const view = renderCell([]);
    expect(screen.getByRole("cell").textContent).toBe("—");

    view.rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <AgentFolderStatusCell rows={[row("working", presence("online", "working", 2))]} />
      </I18nProvider>,
    );
    expect(screen.getByRole("cell").textContent).toBe("1 running · 1 online");

    view.rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <AgentFolderStatusCell rows={[row("queued", presence("offline", "queued", 0, 2))]} />
      </I18nProvider>,
    );
    expect(screen.getByRole("cell").textContent).toBe("1 queued");

    view.rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <AgentFolderStatusCell rows={[row("online", presence("online", "idle"))]} />
      </I18nProvider>,
    );
    expect(screen.getByRole("cell").textContent).toBe("1 online");

    view.rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <AgentFolderStatusCell rows={[row("offline", presence("offline", "idle"))]} />
      </I18nProvider>,
    );
    expect(screen.getByRole("cell").textContent).toBe("Offline");
  });

  it("lists active member names and their running and queued task counts", async () => {
    renderCell([
      row("Worker", presence("online", "working", 2, 1)),
      row("Waiting", presence("offline", "queued", 0, 3)),
      row("Idle", presence("online", "idle")),
    ]);
    await userEvent.hover(screen.getByRole("cell"));
    expect(await screen.findByText("Worker")).toBeInTheDocument();
    expect(screen.getByText("2 running · 1 queued")).toBeInTheDocument();
    expect(screen.getByText("Waiting")).toBeInTheDocument();
    expect(screen.getByText("3 queued")).toBeInTheDocument();
    expect(screen.queryByText("Idle")).not.toBeInTheDocument();
  });
});
