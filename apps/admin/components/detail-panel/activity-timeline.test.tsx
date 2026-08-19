// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ActivityTimeline } from "./activity-timeline";
import type { ActivityEvent } from "@/lib/types";

function makeEvents(count: number): ActivityEvent[] {
  return Array.from({ length: count }, (_, i) => ({
    type: "default" as const,
    text: `Event ${i}`,
    at: new Date(2026, 0, i + 1).toISOString(),
  }));
}

describe("ActivityTimeline", () => {
  it("collapses to 10 events with a 'View all' link, and expands on click (plan §2.2B)", async () => {
    render(<ActivityTimeline events={makeEvents(14)} />);
    expect(screen.getByText("Event 0")).toBeInTheDocument();
    expect(screen.queryByText("Event 13")).not.toBeInTheDocument();

    await userEvent.click(screen.getByText("View all (14)"));
    expect(screen.getByText("Event 13")).toBeInTheDocument();
  });

  it("renders no 'View all' link when there are 10 or fewer events", () => {
    render(<ActivityTimeline events={makeEvents(5)} />);
    expect(screen.queryByText(/View all/)).not.toBeInTheDocument();
  });
});
