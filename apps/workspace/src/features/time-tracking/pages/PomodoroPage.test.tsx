import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { PomodoroPage } from "./PomodoroPage";

// Spy to capture the params passed to usePomodoroHistoryQuery.
const historyQuerySpy = vi.fn();

vi.mock("../hooks/use-pomodoro-history", () => ({
  usePomodoroHistoryQuery: (params?: { limit?: number; offset?: number }) => {
    historyQuerySpy(params);
    return {
      isLoading: false,
      data: {
        // stats.today_count is intentionally set to 5 (UTC-based server value) while
        // the entries array has only 1 entry whose local date is today.
        // The page should display 1 (derived from entries) not 5.
        stats: {
          today_count: 5,
          week_count: 8,
          total_seconds: 1 * 25 * 60,
        },
        entries: [
          {
            id: "today-1",
            start_time: new Date().toISOString(),
            duration_seconds: 1500,
            description: "Deep work",
            issue_id: "issue-1",
            labels: [],
          },
          {
            id: "yesterday-1",
            start_time: new Date(Date.now() - 86_400_000).toISOString(),
            duration_seconds: 1500,
            description: "Review",
            issue_id: null,
            labels: [],
          },
        ],
      },
    };
  },
}));

vi.mock("../components/PomodoroTimer", () => ({
  PomodoroTimer: ({ variant }: { variant?: "compact" | "page" }) => (
    <section aria-label="Current session">
      {variant === "page" ? "Current session" : "Compact timer"}
    </section>
  ),
}));

describe("PomodoroPage", () => {
  it("renders the focus-first layout with history below the hero modules", () => {
    render(<PomodoroPage />);

    expect(screen.getByRole("heading", { name: "Pomodoro" })).toBeInTheDocument();
    expect(screen.getByText("Current session")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Today" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Recent sessions" })).toBeInTheDocument();

    const sections = screen.getAllByRole("region");
    expect(within(sections[0]!).getByText("Current session")).toBeInTheDocument();
    expect(within(sections[1]!).getByText("Today")).toBeInTheDocument();
    expect(within(sections[2]!).getByText("Recent sessions")).toBeInTheDocument();
  });

  it("uses local-midnight entry count for 'Done today', not stats.today_count", () => {
    // stats.today_count is 5 but only 1 entry has today's local date.
    // The summary must show 1 to stay consistent with PomodoroRecentSessions grouping.
    render(<PomodoroPage />);

    const todaySection = screen.getByRole("region", { name: "Today" });
    // "Done today" value is rendered as a <p> immediately following the "Done today" label.
    const doneLabel = within(todaySection).getByText("Done today");
    // The sibling <p> that holds the count value.
    const doneValue = doneLabel.nextElementSibling;
    expect(doneValue?.textContent).toBe("1");
  });

  it("fetches history with an explicit limit large enough for streak calculation", () => {
    historyQuerySpy.mockClear();
    render(<PomodoroPage />);

    expect(historyQuerySpy).toHaveBeenCalled();
    const calledParams = historyQuerySpy.mock.calls[0]?.[0] as
      | { limit?: number }
      | undefined;
    // Must request at least 3 650 entries (≈10 sessions/day × 365 days) so
    // computeStreak can see a full year even for active users.
    expect(calledParams?.limit).toBeGreaterThanOrEqual(3650);
  });
});
