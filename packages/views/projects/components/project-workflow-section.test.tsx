import { expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueWorkflowResponse } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import { effectiveIssueWorkflowOptions } from "@multica/core/issue-workflows";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import enProjects from "../../locales/en/projects.json";
import { ProjectWorkflowSection } from "./project-workflow-section";

const apply = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/api", () => ({
  api: { applyProjectWorkflow: apply, getEffectiveIssueWorkflow: vi.fn() },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));

it("edits the complete definition locally and saves once with its original revision", async () => {
  const user = userEvent.setup();
  const policy = {
    assignee: { type: "keep" },
    executor: { type: "none" },
    instructions: "",
    advance: "human_confirms",
  };
  const definition = {
    workflow: {
      id: "flow",
      name: "Workflow",
      revision: 4,
      initial_status_id: "dev",
    },
    mode: "custom",
    statuses: [
      {
        id: "dev",
        spec_key: "implementation",
        name: "Implementation",
        description: "",
        color: "#6552cb",
        phase: "started",
        position: 0,
        entry_policy: { ...policy, next_status_key: "code_review" },
      },
      {
        id: "review",
        spec_key: "code_review",
        name: "Code Review",
        description: "",
        color: "#6552cb",
        phase: "completed",
        position: 1,
        entry_policy: policy,
      },
    ],
  } as IssueWorkflowResponse;
  apply.mockResolvedValue(definition);
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity, retry: false } },
  });
  client.setQueryData(
    effectiveIssueWorkflowOptions("ws", "project", true).queryKey,
    definition,
  );
  for (const query of [
    memberListOptions("ws"),
    agentListOptions("ws"),
    squadListOptions("ws"),
  ])
    client.setQueryData(query.queryKey, []);
  render(
    <I18nProvider locale="en" resources={{ en: { projects: enProjects } }}>
      <QueryClientProvider client={client}>
        <ProjectWorkflowSection projectId="project" canEdit />
      </QueryClientProvider>
    </I18nProvider>,
  );
  await user.click(screen.getByRole("button", { name: "Edit workflow" }));
  await user.clear(screen.getByLabelText("Status name"));
  await user.type(screen.getByLabelText("Status name"), "Build");
  expect(apply).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Save workflow" }));
  await waitFor(() =>
    expect(apply).toHaveBeenCalledWith(
      "project",
      expect.objectContaining({
        expected_revision: 4,
        mode: "custom",
        spec: expect.objectContaining({
          initial_status: "implementation",
          statuses: expect.arrayContaining([
            expect.objectContaining({
              key: "implementation",
              name: "Build",
              entry_policy: expect.objectContaining({
                next_status_key: "code_review",
              }),
            }),
          ]),
        }),
      }),
    ),
  );
});
