import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { IssueWorkflow, Project } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { ProjectWorkflowControl } from "./project-workflow-row";

const releaseWorkflow = {
  id: "wf-release",
  workspace_id: "workspace-1",
  name: "版本发布",
  description: "",
  is_default: false,
  owner_id: null,
  steps: [],
  project_ids: ["project-1"],
  created_at: "",
  updated_at: "",
} as IssueWorkflow;

vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({ data: [] }) }));
vi.mock("@multica/core/api", () => ({ api: {}, clientErrorMessage: () => null }));
vi.mock("@multica/core/config", () => ({ useFeatureEnabled: () => true }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ settings: () => "/settings" }) }));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: () => ({}) }));
vi.mock("@multica/core/issue-workflows", () => ({
  useIssueWorkflows: () => ({ workflows: [releaseWorkflow] }),
  resolveProjectWorkflow: () => releaseWorkflow,
  handoffStepCount: () => 3,
  useSetProjectWorkflow: () => ({ isPending: false, mutateAsync: vi.fn() }),
}));
vi.mock("../navigation", () => ({ useNavigation: () => ({ push: vi.fn() }) }));
vi.mock("./permissions", () => ({ useWorkflowPermissions: () => ({ canManageProject: true }) }));
vi.mock("./workflow-mapping-dialog", () => ({ WorkflowMappingDialog: () => null }));

afterEach(cleanup);

const project = { id: "project-1", title: "Multica 线上发布", workflow_id: "wf-release" } as Project;

describe("ProjectWorkflowControl", () => {
  // MUL-8119: in the narrow project sidebar the handoff count used to keep its
  // width and squeeze the workflow name down to a single glyph.
  it("gives the whole row to the workflow name and moves the handoff count to the title", () => {
    renderWithI18n(<ProjectWorkflowControl project={project} />);

    const trigger = screen.getByRole("button", { name: /版本发布/ });
    expect(trigger.getAttribute("title")).toBe("版本发布 · 3 handoff steps");
    expect(trigger.textContent).toBe("版本发布");
  });
});
