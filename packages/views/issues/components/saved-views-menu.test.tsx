// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createStore } from "zustand/vanilla";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import {
  viewStoreSlice,
  type IssueViewState,
} from "@multica/core/issues/stores/view-store";
import { useSavedViewsStore } from "@multica/core/issues/stores/saved-views-store";
import { SavedViewsMenu } from "./saved-views-menu";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function makeViewStore() {
  return createStore<IssueViewState>()(viewStoreSlice);
}

function renderMenu(store = makeViewStore()) {
  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ViewStoreProvider store={store}>
        <SavedViewsMenu />
      </ViewStoreProvider>
    </I18nProvider>,
  );
  return { ...utils, store };
}

beforeEach(() => {
  localStorage.clear();
  useSavedViewsStore.setState({ byWorkspace: {} });
});

function openMenu() {
  fireEvent.click(screen.getByRole("button", { name: "Saved views" }));
}

async function openViewActions(name: string, itemName: string) {
  const user = userEvent.setup();
  // Base UI submenus open on hover (with a 100ms delay). userEvent.hover
  // dispatches the pointer events Base UI listens for; fireEvent.mouseEnter
  // does not, so hover must go through userEvent.
  await user.hover(screen.getByText(name));
  await waitFor(() => {
    expect(screen.getByRole("menuitem", { name: itemName })).toBeInTheDocument();
  });
}

describe("SavedViewsMenu", () => {
  it("renders the trigger and shows the empty state", () => {
    renderMenu();

    openMenu();

    expect(
      screen.getByText(
        "No saved views yet. Save the current filters and layout as a reusable view.",
      ),
    ).toBeInTheDocument();
  });

  it("saves the current view state under a name", () => {
    const { store } = renderMenu();
    store.setState({
      viewMode: "table",
      statusFilters: ["in_progress"],
      sortBy: "due_date",
    });

    openMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: /Save current view/ }));
    fireEvent.change(screen.getByPlaceholderText("View name"), {
      target: { value: "My queue" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(screen.getByText("My queue")).toBeInTheDocument();

    const saved = useSavedViewsStore.getState().byWorkspace["ws-1"] ?? [];
    expect(saved).toHaveLength(1);
    expect(saved[0]?.name).toBe("My queue");
    expect(saved[0]?.state.viewMode).toBe("table");
    expect(saved[0]?.state.statusFilters).toEqual(["in_progress"]);
    expect(saved[0]?.state.sortBy).toBe("due_date");
  });

  it("applies a saved view back onto the view store", async () => {
    const seeded = makeViewStore();
    seeded.setState({ viewMode: "swimlane", swimlaneGrouping: "parent" });
    useSavedViewsStore
      .getState()
      .saveView("ws-1", seeded.getState(), "My lanes");

    const viewStore = makeViewStore();
    renderMenu(viewStore);

    openMenu();
    await openViewActions("My lanes", "Apply");
    fireEvent.click(screen.getByRole("menuitem", { name: "Apply" }));

    expect(viewStore.getState().viewMode).toBe("swimlane");
    expect(viewStore.getState().swimlaneGrouping).toBe("parent");
  });

  it("renames a saved view", async () => {
    useSavedViewsStore
      .getState()
      .saveView("ws-1", makeViewStore().getState(), "Old name");
    renderMenu();

    openMenu();
    await openViewActions("Old name", "Rename");
    fireEvent.click(screen.getByRole("menuitem", { name: "Rename" }));

    const input = screen.getByPlaceholderText("View name");
    fireEvent.change(input, { target: { value: "New name" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    const saved = useSavedViewsStore.getState().byWorkspace["ws-1"] ?? [];
    expect(saved[0]?.name).toBe("New name");
    expect(screen.getByText("New name")).toBeInTheDocument();
  });

  it("deletes a saved view", async () => {
    useSavedViewsStore
      .getState()
      .saveView("ws-1", makeViewStore().getState(), "Doomed");
    renderMenu();

    openMenu();
    await openViewActions("Doomed", "Delete");
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));

    expect(useSavedViewsStore.getState().byWorkspace["ws-1"] ?? []).toHaveLength(
      0,
    );
    expect(screen.queryByText("Doomed")).not.toBeInTheDocument();
  });
});
