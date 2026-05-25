import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

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
  labelListOptions: (wsId: string) => ({ queryKey: ["labels", wsId, "list"] }),
  issueLabelsOptions: (wsId: string, issueId: string) => ({
    queryKey: ["labels", wsId, "issue", issueId],
  }),
  useAttachLabel: () => ({ mutate: vi.fn() }),
  useDetachLabel: () => ({ mutate: vi.fn() }),
  useCreateLabel: () => ({ mutate: vi.fn(), isPending: false }),
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
          },
        },
      }),
  }),
}));

import { LabelPicker } from "./label-picker";

describe("LabelPicker", () => {
  it("uses a compact small-text add trigger when appended to an empty board card label row", () => {
    render(<LabelPicker issueId="issue-1" labels={[]} appendAddTrigger />);

    const trigger = screen.getByRole("button", { name: "Add label" });
    expect(trigger).toHaveAttribute("title", "Add label");
    expect(trigger).toHaveTextContent("Add label");
    expect(trigger).toHaveClass("text-[10px]");
  });
});
