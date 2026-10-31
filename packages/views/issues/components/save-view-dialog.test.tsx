import { IssueViewSchema } from "@multica/core/api/schemas";
import { clearIssueSurfaceViewState, getIssueSurfaceViewStore, seedIssueSurfaceViewState } from "@multica/core/issues/stores/surface-view-store";
import { createStore } from "zustand/vanilla";
import { describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
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

const { createView, updateView } = vi.hoisted(() => ({ createView: vi.fn(), updateView: vi.fn() }));
vi.mock("@multica/core/issue-views/mutations", () => ({
  useCreateIssueView: () => ({ mutate: createView, isPending: false }),
  useUpdateIssueView: () => ({ mutate: updateView, isPending: false }),
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


describe("project swimlane saved definitions", () => {
  it.each(["create", "update"] as const)("keeps axes and layout through the actual %s handler and first-open seed", async (operation) => {
    createView.mockClear();
    updateView.mockClear();
    const store = createStore<IssueViewState>()(viewStoreSlice);
    store.setState({
      viewMode: "swimlane", swimlaneGrouping: "project",
      swimlaneProjectAxis: operation === "create" ? "project-columns" : "status-columns",
      swimlaneProjectColumnOrder: ["p2", "p1"],
      collapsedSwimlaneStatusRows: ["qa"],
    });
    const editView = IssueViewSchema.parse({
      id: `axis-${operation}`, name: "Project axes", revision: 7,
      query: { viewMode: "swimlane", swimlaneGrouping: "project" },
      display: { swimlaneProjectAxis: "status-columns", swimlaneProjectColumnOrder: ["p2", "p1"], collapsedSwimlaneStatusRows: ["qa"] },
    });
    renderWithI18n(
      <ViewStoreProvider store={store}>
        <SaveViewDialog open onOpenChange={() => {}} scope={{ kind: "workspace", actorKind: "all" }}
          editView={operation === "update" ? editView : undefined}
          seedFromDefinition={operation === "update"} />
      </ViewStoreProvider>,
    );
    const user = userEvent.setup();
    if (operation === "create") {
      await user.type(screen.getByLabelText("Name"), "Project axes");
    } else {
      // Definition editing starts with the definition rather than the live
      // override. Exercise the draft's real axis control before updating.
      await user.click(screen.getByRole("button", { name: /Default display/ }));
      await user.click(screen.getByRole("combobox", { name: "Axes" }));
      await user.click(screen.getByRole("option", { name: "Project columns / Status rows" }));
      expect(store.getState().swimlaneProjectAxis).toBe("status-columns");
    }
    await user.click(screen.getByRole("button", { name: operation === "create" ? "Create view" : "Save changes" }));
    const mutation = operation === "create" ? createView : updateView;
    await waitFor(() => expect(mutation).toHaveBeenCalledOnce());
    const request = mutation.mock.calls[0]![0];
    expect(request.display).toMatchObject({
      swimlaneProjectAxis: "project-columns",
      swimlaneProjectColumnOrder: ["p2", "p1"],
      collapsedSwimlaneStatusRows: ["qa"],
    });
    if (operation === "update") expect(request.expected_revision).toBe(7);
    // Use the production response schema and surface seed on the intercepted
    // dialog request. The API itself is a fixture, not a backend roundtrip.
    const response = IssueViewSchema.parse({ ...editView, ...request });
    const key = `view:dialog-${operation}`;
    clearIssueSurfaceViewState(key);
    seedIssueSurfaceViewState(key, { ...response.query, ...response.display });
    const reopened = getIssueSurfaceViewStore(key);
    expect(reopened.getState()).toMatchObject({
      swimlaneProjectAxis: "project-columns",
      swimlaneProjectColumnOrder: ["p2", "p1"],
      collapsedSwimlaneStatusRows: ["qa"],
    });
    reopened.getState().setSwimlaneProjectAxis("status-columns");
    seedIssueSurfaceViewState(key, { ...response.query, ...response.display });
    expect(reopened.getState().swimlaneProjectAxis).toBe("status-columns");
    clearIssueSurfaceViewState(key);
  });
});
