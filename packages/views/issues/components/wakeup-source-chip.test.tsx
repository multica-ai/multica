// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueRunLookupProvider } from "./issue-run-lookup-context";
import { WakeupSourceChip } from "./wakeup-source-chip";

vi.mock("@multica/core/api", () => ({
  api: { listTasksByIssue: vi.fn(), listIssueWakeups: vi.fn() },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("../../common/use-viewing-timezone", () => ({ useViewingTimezone: () => "UTC" }));
vi.mock("./wakeup-condition-names", () => ({
  useConditionNames: () => ({ status: (key: string) => key, label: () => undefined, property: () => undefined, actor: (_type: string, id: string) => id }),
}));

const task: AgentTask = {
  id: "task-1", agent_id: "agent", runtime_id: "runtime", issue_id: "issue", status: "completed", priority: 0,
  created_at: "2026-09-07T00:00:00Z", started_at: "2026-09-07T00:00:00Z", dispatched_at: null,
  completed_at: "2026-09-07T00:01:00Z", result: null, error: null, wakeup_id: "wakeup-1",
};

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("WakeupSourceChip", () => {
  it("reads the wakeup id from the shared lookup without fetching per-chip task queries", async () => {
    vi.mocked(api.listIssueWakeups).mockResolvedValue([]);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={client}>
        <IssueRunLookupProvider timeline={[]} tasks={[task]}>
          <WakeupSourceChip issueId="issue" taskId="task-1" />
        </IssueRunLookupProvider>
      </QueryClientProvider>,
    );

    expect(await screen.findByText("Triggered by wakeup")).toBeInTheDocument();
    expect(api.listTasksByIssue).not.toHaveBeenCalled();
    expect(client.getQueryCache().find({ queryKey: issueKeys.tasks("issue") })).toBeUndefined();
    expect(api.listIssueWakeups).toHaveBeenCalledWith("issue");
  });

  it("renders nothing when the shared lookup has no wakeup for the task", () => {
    const client = new QueryClient();
    const { container } = renderWithI18n(
      <QueryClientProvider client={client}>
        <IssueRunLookupProvider timeline={[]} tasks={[{ ...task, wakeup_id: undefined }]}>
          <WakeupSourceChip issueId="issue" taskId="task-1" />
        </IssueRunLookupProvider>
      </QueryClientProvider>,
    );

    expect(container).toBeEmptyDOMElement();
    expect(api.listTasksByIssue).not.toHaveBeenCalled();
    expect(api.listIssueWakeups).not.toHaveBeenCalled();
  });
});
