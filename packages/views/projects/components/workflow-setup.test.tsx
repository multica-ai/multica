import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import {
  effectiveIssueWorkflowOptions,
  type WorkflowDraft,
} from "@multica/core/issue-workflows";
import { projectListOptions } from "@multica/core/projects";
import {
  agentListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import enProjects from "../../locales/en/projects.json";
import enIssues from "../../locales/en/issues.json";
import { WorkflowSetup, type WorkflowSetupScreen } from "./workflow-setup";
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));

function Harness() {
  const [value, setValue] = useState<WorkflowDraft>();
  const [page, setPage] = useState<WorkflowSetupScreen>("source");
  return (
    <WorkflowSetup
      value={value}
      screen={page}
      onScreenChange={setPage}
      onChange={setValue}
      showErrors={false}
      disabled={false}
    />
  );
}
function setup(copy = false) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  for (const query of [
    agentListOptions("ws"),
    squadListOptions("ws"),
  ])
    qc.setQueryData(query.queryKey, []);
  if (copy) {
    qc.setQueryData(projectListOptions("ws").queryKey, {
      projects: [{ id: "source", title: "Editorial", workspace_id: "ws", description: null, icon: null, status: "planned", priority: "none", lead_type: null, lead_id: null, start_date: null, due_date: null, created_at: "", updated_at: "", issue_count: 0, done_count: 0, resource_count: 0 }],
      total: 1,
    });
    qc.setQueryData(effectiveIssueWorkflowOptions("ws", "source").queryKey, {
      workflow: { id: "flow", initial_status_id: "first", workspace_id: "ws", scope_type: "project", scope_id: "source", name: "Editorial", revision: 1, created_at: "", updated_at: "" },
      mode: "custom",
      statuses: [
        {
          id: "first", workflow_id: "flow", legacy_status_key: null, outcome: null, entry_policy_revision: 1, created_at: "", updated_at: "",
          spec_key: "draft",
          name: "Draft",
          position: 0,
          phase: "unstarted",
          color: "#6b7280",
          description: "",
          archived_at: null,
          entry_policy: {
            executor: { type: "none" },
            instructions: "",
          },
        },
      ],
    });
  }
  render(
    <I18nProvider locale="en" resources={{ en: { projects: enProjects, issues: enIssues } }}>
      <QueryClientProvider client={qc}>
        <Harness />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return qc;
}
describe("workflow setup", () => {
  it("defaults to inheritance and allows creating a workflow or copying a project", async () => {
    setup();
    const user = userEvent.setup();
    expect(screen.getByRole("button", { name: /Inherit from workspace/ })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: /Create a new workflow/ })).toBeVisible();
    expect(screen.getByRole("button", { name: /Copy from a project/ })).toBeVisible();
    expect(screen.queryByRole("button", { name: /template/i })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Create a new workflow/ }));
    expect(screen.getByLabelText("Status name")).toHaveValue("Todo");
    await user.click(screen.getByRole("button", { name: "Change workflow source" }));
    await user.click(screen.getByRole("button", { name: /Inherit from workspace/ }));
    expect(screen.getByRole("button", { name: /Inherit from workspace/ })).toHaveAttribute("aria-pressed", "true");
    expect(screen.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument();
  });
});

it("previews a source project before copying its statuses into the local editor", async () => {
  setup(true);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: /Copy from a project/ }));
  await user.click(screen.getByRole("combobox", { name: "Select a project" }));
  await user.click(await screen.findByRole("option", { name: "Editorial" }));
  expect(
    screen.getByRole("region", { name: "Workflow preview" }),
  ).toHaveTextContent("Draft");
  await user.click(screen.getByRole("button", { name: "Use this workflow" }));
  expect(screen.getByLabelText("Status name")).toHaveValue("Draft");
});


it("chooses an action type before its target and clears the target when switching types", async () => {
  const qc = setup();
  qc.setQueryData(agentListOptions("ws").queryKey, [{
    id: "executor", name: "Reviewer", workspace_id: "ws", runtime_id: "runtime",
    description: "", instructions: "", avatar_url: null, runtime_mode: "local",
    runtime_config: {}, custom_args: [], visibility: "workspace",
    permission_mode: "public_to", invocation_targets: [], status: "idle",
    max_concurrent_tasks: 1, model: "", owner_id: "owner", skills: [],
    created_at: "", updated_at: "", archived_at: null, archived_by: null,
  }]);
  qc.setQueryData(squadListOptions("ws").queryKey, [{
    id: "squad", workspace_id: "ws", name: "Review team", description: "",
    instructions: "", avatar_url: null, leader_id: "executor", creator_id: "owner",
    created_at: "", updated_at: "", archived_at: null, archived_by: null,
  }]);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: /Create a new workflow/ }));
  expect(screen.queryByRole("combobox", { name: "Owner" })).not.toBeInTheDocument();
  expect(screen.queryByRole("combobox", { name: "Next status" })).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Action instructions")).not.toBeInTheDocument();
  await user.click(screen.getByRole("combobox", { name: "Run on entry" }));
  expect((await screen.findAllByRole("option")).map((option) => option.textContent)).toEqual(["No action", "Run an agent", "Run a squad"]);
  await user.click(screen.getByRole("option", { name: "Run an agent" }));
  expect(screen.getByRole("button", { name: "Agent" })).toHaveTextContent("Choose an agent…");
  await user.click(screen.getByRole("button", { name: "Agent" }));
  expect(await screen.findByRole("button", { name: "Reviewer" })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Review team" })).not.toBeInTheDocument();
  await user.type(screen.getByPlaceholderText("Search agents…"), "missing");
  expect(screen.queryByRole("button", { name: "Reviewer" })).not.toBeInTheDocument();
  await user.clear(screen.getByPlaceholderText("Search agents…"));
  await user.type(screen.getByPlaceholderText("Search agents…"), "review");
  await user.keyboard("{Enter}");
  expect(screen.getByRole("button", { name: "Agent" })).toHaveTextContent("Reviewer");
  await user.type(screen.getByLabelText("Action instructions"), "Review the change");
  await user.click(screen.getByRole("combobox", { name: "Run on entry" }));
  await user.click(await screen.findByRole("option", { name: "Run a squad" }));
  expect(screen.queryByRole("button", { name: "Agent" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Squad" })).toHaveTextContent("Choose a squad…");
  expect(screen.getByLabelText("Action instructions")).toHaveValue("Review the change");
  await user.click(screen.getByRole("button", { name: "Squad" }));
  expect(await screen.findByRole("button", { name: "Review team" })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Reviewer" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /Review team/ }));
  await user.click(screen.getByRole("combobox", { name: "Run on entry" }));
  await user.click(await screen.findByRole("option", { name: "No action" }));
  expect(screen.queryByRole("button", { name: "Squad" })).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Action instructions")).not.toBeInTheDocument();
  await user.click(screen.getByRole("combobox", { name: "Run on entry" }));
  await user.click(await screen.findByRole("option", { name: "Run an agent" }));
  expect(screen.getByRole("button", { name: "Agent" })).toHaveTextContent("Choose an agent…");
  expect(screen.getByLabelText("Action instructions")).toHaveValue("");
});

it("shows an empty target list without mixing the other executor type into it", async () => {
  setup();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: /Create a new workflow/ }));
  await user.click(screen.getByRole("combobox", { name: "Run on entry" }));
  await user.click(await screen.findByRole("option", { name: "Run a squad" }));
  await user.click(screen.getByRole("button", { name: "Squad" }));
  expect(await screen.findByRole("status")).toHaveTextContent("No squads available");
});
