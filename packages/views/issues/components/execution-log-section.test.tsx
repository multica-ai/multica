import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { ExecutionLogSection } from "./execution-log-section";
import type { AgentTask } from "@multica/core/types/agent";
import enIssues from "../../locales/en/issues.json";

const mockListTasksByIssue = vi.hoisted(() => vi.fn());
const mockListTaskMessages = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    listTasksByIssue: mockListTasksByIssue,
    listTaskMessages: mockListTaskMessages,
    cancelTask: vi.fn(),
    rerunIssue: vi.fn(),
  },
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div data-testid="actor-avatar" />,
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
});
