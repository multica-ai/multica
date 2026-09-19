import { beforeEach, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueWorkflowResponse } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import { effectiveIssueWorkflowOptions } from "@multica/core/issue-workflows";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import enProjects from "../../locales/en/projects.json";
import enIssues from "../../locales/en/issues.json";
import { BoardWorkflowActions } from "./board-workflow-actions";

const mocks = vi.hoisted(() => ({ apply: vi.fn(), role: "admin" }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/permissions", () => ({ useCurrentMember: () => ({ role: mocks.role }) }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Reviewer" }) }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: ({ actorType, actorId }: { actorType: string; actorId: string }) => <span data-testid="executor-avatar">{actorType}:{actorId}</span> }));
vi.mock("@multica/core/api", () => ({ api: { applyProjectWorkflow: mocks.apply, getEffectiveIssueWorkflow: vi.fn() } }));
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));
beforeEach(() => { mocks.role = "admin"; mocks.apply.mockReset(); });

function setup(executor: "none" | "agent" | "squad", mode = "custom", statusId = "review") {
  const definition = {
    workflow: { id: "flow", name: "Workflow", revision: 4, initial_status_id: "todo" }, mode,
    statuses: [
      { id: "todo", spec_key: "todo", name: "Todo", phase: "unstarted", position: 0, color: "#888888", description: "", entry_policy: { executor: { type: "none" }, instructions: "" } },
      { id: "review", spec_key: "review", name: "Code Review", phase: "started", position: 1, color: "#888888", description: "", entry_policy: { executor: executor === "none" ? { type: "none" } : { type: executor, id: "reviewer" }, instructions: executor === "none" ? "" : "Review the change" } },
    ],
  } as IssueWorkflowResponse;
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false }, mutations: { retry: false } } });
  client.setQueryData(effectiveIssueWorkflowOptions("ws", "project", true).queryKey, definition);
  client.setQueryData(agentListOptions("ws").queryKey, [{
    id: "reviewer", name: "Reviewer", workspace_id: "ws", runtime_id: "runtime",
    description: "", instructions: "", avatar_url: null, runtime_mode: "local",
    runtime_config: {}, custom_args: [], visibility: "workspace",
    permission_mode: "public_to", invocation_targets: [], status: "idle",
    max_concurrent_tasks: 1, model: "", owner_id: "owner", skills: [],
    created_at: "", updated_at: "", archived_at: null, archived_by: null,
  }]);
  client.setQueryData(squadListOptions("ws").queryKey, [{
    id: "reviewer", workspace_id: "ws", name: "Review team", description: "",
    instructions: "", avatar_url: null, leader_id: "reviewer", creator_id: "owner",
    created_at: "", updated_at: "", archived_at: null, archived_by: null,
  }]);
  mocks.apply.mockResolvedValue(definition);
  render(<I18nProvider locale="en" resources={{ en: { projects: enProjects, issues: enIssues } }}><QueryClientProvider client={client}>
    <BoardWorkflowActions projectId="project" statusId={statusId} />
  </QueryClientProvider></I18nProvider>);
  return { user: userEvent.setup(), definition };
}

it.each(["agent", "squad"] as const)("opens the %s's column directly and saves only that status with revision protection", async (type) => {
  const { user } = setup(type);
  expect(screen.getByTestId("executor-avatar")).toHaveTextContent(`${type}:reviewer`);
  await user.click(screen.getByRole("button", { name: "Configure Code Review · Reviewer" }));
  expect(screen.getByRole("dialog")).toBeInTheDocument();
  expect(screen.getByLabelText("Status name")).toHaveValue("Code Review");
  expect(screen.queryByRole("button", { name: "Add status" })).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Starting status")).not.toBeInTheDocument();
  await user.clear(screen.getByLabelText("Action instructions"));
  await user.type(screen.getByLabelText("Action instructions"), "Check for regressions");
  await user.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mocks.apply).toHaveBeenCalledWith("project", expect.objectContaining({
    expected_revision: 4, mode: "custom", allow_archive: false,
    spec: expect.objectContaining({ initial_status: "todo", statuses: [
      expect.objectContaining({ key: "todo", name: "Todo", entry_policy: { executor: { type: "none" }, instructions: "" } }),
      expect.objectContaining({ key: "review", entry_policy: { executor: { type, id: "reviewer" }, instructions: "Check for regressions" } }),
    ] }),
  })));
});

it("adds an entry action from the menu and makes inherited settings project-specific", async () => {
  const { user } = setup("none", "default");
  expect(screen.queryByTestId("executor-avatar")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Code Review column options" }));
  await user.click(await screen.findByRole("menuitem", { name: "Configure column" }));
  expect(screen.getByLabelText("Status name")).toHaveValue("Code Review");
  expect(screen.getByText(enProjects.workflow.inherited_hint, { exact: false })).toBeInTheDocument();
  await user.click(screen.getByRole("combobox", { name: "Run on entry" }));
  await user.click(await screen.findByRole("option", { name: "Run a squad" }));
  await user.type(screen.getByLabelText("Action instructions"), "Review the change");
  await user.click(screen.getByRole("button", { name: "Save" }));
  expect(mocks.apply).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "Squad" })).toHaveAttribute("aria-invalid", "true");
  expect(screen.getByRole("button", { name: "Squad" })).toHaveFocus();
  expect(screen.getByRole("button", { name: "Squad" })).toHaveAccessibleDescription(enProjects.workflow.problems.executor);
  await user.click(screen.getByRole("button", { name: "Squad" }));
  await user.click(await screen.findByRole("button", { name: /Review team/ }));
  mocks.apply.mockRejectedValueOnce(new Error("Revision conflict"));
  await user.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByRole("alert");
  expect(screen.getByLabelText("Action instructions")).toHaveValue("Review the change");
  expect(mocks.apply).toHaveBeenCalledWith("project", expect.objectContaining({ mode: "custom", expected_revision: 4 }));
});

it("shows the executor without editing controls to non-admins", () => {
  mocks.role = "member";
  setup("agent");
  expect(screen.getByTestId("executor-avatar")).toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});

it("does not edit a historical column through the current workflow", () => {
  setup("agent", "custom", "old-status");
  expect(screen.queryByTestId("executor-avatar")).not.toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});
