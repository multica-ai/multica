import { describe, it, expect, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AutopilotRun } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// The run history row: a run waiting for a free agent slot reads Queued, and a
// run_only run shows the issue its agent declared it is working on. The status
// derivation matrix lives in run-display-status.test.ts.

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
  useCurrentWorkspace: () => ({ name: "Acme" }),
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
  AppLink: ({ href, children, className }: { href: string; children: React.ReactNode; className?: string }) => (
    <a href={href} className={className}>{children}</a>
  ),
}));
vi.mock("../../issues/components/issue-chip", () => ({
  IssueChip: ({ issueId }: { issueId: string }) => <span data-testid="issue-chip">{issueId}</span>,
}));
vi.mock("../../common/task-transcript", () => ({
  TranscriptButton: ({ isLive }: { isLive: boolean }) => (
    <button type="button" data-testid="transcript" data-live={String(isLive)} />
  ),
}));

import { RunRow } from "./autopilot-detail-page";

function run(overrides: Partial<AutopilotRun> = {}): AutopilotRun {
  return {
    id: "run-1",
    autopilot_id: "ap-1",
    trigger_id: null,
    source: "schedule",
    status: "running",
    issue_id: null,
    task_id: "task-1",
    triggered_at: "2026-09-30T02:00:00Z",
    completed_at: null,
    failure_reason: null,
    trigger_payload: null,
    result: null,
    created_at: "2026-09-30T02:00:00Z",
    task_status: null,
    work_issue_id: null,
    ...overrides,
  };
}

function renderRow(r: AutopilotRun) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RunRow run={r} agentId="agent-1" agentName="Lead" />
    </QueryClientProvider>,
  );
}

describe("RunRow", () => {
  it("shows a run whose task is still queued as Queued, not live", () => {
    renderRow(run({ task_status: "queued" }));
    expect(screen.getByText("Queued")).toBeTruthy();
    expect(screen.queryByText("Running")).toBeNull();
    expect(screen.getByTestId("transcript").getAttribute("data-live")).toBe("false");
  });

  it("shows a claimed run as Running with the issue it is working on", () => {
    renderRow(run({ task_status: "running", work_issue_id: "issue-9" }));
    expect(screen.getByText("Running")).toBeTruthy();
    expect(screen.getByText("Working on")).toBeTruthy();
    expect(screen.getByTestId("issue-chip").closest("a")?.getAttribute("href")).toBe("/acme/issues/issue-9");
    // The row keeps its execution log next to the issue link.
    expect(screen.getByTestId("transcript").getAttribute("data-live")).toBe("true");
  });

  it("keeps Running for an older server that sends no task status", () => {
    renderRow(run());
    expect(screen.getByText("Running")).toBeTruthy();
  });
});
