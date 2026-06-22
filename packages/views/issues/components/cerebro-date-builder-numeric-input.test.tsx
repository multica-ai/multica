// CEREBRO-PATCH(my-issues-date-builder): regression test for FIR-1871.
// The relative "Next/Last N <unit>" amount used a native <input type="number">,
// which on iOS Safari surfaced the alphabetic keyboard. The field is now a text
// input with inputMode="numeric" (numeric keypad on mobile) and SOFT validation:
// it accepts free typing without stripping characters, marks itself red via
// aria-invalid when the value is not a positive integer, and only commits a
// valid amount to the filter. This test renders the V2 builder with a relative
// condition and asserts (1) the amount input requests the numeric keypad,
// (2) invalid input turns the field red and is not committed, and (3) a valid
// integer commits.
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

import { createIssueViewStore, type IssueDateFilter } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { useCerebroFeatureFlagsStore } from "@multica/cerebro-feature-flags";
import { IssueDisplayControls, IssuesHeader } from "./issues-header";

function renderBuilderControls(
  dateFilters: IssueDateFilter[],
  onDateFiltersChange: (next: IssueDateFilter[]) => void,
) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  const store = createIssueViewStore("test-numeric-input");
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <ViewStoreProvider store={store}>
          <IssueDisplayControls
            scopedIssues={[]}
            dateFilters={dateFilters}
            onDateFiltersChange={onDateFiltersChange}
          />
        </ViewStoreProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

function renderHeaderWithDateBuilder(
  dateFilters: IssueDateFilter[],
  onDateFiltersChange: (next: IssueDateFilter[]) => void,
) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  const store = createIssueViewStore("test-header-date-builder");
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <ViewStoreProvider store={store}>
          <IssuesHeader
            scopedIssues={[]}
            dateFilters={dateFilters}
            onDateFiltersChange={onDateFiltersChange}
          />
        </ViewStoreProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("date builder relative amount input (FIR-1871)", () => {
  beforeEach(() => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    useCerebroFeatureFlagsStore.getState().setFlag("cerebro_date_filter_v2", true);
  });
  afterEach(() => {
    vi.restoreAllMocks();
    useCerebroFeatureFlagsStore.getState().setFlag("cerebro_date_filter_v2", undefined);
  });

  const relativeCondition: IssueDateFilter = {
    field: "due_date",
    from: "2026-06-22",
    to: "2026-06-23",
    preset: "custom",
    relative: { direction: "next", amount: 1, unit: "day" },
  };

  it("requests the numeric keypad on mobile (no native number input)", async () => {
    const onChange = vi.fn();
    const { container } = renderBuilderControls([relativeCondition], onChange);

    const trigger = container.querySelector('[data-slot="dropdown-menu-trigger"]');
    fireEvent.click(trigger as Element);
    fireEvent.click(await screen.findByText("Date"));

    const input = (await screen.findByDisplayValue("1")) as HTMLInputElement;
    expect(input.getAttribute("type")).toBe("text");
    expect(input.getAttribute("inputmode")).toBe("numeric");
  });

  it("does not hard-strip: invalid input stays in the field, turns it red, and is not committed", async () => {
    const onChange = vi.fn();
    const { container } = renderBuilderControls([relativeCondition], onChange);

    const trigger = container.querySelector('[data-slot="dropdown-menu-trigger"]');
    fireEvent.click(trigger as Element);
    fireEvent.click(await screen.findByText("Date"));

    const input = (await screen.findByDisplayValue("1")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "1a4" } });

    // The raw text is preserved (no live stripping), the field is marked red,
    // and nothing is committed because the value is not a valid positive integer.
    expect(input.value).toBe("1a4");
    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("commits a valid positive integer and clears the red state", async () => {
    const onChange = vi.fn();
    const { container } = renderBuilderControls([relativeCondition], onChange);

    const trigger = container.querySelector('[data-slot="dropdown-menu-trigger"]');
    fireEvent.click(trigger as Element);
    fireEvent.click(await screen.findByText("Date"));

    const input = (await screen.findByDisplayValue("1")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "14" } });

    expect(input.getAttribute("aria-invalid")).toBe("false");
    expect(onChange).toHaveBeenCalled();
    const committed = onChange.mock.calls.at(-1)![0] as IssueDateFilter[];
    expect(committed[0]?.relative?.amount).toBe(14);
  });

  it("lets shared IssuesHeader surfaces use the same date builder as My Issues", async () => {
    const onChange = vi.fn();
    renderHeaderWithDateBuilder([], onChange);

    fireEvent.click(await screen.findByText("Filter"));
    fireEvent.click(await screen.findByText("Date"));

    expect(await screen.findByText("No date filters yet")).toBeInTheDocument();
    expect(screen.getByText("Add filter")).toBeInTheDocument();
  });
});
