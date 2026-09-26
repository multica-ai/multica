// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentActivityBucket, AgentTask } from "@multica/core/types";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "../../../navigation";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// History fixtures have no issue links, so they need no issue-detail requests.
vi.mock("@multica/core/api", () => ({ api: {} }));

// Keep transcript internals out of history pagination tests while exposing
// the identity of each rendered task row.
vi.mock("../../../common/task-transcript", () => ({
  TranscriptButton: ({ task }: { task: AgentTask }) => <span data-testid={`task-${task.id}`} />,
}));

// Keep "Now" empty while varying activity outcomes and task-list loading.
const agentTasksRef = vi.hoisted(() => ({
  current: (_before?: string) => new Promise<unknown>(() => {}),
}));
const activityRef = vi.hoisted(() => ({ current: [] as AgentActivityBucket[] }));
vi.mock("@multica/core/agents", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/agents")>();
  return {
    ...actual,
    agentTaskSnapshotOptions: () => ({
      queryKey: ["snapshot"],
      queryFn: () => Promise.resolve([]),
    }),
    agentTasksOptions: () => ({
      queryKey: ["agent-tasks"],
      initialPageParam: undefined,
      getNextPageParam: (page: { nextCursor: string | null }) => page.nextCursor ?? undefined,
      queryFn: ({ pageParam }: { pageParam?: string }) => agentTasksRef.current(pageParam),
    }),
    useWorkspaceActivityMap: () => ({
      byAgent: new Map([[
        "agent-1",
        actual.deriveAgentActivity(activityRef.current, "2026-01-01", Date.now()),
      ]]),
    }),
  };
});

import { ActivityTab, AgentPerformanceSummary } from "./activity-tab";

const baseAgent = {
  id: "agent-1",
  name: "Agent",
} as unknown as Agent;

const EMPTY_RECENT = "This agent hasn't completed anything yet.";

function renderTab(performance = false) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/agents/agent-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          <WorkspaceSlugProvider slug="acme">
            {performance && <AgentPerformanceSummary agent={baseAgent} />}
            <ActivityTab agent={baseAgent} showPerformance={performance} />
          </WorkspaceSlugProvider>
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  agentTasksRef.current = () => new Promise<unknown>(() => {});
  activityRef.current = [];
});

describe("agent outcome presentation", () => {
  it("uses completed and failed outcomes in both summaries and shows cancellations separately", () => {
    activityRef.current = [{
      agent_id: "agent-1",
      bucket_at: new Date().toISOString(),
      task_count: 10,
      failed_count: 1,
      completed_count: 1,
      cancelled_count: 8,
    }];
    renderTab(true);
    expect(screen.getByText("50%")).toBeInTheDocument();
    expect(screen.getByText("50% success")).toBeInTheDocument();
    expect(screen.getAllByText("8 cancelled")).toHaveLength(2);
    expect(screen.queryByText("90%")).not.toBeInTheDocument();
  });

  it("does not claim success for cancelled-only data", () => {
    activityRef.current = [{
      agent_id: "agent-1",
      bucket_at: new Date().toISOString(),
      task_count: 8,
      failed_count: 0,
      completed_count: 0,
      cancelled_count: 8,
    }];
    const { container } = renderTab(true);
    expect(screen.queryByText("100%")).not.toBeInTheDocument();
    expect(screen.queryByText("100% success")).not.toBeInTheDocument();
    expect(container.querySelectorAll('rect[fill="var(--color-brand)"]')).toHaveLength(0);
    expect(screen.getByText("success rate").parentElement).toHaveTextContent("—");
  });
});

describe("ActivityTab Recent work loading state", () => {
  it("shows a skeleton, not the empty state, while the task list is loading", () => {
    // Never-resolving queryFn keeps the per-agent task query pending, which is
    // exactly the first-paint window the skeleton is meant to cover.
    const { container } = renderTab();
    expect(
      container.querySelectorAll('[data-slot="skeleton"]').length,
    ).toBeGreaterThan(0);
    expect(screen.queryByText(EMPTY_RECENT)).not.toBeInTheDocument();
  });

  it("shows the empty state once the task list resolves to no runs", async () => {
    agentTasksRef.current = () => Promise.resolve({ tasks: [], nextCursor: null });
    renderTab();
    expect(await screen.findByText(EMPTY_RECENT)).toBeInTheDocument();
    expect(
      document.querySelectorAll('[data-slot="skeleton"]').length,
    ).toBe(0);
  });
});


describe("ActivityTab server pagination", () => {
  it("requests older history only on demand, disables duplicate fetches, and retries failures", async () => {
    let finish: (value: unknown) => void = () => {};
    const query = vi.fn()
      .mockResolvedValueOnce({ tasks: [], nextCursor: "older" })
      .mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }))
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce({ tasks: [], nextCursor: null });
    agentTasksRef.current = query;
    renderTab();
    const more = await screen.findByRole("button", { name: /Show more/ });
    expect(query).toHaveBeenCalledTimes(1);
    fireEvent.click(more);
    await waitFor(() => expect(more).toBeDisabled());
    fireEvent.click(more);
    expect(query).toHaveBeenCalledTimes(2);
    expect(query.mock.calls[1]?.[0]).toBe("older");
    finish({ tasks: [], nextCursor: "oldest" });
    await waitFor(() => expect(more).toBeEnabled());
    fireEvent.click(more);
    const retry = await screen.findByRole("button", { name: "Try again" });
    fireEvent.click(retry);
    await waitFor(() => expect(screen.queryByRole("button", { name: /Show more/ })).not.toBeInTheDocument());
    expect(query.mock.calls[3]?.[0]).toBe("oldest");
  });

  it("renders aggregate duration without fetching task history for the performance summary", () => {
    const query = vi.fn();
    agentTasksRef.current = query;
    activityRef.current = [{ agent_id: "agent-1", bucket_at: new Date().toISOString(), task_count: 201,
      completed_count: 201, failed_count: 0, cancelled_count: 0, duration_ms: 24120000, duration_count: 201 }];
    render(<I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={new QueryClient()}><AgentPerformanceSummary agent={baseAgent} /></QueryClientProvider>
    </I18nProvider>);
    expect(screen.getByText("2m 00s")).toBeInTheDocument();
    expect(query).not.toHaveBeenCalled();
  });
});


