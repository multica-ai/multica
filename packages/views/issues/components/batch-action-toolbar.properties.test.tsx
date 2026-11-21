import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen } from "@testing-library/react";
import type { Issue, IssueProperty } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { BatchActionToolbar } from "./batch-action-toolbar";

const selection = vi.hoisted(() => ({ selectedIds: new Set(["a", "b"]), clear: vi.fn() }));
const batchUpdate = vi.hoisted(() => vi.fn());
const PROPERTIES = vi.hoisted(() => [
  { id: "notes", name: "Notes", type: "multi_text", config: {}, archived: false },
] as IssueProperty[]);

vi.mock("@multica/core/issues/stores/selection-store", () => ({
  useIssueSelectionStore: (selector: (s: typeof selection) => unknown) => selector(selection),
}));
vi.mock("@multica/core/issues/use-issue-deletion", () => ({
  useIssueDeletion: () => ({ canDelete: () => true }),
}));
vi.mock("@multica/core/issues/mutations", () => ({
  useBatchUpdateIssues: () => ({ mutateAsync: batchUpdate, isPending: false }),
  useBatchDeleteIssues: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issue-workflows", () => ({
  useIssueWorkflows: () => ({ workflows: [] }),
  sharedWorkflowStatusKeys: () => null,
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: PROPERTIES }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ getShareableUrl: (path: string) => `https://app.example${path}` }),
}));
vi.mock("./pickers", () => ({
  StatusPicker: () => null,
  PriorityPicker: () => null,
  AssigneePicker: () => null,
  AssigneePickerPanel: () => null,
  DueDatePicker: () => null,
  StartDatePicker: () => null,
}));
vi.mock("./pickers/batch-label-picker", () => ({
  BatchLabelPicker: () => null,
  BatchLabelPanel: () => null,
}));
vi.mock("../../projects/components/project-picker", () => ({ ProjectPicker: () => null }));
vi.mock("../../search/register-context", () => ({ useRegisterCommandBarContext: () => {} }));
const toast = vi.hoisted(() => ({ success: vi.fn(), warning: vi.fn(), error: vi.fn(), dismiss: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

function issue(id: string): Issue {
  return { id, identifier: id, title: id, status: "todo", priority: "none", project_id: null, properties: {} } as Issue;
}

beforeEach(() => {
  batchUpdate.mockReset();
});

async function openNotesEditor() {
  renderWithI18n(<BatchActionToolbar issues={[issue("a"), issue("b")]} />);
  fireEvent.click(screen.getByRole("button", { name: "More actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Custom properties" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Notes" }));
  return screen.findByRole("textbox", { name: "Notes" });
}

async function submit(input: HTMLElement, value: string) {
  fireEvent.change(input, { target: { value } });
  fireEvent.submit(input.closest("form")!);
  await act(async () => {});
}

const sentNotes = () => batchUpdate.mock.calls.map(([request]) => request.properties.set.notes);

describe("BatchActionToolbar multi-value property editor", () => {
  it("keeps the draft locked while the save is in flight, so a second entry cannot replace the first", async () => {
    batchUpdate.mockReturnValue(new Promise(() => {}));
    const input = await openNotesEditor();

    await submit(input, "alpha");
    expect(batchUpdate).toHaveBeenCalledWith({ ids: ["a", "b"], updates: {}, properties: { set: { notes: ["alpha"] } } });
    expect(input).toHaveValue("alpha");
    expect(input).toHaveAttribute("readonly");

    await submit(input, "beta");
    expect(sentNotes()).toEqual([["alpha"]]);
  });

  it("keeps the draft and shows the failure when the write does not apply", async () => {
    batchUpdate.mockResolvedValue({
      updated: 1,
      results: [
        { issue_id: "a", outcome: "updated" },
        { issue_id: "b", outcome: "skipped", code: "properties_too_large" },
      ],
    });
    const input = await openNotesEditor();

    await submit(input, "alpha");
    expect(input).toHaveValue("alpha");
    expect(input).not.toHaveAttribute("readonly");
    expect(screen.getByText("Failed to update issues")).toBeInTheDocument();
  });

  it("clears the draft once every issue took the value", async () => {
    batchUpdate.mockResolvedValue({
      updated: 2,
      results: [
        { issue_id: "a", outcome: "updated" },
        { issue_id: "b", outcome: "updated" },
      ],
    });
    const input = await openNotesEditor();

    await submit(input, "alpha");
    expect(input).toHaveValue("");
    expect(input).not.toHaveAttribute("readonly");
  });
});
