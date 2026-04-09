import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { DueDatePicker } from "./due-date-picker";

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
  Calendar: ({ onSelect }: { onSelect: (value?: Date) => void }) => (
    <button type="button" onClick={() => onSelect(new Date("2026-04-15T00:00:00Z"))}>
      Pick date
    </button>
  ),
}));

describe("DueDatePicker", () => {
  it("updates and clears due_date", () => {
    const onUpdate = vi.fn();

    render(
      <DueDatePicker
        dueDate="2026-04-10T00:00:00Z"
        onUpdate={onUpdate}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Pick date" }));
    fireEvent.click(screen.getByRole("button", { name: "Clear date" }));

    expect(onUpdate).toHaveBeenNthCalledWith(1, { due_date: "2026-04-15T00:00:00.000Z" });
    expect(onUpdate).toHaveBeenNthCalledWith(2, { due_date: null });
  });
});