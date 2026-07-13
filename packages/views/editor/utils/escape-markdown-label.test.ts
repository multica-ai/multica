import { describe, it, expect } from "vitest";
import { escapeMarkdownLabel } from "./escape-markdown-label";

describe("escapeMarkdownLabel", () => {
  it("escapes [ and ]", () => {
    expect(escapeMarkdownLabel("photo[1].png")).toBe("photo\\[1\\].png");
  });

  it("escapes backslash", () => {
    expect(escapeMarkdownLabel("a\\b")).toBe("a\\\\b");
  });

  it("escapes ( and )", () => {
    expect(escapeMarkdownLabel("file(1).txt")).toBe("file\\(1\\).txt");
  });

  it("escapes all special chars together", () => {
    expect(escapeMarkdownLabel("6P4N\\`X[A~Z(S@XO}WE0FT_P.jpg")).toBe(
      "6P4N\\\\`X\\[A~Z\\(S@XO}WE0FT_P.jpg",
    );
  });

  it("leaves normal text unchanged", () => {
    expect(escapeMarkdownLabel("hello world.png")).toBe("hello world.png");
  });
});
