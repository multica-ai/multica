// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { afterEach, describe, it, expect, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { NoteSchema, type Note } from "../core";
import { NoteListSection } from "./note-list-section";

// FIR-4163: a note row has to say which folder its note lives in, and offer a
// way to move it. Extracted from notes-page so both are testable directly.

function note(over: Partial<Note> = {}): Note {
  return NoteSchema.parse({
    id: "n-1",
    title: "Roadmap",
    body: "Ship the notes work.",
    owner_id: "me",
    ...over,
  });
}

const folderNameById = new Map([
  ["folder-a", "Sales"],
  ["folder-b", "Reports"],
]);

function renderSection(
  props: Partial<React.ComponentProps<typeof NoteListSection>> = {},
) {
  const onMove = vi.fn();
  render(
    <NoteListSection
      label=""
      notes={[note({ folder_id: "folder-a" })]}
      selectedId={null}
      onSelect={() => {}}
      myId="me"
      members={[]}
      folderNameById={folderNameById}
      currentFolderId={null}
      onMove={onMove}
      {...props}
    />,
  );
  return { onMove };
}

// This package runs without global auto-cleanup, so unmount explicitly.
afterEach(() => {
  cleanup();
});

describe("NoteListSection", () => {
  it("names the folder a note lives in", () => {
    renderSection();

    expect(screen.getByText("Sales")).toBeInTheDocument();
  });

  it("leaves the folder out when you are already standing in it", () => {
    // Repeating "Sales" on every row inside Sales is noise, not information.
    renderSection({ currentFolderId: "folder-a" });

    expect(screen.queryByText("Sales")).toBeNull();
  });

  it("says nothing about a folder for a note outside the tree", () => {
    renderSection({ notes: [note({ folder_id: null })] });

    expect(screen.queryByText("Sales")).toBeNull();
    expect(screen.queryByText("Reports")).toBeNull();
  });

  it("offers Move to… on the row, and reports which note to move", () => {
    const subject = note({ id: "n-9", folder_id: "folder-b" });
    const { onMove } = renderSection({ notes: [subject] });

    fireEvent.click(screen.getByRole("button", { name: "Actions for Roadmap" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Move to…" }));

    expect(onMove).toHaveBeenCalledWith(subject);
  });
});
