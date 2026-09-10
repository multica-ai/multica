import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Issue } from "@multica/core/types";
import { issueWorkflowOptions } from "@multica/core/issue-workflows";
import en from "../../../locales/en/issues.json";
import { BatchWorkflowStatusPicker } from "./batch-workflow-status-picker";
const transition = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/issues/mutations", () => ({
  useTransitionIssueStatusNode: () => ({ mutateAsync: transition }),
}));
function setup(mixed: boolean) {
  const issues = [
    {
      id: "one",
      workflow_id: "flow",
      workflow_status_id: "first",
      revision: 2,
      transition_id: "cursor-one",
    },
    {
      id: "two",
      workflow_id: mixed ? "other" : "flow",
      workflow_status_id: "first",
      revision: 5,
      transition_id: "cursor-two",
    },
  ] as Issue[];
  const qc = new QueryClient();
  qc.setQueryData(issueWorkflowOptions("ws", "flow").queryKey, {
    workflow: {
      id: "flow",
      revision: 3,
      initial_status_id: "first",
      workspace_id: "ws",
      scope_id: "p",
      scope_type: "project",
      name: "Flow",
      created_at: "",
      updated_at: "",
    },
    mode: "custom",
    statuses: [
      {
        id: "done",
        workflow_id: "flow",
        name: "Published",
        legacy_status_key: "done",
        spec_key: "done",
        phase: "completed",
        outcome: "completed",
        color: "#123456",
        position: 0,
        description: "",
        archived_at: null,
        entry_policy_revision: 1,
        created_at: "",
        updated_at: "",
        entry_policy: {
          assignee: { type: "keep" },
          executor: { type: "none" },
          instructions: "",
          advance: "human_confirms",
        },
      },
    ],
  });
  render(
    <I18nProvider locale="en" resources={{ en: { issues: en } }}>
      <QueryClientProvider client={qc}>
        <BatchWorkflowStatusPicker issues={issues} disabled={false} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}
describe("batch workflow status", () => {
  it("does not offer a shared status for issues pinned to different workflows", async () => {
    transition.mockClear();
    const user = userEvent.setup();
    setup(true);
    await user.click(screen.getByRole("button", { name: "Status" }));
    expect(
      screen.getByText(
        "Select tasks from the same workflow to change their status together.",
      ),
    ).toBeInTheDocument();
    expect(transition).not.toHaveBeenCalled();
  });
  it("uses node identity with each issue's own cursor", async () => {
    transition.mockReset();
    transition.mockResolvedValue({});
    const user = userEvent.setup();
    setup(false);
    await user.click(screen.getByRole("button", { name: "Status" }));
    await user.click(screen.getByRole("button", { name: "Published" }));
    await waitFor(() => expect(transition).toHaveBeenCalledTimes(2));
    expect(transition).toHaveBeenCalledWith({
      id: "one",
      workflow_status_id: "done",
      expected_revision: 2,
      expected_transition_id: "cursor-one",
      expected_workflow_revision: 3,
    });
    expect(transition).toHaveBeenCalledWith({
      id: "two",
      workflow_status_id: "done",
      expected_revision: 5,
      expected_transition_id: "cursor-two",
      expected_workflow_revision: 3,
    });
  });
});
