/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { OfficeRoom } from "@multica/core/agents";
import { renderWithI18n } from "../../test/i18n";
import { OfficeRoomMenu } from "./office-room-menu";

const NAMES: Record<OfficeRoom, string> = {
  product: "Product development",
  research: "Market research",
  planning: "Planning",
  general: "General",
};

async function openMenu(overrides: Partial<Parameters<typeof OfficeRoomMenu>[0]> = {}) {
  const props = {
    current: "general" as OfficeRoom,
    occupancy: { product: 1, research: 4, planning: 0, general: 2 },
    roomNames: NAMES,
    onMove: vi.fn(),
    ...overrides,
  };
  renderWithI18n(<OfficeRoomMenu {...props} />);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Room" }));
  return { props, user };
}

describe("OfficeRoomMenu", () => {
  afterEach(() => {
    cleanup();
  });

  it("shows the room the agent is in on its button", () => {
    renderWithI18n(
      <OfficeRoomMenu
        current="planning"
        occupancy={{ product: 0, research: 0, planning: 1, general: 0 }}
        roomNames={NAMES}
        onMove={() => {}}
      />,
    );

    expect(screen.getByRole("button", { name: "Room" }).textContent).toContain("Planning");
  });

  it("moves the agent to a room with a desk free", async () => {
    const { props, user } = await openMenu();

    await user.click(await screen.findByRole("menuitem", { name: /Planning/ }));

    expect(props.onMove).toHaveBeenCalledWith("planning");
  });

  it("cannot choose the room the agent is already in", async () => {
    const { props, user } = await openMenu();

    const here = await screen.findByRole("menuitem", { name: /General/ });
    expect(here.getAttribute("aria-disabled")).toBe("true");
    await user.click(here);

    expect(props.onMove).not.toHaveBeenCalled();
  });

  it("cannot choose a room with no desk left, and says how many desks the others have taken", async () => {
    await openMenu();

    const full = await screen.findByRole("menuitem", { name: /Market research/ });
    expect(full.getAttribute("aria-disabled")).toBe("true");
    expect(full.textContent).toContain("Full");
    expect((await screen.findByRole("menuitem", { name: /Product development/ })).textContent).toContain("1 / 4");
  });
});
