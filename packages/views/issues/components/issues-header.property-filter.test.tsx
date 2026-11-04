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

function renderFilterMenu(props: IssueProperty[], propertyFilters: IssueViewState["propertyFilters"] = {}) {
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
  store.setState({ propertyFilters });

  const view = renderWithI18n(
    <QueryClientProvider client={qc}>
      <ViewStoreProvider store={store}>
        <IssueFilterMenu trigger={<button type="button">Filter</button>} />
      </ViewStoreProvider>
    </QueryClientProvider>,
  );
  return { store, ...view };
}

/** The submenu's value input, not the menu's own search box. */
function valueInput() {
  const [input] = screen
    .getAllByRole("textbox")
    .filter((element) => !element.closest("[data-filter-search]"));
  if (!input) throw new Error("value input not found");
  return input;
}

async function openPropertySubmenu(name: string) {
  fireEvent.click(screen.getByRole("button", { name: "Filter" }));
  const trigger = await screen.findByRole("menuitem", { name: new RegExp(name) });
  fireEvent.click(trigger);
  await waitFor(() =>
    expect(valueInput()).toBeInTheDocument(),
  );
}

afterEach(() => {
  cleanup();
  // Base UI portals the menu onto document.body; leftovers would duplicate
  // labels across tests.
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("IssueFilterMenu select property filter", () => {
  it.each(["select", "multi_select"])("caps %s filters at 50 including No value and permits replacement", async (type) => {
    const property = {
      ...textProperty("prop-services", "Services"), type,
      config: {
        options: Array.from({ length: 51 }, (_, i) => ({
          id: `option-${i + 1}`, name: `Service ${i + 1}`, color: "#3b82f6",
        })),
      },
    };
    const selected = property.config.options.slice(0, 50).map((option) => option.id);
    const { store } = renderFilterMenu([property], { [property.id]: selected });
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Services/ }));
    const extra = await screen.findByRole("menuitemcheckbox", { name: "Service 51" });
    const noValue = screen.getByRole("menuitemcheckbox", { name: "No value" });
    expect(extra).toHaveAttribute("aria-disabled", "true");
    expect(noValue).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(extra);
    fireEvent.click(noValue);
    expect(store.getState().propertyFilters[property.id]).toEqual(selected);

    const lastSelected = screen.getByRole("menuitemcheckbox", { name: "Service 50" });
    expect(lastSelected).not.toHaveAttribute("aria-disabled", "true");
    fireEvent.click(lastSelected);
    expect(noValue).not.toHaveAttribute("aria-disabled", "true");
    fireEvent.click(noValue);
    expect(store.getState().propertyFilters[property.id]).toEqual([...selected.slice(0, 49), "__none__"]);
    expect(extra).toHaveAttribute("aria-disabled", "true");
    expect(noValue).not.toHaveAttribute("aria-disabled", "true");

    fireEvent.click(noValue);
    expect(extra).not.toHaveAttribute("aria-disabled", "true");
    fireEvent.click(extra);
    expect(store.getState().propertyFilters[property.id]).toEqual([...selected.slice(0, 49), "option-51"]);
    expect(lastSelected).toHaveAttribute("aria-disabled", "true");
  });
});

describe("IssueFilterMenu scalar property filter", () => {
  const PROP = "prop-note";

  it("types into the scalar value input", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    const input = valueInput();
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

    await userEvent.type(valueInput(), "hello{Enter}");

    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["hello"] });
  });

  it("commits the value when focus blurs to a non-menu element", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    const input = valueInput();
    await userEvent.type(input, "hello");
    fireEvent.blur(input);

    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["hello"] });
  });

  it("checking and unchecking No value preserves the committed value", async () => {
    // Regression (review round 2): unchecking "No value" used to drop the
    // committed value because the draft effect wiped it when the set changed.
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    const input = valueInput();
    await userEvent.type(input, "hello{Enter}");
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["hello"] });

    // "No value" composes OR-style with the value, like every other type.
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /No value/ }));
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["hello", "__none__"] });

    // Unchecking removes only the membership — the committed value survives
    // without any draft round-trip.
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /No value/ }));
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["hello"] });
  });

  it("committing a value preserves an existing No-value membership", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /No value/ }));
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["__none__"] });

    await userEvent.type(valueInput(), "abc{Enter}");
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["abc", "__none__"] });
  });

  it("an uncommitted draft survives checking No value, then commits alongside it", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    await userEvent.type(valueInput(), "abc");
    // Clicking the checkbox blurs the input first; the blur guard must skip the
    // premature commit so the checkbox reads the pre-blur state and the draft
    // stays uncommitted in the input.
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /No value/ }));
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["__none__"] });
    expect(valueInput()).toHaveValue("abc");

    // Enter commits the draft as another member of the OR-set.
    await userEvent.type(valueInput(), "{Enter}");
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["abc", "__none__"] });
  });

  it("picking contains with an empty draft, then typing commits an operator object", async () => {
    // Regression: killing the pending operator on click downgraded the next
    // commit back to equality — the pick silently did nothing.
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    expect(screen.getByRole("radiogroup", { name: "Filter operator" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "is" })).toBeChecked();
    const contains = screen.getByRole("radio", { name: "contains" });
    await userEvent.click(contains);
    expect(contains).toBeChecked();
    await userEvent.type(valueInput(), "hello{Enter}");

    expect(store.getState().propertyFilters).toEqual({
      [PROP]: [{ op: "contains", value: "hello" }],
    });
  });

  it("clicking an operator commits the current draft with it immediately", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    await userEvent.type(valueInput(), "hello");
    await userEvent.click(screen.getByRole("radio", { name: "contains" }));

    expect(store.getState().propertyFilters).toEqual({
      [PROP]: [{ op: "contains", value: "hello" }],
    });
  });

  it("switching back to is commits a bare string again", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    await userEvent.type(valueInput(), "hello");
    await userEvent.click(screen.getByRole("radio", { name: "contains" }));
    expect(store.getState().propertyFilters).toEqual({
      [PROP]: [{ op: "contains", value: "hello" }],
    });

    await userEvent.click(screen.getByRole("radio", { name: "is" }));
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["hello"] });
  });
});
