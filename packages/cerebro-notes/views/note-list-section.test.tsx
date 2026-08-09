// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { afterEach, describe, it, expect, vi } from "vitest";
import {
  cleanup,
  createEvent,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { NoteSchema, type Note } from "../core";
import {
  NoteListSection,
  NoteSearchField,
  stepNoteId,
} from "./note-list-section";

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

  // FIR-4028 slice 9: the row says what is inside the note, not only when it
  // was last touched.
  it("counts how many task items are ticked off", () => {
    renderSection({
      notes: [
        note({
          body: "Launch\n- [x] Draft the post\n- [ ] Review it\n- [ ] Publish",
        }),
      ],
    });

    expect(screen.getByText("1/3")).toBeInTheDocument();
    expect(screen.getByTitle("1 of 3 tasks done")).toBeInTheDocument();
  });

  it("says nothing about tasks when the note has no checklist", () => {
    renderSection({ notes: [note({ body: "Just prose, no boxes." })] });

    expect(screen.queryByTitle(/tasks done/)).toBeNull();
  });

  it("shows how many comments a note carries", () => {
    renderSection({ notes: [note({ comment_count: 4 })] });

    expect(screen.getByTitle("4 comments")).toBeInTheDocument();
  });

  it("offers Move to… on the row, and reports which note to move", () => {
    const subject = note({ id: "n-9", folder_id: "folder-b" });
    const { onMove } = renderSection({ notes: [subject] });

    fireEvent.click(screen.getByRole("button", { name: "Actions for Roadmap" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Move to…" }));

    expect(onMove).toHaveBeenCalledWith(subject);
  });
});

// FIR-4028 slice 9: the arrow keys walk the list from inside the search field,
// so you can type a word and step through the matches without ever reaching for
// the mouse — and without the caret jumping around in the field.
describe("stepNoteId", () => {
  const ids = ["a", "b", "c"];

  it("starts at the first note when nothing is selected", () => {
    expect(stepNoteId(ids, null, 1)).toBe("a");
    expect(stepNoteId(ids, null, -1)).toBe("a");
  });

  it("walks one note at a time in the rendered order", () => {
    expect(stepNoteId(ids, "a", 1)).toBe("b");
    expect(stepNoteId(ids, "c", -1)).toBe("b");
  });

  it("stops at the ends instead of wrapping around", () => {
    expect(stepNoteId(ids, "c", 1)).toBe("c");
    expect(stepNoteId(ids, "a", -1)).toBe("a");
  });

  it("has nowhere to go in an empty list", () => {
    expect(stepNoteId([], null, 1)).toBeNull();
  });
});

describe("NoteSearchField", () => {
  function renderField() {
    const onStep = vi.fn();
    render(<NoteSearchField value="" onChange={() => {}} onStep={onStep} />);
    const input = screen.getByPlaceholderText("Search notes…");
    input.focus();
    return { onStep, input };
  }

  it("steps down the list without leaving the search field", () => {
    const { onStep, input } = renderField();

    const event = createEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent(input, event);

    expect(onStep).toHaveBeenCalledWith(1);
    // preventDefault keeps the caret still; focus staying put is the whole
    // point of stepping from the field rather than from the list.
    expect(event.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(input);
  });

  it("steps up the list too", () => {
    const { onStep, input } = renderField();

    fireEvent.keyDown(input, { key: "ArrowUp" });

    expect(onStep).toHaveBeenCalledWith(-1);
  });

  it("leaves every other key to the field", () => {
    const { onStep, input } = renderField();

    const event = createEvent.keyDown(input, { key: "ArrowLeft" });
    fireEvent(input, event);

    expect(onStep).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });
});
