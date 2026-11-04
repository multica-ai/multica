import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import type { Agent, AgentTask, ChatSession, Issue } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import enCommon from "../locales/en/common.json";
import enAgents from "../locales/en/agents.json";
import enChat from "../locales/en/chat.json";
import { AgentTaskPanel } from "./agent-task-panel";

const api = vi.hoisted(() => ({
  listIssues: vi.fn(),
  listAgentTasksPage: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({ api }));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({
    categoryOf: () => "unstarted",
    entryOf: () => undefined,
  }),
}));
vi.mock("../issues/utils/status-label", () => ({
  useStatusLabel: () => (status: string) => status,
}));

const agent = { id: "agent-1", name: "Codex" } as Agent;
const issue = {
  id: "issue-1", identifier: "DEV-1", title: "Inspect the build", status: "backlog",
} as Issue;
const session = { id: "session-1", title: "Review deployment" } as ChatSession;
const navigation: NavigationAdapter = {
  push: vi.fn(), replace: vi.fn(), back: vi.fn(),
  pathname: "/dev/chat", searchParams: new URLSearchParams(), hash: "",
  getShareableUrl: (path) => path,
};

function run(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "run-1", agent_id: agent.id, runtime_id: "runtime-1", issue_id: "",
    status: "completed", priority: 0,
    dispatched_at: "2026-10-23T10:00:00Z", started_at: "2026-10-23T10:00:01Z",
    completed_at: "2026-10-23T10:00:17Z", created_at: "2026-10-23T10:00:00Z",
    result: null, error: null, relative_work_dir: "old/workdir",
    ...overrides,
  };
}

function renderPanel(props: Partial<React.ComponentProps<typeof AgentTaskPanel>> = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, agents: enAgents, chat: enChat } }}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          <WorkspaceSlugProvider slug="dev">
            <AgentTaskPanel wsId="ws-1" agent={agent} canInvoke canReference
              onReference={vi.fn()} onRevealConversation={vi.fn()} {...props} />
          </WorkspaceSlugProvider>
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
  return { ...view, queryClient };
}

beforeEach(() => {
  cleanup();
  vi.resetAllMocks();
  api.listIssues.mockResolvedValue({ issues: [issue], total: 1 });
  api.listAgentTasksPage.mockResolvedValue({ tasks: [], nextCursor: null });
});

describe("Agent workspace task history", () => {
  it("keeps loaded tasks visible when an older page fails and retries that page", async () => {
    api.listIssues
      .mockResolvedValueOnce({ issues: [issue], total: 2 })
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce({ issues: [{ ...issue, id: "issue-2", title: "Review release" }], total: 2 });
    renderPanel();
    expect(await screen.findByRole("link", { name: /Inspect the build/ })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Load more" }));
    expect(await screen.findByRole("alert")).toBeVisible();
    expect(screen.getByRole("link", { name: /Inspect the build/ })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("link", { name: /Review release/ })).toBeVisible();
    expect(api.listIssues.mock.calls.map(([params]) => params.offset)).toEqual([0, 1, 1]);
  });

  it("retains cached rows on refresh failure and refetches instead of loading another page", async () => {
    api.listIssues
      .mockResolvedValueOnce({ issues: [issue], total: 1 })
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce({ issues: [{ ...issue, title: "Refreshed build" }], total: 1 });
    const { queryClient } = renderPanel();
    await screen.findByRole("link", { name: /Inspect the build/ });
    await act(async () => { await queryClient.refetchQueries(); });
    expect(await screen.findByRole("alert")).toBeVisible();
    expect(screen.getByRole("link", { name: /Inspect the build/ })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("link", { name: /Refreshed build/ })).toBeVisible();
    expect(api.listIssues.mock.calls.map(([params]) => params.offset)).toEqual([0, 0, 0]);
  });

  it("shows pending pagination and presents overlapping pages as one task row", async () => {
    let resolvePage: (value: unknown) => void = () => {};
    api.listIssues
      .mockResolvedValueOnce({ issues: [issue], total: 2 })
      .mockImplementationOnce(() => new Promise((resolve) => { resolvePage = resolve; }));
    renderPanel();
    const more = await screen.findByRole("button", { name: "Load more" });
    fireEvent.click(more);
    await waitFor(() => {
      expect(more).toBeDisabled();
      expect(more).toHaveAttribute("aria-busy", "true");
    });
    fireEvent.click(more);
    expect(api.listIssues).toHaveBeenCalledTimes(2);
    await act(async () => {
      resolvePage({ issues: [issue, { ...issue, id: "issue-2", title: "Review release" }], total: 2 });
    });
    expect(await screen.findByRole("link", { name: /Review release/ })).toBeVisible();
    expect(screen.getAllByRole("link", { name: /Inspect the build/ })).toHaveLength(1);
  });

  it("identifies run sources, time, duration and outcome without exposing technical paths", async () => {
    const completedAt = new Date(Date.now() - 120_000).toISOString();
    const startedAt = new Date(Date.parse(completedAt) - 16_000).toISOString();
    api.listAgentTasksPage.mockResolvedValue({
      tasks: [
        run({ chat_session_id: session.id, completed_at: completedAt, started_at: startedAt }),
        run({ id: "run-2", issue_id: issue.id, status: "failed", failure_reason: "agent_error.provider_quota_limit" }),
        run({ id: "run-3", status: "cancelled", cancelled_by: { type: "member", name: "Lee" }, trigger_summary: "Check the release" }),
      ],
      nextCursor: null,
    });
    renderPanel({ sessions: [session] });
    await screen.findByRole("link", { name: /Inspect the build/ });
    fireEvent.click(screen.getByRole("button", { name: "Runs" }));
    expect(await screen.findByText(session.title)).toBeVisible();
    expect(screen.getByText("Inspect the build")).toBeVisible();
    expect(screen.getByText("DEV-1")).toBeVisible();
    expect(screen.getByText("2m ago")).toBeVisible();
    expect(screen.getAllByText("16s").length).toBeGreaterThan(0);
    expect(screen.getByText(enAgents.task_failure.reasons.agent_error_provider_quota_limit)).toBeVisible();
    expect(screen.getByText("Cancelled by Lee")).toBeVisible();
    expect(screen.getByRole("link", { name: "Open conversation" })).toHaveAttribute("href", "/dev/chat?session=session-1");
    expect(screen.getByRole("link", { name: "Open task" })).toHaveAttribute("href", "/dev/issues/issue-1");
    expect(screen.queryByText("run-1")).not.toBeInTheDocument();
    expect(screen.queryByText("old/workdir")).not.toBeInTheDocument();
  });

  it("explains disabled references on keyboard focus without invoking the action", async () => {
    const onReference = vi.fn();
    const reason = "This conversation is read-only";
    renderPanel({ canReference: false, referenceDisabledReason: reason, onReference });
    const reference = await screen.findByRole("button", { name: "Add DEV-1 to the conversation draft" });
    expect(reference).toBeDisabled();
    const trigger = reference.parentElement!;
    expect(trigger).toHaveAttribute("tabindex", "0");
    act(() => trigger.focus());
    expect(await screen.findByRole("tooltip")).toHaveTextContent(reason);
    fireEvent.click(reference);
    expect(onReference).not.toHaveBeenCalled();
  });
});
