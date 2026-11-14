import { describe, expect, it } from "vitest";
import type { Fold } from "../../platform/fold-layout";
import { listColumnWidth } from "./split";

const margins = { top: 0, left: 0, bottom: 0, right: 0 };

describe("listColumnWidth", () => {
  it("keeps one column on a phone-sized screen", () => {
    expect(listColumnWidth(466, [])).toBeNull();
  });

  it("puts the divider on a hinge that runs top to bottom", () => {
    const hinge: Fold = { x: 465, y: 0, width: 20, height: 669, margins, active: false };
    expect(listColumnWidth(951, [hinge])).toBe(475);
  });

  it("ignores a hinge that runs across the screen", () => {
    const hinge: Fold = { x: 0, y: 465, width: 669, height: 20, margins, active: false };
    expect(listColumnWidth(669, [hinge])).toBe(288);
  });

  it("gives the list a bounded share without a hinge", () => {
    expect(listColumnWidth(1180, [])).toBe(420);
    expect(listColumnWidth(600, [])).toBe(280);
  });
});
