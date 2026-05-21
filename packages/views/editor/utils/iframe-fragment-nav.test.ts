import { describe, it, expect } from "vitest";
import {
  withFragmentNavShim,
  __FRAGMENT_NAV_SHIM__,
} from "./iframe-fragment-nav";

describe("withFragmentNavShim", () => {
  it("returns just the shim for undefined input", () => {
    expect(withFragmentNavShim(undefined)).toBe(__FRAGMENT_NAV_SHIM__);
  });

  it("returns just the shim for empty input", () => {
    expect(withFragmentNavShim("")).toBe(__FRAGMENT_NAV_SHIM__);
  });

  it("appends the shim after non-empty HTML", () => {
    const html = "<p>hello</p>";
    const result = withFragmentNavShim(html);

    expect(result.startsWith(html)).toBe(true);
    expect(result.endsWith(__FRAGMENT_NAV_SHIM__)).toBe(true);
  });

  it("appends the shim verbatim without transformation", () => {
    const html = "<p>hello</p>";
    const result = withFragmentNavShim(html);

    expect(result.slice(html.length)).toBe(__FRAGMENT_NAV_SHIM__);
    expect(__FRAGMENT_NAV_SHIM__).not.toContain("</body>");
  });

  it("is deterministic for the same input", () => {
    const html = "<section><a href=\"#x\">jump</a></section>";

    expect(withFragmentNavShim(html)).toBe(withFragmentNavShim(html));
  });
});
