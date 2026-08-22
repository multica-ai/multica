// @vitest-environment jsdom

import { cleanup, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getIssueSchedule: vi.fn(),
    createIssueSchedule: vi.fn(),
    cancelIssueSchedule: vi.fn(),
  },
  ApiError: class ApiError extends Error {
    status: number;
    constructor(message: string, status: number) {
      super(message);
      this.status = status;
    }
  },
}));

import { api } from "@multica/core/api";
import { ScheduleRunDialog } from "./schedule-run-dialog";

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-1",
    workspace_id: "workspace-1",
    number: 1,
    identifier: "ACM-1",
    title: "Test issue",
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: "agent",
    assignee_id: "agent-1",
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
    ...overrides,
  };
}

function renderDialog(issue: Issue) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ScheduleRunDialog issue={issue} open onOpenChange={() => {}} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
});

afterEach(cleanup);

describe("ScheduleRunDialog", () => {
  it("tells the user to assign the issue first when it has no assignee", async () => {
    renderDialog(makeIssue({ assignee_type: null, assignee_id: null }));

    expect(
      await screen.findByText(/Assign this issue to someone before scheduling a run/),
    ).toBeInTheDocument();
    // The date/time picker must not render when there is nothing to schedule
    // against — a filled-in form the submit button would immediately reject
    // is worse than no form.
    expect(screen.queryByText("Date")).not.toBeInTheDocument();
  });

  it("shows the create form for an assigned issue with no pending schedule", async () => {
    vi.mocked(api.getIssueSchedule).mockResolvedValue(null);
    renderDialog(makeIssue());

    expect(await screen.findByText("Date")).toBeInTheDocument();
    expect(screen.getByText("Time")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Schedule" })).toBeInTheDocument();
  });

  it("shows the pending schedule and a cancel affordance instead of the create form", async () => {
    vi.mocked(api.getIssueSchedule).mockResolvedValue({
      id: "sched-1",
      issue_id: "issue-1",
      run_at: "2026-09-01T12:00:00Z",
      status: "pending",
      missed_policy: "notify",
      created_by_user_id: "user-1",
      fired_at: null,
      created_at: "2026-08-20T00:00:00Z",
    });
    renderDialog(makeIssue());

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Cancel schedule" })).toBeInTheDocument();
    });
    // The create form must not render alongside the pending state — there is
    // nothing to schedule while one is already pending (the backend would
    // reject it with 409 anyway; the dialog should not even offer it).
    expect(screen.queryByRole("button", { name: "Schedule" })).not.toBeInTheDocument();
  });
});
