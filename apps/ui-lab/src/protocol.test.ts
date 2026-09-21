// @vitest-environment node
import { describe, expect, it } from "vitest";
import { emptyDraft, numericTokenValue } from "./tokens";
import { isDialogCommand, isPreviewSettings } from "./protocol";

describe("motion preview boundary", () => {
  it("accepts only supported playback speeds and commands", () => {
    const message = {
      type: "multica-ui-lab:preview",
      selectedColor: "--brand",
      draft: emptyDraft(),
      theme: "light",
      scene: "motion",
      buttonScale: "default",
      locale: "en",
    };
    for (const playbackSpeed of [1, 0.5, 0.25])
      expect(isPreviewSettings({ ...message, playbackSpeed })).toBe(true);
    for (const playbackSpeed of [0, -1, 2, Infinity, "1", null, undefined])
      expect(isPreviewSettings({ ...message, playbackSpeed })).toBe(false);
    for (const action of ["open", "close", "replay"])
      expect(isDialogCommand({ type: "multica-ui-lab:dialog", action })).toBe(
        true,
      );
    for (const value of [
      null,
      {},
      { type: "multica-ui-lab:dialog", action: "execute" },
      { type: "other", action: "open" },
    ])
      expect(isDialogCommand(value)).toBe(false);
  });
  it("formats scrubbed numeric values with their declared units", () => {
    expect(numericTokenValue("--dialog-enter-duration", 320)).toBe("320ms");
    expect(numericTokenValue("--button-height-default", 32)).toBe("32px");
    expect(() => numericTokenValue("--unknown", 32)).toThrow();
  });
});
