import { describe, expect, it } from "vitest";
import {
  DEFAULT_DICTATION_SETTINGS,
  glossaryToHint,
  readDictationSettings,
} from "./dictation-settings";

describe("readDictationSettings", () => {
  it("returns the default when the blob is missing or not an object", () => {
    expect(readDictationSettings(undefined)).toEqual(DEFAULT_DICTATION_SETTINGS);
    expect(readDictationSettings(null)).toEqual(DEFAULT_DICTATION_SETTINGS);
    expect(readDictationSettings("nope")).toEqual(DEFAULT_DICTATION_SETTINGS);
  });

  it("keeps valid fields and falls back per-field on garbage", () => {
    expect(readDictationSettings({ glossary: "Helsebixen", cleanup: "yes" })).toEqual({
      glossary: "Helsebixen",
      cleanup: true, // garbage string → default (cleanup is on by default)
    });
    expect(readDictationSettings({ cleanup: false })).toEqual({
      glossary: "", // missing → default
      cleanup: false,
    });
  });
});

describe("glossaryToHint", () => {
  it("collapses newlines and commas into a trimmed, comma-separated hint", () => {
    expect(glossaryToHint("Helsebixen\nmade4men\n  jala ")).toBe(
      "Helsebixen, made4men, jala",
    );
    expect(glossaryToHint("a, b,,\n c")).toBe("a, b, c");
  });

  it("returns an empty string for blank input (no bias sent)", () => {
    expect(glossaryToHint("")).toBe("");
    expect(glossaryToHint("  \n , \n ")).toBe("");
  });
});
