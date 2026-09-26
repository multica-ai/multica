// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import type { WorkflowHandoffPreview } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { WorkflowHandoffConfirmDialog } from "./handoff-confirm-dialog";

const previewWorkflowHandoff = vi.fn<(issueId: string, status: string) => Promise<WorkflowHandoffPreview>>();

vi.mock("@multica/core/api", () => ({
  api: { previewWorkflowHandoff: (issueId: string, status: string) => previewWorkflowHandoff(issueId, status) },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () =>
    buildIssueStatusCatalog([
      { id: "a", workspace_id: "ws-1", key: "implement", name: "Implement", description: "", category: "started", color: "#3b82f6", is_system: false, position: 1, archived_at: null, created_at: "", updated_at: "" },
      { id: "b", workspace_id: "ws-1", key: "code_review", name: "Code review", description: "", category: "started", color: "#a855f7", is_system: false, position: 2, archived_at: null, created_at: "", updated_at: "" },
    ]),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => ({ forge: "Forge", sentinel: "Sentinel" })[id] ?? "?" }),
}));
vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid={`avatar-${actorId}`} />,
}));

const handoff: WorkflowHandoffPreview = {
  handoff: true,
  workflow_name: "Delivery",
  from_status: "implement",
  to_status: "code_review",
  handler_type: "agent",
  handler_id: "sentinel",
  previous_assignee_type: "agent",
  previous_assignee_id: "forge",
  previous_runs: [{ task_id: "t1", agent_id: "forge", status: "running", started_at: new Date(Date.now() - 6 * 60_000).toISOString() }],
  brief: "This project's workflow — Delivery\nReview the PR.",
};

function renderDialog(onConfirm = vi.fn(), onCancel = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkflowHandoffConfirmDialog
        issue={{ id: "issue-1", identifier: "MUL-8121" }}
        toStatus="code_review"
        onCancel={onCancel}
        onConfirm={onConfirm}
      />
    </QueryClientProvider>,
  );
  return { onConfirm, onCancel };
}

afterEach(() => {
  cleanup();
  previewWorkflowHandoff.mockReset();
});

describe("WorkflowHandoffConfirmDialog (MUL-7420)", () => {
  it("names the handoff, warns about the running agent and stops it by default", async () => {
    previewWorkflowHandoff.mockResolvedValue(handoff);
    const { onConfirm } = renderDialog();

    expect(await screen.findByText(/Move MUL-8121 to “Code review”\?/)).toBeTruthy();
    expect(await screen.findByText(/who starts working right away/)).toBeTruthy();
    expect(screen.getByTestId("avatar-forge")).toBeTruthy();
    expect(screen.getByTestId("avatar-sentinel")).toBeTruthy();
    expect(screen.getByText(/Forge is still running \(for 6 minutes\)/)).toBeTruthy();
    expect(screen.getByText(/Review the PR\./)).toBeTruthy();
    expect(previewWorkflowHandoff).toHaveBeenCalledWith("issue-1", "code_review");

    fireEvent.click(screen.getByRole("button", { name: "Move to Code review" }));
    expect(onConfirm).toHaveBeenCalledWith({ stopPreviousRuns: true });
  });

  it("keeps the previous run when the stop box is cleared", async () => {
    previewWorkflowHandoff.mockResolvedValue(handoff);
    const { onConfirm } = renderDialog();
    fireEvent.click(await screen.findByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "Move to Code review" }));
    expect(onConfirm).toHaveBeenCalledWith({ stopPreviousRuns: false });
  });

  it("words a run that has not started as queued and cancels it", async () => {
    previewWorkflowHandoff.mockResolvedValue({
      ...handoff,
      previous_runs: [{ task_id: "t1", agent_id: "forge", status: "queued", started_at: new Date().toISOString() }],
    });
    const { onConfirm } = renderDialog();

    expect(await screen.findByText(/Forge’s run is still queued/)).toBeTruthy();
    expect(screen.queryByText(/still running/)).toBeNull();
    expect(screen.getByText(/Cancel Forge’s queued run/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Move to Code review" }));
    expect(onConfirm).toHaveBeenCalledWith({ stopPreviousRuns: true });
  });

  it("applies without asking when the server finds no handoff", async () => {
    previewWorkflowHandoff.mockResolvedValue({ ...handoff, handoff: false, handler_type: null, handler_id: null });
    const { onConfirm } = renderDialog();
    await waitFor(() => expect(onConfirm).toHaveBeenCalledWith({ stopPreviousRuns: false }));
    expect(screen.queryByText(/Move MUL-8121/)).toBeNull();
  });
});
