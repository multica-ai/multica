import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

const mockCreateLabel = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (options: { queryKey?: readonly unknown[]; initialData?: unknown }) => {
      if (options.queryKey?.includes("issue")) {
        const initialData = options.initialData as { labels?: unknown[] } | undefined;
        return { data: initialData?.labels ?? [] };
      }
      return { data: [] };
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/labels", () => ({
  labelListOptions: (wsId: string, scope?: { projectId?: string | null }) => ({
    queryKey: ["labels", wsId, "list", scope?.projectId ?? null],
  }),
  issueLabelsOptions: (wsId: string, issueId: string) => ({
    queryKey: ["labels", wsId, "issue", issueId],
  }),
  useAttachLabel: () => ({ mutate: vi.fn() }),
  useDetachLabel: () => ({ mutate: vi.fn() }),
  useCreateLabel: () => ({ mutate: mockCreateLabel, isPending: false }),
}));

vi.mock("../../../labels/label-chip", () => ({
  LabelChip: ({ label }: { label: { name: string } }) => <span>{label.name}</span>,
}));

vi.mock("../labels-panel", () => ({
  LabelsPanel: () => <div />,
}));

vi.mock("../../../i18n", () => ({
  useT: () => ({
    t: (selector: any) =>
      selector({
        filters: { placeholder: "Filter" },
        pickers: {
          filter_options_aria: "Filter options",
          no_results: "No results",
          label: {
            trigger_label: "Add label",
            search_placeholder: "Search labels",
            manage_action: "Manage labels",
            manage_dialog_title: "Manage labels",
            create_action: "Create",
            create_failed: "Create failed",
            scope_aria: "Label creation scope",
            scope_project: "Current project",
            scope_workspace: "Workspace",
          },
        },
      }),
  }),
}));

import { LabelPicker } from "./label-picker";

describe("LabelPicker", () => {
  beforeEach(() => {
    mockCreateLabel.mockReset();
    mockCreateLabel.mockImplementation(
      (
        data: { name: string; color: string; project_id?: string | null },
        opts?: { onSuccess?: (label: { id: string }) => void; onSettled?: () => void },
      ) => {
        opts?.onSuccess?.({ id: `${data.project_id ?? "workspace"}:${data.name}` });
        opts?.onSettled?.();
      },
    );
  });

  it("uses a compact small-text add trigger when appended to an empty board card label row", () => {
    render(<LabelPicker issueId="issue-1" labels={[]} appendAddTrigger />);

    const trigger = screen.getByRole("button", { name: "Add label" });
    expect(trigger).toHaveAttribute("title", "Add label");
    expect(trigger).toHaveTextContent("Add label");
    expect(trigger).toHaveClass("text-[10px]");
  });

  it("creates project labels by default and workspace labels when scope is switched", () => {
    render(<LabelPicker issueId="issue-1" labels={[]} projectId="project-1" defaultOpen />);

    fireEvent.change(screen.getByRole("textbox", { name: "Filter options" }), {
      target: { value: "Project Label" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Create/ }));

    expect(mockCreateLabel).toHaveBeenLastCalledWith(
      expect.objectContaining({ name: "Project Label", project_id: "project-1" }),
      expect.any(Object),
    );

    fireEvent.click(screen.getByRole("button", { name: "Workspace" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Filter options" }), {
      target: { value: "Global Label" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Create/ }));

    expect(mockCreateLabel).toHaveBeenLastCalledWith(
      expect.objectContaining({ name: "Global Label", project_id: null }),
      expect.any(Object),
    );
  });
});
