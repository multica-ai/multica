import { describe, expect, it } from "vitest";
import { runtimeDisplayName } from "./display";

describe("runtimeDisplayName", () => {
  it("prefers a custom name when set", () => {
    expect(
      runtimeDisplayName({ name: "Claude (host)", custom_name: "Prod Box" }),
    ).toBe("Prod Box");
  });

  it("trims the custom name", () => {
    expect(
      runtimeDisplayName({ name: "Claude (host)", custom_name: "  Prod Box  " }),
    ).toBe("Prod Box");
  });

  it("falls back to the default name when custom is empty, whitespace, null, or missing", () => {
    expect(runtimeDisplayName({ name: "Claude (host)", custom_name: "" })).toBe(
      "Claude (host)",
    );
    expect(
      runtimeDisplayName({ name: "Claude (host)", custom_name: "   " }),
    ).toBe("Claude (host)");
    expect(
      runtimeDisplayName({ name: "Claude (host)", custom_name: null }),
    ).toBe("Claude (host)");
    expect(runtimeDisplayName({ name: "Claude (host)" })).toBe("Claude (host)");
  });
});
