import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DayButton } from "react-day-picker";
import { Calendar } from "./calendar";

afterEach(cleanup);

function dayButton(container: HTMLElement, date: Date) {
  return container.querySelector<HTMLButtonElement>(
    `button[data-day="${date.toLocaleDateString()}"]`,
  )!;
}

describe("Calendar keyboard focus", () => {
  it.each([
    ["ArrowRight", new Date(2026, 10, 5), new Date(2026, 10, 6)],
    ["ArrowLeft", new Date(2026, 10, 5), new Date(2026, 10, 4)],
    ["ArrowDown", new Date(2026, 10, 5), new Date(2026, 10, 12)],
    ["ArrowUp", new Date(2026, 10, 5), new Date(2026, 9, 29)],
    ["ArrowRight", new Date(2026, 10, 30), new Date(2026, 11, 1)],
    ["ArrowLeft", new Date(2026, 10, 1), new Date(2026, 9, 31)],
    ["Home", new Date(2026, 10, 5), new Date(2026, 10, 1)],
    ["End", new Date(2026, 10, 5), new Date(2026, 10, 7)],
    ["PageUp", new Date(2026, 10, 5), new Date(2026, 9, 5)],
    ["PageDown", new Date(2026, 10, 5), new Date(2026, 11, 5)],
  ])("moves DOM focus with %s from %s to %s", async (key, selected, target) => {
    const onSelect = vi.fn();
    const { container } = render(
      <Calendar mode="single" selected={selected} onSelect={onSelect} />,
    );
    const initial = dayButton(container, selected);
    act(() => initial.focus());

    fireEvent.keyDown(initial, { key });

    await waitFor(() => expect(dayButton(container, target)).toHaveFocus());
    expect(onSelect).not.toHaveBeenCalled();
    fireEvent.click(document.activeElement!);
    expect(onSelect.mock.calls[0]?.[0]).toEqual(target);
  });

  it("skips disabled days and leaves their buttons disabled", async () => {
    const selected = new Date(2026, 10, 5);
    const disabled = new Date(2026, 10, 6);
    const onSelect = vi.fn();
    const { container } = render(
      <Calendar mode="single" selected={selected} disabled={disabled} onSelect={onSelect} />,
    );
    expect(dayButton(container, disabled)).toBeDisabled();
    fireEvent.click(dayButton(container, disabled));
    expect(onSelect).not.toHaveBeenCalled();

    const initial = dayButton(container, selected);
    act(() => initial.focus());
    fireEvent.keyDown(initial, { key: "ArrowRight" });

    await waitFor(() => expect(dayButton(container, new Date(2026, 10, 7))).toHaveFocus());
  });

  it("keeps a caller's custom day button", () => {
    const { container } = render(
      <Calendar
        mode="single"
        defaultMonth={new Date(2026, 10, 1)}
        components={{
          DayButton: (props) => <DayButton {...props} data-custom-day="true" />,
        }}
      />,
    );

    expect(container.querySelector("button[data-custom-day=true]")).toBeInTheDocument();
    expect(container.querySelector("button[data-day]")).toBeNull();
  });
});
