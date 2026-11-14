import { describe, expect, it } from "vitest";
import { themeAgentPrompt, themeIdForDescription } from "./theme-agent";

describe("themeIdForDescription", () => {
  it("names the theme after the description's words", () => {
    expect(themeIdForDescription("A calm theme inspired by Tokyo at night, with neon pink", [])).toBe(
      "calm-tokyo-night-neon",
    );
    expect(themeIdForDescription("Café crème", [])).toBe("cafe-creme");
  });

  it("falls back to my-theme when no Latin word is left", () => {
    expect(themeIdForDescription("东京夜晚的霓虹主题", [])).toBe("my-theme");
    expect(themeIdForDescription("make a theme", [])).toBe("my-theme");
  });

  it("does not reuse an installed or built-in id", () => {
    expect(themeIdForDescription("东京", ["my-theme", "my-theme-2"])).toBe("my-theme-3");
    expect(themeIdForDescription("github", [])).toBe("github-2");
  });
});

describe("themeAgentPrompt", () => {
  it("asks for the theme in the folder Multica watches, with a zip when the folder is elsewhere", () => {
    const prompt = themeAgentPrompt({
      description: "  Night sky behind frosted glass  ",
      id: "night-sky",
      folder: "/Users/ada/Library/Application Support/Multica/themes/",
    });
    expect(prompt).toContain("\nNight sky behind frosted glass\n");
    expect(prompt).toContain("`night-sky`. Keep it for every later change");
    expect(prompt).toContain("`/Users/ada/Library/Application Support/Multica/themes/night-sky`");
    expect(prompt).toContain("attach it to your reply");
    expect(prompt).toContain("node multica-themes/scripts/theme-check.mjs");
    expect(prompt).toContain("0 errors");
  });

  it("joins a Windows folder with its own separator", () => {
    const prompt = themeAgentPrompt({ description: "x", id: "x", folder: "C:\\Users\\ada\\AppData\\Multica\\themes" });
    expect(prompt).toContain("`C:\\Users\\ada\\AppData\\Multica\\themes\\x`");
  });

  it("asks for a zip on web", () => {
    const prompt = themeAgentPrompt({ description: "Paper", id: "paper", folder: null });
    expect(prompt).toContain("`paper.zip`");
    expect(prompt).not.toContain("watches that folder");
  });
});
