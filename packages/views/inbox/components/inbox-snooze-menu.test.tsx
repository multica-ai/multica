import { useState } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { InboxSnoozeMenu } from "./inbox-snooze-menu";
import { formatSnoozeTime } from "./inbox-snooze-time";

const at = (y: number, m: number, d: number, h: number, min = 0) => new Date(y, m - 1, d, h, min);
// A Wednesday afternoon, with the evening still more than an hour away.
const NOW = at(2026, 6, 17, 14, 20);
// The times are written the way the locale writes them, which is the
// formatter's business and has its own tests.
const time = (until: Date, now = NOW) => formatSnoozeTime(until, now, "en");

function Harness({
  onSnooze,
  onUnsnooze,
  snoozed,
  initialOpen = true,
}: {
  onSnooze: (until: Date) => void;
  onUnsnooze?: () => void;
  snoozed?: boolean;
  initialOpen?: boolean;
}) {
  const [open, setOpen] = useState(initialOpen);
  return (
    <InboxSnoozeMenu
      open={open}
      onOpenChange={setOpen}
      snoozed={snoozed}
      onSnooze={onSnooze}
      onUnsnooze={onUnsnooze}
      trigger={<button type="button">Snooze</button>}
    />
  );
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(NOW);
});
afterEach(() => {
  vi.useRealTimers();
});

describe("InboxSnoozeMenu", () => {
  it("offers a few times worth one click, each with the time it means", async () => {
    renderWithI18n(<Harness onSnooze={vi.fn()} />);

    const items = await screen.findAllByRole("menuitem");
    expect(items.map((item) => item.textContent)).toEqual([
      `In 1 hour${time(at(2026, 6, 17, 15, 20))}`,
      `Later today${time(at(2026, 6, 17, 18))}`,
      `Tomorrow${time(at(2026, 6, 18, 9))}`,
      `Next Monday${time(at(2026, 6, 22, 9))}`,
      "Pick a time...",
    ]);
    expect(
      screen.getByText("Comes back early if something new needs you, such as a mention or a failed run."),
    ).toBeInTheDocument();
  });

  it("snoozes until the time picked", async () => {
    const onSnooze = vi.fn();
    renderWithI18n(<Harness onSnooze={onSnooze} />);

    fireEvent.click(await screen.findByRole("menuitem", { name: /^Later today/ }));

    expect(onSnooze).toHaveBeenCalledWith(at(2026, 6, 17, 18));
  });

  it("offers the way back only for an issue that is already snoozed", async () => {
    const onUnsnooze = vi.fn();
    const { unmount } = renderWithI18n(<Harness onSnooze={vi.fn()} onUnsnooze={onUnsnooze} />);
    await screen.findByRole("menuitem", { name: /^In 1 hour/ });
    expect(screen.queryByRole("menuitem", { name: "Unsnooze" })).toBeNull();
    unmount();

    renderWithI18n(<Harness onSnooze={vi.fn()} onUnsnooze={onUnsnooze} snoozed />);
    fireEvent.click(await screen.findByRole("menuitem", { name: "Unsnooze" }));

    expect(onUnsnooze).toHaveBeenCalledTimes(1);
  });

  it("reads the clock again each time it opens", async () => {
    renderWithI18n(<Harness onSnooze={vi.fn()} initialOpen={false} />);
    const later = at(2026, 6, 17, 15, 0);
    vi.setSystemTime(later);

    fireEvent.click(screen.getByRole("button", { name: "Snooze" }));

    expect(await screen.findByRole("menuitem", { name: /^In 1 hour/ })).toHaveTextContent(
      time(at(2026, 6, 17, 16, 0), later),
    );
  });
});

describe("InboxSnoozeMenu custom time", () => {
  async function openDialog(onSnooze = vi.fn()) {
    renderWithI18n(<Harness onSnooze={onSnooze} />);
    fireEvent.click(await screen.findByRole("menuitem", { name: "Pick a time..." }));
    const dialog = await screen.findByRole("dialog");
    const field = dialog.querySelector('input[type="datetime-local"]') as HTMLInputElement;
    return { dialog, field, onSnooze };
  }

  it("starts from tomorrow morning", async () => {
    const { dialog, field } = await openDialog();

    expect(within(dialog).getByRole("heading", { name: "Snooze until" })).toBeInTheDocument();
    expect(field.value).toBe("2026-06-18T09:00");
  });

  it("snoozes until the time typed, in the viewer's own time zone", async () => {
    const { dialog, field, onSnooze } = await openDialog();

    fireEvent.change(field, { target: { value: "2026-07-03T08:30" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Snooze" }));

    expect(onSnooze).toHaveBeenCalledWith(at(2026, 7, 3, 8, 30));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("refuses a time that has passed and says what it takes", async () => {
    const { dialog, field, onSnooze } = await openDialog();

    fireEvent.change(field, { target: { value: "2026-06-17T09:00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Snooze" }));

    expect(onSnooze).not.toHaveBeenCalled();
    expect(within(dialog).getByRole("alert")).toHaveTextContent(
      "Choose a time in the future, within a year.",
    );
    expect(field).toHaveAttribute("aria-invalid", "true");

    fireEvent.change(field, { target: { value: "2026-06-19T09:00" } });
    expect(within(dialog).queryByRole("alert")).toBeNull();
  });
});
