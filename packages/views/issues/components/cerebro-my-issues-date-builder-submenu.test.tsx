// CEREBRO-PATCH(my-issues-date-builder): regression test for FIR-1658 / FIR-1799.
// The stacked date builder (My Issues path, onDateFiltersChange set) rendered the
// empty-state "No date filters yet" label as a bare Base UI Menu.GroupLabel with
// no enclosing Menu.Group. Base UI throws error #31 ("MenuGroupContext is
// missing") on open, which white-screened the whole My Issues page on Sara the
// moment the Date submenu mounted (dateFilters is never persisted, so the list is
// always empty on load -> every user hit it). This test opens the Date submenu in
// the builder (empty) configuration and asserts it renders without crashing.
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/cerebro-saved-filters/views", () => ({
  SavedFiltersMenuSection: () => null,
  SaveFilterDialogHost: () => null,
}));

import { createIssueViewStore } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { IssueDisplayControls } from "./issues-header";

function renderBuilderControls(
  dateFilters: Parameters<typeof IssueDisplayControls>[0]["dateFilters"] = [],
) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  const store = createIssueViewStore("test-date-builder");
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <ViewStoreProvider store={store}>
          <IssueDisplayControls
            scopedIssues={[]}
            dateFilters={dateFilters}
            onDateFiltersChange={() => {}}
          />
        </ViewStoreProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("my-issues stacked date builder submenu (FIR-1658 / FIR-1799)", () => {
  beforeEach(() => {
    vi.spyOn(console, "error").mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("opens the empty Date builder submenu without crashing", async () => {
    renderBuilderControls();

    fireEvent.click(await screen.findByText("Filter"));
    fireEvent.click(await screen.findByText("Date"));

    // Before the fix the bare GroupLabel here threw Base UI error #31 on render,
    // taking down the whole page. The empty-state copy and the add affordance
    // must render instead.
    expect(await screen.findByText("No date filters yet")).toBeInTheDocument();
    expect(screen.getByText("Add date filter")).toBeInTheDocument();
  });

  it("opens the Date builder submenu with an existing condition without crashing", async () => {
    const { container } = renderBuilderControls([
      { field: "created_at", from: "2026-06-15", to: "2026-06-21", preset: "last_7_days" },
    ]);

    // With an active filter the trigger label switches to the count, so click the
    // trigger by its slot rather than by visible text.
    const trigger = container.querySelector('[data-slot="dropdown-menu-trigger"]');
    fireEvent.click(trigger as Element);
    fireEvent.click(await screen.findByText("Date"));

    // The condition row (field label) and the add/clear affordances must render;
    // the empty-state copy must not.
    expect(await screen.findByText("Add date filter")).toBeInTheDocument();
    expect(screen.getByText("Clear all dates")).toBeInTheDocument();
    expect(screen.queryByText("No date filters yet")).not.toBeInTheDocument();
  });
});
