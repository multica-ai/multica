import { describe, expect, it } from "vitest";
import { MAX_VOICE_HOTWORDS, voiceHotwords } from "./hotwords";

describe("voiceHotwords", () => {
  it("puts vocabulary first and drops case-insensitive repeats and blanks", () => {
    expect(voiceHotwords(["Multica", " "], ["Lambda", "MULTICA", " Jiayuan "])).toEqual([
      "Multica",
      "Lambda",
      "Jiayuan",
    ]);
  });

  it("caps the list", () => {
    const context = Array.from({ length: 100 }, (_, i) => `name${i}`);
    const words = voiceHotwords(["Mine"], context);
    expect(words).toHaveLength(MAX_VOICE_HOTWORDS);
    expect(words[0]).toBe("Mine");
  });
});
