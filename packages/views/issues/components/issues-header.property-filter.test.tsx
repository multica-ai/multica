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
import type { Issue, IssueProperty, IssuePropertyValues, IssueTableFacetsResponse } from "@multica/core/types";
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

function propertyOf(id: string, name: string, type: IssueProperty["type"]): IssueProperty {
  return {
    id,
    workspace_id: "ws-1",
    name,
    type,
    config: {},
    position: 1,
    archived: false,
    created_at: "",
    updated_at: "",
  };
}

/** Minimal issue with only the fields the local facet-count path reads. */
function issueWithProperties(id: string, properties: IssuePropertyValues): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: "Test",
    description: null,
    status: "todo",
    priority: "medium",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "u-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    labels: [],
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
    properties,
  };
}

function renderFilterMenu(
  props: IssueProperty[],
  scopedIssues?: Issue[],
  tableFacetCounts?: IssueTableFacetsResponse,
) {
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
        <IssueFilterMenu
          trigger={<button type="button">Filter</button>}
          scopedIssues={scopedIssues}
          tableFacetCounts={tableFacetCounts}
        />
      </ViewStoreProvider>
    </QueryClientProvider>,
  );
  return { store, ...view };
}

async function openPropertySubmenu(name: string, inputRole: "textbox" | "spinbutton" = "textbox") {
  fireEvent.click(screen.getByRole("button", { name: "Filter" }));
  const trigger = await screen.findByRole("menuitem", { name: new RegExp(name) });
  fireEvent.click(trigger);
  // A number scalar renders <input type="number">, whose ARIA role is
  // spinbutton rather than textbox.
  await waitFor(() =>
    expect(screen.getByRole(inputRole)).toBeInTheDocument(),
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

  it("checking and unchecking No value preserves the committed value", async () => {
    // Regression (review round 2): unchecking "No value" used to drop the
    // committed value because the draft effect wiped it when the set changed.
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    const input = screen.getByRole("textbox");
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

    await userEvent.type(screen.getByRole("textbox"), "abc{Enter}");
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["abc", "__none__"] });
  });

  it("an uncommitted draft survives checking No value, then commits alongside it", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    await userEvent.type(screen.getByRole("textbox"), "abc");
    // Clicking the checkbox blurs the input first; the blur guard must skip the
    // premature commit so the checkbox reads the pre-blur state and the draft
    // stays uncommitted in the input.
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /No value/ }));
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["__none__"] });
    expect(screen.getByRole("textbox")).toHaveValue("abc");

    // Enter commits the draft as another member of the OR-set.
    await userEvent.type(screen.getByRole("textbox"), "{Enter}");
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["abc", "__none__"] });
  });

  it("lists observed scalar values as checkboxes with counts and toggles them", async () => {
    // The observed rows come from the local facet-count path over the loaded
    // issues; the server facet path feeds the same map with the same keys.
    const { store } = renderFilterMenu([textProperty(PROP, "Note")], [
      issueWithProperties("i-1", { [PROP]: "alpha" }),
      issueWithProperties("i-2", { [PROP]: "alpha" }),
      issueWithProperties("i-3", { [PROP]: "beta" }),
    ]);
    await openPropertySubmenu("Note");

    const alpha = screen.getByRole("menuitemcheckbox", { name: /alpha/ });
    expect(alpha).toHaveTextContent("2");
    expect(screen.getByRole("menuitemcheckbox", { name: /beta/ })).toHaveTextContent("1");

    // Toggling an observed value commits it as a bare equality member — the
    // same shape the free input produces — so the two paths compose.
    await userEvent.click(alpha);
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["alpha"] });
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /alpha/ }));
    expect(store.getState().propertyFilters).toEqual({});
  });

  it("lists observed number values under their canonical string key", async () => {
    const { store } = renderFilterMenu([propertyOf(PROP, "Estimate", "number")], [
      issueWithProperties("i-1", { [PROP]: 3.5 }),
      issueWithProperties("i-2", { [PROP]: 3.5 }),
    ]);
    await openPropertySubmenu("Estimate", "spinbutton");

    const threePointFive = screen.getByRole("menuitemcheckbox", { name: /3\.5/ });
    expect(threePointFive).toHaveTextContent("2");
    await userEvent.click(threePointFive);
    // The committed member is the equality string, which matches the stored
    // jsonb number on both the server and the client matcher.
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["3.5"] });
  });

  it("an unchanged blur or Enter commit does not drop checkbox-selected members", async () => {
    // Regression (adversarial review): the blur commit rewrote the whole set
    // from the draft, so checking "alpha" and "beta" and then merely clicking
    // the input and away silently dropped "beta".
    const { store } = renderFilterMenu([textProperty(PROP, "Note")], [
      issueWithProperties("i-1", { [PROP]: "alpha" }),
      issueWithProperties("i-2", { [PROP]: "alpha" }),
      issueWithProperties("i-3", { [PROP]: "beta" }),
    ]);
    await openPropertySubmenu("Note");

    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /alpha/ }));
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /beta/ }));
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["alpha", "beta"] });

    // The input shows the first member ("alpha"); blurring it unchanged and
    // pressing Enter must both be no-ops.
    fireEvent.blur(screen.getByRole("textbox"));
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["alpha", "beta"] });
    await userEvent.type(screen.getByRole("textbox"), "{Enter}");
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["alpha", "beta"] });
  });

  it("preserves other observed members when editing the input value", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")], [
      issueWithProperties("i-1", { [PROP]: "alpha" }),
      issueWithProperties("i-2", { [PROP]: "beta" }),
    ]);
    await openPropertySubmenu("Note");

    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /alpha/ }));
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /beta/ }));
    await userEvent.clear(screen.getByRole("textbox"));
    await userEvent.type(screen.getByRole("textbox"), "gamma");
    fireEvent.blur(screen.getByRole("textbox"));

    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["gamma", "beta"] });
  });

  it("keeps an uncommitted draft when an observed value is toggled", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")], [
      issueWithProperties("i-1", { [PROP]: "alpha" }),
    ]);
    await openPropertySubmenu("Note");

    await userEvent.type(screen.getByRole("textbox"), "custom");
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /alpha/ }));
    expect(screen.getByRole("textbox")).toHaveValue("custom");

    await userEvent.type(screen.getByRole("textbox"), "{Enter}");
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["custom", "alpha"] });
  });

  it("does not expose malformed values in the local scalar facet fallback", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")], [
      issueWithProperties("i-1", { [PROP]: "alpha" }),
      issueWithProperties("i-2", { [PROP]: 42 }),
      issueWithProperties("i-3", { [PROP]: ["array-value"] }),
    ]);
    await openPropertySubmenu("Note");

    expect(screen.getByRole("menuitemcheckbox", { name: /alpha/ })).toBeInTheDocument();
    expect(screen.queryByRole("menuitemcheckbox", { name: /42/ })).toBeNull();
    expect(screen.queryByRole("menuitemcheckbox", { name: /array-value/ })).toBeNull();
    expect(store.getState().propertyFilters).toEqual({});
  });

  it("counts missing scalar properties as No value in the local fallback", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")], [
      issueWithProperties("i-1", {}),
      issueWithProperties("i-2", { [PROP]: "alpha" }),
    ]);
    await openPropertySubmenu("Note");

    expect(screen.getByRole("menuitemcheckbox", { name: /No value/ })).toHaveTextContent("1");
    expect(screen.getByRole("menuitemcheckbox", { name: /alpha/ })).toHaveTextContent("1");
    expect(store.getState().propertyFilters).toEqual({});
  });

  it("caps local observed scalar values at the server's deterministic top 50", async () => {
    const scopedIssues = Array.from({ length: 55 }, (_, index) =>
      issueWithProperties(`i-${index}`, { [PROP]: `value-${String(index).padStart(2, "0")}` }),
    );
    renderFilterMenu([textProperty(PROP, "Note")], scopedIssues);
    await openPropertySubmenu("Note");

    // Fifty observed rows plus the No value row; equal-count ties are ordered
    // by key, so value-50 and above fall outside the server-compatible list.
    expect(screen.getAllByRole("menuitemcheckbox")).toHaveLength(51);
    expect(screen.getByRole("menuitemcheckbox", { name: /value-00/ })).toBeInTheDocument();
    expect(screen.queryByRole("menuitemcheckbox", { name: /value-50/ })).toBeNull();
  });

  it("renders observed values from server facets and ignores the legacy __set__ bucket", async () => {
    // A new menu against an OLD backend receives the pre-per-value shape: the
    // two-bucket "__set__"/"__none__" response. "__set__" must not surface as
    // a row, the per-value rows must render, and the "No value" count must
    // still come through.
    const { store } = renderFilterMenu([textProperty(PROP, "Note")], [], {
      facets: [
        {
          kind: "property",
          property_id: PROP,
          values: [
            { key: "__set__", count: 3 },
            { key: "alpha", count: 2 },
            { key: "__none__", count: 7 },
          ],
        },
      ],
    } as unknown as IssueTableFacetsResponse);
    await openPropertySubmenu("Note");

    expect(screen.getByRole("menuitemcheckbox", { name: /alpha/ })).toHaveTextContent("2");
    expect(screen.queryByRole("menuitemcheckbox", { name: /__set__/ })).toBeNull();
    expect(screen.getByRole("menuitemcheckbox", { name: /No value/ })).toHaveTextContent("7");

    // Toggling a server-facet value commits the same bare equality member.
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /alpha/ }));
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["alpha"] });
  });

  it("keeps a pending operator when an observed value is toggled before typing", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")], [
      issueWithProperties("i-1", { [PROP]: "alpha" }),
    ]);
    await openPropertySubmenu("Note");

    await userEvent.click(screen.getByRole("radio", { name: "contains" }));
    await userEvent.click(screen.getByRole("menuitemcheckbox", { name: /alpha/ }));
    await userEvent.type(screen.getByRole("textbox"), "custom{Enter}");

    expect(store.getState().propertyFilters).toEqual({
      [PROP]: [{ op: "contains", value: "custom" }, "alpha"],
    });
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
    await userEvent.type(screen.getByRole("textbox"), "hello{Enter}");

    expect(store.getState().propertyFilters).toEqual({
      [PROP]: [{ op: "contains", value: "hello" }],
    });
  });

  it("clicking an operator commits the current draft with it immediately", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    await userEvent.type(screen.getByRole("textbox"), "hello");
    await userEvent.click(screen.getByRole("radio", { name: "contains" }));

    expect(store.getState().propertyFilters).toEqual({
      [PROP]: [{ op: "contains", value: "hello" }],
    });
  });

  it("switching back to is commits a bare string again", async () => {
    const { store } = renderFilterMenu([textProperty(PROP, "Note")]);
    await openPropertySubmenu("Note");

    await userEvent.type(screen.getByRole("textbox"), "hello");
    await userEvent.click(screen.getByRole("radio", { name: "contains" }));
    expect(store.getState().propertyFilters).toEqual({
      [PROP]: [{ op: "contains", value: "hello" }],
    });

    await userEvent.click(screen.getByRole("radio", { name: "is" }));
    expect(store.getState().propertyFilters).toEqual({ [PROP]: ["hello"] });
  });
});
