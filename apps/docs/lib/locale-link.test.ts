import { describe, expect, it } from "vitest";
import { prefixLocale } from "./locale-link";

describe("prefixLocale", () => {
  it("prefixes root-relative paths with the active non-default locale", () => {
    expect(prefixLocale("/workspaces", "zh")).toBe("/zh/workspaces");
    expect(prefixLocale("/agents-create", "zh")).toBe("/zh/agents-create");
  });

  it("preserves anchors and query strings on prefixed paths", () => {
    expect(prefixLocale("/providers#claude-code", "zh")).toBe(
      "/zh/providers#claude-code",
    );
    expect(prefixLocale("/agents?from=docs", "zh")).toBe(
      "/zh/agents?from=docs",
    );
  });

  it("rewrites the bare root path to the locale root", () => {
    expect(prefixLocale("/", "zh")).toBe("/zh");
  });

  it("leaves the default language untouched (URLs are prefix-less)", () => {
    expect(prefixLocale("/workspaces", "en")).toBe("/workspaces");
    expect(prefixLocale("/", "en")).toBe("/");
  });

  it("does not double-prefix paths that already carry a known locale", () => {
    expect(prefixLocale("/zh/workspaces", "zh")).toBe("/zh/workspaces");
    expect(prefixLocale("/en/workspaces", "zh")).toBe("/en/workspaces");
  });

  it("leaves external URLs alone", () => {
    expect(prefixLocale("https://multica.ai/download", "zh")).toBe(
      "https://multica.ai/download",
    );
    expect(prefixLocale("mailto:hello@multica.ai", "zh")).toBe(
      "mailto:hello@multica.ai",
    );
    expect(prefixLocale("tel:+1234567890", "zh")).toBe("tel:+1234567890");
  });

  it("leaves in-page anchors and relative paths alone", () => {
    expect(prefixLocale("#section", "zh")).toBe("#section");
    expect(prefixLocale("./sibling", "zh")).toBe("./sibling");
    expect(prefixLocale("../sibling", "zh")).toBe("../sibling");
  });

  it("returns empty/undefined hrefs unchanged", () => {
    expect(prefixLocale("", "zh")).toBe("");
  });
});
