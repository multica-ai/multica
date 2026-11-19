import { describe, expect, it } from "vitest";
import { parseAvatarEmoji } from "./avatar-emoji";

describe("parseAvatarEmoji", () => {
  it("reads the emoji the server assigns new agents", () => {
    expect(parseAvatarEmoji("emoji:🌙")).toBe("🌙");
    expect(parseAvatarEmoji("emoji: 🦊 ")).toBe("🦊");
  });

  it("leaves image URLs and empty markers alone", () => {
    expect(parseAvatarEmoji("https://cdn.multica.ai/a.png")).toBeNull();
    expect(parseAvatarEmoji("emoji:")).toBeNull();
    expect(parseAvatarEmoji(null)).toBeNull();
  });
});
