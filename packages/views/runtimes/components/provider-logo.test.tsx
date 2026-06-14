// @vitest-environment jsdom

import { describe, it, expect } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ProviderLogo } from "./provider-logo";

describe("ProviderLogo", () => {
  it("renders the manifest-supplied icon when iconUrl is set", () => {
    // External runtime extensions ship `icon_url` in their runtime.json.
    // We must render that URL verbatim — not look up a hardcoded SVG —
    // so any custom internal CLI logo flows through without code changes.
    render(
      <ProviderLogo
        provider="lightbox-coder"
        iconUrl="https://example.com/lb.svg"
      />,
    );
    const img = screen.getByRole("img", { name: "lightbox-coder" });
    expect(img).toBeTruthy();
    expect(img.getAttribute("src")).toBe("https://example.com/lb.svg");
  });

  it("falls back to the built-in SVG when iconUrl is empty", () => {
    // Built-in providers always omit iconUrl; the switch table must still
    // light up. Using a known provider here keeps the assertion simple
    // (claude renders an inline <svg>, not an <img>).
    const { container } = render(<ProviderLogo provider="claude" />);
    expect(container.querySelector("svg")).toBeTruthy();
  });

  it("renders the generic Monitor fallback for an unknown provider with no iconUrl", () => {
    // Unknown provider + no iconUrl is the worst case we still need to
    // handle gracefully — render the fallback Monitor icon so the row
    // doesn't visually collapse.
    const { container } = render(<ProviderLogo provider="totally-new" />);
    expect(container.querySelector("svg")).toBeTruthy();
  });

  it("falls back when a manifest-supplied icon fails to load", () => {
    const { container } = render(
      <ProviderLogo
        provider="totally-new"
        iconUrl="https://example.com/missing.svg"
      />,
    );
    fireEvent.error(screen.getByRole("img", { name: "totally-new" }));

    expect(screen.queryByRole("img", { name: "totally-new" })).toBeNull();
    expect(container.querySelector("svg")).toBeTruthy();
  });

  it("preserves the className across both render paths", () => {
    // Prevent a regression where a custom className silently gets
    // dropped on the iconUrl path (would make the logo too big or too
    // small in any list that customises sizing).
    const { container: imgContainer } = render(
      <ProviderLogo
        provider="x"
        iconUrl="https://example.com/x.png"
        className="h-8 w-8"
      />,
    );
    expect(
      imgContainer.querySelector("img")?.className.includes("h-8"),
    ).toBe(true);

    const { container: svgContainer } = render(
      <ProviderLogo provider="claude" className="h-3 w-3" />,
    );
    expect(
      svgContainer.querySelector("svg")?.getAttribute("class")?.includes("h-3"),
    ).toBe(true);
  });
});
