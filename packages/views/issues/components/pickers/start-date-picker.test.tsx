import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ButtonHTMLAttributes, ReactElement, ReactNode } from "react";
import { StartDatePicker } from "./start-date-picker";

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  PopoverTrigger: ({
    children,
    render,
    ...props
  }: {
    children: ReactNode;
    render?: ReactElement;
  } & ButtonHTMLAttributes<HTMLButtonElement>) =>
    render ? <>{render}</> : <button type="button" {...props}>{children}</button>,
  PopoverContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({ children, ...props }: ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" {...props}>
      {children}
    </button>
  ),
}));

vi.mock("../../../i18n", () => ({
  useT: () => ({
    t: (selector: (dict: any) => string) =>
      selector({
        pickers: {
          start_date: {
            trigger_label: "Start date",
            clear_action: "Clear",
          },
        },
      }),
  }),
}));

describe("StartDatePicker", () => {
  it("shows a time label when the start date has minutes", () => {
    const startDate = "2026-05-25T06:30:00.000Z";
    render(<StartDatePicker startDate={startDate} onUpdate={vi.fn()} />);

    const button = screen.getByRole("button");
    const date = new Date(startDate);
    expect(button).toHaveTextContent(date.toLocaleDateString("en-US", { month: "short", day: "numeric" }));
    expect(button).toHaveTextContent(date.toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" }));
  });

  it("emits an ISO timestamp when the datetime input changes", async () => {
    const onUpdate = vi.fn();

    render(<StartDatePicker startDate={null} onUpdate={onUpdate} />);

    const input = screen.getByLabelText("Start date");
    fireEvent.change(input, { target: { value: "2026-06-01T09:15" } });

    expect(onUpdate).toHaveBeenCalledWith({
      start_date: new Date("2026-06-01T09:15").toISOString(),
    });
  });

  it("clears the start date", async () => {
    const onUpdate = vi.fn();

    render(<StartDatePicker startDate="2026-05-25T06:30:00.000Z" onUpdate={onUpdate} />);

    fireEvent.click(screen.getByRole("button", { name: "Clear" }));

    expect(onUpdate).toHaveBeenCalledWith({ start_date: null });
  });
});
