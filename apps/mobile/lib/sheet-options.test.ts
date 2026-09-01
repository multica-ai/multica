// @vitest-environment node
import { describe, expect, it } from "vitest";
import { sheetScreenOptions } from "./sheet-options";

describe("sheetScreenOptions", () => {
  it("keeps iOS formSheet detents and grabber", () => {
    expect(sheetScreenOptions("ios")).toMatchObject({
      presentation: "formSheet",
      sheetGrabberVisible: true,
      sheetAllowedDetents: [0.6, 0.95],
      headerShown: false,
    });
  });

  it("falls Android back to a bottom-entering modal", () => {
    expect(sheetScreenOptions("android")).toEqual({
      presentation: "modal",
      animation: "slide_from_bottom",
      gestureEnabled: true,
      contentStyle: { flex: 1 },
      headerShown: false,
    });
  });
});
