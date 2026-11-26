import { useState } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { InboxSnoozeMenu } from "./inbox-snooze-menu";

const onSnooze = vi.fn();

async function findDay(date: Date): Promise<HTMLElement> {
  await screen.findByText("Pick a date and time");
  // The calendar loads lazily, which can take a while on a busy test run.
  return vi.waitUntil(
    () => document.querySelector<HTMLElement>(`button[data-day="${date.toLocaleDateString()}"]`),
    { timeout: 10_000 },
  );
}

function Harness({ initialOpen = true }: { initialOpen?: boolean }) {
  const [open, setOpen] = useState(initialOpen);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Open from outside
      </button>
      <InboxSnoozeMenu
        open={open}
        onOpenChange={setOpen}
        onSnooze={onSnooze}
        trigger={<button type="button">Snooze</button>}
      />
    </>
  );
}

describe("InboxSnoozeMenu", () => {
  beforeEach(() => {
    // Monday, November 16, 14:20 local. Only the clock is faked, so the lazy
    // calendar still loads.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(2026, 10, 16, 14, 20));
    onSnooze.mockReset();
  });
  afterEach(() => vi.useRealTimers());

  it("offers five durations, each with the time it ends", async () => {
    renderWithI18n(<Harness />);
    const items = await screen.findAllByRole("menuitem");
    expect(items.map((item) => item.textContent)).toEqual([
      "1 hour3:20 PM",
      "3 hours5:20 PM",
      "1 daytomorrow 2:20 PM",
      "3 daysThu 2:20 PM",
      "1 weekNov 23, 2:20 PM",
      "Pick a date and time...",
    ]);
  });

  it("snoozes until a day and time picked in place of the list", async () => {
    renderWithI18n(<Harness />);
    fireEvent.click(await screen.findByRole("menuitem", { name: /Pick a date and time/ }));

    expect(await screen.findByText("Pick a date and time")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Snooze until tomorrow 9:00 AM" })).toBeEnabled();

    fireEvent.click(await findDay(new Date(2026, 10, 20)));
    const hour = screen.getByRole("textbox", { name: "Hour" });
    fireEvent.keyDown(hour, { key: "1" });
    fireEvent.keyDown(hour, { key: "5" });
    fireEvent.click(screen.getByRole("button", { name: "Snooze until Fri 3:00 PM" }));

    expect(onSnooze).toHaveBeenCalledWith(new Date(2026, 10, 20, 15, 0));
    expect(screen.queryByText("Pick a date and time")).toBeNull();
  });

  it("refuses a time that has already passed", async () => {
    renderWithI18n(<Harness />);
    fireEvent.click(await screen.findByRole("menuitem", { name: /Pick a date and time/ }));
    fireEvent.click(await findDay(new Date(2026, 10, 16)));

    expect(screen.getByRole("button", { name: "Pick a time in the future, within a year." })).toBeDisabled();
  });

  it("goes back to the durations", async () => {
    renderWithI18n(<Harness />);
    fireEvent.click(await screen.findByRole("menuitem", { name: /Pick a date and time/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Back" }));
    expect(await screen.findByRole("menuitem", { name: /^1 hour/ })).toBeInTheDocument();
  });

  it("reads the clock when the parent opens it, as H and the hover card do", async () => {
    renderWithI18n(<Harness initialOpen={false} />);
    vi.setSystemTime(new Date(2026, 10, 16, 16, 20));
    fireEvent.click(screen.getByText("Open from outside"));
    fireEvent.click(await screen.findByRole("menuitem", { name: /^1 hour/ }));
    expect(onSnooze).toHaveBeenCalledWith(new Date(2026, 10, 16, 17, 20));
  });

  it("opens on the durations again after closing from the picker", async () => {
    renderWithI18n(<Harness />);
    fireEvent.click(await screen.findByRole("menuitem", { name: /Pick a date and time/ }));
    await screen.findByText("Pick a date and time");
    fireEvent.keyDown(screen.getByRole("button", { name: "Back" }), { key: "Escape" });
    await vi.waitUntil(() => !screen.queryByText("Pick a date and time"));
    fireEvent.click(screen.getByText("Open from outside"));
    expect(await screen.findByRole("menuitem", { name: /^1 hour/ })).toBeInTheDocument();
  });
});
