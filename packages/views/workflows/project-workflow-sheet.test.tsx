import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { IssueWorkflow, Project } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { ProjectWorkflowSheet } from "./project-workflow-sheet";

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  permissions: { canCreateWorkflow: true, canEditWorkflow: false, canManageProject: true },
}));

const releaseWorkflow = {
  id: "wf-release",
  workspace_id: "workspace-1",
  name: "Release",
  description: "",
  is_default: false,
  owner_id: null,
  steps: [{ status_key: "todo" }, { status_key: "in_review" }],
  project_ids: ["project-1"],
  created_at: "",
  updated_at: "",
} as unknown as IssueWorkflow;

vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({ data: [] }) }));
vi.mock("@multica/core/api", () => ({ api: {}, clientErrorMessage: () => null }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ settings: () => "/settings" }) }));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: () => ({}) }));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({ categoryOf: () => "unstarted", colorOf: () => undefined, iconOf: () => undefined }),
}));
vi.mock("@multica/core/issue-workflows", () => ({
  useIssueWorkflows: () => ({ workflows: [releaseWorkflow] }),
  resolveProjectWorkflow: () => releaseWorkflow,
  statusAcceptsHandoff: () => true,
  handoffStepCount: () => 0,
  useSetProjectWorkflow: () => ({ isPending: false, mutateAsync: vi.fn() }),
}));
vi.mock("../navigation", () => ({ useNavigation: () => ({ push: mocks.push }) }));
vi.mock("./permissions", () => ({ useWorkflowPermissions: () => mocks.permissions }));
vi.mock("./workflow-mapping-dialog", () => ({ WorkflowMappingDialog: () => null }));
vi.mock("./handoff-form", () => ({ WorkflowHandoffForm: () => null }));
vi.mock("./step-handler", () => ({ useStepHandlerLabel: () => () => null, stepHandlerActor: () => null }));
vi.mock("../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../issues/components/status-icon", () => ({ StatusIcon: () => null }));
vi.mock("../issues/utils/status-label", () => ({ useStatusLabel: () => (key: string) => key }));

const project = { id: "project-1", title: "Launch", workflow_id: "wf-release" } as Project;

beforeEach(() => {
  mocks.push.mockReset();
  mocks.permissions = { canCreateWorkflow: true, canEditWorkflow: false, canManageProject: true };
});
afterEach(cleanup);

describe("ProjectWorkflowSheet", () => {
  it("offers switching and managing once, in the panel header", () => {
    renderWithI18n(<ProjectWorkflowSheet project={project} open onOpenChange={vi.fn()} />);

    expect(screen.getByText("Release workflow")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /Switch workflow/ })).toHaveLength(1);
    expect(screen.getAllByRole("button", { name: /Manage workflows/ })).toHaveLength(1);
    expect(screen.getByRole("button", { name: /todo/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /in_review/ })).toBeInTheDocument();
  });

  it("shows members who can't switch the flow without the switch control", () => {
    mocks.permissions = { canCreateWorkflow: true, canEditWorkflow: false, canManageProject: false };
    renderWithI18n(<ProjectWorkflowSheet project={project} open onOpenChange={vi.fn()} />);

    expect(screen.getByText("Release workflow")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Switch workflow/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /todo/ })).toBeInTheDocument();
  });

  it("closes the panel before going to workflow settings", () => {
    const onOpenChange = vi.fn();
    renderWithI18n(<ProjectWorkflowSheet project={project} open onOpenChange={onOpenChange} />);

    fireEvent.click(screen.getByRole("button", { name: /Manage workflows/ }));

    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(mocks.push).toHaveBeenCalledWith("/settings?tab=workflows");
  });
});
