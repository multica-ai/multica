import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { toast } from "sonner";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { createTransientIssueViewStore } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import type { IssueTableFilters } from "@multica/core/types";
import { renderWithI18n } from "../../../../test/i18n";
import { GraphFilters } from "./graph-filters";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
afterEach(() => { cleanup(); document.body.innerHTML = ""; vi.clearAllMocks(); });

type Props = Partial<Parameters<typeof GraphFilters>[0]>;
function setup(props: Props = {}) {
  setApiInstance({
    listIssueStatuses: async () => ({ statuses: [], categories: [], total: 0 }),
    listProperties: async () => ({ properties: [] }),
  } as unknown as ApiClient);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  // The list's own store: the trajectory menu must never write to it.
  const native = createTransientIssueViewStore();
  native.setState({ statusFilters: ["in_review"] });
  const onChange = vi.fn();
  const filters = props.filters ?? {}, initial = props.initial ?? filters;
  const ui = (next: Props) => <QueryClientProvider client={client}><ViewStoreProvider store={native}>
    <GraphFilters filters={filters} initial={initial} onChange={onChange} {...next} />
  </ViewStoreProvider></QueryClientProvider>;
  const view = renderWithI18n(ui(props));
  return { native, onChange, rerender: (next: Props) => view.rerender(ui({ ...props, ...next })) };
}
const search = () => screen.findByRole("textbox", { name: "Filter by field or value..." });
const openMenu = async (name: string | RegExp = "Filter") => {
  fireEvent.click(screen.getByRole("button", { name }));
  return search();
};
const pick = async (section: RegExp, value: string) => {
  fireEvent.click(await screen.findByRole("menuitem", { name: section }));
  fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: value }));
};

it("edits with the issues page menu, applies each change at once, and leaves the list's filters alone", async () => {
  const filters = { statuses: ["in_review"] } as IssueTableFilters;
  const { native, onChange } = setup({ filters });
  await openMenu("1 filter");
  await pick(/^Priority/, "Urgent");
  await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({ statuses: ["in_review"], priorities: ["urgent"] }));
  expect(filters).toEqual({ statuses: ["in_review"] });
  expect(native.getState().statusFilters).toEqual(["in_review"]);
  expect(native.getState().priorityFilters).toEqual([]);
});

it("sends inherited conditions back unchanged until edited, and restores them from the menu", async () => {
  const initial: IssueTableFilters = { working_issue_ids: ["i1"], properties: { unknown: [{ op: "contains", value: "keep" }] } };
  const { onChange, rerender } = setup({ filters: initial, initial });
  await openMenu(/filters?$/);
  expect(screen.queryByRole("menuitem", { name: "Restore list filters" })).not.toBeInTheDocument();
  expect(onChange).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Show sub-issues" }));
  await waitFor(() => expect(onChange).toHaveBeenCalledTimes(1));
  // Running work goes back as the server-resolved flag; an unknown property survives.
  const edited = onChange.mock.calls[0]![0] as IssueTableFilters;
  expect(edited).toEqual({ working_only: true, properties: initial.properties, include_sub_issues: false });
  rerender({ filters: edited });
  fireEvent.click(await screen.findByRole("menuitem", { name: "Restore list filters" }));
  expect(onChange).toHaveBeenLastCalledWith(initial);
});

it("opens the shared advanced panel for an inherited condition tree", async () => {
  const filters: IssueTableFilters = { advanced: { match: "any", children: [{ field: "priorities", values: ["urgent"] }, { field: "priorities", values: ["high"] }] } };
  const { onChange } = setup({ filters });
  fireEvent.click(screen.getByRole("button", { name: /filters?$/ }));
  expect(await screen.findByTestId("advanced-filter-panel")).toBeInTheDocument();
  expect(onChange).not.toHaveBeenCalled();
});

it("clears with the menu's own reset", async () => {
  const filters = { priorities: ["high"] } as IssueTableFilters;
  const { onChange } = setup({ filters, initial: {} });
  await openMenu("1 filter");
  fireEvent.click(screen.getByRole("menuitem", { name: "Reset all filters" }));
  await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({}));
});

it("applies unread once the viewer's membership loads, and reverts with a notice when it fails", async () => {
  let settle!: { resolve: (ids: string[]) => void; reject: (error: Error) => void };
  const loadUnread = vi.fn(() => new Promise<string[]>((resolve, reject) => { settle = { resolve, reject }; }));
  const { onChange } = setup({ loadUnread });
  await openMenu();
  await pick(/^Read state/, "Unread");
  expect(loadUnread).toHaveBeenCalledOnce();
  expect(onChange).not.toHaveBeenCalled();
  await act(async () => settle.resolve(["i2", "i2"]));
  expect(onChange).toHaveBeenLastCalledWith({ unread_issue_ids: ["i2"] });
  fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Unread" }));
  await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({}));
  fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Unread" }));
  await act(async () => settle.reject(new Error("synthetic")));
  expect(toast.error).toHaveBeenCalledWith("Couldn't load your unread issues");
  await waitFor(() => expect(screen.getByRole("menuitemcheckbox", { name: "Unread" })).toHaveAttribute("aria-checked", "false"));
  expect(onChange).toHaveBeenCalledTimes(2);
});

it("cancels a pending unread read on unmount", async () => {
  const cancelUnread = vi.fn();
  setup({ loadUnread: () => new Promise(() => {}), cancelUnread });
  await openMenu();
  await pick(/^Read state/, "Unread");
  cleanup();
  expect(cancelUnread).toHaveBeenCalled();
});

it("opens on request from the view", async () => {
  const { rerender } = setup({ openRequest: 0 });
  expect(screen.queryByRole("textbox", { name: "Filter by field or value..." })).not.toBeInTheDocument();
  rerender({ openRequest: 1 });
  expect(await search()).toBeInTheDocument();
});
