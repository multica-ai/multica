import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import type { PomodoroSession } from "@/shared/types";
import { PomodoroStatusPill } from "./PomodoroStatusPill";
import { getPomodoroHeaderLabel } from "../lib/pomodoro-display";

let mockSession: PomodoroSession | undefined;

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

vi.mock("../hooks/use-pomodoro", () => ({
  usePomodoroQuery: () => ({
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
      phase: "work",
      phase_duration_seconds: 1500,
      status: "idle",
      elapsed_seconds: 0,
      started_at: null,
    };

    const { container } = render(<PomodoroStatusPill />);

    expect(container).toBeEmptyDOMElement();
  });

  it("shows a pomodoro link with focus label and remaining time for a running work session", async () => {
    mockSession = {
      id: "session-1",
      phase: "work",
      phase_duration_seconds: 1500,
      status: "running",
      elapsed_seconds: 120,
      started_at: "2024-06-01T10:00:00.000Z",
    };

    render(<PomodoroStatusPill />);

    const link = screen.getByRole("link", { name: /focus 23:00/i });
    expect(link).toHaveAttribute("href", "/pomodoro");
    expect(screen.getByText("Focus")).toBeInTheDocument();
    expect(screen.getByText("23:00")).toBeInTheDocument();

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    expect(screen.getByText("22:59")).toBeInTheDocument();
  });

  it("renders the correct remaining time on the first frame when switching to running", () => {
    mockSession = {
      phase: "work",
      phase_duration_seconds: 1500,
      status: "idle",
      elapsed_seconds: 0,
      started_at: null,
    };

    const { rerender } = render(<PomodoroStatusPill />);

    expect(screen.queryByRole("link")).not.toBeInTheDocument();

    vi.setSystemTime(new Date("2024-06-01T10:00:02.000Z"));
    mockSession = {
      id: "session-3",
      phase: "work",
      phase_duration_seconds: 1500,
      status: "running",
      elapsed_seconds: 120,
      started_at: "2024-06-01T10:00:00.000Z",
    };

    rerender(<PomodoroStatusPill />);

    expect(screen.getByRole("link", { name: /focus 22:58/i })).toBeInTheDocument();
    expect(screen.getByText("22:58")).toBeInTheDocument();
  });

  it("clears the interval when the pill unmounts", async () => {
    mockSession = {
      id: "session-4",
      phase: "work",
      phase_duration_seconds: 1500,
      status: "running",
      elapsed_seconds: 0,
      started_at: "2024-06-01T10:00:00.000Z",
    };

    const clearIntervalSpy = vi.spyOn(window, "clearInterval");
    const { unmount } = render(<PomodoroStatusPill />);

    expect(screen.getByText("25:00")).toBeInTheDocument();

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
      phase: "break",
      phase_duration_seconds: 300,
      status: "running",
      elapsed_seconds: 0,
      started_at: "2024-06-01T10:00:00.000Z",
    };

    render(<PomodoroStatusPill />);

    expect(screen.getByText("Break")).toBeInTheDocument();
  });

  it("maps long breaks to the break label", () => {
    expect(getPomodoroHeaderLabel("long_break")).toBe("Break");
  });
});
