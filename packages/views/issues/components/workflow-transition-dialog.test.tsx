import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { issueWorkflowKeys } from "@multica/core/issue-workflows";
import type { AutomationExecution, Issue, IssueWorkflowResponse } from "@multica/core/types";
import enIssues from "../../locales/en/issues.json";
import { WorkflowTransitionDialog } from "./workflow-transition-dialog";

const mocks = vi.hoisted(() => ({ transition: vi.fn() }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: (_type: string, id: string) => id === "reviewer" ? "Review Agent" : "Alice" }) }));
vi.mock("@multica/core/api", () => ({ api: { transitionIssueStatusNode: mocks.transition } }));
const issue = { id: "issue", identifier: "MUL-1", workflow_id: "flow", workflow_status_id: "dev", transition_id: "entry", revision: 7, workspace_id: "ws" } as Issue;
const policy = { executor: { type: "none" }, instructions: "" } as const;
const workflow = { workflow: { id: "flow", revision: 3 }, statuses: [
  { id: "dev", workflow_id: "flow", spec_key: "dev", name: "Development", phase: "started", entry_policy: policy, archived_at: null },
  { id: "review", workflow_id: "flow", spec_key: "review", name: "Code Review", phase: "started", entry_policy: { ...policy, executor: { type: "agent", id: "reviewer" }, instructions: "Review code" }, archived_at: null },
], mode: "custom" } as IssueWorkflowResponse;
function entry(status: string): AutomationExecution {
  return { id: "execution", issue_id: "issue", workflow_id: "flow", status_id: "dev", trigger_transition_id: "entry", executor_type: "agent", executor_id: "reviewer", status, policy_snapshot: { ...policy, executor: { type: "agent", id: "reviewer" }, instructions: "Implement change" }, created_at: "2026-09-07T01:00:00Z" } as AutomationExecution;
}
function mount(executions: AutomationExecution[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } } });
  client.setQueryData(issueWorkflowKeys.executions("ws", "issue"), executions);
  render(<I18nProvider locale="en" resources={{ en: { issues: enIssues } }}><QueryClientProvider client={client}>
    <WorkflowTransitionDialog issue={issue} target={workflow.statuses[1]!} workflowRevision={3} onClose={vi.fn()} />
  </QueryClientProvider></I18nProvider>);
  return client;
}
beforeEach(() => { vi.clearAllMocks(); mocks.transition.mockImplementation(() => new Promise(() => {})); });

describe("WorkflowTransitionDialog", () => {
  it("previews stopping the current execution before a status picker transition", () => {
    mount([entry("running")]);
    expect(screen.getByRole("dialog")).toHaveTextContent("Stops the current execution");
    expect(mocks.transition).not.toHaveBeenCalled();
  });
});
