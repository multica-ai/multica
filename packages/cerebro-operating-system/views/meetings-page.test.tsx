import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { MeetingConfig } from "../core/types";
import { MeetingsPage } from "./meetings-page";

const state = vi.hoisted(() => ({
  meeting: {
    workspace_id: "workspace-1",
    cadence_unit: "manual",
    cadence_count: 1,
    agenda: [],
    available_note_types: [
      { id: "note-weekly", name: "Business Review", cadence_unit: "week", cadence_count: 1, enabled: true, current_note_id: "note-current", anchor_weekday: 1, next_meeting_date: "2026-07-27", upcoming_dates: ["2026-07-27", "2026-08-03"], year_dates: ["2026-07-27", "2026-08-03"] },
      { id: "note-monthly", name: "Finance Review", cadence_unit: "month", cadence_count: 1, enabled: true, anchor_weekday: 1, anchor_week_of_month: 3, next_meeting_date: "2026-08-17", upcoming_dates: ["2026-08-17"], year_dates: ["2026-08-17", "2026-09-21"], participants: [{ type: "member", id: "m1" }, { type: "agent", id: "a1" }] },
      { id: "note-manual", name: "Ad-hoc", cadence_unit: "manual", cadence_count: 1, enabled: true },
    ],
  } as MeetingConfig,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return { ...actual, useQuery: () => ({ data: state.meeting, isLoading: false, isError: false }) };
});
vi.mock("../core/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../core/queries")>();
  return { ...actual, meetingOptions: () => ({ queryKey: ["meeting"] }), settingsOptions: () => ({ queryKey: ["settings"] }) };
});

describe("MeetingsPage", () => {
  it("lists every recurring Note in the planner with its recurrence summary", () => {
    render(<MeetingsPage />);
    const planner = within(screen.getByRole("region", { name: "Cycles planner" }));
    expect(planner.getByText("Business Review")).toBeInTheDocument();
    expect(planner.getByText("Finance Review")).toBeInTheDocument();
    expect(planner.getByText(/Every month on the third Monday/)).toBeInTheDocument();
    expect(planner.getByText(/2 participants/)).toBeInTheDocument();
  });

  it("drops manual Notes from the planner", () => {
    render(<MeetingsPage />);
    expect(screen.queryByText("Ad-hoc")).not.toBeInTheDocument();
  });

  it("opens a recurring Note from the planner", () => {
    render(<MeetingsPage renderCurrentNote={(noteId) => <div>Canonical Note {noteId}</div>} />);
    fireEvent.click(screen.getByRole("button", { name: /Business Review/ }));
    expect(screen.getByText("Canonical Note note-current")).toBeInTheDocument();
  });

  it("has no agenda or cycle-setup controls anymore", () => {
    render(<MeetingsPage />);
    expect(screen.queryByRole("button", { name: "Cycle setup" })).not.toBeInTheDocument();
    expect(screen.queryByText("Agenda")).not.toBeInTheDocument();
  });
});
