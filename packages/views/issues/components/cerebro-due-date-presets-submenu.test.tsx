// CEREBRO-PATCH(my-issues-due-date-presets): regression test for FIR-1658.
// The due-date presets section renders a Base UI Menu.GroupLabel; if that label
// is not wrapped in a Menu.Group, Base UI throws error #31 ("MenuGroupContext is
// missing") on open, which white-screened the whole My Issues page on staging.
// This test opens the Date submenu with dueDatePresets enabled and asserts the
// three presets render without crashing.
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

// The saved-filters section makes its own API-backed queries that are irrelevant
// to this regression; stub it so the filter menu mounts cleanly in jsdom.
vi.mock("@multica/cerebro-saved-filters/views", () => ({
  SavedFiltersMenuSection: () => null,
  SaveFilterDialogHost: () => null,
}));

import { createIssueViewStore } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { IssueDisplayControls } from "./issues-header";

function renderControls() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  const store = createIssueViewStore("test-due-date-presets");
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <ViewStoreProvider store={store}>
          <IssueDisplayControls
            scopedIssues={[]}
            dateFilter={null}
            onDateFilterChange={() => {}}
            dueDatePresets
          />
        </ViewStoreProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("due-date presets date submenu (FIR-1658)", () => {
  beforeEach(() => {
    vi.spyOn(console, "error").mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("opens the Date submenu with due-date presets without crashing", async () => {
    renderControls();

    fireEvent.click(await screen.findByText("Filter"));
    fireEvent.click(await screen.findByText("Date"));

    // These three only render when dueDatePresets is on; before the fix the
    // bare GroupLabel above them threw Base UI error #31 on render.
    expect(await screen.findByText("Overdue")).toBeInTheDocument();
    expect(screen.getByText("This week")).toBeInTheDocument();
    expect(screen.getByText("No due date")).toBeInTheDocument();
  });
});
