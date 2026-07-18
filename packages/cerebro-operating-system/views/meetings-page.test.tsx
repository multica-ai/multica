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
    cadence_unit: "week",
    cadence_count: 1,
    agenda: [{ id: "review", name: "Review", position: 0, binding: "goals" }],
    available_note_types: [
      { id: "note-weekly", name: "Business Review", cadence_unit: "week", cadence_count: 1, enabled: true },
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

    expect(screen.getByText("Every week")).toBeInTheDocument();
    expect(screen.getByText(/Timing is controlled by Business Review in recurring Notes/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Cadence count")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cadence" })).not.toBeInTheDocument();
  });

  it("saves the cadence of a newly selected recurring note", () => {
    render(<MeetingsPage />);

    fireEvent.click(screen.getByRole("button", { name: "Recurring note type" }));
    fireEvent.click(screen.getByRole("option", { name: /Monthly Review/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save meeting" }));

    expect(state.save).toHaveBeenCalledWith(expect.objectContaining({
      note_type_id: "note-monthly",
      cadence_unit: "month",
      cadence_count: 1,
    }));
  });
});
