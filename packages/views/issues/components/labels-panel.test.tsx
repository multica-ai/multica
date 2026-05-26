import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

const mockCreateLabel = vi.hoisted(() => vi.fn());
const mockLabels = vi.hoisted(() => ({
  list: [
    {
      id: "project-label",
      workspace_id: "ws-test",
      project_id: "project-1",
      name: "Project Only",
      color: "#ef4444",
      created_at: "",
      updated_at: "",
    },
    {
      id: "workspace-label",
      workspace_id: "ws-test",
      project_id: null,
      name: "Global",
      color: "#22c55e",
      created_at: "",
      updated_at: "",
    },
  ],
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: () => ({ data: mockLabels.list, isLoading: false }),
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/labels", () => ({
  labelListOptions: (wsId: string, scope?: { projectId?: string | null }) => ({
    queryKey: ["labels", wsId, scope?.projectId ?? null],
  }),
  useCreateLabel: () => ({ mutate: mockCreateLabel, isPending: false }),
  useUpdateLabel: () => ({ mutate: vi.fn(), isPending: false }),
  useDeleteLabel: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("../../labels/label-chip", () => ({
  LabelChip: ({ label }: { label: { name: string } }) => <span>{label.name}</span>,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn() },
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (selector: any, vars?: Record<string, string>) =>
      selector({
        labels_panel: {
          intro: "Create and manage labels to categorize issues across your workspace.",
          project_intro: "Create and manage labels for this project or the whole workspace.",
          scope_aria: "Label scope",
          scope_project: "This project",
          scope_workspace: "Workspace",
          new_placeholder: "New label name...",
          new_aria: "New label name",
          add_action: "Add",
          loading: "Loading...",
          empty_workspace: "No workspace labels yet.",
          empty_project: "No project labels yet.",
          name_required: "Label name is required.",
          color_label: "Color",
          pick_color_aria: "Pick a color",
          edit_aria: `Edit ${vars?.name ?? ""}`,
          delete_aria: `Delete ${vars?.name ?? ""}`,
          save_aria: "Save",
          cancel_aria: "Cancel",
          delete_dialog_title: "Delete label?",
          delete_dialog_desc_prefix: "The label ",
          delete_dialog_desc_suffix: " will be removed from all issues. This cannot be undone.",
          delete_dialog_cancel: "Cancel",
          delete_dialog_confirm: "Delete",
          create_failed: "Failed to create label",
          update_failed: "Failed to update label",
          delete_failed: "Failed to delete label",
        },
      }),
  }),
}));

import { LabelsPanel } from "./labels-panel";

describe("LabelsPanel", () => {
  beforeEach(() => {
    mockCreateLabel.mockReset();
  });

  it("defaults to project labels and creates in the current project", () => {
    render(<LabelsPanel projectId="project-1" />);

    expect(screen.getByText("Project Only")).toBeInTheDocument();
    expect(screen.queryByText("Global")).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("New label name"), {
      target: { value: "Regression" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(mockCreateLabel).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Regression", project_id: "project-1" }),
      expect.any(Object),
    );
  });

  it("switches to workspace labels and creates a global label", () => {
    render(<LabelsPanel projectId="project-1" />);

    fireEvent.click(screen.getByRole("button", { name: "Workspace" }));

    expect(screen.getByText("Global")).toBeInTheDocument();
    expect(screen.queryByText("Project Only")).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("New label name"), {
      target: { value: "Shared" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(mockCreateLabel).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Shared", project_id: null }),
      expect.any(Object),
    );
  });
});
