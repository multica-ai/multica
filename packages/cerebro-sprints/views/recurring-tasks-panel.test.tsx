import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

const mockCerebroRequest = vi.hoisted(() => vi.fn());
const mockListMembers = vi.hoisted(() => vi.fn());
const mockListAgents = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      ...actual.api,
      cerebroRequest: mockCerebroRequest,
      listMembers: mockListMembers,
      listAgents: mockListAgents,
    },
  };
});

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { RecurringTasksPanel, sprintWeekdayLabel } from "./recurring-tasks-panel";

function renderPanel() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <RecurringTasksPanel workspaceId="ws-1" projectId="project-1" />
    </QueryClientProvider>,
  );
}

const sprintSettings = {
  project_id: "project-1",
  workspace_id: "ws-1",
  enabled: true,
  duration_unit: "week",
  duration_count: 2,
  start_weekday: 1,
  name_template: "Sprint {n}",
  auto_create_enabled: true,
  auto_create_lead_days: 0,
  move_incomplete_enabled: false,
  move_incomplete_target_status: "todo",
  timezone: "Europe/Copenhagen",
  accepts_external_issues: false,
  created_at: "2026-06-20T00:00:00Z",
  updated_at: "2026-06-20T00:00:00Z",
};

describe("RecurringTasksPanel", () => {
  it("translates a sprint day into a weekday from the project start weekday", () => {
    expect(sprintWeekdayLabel(1, 1)).toBe("Monday");
    expect(sprintWeekdayLabel(1, 3)).toBe("Wednesday");
    expect(sprintWeekdayLabel(5, 4)).toBe("Monday");
  });

  it("lets a new recurring task choose a member assignee and shows the weekday", async () => {
    const user = userEvent.setup();
    mockListMembers.mockResolvedValueOnce([
      { user_id: "user-1", name: "Anna", email: "anna@example.test", role: "member" },
    ]);
    mockListAgents.mockResolvedValueOnce([
      { id: "agent-1", name: "Lone", archived_at: null },
    ]);
    mockCerebroRequest.mockImplementation(async (path: string, init?: RequestInit) => {
      if (path.endsWith("/sprint-settings")) return sprintSettings;
      if (path.endsWith("/sprint-recurring-tasks") && !init) return { recurring_tasks: [] };
      if (path.endsWith("/sprint-recurring-tasks") && init?.method === "POST") {
        return {
          id: "rt-1",
          workspace_id: "ws-1",
          project_id: "project-1",
          created_at: "",
          updated_at: "",
          ...JSON.parse(String(init.body)),
        };
      }
      throw new Error(`Unexpected request ${path}`);
    });

    renderPanel();

    await user.click(await screen.findByRole("button", { name: "Add recurring task" }));
    expect(screen.getByText("Day 1 is Monday.")).toBeTruthy();

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Write weekly report" } });
    fireEvent.change(screen.getByLabelText("Sprint day"), { target: { value: "3" } });
    expect(screen.getByText("Day 3 is Wednesday.")).toBeTruthy();

    await user.click(screen.getByLabelText("Assignee"));
    await user.click(screen.getByRole("option", { name: "Anna" }));
    expect(screen.getByLabelText("Assignee").textContent).toContain("Anna");
    expect(screen.getByLabelText("Assignee").textContent).not.toContain("member:member-1");
    await user.click(screen.getByRole("button", { name: "Add" }));

    await waitFor(() => {
      expect(mockCerebroRequest).toHaveBeenCalledWith(
        "/api/cerebro/projects/project-1/sprint-recurring-tasks",
        expect.objectContaining({
          method: "POST",
          body: expect.stringContaining('"assignee_type":"member"'),
        }),
      );
    });
    expect(
      mockCerebroRequest.mock.calls.some(([, init]) =>
        String((init as RequestInit | undefined)?.body ?? "").includes('"assignee_id":"user-1"'),
      ),
    ).toBe(true);
  });

  it("supports editing the assignee on an existing recurring task", async () => {
    const user = userEvent.setup();
    mockListMembers.mockResolvedValueOnce([
      { user_id: "user-1", name: "Anna", email: "anna@example.test", role: "member" },
    ]);
    mockListAgents.mockResolvedValueOnce([
      { id: "agent-1", name: "Lone", archived_at: null },
    ]);
    mockCerebroRequest.mockImplementation(async (path: string, init?: RequestInit) => {
      if (path.endsWith("/sprint-settings")) return sprintSettings;
      if (path.endsWith("/sprint-recurring-tasks") && !init) {
        return {
          recurring_tasks: [
            {
              id: "rt-1",
              workspace_id: "ws-1",
              project_id: "project-1",
              cadence_unit: "week",
              cadence_count: 1,
              title: "Write weekly report",
              description: "",
              priority: "",
              sprint_day_offset: 3,
              enabled: true,
              created_at: "",
              updated_at: "",
            },
          ],
        };
      }
      if (path.endsWith("/sprint-recurring-tasks/rt-1") && init?.method === "PUT") {
        return {
          id: "rt-1",
          workspace_id: "ws-1",
          project_id: "project-1",
          created_at: "",
          updated_at: "",
          ...JSON.parse(String(init.body)),
        };
      }
      throw new Error(`Unexpected request ${path}`);
    });

    renderPanel();

    const row = await screen.findByText("Write weekly report");
    await user.click(within(row.closest("li")!).getByRole("button", { name: "Edit" }));
    await user.click(screen.getByLabelText("Assignee"));
    await user.click(await screen.findByRole("option", { name: "Lone" }));
    expect(screen.getByLabelText("Assignee").textContent).toContain("Lone");
    expect(screen.getByLabelText("Assignee").textContent).not.toContain("agent:agent-1");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockCerebroRequest).toHaveBeenCalledWith(
        "/api/cerebro/sprint-recurring-tasks/rt-1",
        expect.objectContaining({
          method: "PUT",
          body: expect.stringContaining('"assignee_type":"agent"'),
        }),
      );
    });
    expect(
      mockCerebroRequest.mock.calls.some(([, init]) =>
        String((init as RequestInit | undefined)?.body ?? "").includes('"assignee_id":"agent-1"'),
      ),
    ).toBe(true);
  });
});
