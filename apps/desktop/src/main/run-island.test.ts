// @vitest-environment node
import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({}));
vi.mock("@electron-toolkit/utils", () => ({ is: { dev: false } }));
vi.mock("../preload/run-island.cjs?asset", () => ({
  default: "/preload/run-island.cjs",
}));
import { defaultIslandProfile } from "../shared/run-island";
import { islandLayout } from "./run-island";

describe("Run Island bounds in display coordinates", () => {
  const bounds = { x: -1496, y: -82, width: 1496, height: 966 };
  const display = {
    bounds,
    internal: true,
    workArea: { ...bounds, y: -49, height: 930 },
  };

  it("reserves the camera area on a built-in display and grows downward from the same centre", () => {
    const cap = islandLayout(display, false, 2);
    const open = islandLayout(display, true, 2);
    expect(cap.attached).toBe(true);
    expect(cap.bounds.y).toBe(bounds.y);
    expect(open.bounds.y).toBe(cap.bounds.y);
    expect(open.bounds.x + open.bounds.width / 2).toBe(
      cap.bounds.x + cap.bounds.width / 2,
    );
    expect(open.bounds.height).toBeGreaterThan(cap.bounds.height);
  });

  it("floats below the menu on an external or standard built-in display and caps long lists", () => {
    expect(
      islandLayout({ ...display, internal: false }, false, 0).bounds.y,
    ).toBe(display.workArea.y + 4);
    expect(
      islandLayout(
        { ...display, workArea: { ...bounds, y: bounds.y + 24 } },
        true,
        5,
      ).attached,
    ).toBe(false);
    expect(islandLayout(display, true, 150).bounds).toEqual(
      islandLayout(display, true, 5).bounds,
    );
  });
  it("fits three agent initials on the right of a centered MacBook camera cutout", () => {
    const cap = islandLayout(display, false, 3, {
      ...defaultIslandProfile,
      trailing: "agents",
    });
    const rightWing = (cap.bounds.width - 200) / 2;
    expect(rightWing).toBeGreaterThanOrEqual(13 + 3 * 14 + 2 * 2);
  });
});
