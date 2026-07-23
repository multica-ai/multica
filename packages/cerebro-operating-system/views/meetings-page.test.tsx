import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { MeetingConfig } from "../core/types";
import { MeetingsPage } from "./meetings-page";

const state = vi.hoisted(() => ({
  meeting: {
    workspace_id: "workspace-1",
    note_type_id: "note-weekly",
    note_type_name: "Business Review",
    current_note_id: "note-current",
    cadence_unit: "week",
    cadence_count: 1,
    agenda: [{ id: "review", name: "Review", position: 0, binding: "goals" }],
    available_note_types: [
      { id: "note-weekly", name: "Business Review", cadence_unit: "week", cadence_count: 1, enabled: true, current_note_id: "note-current" },
      { id: "note-monthly", name: "Monthly Review", cadence_unit: "month", cadence_count: 1, enabled: true },
    ],
  } as MeetingConfig,
  save: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return { ...actual, useQuery: () => ({ data: state.meeting, isLoading: false, isError: false }) };
});
vi.mock("../core/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../core/queries")>();
  return {
    ...actual,
    meetingOptions: () => ({ queryKey: ["meeting"] }),
    useUpdateMeeting: () => ({ mutate: state.save, isPending: false, isError: false }),
  };
});

describe("MeetingsPage", () => {
  beforeEach(() => state.save.mockReset());

  it("takes its timing from the selected recurring note instead of separate cadence controls", () => {
    render(<MeetingsPage />);
    fireEvent.click(screen.getByRole("button", { name: "Cycle setup" }));

    expect(screen.getByText("Every week")).toBeInTheDocument();
    expect(screen.getByText(/Timing is controlled by Business Review in recurring Notes/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Cadence count")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cadence" })).not.toBeInTheDocument();
  });

  it("saves the cadence of a newly selected recurring note", () => {
    render(<MeetingsPage />);
    fireEvent.click(screen.getByRole("button", { name: "Cycle setup" }));

    fireEvent.click(screen.getByRole("button", { name: "Recurring note type" }));
    fireEvent.click(screen.getByRole("option", { name: /Monthly Review/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save Cycle setup" }));

    expect(state.save).toHaveBeenCalledWith(expect.objectContaining({
      note_type_id: "note-monthly",
      cadence_unit: "month",
      cadence_count: 1,
    }));
  });

  it("renders the canonical current Note as the primary Cycle surface", () => {
    render(<MeetingsPage renderCurrentNote={(noteId) => <div>Canonical Note {noteId}</div>} />);
    expect(screen.getByText("Canonical Note note-current")).toBeInTheDocument();
    expect(screen.queryByText("Recurring note type")).not.toBeInTheDocument();
  });

  it("shows the cycle timeline overview in the default view", () => {
    render(<MeetingsPage renderCurrentNote={(noteId) => <div>Canonical Note {noteId}</div>} />);
    expect(screen.getByRole("region", { name: "Cycles timeline" })).toBeInTheDocument();
  });

  it("still shows the timeline when no recurring note type has been selected yet", () => {
    const previous = state.meeting;
    state.meeting = { ...previous, note_type_id: undefined, note_type_name: undefined, current_note_id: undefined, cadence_unit: "manual", cadence_count: 1 };
    try {
      render(<MeetingsPage />);
      expect(screen.getByRole("region", { name: "Cycles timeline" })).toBeInTheDocument();
    } finally {
      state.meeting = previous;
    }
  });

  it("never persists the previewed fallback cadence when nothing was selected", () => {
    const previous = state.meeting;
    state.meeting = { ...previous, note_type_id: undefined, note_type_name: undefined, current_note_id: undefined, cadence_unit: "manual", cadence_count: 1 };
    try {
      render(<MeetingsPage />);
      fireEvent.click(screen.getByRole("button", { name: "Cycle setup" }));
      fireEvent.click(screen.getByRole("button", { name: "Save Cycle setup" }));

      expect(state.save).toHaveBeenCalledWith(expect.objectContaining({ cadence_unit: "manual" }));
    } finally {
      state.meeting = previous;
    }
  });
});
