import { render, screen, waitFor, cleanup } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CommentSelectionBubble } from "./comment-selection-bubble";

beforeEach(() => {
  Object.defineProperty(document.documentElement, "clientWidth", { configurable: true, value: 1024 });
  Object.defineProperty(document.documentElement, "clientHeight", { configurable: true, value: 768 });
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(24);
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(24);
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function fixture(tops: number[], right = 1024) {
  const source = document.createElement("div");
  source.textContent = "Selected text";
  document.body.append(source);
  const geometry = { right, tops };
  vi.spyOn(source, "getBoundingClientRect").mockImplementation(() => new DOMRect(10, 10, geometry.right - 10, 700));
  const ranges = tops.map((_, i) => {
    const range = document.createRange();
    range.selectNodeContents(source);
    range.getBoundingClientRect = () => new DOMRect(10, geometry.tops[i], 120, 20);
    return range;
  });
  const view = render(<>{ranges.map((range, i) =>
    <CommentSelectionBubble key={i} range={range} source={source} owner="test" markerRanges={ranges}>
      <button>{i + 1}</button>
    </CommentSelectionBubble>,
  )}</>);
  return { geometry, source, dispose: () => { view.unmount(); source.remove(); } };
}

// Real Floating UI middleware, with only browser geometry supplied by jsdom.
describe("annotation marker layout", () => {
  it("keeps a visible quote's marker visible at the viewport's right edge", async () => {
    const { dispose } = fixture([100]);
    const marker = await screen.findByRole("button", { name: "1" });
    expect(marker).toBeVisible();
    expect(parseFloat(marker.parentElement!.style.left)).toBeLessThanOrEqual(992);
    dispose();
  });

  it("tracks range movement without a resize event and hides offscreen quotes", async () => {
    const { geometry, dispose } = fixture([100], 600);
    const marker = await screen.findByRole("button", { name: "1" });
    const overlay = marker.parentElement!;
    await waitFor(() => expect(overlay.style.top).toBe("100px"));
    geometry.tops[0] = 240;
    await waitFor(() => expect(overlay.style.top).toBe("240px"));
    geometry.tops[0] = -100;
    await waitFor(() => expect(marker).not.toBeVisible());
    geometry.tops[0] = 300;
    await waitFor(() => expect(marker).toBeVisible());
    dispose();
  });

  it("stacks nearby markers without clamping them onto each other at the bottom", async () => {
    const { dispose } = fixture([740, 741]);
    const first = await screen.findByRole("button", { name: "1" });
    const second = await screen.findByRole("button", { name: "2" });
    expect(parseFloat(second.parentElement!.style.top) - parseFloat(first.parentElement!.style.top)).toBe(28);
    dispose();
  });
});
