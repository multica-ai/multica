/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { FileTree, type FileTreeActions } from "./file-tree";

afterEach(cleanup);

const PATHS = ["SKILL.md", "references/api.md", "notes.txt"];

function makeActions(overrides: Partial<FileTreeActions> = {}): FileTreeActions {
  return {
    validatePath: () => "",
    onRename: vi.fn(),
    onDelete: vi.fn(),
    reservedPath: "SKILL.md",
    ...overrides,
  };
}

describe("FileTree row actions", () => {
  it("offers none without actions, so a read-only tree stays read-only", async () => {
    await renderWithI18n(
      <FileTree filePaths={PATHS} selectedPath="SKILL.md" onSelect={vi.fn()} />,
    );

    expect(screen.queryByRole("button", { name: /notes\.txt/ })).toBeNull();
  });

  it("withholds them from the reserved file, which owns the skill's content", async () => {
    await renderWithI18n(
      <FileTree
        filePaths={PATHS}
        selectedPath="SKILL.md"
        onSelect={vi.fn()}
        actions={makeActions()}
      />,
    );

    // Deleting or renaming SKILL.md is refused by the server, so the row must
    // not present it as available.
    const rows = screen.getAllByRole("tab");
    const skillMd = rows.find((row) => row.textContent === "SKILL.md")!;
    expect(skillMd.parentElement!.querySelector("[aria-label]")).toBeNull();
  });

  it("deletes the row acted on, not whichever file happens to be open", async () => {
    const onDelete = vi.fn();
    await renderWithI18n(
      <FileTree
        filePaths={PATHS}
        selectedPath="SKILL.md"
        onSelect={vi.fn()}
        actions={makeActions({ onDelete })}
      />,
    );

    await userEvent.click(screen.getByRole("button", { name: /notes\.txt/ }));
    await userEvent.click(await screen.findByRole("menuitem", { name: /删除|Delete/ }));

    expect(onDelete).toHaveBeenCalledWith("notes.txt");
  });

  it("renames in place and reports the new path", async () => {
    const onRename = vi.fn();
    await renderWithI18n(
      <FileTree
        filePaths={PATHS}
        selectedPath="notes.txt"
        onSelect={vi.fn()}
        actions={makeActions({ onRename })}
      />,
    );

    await userEvent.click(screen.getByRole("button", { name: /notes\.txt/ }));
    await userEvent.click(
      await screen.findByRole("menuitem", { name: /重命名|Rename/ }),
    );

    const input = screen.getByRole("textbox");
    await userEvent.clear(input);
    await userEvent.type(input, "notes.md{Enter}");

    expect(onRename).toHaveBeenCalledWith("notes.txt", "notes.md");
  });

  it("keeps a rejected path in the editor rather than committing it", async () => {
    const onRename = vi.fn();
    await renderWithI18n(
      <FileTree
        filePaths={PATHS}
        selectedPath="notes.txt"
        onSelect={vi.fn()}
        actions={makeActions({
          onRename,
          validatePath: () => "already exists",
        })}
      />,
    );

    await userEvent.click(screen.getByRole("button", { name: /notes\.txt/ }));
    await userEvent.click(
      await screen.findByRole("menuitem", { name: /重命名|Rename/ }),
    );

    const input = screen.getByRole("textbox");
    await userEvent.clear(input);
    await userEvent.type(input, "references/api.md{Enter}");

    expect(onRename).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("already exists");
  });
});

describe("FileTree entry points", () => {
  it("opens the same menu from a right-click on the row", async () => {
    await renderWithI18n(
      <FileTree
        filePaths={PATHS}
        selectedPath="SKILL.md"
        onSelect={vi.fn()}
        actions={makeActions()}
      />,
    );

    const row = screen.getAllByRole("tab").find((r) => r.textContent === "notes.txt")!
      .parentElement!;
    // Right-click is the entry point that leaves no visual trace, so it is the
    // one most likely to silently stop working.
    await userEvent.pointer({ target: row, keys: "[MouseRight]" });

    expect(
      await screen.findByRole("menuitem", { name: /重命名|Rename/ }),
    ).toBeInTheDocument();
  });

});
