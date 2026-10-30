import { createStore } from "zustand/vanilla";
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  type IssueViewState,
  viewStoreSlice,
} from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { renderWithI18n } from "../../test/i18n";
import { DraftDefinitionFields, SaveViewDialog } from "./save-view-dialog";

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [] }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/issue-views/mutations", () => ({
  useCreateIssueView: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateIssueView: () => ({ mutate: vi.fn(), isPending: false }),
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

describe("SaveViewDialog draft lifecycle", () => {
  const liveStore = createStore<IssueViewState>()(viewStoreSlice);
  // Hosts build `scope` inline, so every host render passes a new object.
  const dialog = (open: boolean) => (
    <ViewStoreProvider store={liveStore}>
      <SaveViewDialog
        open={open}
        onOpenChange={() => {}}
        scope={{ kind: "my", variant: "assigned" }}
      />
    </ViewStoreProvider>
  );

  it("keeps a half-typed name when the host re-renders", async () => {
    const user = userEvent.setup();
    const { rerender } = renderWithI18n(dialog(true));

    await user.type(screen.getByLabelText("Name"), "Ongoing");
    rerender(dialog(true));

    expect(screen.getByLabelText("Name")).toHaveValue("Ongoing");
  });

  it("starts from a blank name on the next open", async () => {
    const user = userEvent.setup();
    const { rerender } = renderWithI18n(dialog(true));

    await user.type(screen.getByLabelText("Name"), "Ongoing");
    rerender(dialog(false));
    rerender(dialog(true));

    expect(screen.getByLabelText("Name")).toHaveValue("");
  });
});
