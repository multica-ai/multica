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
  members: [{ id: "m1", name: "Jesper" }],
  agents: [{ id: "a1", name: "Sara" }],
  saveCycle: vi.fn(),
  deleteCycle: vi.fn(),
  openCycleNote: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: ({ queryKey }: { queryKey: string[] }) => {
      if (queryKey[0] === "members") return { data: state.members, isLoading: false, isError: false };
      if (queryKey[0] === "agents") return { data: state.agents, isLoading: false, isError: false };
      return { data: state.meeting, isLoading: false, isError: false };
    },
  };
});
vi.mock("../core/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../core/queries")>();
  const mutation = (fn: ReturnType<typeof vi.fn>) => () => ({ mutate: fn, isPending: false, isError: false });
  return {
    ...actual,
    meetingOptions: () => ({ queryKey: ["meeting"] }),
    settingsOptions: () => ({ queryKey: ["settings"] }),
    useSaveCycle: mutation(state.saveCycle),
    useDeleteCycle: mutation(state.deleteCycle),
    useOpenCycleNote: mutation(state.openCycleNote),
  };
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
    fireEvent.click(screen.getByRole("button", { name: "Open Business Review" }));
    expect(screen.getByText("Canonical Note note-current")).toBeInTheDocument();
  });

  // FIR-3589 item 6: a cycle that has never run still opens — its Note is
  // written on first open rather than the row being a dead end.
  it("writes the Note the first time a never-run cycle is opened", () => {
    state.openCycleNote.mockReset();
    render(<MeetingsPage renderCurrentNote={(noteId) => <div>Canonical Note {noteId}</div>} />);

    fireEvent.click(screen.getByRole("button", { name: "Open Finance Review" }));

    expect(state.openCycleNote).toHaveBeenCalledWith("note-monthly", expect.any(Object));
  });

  // FIR-3589 item 6: cycles are created and re-timed from this page.
  it("creates a cycle with a name and a repeat rhythm", () => {
    state.saveCycle.mockReset();
    render(<MeetingsPage />);

    fireEvent.click(screen.getByRole("button", { name: "Add cycle" }));
    fireEvent.change(screen.getByLabelText("Cycle name"), { target: { value: "Quarterly Review" } });
    fireEvent.click(screen.getByRole("button", { name: "Repeats" }));
    fireEvent.click(screen.getByRole("option", { name: "Every quarter" }));
    fireEvent.click(screen.getByRole("button", { name: "Create cycle" }));

    expect(state.saveCycle).toHaveBeenCalledWith(
      expect.objectContaining({ id: undefined, input: expect.objectContaining({ name: "Quarterly Review", cadence_unit: "quarter" }) }),
      expect.any(Object),
    );
  });

  it("changes when an existing cycle meets", () => {
    state.saveCycle.mockReset();
    render(<MeetingsPage />);

    fireEvent.click(screen.getByRole("button", { name: "Edit Finance Review" }));
    fireEvent.click(screen.getByRole("button", { name: "Week of the month" }));
    fireEvent.click(screen.getByRole("option", { name: "Last" }));
    fireEvent.click(screen.getByRole("button", { name: "Save cycle" }));

    expect(state.saveCycle).toHaveBeenCalledWith(
      expect.objectContaining({ id: "note-monthly", input: expect.objectContaining({ anchor_week_of_month: -1, anchor_weekday: 1 }) }),
      expect.any(Object),
    );
  });

  it("has no agenda or cycle-setup controls anymore", () => {
    render(<MeetingsPage />);
    expect(screen.queryByRole("button", { name: "Cycle setup" })).not.toBeInTheDocument();
    expect(screen.queryByText("Agenda")).not.toBeInTheDocument();
  });
});
