import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { Kbd, KbdGlyph, KBD_GLYPHS, type KbdGlyphName } from "./kbd";

describe("Kbd", () => {
  it("applies OpenType case feature and em-scaled classes", () => {
    const { container } = render(<Kbd>[</Kbd>);
    const kbd = container.querySelector("[data-slot='kbd']") as HTMLElement;
    expect(kbd).not.toBeNull();
    expect(kbd.style.fontFeatureSettings).toBe('"case" 1');
    expect(kbd.className).toContain("h-[1.85em]");
    expect(kbd.className).toContain("in-data-[slot=tooltip-content]:bg-white/10!");
  });

  it("supports md size and compact padding", () => {
    const { container } = render(
      <Kbd size="md" compact>
        K
      </Kbd>,
    );
    const kbd = container.querySelector("[data-slot='kbd']") as HTMLElement;
    expect(kbd.className).toContain("h-[2.15em]");
    expect(kbd.className).toContain("px-[0.42em]");
  });

  it("takes its ink from currentColor inside a primary button", () => {
    const { container } = render(<Kbd>K</Kbd>);
    const kbd = container.querySelector("[data-slot='kbd']") as HTMLElement;
    const inPrimary = "in-[[data-slot=button][data-variant=default]]";
    expect(kbd.className).toContain(`${inPrimary}:text-current!`);
    expect(kbd.className).toContain(`${inPrimary}:bg-current/14!`);
    expect(kbd.className).toContain(`${inPrimary}:border-current/25!`);
  });

  it("renders every optical glyph with proportional em width and shared 16-unit height", () => {
    for (const name of Object.keys(KBD_GLYPHS) as KbdGlyphName[]) {
      const { container, unmount } = render(<KbdGlyph name={name} />);
      const svg = container.querySelector(`[data-kbd-glyph="${name}"]`) as SVGElement;
      expect(svg).not.toBeNull();
      expect(svg.getAttribute("viewBox")).toBe(`0 0 ${KBD_GLYPHS[name].width} 16`);
      expect(svg.getAttribute("aria-hidden")).toBe("true");
      expect(svg.style.height).toBe("1.18em");
      expect(svg.style.width).toMatch(/em$/);
      unmount();
    }
  });
});
