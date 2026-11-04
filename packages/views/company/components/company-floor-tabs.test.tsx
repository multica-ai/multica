/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CompanyFloor } from "@multica/core/company";
import { renderWithI18n } from "../../test/i18n";
import { CompanyFloorTabs } from "./company-floor-tabs";

const mutations = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), remove: vi.fn() }));

vi.mock("@multica/core/company", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/company")>()),
  useCreateCompanyFloor: () => ({ mutate: mutations.create, isPending: false }),
  useUpdateCompanyFloor: () => ({ mutate: mutations.update }),
  useDeleteCompanyFloor: () => ({ mutate: mutations.remove }),
}));

function floor(id: string, name: string, position: number): CompanyFloor {
  return { id, name, position, workspace_id: "ws-1", created_at: "", updated_at: "" };
}

const FLOORS = [floor("f1", "Research", 0), floor("f2", "Build", 1), floor("f3", "Launch", 2)];

describe("CompanyFloorTabs", () => {
  const onSelect = vi.fn();

  beforeEach(() => {
    Object.values(mutations).forEach((fn) => fn.mockReset());
    onSelect.mockReset();
  });
  afterEach(cleanup);

  function setup(props: Partial<Parameters<typeof CompanyFloorTabs>[0]> = {}) {
    return renderWithI18n(
      <CompanyFloorTabs
        floors={FLOORS}
        currentId="f2"
        occupied={new Set(["f1"])}
        canManage
        onSelect={onSelect}
        {...props}
      />,
    );
  }

  it("selects a floor and marks the current one", () => {
    setup();

    expect(screen.getByRole("tab", { name: "Build" }).getAttribute("aria-selected")).toBe("true");
    fireEvent.click(screen.getByRole("tab", { name: "Launch" }));

    expect(onSelect).toHaveBeenCalledWith("f3");
  });

  it("renames a floor in place on double-click", () => {
    setup();

    fireEvent.doubleClick(screen.getByRole("tab", { name: "Launch" }));
    const field = screen.getByRole("textbox", { name: "Rename" });
    fireEvent.change(field, { target: { value: "  Ship  " } });
    fireEvent.keyDown(field, { key: "Enter" });

    expect(mutations.update).toHaveBeenCalledWith({ id: "f3", name: "Ship" }, expect.any(Object));
  });

  it("keeps the name when the rename is cancelled with Escape", () => {
    setup();

    fireEvent.doubleClick(screen.getByRole("tab", { name: "Launch" }));
    const field = screen.getByRole("textbox", { name: "Rename" });
    fireEvent.change(field, { target: { value: "Ship" } });
    fireEvent.keyDown(field, { key: "Escape" });

    expect(mutations.update).not.toHaveBeenCalled();
    expect(screen.getByRole("tab", { name: "Launch" })).toBeTruthy();
  });

  it("adds a floor under the next free default name and opens it", () => {
    setup();

    fireEvent.click(screen.getByRole("button", { name: "Add floor" }));

    expect(mutations.create).toHaveBeenCalledWith({ name: "Floor 4" }, expect.any(Object));
    mutations.create.mock.calls[0]![1].onSuccess({ id: "f4" });
    expect(onSelect).toHaveBeenCalledWith("f4");
  });

  it("moves the current floor one place", async () => {
    const user = userEvent.setup();
    setup();

    await user.click(screen.getByRole("button", { name: "Build options" }));
    await user.click(await screen.findByRole("menuitem", { name: "Move left" }));

    expect(mutations.update).toHaveBeenCalledWith({ id: "f2", position: 0 }, expect.any(Object));
  });

  it("deletes an empty floor and refuses one that holds a squad", async () => {
    const user = userEvent.setup();
    const { rerender } = setup();

    await user.click(screen.getByRole("button", { name: "Build options" }));
    await user.click(await screen.findByRole("menuitem", { name: "Delete floor" }));
    expect(mutations.remove).toHaveBeenCalledWith("f2", expect.any(Object));

    rerender(
      <CompanyFloorTabs floors={FLOORS} currentId="f1" occupied={new Set(["f1"])} canManage onSelect={onSelect} />,
    );
    await user.click(await screen.findByRole("button", { name: "Research options" }));
    expect((await screen.findByRole("menuitem", { name: "Delete floor" })).getAttribute("aria-disabled")).toBe("true");
  });

  it("only switches floors for someone who cannot manage them", () => {
    setup({ canManage: false });

    fireEvent.doubleClick(screen.getByRole("tab", { name: "Launch" }));

    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.queryByRole("button", { name: "Add floor" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Build options" })).toBeNull();
  });

  it("dots a floor with a decision waiting", () => {
    setup({ attention: new Set(["f3"]) });

    expect(within(screen.getByRole("tab", { name: /Launch/ })).getByRole("img", { name: "Waiting for a decision" })).toBeTruthy();
    expect(within(screen.getByRole("tab", { name: "Build" })).queryByRole("img")).toBeNull();
  });
});
