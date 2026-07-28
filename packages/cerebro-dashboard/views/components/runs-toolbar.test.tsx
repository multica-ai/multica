// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AnalyticsFilter } from "../../core/analytics";
import { RunsToolbar } from "./runs-toolbar";

vi.mock("@multica/cerebro-usage", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/cerebro-usage")>();
  return {
    ...actual,
    analyticsQueryOptions: (workspaceId: string, query: unknown) => ({
      queryKey: ["analytics", workspaceId, "query", query] as const,
      queryFn: () => ({
        columns: ["provider", "runs"],
        rows: [
          { provider: "Anthropic", runs: 7 },
          { provider: null, runs: 2 },
        ],
      }),
    }),
  };
});

afterEach(cleanup);

function renderToolbar(overrides: Partial<React.ComponentProps<typeof RunsToolbar>> = {}) {
  const props: React.ComponentProps<typeof RunsToolbar> = {
    workspaceId: "ws-1",
    range: "30d",
    onRangeChange: vi.fn(),
    filters: [] as AnalyticsFilter[],
    onAddFilter: vi.fn(),
    onRemoveFilter: vi.fn(),
    onClear: vi.fn(),
    onCustomize: vi.fn(),
    onNewVisual: vi.fn(),
    ...overrides,
  };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <RunsToolbar {...props} />
    </QueryClientProvider>,
  );
  return props;
}

describe("RunsToolbar", () => {
  it("matches the mockup controls and applies a picked filter value", async () => {
    const props = renderToolbar({ filters: [{ dimension: "person", operator: "in", values: ["Lone"] }] });

    expect(screen.getByText("Last 30 days")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Time range"), { target: { value: "7d" } });
    expect(props.onRangeChange).toHaveBeenCalledWith("7d");
    expect(screen.getByRole("button", { name: "Member is Lone ×" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Customize layout" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Dashboard actions" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Customize layout" }));
    expect(props.onCustomize).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: "Dashboard actions" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "New visual" }));
    expect(props.onNewVisual).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: "Clear all" }));
    expect(props.onClear).toHaveBeenCalledOnce();

    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    fireEvent.change(screen.getByLabelText("Filter dimension"), { target: { value: "provider" } });
    fireEvent.change(screen.getByLabelText("Filter operator"), { target: { value: "not_in" } });
    await waitFor(() => expect(screen.getByText("Anthropic · 7")).toBeTruthy());
    fireEvent.change(screen.getByLabelText("Filter value"), { target: { value: "Anthropic" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply filter" }));
    expect(props.onAddFilter).toHaveBeenCalledWith("provider", "Anthropic", "not_in");
  });

  it("offers the unset value as a pickable option with an app-language label", async () => {
    const props = renderToolbar();

    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    fireEvent.change(screen.getByLabelText("Filter dimension"), { target: { value: "provider" } });
    await waitFor(() => expect(screen.getByText("No provider")).toBeTruthy());
    fireEvent.change(screen.getByLabelText("Filter value"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply filter" }));
    expect(props.onAddFilter).toHaveBeenCalledWith("provider", "", "in");
  });

  it("uses a free-text field for contains matching", () => {
    const props = renderToolbar();

    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    fireEvent.change(screen.getByLabelText("Filter dimension"), { target: { value: "model" } });
    fireEvent.change(screen.getByLabelText("Filter operator"), { target: { value: "contains" } });
    fireEvent.change(screen.getByLabelText("Filter value"), { target: { value: "gpt" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply filter" }));
    expect(props.onAddFilter).toHaveBeenCalledWith("model", "gpt", "contains");
  });

  it("uses the approved mockup accent colors for actions and filter chips", () => {
    renderToolbar({
      filters: [
        { dimension: "person", operator: "in", values: ["Lone"] },
        { dimension: "status", operator: "not_in", values: ["Cancelled"] },
      ],
    });

    expect(screen.getByRole("button", { name: "Add filter" }).className).toContain("bg-[#6557d8]");
    expect(screen.getByRole("button", { name: "Member is Lone ×" }).className).toContain("border-[rgba(101,87,216,0.35)]");
    expect(screen.getByRole("button", { name: "Member is Lone ×" }).className).toContain("bg-[rgba(101,87,216,0.10)]");
    expect(screen.getByRole("button", { name: "Member is Lone ×" }).className).not.toContain("bg-[#6557d8]");
    expect(screen.getByRole("button", { name: "Status is not Cancelled ×" }).className).toContain("border-amber-400");
  });
});
