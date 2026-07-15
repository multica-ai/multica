// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RunsToolbar } from "./runs-toolbar";

afterEach(cleanup);

describe("RunsToolbar", () => {
  it("matches the mockup controls and applies a shared filter", () => {
    const onAddFilter = vi.fn();
    const onCustomize = vi.fn();
    const onNewVisual = vi.fn();
    const onClear = vi.fn();
    const onRangeChange = vi.fn();
    render(
      <RunsToolbar
        range="30d"
        onRangeChange={onRangeChange}
        filters={[{ dimension: "person", operator: "in", values: ["Lone"] }]}
        onAddFilter={onAddFilter}
        onRemoveFilter={vi.fn()}
        onClear={onClear}
        onCustomize={onCustomize}
        onNewVisual={onNewVisual}
      />,
    );

    expect(screen.getByText("Last 30 days")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Time range"), { target: { value: "7d" } });
    expect(onRangeChange).toHaveBeenCalledWith("7d");
    expect(screen.getByRole("button", { name: "Person: Lone ×" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Customize layout" }));
    expect(onCustomize).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: "New visual" }));
    expect(onNewVisual).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: "Clear all" }));
    expect(onClear).toHaveBeenCalledOnce();

    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    fireEvent.change(screen.getByLabelText("Filter dimension"), { target: { value: "provider" } });
    fireEvent.change(screen.getByLabelText("Filter operator"), { target: { value: "not_in" } });
    fireEvent.change(screen.getByLabelText("Filter value"), { target: { value: "Anthropic" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply filter" }));
    expect(onAddFilter).toHaveBeenCalledWith("provider", "Anthropic", "not_in");
  });

  it("uses the approved mockup accent colors for actions and filter chips", () => {
    render(
      <RunsToolbar
        range="30d"
        onRangeChange={vi.fn()}
        filters={[
          { dimension: "person", operator: "in", values: ["Lone"] },
          { dimension: "status", operator: "not_in", values: ["Cancelled"] },
        ]}
        onAddFilter={vi.fn()}
        onRemoveFilter={vi.fn()}
        onClear={vi.fn()}
        onCustomize={vi.fn()}
        onNewVisual={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Add filter" }).className).toContain("bg-[#6557d8]");
    expect(screen.getByRole("button", { name: "Person: Lone ×" }).className).toContain("border-[rgba(101,87,216,0.35)]");
    expect(screen.getByRole("button", { name: "Person: Lone ×" }).className).toContain("bg-[rgba(101,87,216,0.10)]");
    expect(screen.getByRole("button", { name: "Person: Lone ×" }).className).not.toContain("bg-[#6557d8]");
    expect(screen.getByRole("button", { name: "Status: Cancelled ×" }).className).toContain("border-amber-400");
  });
});
