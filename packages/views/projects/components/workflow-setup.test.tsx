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
  memberListOptions,
  agentListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import enProjects from "../../locales/en/projects.json";
import { WorkflowSetup, type WorkflowSetupScreen } from "./workflow-setup";
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
    memberListOptions("ws"),
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
          phase: "backlog",
          color: "#6b7280",
          description: "",
          archived_at: null,
          entry_policy: {
            assignee: { type: "keep" },
            executor: { type: "none" },
            instructions: "",
            advance: "human_confirms",
          },
        },
      ],
    });
  }
  render(
    <I18nProvider locale="en" resources={{ en: { projects: enProjects } }}>
      <QueryClientProvider client={qc}>
        <Harness />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return qc;
}
describe("workflow setup", () => {
  it("separates workflow sources from specific templates and opens the shared status editor", async () => {
    setup();
    const user = userEvent.setup();
    expect(
      screen.getByRole("button", { name: /Start from scratch/ }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: /Copy from a project/ }),
    ).toBeVisible();
    expect(
      screen.queryByText("Software development and review"),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Use a template/ }));
    await user.click(
      screen.getByRole("button", { name: /Software development and review/ }),
    );
    expect(screen.getByLabelText("Status name")).toHaveValue("Ready");
    await user.click(
      screen.getByRole("button", { name: /Code review.*Choose an agent/ }),
    );
    expect(screen.getByLabelText("Status name")).toHaveValue("Code review");
    expect(
      screen.getByLabelText("Action instructions"),
    ).toHaveValue(enProjects.workflow.review_prompt);
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


it("configures the owner and action independently", async () => {
  const qc = setup();
  qc.setQueryData(memberListOptions("ws").queryKey, [{
    id: "member", workspace_id: "ws", user_id: "owner", name: "Alex",
    role: "member", created_at: "", email: "alex@example.test", avatar_url: null,
  }]);
  qc.setQueryData(agentListOptions("ws").queryKey, [{
    id: "executor", name: "Reviewer", workspace_id: "ws", runtime_id: "runtime",
    description: "", instructions: "", avatar_url: null, runtime_mode: "local",
    runtime_config: {}, custom_args: [], visibility: "workspace",
    permission_mode: "public_to", invocation_targets: [], status: "idle",
    max_concurrent_tasks: 1, model: "", owner_id: "owner", skills: [],
    created_at: "", updated_at: "", archived_at: null, archived_by: null,
  }]);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: /Start from scratch/ }));
  await user.click(screen.getByRole("combobox", { name: "Owner" }));
  await user.click(await screen.findByRole("option", { name: "Alex" }));
  await user.click(screen.getByRole("combobox", { name: "Run on entry" }));
  await user.click(await screen.findByRole("option", { name: "Reviewer · Agent" }));
  expect(screen.getByRole("combobox", { name: "Owner" })).toHaveTextContent("Alex");
  await user.click(screen.getByRole("combobox", { name: "Owner" }));
  await user.click(await screen.findByRole("option", { name: "Keep current assignee" }));
  expect(screen.getByRole("combobox", { name: "Run on entry" })).toHaveTextContent("Reviewer · Agent");
  await user.click(screen.getByRole("combobox", { name: "How does work advance?" }));
  await user.click(await screen.findByRole("option", { name: "Allow the executor to advance" }));
  await user.click(screen.getByRole("combobox", { name: "Run on entry" }));
  await user.click(await screen.findByRole("option", { name: "No action" }));
  await user.click(screen.getByRole("combobox", { name: "Run on entry" }));
  await user.click(await screen.findByRole("option", { name: "Reviewer · Agent" }));
  expect(screen.getByRole("combobox", { name: "How does work advance?" })).toHaveTextContent("Wait for human confirmation");
});