function historyTask(index: number): AgentTask {
  return {
    id: String(index), agent_id: "agent-1", runtime_id: "runtime-1", issue_id: "",
    status: "completed", priority: 0, dispatched_at: null, started_at: null,
    completed_at: new Date(Date.now() - index * 60000).toISOString(),
    created_at: new Date(Date.now() - index * 60000).toISOString(),
    result: null, error: null,
  };
}

describe("ActivityTab loaded history rows", () => {
  it("reveals cached rows before requesting older history and deduplicates overlapping pages", async () => {
    const tasks = Array.from({ length: 31 }, (_, i) => historyTask(i));
    const query = vi.fn()
      .mockResolvedValueOnce({ tasks, nextCursor: "older" })
      .mockResolvedValueOnce({ tasks: [tasks[30], historyTask(31)], nextCursor: null });
    agentTasksRef.current = query;
    renderTab();
    await screen.findByTestId("task-0");
    expect(screen.getAllByTestId(/^task-/)).toHaveLength(10);
    expect(screen.queryByTestId("task-10")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Show more/ }));
    expect(screen.getAllByTestId(/^task-/)).toHaveLength(30);
    expect(query).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: /Show more/ }));
    expect(screen.getAllByTestId(/^task-/)).toHaveLength(31);
    expect(query).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: /Show more/ }));
    await screen.findByTestId("task-31");
    expect(query).toHaveBeenCalledTimes(2);
    expect(query.mock.calls[1]?.[0]).toBe("older");
    expect(screen.getAllByTestId(/^task-/)).toHaveLength(32);
    expect(screen.getAllByTestId("task-30")).toHaveLength(1);
    expect(screen.queryByRole("button", { name: /Show more/ })).not.toBeInTheDocument();
  });

  it("keeps loaded rows visible when an older page fails and retries the same cursor", async () => {
    const query = vi.fn()
      .mockResolvedValueOnce({ tasks: [historyTask(0)], nextCursor: "older" })
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce({ tasks: [historyTask(1)], nextCursor: null });
    agentTasksRef.current = query;
    renderTab();
    await screen.findByTestId("task-0");
    fireEvent.click(screen.getByRole("button", { name: /Show more/ }));
    const retry = await screen.findByRole("button", { name: "Try again" });
    expect(screen.getByTestId("task-0")).toBeInTheDocument();
    expect(screen.queryByText(EMPTY_RECENT)).not.toBeInTheDocument();
    fireEvent.click(retry);
    await screen.findByTestId("task-1");
    expect(screen.getAllByTestId(/^task-/)).toHaveLength(2);
    expect(query.mock.calls.slice(1).map(([before]) => before)).toEqual(["older", "older"]);
  });
});

// A loaded page is not the total history. Keep the denominator unknown until
// the server reports that the last page has been reached.
describe("ActivityTab history completeness", () => {
  it.each([
    { nextCursor: "older", subtitle: "10 latest runs" },
    { nextCursor: null, subtitle: "10 of 200 runs" },
  ])("shows '$subtitle' for a 200-row page with cursor $nextCursor", async ({ nextCursor, subtitle }) => {
    agentTasksRef.current = () => Promise.resolve({
      tasks: Array.from({ length: 200 }, (_, i) => historyTask(i)),
      nextCursor,
    });
    renderTab();
    expect(await screen.findByText(subtitle)).toBeInTheDocument();
    if (nextCursor) {
      expect(screen.queryByText("10 of 200 runs")).not.toBeInTheDocument();
    }
    expect(screen.getAllByTestId(/^task-/)).toHaveLength(10);
  });

  it.each([
    { nextCursor: "oldest", subtitle: "50 latest runs" },
    { nextCursor: null, subtitle: "50 of 220 runs" },
  ])("shows '$subtitle' after loading more history with cursor $nextCursor", async ({ nextCursor, subtitle }) => {
    const query = vi.fn()
      .mockResolvedValueOnce({
        tasks: Array.from({ length: 20 }, (_, i) => historyTask(i)),
        nextCursor: "older",
      })
      .mockResolvedValueOnce({
        tasks: Array.from({ length: 200 }, (_, i) => historyTask(i + 20)),
        nextCursor,
      });
    agentTasksRef.current = query;
    renderTab();
    expect(await screen.findByText("10 latest runs")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Show more/ }));
    expect(screen.getByText("20 latest runs")).toBeInTheDocument();
    expect(query).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: /Show more/ }));
    expect(await screen.findByText(subtitle)).toBeInTheDocument();
    if (nextCursor) {
      expect(screen.queryByText(/of 220 runs/)).not.toBeInTheDocument();
    }
    expect(screen.getAllByTestId(/^task-/)).toHaveLength(50);
    expect(query.mock.calls[1]?.[0]).toBe("older");
  });
});
