// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { CyclesPage } from "./cycles-page";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({ data: { id: "note-current", body: "Review body" }, isLoading: false, isError: false }) }));
vi.mock("../core", () => ({ noteDetailOptions: (wsId: string, noteId: string) => ({ queryKey: ["notes", wsId, noteId] }) }));
vi.mock("./notes-page", () => ({ NoteEditor: ({ note, wsId }: { note: { id: string; body: string }; wsId: string }) => <div>Canonical editor {note.id} in {wsId}: {note.body}</div> }));
vi.mock("@multica/cerebro-operating-system/views", () => ({ MeetingsPage: ({ renderCurrentNote }: { renderCurrentNote: (noteId: string, openSetup: () => void) => React.ReactNode }) => <>{renderCurrentNote("note-current", vi.fn())}</> }));

describe("CyclesPage", () => {
  it("loads the current Note through the canonical query and editor seam", () => {
    render(<CyclesPage />);
    expect(screen.getByText("Canonical editor note-current in workspace-1: Review body")).toBeInTheDocument();
  });
});
