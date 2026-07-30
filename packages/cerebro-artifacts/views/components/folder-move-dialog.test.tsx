import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ArtifactFolder } from "@multica/core/types";
import { FolderMoveDialog, buildFolderChoices } from "./folder-move-dialog";

// FIR-4163: the move picker is shared by Documents and Notes, so its behaviour
// is tested once here rather than through either page.

function folder(
  id: string,
  name: string,
  parent_id: string | null = null,
): ArtifactFolder {
  return {
    id,
    workspace_id: "ws-1",
    parent_id,
    name,
    kind: "document",
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
  };
}

// Two folders share the name "2026" on purpose — the path is the only thing
// that tells them apart, which is why the flat list it replaced was unusable.
const folders: ArtifactFolder[] = [
  folder("finance", "Finance"),
  folder("finance-2026", "2026", "finance"),
  folder("finance-q1", "Q1", "finance-2026"),
  folder("sales", "Sales"),
  folder("sales-2026", "2026", "sales"),
];

describe("buildFolderChoices", () => {
  it("lists every folder depth-first with its full path", () => {
    expect(
      buildFolderChoices(folders).map((c) => [c.path, c.depth]),
    ).toEqual([
      ["Finance", 0],
      ["Finance / 2026", 1],
      ["Finance / 2026 / Q1", 2],
      ["Sales", 0],
      ["Sales / 2026", 1],
    ]);
  });

  it("sorts siblings alphabetically", () => {
    const unsorted = [folder("b", "Beta"), folder("a", "Alpha")];
    expect(buildFolderChoices(unsorted).map((c) => c.path)).toEqual([
      "Alpha",
      "Beta",
    ]);
  });
});

describe("FolderMoveDialog", () => {
  function renderDialog(
    props: Partial<React.ComponentProps<typeof FolderMoveDialog>> = {},
  ) {
    const onMove = vi.fn();
    render(
      <FolderMoveDialog
        open
        onOpenChange={() => {}}
        folders={folders}
        subject="Quarterly numbers"
        currentFolderId={null}
        rootLabel="All documents"
        onMove={onMove}
        {...props}
      />,
    );
    return { onMove };
  }

  it("shows the path of every nested destination", () => {
    renderDialog();

    expect(screen.getByText("Finance / 2026 / Q1")).toBeInTheDocument();
    expect(screen.getByText("Sales / 2026")).toBeInTheDocument();
    // Root-level folders need no path — their name already is one.
    expect(screen.queryByText("Finance / Finance")).toBeNull();
  });

  it("moves to the picked folder", () => {
    const { onMove } = renderDialog();

    fireEvent.click(screen.getByText("Finance / 2026 / Q1"));
    fireEvent.click(screen.getByRole("button", { name: "Move here" }));

    expect(onMove).toHaveBeenCalledWith("finance-q1");
  });

  it("filters on the path, so a parent name keeps its children", () => {
    renderDialog();

    fireEvent.change(screen.getByPlaceholderText("Search folders…"), {
      target: { value: "finance" },
    });

    expect(screen.getByText("Finance / 2026")).toBeInTheDocument();
    expect(screen.getByText("Finance / 2026 / Q1")).toBeInTheDocument();
    expect(screen.queryByText("Sales / 2026")).toBeNull();
  });

  it("says so when nothing matches", () => {
    renderDialog();

    fireEvent.change(screen.getByPlaceholderText("Search folders…"), {
      target: { value: "payroll" },
    });

    expect(screen.getByText(/No folder matches/)).toBeInTheDocument();
  });

  it("cannot move to the folder it already sits in", () => {
    const { onMove } = renderDialog({ currentFolderId: "sales" });

    const confirm = screen.getByRole("button", { name: "Move here" });
    expect(confirm).toBeDisabled();

    fireEvent.click(screen.getByText("Finance"));
    expect(confirm).not.toBeDisabled();
    fireEvent.click(confirm);

    expect(onMove).toHaveBeenCalledWith("finance");
  });

  it("offers the root as a destination", () => {
    const { onMove } = renderDialog({ currentFolderId: "sales" });

    fireEvent.click(screen.getByText("All documents"));
    fireEvent.click(screen.getByRole("button", { name: "Move here" }));

    expect(onMove).toHaveBeenCalledWith(null);
  });
});
