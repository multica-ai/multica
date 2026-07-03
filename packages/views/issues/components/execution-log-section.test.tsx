import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { ExecutionLogSection } from "./execution-log-section";
import type { AgentTask } from "@multica/core/types/agent";
import enIssues from "../../locales/en/issues.json";

const mockListTasksByIssue = vi.hoisted(() => vi.fn());
const mockListTaskMessages = vi.hoisted(() => vi.fn());
const mockRerunIssue = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    listTasksByIssue: mockListTasksByIssue,
    listTaskMessages: mockListTaskMessages,
    cancelTask: vi.fn(),
    rerunIssue: mockRerunIssue,
  },
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div data-testid="actor-avatar" />,
}));

const mockToast = vi.hoisted(() => ({ error: vi.fn() }));
vi.mock("sonner", () => ({
  toast: mockToast,
}));

const TEST_RESOURCES = { en: { issues: enIssues } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}

const task = {
  id: "task-1",
  issue_id: "issue-1",
  agent_id: "agent-1",
  status: "running",
  created_at: new Date().toISOString(),
  started_at: new Date().toISOString(),
} as AgentTask;

describe("ExecutionLogSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders an inline transcript toggle for an active task", async () => {
    mockListTasksByIssue.mockResolvedValue([task]);
    mockListTaskMessages.mockResolvedValue([]);
    render(<ExecutionLogSection issueId="issue-1" />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByText("Live")).toBeInTheDocument());
  });

  it("shows the live inline panel by default for active tasks", async () => {
    mockListTasksByIssue.mockResolvedValue([task]);
    mockListTaskMessages.mockResolvedValue([]);
    render(<ExecutionLogSection issueId="issue-1" />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByText("Live")).toBeInTheDocument());
    await waitFor(() => expect(screen.getByText("Waiting for events...")).toBeInTheDocument());
  });

  it("shows an explicit retry action for a failed task", async () => {
    const failedTask = {
      ...task,
      id: "task-failed",
      status: "failed",
      completed_at: new Date().toISOString(),
    } as AgentTask;
    mockListTasksByIssue.mockResolvedValue([failedTask]);
    mockListTaskMessages.mockResolvedValue([]);

    render(<ExecutionLogSection issueId="issue-1" />, { wrapper: Wrapper });

    const retryButton = await screen.findByRole("button", { name: "Retry task" });
    expect(retryButton).toHaveTextContent("Retry");
    expect(retryButton).toHaveClass("cursor-pointer", "hover:-translate-y-px");
    fireEvent.click(retryButton);

    await waitFor(() =>
      expect(mockRerunIssue).toHaveBeenCalledWith("issue-1", "task-failed"),
    );
  });
});
