import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { BatchActionToolbar } from "./batch-action-toolbar";
import { IssueActionsContextMenu, IssueContextMenuProvider } from "../actions/issue-actions-context-menu";

// A right-click on a row that is part of a multi-row selection opens the
// batch menu for the whole selection instead of that row's own menu.

const selection = vi.hoisted(() => ({ selectedIds: new Set<string>(["a", "b"]), clear: vi.fn() }));

vi.mock("@multica/core/issues/stores/selection-store", () => ({
  useIssueSelectionStore: (selector: (s: typeof selection) => unknown) => selector(selection),
}));
vi.mock("@multica/core/issues/use-issue-deletion", () => ({
  useIssueDeletion: () => ({ canDelete: () => true }),
}));
vi.mock("@multica/core/issues/mutations", () => ({
  useBatchUpdateIssues: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useBatchDeleteIssues: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useCancelIssueRun: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issue-workflows", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issue-workflows")>()),
  useIssueWorkflows: () => ({ workflows: [] }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [] }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ getShareableUrl: (path: string) => `https://app.example${path}` }),
}));
vi.mock("../utils/status-options", () => ({ useIssueStatusOptions: () => ({ options: [] }) }));
vi.mock("./pickers", () => ({
  StatusPicker: () => null,
  PriorityPicker: () => null,
  AssigneePicker: () => null,
  AssigneePickerPanel: () => null,
}));
vi.mock("../../search/register-context", () => ({ useRegisterCommandBarContext: () => {} }));
vi.mock("../actions/use-issue-actions", () => ({
  useIssueActions: () => {
    throw new Error("the single-issue menu must not open for a selected row");
  },
}));

function issue(id: string): Issue {
  return { id, identifier: `MUL-${id}`, title: id, status: "todo", priority: "none", project_id: null } as Issue;
}

describe("BatchActionToolbar — batch context menu", () => {
  it("opens for a right-click on a selected row and acts on the whole selection", async () => {
    const issues = [issue("a"), issue("b"), issue("c")];
    renderWithI18n(
      <IssueContextMenuProvider>
        {issues.map((row) => (
          <IssueActionsContextMenu key={row.id} issue={row}>
            <div>row {row.id}</div>
          </IssueActionsContextMenu>
        ))}
        <BatchActionToolbar issues={issues} />
      </IssueContextMenuProvider>,
    );

    fireEvent.contextMenu(screen.getByText("row a"), { clientX: 10, clientY: 20 });
    const menu = await screen.findByRole("menu");
    expect(menu).toHaveTextContent("2 selected");
    expect(screen.getByRole("menuitem", { name: "Delete 2 issues" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("menuitem", { name: "Clear selection" }));
    expect(selection.clear).toHaveBeenCalledTimes(1);
  });
});
