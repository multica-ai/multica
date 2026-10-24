// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { DIRECTION_STORAGE_KEY, loadDirection } from "./direction";
import { isPreviewSettings } from "./protocol";
import { emptyDraft } from "./tokens";

afterEach(() => vi.unstubAllGlobals());

describe("UI Lab layout direction", () => {
  it("defaults to left-to-right and restores only a saved ltr or rtl", () => {
    for (const saved of [null, "", "auto", "RTL", "garbage"]) {
      vi.stubGlobal("localStorage", {
        getItem: vi.fn().mockReturnValue(saved),
      });
      expect(loadDirection()).toBe("ltr");
    }
    for (const saved of ["ltr", "rtl"]) {
      const getItem = vi.fn().mockReturnValue(saved);
      vi.stubGlobal("localStorage", { getItem });
      expect(loadDirection()).toBe(saved);
      expect(getItem).toHaveBeenCalledWith(DIRECTION_STORAGE_KEY);
    }
    vi.stubGlobal("localStorage", {
      getItem: () => {
        throw new Error("Storage blocked");
      },
    });
    expect(loadDirection()).toBe("ltr");
  });

  it("forwards only a valid direction to the preview iframe", () => {
    const message = {
      type: "multica-ui-lab:preview",
      selectedColor: "--brand",
      playbackSpeed: 1,
      scene: "list",
      theme: "light",
      buttonScale: "default",
      locale: "en",
      draft: emptyDraft(),
    };
    for (const direction of ["ltr", "rtl"])
      expect(isPreviewSettings({ ...message, direction })).toBe(true);
    for (const direction of [undefined, null, "", "auto", "RTL", 1])
      expect(isPreviewSettings({ ...message, direction })).toBe(false);
  });
});
