import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ArtifactFolder } from "@multica/core/types/artifact";

import { FolderBreadcrumb, folderPathChain } from "./folder-breadcrumb";

function folder(id: string, name: string, parentId: string | null = null) {
  return {
    id,
    name,
    parent_id: parentId,
    kind: "note",
    workspace_id: "ws",
    created_at: "",
    updated_at: "",
  } as unknown as ArtifactFolder;
}

const FOLDERS = [
  folder("firtal", "Firtal"),
  folder("q3", "Q3 planning", "firtal"),
  folder("other", "Other"),
];

describe("folderPathChain", () => {
  it("returns the folders from the root down to the given one", () => {
    expect(folderPathChain(FOLDERS, "q3").map((f) => f.name)).toEqual([
      "Firtal",
      "Q3 planning",
    ]);
  });

  it("returns nothing for a note that sits outside every folder", () => {
    expect(folderPathChain(FOLDERS, null)).toEqual([]);
  });

  it("stops instead of looping when a folder's parent is missing or circular", () => {
    const broken = [folder("a", "A", "b"), folder("b", "B", "a")];
    expect(folderPathChain(broken, "a").length).toBeLessThanOrEqual(2);
  });
});

describe("FolderBreadcrumb (FIR-4028 slice 8)", () => {
  it("shows the whole path, not just the folder the note sits in", () => {
    render(
      <FolderBreadcrumb
        folders={FOLDERS}
        folderId="q3"
        rootLabel="All notes"
        onOpenFolder={() => {}}
      />,
    );
    const crumbs = screen
      .getAllByTestId("folder-crumb")
      .map((el) => el.textContent);
    expect(crumbs).toEqual(["All notes", "Firtal", "Q3 planning"]);
  });

  it("navigates to the crumb that was clicked, and to the root by its own crumb", () => {
    const onOpenFolder = vi.fn();
    render(
      <FolderBreadcrumb
        folders={FOLDERS}
        folderId="q3"
        rootLabel="All notes"
        onOpenFolder={onOpenFolder}
      />,
    );
    screen.getByRole("button", { name: "Firtal" }).click();
    expect(onOpenFolder).toHaveBeenCalledWith("firtal");

    screen.getByRole("button", { name: "All notes" }).click();
    expect(onOpenFolder).toHaveBeenCalledWith(null);
  });

  it("renders the path as plain text when the surface cannot navigate", () => {
    render(
      <FolderBreadcrumb folders={FOLDERS} folderId="q3" rootLabel="All notes" />,
    );
    expect(screen.getAllByTestId("folder-crumb")).toHaveLength(3);
    expect(screen.queryByRole("button", { name: "Firtal" })).toBeNull();
  });

  it("offers the folder tree behind the chevron only when a tree is supplied", () => {
    const { rerender } = render(
      <FolderBreadcrumb folders={FOLDERS} folderId="q3" rootLabel="All notes" />,
    );
    expect(screen.queryByRole("button", { name: /change folder/i })).toBeNull();

    rerender(
      <FolderBreadcrumb folders={FOLDERS} folderId="q3" rootLabel="All notes">
        <div>tree</div>
      </FolderBreadcrumb>,
    );
    expect(
      screen.getByRole("button", { name: /change folder/i }),
    ).toBeInTheDocument();
  });
});
