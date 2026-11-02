// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ListGrid } from "@multica/ui/components/ui/list-grid";
import { FolderExplorerBreadcrumbs } from "./folder-explorer-breadcrumbs";
import { FolderExplorerRow } from "./folder-explorer-row";
import { NewFolderButton } from "./new-folder-button";

describe("folder explorer components", () => {
  it("navigates from every breadcrumb, including the root", async () => {
    const onNavigate = vi.fn();
    render(
      <FolderExplorerBreadcrumbs
        ariaLabel="Folder location"
        rootLabel="Skills"
        path={[
          { id: "a", name: "Alpha", parentId: null, managed: false },
          { id: "b", name: "Beta", parentId: "a", managed: false },
        ]}
        onNavigate={onNavigate}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Skills" }));
    expect(onNavigate).toHaveBeenLastCalledWith(null);
    await userEvent.click(screen.getByRole("button", { name: "Alpha" }));
    expect(onNavigate).toHaveBeenLastCalledWith("a");
    expect(screen.getByRole("button", { name: "Beta" }).getAttribute("aria-current")).toBe("page");
  });

  it("opens a folder from the row and from its keyboard-accessible button", async () => {
    const onOpen = vi.fn();
    render(
      <ListGrid className="grid-cols-[1fr]">
        <FolderExplorerRow
          name="Engineering"
          typeLabel="Package"
          countLabel="3 skills"
          openLabel="Open Engineering"
          onOpen={onOpen}
        />
      </ListGrid>,
    );
    const button = screen.getByRole("button", { name: "Open Engineering" });
    button.focus();
    await userEvent.keyboard("{Enter}");
    expect(onOpen).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByText("3 skills"));
    expect(onOpen).toHaveBeenCalledTimes(2);
    expect(button.closest(".h-12")).toBeTruthy();
  });

  it("exposes expansion state and renders 20px depth guides", () => {
    render(
      <ListGrid>
        <FolderExplorerRow
          name="Nested"
          countLabel="1 skill"
          openLabel="Collapse Nested"
          onOpen={vi.fn()}
          expanded
          depth={2}
        />
      </ListGrid>,
    );
    const button = screen.getByRole("button", { name: "Collapse Nested" });
    expect(button.getAttribute("aria-expanded")).toBe("true");
    expect(button.querySelector("svg")?.getAttribute("class")).toContain("rotate-90");
    expect(button.querySelectorAll(".bg-border")).toHaveLength(2);
    expect(button.querySelector('[style*="width: 40px"]')).not.toBeNull();
  });

  it("opens a full-width directory with Enter and isolates its action button", async () => {
    const onOpen = vi.fn();
    const onAction = vi.fn();
    render(<ListGrid>
      <FolderExplorerRow layout="container" name="Design" countLabel="2 agents"
        openLabel="Open Design" onOpen={onOpen} actions={<button onClick={onAction}>Actions</button>} />
    </ListGrid>);
    const open = screen.getByRole("button", { name: "Open Design" });
    open.focus();
    await userEvent.keyboard("{Enter}");
    expect(onOpen).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole("button", { name: "Actions" }));
    expect(onAction).toHaveBeenCalledTimes(1);
    expect(onOpen).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole("row"));
    expect(onOpen).toHaveBeenCalledTimes(2);
    expect(screen.getAllByRole("cell")).toHaveLength(1);
  });

  it("keeps the managed-folder reason visible beside the disabled action", () => {
    render(
      <NewFolderButton
        label="New folder"
        disabled
        disabledReason="Package folders follow the package."
        onClick={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: "New folder" }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByText("Package folders follow the package.")).toBeTruthy();
  });
});
