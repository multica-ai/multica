import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { issueWorkflowKeys } from "@multica/core/issue-workflows";
import type { AutomationExecution, Issue, IssueWorkflowResponse } from "@multica/core/types";
import enIssues from "../../locales/en/issues.json";
import { AutomationExecutionSection } from "./automation-execution-section";
import { WorkflowTransitionDialog } from "./workflow-transition-dialog";

const mocks = vi.hoisted(() => ({ transition: vi.fn(), takeover: vi.fn() }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: (_type: string, id: string) => id === "reviewer" ? "Review Agent" : "Alice" }) }));
vi.mock("@multica/core/api", () => ({ api: { transitionIssueStatusNode: mocks.transition, takeOverAutomationExecution: mocks.takeover } }));
vi.mock("./execution-log-section", () => ({ ExecutionLogSection: () => <div>Execution transcript</div> }));
const issue = { id: "issue", identifier: "MUL-1", workflow_id: "flow", workflow_status_id: "dev", transition_id: "entry", revision: 7, workspace_id: "ws" } as Issue;
const policy = { assignee: { type: "keep" }, executor: { type: "none" }, instructions: "", advance: "human_confirms", next_status_key: "review" } as const;
const workflow = { workflow: { id: "flow", revision: 3 }, statuses: [
  { id: "dev", workflow_id: "flow", spec_key: "dev", name: "Development", phase: "started", entry_policy: policy, archived_at: null },
  { id: "review", workflow_id: "flow", spec_key: "review", name: "Code Review", phase: "started", entry_policy: { ...policy, next_status_key: "", executor: { type: "agent", id: "reviewer" }, instructions: "Review code" }, archived_at: null },
], mode: "custom" } as IssueWorkflowResponse;
function entry(status: string): AutomationExecution {
  return { id: "execution", issue_id: "issue", workflow_id: "flow", status_id: "dev", trigger_transition_id: "entry", executor_type: "agent", executor_id: "reviewer", status, policy_snapshot: { ...policy, executor: { type: "agent", id: "reviewer" }, instructions: "Implement change" }, created_at: "2026-09-07T01:00:00Z" } as AutomationExecution;
}
function mount(executions: AutomationExecution[] = [], dialog = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } } });
  client.setQueryData(issueWorkflowKeys.detail("ws", "flow"), workflow);
  client.setQueryData(issueWorkflowKeys.executions("ws", "issue"), executions);
  render(<I18nProvider locale="en" resources={{ en: { issues: enIssues } }}><QueryClientProvider client={client}>
    {dialog ? <WorkflowTransitionDialog issue={issue} target={workflow.statuses[1]!} workflowRevision={3} onClose={vi.fn()} /> : <AutomationExecutionSection issue={issue} />}
  </QueryClientProvider></I18nProvider>);
  return client;
}
beforeEach(() => { vi.clearAllMocks(); mocks.transition.mockImplementation(() => new Promise(() => {})); mocks.takeover.mockImplementation(() => new Promise(() => {})); });

describe("workflow action wiring", () => {
  // State/identity cases are owned by core/issue-workflows/handoff.test.ts.
  it("updates the action after completion, sends both revisions, and disables the pending action", async () => {
    const client = mount();
    expect(screen.getByRole("button", { name: "Enter Code Review" })).toBeInTheDocument();
    expect(screen.getByText("Starts Review Agent")).toBeInTheDocument();
    act(() => client.setQueryData(issueWorkflowKeys.executions("ws", "issue"), [entry("completed")]));
    fireEvent.click(await screen.findByRole("button", { name: "Confirm and enter Code Review" }));
    await waitFor(() => expect(mocks.transition).toHaveBeenCalledWith("issue", { workflow_status_id: "review", expected_revision: 7, expected_transition_id: "entry", expected_workflow_revision: 3 }));
    expect(screen.getByRole("button", { name: "Updating..." })).toBeDisabled();
  });
  it("offers the real execution view and takeover without changing status", async () => {
    mount([entry("running")]);
    fireEvent.click(screen.getByRole("button", { name: "View executions" }));
    expect(screen.getByText("Execution transcript")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Take over" }));
    await waitFor(() => expect(mocks.takeover).toHaveBeenCalledWith("issue", "execution", 7));
    expect(mocks.transition).not.toHaveBeenCalled();
  });
  it("shows a recoverable error when a preview is stale", async () => {
    mocks.transition.mockRejectedValue(new Error("conflict"));
    mount();
    fireEvent.click(screen.getByRole("button", { name: "Enter Code Review" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Unable to update");
    expect(screen.getByRole("button", { name: "Refresh" })).toBeInTheDocument();
  });
  it("previews stopping the current execution before a status picker transition", () => {
    mount([entry("running")], true);
    expect(screen.getByRole("dialog")).toHaveTextContent("Stops the current execution");
    expect(mocks.transition).not.toHaveBeenCalled();
  });
});
