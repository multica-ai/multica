import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { IssueWorkflow } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { WorkflowPill } from "./workflow-pill";

function workflow(id: string, name: string, isDefault: boolean): IssueWorkflow {
  return {
    id,
    workspace_id: "workspace-1",
    name,
    description: "",
    is_default: isDefault,
    owner_id: null,
    steps: [],
    project_ids: [],
    created_at: "",
    updated_at: "",
  };
}

vi.mock("@multica/core/config", () => ({ useFeatureEnabled: () => true }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
const workflows = vi.hoisted(() => ({ list: [] as IssueWorkflow[] }));
vi.mock("@multica/core/issue-workflows", () => ({
  useIssueWorkflows: () => ({ workflows: workflows.list }),
}));

beforeEach(() => {
  workflows.list = [workflow("wf-default", "Default", true), workflow("wf-delivery", "Delivery", false)];
});
afterEach(cleanup);

describe("WorkflowPill", () => {
  it("opens the workflow list and reports the choice", async () => {
    const onChange = vi.fn();
    renderWithI18n(<WorkflowPill value={null} onChange={onChange} />);

    fireEvent.click(screen.getByRole("button", { name: /Default/ }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Delivery/ }));

    expect(onChange).toHaveBeenCalledWith("wf-delivery");
  });

  it("shows Default and where to create more when it is the only workflow", async () => {
    workflows.list = [workflow("wf-default", "Default", true)];
    renderWithI18n(<WorkflowPill value={null} onChange={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: /Default/ }));

    expect(await screen.findByRole("menuitem", { name: /Default/ })).toBeTruthy();
    expect(screen.getByText(/Settings → Workflows/)).toBeTruthy();
  });
});
