import { describe, it, expect, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { CustomReminderPicker } from "@multica/cerebro-inbox";

// FIR-2546 — desktop reminder picker swapped the segmented HH:MM TimeInput for
// a calendar + scrollable hour/minute columns (time.rdsx.dev style). These
// tests lock the interaction contract: the value stays a `datetime-local`
// string, picking an hour/minute keeps the rest of the moment intact.

const MIN = new Date(2026, 4, 30, 9, 0); // 2026-05-30 09:00 local

describe("CustomReminderPicker (desktop)", () => {
  it("renders all 24 hours and the 5-minute steps as listbox options", () => {
    render(
      <CustomReminderPicker value="2026-05-30T09:00" min={MIN} onChange={() => {}} />,
    );
    const hours = within(screen.getByRole("listbox", { name: "Hour" })).getAllByRole(
      "option",
    );
    const minutes = within(
      screen.getByRole("listbox", { name: "Minute" }),
    ).getAllByRole("option");
    expect(hours).toHaveLength(24);
    expect(minutes).toHaveLength(12); // 00, 05, … 55
    expect(hours[9]).toHaveAttribute("aria-selected", "true"); // 09 selected
    expect(minutes[0]).toHaveAttribute("aria-selected", "true"); // 00 selected
  });

  it("picking an hour keeps the date + minute and re-serializes the value", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <CustomReminderPicker value="2026-05-30T09:30" min={MIN} onChange={onChange} />,
    );
    const hourList = screen.getByRole("listbox", { name: "Hour" });
    await user.click(within(hourList).getByRole("option", { name: "14" }));
    expect(onChange).toHaveBeenCalledWith("2026-05-30T14:30");
  });

  it("picking a minute keeps the date + hour and re-serializes the value", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <CustomReminderPicker value="2026-05-30T09:30" min={MIN} onChange={onChange} />,
    );
    const minuteList = screen.getByRole("listbox", { name: "Minute" });
    await user.click(within(minuteList).getByRole("option", { name: "45" }));
    expect(onChange).toHaveBeenCalledWith("2026-05-30T09:45");
  });

  it("highlights the nearest 5-minute step for an off-grid seed time", () => {
    render(
      <CustomReminderPicker value="2026-05-30T09:22" min={MIN} onChange={() => {}} />,
    );
    const minutes = within(
      screen.getByRole("listbox", { name: "Minute" }),
    ).getAllByRole("option");
    // 22 rounds to 20 → index 4.
    expect(minutes[4]).toHaveAttribute("aria-selected", "true");
  });
});
