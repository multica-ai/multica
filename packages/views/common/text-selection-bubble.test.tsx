import { render, screen, waitFor, cleanup } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TextSelectionBubble } from "./text-selection-bubble";

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
    <TextSelectionBubble key={i} range={range} source={source} owner="test" markerRanges={ranges}>
      <button>{i + 1}</button>
    </TextSelectionBubble>,
  )}</>);
  return { geometry, source, dispose: () => { view.unmount(); source.remove(); } };
}

// 使用真实 Floating UI 中间件，仅补齐 jsdom 缺少的浏览器几何信息。
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


describe("annotation editor layout", () => {
  it("keeps the portaled editor inside its source scroll panel", async () => {
    const panel = document.createElement("div");
    panel.style.cssText = "position: relative; overflow: auto; width: 500px; height: 700px;";
    const source = document.createElement("div");
    source.textContent = "Selected text near the left edge";
    panel.append(source);
    document.body.append(panel);
    vi.spyOn(panel, "getBoundingClientRect").mockReturnValue(new DOMRect(400, 0, 500, 700));
    for (const [key, value] of Object.entries({ clientWidth: 500, clientHeight: 700, offsetWidth: 500, offsetHeight: 700 })) {
      Object.defineProperty(panel, key, { configurable: true, value });
    }
    vi.spyOn(source, "getBoundingClientRect").mockReturnValue(new DOMRect(420, 100, 460, 500));
    const range = document.createRange();
    range.selectNodeContents(source);
    range.getBoundingClientRect = () => new DOMRect(420, 100, 100, 20);
    vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(320);
    const view = render(<TextSelectionBubble range={range} source={source} owner="test">
      <textarea aria-label="Annotation note" />
    </TextSelectionBubble>);
    try {
      const input = await screen.findByRole("textbox", { name: "Annotation note" });
      // 只按选区居中会把编辑器放到 x=310，盖住收件箱列表。
      // 使用原文所在裁剪容器作为浮层边界。
      await waitFor(() => expect(parseFloat(input.parentElement!.style.left)).toBe(408));
    } finally { view.unmount(); panel.remove(); }
  });
});
