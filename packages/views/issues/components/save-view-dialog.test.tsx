import { createStore } from "zustand/vanilla";
import { describe, expect, it, vi } from "vitest";
import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  type IssueViewState,
  viewStoreSlice,
} from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { renderWithI18n } from "../../test/i18n";
import { DraftDefinitionFields, DraftWorkflowFields } from "./save-view-dialog";

const queryOptionsSeen = vi.hoisted(() => vi.fn());
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: (options: { queryKey: readonly unknown[] }) => {
    queryOptionsSeen(options);
    return options.queryKey[0] === "issue-workflows" ? { isSuccess: true, data: { workflow: { id: "default" }, statuses: [] } } : { data: undefined };
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("./issues-header", () => ({
  IssueFilterMenu: ({ trigger }: { trigger: React.ReactNode }) => trigger,
}));

vi.mock("./filter-chips-bar", () => ({
  FilterChipList: ({ trailing }: { trailing?: React.ReactNode }) => trailing,
}));

function renderFields(sortBy: IssueViewState["sortBy"]) {
  const store = createStore<IssueViewState>()(viewStoreSlice);
  store.setState({ sortBy, sortDirection: "asc" });
  renderWithI18n(
    <ViewStoreProvider store={store}>
      <DraftDefinitionFields />
    </ViewStoreProvider>,
  );
  return store;
}

describe("DraftDefinitionFields ordering", () => {
  it("lets a saved view default switch between ascending and descending", async () => {
    const user = userEvent.setup();
    const store = renderFields("created_at");

    await user.click(screen.getByRole("button", { name: /Default display/ }));
    await user.click(screen.getByRole("button", { name: "Oldest first" }));

    expect(store.getState().sortDirection).toBe("desc");
    expect(screen.getByRole("button", { name: "Newest first" })).toBeInTheDocument();
  });

  it("hides direction for manual ordering, where direction has no effect", async () => {
    const user = userEvent.setup();
    renderFields("position");

    await user.click(screen.getByRole("button", { name: /Default display/ }));

    expect(screen.queryByRole("button", { name: "Workflow order" })).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Reverse workflow order" }),
    ).not.toBeInTheDocument();
  });
});


it("resolves saved-view status choices from the draft project filters rather than the page behind it", () => {
  const store = createStore<IssueViewState>()(viewStoreSlice);
  const node = "22222222-2222-4222-8222-222222222222";
  store.setState({ projectFilters: ["project-a"], statusFilters: [node] });
  renderWithI18n(<ViewStoreProvider store={store}><DraftWorkflowFields scope={{ kind: "workspace" }} /></ViewStoreProvider>);
  expect(queryOptionsSeen).toHaveBeenCalledWith(expect.objectContaining({ queryKey: ["issue-workflows", "workspace-1", "effective", "project-a", { includeArchived: true }] }));
  const requestKeys = queryOptionsSeen.mock.calls.map(([options]) => JSON.stringify(options.queryKey));
  expect(requestKeys.some((key) => key.includes('"workflow_status_ids":["' + node + '"]'))).toBe(true);
  act(() => store.setState({ projectFilters: ["project-b"] }));
  expect(queryOptionsSeen).toHaveBeenCalledWith(expect.objectContaining({ queryKey: ["issue-workflows", "workspace-1", "effective", "project-b", { includeArchived: true }] }));
  expect(store.getState().statusFilters).toEqual([node]);
});
