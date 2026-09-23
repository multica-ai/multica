import { beforeEach, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { api } from "@multica/core/api";
import type {
  WorkspaceWakeup,
  WorkspaceWakeupFilters,
} from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { WorkspaceWakeups } from "./workspace-wakeups";

vi.mock("../../common/use-viewing-timezone", () => ({
  useViewingTimezone: () => "UTC",
}));
vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaceWakeups: vi.fn(),
    disableIssueWakeup: vi.fn(),
    enableIssueWakeup: vi.fn(),
    listIssueWakeups: vi.fn(),
    editIssueWakeupInstruction: vi.fn(),
  },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/ws/issues/${id}`,
  }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({
    href,
    children,
    ...props
  }: {
    href: string;
    children: ReactNode;
  }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../common/task-transcript", () => ({
  TranscriptButton: () => <button>Transcript</button>,
}));

let rows: WorkspaceWakeup[];
let queries: WorkspaceWakeupFilters[];
const list = vi.mocked(api.listWorkspaceWakeups);
const disable = vi.mocked(api.disableIssueWakeup);
const enable = vi.mocked(api.enableIssueWakeup);
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <WorkspaceWakeups />
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  queries = [];
  rows = ["a", "b"].map((id) => ({
    id,
    issue_id: `issue-${id}`,
    issue_identifier: `DEV-${id}`,
    issue_title: `Issue ${id}`,
    issue_closed: false,
    agent_id: id,
    agent_name: `Agent ${id}`,
    can_manage: true,
    active_runs: 0,
    task: null,
    kind: "event",
    mode: "continuous",
    event_types: ["task.completed"],
    filter_agent_id: null,
    filter_task_id: null,
    interval_seconds: null,
    cron_expression: null,
    timezone: "UTC",
    next_fire_at: null,
    enabled: true,
    revision: 1,
    disabled_at: null,
    last_task_id: null,
    last_error: null,
  }));
  list.mockReset().mockImplementation(async (filters) => {
    queries.push(filters);
    return {
      items: rows,
      total: 60,
      counts: { all: 60, active: 58, disabled: 1, ended: 1 },
      agents: [
        { id: "a", name: "Agent a" },
        { id: "b", name: "Agent b" },
      ],
    };
  });
  disable.mockReset().mockResolvedValue(undefined);
  enable.mockReset().mockResolvedValue(undefined);
});

it("shows consumed running work separately from its off state and keeps transcript access", async () => {
  rows[0] = {
    ...rows[0]!,
    enabled: false,
    mode: "once",
    last_task_id: "run",
    active_runs: 1,
    task: {
      id: "run",
      agent_id: "a",
      issue_id: "issue-a",
      runtime_id: "runtime",
      status: "running",
      priority: 0,
      created_at: new Date().toISOString(),
      started_at: new Date().toISOString(),
      dispatched_at: null,
      completed_at: null,
      result: null,
      error: null,
    },
  };
  mount();
  const row = await screen.findByRole("row", { name: /Issue a/ });
  expect(within(row).getByText("Running")).toBeVisible();
  expect(within(row).getByText("Triggered")).toBeVisible();
  expect(within(row).queryByText("Completed")).toBeNull();
  expect(within(row).getByRole("button", { name: "Transcript" })).toBeVisible();
  expect(
    within(row).getByRole("button", { name: "Enable again" }),
  ).toBeDisabled();
  expect(within(row).getByRole("checkbox")).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  expect(within(row).getByRole("link", { name: /Issue a/ })).toHaveAttribute(
    "href",
    "/ws/issues/issue-a",
  );
});

it("confirms batch consequences and retains only failed selections for retry", async () => {
  disable.mockImplementation(async (_issue, id) => {
    if (id === "b") throw new Error("forbidden");
  });
  mount();
  await screen.findByText("Issue a");
  fireEvent.click(
    screen.getByRole("checkbox", {
      name: "Select enabled wakeups on this page",
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Turn off selected" }));
  expect(
    screen.getByText(/Dispatched runs will continue/),
  ).toBeVisible();
  expect(disable).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("button", { name: "Turn off" }),
  );
  await screen.findByText("Turned off 1; 1 failed. Failed items remain selected for retry.");
  expect(disable.mock.calls).toEqual([
    ["issue-a", "a"],
    ["issue-b", "b"],
  ]);
  expect(
    screen.getByRole("checkbox", { name: "Select DEV-a, Agent a" }),
  ).not.toBeChecked();
  expect(
    screen.getByRole("checkbox", { name: "Select DEV-b, Agent b" }),
  ).toBeChecked();
});

it("resets page and selection on scope or search changes and sends bounded page requests", async () => {
  mount();
  await screen.findByText("Issue a");
  fireEvent.click(screen.getByRole("button", { name: "Next" }));
  await waitFor(() => expect(queries.at(-1)?.offset).toBe(50));
  await screen.findByText("Issue a");
  fireEvent.click(
    screen.getByRole("checkbox", { name: "Select DEV-a, Agent a" }),
  );
  fireEvent.click(screen.getByRole("button", { name: /^All\s*60/ }));
  await waitFor(() =>
    expect(queries.at(-1)).toMatchObject({
      scope: "all",
      offset: 0,
      limit: 50,
    }),
  );
  expect(
    screen.queryByRole("button", { name: "Turn off selected" }),
  ).toBeNull();
  fireEvent.change(screen.getByRole("searchbox"), {
    target: { value: "CI & release" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Search" }));
  await waitFor(() => expect(queries.at(-1)?.search).toBe("CI & release"));
});

it("makes read-only rules non-selectable and surfaces inventory failures", async () => {
  rows[0]!.can_manage = false;
  const view = mount();
  await screen.findByText("Issue a");
  expect(
    screen.getByRole("checkbox", { name: "Select DEV-a, Agent a" }),
  ).toHaveAttribute("aria-disabled", "true");
  expect(
    screen.getByRole("switch", { name: "Wakeup for Agent a" }),
  ).toHaveAttribute("aria-disabled", "true");
  view.unmount();
  list.mockRejectedValue(new Error("offline"));
  mount();
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Could not load wakeups",
  );
  expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled();
});


it("opens the shared prompt editor from a manageable row without loading all prompts", async () => {
  const [first, second] = rows;
  if (!first || !second) throw new Error("Missing wakeup fixtures");
  vi.mocked(api.listIssueWakeups).mockResolvedValue([{ ...first, instruction: "Inspect the result" }]);
  vi.mocked(api.editIssueWakeupInstruction).mockResolvedValue(undefined);
  second.can_manage = false;
  mount();
  const buttons = await screen.findAllByRole("button", { name: "Edit prompt" });
  expect(buttons[1]).toBeDisabled();
  expect(api.listIssueWakeups).not.toHaveBeenCalled();
  if (!buttons[0]) throw new Error("Missing edit button");
  fireEvent.click(buttons[0]);
  const input = await screen.findByRole("textbox", { name: "What to do when woken" });
  fireEvent.change(input, { target: { value: "Inspect and summarize" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(api.editIssueWakeupInstruction).toHaveBeenCalledWith("issue-a", "a", { instruction: "Inspect and summarize", expected_instruction: "Inspect the result", revision: 1 }));
});
