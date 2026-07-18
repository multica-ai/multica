import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { ProjectStartDatePicker } from "./project-start-date-picker";
import { ProjectDueDatePicker } from "./project-due-date-picker";

describe("ProjectStartDatePicker", () => {
  it("shows the placeholder label when no date is set", () => {
    renderWithI18n(<ProjectStartDatePicker startDate={null} onUpdate={vi.fn()} />);
    expect(screen.getByText("Start date")).toBeInTheDocument();
  });

  it("shows the formatted calendar day when a date is set", () => {
    renderWithI18n(
      <ProjectStartDatePicker startDate="2026-03-01" onUpdate={vi.fn()} />,
    );
    expect(screen.getByText("Mar 1")).toBeInTheDocument();
  });

  it("emits start_date: null when the date is cleared", async () => {
    const onUpdate = vi.fn();
    const user = userEvent.setup();
    renderWithI18n(
      <ProjectStartDatePicker startDate="2026-03-01" onUpdate={onUpdate} />,
    );
    await user.click(screen.getByText("Mar 1")); // open popover
    await user.click(screen.getByRole("button", { name: "Clear date" }));
    expect(onUpdate).toHaveBeenCalledWith({ start_date: null });
  });
});

describe("ProjectDueDatePicker", () => {
  it("shows the placeholder label when no date is set", () => {
    renderWithI18n(<ProjectDueDatePicker dueDate={null} onUpdate={vi.fn()} />);
    expect(screen.getByText("Due date")).toBeInTheDocument();
  });

  it("shows the formatted calendar day when a date is set", () => {
    renderWithI18n(<ProjectDueDatePicker dueDate="2026-03-01" onUpdate={vi.fn()} />);
    expect(screen.getByText("Mar 1")).toBeInTheDocument();
  });

  it("emits due_date: null when the date is cleared", async () => {
    const onUpdate = vi.fn();
    const user = userEvent.setup();
    renderWithI18n(
      <ProjectDueDatePicker dueDate="2026-03-01" onUpdate={onUpdate} />,
    );
    await user.click(screen.getByText("Mar 1")); // open popover
    await user.click(screen.getByRole("button", { name: "Clear date" }));
    expect(onUpdate).toHaveBeenCalledWith({ due_date: null });
  });
});
