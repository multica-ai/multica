import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentTask } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { ExecutionLogSection } from "./execution-log-section";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

const mockApi = vi.hoisted(() => ({
  listTasksByIssue: vi.fn(),
  cancelTask: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApi,
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getMemberName: (id: string) => (id === "user-1" ? "Ada Lovelace" : "Unknown"),
    getAgentName: (id: string) => (id === "agent-1" ? "Claude Agent" : "Unknown Agent"),
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid="actor-avatar">{actorId}</span>,
}));

vi.mock("../../common/task-transcript", () => ({
  TranscriptButton: () => <button data-testid="transcript-button">transcript</button>,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn() },
}));

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    dispatched_at: "2026-01-01T00:00:00Z",
    started_at: "2026-01-01T00:00:00Z",
    completed_at: "2026-01-01T00:01:00Z",
    result: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

function renderSection() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={createQueryClient()}>
        <ExecutionLogSection issueId="issue-1" />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("ExecutionLogSection local CLI rows", () => {
  it("renders CLI name, owner, cwd, transcript, and exit code", async () => {
    mockApi.listTasksByIssue.mockResolvedValue([
      makeTask({
        id: "run-1",
        agent_id: "",
        runtime_id: "",
        kind: "local_cli",
        owner_id: "user-1",
        cli_name: "codex",
        work_dir: "/Users/ada/project",
        context_dir: "/Users/ada/project/.multica/runs/run-1",
        exit_code: 7,
        trigger_summary: "Local codex",
      }),
    ]);

    renderSection();

    await waitFor(() => expect(screen.getByText("Execution log")).toBeInTheDocument());
    fireEvent.click(screen.getByText("Show past runs (1)"));

    expect(screen.getByText("codex · Ada Lovelace · project")).toBeInTheDocument();
    expect(screen.getByText("exit 7")).toBeInTheDocument();
    expect(screen.getByTestId("transcript-button")).toBeInTheDocument();
  });

  it("hides cancel for a running local CLI row but keeps transcript visible", async () => {
    mockApi.listTasksByIssue.mockResolvedValue([
      makeTask({
        id: "run-1",
        agent_id: "",
        runtime_id: "",
        kind: "local_cli",
        status: "running",
        owner_id: "user-1",
        cli_name: "codex",
        work_dir: "/Users/ada/project",
        completed_at: null,
      }),
    ]);

    renderSection();

    await waitFor(() => expect(screen.getByText("Execution log")).toBeInTheDocument());

    expect(screen.getByText("codex · Ada Lovelace · project")).toBeInTheDocument();
    expect(screen.getByTestId("transcript-button")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel task" })).not.toBeInTheDocument();
  });

  it("keeps cancel visible for a running agent task", async () => {
    mockApi.listTasksByIssue.mockResolvedValue([
      makeTask({
        id: "task-1",
        status: "running",
        trigger_summary: "Implement the feature",
        completed_at: null,
      }),
    ]);

    renderSection();

    await waitFor(() => expect(screen.getByText("Execution log")).toBeInTheDocument());

    expect(screen.getByRole("button", { name: "Cancel task" })).toBeInTheDocument();
  });
});
