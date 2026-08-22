// @vitest-environment jsdom

/**
 * The scalar custom-property filter (text / number / date / url), driven
 * against the REAL Base UI menu primitives — not a flattened mock, because the
 * menu popup's typeahead/list-navigation handlers are exactly what break the
 * value input (they stopEvent() every printable key). See
 * issues-header.status-filter.test.tsx for the same rationale on the status
 * section.
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createStore } from "zustand/vanilla";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import {
  type IssueViewState,
  viewStoreSlice,
} from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import type { IssueProperty } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueFilterMenu } from "./issues-header";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function textProperty(id: string, name: string): IssueProperty {
  return {
    id,
    workspace_id: "ws-1",
    name,
    type: "text",
    config: {},
    position: 1,
    archived: false,
    created_at: "",
    updated_at: "",
  };
}

function renderFilterMenu(props: IssueProperty[]) {
  setApiInstance({
    listIssueStatuses: async () => ({ statuses: [], categories: [], total: 0 }),
    listProperties: async () => ({ properties: props }),
  } as unknown as ApiClient);
  // PropertyFilterOptions reads the signed-in member via useAuthStore to sort
  // actor options; register a minimal store so the menu renders for scalar
  // types too.
  registerAuthStore(
    createAuthStore({
      api: {} as ApiClient,
      storage: {
        getItem: () => null,
        setItem: () => {},
        removeItem: () => {},
      },
    }),
  );

  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const store = createStore<IssueViewState>()(viewStoreSlice);

  const view = renderWithI18n(
    <QueryClientProvider client={qc}>
      <ViewStoreProvider store={store}>
        <IssueFilterMenu trigger={<button type="button">Filter</button>} />
      </ViewStoreProvider>
    </QueryClientProvider>,
  );
  return { store, ...view };
}

async function openPropertySubmenu(name: string) {
  fireEvent.click(screen.getByRole("button", { name: "Filter" }));
  const trigger = await screen.findByRole("menuitem", { name: new RegExp(name) });
  fireEvent.click(trigger);
  await waitFor(() =>
    expect(screen.getByRole("textbox")).toBeInTheDocument(),
  );
}

afterEach(() => {
  cleanup();
  // Base UI portals the menu onto document.body; leftovers would duplicate
  // labels across tests.
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("IssueFilterMenu scalar property filter", () => {
  const PROP = "prop-note";

  it("types into the scalar value input", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    const input = screen.getByRole("textbox");
    await userEvent.type(input, "hello");

    // The regression: Base UI's menu popup stops every printable key, so the
    // input swallowed characters — value stayed "".
    expect(input).toHaveValue("hello");
    // Typing alone must not commit; the filter changes only on Enter/blur.
    expect(store.getState().propertyFilters).toEqual({});
  });

  it("commits the value on Enter", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    await userEvent.type(screen.getByRole("textbox"), "hello{Enter}");

    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["hello"] });
  });

  it("commits the value when focus blurs to a non-menu element", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    const input = screen.getByRole("textbox");
    await userEvent.type(input, "hello");
    fireEvent.blur(input);

    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["hello"] });
  });

  it("checking No value clears a committed value; unchecking restores the draft", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    const input = screen.getByRole("textbox");
    await userEvent.type(input, "hello{Enter}");
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["hello"] });

    // Checking "No value" clears the value (mutual exclusion).
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /No value/ }));
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["__none__"] });

    // Re-type and uncheck: the value still in the input is committed.
    await userEvent.type(screen.getByRole("textbox"), "world");
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /No value/ }));
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["world"] });
  });

  it("unchecking No value restores a typed (uncommitted) value", async () => {
    // Seed the filter with No value checked, then type before unchecking.
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    store.getState().setPropertyFilterValues(PROP, ["__none__"]);
    await openPropertySubmenu("Note");

    await userEvent.type(screen.getByRole("textbox"), "abc");
    // Clicking the checkbox blurs the input first; the blur guard must skip the
    // premature commit so the checkbox reads the pre-blur state and the draft
    // survives for the uncheck.
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /No value/ }));

    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["abc"] });
  });
});
