import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { DateOnlyPicker } from "./date-only-picker";

describe("date picker progressive disclosure", () => {
  it("keeps the calendar mounted when its one-shot defaultOpen flag clears", async () => {
    const props = { value: null, onChange: vi.fn(), icon: <span />, placeholder: "Start date", emptyLabel: "No date" };
    const { rerender } = renderWithI18n(<DateOnlyPicker {...props} defaultOpen />);
    await act(() => vi.dynamicImportSettled());
    expect(await screen.findByRole("grid")).toBeVisible();
    rerender(<DateOnlyPicker {...props} defaultOpen={false} />);
    expect(screen.getByRole("grid")).toBeVisible();
  });

  it("selects a calendar day after loading and closes the picker", async () => {
    const onChange = vi.fn();
    renderWithI18n(<DateOnlyPicker value="2026-11-05" onChange={onChange} icon={<span />} placeholder="Start date" emptyLabel="No date" defaultOpen />);
    await act(() => vi.dynamicImportSettled());
    await screen.findByRole("grid");
    const target = new Date(2026, 10, 6);
    fireEvent.click(document.querySelector(`button[data-day="${target.toLocaleDateString()}"]`)!);
    expect(onChange).toHaveBeenCalledWith("2026-11-06");
    await waitFor(() => expect(screen.queryByRole("grid")).not.toBeInTheDocument());
  });
});
