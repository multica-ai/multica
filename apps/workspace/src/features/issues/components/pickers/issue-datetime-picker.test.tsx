import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { IssueDateTimePicker } from "./issue-datetime-picker";

vi.mock("@/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  PopoverTrigger: ({ children, className }: { children: React.ReactNode; className?: string }) => (
    <button type="button" className={className}>
      {children}
    </button>
  ),
  PopoverContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("@/components/ui/calendar", () => ({
  Calendar: ({ onSelect, selected }: { onSelect: (value?: Date) => void; selected?: Date }) => (
    <div>
      <button type="button" onClick={() => onSelect(new Date("2026-04-15T00:00:00Z"))}>
        Pick day
      </button>
      <span data-testid="selected-day">{selected ? selected.toDateString() : "none"}</span>
    </div>
  ),
}));

vi.mock("@/components/ui/input", () => ({
  Input: (props: React.ComponentProps<"input">) => <input {...props} />,
}));

vi.mock("@/components/ui/scroll-area", () => ({
  ScrollArea: ({ children, className }: { children: React.ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
}));

describe("IssueDateTimePicker", () => {
  it("updates start_date with typed time input", () => {
    const onUpdate = vi.fn();
    const expectedDate = new Date("2026-04-15T00:00:00Z");
    expectedDate.setHours(14);
    expectedDate.setMinutes(45);
    expectedDate.setSeconds(0, 0);

    render(
      <IssueDateTimePicker
        field="start_date"
        dateTimeValue="2026-04-10T09:30:00Z"
        onUpdate={onUpdate}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Pick day" }));
    fireEvent.change(screen.getByPlaceholderText("HH:mm"), {
      target: { value: "14:45" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    expect(onUpdate).toHaveBeenCalledWith({
      start_date: expectedDate.toISOString(),
    });
  });

  it("defaults empty values to today and applies quick shortcuts", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-04-22T12:00:00Z"));

    const onUpdate = vi.fn();
    const expectedDate = new Date();
    expectedDate.setDate(expectedDate.getDate() + 1);
    expectedDate.setHours(0, 0, 0, 0);

    render(
      <IssueDateTimePicker
        field="start_date"
        dateTimeValue={null}
        onUpdate={onUpdate}
      />,
    );

    expect(screen.getByTestId("selected-day").textContent).not.toBe("none");

    fireEvent.click(screen.getByRole("button", { name: "Tomorrow" }));
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    expect(onUpdate).toHaveBeenCalledWith({
      start_date: expectedDate.toISOString(),
    });

    vi.useRealTimers();
  });

  it("clears end_date", () => {
    const onUpdate = vi.fn();

    render(
      <IssueDateTimePicker
        field="end_date"
        dateTimeValue="2026-04-10T09:30:00Z"
        onUpdate={onUpdate}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Clear date" }));

    expect(onUpdate).toHaveBeenCalledWith({ end_date: null });
  });
});