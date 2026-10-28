import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { DateOnlyPicker } from "./date-only-picker";

describe("date picker progressive disclosure", () => {
  it("keeps the calendar mounted when its one-shot defaultOpen flag clears", async () => {
    const props = { value: null, onChange: vi.fn(), icon: <span />, placeholder: "Start date", emptyLabel: "No date" };
    const { rerender } = renderWithI18n(<DateOnlyPicker {...props} defaultOpen />);
    expect(await screen.findByRole("grid")).toBeVisible();
    rerender(<DateOnlyPicker {...props} defaultOpen={false} />);
    expect(screen.getByRole("grid")).toBeVisible();
  });
});
