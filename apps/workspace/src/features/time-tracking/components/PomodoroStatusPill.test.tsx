import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import type { FocusSession } from "@/shared/types";
import { PomodoroStatusPill } from "./PomodoroStatusPill";

let mockSession: FocusSession | undefined;

vi.mock("@/shared/router", () => ({
  Link: ({
    href,
    children,
    className,
    ...props
  }: {
    href: string;
    children: React.ReactNode;
    className?: string;
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a href={href} className={className} {...props}>
      {children}
    </a>
  ),
}));

vi.mock("../hooks/use-focus", () => ({
  useFocusQuery: () => ({
    data: mockSession,
    isLoading: false,
  }),
}));

describe("PomodoroStatusPill", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-06-01T10:00:00.000Z"));
    mockSession = undefined;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders nothing when there is no active running session", () => {
    mockSession = {
      mode: "flowtime",
      phase: "idle",
      label_ids: [],
      elapsed_focus_seconds: 0,
      started_at: null,
    };

    const { container } = render(<PomodoroStatusPill />);

    expect(container).toBeEmptyDOMElement();
  });

  it("shows a focus link with elapsed time for a running focus session", async () => {
    mockSession = {
      id: "session-1",
      mode: "flowtime",
      phase: "focusing",
      label_ids: [],
      elapsed_focus_seconds: 120,
      started_at: "2024-06-01T10:00:00.000Z",
    };

    render(<PomodoroStatusPill />);

    const link = screen.getByRole("link", { name: /focus 2:00/i });
    expect(link).toHaveAttribute("href", "/focus");
    expect(screen.getByText("Focus")).toBeInTheDocument();
    expect(screen.getByText("2:00")).toBeInTheDocument();

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    expect(screen.getByText("2:01")).toBeInTheDocument();
  });

  it("renders the correct elapsed time on the first frame when switching to focusing", () => {
    mockSession = {
      mode: "flowtime",
      phase: "idle",
      label_ids: [],
      elapsed_focus_seconds: 0,
      started_at: null,
    };

    const { rerender } = render(<PomodoroStatusPill />);

    expect(screen.queryByRole("link")).not.toBeInTheDocument();

    vi.setSystemTime(new Date("2024-06-01T10:00:02.000Z"));
    mockSession = {
      id: "session-3",
      mode: "flowtime",
      phase: "focusing",
      label_ids: [],
      elapsed_focus_seconds: 120,
      started_at: "2024-06-01T10:00:00.000Z",
    };

    rerender(<PomodoroStatusPill />);

    expect(screen.getByRole("link", { name: /focus 2:02/i })).toBeInTheDocument();
    expect(screen.getByText("2:02")).toBeInTheDocument();
  });

  it("clears the interval when the pill unmounts", async () => {
    mockSession = {
      id: "session-4",
      mode: "flowtime",
      phase: "focusing",
      label_ids: [],
      elapsed_focus_seconds: 0,
      started_at: "2024-06-01T10:00:00.000Z",
    };

    const clearIntervalSpy = vi.spyOn(window, "clearInterval");
    const { unmount } = render(<PomodoroStatusPill />);

    expect(screen.getByText("0:00")).toBeInTheDocument();

    unmount();

    await act(async () => {
      vi.advanceTimersByTime(3000);
    });

    expect(clearIntervalSpy).toHaveBeenCalled();
    clearIntervalSpy.mockRestore();
  });

  it("shows break for a running break session", () => {
    mockSession = {
      id: "session-2",
      mode: "flowtime",
      phase: "breaking",
      label_ids: [],
      elapsed_focus_seconds: 1500,
      suggested_break_seconds: 300,
      started_at: "2024-06-01T10:00:00.000Z",
    };

    render(<PomodoroStatusPill />);

    expect(screen.getByText("Break")).toBeInTheDocument();
    expect(screen.getByText("5:00")).toBeInTheDocument();
  });
});
