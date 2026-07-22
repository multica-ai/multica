import { describe, expect, it } from "vitest";
import { runtimeDisplayLabel, runtimeDisplayName } from "./display";

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

describe("runtimeDisplayLabel", () => {
  it("re-attaches the provider when a custom alias hides it", () => {
    expect(
      runtimeDisplayLabel({
        name: "Codex (EvaM2.local)",
        custom_name: "evam2",
        provider: "codex",
      }),
    ).toBe("evam2 (Codex)");
  });

  it("returns the daemon name unchanged when no alias is set", () => {
    expect(
      runtimeDisplayLabel({
        name: "Codex (EvaM2.local)",
        custom_name: "",
        provider: "codex",
      }),
    ).toBe("Codex (EvaM2.local)");
    expect(
      runtimeDisplayLabel({
        name: "Codex (EvaM2.local)",
        custom_name: null,
        provider: "codex",
      }),
    ).toBe("Codex (EvaM2.local)");
  });

  it("omits the provider suffix when the provider is empty", () => {
    expect(
      runtimeDisplayLabel({ name: "host", custom_name: "evam2", provider: "" }),
    ).toBe("evam2");
  });

  it("uses the daemon's provider display name for overridden slugs", () => {
    // Trae and Qwen use display names that differ from title-cased slugs;
    // aliases must match the daemon's no-alias names.
    expect(
      runtimeDisplayLabel({
        name: "Trae (host)",
        custom_name: "box",
        provider: "traecli",
      }),
    ).toBe("box (Trae)");
    expect(
      runtimeDisplayLabel({
        name: "Qwen Code (host)",
        custom_name: "box",
        provider: "qwen",
      }),
    ).toBe("box (Qwen Code)");
  });

  it("first-letter-capitalizes non-overridden slugs, matching the daemon", () => {
    // Providers without an explicit daemon override are first-letter
    // capitalized on both the alias and no-alias paths, so the label must match
    // the daemon (e.g. no-alias name is "Openclaw (host)").
    expect(
      runtimeDisplayLabel({
        name: "Openclaw (host)",
        custom_name: "box",
        provider: "openclaw",
      }),
    ).toBe("box (Openclaw)");
    expect(
      runtimeDisplayLabel({
        name: "Codex (host)",
        custom_name: "box",
        provider: "codex",
      }),
    ).toBe("box (Codex)");
  });
});
