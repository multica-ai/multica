import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ScheduledMessageControl } from "./scheduled-message-control";

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

vi.mock("./scheduled-message", () => ({
  deleteScheduledMessage: vi.fn(),
  listScheduledMessages: vi.fn().mockResolvedValue([]),
  nextMondayAtNine: () => new Date("2099-01-12T09:00:00"),
  sendScheduledMessageNow: vi.fn(),
  toLocalInputValue: () => "2099-01-02T09:00",
  tomorrowAtNine: () => new Date("2099-01-02T09:00:00"),
  updateScheduledMessage: vi.fn(),
}));

function renderControl(onSubmit = vi.fn()) {
  render(
    <ScheduledMessageControl
      issueId="channel-1"
      disabled={false}
      canSchedule
      onSchedule={vi.fn().mockResolvedValue(undefined)}
      onSubmit={onSubmit}
      submitDisabled={false}
      submitting={false}
    />,
  );
  return { onSubmit };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("ScheduledMessageControl responsive submit", () => {
  it("opens the scheduling choices from the compact desktop side toggle", async () => {
    renderControl();

    const desktopControl = screen.getByTestId("desktop-scheduled-submit");
    const submit = within(desktopControl).getByRole("button", { name: "Submit" });
    const toggle = screen.getByTestId("desktop-schedule-toggle");
    expect(submit).toHaveClass("sm:size-9");
    expect(toggle).toHaveClass("sm:size-9");
    fireEvent.keyDown(toggle, { key: "ArrowDown" });

    expect(screen.getByRole("menu")).toHaveAttribute("data-open", "");
    expect(screen.getByRole("menuitem", { name: "Tomorrow at 9:00 AM" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Next Monday at 9:00 AM" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Custom time…" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Scheduled messages" })).toBeInTheDocument();
  });

  it("submits once on a normal mobile tap", () => {
    const { onSubmit } = renderControl();

    fireEvent.pointerDown(screen.getByTestId("mobile-submit"), { pointerType: "touch" });
    act(() => vi.advanceTimersByTime(200));
    fireEvent.pointerUp(screen.getByTestId("mobile-submit"), { pointerType: "touch" });
    fireEvent.click(screen.getByTestId("mobile-submit"));

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("menuitem", { name: "Tomorrow at 9:00 AM" })).not.toBeInTheDocument();
  });

  it("opens the bottom sheet on mobile long press without submitting on release", () => {
    const { onSubmit } = renderControl();
    const submit = screen.getByTestId("mobile-submit");

    fireEvent.pointerDown(submit, { pointerType: "touch" });
    act(() => vi.advanceTimersByTime(500));
    fireEvent.pointerUp(submit, { pointerType: "touch" });
    fireEvent.click(submit);

    const sheet = screen.getByRole("dialog", { name: "Schedule message" });
    expect(sheet).toHaveAttribute("data-side", "bottom");
    expect(screen.getByRole("button", { name: "Tomorrow at 9:00 AM" })).toBeInTheDocument();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("cancels the hold when the pointer is cancelled", () => {
    const { onSubmit } = renderControl();
    const submit = screen.getByTestId("mobile-submit");

    fireEvent.pointerDown(submit, { pointerType: "touch" });
    act(() => vi.advanceTimersByTime(300));
    fireEvent.pointerCancel(submit, { pointerType: "touch" });
    act(() => vi.advanceTimersByTime(500));
    fireEvent.click(submit);

    expect(screen.queryByRole("menuitem", { name: "Tomorrow at 9:00 AM" })).not.toBeInTheDocument();
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });
});
