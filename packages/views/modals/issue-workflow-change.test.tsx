import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Issue, IssueWorkflowResponse } from "@multica/core/types";
import { issueDetailOptions } from "@multica/core/issues/queries";
import {
  issueWorkflowOptions,
  issueAutomationExecutionsOptions,
  effectiveIssueWorkflowOptions,
} from "@multica/core/issue-workflows";
import en from "../locales/en/issues.json";
import { IssueWorkflowChangeModal } from "./issue-workflow-change";

const mocks = vi.hoisted(() => ({ update: vi.fn(), transition: vi.fn() }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Reviewer" }),
}));
vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutateAsync: mocks.update }),
  useTransitionIssueStatusNode: () => ({ mutateAsync: mocks.transition }),
}));
const issue = {
  id: "issue",
  workspace_id: "ws",
  project_id: "source",
  workflow_id: "pinned",
  workflow_status_id: "pinned-first",
  transition_id: "cursor",
  revision: 7,
} as Issue;
function flow(id: string): IssueWorkflowResponse {
  return {
    workflow: {
      id,
      workspace_id: "ws",
      scope_type: "project",
      scope_id: "source",
      name: id,
      revision: 3,
      initial_status_id: `${id}-first`,
      created_at: "",
      updated_at: "",
    },
    mode: "custom",
    statuses: ["First", "Done"].map((name, index) => ({
      id: `${id}-${name.toLowerCase()}`,
      workflow_id: id,
      legacy_status_key: index ? "done" : "todo",
      spec_key: name,
      name: `${id} ${name}`,
      description: "",
      color: "#123456",
      position: index,
      phase: index ? "completed" : "unstarted",
      outcome: index ? "completed" : null,
      archived_at: null,
      entry_policy: {
        assignee: { type: "keep" },
        executor: { type: "none" },
        instructions: "",
        advance: "human_confirms",
      },
      entry_policy_revision: 1,
      created_at: "",
      updated_at: "",
    })),
  };
}
function setup(updates: Record<string, unknown>) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  qc.setQueryData(issueDetailOptions("ws", "issue").queryKey, issue);
  qc.setQueryData(
    issueWorkflowOptions("ws", "pinned").queryKey,
    flow("pinned"),
  );
  qc.setQueryData(issueAutomationExecutionsOptions("ws", "issue").queryKey, []);
  qc.setQueryData(
    effectiveIssueWorkflowOptions("ws", "source").queryKey,
    flow("new-default"),
  );
  qc.setQueryData(
    effectiveIssueWorkflowOptions("ws", "target").queryKey,
    flow("target"),
  );
  const close = vi.fn();
  render(
    <I18nProvider locale="en" resources={{ en: { issues: en } }}>
      <QueryClientProvider client={qc}>
        <IssueWorkflowChangeModal
          data={{ issueId: "issue", updates }}
          onClose={close}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { close, qc };
}
describe("workflow change resolution", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.transition.mockResolvedValue({ issue: { ...issue, revision: 8 } });
    mocks.update.mockResolvedValue({ ...issue, revision: 8 });
  });
  it("uses the issue's pinned workflow and cursor even when the project's default has changed", async () => {
    const user = userEvent.setup();
    const { close, qc } = setup({ status: "done" });
    expect(
      screen.getByRole("button", { name: "pinned Done" }),
    ).toBeInTheDocument();
    qc.setQueryData(issueDetailOptions("ws", "issue").queryKey, {
      ...issue,
      revision: 9,
    });
    await user.click(screen.getByRole("button", { name: "Enter pinned Done" }));
    await waitFor(() =>
      expect(mocks.transition).toHaveBeenCalledWith({
        id: "issue",
        workflow_status_id: "pinned-done",
        expected_revision: 7,
        expected_transition_id: "cursor",
        expected_workflow_revision: 3,
      }),
    );
    expect(mocks.update).not.toHaveBeenCalled();
    expect(close).toHaveBeenCalled();
  });
  it("requires an explicit destination status and sends project and node together", async () => {
    const user = userEvent.setup();
    setup({ project_id: "target" });
    expect(screen.getByRole("button", { name: "Move task" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Choose a status" }));
    expect(
      screen.queryByRole("button", { name: "pinned First" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "target First" }));
    await user.click(screen.getByRole("button", { name: "Move task" }));
    await waitFor(() =>
      expect(mocks.update).toHaveBeenCalledWith({
        id: "issue",
        project_id: "target",
        workflow_status_id: "target-first",
        expected_revision: 7,
        expected_transition_id: "cursor",
      }),
    );
    expect(mocks.transition).not.toHaveBeenCalled();
  });
  it("keeps the dialog open on a stale transition instead of closing or silently retrying", async () => {
    mocks.transition.mockRejectedValue(new Error("issue transition conflict"));
    const user = userEvent.setup();
    const { close } = setup({ workflow_status_id: "pinned-done" });
    await user.click(screen.getByRole("button", { name: "Enter pinned Done" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "issue transition conflict",
    );
    expect(close).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "Enter pinned Done" }),
    ).toBeDisabled();
  });
});
